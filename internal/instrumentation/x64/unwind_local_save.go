package x64

import (
	"encoding/binary"
	"fmt"

	"golang.org/x/arch/x86/x86asm"
)

// The primary allocates the entire frame. The final fragment saves registers
// consecutively before other work and restores them before releasing the frame.
func validateLocalSave(code, primary []byte, start int, local []byte) error {
	fail := func() error {
		return fmt.Errorf("%w: chained nonvolatile saves do not match verified frame and control flow", ErrUnsupported)
	}
	if len(primary) < 8 || primary[3] != 0 || validateUnwindCodes(primary) != nil || validatePrologueActions(code, primary) != nil {
		return fail()
	}
	info := append([]byte(nil), local...)
	info[0] = 1
	if validateUnwindCodes(info) != nil || info[4] != info[1] {
		return fail()
	}
	prologue, err := x86asm.Decode(code[:primary[1]], 64)
	if err != nil || prologue.Op != x86asm.SUB || prologue.Len != int(primary[1]) {
		return fail()
	}

	allocation, ok := prologue.Args[1].(x86asm.Imm)
	if !ok || allocation <= 0 {
		return fail()
	}

	type savedRegister struct {
		end  byte
		reg  x86asm.Reg
		disp int64
		xmm  bool
	}
	var saves []savedRegister
	var registers uint32
	slots := make(map[uint32]bool)
	for at := 4; at < 4+int(local[2])*2; {
		op, number := local[at+1]&15, uint32(local[at+1]>>4)
		width, step := uint32(8), 4
		reg := x86asm.RAX + x86asm.Reg(number)
		xmm := op == 8 || op == 9
		if xmm {
			reg = x86asm.X0 + x86asm.Reg(number)
			number += 16
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
		if disp%width != 0 || uint64(disp)+uint64(width) > uint64(allocation) || registers&(1<<number) != 0 {
			return fail()
		}
		registers |= 1 << number
		for occupied := disp / 8; occupied < (disp+width)/8; occupied++ {
			if slots[occupied] {
				return fail()
			}
			slots[occupied] = true
		}
		saves = append(saves, savedRegister{local[at], reg, int64(disp), xmm})
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
	if len(fragment) < count*2+2 {
		return fail()
	}
	restore := len(fragment) - count - 2
	offset := 0
	for i := 0; i < count; i++ {
		// Metadata lists saves from last to first. The epilogue reloads
		// them in that same order.
		metadata := saves[count-1-i]
		op := x86asm.MOV
		if metadata.xmm {
			op = fragment[i].Op
			if op != x86asm.MOVAPS && op != x86asm.MOVUPS {
				return fail()
			}
		}
		matchesSlot := func(arg x86asm.Arg) bool {
			mem, ok := arg.(x86asm.Mem)
			return ok && mem.Base == x86asm.RSP && mem.Index == 0 && mem.Segment == 0 && mem.Disp == metadata.disp
		}
		save, load := fragment[i], fragment[restore+count-1-i]
		offset += save.Len
		if offset != int(metadata.end) || save.Op != op || !matchesSlot(save.Args[0]) || save.Args[1] != metadata.reg ||
			load.Op != op || load.Args[0] != metadata.reg || !matchesSlot(load.Args[1]) {
			return fail()
		}
	}
	for _, inst := range fragment[count:restore] {
		if _, relative := inst.Args[0].(x86asm.Rel); relative || inst.Op == x86asm.RET || frameOperations[inst.Op.String()] ||
			inst.Args[0] == x86asm.RSP || inst.Args[1] == x86asm.RSP {
			return fail()
		}
	}
	release, ret := fragment[len(fragment)-2], fragment[len(fragment)-1]
	if release.Op != x86asm.ADD || release.Args[0] != x86asm.RSP || release.Args[1] != x86asm.Imm(allocation) || ret.Op != x86asm.RET {
		return fail()
	}
	return nil
}
