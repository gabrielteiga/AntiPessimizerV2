package x64

import (
	"encoding/binary"
	"fmt"

	"golang.org/x/arch/x86/x86asm"
)

const MaxFunctionBytes = 8192
const MaxFunctionImageBytes = 32768

type FunctionPlan struct {
	TailTargets                    []uint32 // exact selected source RVAs; native adapter must close shared physical frames
	Original, Source, Patch, Image []byte
	BodyRVA, ExitRVA               uint32
	Functions                      [3]RuntimeFunction
	OriginalHandlerRVA             uint32
	RelocatedHandler               bool
	HandlerScopeStyle              uint32
}

// BuildPEFunction preserves a verified version-1 native prologue and its unwind
// codes while relocating the full procedure. An original UHANDLER-only cleanup
// is forwarded with its source metadata and PCs. Recognized CRT and reviewed
// Delphi scopes are translated for catch/resume. Adjacent v1 fragments can
// share the primary's copied unwind record when they only inherit its frame.
// A native cleanup handler records exceptional exits;
// it must have the Windows EXCEPTION_DISPOSITION handler ABI, not a Go callback.
func BuildPEFunction(image []byte, rva, size uint32, loadedBase, base, enter, exit, unwind uint64, id uint32) (FunctionPlan, error) {
	p, err := parsePEImage(image)
	if err != nil {
		return FunctionPlan{}, err
	}
	return p.BuildFunction(rva, size, loadedBase, base, enter, exit, unwind, id)
}

// BuildFunction plans against this prepared image. Every call still validates
// the selected code/prologue, native unwind actions and actual relocation range.
func (p *PEImage) BuildFunction(rva, size uint32, loadedBase, base, enter, exit, unwind uint64, id uint32) (FunctionPlan, error) {
	return p.buildSelectedFunction(rva, size, loadedBase, base, enter, exit, unwind, id, nil)
}

// BuildSelectedFunction permits verified direct and indirect tail jumps to exact
// entries in this selection. The owner must install every selected target and
// use an adapter with shared-physical-frame completion semantics.
func (p *PEImage) BuildSelectedFunction(rva, size uint32, loadedBase, base, enter, exit, unwind uint64, id uint32, selected []uint32) (FunctionPlan, error) {
	targets := make(map[uint64]bool, len(selected))
	for _, target := range selected {
		if p == nil {
			return FunctionPlan{}, fmt.Errorf("%w: uninitialized selected image", ErrUnsupported)
		}
		if _, ok := p.mapped(target, 1, true); !ok {
			return FunctionPlan{}, fmt.Errorf("%w: selected tail target is outside executable image", ErrUnsupported)
		}
		targets[loadedBase+uint64(target)] = true
	}
	return p.buildSelectedFunction(rva, size, loadedBase, base, enter, exit, unwind, id, targets)
}
func (p *PEImage) buildSelectedFunction(rva, size uint32, loadedBase, base, enter, exit, unwind uint64, id uint32, selected map[uint64]bool) (FunctionPlan, error) {
	fail := func(reason string) (FunctionPlan, error) {
		return FunctionPlan{}, fmt.Errorf("%w: function: %s", ErrUnsupported, reason)
	}
	var metadata functionUnwind
	code, imageSize, err := p.readCode(rva, size, &metadata)
	if err != nil {
		return FunctionPlan{}, err
	}
	if loadedBase == 0 || loadedBase >= maxUserAddress || uint64(imageSize) >= maxUserAddress-loadedBase || base == 0 || base > maxUserAddress-MaxFunctionImageBytes || id == 0 {
		return fail("invalid addresses/ID")
	}
	source := loadedBase + uint64(rva)
	originalInfo := metadata.info
	if len(originalInfo) == 0 {
		originalInfo = []byte{1, 0, 0, 0}
	}
	if err := validatePrologueActions(code, originalInfo); err != nil {
		return FunctionPlan{}, err
	}
	for start, i := 0, 0; i < len(metadata.chainBoundaries); i++ {
		end := metadata.chainBoundaries[i]
		for start < end {
			inst, err := x86asm.Decode(code[start:end], 64)
			if err != nil || inst.Op == 0 || inst.Len == 0 || inst.Len > end-start {
				return fail("unwind chain boundary splits an instruction")
			}
			start += inst.Len
		}
	}
	framed := originalInfo[2] != 0
	entry := entryJump(source, base)
	if metadata.handlerRVA != 0 {
		selected = nil
	}
	var restored map[int]bool
	if len(selected) > 0 && (framed || originalInfo[1] != 0) {
		restored = verifiedTailEpilogues(code, originalInfo)
	}
	// validatePrologueActions already matched every push to its unwind action.
	savedRBX := originalInfo[1] != 0 && code[0] == 0x53
	flow, err := rewriteSelectedFunction(code, source, base, framed, int(originalInfo[1]), len(entry), true, savedRBX, selected, restored)
	if err != nil {
		return FunctionPlan{}, err
	}
	if len(metadata.chainBoundaries) != 0 && flow.overwrite > metadata.chainBoundaries[0] {
		return fail("entry patch crosses an unwind chain boundary")
	}
	in, err := BuildEntryProbe(base, enter, base+512, id)
	if err != nil {
		return FunctionPlan{}, err
	}
	out, err := BuildExitProbe(base+uint64(flow.exitRVA), exit, id)
	if err != nil {
		return FunctionPlan{}, err
	}
	thunkRVA := (flow.exitRVA + uint32(len(out.Image)) + 15) &^ 15
	infoRVA := (thunkRVA + JumpSize + 3) &^ 3
	dataSize := 8 // procedure ID and descriptor kind
	if metadata.handlerRVA != 0 {
		dataSize = 36 + len(flow.pcMap)*4
	}
	var scopes, typeBlocks []byte
	if len(metadata.scopes) != 0 {
		// The aliased dispatcher keeps the source ImageBase so external filters
		// and cleanup callbacks retain their original RVAs. Clone code, scopes,
		// and an independent shadow unwind record must all fit in that RVA space.
		if base < loadedBase || base-loadedBase > uint64(^uint32(0))-MaxFunctionImageBytes {
			return fail("exception forwarding requires image-relative clone addresses")
		}
		scopes, err = relocateScopes(metadata.scopes, code, flow, rva, base-loadedBase+512, metadata.scopeStyle)
		if err != nil {
			return FunctionPlan{}, err
		}
		if metadata.scopeStyle == 3 {
			blockRVA := base - loadedBase + uint64(infoRVA) + uint64(len(originalInfo)) + 4 + scopeDescriptorSize + uint64(len(flow.pcMap)*4) + uint64(len(scopes))
			var addresses map[uint32]uint32
			typeBlocks, addresses, err = relocateTypeTables(metadata.typeTables, code, flow, rva, base-loadedBase+512, blockRVA)
			if err != nil {
				return FunctionPlan{}, err
			}
			for at := 4; at < len(scopes); at += 16 {
				if target, ok := addresses[binary.LittleEndian.Uint32(scopes[at+8:])]; ok {
					binary.LittleEndian.PutUint32(scopes[at+8:], target)
				}
			}
		}
		dataSize = scopeDescriptorSize + len(flow.pcMap)*4 + len(scopes) + 12 + len(originalInfo) + 4 + 16 + len(typeBlocks)
	}
	total := int(infoRVA) + len(originalInfo) + 4 + dataSize
	if total > MaxFunctionImageBytes {
		return fail("function image too large")
	}
	if base < loadedBase+uint64(imageSize) && base+uint64(total) > loadedBase {
		return fail("allocation overlaps loaded image")
	}
	for _, helper := range []uint64{enter, exit, unwind} {
		if helper == 0 || helper >= maxUserAddress || helper >= base && helper < base+uint64(total) || helper >= source && helper < source+uint64(size) {
			return fail("invalid or overlapping helper")
		}
	}
	for _, target := range flow.dataTargets {
		if target >= base && target < base+uint64(total) {
			return fail("RIP data overlaps allocation")
		}
	}
	data := make([]byte, total)
	copy(data, in.Image)
	copy(data[512:], flow.code)
	copy(data[flow.exitRVA:], out.Image)
	copy(data[thunkRVA:], jump(unwind))
	info := append([]byte(nil), originalInfo...)
	info[0] = 17 // v1 | UNW_FLAG_UHANDLER
	if len(scopes) != 0 {
		info[0] = 25
	} // search plus profiler unwind
	copy(data[infoRVA:], info)
	binary.LittleEndian.PutUint32(data[int(infoRVA)+len(info):], thunkRVA)
	binary.LittleEndian.PutUint32(data[int(infoRVA)+len(info)+4:], id)
	if metadata.handlerRVA != 0 {
		descriptor := data[int(infoRVA)+len(info)+4:]
		values := []uint32{id, 1, rva, size, metadata.runtimeRVA, metadata.infoRVA, metadata.handlerRVA, metadata.handlerDataRVA, uint32(len(flow.pcMap))}
		for i, value := range values {
			binary.LittleEndian.PutUint32(descriptor[i*4:], value)
		}
		mapAt := 36
		if len(scopes) != 0 {
			binary.LittleEndian.PutUint32(descriptor[4:], 2)
			mapAt = scopeDescriptorSize
			scopeAt := mapAt + len(flow.pcMap)*4
			shadowAt := scopeAt + len(scopes) + len(typeBlocks)
			aliasAt := shadowAt + 12
			binary.LittleEndian.PutUint32(descriptor[36:], metadata.scopeStyle)
			binary.LittleEndian.PutUint32(descriptor[40:], uint32(len(scopes)))
			binary.LittleEndian.PutUint32(descriptor[44:], uint32(metadata.handlerFlags))
			binary.LittleEndian.PutUint32(descriptor[48:], uint32(shadowAt))
			binary.LittleEndian.PutUint32(descriptor[52:], uint32(scopeAt))
			copy(descriptor[scopeAt:], scopes)
			copy(descriptor[scopeAt+len(scopes):], typeBlocks)
			difference := uint32(base - loadedBase)
			descriptorRVA := infoRVA + uint32(len(info)) + 4
			binary.LittleEndian.PutUint32(descriptor[shadowAt:], difference+512)
			binary.LittleEndian.PutUint32(descriptor[shadowAt+4:], difference+512+uint32(len(flow.code)))
			binary.LittleEndian.PutUint32(descriptor[shadowAt+8:], difference+descriptorRVA+uint32(aliasAt))
			copy(descriptor[aliasAt:], info)
			binary.LittleEndian.PutUint32(descriptor[aliasAt+len(info):], difference+thunkRVA)
			alias := descriptor[aliasAt+len(info)+4:]
			binary.LittleEndian.PutUint32(alias, id)
			binary.LittleEndian.PutUint32(alias[4:], 3) // canonical descriptor alias
			binary.LittleEndian.PutUint64(alias[8:], base+uint64(descriptorRVA))
		}
		for i, mapping := range flow.pcMap {
			binary.LittleEndian.PutUint16(descriptor[mapAt+i*4:], mapping.original)
			binary.LittleEndian.PutUint16(descriptor[mapAt+2+i*4:], mapping.relocated)
		}
	}
	patch := append(entry, make([]byte, flow.overwrite-len(entry))...)
	for i := len(entry); i < len(patch); i++ {
		patch[i] = 0x90
	}
	tails := make([]uint32, 0, len(flow.tailTargets))
	for _, target := range flow.tailTargets {
		tails = append(tails, uint32(target-loadedBase))
	}
	return FunctionPlan{TailTargets: tails, Original: append([]byte(nil), code[:flow.overwrite]...), Source: append([]byte(nil), code...), Patch: patch, Image: data, BodyRVA: 512, ExitRVA: flow.exitRVA, OriginalHandlerRVA: metadata.handlerRVA, RelocatedHandler: len(scopes) != 0, HandlerScopeStyle: metadata.scopeStyle, Functions: [3]RuntimeFunction{{0, in.CodeSize, in.UnwindRVA}, {512, 512 + uint32(len(flow.code)), infoRVA}, {flow.exitRVA, flow.exitRVA + out.CodeSize, flow.exitRVA + out.UnwindRVA}}}, nil
}

func validateUnwindCodes(info []byte) error {
	fail := func() error { return fmt.Errorf("%w: invalid or unsupported native unwind codes", ErrUnsupported) }
	if len(info) < 4 || info[0] != 1 || len(info) != 4+((int(info[2])+1)&^1)*2 {
		return fail()
	}
	nonvolatile := func(r byte) bool { return r == 3 || r == 5 || r == 6 || r == 7 || r >= 12 && r <= 15 }
	frame := info[3] & 15
	if frame != 0 && !nonvolatile(frame) || frame == 0 && info[3]>>4 != 0 {
		return fail()
	}
	count, previous, sets := int(info[2]), byte(255), 0
	for at := 0; at < count; {
		offset, op := info[4+at*2], info[5+at*2]
		if offset == 0 || offset > info[1] || offset > previous {
			return fail()
		}
		previous = offset
		extra := 0
		switch op & 15 {
		case 0:
			if !nonvolatile(op >> 4) {
				return fail()
			}
		case 1:
			if op>>4 == 0 {
				extra = 1
			} else if op>>4 == 1 {
				extra = 2
			} else {
				return fail()
			}
		case 2:
		case 3:
			if op>>4 != 0 || frame == 0 {
				return fail()
			}
			sets++
		case 4:
			if !nonvolatile(op >> 4) {
				return fail()
			}
			extra = 1
		case 5:
			if !nonvolatile(op >> 4) {
				return fail()
			}
			extra = 2
		case 8:
			if op>>4 < 6 {
				return fail()
			}
			extra = 1
		case 9:
			if op>>4 < 6 {
				return fail()
			}
			extra = 2
		default:
			return fail()
		}
		if at+extra >= count {
			return fail()
		}
		if op&15 == 1 {
			amount := uint64(binary.LittleEndian.Uint16(info[6+at*2:])) * 8
			if extra == 2 {
				amount = uint64(binary.LittleEndian.Uint32(info[6+at*2:]))
			}
			if amount == 0 || amount%8 != 0 {
				return fail()
			}
		}
		at += 1 + extra
	}
	if frame != 0 && sets != 1 || frame == 0 && sets != 0 {
		return fail()
	}
	return nil
}

// Tie each unwind action to the instruction that actually completes at its
// declared offset. This prevents a syntactically valid but wrong stack size or
// saved-register description from being copied into executable metadata.
func validatePrologueActions(code, info []byte) error {
	fail := func() error { return fmt.Errorf("%w: unwind actions do not match prologue", ErrUnsupported) }
	prologue := int(info[1])
	if prologue > len(code) {
		return fail()
	}
	ends := map[byte]x86asm.Inst{}
	depths := map[byte]int64{}
	stackActions := map[byte]bool{}
	var depth int64
	for at := 0; at < prologue; {
		inst, err := x86asm.Decode(code[at:], 64)
		if err != nil || inst.Len == 0 || at+inst.Len > prologue {
			return fail()
		}
		at += inst.Len
		ends[byte(at)] = inst
		switch {
		case inst.Op == x86asm.PUSH:
			depth += 8
			stackActions[byte(at)] = true
		case (inst.Op == x86asm.SUB || inst.Op == x86asm.ADD) && inst.Args[0] == x86asm.RSP:
			amount, ok := inst.Args[1].(x86asm.Imm)
			if inst.Op == x86asm.ADD {
				amount = -amount
			}
			if !ok || amount <= 0 || int64(amount) > 1<<32 {
				return fail()
			}
			depth += int64(amount)
			stackActions[byte(at)] = true
		case inst.Op == x86asm.POP || inst.Op == x86asm.LEAVE || inst.Args[0] == x86asm.RSP:
			// Unsupported stack updates cannot silently lack unwind actions.
			return fail()
		}
		depths[byte(at)] = depth
	}
	seen := map[byte]bool{}
	frameSetAt := byte(0)
	for at := 0; at < int(info[2]); {
		op := info[5+at*2]
		if op&15 == 3 {
			frameSetAt = info[4+at*2]
		}
		extra := 0
		switch op & 15 {
		case 1:
			extra = 1 + int(op>>4)
		case 4, 8:
			extra = 1
		case 5, 9:
			extra = 2
		}
		at += 1 + extra
	}
	for at := 0; at < int(info[2]); {
		offset, op := info[4+at*2], info[5+at*2]
		inst, ok := ends[offset]
		if !ok || seen[offset] {
			return fail()
		}
		seen[offset] = true
		extra := 0
		reg := x86asm.RAX + x86asm.Reg(op>>4)
		switch op & 15 {
		case 0:
			if inst.Op != x86asm.PUSH || inst.Args[0] != reg {
				return fail()
			}
		case 1, 2:
			amount := uint64(op>>4)*8 + 8
			if op&15 == 1 {
				extra = 1
				amount = uint64(binary.LittleEndian.Uint16(info[6+at*2:])) * 8
				if op>>4 == 1 {
					extra = 2
					amount = uint64(binary.LittleEndian.Uint32(info[6+at*2:]))
				}
			}
			imm, ok := inst.Args[1].(x86asm.Imm)
			if inst.Op == x86asm.ADD {
				imm = -imm
			}
			if (inst.Op != x86asm.SUB && inst.Op != x86asm.ADD) || inst.Args[0] != x86asm.RSP || !ok || imm <= 0 || uint64(imm) != amount {
				return fail()
			}
		case 3:
			frame := x86asm.RAX + x86asm.Reg(info[3]&15)
			if inst.Args[0] != frame {
				return fail()
			}
			if inst.Op == x86asm.MOV {
				if inst.Args[1] != x86asm.RSP || info[3]>>4 != 0 {
					return fail()
				}
			} else {
				mem, ok := inst.Args[1].(x86asm.Mem)
				if inst.Op != x86asm.LEA || !ok || mem.Base != x86asm.RSP || mem.Index != 0 || mem.Disp != int64(info[3]>>4)*16 {
					return fail()
				}
			}
		case 4, 5:
			extra = 1
			if op&15 == 5 {
				extra = 2
			}
			mem, ok := inst.Args[0].(x86asm.Mem)
			if inst.Op != x86asm.MOV || !ok || inst.Args[1] != reg {
				return fail()
			}
			amount := int64(binary.LittleEndian.Uint16(info[6+at*2:])) * 8
			if extra == 2 {
				amount = int64(binary.LittleEndian.Uint32(info[6+at*2:]))
			}
			if !savedAtOffset(mem, amount, depth-depths[offset], info[3], frameSetAt != 0 && offset > frameSetAt) {
				return fail()
			}
		case 8, 9:
			extra = 1
			if op&15 == 9 {
				extra = 2
			}
			mem, ok := inst.Args[0].(x86asm.Mem)
			if !ok || inst.Args[1] != x86asm.X0+x86asm.Reg(op>>4) {
				return fail()
			}
			amount := int64(binary.LittleEndian.Uint16(info[6+at*2:])) * 16
			if extra == 2 {
				amount = int64(binary.LittleEndian.Uint32(info[6+at*2:]))
			}
			if !savedAtOffset(mem, amount, depth-depths[offset], info[3], frameSetAt != 0 && offset > frameSetAt) {
				return fail()
			}
			switch inst.Op {
			case x86asm.MOVAPS, x86asm.MOVUPS, x86asm.MOVAPD, x86asm.MOVUPD, x86asm.MOVDQA, x86asm.MOVDQU:
			default:
				return fail()
			}
		default:
			return fail()
		}
		at += 1 + extra
	}
	for offset := range stackActions {
		if !seen[offset] {
			return fail()
		}
	}
	return nil
}

func savedAtOffset(mem x86asm.Mem, want, remainingStack int64, frame byte, frameEstablished bool) bool {
	if mem.Index != 0 || mem.Segment != 0 {
		return false
	}
	if mem.Base == x86asm.RSP {
		return mem.Disp+remainingStack == want
	}
	return frameEstablished && mem.Base == x86asm.RAX+x86asm.Reg(frame&15) && mem.Disp+int64(frame>>4)*16 == want
}
