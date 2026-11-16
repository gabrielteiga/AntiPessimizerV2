// Package x64 constructs checked trampoline plans without changing executable
// memory. A plan alone is not proof that installing a hook is safe: native ABI,
// thread coordination, CFG/CET and Windows unwind registration are separate gates.
package x64

import (
	"encoding/binary"
	"errors"
	"fmt"
	"strings"

	"golang.org/x/arch/x86/x86asm"
)

var ErrUnsupported = errors.New("unsupported x64 hook plan")

const JumpSize = 14
const maxUserAddress = uint64(1) << 47

type Plan struct {
	Overwrite                   int
	Resume                      uint64
	Original, Patch, Trampoline []byte
}
type decoded struct {
	offset int
	inst   x86asm.Inst
}

var prefixOperations = map[string]bool{
	"PUSH": true, "POP": true, "MOV": true, "LEA": true, "ADD": true, "SUB": true,
	"AND": true, "OR": true, "XOR": true, "TEST": true, "CMP": true, "NOP": true,
	"XCHG": true, "MOVZX": true, "MOVSX": true, "MOVSXD": true,
	"MOVAPS": true, "MOVUPS": true, "MOVAPD": true, "MOVUPD": true,
	"MOVDQA": true, "MOVDQU": true, "MOVQ": true, "MOVD": true, "MOVSD": true, "MOVSS": true,
	"PXOR": true, "XORPS": true, "XORPD": true,
}

func unsupported(reason string) (Plan, error) {
	return Plan{}, fmt.Errorf("%w: %s", ErrUnsupported, reason)
}
func jump(target uint64) []byte {
	b := make([]byte, JumpSize)
	b[0] = 0xff
	b[1] = 0x25
	binary.LittleEndian.PutUint64(b[6:], target)
	return b
}
func address(base uint64, delta int64) (uint64, bool) {
	if delta < 0 {
		n := uint64(-(delta + 1)) + 1
		if n > base {
			return 0, false
		}
		return base - n, true
	}
	n := uint64(delta)
	if n > ^uint64(0)-base {
		return 0, false
	}
	return base + n, true
}

// Build accepts the entire known procedure extent. It decodes whole instructions,
// rejects control flow in the relocated prefix and incoming direct branches from
// the supplied body, and adjusts supported RIP-relative disp32 memory operands.
// The caller must separately exclude outside branches/entries into that prefix.
// Only ordinary user-space Windows x64 addresses below 2^47 are supported here.
func Build(code []byte, source, trampoline, hook uint64) (Plan, error) {
	if len(code) <= JumpSize || len(code) > 1<<20 {
		return unsupported("invalid procedure extent")
	}
	for _, a := range []uint64{source, trampoline, hook} {
		if a == 0 || a >= maxUserAddress {
			return unsupported("address outside supported user space")
		}
	}
	if uint64(len(code)) >= maxUserAddress-source || 64 >= maxUserAddress-trampoline {
		return unsupported("address range overflow")
	}
	end := source + uint64(len(code))
	if trampoline < end && trampoline+64 > source {
		return unsupported("trampoline overlaps procedure")
	}
	if hook >= source && hook < end || hook >= trampoline && hook < trampoline+64 {
		return unsupported("hook target overlaps procedure/trampoline")
	}
	var instructions []decoded
	consumed := 0
	decodeAt := func(offset int) (x86asm.Inst, error) {
		inst, err := x86asm.Decode(code[offset:], 64)
		if err != nil || inst.Op == 0 || inst.Len == 0 || inst.Len > len(code)-offset {
			return inst, fmt.Errorf("%w: invalid/truncated instruction", ErrUnsupported)
		}
		for _, p := range inst.Prefix {
			if p&x86asm.PrefixInvalid != 0 {
				return inst, fmt.Errorf("%w: invalid instruction prefix", ErrUnsupported)
			}
		}
		return inst, nil
	}
	for consumed < JumpSize {
		inst, err := decodeAt(consumed)
		if err != nil {
			return Plan{}, err
		}
		if !prefixOperations[inst.Op.String()] {
			return unsupported("control flow or unsupported instruction in prologue: " + inst.Op.String())
		}
		instructions = append(instructions, decoded{consumed, inst})
		consumed += inst.Len
	}
	if consumed >= len(code) {
		return unsupported("no remaining procedure body")
	}
	// Decode the body incrementally, retaining only the small relocated prefix.
	// Any relative operand can transfer control (including less common opcodes
	// such as XBEGIN), so inspect operands rather than a mnemonic-only allowlist.
	for offset := consumed; offset < len(code); {
		inst, err := decodeAt(offset)
		if err != nil {
			return Plan{}, err
		}
		control := strings.HasPrefix(inst.Op.String(), "J") || strings.HasPrefix(inst.Op.String(), "LOOP")
		relative := false
		for _, arg := range inst.Args {
			if rel, ok := arg.(x86asm.Rel); ok {
				relative = true
				target, valid := address(source+uint64(offset+inst.Len), int64(rel))
				if !valid {
					return unsupported("branch address overflow")
				}
				if target >= source && target < source+uint64(consumed) {
					return unsupported("body branches into overwritten prologue")
				}
			}
		}
		if control && !relative {
			return unsupported("indirect branch prevents local jump-back validation")
		}
		offset += inst.Len
	}
	relocated := append([]byte(nil), code[:consumed]...)
	for _, d := range instructions {
		inst := d.inst
		hasRIP := false
		for _, arg := range inst.Args {
			if _, ok := arg.(x86asm.Rel); ok {
				return unsupported("relative control operand in prologue")
			}
			mem, ok := arg.(x86asm.Mem)
			if !ok {
				continue
			}
			if mem.Base == x86asm.EIP {
				return unsupported("address-size-relative memory is not supported")
			}
			if mem.Base != x86asm.RIP {
				continue
			}
			if mem.Segment != 0 {
				return unsupported("segment-relative RIP operand is not supported")
			}
			hasRIP = true
			if inst.PCRel != 4 || inst.PCRelOff < 0 || inst.PCRelOff+4 > inst.Len {
				return unsupported("unsupported RIP-relative encoding")
			}
			disp := int64(int32(binary.LittleEndian.Uint32(code[d.offset+inst.PCRelOff:])))
			target, valid := address(source+uint64(d.offset+inst.Len), disp)
			if !valid {
				return unsupported("RIP target overflow")
			}
			if target == 0 || target >= maxUserAddress {
				return unsupported("RIP target outside supported user space")
			}
			if target >= source && target < source+uint64(consumed) {
				return unsupported("RIP target lies in overwritten bytes")
			}
			// Addresses are restricted to signed-positive user space, so this difference
			// cannot overflow int64. Validate disp32 before narrowing it.
			delta := int64(target) - int64(trampoline+uint64(d.offset+inst.Len))
			if delta < -(int64(1)<<31) || delta > 1<<31-1 {
				return unsupported("trampoline is too far from RIP-relative target")
			}
			binary.LittleEndian.PutUint32(relocated[d.offset+inst.PCRelOff:], uint32(int32(delta)))
		}
		if inst.PCRel != 0 && !hasRIP {
			return unsupported("unclassified position-dependent operand")
		}
	}
	patch := jump(hook)
	for len(patch) < consumed {
		patch = append(patch, 0x90)
	}
	resume := source + uint64(consumed)
	return Plan{Overwrite: consumed, Resume: resume, Original: append([]byte(nil), code[:consumed]...), Patch: patch, Trampoline: append(relocated, jump(resume)...)}, nil
}
