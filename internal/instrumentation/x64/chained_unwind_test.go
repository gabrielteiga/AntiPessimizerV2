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
	code := append(append(bytes.Clone(original[:16]), 0x90, 0x90, 0x90, 0x90), original[16:]...)
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
	}
	for _, bad := range []struct {
		at     int
		b      byte
		reason string
	}{
		{14, 0x75, "falls through"},
		{15, 3, "targets padding"},
		{17, 0xcc, "not covered"},
	} {
		changed := bytes.Clone(code)
		changed[bad.at] = bad.b
		if _, err := build(changed); err == nil || !strings.Contains(err.Error(), bad.reason) {
			t.Fatal("invalid chain gap", bad, err)
		}
	}
}
