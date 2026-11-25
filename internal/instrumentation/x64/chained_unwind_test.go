package x64

import (
	"bytes"
	"encoding/binary"
	"errors"
	"strings"
	"testing"
)

func chainedPE() ([]byte, []byte) {
	image, code := framedPE()
	put := binary.LittleEndian.PutUint32
	copy(image[0x424:0x430], image[0x40c:0x418])
	put(image[peOptional+140:], 48)
	for i, fragment := range [][3]uint32{
		{peLeafRVA, peLeafRVA + 16, 0x2080},
		{peLeafRVA + 16, peLeafRVA + 24, 0x20a0},
		{peLeafRVA + 24, peLeafRVA + uint32(len(code)), 0x20b0},
	} {
		for j, word := range fragment {
			put(image[0x400+i*12+j*4:], word)
		}
	}
	image[0x4a0], image[0x4b0] = 33, 33
	copy(image[0x4a4:], image[0x400:0x40c])
	copy(image[0x4b4:], image[0x40c:0x418])
	return image, code
}

func TestInheritedUnwindChainUsesOneCopiedFrame(t *testing.T) {
	image, code := chainedPE()
	before := bytes.Clone(image)
	for _, direct := range []bool{false, true} {
		if direct {
			copy(image[0x4b4:], image[0x400:0x40c])
			before = bytes.Clone(image)
		}
		p, err := BuildPEFunction(image, peLeafRVA, uint32(len(code)), 0x100000, 0x180000, 0x300000, 0x400000, 0x500000, 7)
		if err != nil {
			t.Fatal(err)
		}
		f := p.Functions[1]
		if !bytes.Equal(p.Source, code) || !bytes.Equal(p.Image[f.Unwind:f.Unwind+8], []byte{17, 5, 2, 0, 5, 0x32, 1, 0x30}) || f.End-f.Begin <= uint32(len(code)) {
			t.Fatal("copied runtime function lost part of the frame", f)
		}
		if !bytes.Equal(image, before) {
			t.Fatal("planner changed the original PE")
		}
	}
}

func TestInheritedUnwindChainRejectsUnverifiedFragments(t *testing.T) {
	put := binary.LittleEndian.PutUint32
	cases := map[string]func([]byte){
		"secondary has a handler":     func(b []byte) { b[0x4a0] |= 8 },
		"secondary changes the frame": func(b []byte) { b[0x4a3] = 5 },
		"secondary has unwind codes":  func(b []byte) { b[0x4a2] = 1 },
		"secondary uses version two":  func(b []byte) { b[0x4a0] = 34 },
		"primary has a handler":       func(b []byte) { b[0x480] = 17 },
		"secondary links itself":      func(b []byte) { copy(b[0x4a4:], b[0x40c:0x418]) },
		"secondary is truncated":      func(b []byte) { put(b[0x414:], 0x21fc); b[0x5fc] = 33 },
		"secondary points forward":    func(b []byte) { copy(b[0x4a4:], b[0x418:0x424]) },
		"secondary points elsewhere":  func(b []byte) { copy(b[0x4a4:], b[0x424:0x430]) },
		"secondary link is partial":   func(b []byte) { put(b[0x4a8:], peLeafRVA+15) },
		"gap in runtime coverage":     func(b []byte) { put(b[0x40c:], peLeafRVA+17) },
		"instruction spans boundary": func(b []byte) {
			put(b[0x404:], peLeafRVA+17)
			put(b[0x40c:], peLeafRVA+17)
			copy(b[0x4a4:], b[0x400:0x40c])
			copy(b[0x4b4:], b[0x40c:0x418])
		},
	}
	for name, mutate := range cases {
		t.Run(name, func(t *testing.T) {
			image, code := chainedPE()
			mutate(image)
			p, err := BuildPEFunction(image, peLeafRVA, uint32(len(code)), 0x100000, 0x180000, 0x300000, 0x400000, 0x500000, 7)
			if !errors.Is(err, ErrUnsupported) || len(p.Image) != 0 {
				t.Fatal("unverified chain accepted", err)
			}
		})
	}
	image, code := chainedPE()
	for _, extent := range [][2]uint32{{peLeafRVA, uint32(len(code)) - 1}, {peLeafRVA + 16, 8}} {
		if _, err := BuildPEFunction(image, extent[0], extent[1], 0x100000, 0x180000, 0x300000, 0x400000, 0x500000, 7); !errors.Is(err, ErrUnsupported) {
			t.Fatal("partial or standalone inherited frame accepted", extent, err)
		}
	}
}

func TestChainedPaddingMustBeUnreachable(t *testing.T) {
	image, original := chainedPE()
	code := append(append(bytes.Clone(original[:16]), 0x0f, 0x1f, 0x00, 0xcc), original[16:]...)
	copy(code[14:16], []byte{0xeb, 4})
	put := binary.LittleEndian.PutUint32
	put(code[24:], 0x1080-(peLeafRVA+28))
	put(image[0x40c:], peLeafRVA+20)
	put(image[0x410:], peLeafRVA+28)
	put(image[0x418:], peLeafRVA+28)
	put(image[0x41c:], peLeafRVA+uint32(len(code)))
	copy(image[0x4b4:], image[0x40c:0x418])
	build := func(code []byte) (FunctionPlan, error) {
		copy(image[0x220:], code)
		return BuildPEFunction(image, peLeafRVA, uint32(len(code)), 0x100000, 0x180000, 0x300000, 0x400000, 0x500000, 7)
	}
	if p, err := build(code); err != nil || !bytes.Equal(p.Source, code) {
		t.Fatal("valid padded chain rejected", err)
	} else if gap := p.Image[p.BodyRVA+19 : p.BodyRVA+23]; !bytes.Equal(gap, []byte{0x90, 0x90, 0x90, 0x90}) {
		t.Fatal("clone did not retain verified gap length", gap)
	}
	for _, bad := range []struct {
		at     int
		b      byte
		reason string
	}{
		{14, 0x75, "falls through"},
		{15, 3, "targets padding"},
		{16, 0x50, "non-padding"},
		{19, 0x66, "splits an instruction"},
	} {
		changed := bytes.Clone(code)
		changed[bad.at] = bad.b
		if _, err := build(changed); err == nil || !strings.Contains(err.Error(), bad.reason) {
			t.Fatal("invalid chain gap", bad, err)
		}
	}
}

func localSavePE() ([]byte, []byte) {
	const split = 8
	code := []byte{
		0x48, 0x83, 0xec, 0x28, 0x90, 0x90, 0xeb, 0, // SUB RSP,40; NOPs; JMP secondary
		0x48, 0x89, 0x5c, 0x24, 24, // MOV [RSP+24],RBX
		0x48, 0x89, 0xcb, 0x48, 0x8b, 0x03, // MOV RBX,RCX; MOV RAX,[RBX]
		0x48, 0x8b, 0x5c, 0x24, 24, // MOV RBX,[RSP+24]
		0x48, 0x83, 0xc4, 0x28, 0xc3, // ADD RSP,40; RET
	}
	image := scalarImage(code)
	put := binary.LittleEndian.PutUint32
	put(image[0x404:], peLeafRVA+split)
	put(image[0x40c:], peLeafRVA+split)
	put(image[0x410:], peLeafRVA+uint32(len(code)))
	put(image[0x414:], 0x20a0)
	copy(image[0x480:], []byte{1, 4, 1, 0, 4, 0x42, 0, 0})
	copy(image[0x4a0:], []byte{33, 5, 2, 0, 5, 0x34, 3, 0})
	copy(image[0x4a8:], image[0x400:0x40c])
	return image, code
}

func TestChainedLocalSavePreservesSeparateUnwindRange(t *testing.T) {
	image, code := localSavePE()
	plan, err := BuildPEFunction(image, peLeafRVA, uint32(len(code)), 0x100000, 0x180000, 0x300000, 0x400000, 0x500000, 7)
	if err != nil || len(plan.Functions) != 4 {
		t.Fatal("missing separate native runtime range", plan.Functions, err)
	}
	primary, secondary := plan.Functions[1], plan.Functions[2]
	if primary.End != plan.BodyRVA+11 || secondary.Begin != primary.End || secondary.End >= plan.ExitRVA {
		t.Fatal("short jump relocation changed the fragment boundary", plan.Functions)
	}
	info := plan.Image[secondary.Unwind:]
	u32 := binary.LittleEndian.Uint32
	if !bytes.Equal(info[:8], []byte{33, 5, 2, 0, 5, 0x34, 3, 0}) || u32(info[8:]) != primary.Begin || u32(info[12:]) != primary.End || u32(info[16:]) != primary.Unwind {
		t.Fatal("local save lost its primary record", info)
	}
}

func TestChainedLocalSaveRejectsSkippedOrMismatchedSave(t *testing.T) {
	for _, change := range []struct {
		at int
		b  byte
	}{
		{0x227, 5},    // jumps over the save
		{0x22c, 16},   // save uses a different slot
		{0x237, 16},   // reload uses a different slot
		{0x4a5, 0x54}, // unwind names RBP instead of RBX
		{0x4a6, 5},    // offset is outside the fixed allocation
	} {
		image, code := localSavePE()
		image[change.at] = change.b
		if _, err := BuildPEFunction(image, peLeafRVA, uint32(len(code)), 0x100000, 0x180000, 0x300000, 0x400000, 0x500000, 7); !errors.Is(err, ErrUnsupported) {
			t.Fatal("invalid local save accepted", change, err)
		}
	}
}

func TestChainedLocalSavesRejectConflictingMetadata(t *testing.T) {
	const split = 8
	primary := []byte{1, 4, 1, 0, 4, 0x42, 0, 0}
	code := []byte{
		0x48, 0x83, 0xec, 0x28, 0x90, 0x90, 0xeb, 0,
		0x48, 0x89, 0x5c, 0x24, 24, // save RBX
		0x48, 0x89, 0x74, 0x24, 32, // save RSI
		0x48, 0x8b, 0x01, // read caller data
		0x48, 0x8b, 0x74, 0x24, 32, // restore RSI
		0x48, 0x8b, 0x5c, 0x24, 24, // restore RBX
		0x48, 0x83, 0xc4, 0x28, 0xc3,
	}
	valid := []byte{33, 10, 4, 0, 10, 0x64, 4, 0, 5, 0x34, 3, 0}
	if err := validateLocalSave(code, primary, split, valid); err != nil {
		t.Fatal(err)
	}
	for _, change := range []struct {
		offset int
		value  byte
	}{
		{5, 0x34}, // duplicate register
		{6, 3},    // overlapping slot
		{6, 5},    // outside primary frame
		{8, 4},    // splits an instruction
		{9, 0x35}, // wrong unwind operation
	} {
		metadata := bytes.Clone(valid)
		metadata[change.offset] = change.value
		if err := validateLocalSave(code, primary, split, metadata); !errors.Is(err, ErrUnsupported) {
			t.Fatal("mismatched unwind metadata accepted", change, err)
		}
	}
}

func TestChainedXMMAndGeneralSavesValidateDistinctSlots(t *testing.T) {
	primary := []byte{1, 4, 1, 0, 4, 0x62, 0, 0}
	code := []byte{
		0x48, 0x83, 0xec, 0x38, 0x90, 0x90, 0xeb, 0,
		0x48, 0x89, 0x5c, 0x24, 32,
		0x0f, 0x29, 0x74, 0x24, 16,
		0x48, 0x8b, 0x01,
		0x0f, 0x28, 0x74, 0x24, 16,
		0x48, 0x8b, 0x5c, 0x24, 32,
		0x48, 0x83, 0xc4, 0x38, 0xc3,
	}
	valid := []byte{33, 10, 4, 0, 10, 0x68, 1, 0, 5, 0x34, 4, 0}
	if err := validateLocalSave(code, primary, 8, valid); err != nil {
		t.Fatal(err)
	}
	for _, change := range []struct {
		at int
		b  byte
	}{
		{5, 0x58}, // XMM5 is volatile
		{6, 3},    // extends beyond allocation
		{10, 3},   // RBX overlaps high half of XMM6
	} {
		local := bytes.Clone(valid)
		local[change.at] = change.b
		if err := validateLocalSave(code, primary, 8, local); !errors.Is(err, ErrUnsupported) {
			t.Fatal("invalid mixed save accepted", change, err)
		}
	}
}

func TestChainedMOVUPSSavesMatchUnwind(t *testing.T) {
	primary := []byte{1, 4, 1, 0, 4, 0x62, 0, 0}
	code := []byte{
		0x48, 0x83, 0xec, 0x38, 0x90, 0x90, 0xeb, 0,
		0x48, 0x89, 0x5c, 0x24, 32,
		0x0f, 0x11, 0x74, 0x24, 16,
		0x48, 0x8b, 0x01,
		0x0f, 0x10, 0x74, 0x24, 16,
		0x48, 0x8b, 0x5c, 0x24, 32,
		0x48, 0x83, 0xc4, 0x38, 0xc3,
	}
	local := []byte{33, 10, 4, 0, 10, 0x68, 1, 0, 5, 0x34, 4, 0}
	if err := validateLocalSave(code, primary, 8, local); err != nil {
		t.Fatal(err)
	}
	for _, at := range []int{17, 25} {
		changed := bytes.Clone(code)
		changed[at] = 32
		if err := validateLocalSave(changed, primary, 8, local); !errors.Is(err, ErrUnsupported) {
			t.Fatal("mismatched XMM slot accepted", at, err)
		}
	}
}

func TestInheritedChainPreservesOriginalCleanupDescriptor(t *testing.T) {
	image, code := chainedPE()
	image[0x480] = 17
	binary.LittleEndian.PutUint32(image[0x488:], 0x1080)
	binary.LittleEndian.PutUint32(image[0x48c:], 0x31415926)
	plan, err := BuildPEFunction(image, peLeafRVA, uint32(len(code)), 0x100000, 0x180000, 0x300000, 0x400000, 0x500000, 7)
	if err != nil || plan.OriginalHandlerRVA != 0x1080 || !plan.InheritedHandler || plan.RelocatedHandler || len(plan.Functions) != 3 {
		t.Fatal("original cleanup chain rejected", err, plan.OriginalHandlerRVA)
	}
	info := plan.Image[plan.Functions[1].Unwind:]
	descriptor := info[12:]
	for i, want := range []uint32{7, 1, peLeafRVA, uint32(len(code)), 0x2000, 0x2080, 0x1080, 0x208c} {
		if got := binary.LittleEndian.Uint32(descriptor[i*4:]); got != want {
			t.Fatal("original cleanup descriptor changed", i, got, want)
		}
	}
	image, code = localSavePE()
	image[0x480] = 17
	binary.LittleEndian.PutUint32(image[0x488:], 0x1080)
	plan, err = BuildPEFunction(image, peLeafRVA, uint32(len(code)), 0x100000, 0x180000, 0x300000, 0x400000, 0x500000, 7)
	if err != nil || !plan.InheritedHandler || plan.RelocatedHandler || len(plan.Functions) != 4 {
		t.Fatal("local saves with original cleanup rejected", err)
	}
	info = plan.Image[plan.Functions[1].Unwind:]
	if binary.LittleEndian.Uint32(info[12+8*4:]) == 0 {
		t.Fatal("cleanup lost its full-body PC map")
	}
}

func TestInheritedChainRelocatesRecognizedExceptionScopes(t *testing.T) {
	for _, flags := range []byte{9, 25} {
		image, code := scopesPE()
		copy(image[0x418:0x424], image[0x40c:0x418])
		w32(image, peOptional+140, 36)
		w32(image, 0x404, peLeafRVA+16)
		w32(image, 0x40c, peLeafRVA+16)
		w32(image, 0x410, peLeafRVA+uint32(len(code)))
		w32(image, 0x414, 0x20c0)
		image[0x4c0] = 33
		copy(image[0x4c4:], image[0x400:0x40c])
		image[0x480] = flags
		w32(image, 0x490, peLeafRVA+16)
		w32(image, 0x494, peLeafRVA+24)
		w32(image, 0x49c, peLeafRVA+24)
		plan, err := BuildPEFunction(image, peLeafRVA, uint32(len(code)), 0x100000, 0x180000, 0x300000, 0x400000, 0x500000, 7)
		if err != nil || !plan.RelocatedHandler || !plan.InheritedHandler || plan.HandlerScopeStyle != 1 {
			t.Fatal("inherited exception scopes rejected", flags, err)
		}
		descriptor := plan.Image[plan.Functions[1].Unwind+12:]
		scopes := descriptor[binary.LittleEndian.Uint32(descriptor[52:]):]
		for i, want := range []uint32{1, 0x80210, 0x80218, 1, 0x80218} {
			if got := binary.LittleEndian.Uint32(scopes[i*4:]); got != want {
				t.Fatal("inherited scope lost relocated PC", i, got, want)
			}
		}
	}
}

func TestChainedLargeAllocationChecksHighSaveSlots(t *testing.T) {
	code := []byte{
		0x48, 0x81, 0xec, 0x28, 0x02, 0, 0, 0x90, 0xeb, 0,
		0x48, 0x89, 0x9c, 0x24, 0x20, 0x02, 0, 0,
		0x0f, 0x29, 0xb4, 0x24, 0x10, 0x02, 0, 0,
		0x48, 0x8b, 0x01,
		0x0f, 0x28, 0xb4, 0x24, 0x10, 0x02, 0, 0,
		0x48, 0x8b, 0x9c, 0x24, 0x20, 0x02, 0, 0,
		0x48, 0x81, 0xc4, 0x28, 0x02, 0, 0, 0xc3,
	}
	local := []byte{33, 16, 4, 0, 16, 0x68, 33, 0, 8, 0x34, 68, 0}
	for _, primary := range [][]byte{
		{1, 7, 2, 0, 7, 1, 69, 0},
		{1, 7, 3, 0, 7, 0x11, 0x28, 2, 0, 0, 0, 0},
	} {
		if err := validateLocalSave(code, primary, 10, local); err != nil {
			t.Fatal(err)
		}
		image := scalarImage(code)
		put := binary.LittleEndian.PutUint32
		put(image[0x404:], peLeafRVA+10)
		put(image[0x40c:], peLeafRVA+10)
		put(image[0x410:], peLeafRVA+uint32(len(code)))
		put(image[0x414:], 0x20a0)
		copy(image[0x480:], primary)
		copy(image[0x4a0:], local)
		copy(image[0x4ac:], image[0x400:0x40c])
		plan, err := BuildPEFunction(image, peLeafRVA, uint32(len(code)), 0x100000, 0x180000, 0x300000, 0x400000, 0x500000, 7)
		if err != nil || len(plan.Functions) != 4 {
			t.Fatal("large allocation chain rejected", err)
		}
		for _, slot := range []byte{66, 67, 69} {
			bad := bytes.Clone(local)
			bad[10] = slot
			changedCode := bytes.Clone(code)
			binary.LittleEndian.PutUint32(changedCode[14:], uint32(slot)*8)
			binary.LittleEndian.PutUint32(changedCode[41:], uint32(slot)*8)
			if err := validateLocalSave(changedCode, primary, 10, bad); !errors.Is(err, ErrUnsupported) {
				t.Fatal("overlap or out-of-frame save accepted", slot, err)
			}
		}
		wrongAllocation := bytes.Clone(primary)
		wrongAllocation[6]++
		if err := validateLocalSave(code, wrongAllocation, 10, local); !errors.Is(err, ErrUnsupported) {
			t.Fatal("wrong allocation accepted", err)
		}
	}
}

func TestChainedFarSaveAlignsEmbeddedRuntimeRecord(t *testing.T) {
	image, code := localSavePE()
	local := []byte{33, 5, 3, 0, 5, 0x35, 24, 0, 0, 0, 0, 0}
	copy(image[0x4a0:], local)
	copy(image[0x4ac:], image[0x400:0x40c])
	plan, err := BuildPEFunction(image, peLeafRVA, uint32(len(code)), 0x100000, 0x180000, 0x300000, 0x400000, 0x500000, 7)
	if err != nil || len(plan.Functions) != 4 {
		t.Fatal("far save rejected", err)
	}
	info := plan.Image[plan.Functions[2].Unwind:]
	if !bytes.Equal(info[:12], local) || binary.LittleEndian.Uint32(info[20:]) != plan.Functions[1].Unwind {
		t.Fatal("odd unwind slots lost aligned primary record", info[:24])
	}
	for _, change := range []struct {
		at int
		b  byte
	}{{0x4a6, 25}, {0x4a8, 1}, {0x4a5, 0x55}} {
		bad := bytes.Clone(image)
		bad[change.at] = change.b
		if _, err := BuildPEFunction(bad, peLeafRVA, uint32(len(code)), 0x100000, 0x180000, 0x300000, 0x400000, 0x500000, 7); !errors.Is(err, ErrUnsupported) {
			t.Fatal("invalid far save accepted", change, err)
		}
	}
}

func TestChainedFullWidthXMMMoveVariants(t *testing.T) {
	primary := []byte{1, 7, 2, 0, 7, 1, 69, 0}
	local := []byte{33, 17, 6, 0, 17, 0x69, 0x10, 2, 0, 0, 8, 0x35, 0x20, 2, 0, 0}
	for _, tc := range []struct {
		name                string
		prefix, store, load byte
	}{
		{"MOVAPD", 0x66, 0x29, 0x28}, {"MOVUPD", 0x66, 0x11, 0x10}, {"MOVDQA", 0x66, 0x7f, 0x6f}, {"MOVDQU", 0xf3, 0x7f, 0x6f},
	} {
		t.Run(tc.name, func(t *testing.T) {
			code := []byte{0x48, 0x81, 0xec, 0x28, 2, 0, 0, 0x90, 0xeb, 0, 0x48, 0x89, 0x9c, 0x24, 0x20, 2, 0, 0}
			code = append(code, tc.prefix, 0x0f, tc.store, 0xb4, 0x24, 0x10, 2, 0, 0)
			code = append(code, 0x48, 0x8b, 0x01)
			code = append(code, tc.prefix, 0x0f, tc.load, 0xb4, 0x24, 0x10, 2, 0, 0)
			code = append(code, 0x48, 0x8b, 0x9c, 0x24, 0x20, 2, 0, 0, 0x48, 0x81, 0xc4, 0x28, 2, 0, 0, 0xc3)
			if err := validateLocalSave(code, primary, 10, local); err != nil {
				t.Fatal(err)
			}
			partial := bytes.Clone(code)
			copy(partial[18:21], []byte{0xf2, 0x0f, 0x11})
			copy(partial[30:33], []byte{0xf2, 0x0f, 0x10})
			if err := validateLocalSave(partial, primary, 10, local); !errors.Is(err, ErrUnsupported) {
				t.Fatal("partial-width XMM save accepted", err)
			}
		})
	}
}

func frameLocalSavePE(release []byte) ([]byte, []byte) {
	code := []byte{0x55, 0x48, 0x83, 0xec, 0x30, 0x48, 0x8d, 0x6c, 0x24, 32}
	code = append(code, bytes.Repeat([]byte{0x90}, 8)...)
	code = append(code, 0x48, 0x89, 0x5c, 0x24, 40, 0x0f, 0x29, 0x74, 0x24, 16, 0x48, 0x8b, 0x01,
		0x0f, 0x28, 0x74, 0x24, 16, 0x48, 0x8b, 0x5c, 0x24, 40)
	code = append(code, release...)
	code = append(code, 0x5d, 0xc3)
	image := scalarImage(code)
	put := binary.LittleEndian.PutUint32
	put(image[0x404:], peLeafRVA+18)
	put(image[0x40c:], peLeafRVA+18)
	put(image[0x410:], peLeafRVA+uint32(len(code)))
	put(image[0x414:], 0x20a0)
	copy(image[0x480:], []byte{1, 10, 3, 0x25, 10, 3, 5, 0x52, 1, 0x50, 0, 0})
	copy(image[0x4a0:], []byte{33, 10, 4, 0x25, 10, 0x68, 1, 0, 5, 0x34, 5, 0})
	copy(image[0x4ac:], image[0x400:0x40c])
	return image, code
}

func TestChainedLocalSavesInheritBiasedFrameRegister(t *testing.T) {
	for _, release := range [][]byte{{0x48, 0x83, 0xc4, 48}, {0x48, 0x8d, 0x65, 16}} {
		image, code := frameLocalSavePE(release)
		plan, err := BuildPEFunction(image, peLeafRVA, uint32(len(code)), 0x100000, 0x180000, 0x300000, 0x400000, 0x500000, 7)
		if err != nil || len(plan.Functions) != 4 {
			t.Fatal("biased frame local saves rejected", err)
		}
		if plan.Image[plan.Functions[1].Unwind+3] != 0x25 || plan.Image[plan.Functions[2].Unwind+3] != 0x25 {
			t.Fatal("copied chain changed inherited frame register")
		}
		for _, change := range []struct {
			at int
			b  byte
		}{{0x4a3, 0x15}, {0x4a3, 0}, {0x220 + len(code) - 2, 0x5b}} {
			bad := bytes.Clone(image)

			bad[change.at] = change.b
			if _, err := BuildPEFunction(bad, peLeafRVA, uint32(len(code)), 0x100000, 0x180000, 0x300000, 0x400000, 0x500000, 7); !errors.Is(err, ErrUnsupported) {
				t.Fatal("mismatched inherited frame accepted", change, err)
			}
		}
	}
}

func TestChainedLocalSavesRejectMisalignedAlignedXMMMove(t *testing.T) {
	image, code := frameLocalSavePE([]byte{0x48, 0x83, 0xc4, 48})
	primary := bytes.Clone(image[0x480:0x48c])
	primary[7] = 0x62
	code[4] = 56
	code[len(code)-3] = 56
	if err := validateLocalSave(code, primary, 18, image[0x4a0:0x4ac]); !errors.Is(err, ErrUnsupported) {
		t.Fatal("unaligned MOVAPS save accepted", err)
	}
}

func TestChainedLocalSavesUseHighFrameRegister(t *testing.T) {
	image, original := frameLocalSavePE([]byte{0x48, 0x8d, 0x65, 16})
	code := []byte{0x41, 0x54, 0x48, 0x83, 0xec, 48, 0x4c, 0x8d, 0x64, 0x24, 32}
	code = append(code, original[10:len(original)-6]...)
	code = append(code, 0x49, 0x8d, 0x64, 0x24, 16, 0x41, 0x5c, 0xc3)
	copy(image[0x220:], code)
	put := binary.LittleEndian.PutUint32
	put(image[0x404:], peLeafRVA+19)
	put(image[0x40c:], peLeafRVA+19)
	put(image[0x410:], peLeafRVA+uint32(len(code)))
	copy(image[0x480:], []byte{1, 11, 3, 0x2c, 11, 3, 6, 0x52, 2, 0xc0, 0, 0})
	image[0x4a3] = 0x2c
	copy(image[0x4ac:], image[0x400:0x40c])
	plan, err := BuildPEFunction(image, peLeafRVA, uint32(len(code)), 0x100000, 0x180000, 0x300000, 0x400000, 0x500000, 7)
	if err != nil || len(plan.Functions) != 4 {
		t.Fatal("R12 inherited frame rejected", err)
	}
	if plan.Image[plan.Functions[1].Unwind+3] != 0x2c || plan.Image[plan.Functions[2].Unwind+3] != 0x2c {
		t.Fatal("high frame-register identity changed")
	}
}

func primaryAndLocalSavePE() ([]byte, []byte) {
	code := []byte{0x48, 0x83, 0xec, 56, 0x48, 0x89, 0x5c, 0x24, 48, 0x48, 0x89, 0xcb}
	code = append(code, bytes.Repeat([]byte{0x90}, 8)...)
	code = append(code,
		0x48, 0x89, 0x74, 0x24, 40, 0x0f, 0x29, 0x74, 0x24, 16,
		0x48, 0x89, 0xde, 0x66, 0x0f, 0xef, 0xf6, 0x48, 0x8b, 0x06,
		0x0f, 0x28, 0x74, 0x24, 16, 0x48, 0x8b, 0x74, 0x24, 40,
		0x48, 0x8b, 0x5c, 0x24, 48, 0x48, 0x83, 0xc4, 56, 0xc3)
	image := scalarImage(code)
	put := binary.LittleEndian.PutUint32
	put(image[0x404:], peLeafRVA+20)
	put(image[0x40c:], peLeafRVA+20)
	put(image[0x410:], peLeafRVA+uint32(len(code)))
	put(image[0x414:], 0x20a0)
	copy(image[0x480:], []byte{1, 12, 3, 0, 9, 0x34, 6, 0, 4, 0x62, 0, 0})
	copy(image[0x4a0:], []byte{33, 10, 4, 0, 10, 0x68, 1, 0, 5, 0x64, 5, 0})
	copy(image[0x4ac:], image[0x400:0x40c])
	return image, code
}

func TestChainedLocalSavesPreservePrimaryMemorySave(t *testing.T) {
	image, code := primaryAndLocalSavePE()
	plan, err := BuildPEFunction(image, peLeafRVA, uint32(len(code)), 0x100000, 0x180000, 0x300000, 0x400000, 0x500000, 7)
	if err != nil || len(plan.Functions) != 4 {
		t.Fatal("primary memory save rejected", err)
	}
	if !bytes.Equal(plan.Image[plan.Functions[1].Unwind+1:plan.Functions[1].Unwind+12], image[0x481:0x48c]) {
		t.Fatal("primary unwind actions changed")
	}
	for name, change := range map[string]func([]byte){
		"overlapping primary and local slots": func(b []byte) { b[0x4aa] = 6; b[0x220+24] = 48; b[0x220+49] = 48 },
		"XMM slot outside the frame":          func(b []byte) { b[0x4a6] = 3 },
		"primary reload reads another slot":   func(b []byte) { b[0x220+len(code)-6] = 40 },
	} {
		t.Run(name, func(t *testing.T) {
			bad := bytes.Clone(image)
			change(bad)
			if _, err := BuildPEFunction(bad, peLeafRVA, uint32(len(code)), 0x100000, 0x180000, 0x300000, 0x400000, 0x500000, 7); !errors.Is(err, ErrUnsupported) {
				t.Fatal("invalid primary/local save combination accepted", err)
			}
		})
	}
}

func TestChainedMultiplePrimaryMemorySaves(t *testing.T) {
	primary := []byte{0x48, 0x83, 0xec, 88, 0x48, 0x89, 0x5c, 0x24, 80,
		0x0f, 0x29, 0x7c, 0x24, 48, 0x66, 0x0f, 0xef, 0xff, 0x48, 0x89, 0xcb}
	secondary := []byte{0x48, 0x89, 0x74, 0x24, 72, 0x0f, 0x29, 0x74, 0x24, 16,
		0x48, 0x89, 0xde, 0x66, 0x0f, 0xef, 0xf6, 0x48, 0x8b, 0x06,
		0x0f, 0x28, 0x74, 0x24, 16, 0x48, 0x8b, 0x74, 0x24, 72,
		0x0f, 0x28, 0x7c, 0x24, 48, 0x48, 0x8b, 0x5c, 0x24, 80, 0x48, 0x83, 0xc4, 88, 0xc3}
	info := []byte{1, 21, 5, 0, 14, 0x78, 3, 0, 9, 0x34, 10, 0, 4, 0xa2, 0, 0}
	local := []byte{33, 10, 4, 0, 10, 0x68, 1, 0, 5, 0x64, 9, 0}
	build := func(prefix, suffix, unwind, chain []byte) ([]byte, []byte) {
		code := append(bytes.Clone(prefix), bytes.Repeat([]byte{0x90}, 8)...)
		start := len(code)
		code = append(code, suffix...)
		image := scalarImage(code)
		put := binary.LittleEndian.PutUint32
		put(image[0x404:], peLeafRVA+uint32(start))
		put(image[0x40c:], peLeafRVA+uint32(start))
		put(image[0x410:], peLeafRVA+uint32(len(code)))
		put(image[0x414:], 0x20a0)
		copy(image[0x480:], unwind)
		copy(image[0x4a0:], chain)
		copy(image[0x4ac:], image[0x400:0x40c])
		return image, code
	}
	check := func(t *testing.T, prefix, suffix, unwind, chain []byte) {
		t.Helper()
		image, code := build(prefix, suffix, unwind, chain)
		plan, err := BuildPEFunction(image, peLeafRVA, uint32(len(code)), 0x100000, 0x180000, 0x300000, 0x400000, 0x500000, 7)
		if err != nil || len(plan.Functions) != 4 {
			t.Fatal("primary save combination rejected", err)
		}
		at := plan.Functions[1].Unwind
		if !bytes.Equal(plan.Image[at+1:at+uint32(len(unwind))], unwind[1:]) {
			t.Fatal("primary metadata changed")
		}
	}
	t.Run("small mixed saves", func(t *testing.T) { check(t, primary, secondary, info, local) })
	t.Run("large allocation and FAR saves", func(t *testing.T) {
		prefix := append([]byte{0x48, 0x81, 0xec, 0x98, 0, 0, 0}, primary[4:]...)
		suffix := append(bytes.Clone(secondary[:len(secondary)-5]), 0x48, 0x81, 0xc4, 0x98, 0, 0, 0, 0xc3)
		unwind := []byte{1, 24, 8, 0, 17, 0x79, 48, 0, 0, 0, 12, 0x35, 80, 0, 0, 0, 7, 1, 19, 0}
		check(t, prefix, suffix, unwind, local)
	})
	t.Run("frame pointer and pushes", func(t *testing.T) {
		prefix := []byte{0x55, 0x48, 0x83, 0xec, 80, 0x48, 0x8d, 0x6c, 0x24, 32}
		prefix = append(prefix, primary[4:]...)
		prefix[14] = 72
		suffix := bytes.Clone(secondary[:len(secondary)-5])
		suffix[4], suffix[29], suffix[39] = 64, 64, 72
		suffix = append(suffix, 0x48, 0x8d, 0x65, 48, 0x5d, 0xc3)
		unwind := []byte{1, 27, 7, 0x25, 20, 0x78, 3, 0, 15, 0x34, 9, 0, 10, 3, 5, 0x92, 1, 0x50, 0, 0}
		chain := bytes.Clone(local)
		chain[3], chain[10] = 0x25, 8
		check(t, prefix, suffix, unwind, chain)
	})
	for name, change := range map[string]func([]byte, []byte, []byte){
		"primary save overlaps local": func(p, s, u []byte) { p[13] = 16; s[34] = 16; u[6] = 1 },
		"primary XMM misalignment": func(p, s, u []byte) {
			p[3] = 80
			p[8] = 72
			s[4] = 64
			s[29] = 64
			s[39] = 72
			s[len(s)-2] = 80
			u[10] = 9
			u[13] = 0x92
		},
		"primary reload wrong register": func(p, s, u []byte) { s[32] = 0x74 },
		"primary reload wrong width":    func(p, s, u []byte) { s[31] = 0x12 },
		"primary reload wrong slot":     func(p, s, u []byte) { s[39] = 72 },
		"duplicate primary register":    func(p, s, u []byte) { p[11] = 0x74; u[5] = 0x68 },
	} {
		t.Run(name, func(t *testing.T) {
			p, s, u := bytes.Clone(primary), bytes.Clone(secondary), bytes.Clone(info)
			change(p, s, u)
			code := append(p, bytes.Repeat([]byte{0x90}, 8)...)
			start := len(code)
			code = append(code, s...)
			if err := validateLocalSave(code, u, start, local); !errors.Is(err, ErrUnsupported) {
				t.Fatal("invalid primary save accepted", err)
			}
		})
	}
}

func TestChainedLocalSaveDirectCallsReserveCalleeHomeSlots(t *testing.T) {
	prefix := []byte{0x48, 0x83, 0xec, 88, 0x48, 0x89, 0x5c, 0x24, 80, 0x0f, 0x29, 0x7c, 0x24, 48, 0x66, 0x0f, 0xef, 0xff, 0x48, 0x89, 0xcb}
	body := []byte{0x48, 0x89, 0x74, 0x24, 72, 0x0f, 0x29, 0x74, 0x24, 32, 0x48, 0x89, 0xde, 0x66, 0x0f, 0xef, 0xf6,
		0x48, 0x89, 0xf1, 0xe8, 0, 1, 0, 0, 0x90,
		0x0f, 0x28, 0x74, 0x24, 32, 0x48, 0x8b, 0x74, 0x24, 72,
		0x0f, 0x28, 0x7c, 0x24, 48, 0x48, 0x8b, 0x5c, 0x24, 80, 0x48, 0x83, 0xc4, 88, 0xc3}
	unwind := []byte{1, 21, 5, 0, 14, 0x78, 3, 0, 9, 0x34, 10, 0, 4, 0xa2, 0, 0}
	local := []byte{33, 10, 4, 0, 10, 0x68, 2, 0, 5, 0x64, 9, 0}
	check := func(p, b, u, l []byte) error {
		code := append(bytes.Clone(p), bytes.Repeat([]byte{0x90}, 8)...)
		start := len(code)
		code = append(code, b...)
		image := scalarImage(code)
		put := binary.LittleEndian.PutUint32
		put(image[0x404:], peLeafRVA+uint32(start))
		put(image[0x40c:], peLeafRVA+uint32(start))
		put(image[0x410:], peLeafRVA+uint32(len(code)))
		put(image[0x414:], 0x20a0)
		copy(image[0x480:], u)
		copy(image[0x4a0:], l)
		copy(image[0x4ac:], image[0x400:0x40c])
		_, err := BuildPEFunction(image, peLeafRVA, uint32(len(code)), 0x100000, 0x180000, 0x300000, 0x400000, 0x500000, 7)
		return err
	}
	if err := check(prefix, body, unwind, local); err != nil {
		t.Fatal("verified direct call rejected", err)
	}
	branched := append(bytes.Clone(body[:17]),
		0xb9, 3, 0, 0, 0, 0xff, 0xc9, 0x75, 0xfc, 0x48, 0x85, 0xf6, 0x74, 11,
		0x48, 0x89, 0xf1, 0xe8, 0, 1, 0, 0, 0x90, 0xeb, 3, 0x48, 0x8b, 6)
	branched = append(branched, body[26:]...)
	if err := check(prefix, branched, unwind, local); err != nil {
		t.Fatal("bounded branches rejected", err)
	}
	for name, displacement := range map[string]byte{
		"branch skips saves":          225, // target offset zero, from next PC 31
		"branch enters a reload":      15,  // target offset 46, inside the first reload
		"branch skips a reload":       19,  // target offset 50, second reload
		"branch leaves the procedure": 100,
	} {
		t.Run(name, func(t *testing.T) {
			bad := bytes.Clone(branched)
			bad[30] = displacement
			if err := check(prefix, bad, unwind, local); !errors.Is(err, ErrUnsupported) {
				t.Fatal("unsafe branch accepted", err)
			}
		})
	}
	for name, change := range map[string]func([]byte, []byte, []byte, []byte){
		"local save in callee home area":   func(p, b, u, l []byte) { b[9] = 16; b[30] = 16; l[6] = 1 },
		"primary save in callee home area": func(p, b, u, l []byte) { p[13] = 16; b[40] = 16; u[6] = 1 },
		"indirect call":                    func(p, b, u, l []byte) { copy(b[20:], []byte{0xff, 0xd0, 0x90, 0x90, 0x90}) },
		"misaligned call stack": func(p, b, u, l []byte) {
			p[3] = 96
			p[10] = 0x11
			b[6] = 0x11
			b[27] = 0x10
			b[37] = 0x10
			b[len(b)-2] = 96
			u[13] = 0xb2
		},
	} {
		t.Run(name, func(t *testing.T) {
			p, b, u, l := bytes.Clone(prefix), bytes.Clone(body), bytes.Clone(unwind), bytes.Clone(local)
			change(p, b, u, l)
			if err := check(p, b, u, l); !errors.Is(err, ErrUnsupported) {
				t.Fatal("unsafe chained call accepted", err)
			}
		})
	}
}
