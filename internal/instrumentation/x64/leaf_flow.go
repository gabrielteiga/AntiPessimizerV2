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
	kind                      byte // 0=ordinary, 1=return, 2=jump, 3=conditional, 4=call, 5=selected tail jump
	condition                 byte
	target                    int
	requiresPrevious          bool
}
type leafBody struct {
	code        []byte
	overwrite   int
	exitRVA     uint32
	dataTargets []uint64
	tailTargets []uint64
	pcMap       []pcMapping
}

type pcMapping struct{ original, relocated uint16 }

// rewriteLeafBody widens internal relative branches and redirects every RET.
// Every target must be a decoded instruction boundary inside this procedure.
// No direct/indirect calls, external branches or implicit stack changes exist in
// this subset. The copied body therefore remains a native leaf for unwind.
func rewriteLeafBody(code []byte, source, base uint64) (leafBody, error) {
	return rewriteFunctionBody(code, source, base, false, 0)
}
func rewriteFunctionBody(code []byte, source, base uint64, framed bool, prologue int) (leafBody, error) {
	return rewriteFunctionWithPatch(code, source, base, framed, prologue, JumpSize, false)
}
func rewriteFunctionWithPatch(code []byte, source, base uint64, framed bool, prologue, patchSize int, nativeFunction bool) (leafBody, error) {
	return rewriteSelectedFunction(code, source, base, framed, prologue, patchSize, nativeFunction, nil, nil)
}
func rewriteSelectedFunction(code []byte, source, base uint64, framed bool, prologue, patchSize int, nativeFunction bool, selected map[uint64]bool, restored map[int]bool) (leafBody, error) {
	fail := func(reason string) (leafBody, error) {
		return leafBody{}, fmt.Errorf("%w: leaf: %s", ErrUnsupported, reason)
	}
	if len(code) < patchSize {
		return fail("procedure is smaller than the entry jump")
	}
	var instructions []leafInstruction
	offsets := map[int]int{}
	total, overwrite := 0, 0
	var tailTargets []uint64
	for offset := 0; offset < len(code); {
		inst, err := x86asm.Decode(code[offset:], 64)
		if err != nil || inst.Op == 0 || inst.Len == 0 || inst.Len > len(code)-offset {
			return fail("invalid instruction")
		}
		for _, p := range inst.Prefix {
			if p&x86asm.PrefixInvalid != 0 || p&0xff == x86asm.PrefixLOCK && (!nativeFunction || !lockedMemoryOperation(inst)) {
				return fail("unsupported prefix")
			}
		}
		if stringOperations[inst.Op.String()] && (!nativeFunction || !framed || inst.AddrSize != 64) {
			return fail("string operation requires a verified native frame and 64-bit addresses")
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
		case nativeFunction && !framed && prologue == 0 && inst.Op == x86asm.JMP &&
			inst.Len == 3 && code[offset] == 0x41 && code[offset+1] == 0xff && code[offset+2] == 0xe3:
			// JMP R11 can share the selected callee's frame only when the
			// immediately preceding LEA proves the exact destination.
			if len(instructions) == 0 {
				return fail("indirect tail jump has no known destination")
			}
			previous := instructions[len(instructions)-1]
			target, ok := ripTailAddress(code, source, previous)
			if !ok || target >= source && target < source+uint64(len(code)) || !selected[target] {
				return fail("indirect tail jump requires an exact selected destination")
			}
			d.kind, d.length, d.absolute, d.requiresPrevious = 5, 5, target, true
			tailTargets = append(tailTargets, target)
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
		case inst.Op == x86asm.RDTSC:
			// RDTSC produces EDX:EAX without changing stack state. Preserve
			// the precise encoding and keep it in the full native planner.
			if !nativeFunction || inst.Len != 2 || code[offset] != 0x0f || code[offset+1] != 0x31 {
				return fail("unsupported timestamp counter instruction")
			}
		default:
			if !leafOperations[inst.Op.String()] && !scalarOperations[inst.Op.String()] && !(framed && frameOperations[inst.Op.String()]) && !(nativeFunction && (atomicOperations[inst.Op.String()] || mxcsrOperations[inst.Op.String()] || framed && (stringOperations[inst.Op.String()] || wideAtomicOperation(inst)))) {
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
				if !ok || d.kind != 2 || !nativeFunction || (framed || prologue != 0) && !restored[offset] || !selected[target] {
					return fail("branch leaves the procedure; direct tail calls require selecting their exact target and verified stack restoration")
				}
				d.kind = 5
				d.absolute = target
				tailTargets = append(tailTargets, target)
			} else {
				d.target = int(target - source)
			}
		}
		if offset < prologue && d.length != inst.Len {
			return fail("relocation changes original prologue offsets")
		}
		offsets[offset] = len(instructions)
		instructions = append(instructions, d)
		if offset < patchSize {
			overwrite = offset + inst.Len
		}
		total += d.length
		limit := 6144
		if nativeFunction {
			limit = 12288
		}
		if total > limit {
			return fail("expanded body exceeds allocation limit")
		}
		offset += inst.Len
	}
	if len(instructions) == 0 {
		return fail("missing final RET")
	}
	last := instructions[len(instructions)-1].kind
	if last != 1 && last != 5 && !(framed && last == 2) {
		return fail("body falls through its declared extent")
	}
	for _, d := range instructions {
		if d.kind == 2 || d.kind == 3 {
			i, ok := offsets[d.target]
			if !ok {
				return fail("branch targets the middle of an instruction")
			}
			if instructions[i].requiresPrevious {
				return fail("branch bypasses the indirect tail destination assignment")
			}
		}
	}
	out := leafBody{tailTargets: tailTargets, code: make([]byte, total), overwrite: overwrite, exitRVA: uint32((512 + total + 15) &^ 15)}
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
		if d.kind == 4 || d.kind == 5 {
			delta := int64(d.absolute) - int64(base+512+uint64(d.relocated+5))
			if delta < -1<<31 || delta > 1<<31-1 {
				return fail("call target too far from clone")
			}
			dst[0] = 0xe8
			if d.kind == 5 {
				dst[0] = 0xe9
			}
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
				flatString := stringOperations[d.inst.Op.String()] && (arg.Segment == x86asm.ES || arg.Segment == x86asm.DS)
				if arg.Segment != 0 && !flatString && !(framed && (arg.Segment == x86asm.GS || arg.Segment == x86asm.FS)) || arg.Base == x86asm.EIP {
					return fail("segmented/address-size memory")
				}
				if !framed && (arg.Index == x86asm.RSP || arg.Index == x86asm.ESP) {
					return fail("stack index")
				}
				if !framed && arg.Base == x86asm.RSP {
					if arg.Index != 0 || arg.Disp < 8 {
						return fail("return slot or indexed stack memory")
					}
					readOnly := d.inst.Op == x86asm.CMP || d.inst.Op == x86asm.TEST || d.inst.Op == x86asm.BT || d.inst.Op == x86asm.MUL || d.inst.Op == x86asm.IMUL || d.inst.Op == x86asm.DIV || d.inst.Op == x86asm.IDIV || d.inst.Op == x86asm.LDMXCSR
					writes := operand == 0 && !readOnly || d.inst.Op == x86asm.XCHG
					if writes && (d.inst.Op == x86asm.BTS || d.inst.Op == x86asm.BTR || d.inst.Op == x86asm.BTC) {
						bit, constant := d.inst.Args[1].(x86asm.Imm)
						if !constant || bit < 0 || int64(bit) >= int64(d.inst.MemBytes)*8 {
							return fail("indexed bit write can escape home slots")
						}
					}
					if writes && (!nativeFunction || d.inst.MemBytes <= 0 || arg.Disp+int64(d.inst.MemBytes) > 40) {
						return fail("stack write outside Win64 home slots")
					}
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
	out.pcMap = []pcMapping{{0, 0}}
	for _, d := range instructions {
		if d.length != d.inst.Len {
			out.pcMap = append(out.pcMap, pcMapping{uint16(d.offset + d.inst.Len), uint16(d.relocated + d.length)})
		}
	}
	return out, nil
}

func ripTailAddress(code []byte, source uint64, previous leafInstruction) (uint64, bool) {
	inst := previous.inst
	mem, ok := inst.Args[1].(x86asm.Mem)
	if inst.Op != x86asm.LEA || inst.Args[0] != x86asm.R11 || !ok || mem.Base != x86asm.RIP ||
		mem.Index != 0 || mem.Segment != 0 || inst.PCRel != 4 || inst.PCRelOff < 0 || inst.PCRelOff+4 > inst.Len {
		return 0, false
	}
	at := previous.offset + inst.PCRelOff
	displacement := int64(int32(binary.LittleEndian.Uint32(code[at:])))
	return address(source+uint64(previous.offset+inst.Len), displacement)
}

var frameOperations = map[string]bool{"PUSH": true, "POP": true, "LEAVE": true, "CALL": true}

func nativeFrameRegister(r x86asm.Reg) bool {
	return r >= x86asm.AL && r <= x86asm.R15B || r >= x86asm.AX && r <= x86asm.R15W || r >= x86asm.EAX && r <= x86asm.R15L || r >= x86asm.RAX && r <= x86asm.R15 || r >= x86asm.X0 && r <= x86asm.X15
}
