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
	if err != nil || plan.OriginalHandlerRVA != 0x1080 || plan.RelocatedHandler || len(plan.Functions) != 3 {
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
	if _, err := BuildPEFunction(image, peLeafRVA, uint32(len(code)), 0x100000, 0x180000, 0x300000, 0x400000, 0x500000, 7); !errors.Is(err, ErrUnsupported) {
		t.Fatal("unverified combination of local saves and cleanup accepted", err)
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
		if err != nil || !plan.RelocatedHandler || plan.HandlerScopeStyle != 1 {
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
