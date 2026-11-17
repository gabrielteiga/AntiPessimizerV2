package x64

import (
	"encoding/binary"
	"fmt"

	"golang.org/x/arch/x86/x86asm"
)

// RuntimeFunction uses RVAs relative to the allocation containing a LeafPlan.
// Register these ranges with RtlAddFunctionTable before exposing any entry.
// The cloned body is a native leaf: it neither changes RSP nor saves nonvolatile
// registers, and Windows unwinds it using the original return address at [RSP].
type RuntimeFunction struct{ Begin, End, Unwind uint32 }

// LeafPlan clones a restricted, straight-line native leaf and replaces its RET
// with a jump to the exit probe. It never replaces the caller's return address.
// This is a byte plan, not an installer or a claim of general Delphi support.
// The owner must verify the source is a leaf with no exception metadata, exclude
// external entries into its interior, and keep every selected caller quiescent
// while applying/restoring Patch. Source extent must come from trusted metadata.
type LeafPlan struct {
	Original, Patch, Image []byte
	Source                 []byte // complete expected source bytes, before entry patch
	BodyRVA, ExitRVA       uint32
	Functions              [2]RuntimeFunction
}

// BuildLeaf supports volatile Win64 GPRs/XMM0..5, simple arithmetic and data
// movement, read-only stack operands and RIP-relative data within disp32 reach.
// Calls, branches, stack/nonvolatile writes, RET imm16, handlers and embedded
// data are rejected. Unsupported procedures must remain unmodified.
func BuildLeaf(code []byte, source, base, enter, exit uint64, id uint32) (LeafPlan, error) {
	fail := func(reason string) (LeafPlan, error) {
		return LeafPlan{}, fmt.Errorf("%w: leaf: %s", ErrUnsupported, reason)
	}
	if len(code) <= JumpSize || len(code) > 4096 {
		return fail("invalid procedure extent")
	}
	if source == 0 || source >= maxUserAddress || uint64(len(code)) >= maxUserAddress-source {
		return fail("invalid source range")
	}
	if base == 0 || base >= maxUserAddress || uint64(len(code))+2048 >= maxUserAddress-base {
		return fail("invalid allocation range")
	}
	const bodyRVA = 512
	body := append([]byte(nil), code...)
	returned := false
	overwrite := 0
	var dataTargets []uint64
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
		if inst.Op == x86asm.RET {
			if inst.Len != 1 || code[offset] != 0xc3 || offset+1 != len(code) {
				return fail("only a final near RET is supported")
			}
			returned = true
			break
		}
		if !leafOperations[inst.Op.String()] {
			return fail("unsupported operation: " + inst.Op.String())
		}
		hasRIP := false
		for operand, arg := range inst.Args {
			switch arg := arg.(type) {
			case x86asm.Rel:
				return fail("relative control flow")
			case x86asm.Reg:
				if !volatileLeafRegister(arg) {
					return fail("stack, nonvolatile or unsupported register")
				}
			case x86asm.Mem:
				if arg.Segment != 0 || arg.Base == x86asm.EIP {
					return fail("segmented/address-size memory")
				}
				if arg.Index == x86asm.RSP || arg.Index == x86asm.ESP {
					return fail("stack index")
				}
				if arg.Base == x86asm.RSP {
					if operand == 0 || arg.Index != 0 || arg.Disp < 8 {
						return fail("stack write or non-argument stack read")
					}
				}
				if arg.Base == x86asm.ESP {
					return fail("address-size stack memory")
				}
				if arg.Base != x86asm.RIP {
					continue
				}
				hasRIP = true
				if inst.PCRel != 4 || inst.PCRelOff < 0 || inst.PCRelOff+4 > inst.Len {
					return fail("unsupported RIP encoding")
				}
				target, ok := address(source+uint64(offset+inst.Len), arg.Disp)
				if !ok || target == 0 || target >= maxUserAddress {
					return fail("invalid RIP target")
				}
				if target >= source && target < source+uint64(len(code)) {
					return fail("RIP reference into the replaced procedure")
				}
				dataTargets = append(dataTargets, target)
				delta := int64(target) - int64(base+bodyRVA+uint64(offset+inst.Len))
				if delta < -1<<31 || delta > 1<<31-1 {
					return fail("RIP target too far from clone")
				}
				binary.LittleEndian.PutUint32(body[offset+inst.PCRelOff:], uint32(int32(delta)))
			}
		}
		if inst.PCRel != 0 && !hasRIP {
			return fail("unclassified position-dependent operand")
		}
		if offset < JumpSize {
			overwrite = offset + inst.Len
		}
		offset += inst.Len
	}
	if !returned {
		return fail("missing final RET")
	}
	exitRVA := uint32((bodyRVA + len(body) - 1 + JumpSize + 15) &^ 15)
	in, err := BuildEntryProbe(base, enter, base+bodyRVA, id)
	if err != nil {
		return LeafPlan{}, err
	}
	out, err := BuildExitProbe(base+uint64(exitRVA), exit, id)
	if err != nil {
		return LeafPlan{}, err
	}
	imageSize := uint64(exitRVA) + uint64(len(out.Image))
	if base < source+uint64(len(code)) && base+imageSize > source {
		return fail("source overlaps allocation")
	}
	for _, target := range dataTargets {
		if target >= base && target < base+imageSize {
			return fail("RIP data overlaps allocation")
		}
	}
	for _, helper := range []uint64{enter, exit} {
		if helper >= source && helper < source+uint64(len(code)) || helper >= base && helper < base+imageSize {
			return fail("helper overlaps source or allocation")
		}
	}
	// The whole leaf has already been checked and contains no branch or call.
	// Save whole instructions until the entry jump fits; no prefix-only decoder
	// restriction is needed for the supported scalar SSE operations.
	patch := append(jump(base), make([]byte, overwrite-JumpSize)...)
	for i := JumpSize; i < len(patch); i++ {
		patch[i] = 0x90
	}
	body = append(body[:len(body)-1], jump(base+uint64(exitRVA))...)
	image := make([]byte, imageSize)
	copy(image, in.Image)
	copy(image[bodyRVA:], body)
	copy(image[exitRVA:], out.Image)
	return LeafPlan{Original: append([]byte(nil), code[:overwrite]...), Source: append([]byte(nil), code...), Patch: patch, Image: image, BodyRVA: bodyRVA, ExitRVA: exitRVA, Functions: [2]RuntimeFunction{{0, in.CodeSize, in.UnwindRVA}, {exitRVA, exitRVA + out.CodeSize, exitRVA + out.UnwindRVA}}}, nil
}

var leafOperations = map[string]bool{
	"NOP": true, "MOV": true, "MOVZX": true, "MOVSX": true, "MOVSXD": true, "LEA": true,
	"ADD": true, "SUB": true, "AND": true, "OR": true, "XOR": true, "TEST": true, "CMP": true,
	"MOVAPS": true, "MOVUPS": true, "MOVAPD": true, "MOVUPD": true, "MOVDQA": true, "MOVDQU": true,
	// x86asm distinguishes scalar SSE MOVSD_XMM from string MOVSD. The
	// latter updates implicit nonvolatile registers and is not a native leaf.
	"MOVQ": true, "MOVD": true, "MOVSD_XMM": true, "MOVSS": true, "PXOR": true, "XORPS": true, "XORPD": true,
	"ADDSD": true, "ADDSS": true, "SUBSD": true, "SUBSS": true, "MULSD": true, "MULSS": true,
	"DIVSD": true, "DIVSS": true,
}

func volatileLeafRegister(reg x86asm.Reg) bool {
	switch reg {
	case x86asm.AL, x86asm.CL, x86asm.DL, x86asm.AH, x86asm.CH, x86asm.DH,
		x86asm.AX, x86asm.CX, x86asm.DX, x86asm.EAX, x86asm.ECX, x86asm.EDX,
		x86asm.RAX, x86asm.RCX, x86asm.RDX:
		return true
	}
	return reg >= x86asm.R8B && reg <= x86asm.R11B || reg >= x86asm.R8W && reg <= x86asm.R11W ||
		reg >= x86asm.R8L && reg <= x86asm.R11L || reg >= x86asm.R8 && reg <= x86asm.R11 || reg >= x86asm.X0 && reg <= x86asm.X5
}
