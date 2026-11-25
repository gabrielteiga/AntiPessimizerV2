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
	if len(primary) != 8 || primary[2] != 1 || primary[3] != 0 || primary[5]&15 != 2 || primary[4] != primary[1] {
		return fail()
	}
	allocation := int64(primary[5]>>4)*8 + 8
	info := append([]byte(nil), local...)
	info[0] = 1
	if validateUnwindCodes(info) != nil || info[4] != info[1] {
		return fail()
	}
	prologue, err := x86asm.Decode(code[:primary[1]], 64)
	if err != nil || prologue.Op != x86asm.SUB || prologue.Len != int(primary[1]) {
		return fail()
	}

	var registers, slots uint32
	for at := 4; at < len(local); at += 4 {
		reg := uint32(local[at+1] >> 4)
		slot := uint32(binary.LittleEndian.Uint16(local[at+2:]))
		width := uint32(1)
		switch local[at+1] & 15 {
		case 4:
		case 8:
			reg += 16
			slot *= 2
			width = 2
		default:
			return fail()
		}
		if (slot+width)*8 > uint32(allocation) {
			return fail()
		}
		mask := uint32((1<<width)-1) << slot
		if registers&(1<<reg) != 0 || slots&mask != 0 {
			return fail()
		}
		registers |= 1 << reg
		slots |= mask
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
	count := int(local[2]) / 2
	if len(fragment) < count*2+2 {
		return fail()
	}
	restore := len(fragment) - count - 2
	offset := 0
	for i := 0; i < count; i++ {
		// Metadata lists saves from last to first. The epilogue reloads
		// them in that same order.
		at := 4 + (count-1-i)*4
		reg := x86asm.RAX + x86asm.Reg(local[at+1]>>4)
		disp := int64(binary.LittleEndian.Uint16(local[at+2:])) * 8
		op := x86asm.MOV
		if local[at+1]&15 == 8 {
			reg = x86asm.X0 + x86asm.Reg(local[at+1]>>4)
			disp *= 2
			op = fragment[i].Op
			if op != x86asm.MOVAPS && op != x86asm.MOVUPS {
				return fail()
			}
		}
		matchesSlot := func(arg x86asm.Arg) bool {
			mem, ok := arg.(x86asm.Mem)
			return ok && mem.Base == x86asm.RSP && mem.Index == 0 && mem.Segment == 0 && mem.Disp == disp
		}
		save, load := fragment[i], fragment[restore+count-1-i]
		offset += save.Len
		if offset != int(local[at]) || save.Op != op || !matchesSlot(save.Args[0]) || save.Args[1] != reg ||
			load.Op != op || load.Args[0] != reg || !matchesSlot(load.Args[1]) {
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
