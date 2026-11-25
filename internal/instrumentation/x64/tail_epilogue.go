package x64

import (
	"encoding/binary"
	"golang.org/x/arch/x86/x86asm"
)

// stackFrameLayout describes pushes followed by fixed allocation and optional
// frame-pointer setup. Memory saves and interleaved pushes need another layout.
func stackFrameLayout(info []byte) (uint64, []x86asm.Reg, bool) {
	if validateUnwindCodes(info) != nil {
		return 0, nil, false
	}
	var allocation uint64
	var pops []x86asm.Reg
	firstAllocation := 256
	var pushed []int
	for at := 0; at < int(info[2]); {
		offset, op := int(info[4+at*2]), info[5+at*2]
		extra := 0
		switch op & 15 {
		case 0:
			pops = append(pops, x86asm.RAX+x86asm.Reg(op>>4))
			pushed = append(pushed, offset)
		case 1:
			extra = 1
			amount := uint64(binary.LittleEndian.Uint16(info[6+at*2:])) * 8
			if op>>4 == 1 {
				extra = 2
				amount = uint64(binary.LittleEndian.Uint32(info[6+at*2:]))
			}
			allocation += amount
			if offset < firstAllocation {
				firstAllocation = offset
			}
		case 2:
			allocation += uint64(op>>4)*8 + 8
			if offset < firstAllocation {
				firstAllocation = offset
			}
		case 3:
		default:
			return 0, nil, false
		}
		at += 1 + extra
	}
	for _, offset := range pushed {
		if offset > firstAllocation {
			return 0, nil, false
		}
	}
	return allocation, pops, true
}

func verifiedTailEpilogues(code, info []byte) map[int]int {
	allocation, pops, ok := stackFrameLayout(info)
	if !ok {
		return nil
	}
	type instruction struct {
		at   int
		inst x86asm.Inst
	}
	var decoded []instruction
	for at := 0; at < len(code); {
		inst, err := x86asm.Decode(code[at:], 64)
		if err != nil || inst.Op == 0 || inst.Len < 1 || inst.Len > len(code)-at {
			return nil
		}
		decoded = append(decoded, instruction{at, inst})
		at += inst.Len
	}
	result := map[int]int{}
	for i, d := range decoded {
		if d.inst.Op != x86asm.JMP {
			continue
		}
		if _, direct := d.inst.Args[0].(x86asm.Rel); !direct && d.inst.Args[0] != x86asm.R11 {
			continue
		}
		begin := i - len(pops)
		if begin < 0 {
			continue
		}
		valid := true
		for j, reg := range pops {
			inst := decoded[begin+j].inst
			if inst.Op != x86asm.POP || inst.Args[0] != reg {
				valid = false
				break
			}
		}
		if !valid {
			continue
		}
		if allocation != 0 {
			begin--
			if begin < 0 {
				continue
			}
			inst := decoded[begin].inst
			restored := verifiedStackRelease(inst, info, allocation)
			if !restored {
				continue
			}
		}
		if begin == i || decoded[begin].at < int(info[1]) {
			continue
		}
		// No branch may bypass a suffix of the required restoration.
		// A branch to its first instruction remains a supported shared epilogue.
		for _, from := range decoded {
			rel, ok := from.inst.Args[0].(x86asm.Rel)
			if !ok || from.inst.Op == x86asm.CALL {
				continue
			}
			target := int64(from.at+from.inst.Len) + int64(rel)
			if target > int64(decoded[begin].at) && target <= int64(d.at) {
				valid = false
				break
			}
		}
		if valid {
			result[d.at] = decoded[begin].at
		}
	}
	return result
}

func verifiedStackRelease(inst x86asm.Inst, info []byte, allocation uint64) bool {
	restored := false
	if inst.Op == x86asm.ADD && inst.Args[0] == x86asm.RSP {
		imm, ok := inst.Args[1].(x86asm.Imm)
		restored = ok && imm > 0 && uint64(imm) == allocation
	}
	if inst.Op == x86asm.LEA && inst.Args[0] == x86asm.RSP && info[3]&15 != 0 {
		mem, ok := inst.Args[1].(x86asm.Mem)
		restored = ok && mem.Base == x86asm.RAX+x86asm.Reg(info[3]&15) && mem.Index == 0 && mem.Segment == 0 && mem.Disp == int64(allocation)-int64(info[3]>>4)*16
	}
	return restored
}
