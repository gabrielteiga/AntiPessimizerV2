package x64

import (
	"encoding/binary"
	"fmt"

	"golang.org/x/arch/x86/x86asm"
)

// Validate a shrink-wrapped general-purpose save in the final fragment. The
// primary allocates the entire frame. The secondary saves and reloads one
// nonvolatile register in that allocation, with no branch bypassing the save.
func validateLocalSave(code, primary []byte, start int, local []byte) error {
	fail := func() error {
		return fmt.Errorf("%w: chained nonvolatile save does not match verified frame and control flow", ErrUnsupported)
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
	displacement := int64(binary.LittleEndian.Uint16(local[6:])) * 8
	if displacement+8 > allocation {
		return fail()
	}
	register := x86asm.RAX + x86asm.Reg(local[5]>>4)
	mem := func(arg x86asm.Arg) bool {
		m, ok := arg.(x86asm.Mem)
		return ok && m.Base == x86asm.RSP && m.Segment == 0 && m.Index == 0 && m.Disp == displacement
	}
	var save, restore, release, ret x86asm.Inst
	for at := int(primary[1]); at < len(code); {
		inst, err := x86asm.Decode(code[at:], 64)
		if err != nil || inst.Op == 0 || inst.Len == 0 || at+inst.Len > len(code) {
			return fail()
		}
		next := at + inst.Len
		switch {
		case at == start:
			save = inst
		case at < start:
			if inst.Op != x86asm.NOP {
				rel, ok := inst.Args[0].(x86asm.Rel)
				if inst.Op != x86asm.JMP || !ok || int64(next)+int64(rel) != int64(start) {
					return fail()
				}
			}
		default:
			if restore.Op != 0 {
				if release.Op == 0 {
					release = inst
				} else if next == len(code) {
					ret = inst
				} else {
					return fail()
				}
			} else if inst.Op == x86asm.MOV && inst.Args[0] == register && mem(inst.Args[1]) {
				restore = inst
			} else if _, relative := inst.Args[0].(x86asm.Rel); relative || inst.Op == x86asm.RET || frameOperations[inst.Op.String()] || inst.Args[0] == x86asm.RSP || inst.Args[1] == x86asm.RSP {
				return fail()
			}
		}
		at = next
	}
	if save.Op != x86asm.MOV || save.Len != int(local[1]) || !mem(save.Args[0]) || save.Args[1] != register ||
		restore.Op != x86asm.MOV || release.Op != x86asm.ADD || release.Args[0] != x86asm.RSP || release.Args[1] != x86asm.Imm(allocation) || ret.Op != x86asm.RET {
		return fail()
	}
	return nil
}
