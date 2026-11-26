package x64

import (
	"encoding/binary"
	"fmt"

	"golang.org/x/arch/x86/x86asm"
)

// The primary allocates the entire frame. The final fragment saves registers
// consecutively before other work and restores them before releasing the frame.
func validateLocalSave(code, primary []byte, start int, local []byte) error {
	_, _, err := verifiedLocalSaveBody(code, primary, start, local)
	return err
}

func verifiedLocalSaveBody(code, primary []byte, start int, local []byte) (int, int, error) {
	fail := func() (int, int, error) {
		return 0, 0, fmt.Errorf("%w: chained nonvolatile saves do not match verified frame and control flow", ErrUnsupported)
	}
	if len(primary) < 8 || len(local) < 8 || local[3] != primary[3] || validateUnwindCodes(primary) != nil || validatePrologueActions(code, primary) != nil {
		return fail()
	}
	info := append([]byte(nil), local...)
	info[0] = 1
	info[3] = 0 // the secondary inherits the already-established frame register
	if validateUnwindCodes(info) != nil || info[4] != info[1] {
		return fail()
	}
	stackInfo, primarySaves, ok := primarySaveLayout(code, primary)
	if !ok {
		return fail()
	}
	allocation, pops, ok := stackFrameLayout(stackInfo)
	if !ok || allocation == 0 {
		return fail()
	}
	primaryReloads := len(primarySaves)
	var saves []chainedSavedRegister
	var registers uint32
	slots := make(map[uint32]bool)
	reserve := func(save chainedSavedRegister) bool {
		width, number := uint64(8), uint32(save.reg-x86asm.RAX)
		if save.xmm {
			width, number = 16, uint32(save.reg-x86asm.X0)+16
		}
		if save.disp < 0 || uint64(save.disp)%width != 0 || uint64(save.disp)+width > allocation || registers&(1<<number) != 0 {
			return false
		}
		registers |= 1 << number
		for slot := uint32(save.disp / 8); slot < uint32((uint64(save.disp)+width)/8); slot++ {
			if slots[slot] {
				return false
			}
			slots[slot] = true
		}
		return true
	}
	for _, reg := range pops {
		registers |= 1 << uint32(reg-x86asm.RAX)
	}
	for _, save := range primarySaves {
		if !reserve(save) || !alignedChainedSave(save.op, save.disp, allocation, len(pops)) {
			return fail()
		}
	}

	for at := 4; at < 4+int(local[2])*2; {
		op, number := local[at+1]&15, uint32(local[at+1]>>4)
		width, step := uint32(8), 4
		reg := x86asm.RAX + x86asm.Reg(number)
		xmm := op == 8 || op == 9
		if xmm {
			reg = x86asm.X0 + x86asm.Reg(number)
			width = 16
		}
		disp := uint32(binary.LittleEndian.Uint16(local[at+2:])) * width
		switch op {
		case 4, 8:
		case 5, 9:
			disp = binary.LittleEndian.Uint32(local[at+2:])
			step = 6
		default:
			return fail()
		}
		save := chainedSavedRegister{end: local[at], reg: reg, disp: int64(disp), xmm: xmm}
		if !reserve(save) {
			return fail()
		}
		saves = append(saves, save)
		at += step
	}

	var fragment []x86asm.Inst
	for at := int(primary[1]); at < len(code); {
		inst, err := x86asm.Decode(code[at:], 64)
		if err != nil || inst.Op == 0 || inst.Len == 0 || at+inst.Len > len(code) {
			return fail()
		}
		next := at + inst.Len
		if at < start {
			if inst.Op != x86asm.NOP {
				rel, ok := inst.Args[0].(x86asm.Rel)
				if inst.Op != x86asm.JMP || !ok || int64(next)+int64(rel) != int64(start) {
					return fail()
				}
			}
		} else {
			fragment = append(fragment, inst)
		}
		at = next
	}
	count := len(saves)
	if len(fragment) < count*2+len(pops)+2+primaryReloads {
		return fail()
	}
	restore := len(fragment) - count - len(pops) - 2 - primaryReloads
	offset := 0
	for i := 0; i < count; i++ {
		// Metadata lists saves from last to first. The epilogue reloads
		// them in that same order.
		metadata := saves[count-1-i]
		op := x86asm.MOV
		if metadata.xmm {
			op = fragment[i].Op
			switch op {
			case x86asm.MOVAPS, x86asm.MOVUPS, x86asm.MOVAPD, x86asm.MOVUPD, x86asm.MOVDQA, x86asm.MOVDQU:
			default:
				return fail()
			}
			// Win64 entry RSP includes an eight-byte return slot. The
			// primary's allocation and pushes determine each save's alignment.
			if !alignedChainedSave(op, metadata.disp, allocation, len(pops)) {
				return fail()
			}
		}
		matchesSlot := func(arg x86asm.Arg) bool {
			mem, ok := arg.(x86asm.Mem)
			return ok && savedAtOffset(mem, metadata.disp, 0, primary[3], primary[3]&15 != 0)
		}
		save, load := fragment[i], fragment[restore+count-1-i]
		offset += save.Len
		if offset != int(metadata.end) || save.Op != op || !matchesSlot(save.Args[0]) || save.Args[1] != metadata.reg ||
			load.Op != op || load.Args[0] != metadata.reg || !matchesSlot(load.Args[1]) {
			return fail()
		}
	}
	bodyStart, bodyEnd := start+offset, start+offset
	for _, inst := range fragment[count:restore] {
		bodyEnd += inst.Len
	}
	bodyPC := bodyStart
	for _, inst := range fragment[count:restore] {
		bodyPC += inst.Len
		if rel, relative := inst.Args[0].(x86asm.Rel); relative && inst.Op != x86asm.CALL {
			op := inst.Op.String()
			target := int64(bodyPC) + int64(rel)
			if len(op) == 0 || op[0] != 'J' || target < int64(bodyStart) || target > int64(bodyEnd) {
				return fail()
			}
			// The normal control-flow planner also requires instruction
			// boundaries and a supported branch encoding. No path may enter
			// a partial save or skip an epilogue reload.
			continue
		}
		if inst.Op == x86asm.CALL {
			if _, direct := inst.Args[0].(x86asm.Rel); !direct || inst.Len != 5 || (allocation+uint64(len(pops))*8)%16 != 8 || allocation < 32 {
				return fail()
			}
			// A callee owns the first four eight-byte home slots. Saves
			// must survive a callee that writes every Win64 argument slot.
			for slot := uint32(0); slot < 4; slot++ {
				if slots[slot] {
					return fail()
				}
			}
			continue
		}
		if _, relative := inst.Args[0].(x86asm.Rel); relative || inst.Op == x86asm.RET || frameOperations[inst.Op.String()] ||
			inst.Args[0] == x86asm.RSP || inst.Args[1] == x86asm.RSP {
			return fail()
		}
	}
	for i, save := range primarySaves {
		load := fragment[restore+count+i]
		mem, ok := load.Args[1].(x86asm.Mem)
		if load.Op != save.op || load.Args[0] != save.reg || !ok || !savedAtOffset(mem, save.disp, 0, primary[3], primary[3]&15 != 0) {
			return fail()
		}
	}
	releaseAt := len(fragment) - len(pops) - 2
	release, ret := fragment[releaseAt], fragment[len(fragment)-1]
	if !verifiedStackRelease(release, primary, allocation) || ret.Op != x86asm.RET {
		return fail()
	}
	for i, reg := range pops {
		inst := fragment[releaseAt+1+i]
		if inst.Op != x86asm.POP || inst.Args[0] != reg {
			return fail()
		}
	}
	return bodyStart, bodyEnd, nil
}

// Memory saves are removed only for stack-layout calculation. The original
// metadata still describes each copied instruction and is emitted unchanged.
type chainedSavedRegister struct {
	end  byte
	reg  x86asm.Reg
	disp int64
	xmm  bool
	op   x86asm.Op
}

func alignedChainedSave(op x86asm.Op, disp int64, allocation uint64, pushes int) bool {
	aligned := op == x86asm.MOVAPS || op == x86asm.MOVAPD || op == x86asm.MOVDQA
	return !aligned || (uint64(disp)+8)%16 == (allocation+uint64(pushes)*8)%16
}

func primarySaveLayout(code, info []byte) ([]byte, []chainedSavedRegister, bool) {
	stack := append([]byte(nil), info[:4]...)
	stack[0], stack[2] = 1, 0
	var saves []chainedSavedRegister
	ends := make(map[byte]x86asm.Inst)
	for at := 0; at < int(info[1]); {
		inst, err := x86asm.Decode(code[at:], 64)
		if err != nil || inst.Len == 0 {
			return nil, nil, false
		}
		at += inst.Len
		ends[byte(at)] = inst
	}
	for at := 4; at < 4+int(info[2])*2; {
		op, number := info[at+1]&15, info[at+1]>>4
		step := 2
		switch op {
		case 1:
			step += 2 + int(number)*2
		case 4, 8:
			step = 4
		case 5, 9:
			step = 6
		}
		if op == 4 || op == 5 || op == 8 || op == 9 {
			xmm := op == 8 || op == 9
			width, reg := int64(8), x86asm.RAX+x86asm.Reg(number)
			if xmm {
				width, reg = 16, x86asm.X0+x86asm.Reg(number)
			}
			disp := int64(binary.LittleEndian.Uint16(info[at+2:])) * width
			if step == 6 {
				disp = int64(binary.LittleEndian.Uint32(info[at+2:]))
			}
			saves = append(saves, chainedSavedRegister{end: info[at], reg: reg, disp: disp, xmm: xmm, op: ends[info[at]].Op})
		} else {
			stack = append(stack, info[at:at+step]...)
			stack[2] += byte(step / 2)
		}
		at += step
	}
	if stack[2]&1 != 0 {
		stack = append(stack, 0, 0)
	}
	return stack, saves, true
}

// The handler's full-body shadow needs every save even though the registered
// clone retains separate chained ranges. Version-1 code offsets are one byte.
func combineScopedUnwind(primary, local []byte, localStart uint32) ([]byte, error) {
	fail := func() ([]byte, error) {
		return nil, fmt.Errorf("%w: scoped local saves exceed the verified shadow unwind layout", ErrUnsupported)
	}
	if validateUnwindCodes(primary) != nil || len(local) < 4 || local[0] != 33 || local[3] != primary[3] || local[2] == 0 || localStart < uint32(primary[1]) || localStart > 255 || localStart+uint32(local[1]) > 255 || int(primary[2])+int(local[2]) > 255 {
		return fail()
	}
	localInfo := append([]byte(nil), local...)
	localInfo[0], localInfo[3] = 1, 0
	if validateUnwindCodes(localInfo) != nil {
		return fail()
	}
	count := int(primary[2]) + int(local[2])
	result := make([]byte, 4+((count+1)&^1)*2)
	copy(result, primary[:4])
	result[1], result[2] = byte(localStart+uint32(local[1])), byte(count)
	copy(result[4:], local[4:4+int(local[2])*2])
	for at := 4; at < 4+int(local[2])*2; {
		op := result[at+1] & 15
		if op != 4 && op != 5 && op != 8 && op != 9 {
			return fail()
		}
		result[at] += byte(localStart)
		if op == 5 || op == 9 {
			at += 6
		} else {
			at += 4
		}
	}
	copy(result[4+int(local[2])*2:], primary[4:4+int(primary[2])*2])
	if validateUnwindCodes(result) != nil {
		return fail()
	}
	return result, nil
}
