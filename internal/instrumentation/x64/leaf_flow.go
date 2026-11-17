package x64

import (
	"encoding/binary"
	"fmt"

	"golang.org/x/arch/x86/x86asm"
)

type leafInstruction struct {
	absolute uint64

	offset, relocated, length int
	inst                      x86asm.Inst
	kind                      byte // 0=ordinary, 1=return, 2=jump, 3=conditional, 4=call
	condition                 byte
	target                    int
}
type leafBody struct {
	code        []byte
	overwrite   int
	exitRVA     uint32
	dataTargets []uint64
}

// rewriteLeafBody widens internal relative branches and redirects every RET.
// Every target must be a decoded instruction boundary inside this procedure.
// No direct/indirect calls, external branches or implicit stack changes exist in
// this subset. The copied body therefore remains a native leaf for unwind.
func rewriteLeafBody(code []byte, source, base uint64) (leafBody, error) {
	return rewriteFunctionBody(code, source, base, false, 0)
}
func rewriteFunctionBody(code []byte, source, base uint64, framed bool, prologue int) (leafBody, error) {
	fail := func(reason string) (leafBody, error) {
		return leafBody{}, fmt.Errorf("%w: leaf: %s", ErrUnsupported, reason)
	}
	var instructions []leafInstruction
	offsets := map[int]int{}
	total, overwrite := 0, 0
	for offset := 0; offset < len(code); {
		inst, err := x86asm.Decode(code[offset:], 64)
		if err != nil || inst.Op == 0 || inst.Len == 0 || inst.Len > len(code)-offset {
			return fail("invalid instruction")
		}
		for _, p := range inst.Prefix {
			if p&x86asm.PrefixInvalid != 0 || p&0xff == x86asm.PrefixLOCK {
				return fail("unsupported prefix")
			}
		}
		d := leafInstruction{offset: offset, relocated: total, length: inst.Len, inst: inst}
		switch {
		case inst.Op == x86asm.RET:
			if inst.Len != 1 || code[offset] != 0xc3 {
				return fail("only near RET is supported")
			}
			d.kind = 1
			d.length = JumpSize
		case inst.Op == x86asm.JMP && (code[offset] == 0xeb && inst.Len == 2 || code[offset] == 0xe9 && inst.Len == 5):
			d.kind = 2
			d.length = 5
		case inst.Len == 2 && code[offset] >= 0x70 && code[offset] <= 0x7f:
			d.kind = 3
			d.condition = code[offset] & 15
			d.length = 6
		case inst.Len == 6 && code[offset] == 0x0f && code[offset+1] >= 0x80 && code[offset+1] <= 0x8f:
			d.kind = 3
			d.condition = code[offset+1] & 15
			d.length = 6
		case framed && inst.Op == x86asm.CALL && code[offset] == 0xe8 && inst.Len == 5:
			d.kind = 4
			rel, ok := inst.Args[0].(x86asm.Rel)
			if !ok {
				return fail("invalid relative call")
			}
			target, ok := address(source+uint64(offset+inst.Len), int64(rel))
			if !ok || target == 0 || target >= maxUserAddress {
				return fail("invalid call target")
			}
			if target > source && target < source+uint64(len(code)) {
				return fail("call into procedure interior")
			}
			d.absolute = target
		default:
			if !leafOperations[inst.Op.String()] && !(framed && frameOperations[inst.Op.String()]) {
				return fail("unsupported operation: " + inst.Op.String())
			}
		}
		if d.kind == 2 || d.kind == 3 {
			rel, ok := inst.Args[0].(x86asm.Rel)
			if !ok {
				return fail("invalid branch operand")
			}
			target, ok := address(source+uint64(offset+inst.Len), int64(rel))
			if !ok || target < source || target >= source+uint64(len(code)) {
				return fail("branch leaves the procedure")
			}
			d.target = int(target - source)
		}
		if offset < prologue && d.length != inst.Len {
			return fail("relocation changes original prologue offsets")
		}
		offsets[offset] = len(instructions)
		instructions = append(instructions, d)
		if offset < JumpSize {
			overwrite = offset + inst.Len
		}
		total += d.length
		if total > 6144 {
			return fail("expanded body exceeds allocation limit")
		}
		offset += inst.Len
	}
	if len(instructions) == 0 {
		return fail("missing final RET")
	}
	last := instructions[len(instructions)-1].kind
	if last != 1 && !(framed && last == 2) {
		return fail("body falls through its declared extent")
	}
	for _, d := range instructions {
		if d.kind == 2 || d.kind == 3 {
			if _, ok := offsets[d.target]; !ok {
				return fail("branch targets the middle of an instruction")
			}
		}
	}
	out := leafBody{code: make([]byte, total), overwrite: overwrite, exitRVA: uint32((512 + total + 15) &^ 15)}
	for _, d := range instructions {
		dst := out.code[d.relocated : d.relocated+d.length]
		if d.kind == 1 {
			copy(dst, jump(base+uint64(out.exitRVA)))
			continue
		}
		if d.kind == 2 || d.kind == 3 {
			target := instructions[offsets[d.target]].relocated
			delta := int32(target - (d.relocated + d.length))
			at := 1
			if d.kind == 2 {
				dst[0] = 0xe9
			} else {
				dst[0] = 0x0f
				dst[1] = 0x80 | d.condition
				at = 2
			}
			binary.LittleEndian.PutUint32(dst[at:], uint32(delta))
			continue
		}
		if d.kind == 4 {
			delta := int64(d.absolute) - int64(base+512+uint64(d.relocated+5))
			if delta < -1<<31 || delta > 1<<31-1 {
				return fail("call target too far from clone")
			}
			dst[0] = 0xe8
			binary.LittleEndian.PutUint32(dst[1:], uint32(int32(delta)))
			continue
		}
		copy(dst, code[d.offset:d.offset+d.inst.Len])
		hasRIP := false
		for operand, arg := range d.inst.Args {
			switch arg := arg.(type) {
			case x86asm.Rel:
				return fail("unsupported relative control flow")
			case x86asm.Reg:
				if !volatileLeafRegister(arg) && !(framed && nativeFrameRegister(arg)) {
					return fail("stack, nonvolatile or unsupported register")
				}
			case x86asm.Mem:
				if arg.Segment != 0 && !(framed && (arg.Segment == x86asm.GS || arg.Segment == x86asm.FS)) || arg.Base == x86asm.EIP {
					return fail("segmented/address-size memory")
				}
				if !framed && (arg.Index == x86asm.RSP || arg.Index == x86asm.ESP) {
					return fail("stack index")
				}
				if !framed && arg.Base == x86asm.RSP && (operand == 0 || arg.Index != 0 || arg.Disp < 8) {
					return fail("stack write or non-argument stack read")
				}
				if arg.Base == x86asm.ESP {
					return fail("address-size stack memory")
				}
				if arg.Base != x86asm.RIP {
					continue
				}
				hasRIP = true
				if d.inst.PCRel != 4 || d.inst.PCRelOff < 0 || d.inst.PCRelOff+4 > d.inst.Len {
					return fail("unsupported RIP encoding")
				}
				// x86asm may present disp32 as an unsigned-looking Mem.Disp.
				// The CPU sign extends the encoded displacement in RIP mode.
				disp := int64(int32(binary.LittleEndian.Uint32(code[d.offset+d.inst.PCRelOff:])))
				target, ok := address(source+uint64(d.offset+d.inst.Len), disp)
				if !ok || target == 0 || target >= maxUserAddress {
					return fail("invalid RIP target")
				}
				if target >= source && target < source+uint64(len(code)) {
					return fail("RIP reference into replaced procedure")
				}
				delta := int64(target) - int64(base+512+uint64(d.relocated+d.inst.Len))
				if delta < -1<<31 || delta > 1<<31-1 {
					return fail("RIP target too far from clone")
				}
				binary.LittleEndian.PutUint32(dst[d.inst.PCRelOff:], uint32(int32(delta)))
				out.dataTargets = append(out.dataTargets, target)
			}
		}
		if d.inst.PCRel != 0 && !hasRIP {
			return fail("unclassified position-dependent operand")
		}
	}
	return out, nil
}

var frameOperations = map[string]bool{"PUSH": true, "POP": true, "LEAVE": true, "CALL": true, "INC": true, "DEC": true, "NEG": true, "NOT": true, "IMUL": true, "MUL": true, "IDIV": true, "DIV": true, "SHL": true, "SHR": true, "SAR": true, "SAL": true, "ADC": true, "SBB": true, "CDQE": true, "CQO": true, "CDQ": true, "CMOVE": true, "CMOVNE": true, "CMOVG": true, "CMOVGE": true, "CMOVL": true, "CMOVLE": true, "CMOVA": true, "CMOVAE": true, "CMOVB": true, "CMOVBE": true, "SETE": true, "SETNE": true, "SETG": true, "SETGE": true, "SETL": true, "SETLE": true, "CVTSI2SD": true, "CVTTSD2SI": true, "UCOMISD": true, "COMISD": true}

func nativeFrameRegister(r x86asm.Reg) bool {
	return r >= x86asm.AL && r <= x86asm.R15B || r >= x86asm.AX && r <= x86asm.R15W || r >= x86asm.EAX && r <= x86asm.R15L || r >= x86asm.RAX && r <= x86asm.R15 || r >= x86asm.X0 && r <= x86asm.X15
}
