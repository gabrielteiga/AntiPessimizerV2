package x64

import (
	"bytes"
	"encoding/binary"
	"errors"
	"testing"
)

func leafCode(body ...byte) []byte {
	return append(append(bytes.Repeat([]byte{0x90}, 16), body...), 0xc3)
}

func TestLeafPlanRoutesOriginalReturnThroughExit(t *testing.T) {
	original := leafCode(0x48, 0x8d, 0x04, 0x11, // lea rax,[rcx+rdx]
		0x4c, 0x01, 0xc0, 0x4c, 0x01, 0xc8, // add rax,r8; add rax,r9
		0x48, 0x03, 0x44, 0x24, 0x28, 0x48, 0x03, 0x44, 0x24, 0x30) // two stack args
	before := append([]byte(nil), original...)
	p, err := BuildLeaf(original, 0x100000, 0x180000, 0x300000, 0x400000, 7)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(original, before) || len(p.Patch) < JumpSize || !bytes.Equal(p.Original, original[:len(p.Patch)]) {
		t.Fatal("source mutated or patch lacks whole saved instructions")
	}
	if got := binary.LittleEndian.Uint64(p.Patch[6:]); got != 0x180000 {
		t.Fatalf("entry goes to %#x", got)
	}
	body := p.Image[p.BodyRVA:]
	if !bytes.Equal(body[:len(original)-1], original[:len(original)-1]) {
		t.Fatal("leaf body changed")
	}
	if !bytes.Equal(body[len(original)-1:len(original)-1+JumpSize], jump(0x180000+uint64(p.ExitRVA))) {
		t.Fatal("RET does not go to exit")
	}
	if p.Image[p.Functions[1].End-1] != 0xc3 || p.Functions[1].Begin != p.ExitRVA || p.Functions[0].End > p.BodyRVA {
		t.Fatal("bad native return/unwind ranges", p.Functions)
	}
	original[0] = 0xcc
	if p.Original[0] != 0x90 || p.Image[p.BodyRVA] != 0x90 {
		t.Fatal("plan aliases input")
	}
}

func TestLeafPlanRelocatesRIPData(t *testing.T) {
	code := leafCode(0x48, 0x8b, 0x05, 0, 0, 0, 0)
	const source, base, target = 0x100000, 0x180000, 0x101000
	binary.LittleEndian.PutUint32(code[19:], target-(source+23))
	p, err := BuildLeaf(code, source, base, 0x300000, 0x400000, 1)
	if err != nil {
		t.Fatal(err)
	}
	disp := int64(int32(binary.LittleEndian.Uint32(p.Image[p.BodyRVA+19:])))
	if int64(base+p.BodyRVA+23)+disp != target {
		t.Fatal("cloned RIP operand changed target")
	}
}

func TestLeafPlanRejectsUnsupportedBehavior(t *testing.T) {
	cases := map[string][]byte{
		"call":                    leafCode(0xe8, 0, 0, 0, 0),
		"branch":                  leafCode(0x75, 0),
		"jump":                    leafCode(0xff, 0xe0),
		"push":                    leafCode(0x50),
		"pop":                     leafCode(0x58),
		"stack allocation":        leafCode(0x48, 0x83, 0xec, 0x28),
		"stack pointer copy":      leafCode(0x48, 0x89, 0xe0),
		"stack store":             leafCode(0x48, 0x89, 0x44, 0x24, 0x08),
		"return slot read":        leafCode(0x48, 0x8b, 0x04, 0x24),
		"nonvolatile integer":     leafCode(0x48, 0x89, 0xcb),
		"nonvolatile subregister": leafCode(0xb3, 1),
		"nonvolatile simd":        leafCode(0x66, 0x0f, 0xef, 0xf6),
		"string":                  leafCode(0xa5),
		"rep string":              leafCode(0xf3, 0xa5),
		"segmented memory":        leafCode(0x65, 0x48, 0x8b, 0x04, 0x25, 0x30, 0, 0, 0),
		"AVX":                     leafCode(0xc5, 0xf8, 0x57, 0xc0),
		"lock":                    leafCode(0xf0, 0x48, 0x01, 0x01),
		"embedded return":         leafCode(0xc3, 0x90),
		"ret pop":                 append(bytes.Repeat([]byte{0x90}, 16), 0xc2, 0x10, 0),
		"missing return":          bytes.Repeat([]byte{0x90}, 32),
		"truncated":               append(bytes.Repeat([]byte{0x90}, 16), 0x48, 0x8b),
		"short":                   []byte{0xc3},
		"oversized":               append(bytes.Repeat([]byte{0x90}, 4096), 0xc3),
	}
	for name, code := range cases {
		t.Run(name, func(t *testing.T) {
			before := append([]byte(nil), code...)
			p, err := BuildLeaf(code, 0x100000, 0x180000, 0x300000, 0x400000, 1)
			if !errors.Is(err, ErrUnsupported) || len(p.Image) != 0 || !bytes.Equal(code, before) {
				t.Fatal("unsupported leaf was accepted or partially changed", p, err)
			}
		})
	}
	for _, v := range []struct {
		name                      string
		source, base, enter, exit uint64
		id                        uint32
	}{
		{"source overlap", 0x100000, 0x100010, 0x300000, 0x400000, 1},
		{"helper in body", 0x100000, 0x180000, 0x180220, 0x400000, 1},
		{"helper in source", 0x100000, 0x180000, 0x100005, 0x400000, 1},
		{"source overflow", ^uint64(0) - 8, 0x180000, 0x300000, 0x400000, 1},
		{"allocation overflow", 0x100000, maxUserAddress - 1000, 0x300000, 0x400000, 1},
		{"distant RIP", 0x100000, 0x100000000, 0x300000, 0x400000, 1},
		{"zero id", 0x100000, 0x180000, 0x300000, 0x400000, 0},
	} {
		t.Run(v.name, func(t *testing.T) {
			code := leafCode(0x48, 0x8b, 0x05, 0x00, 0x10, 0, 0)
			if p, err := BuildLeaf(code, v.source, v.base, v.enter, v.exit, v.id); !errors.Is(err, ErrUnsupported) || len(p.Image) != 0 {
				t.Fatal(p, err)
			}
		})
	}
	t.Run("self reference", func(t *testing.T) {
		code := leafCode(0x48, 0x8b, 0x05, 0xe9, 0xff, 0xff, 0xff)
		if _, err := BuildLeaf(code, 0x100000, 0x180000, 0x300000, 0x400000, 1); !errors.Is(err, ErrUnsupported) {
			t.Fatal(err)
		}
	})
}

func FuzzLeafPlan(f *testing.F) {
	f.Add(leafCode(0x48, 0x89, 0xc8))
	f.Add(leafCode(0x48, 0x83, 0xec, 0x28))
	f.Fuzz(func(t *testing.T, code []byte) {
		before := append([]byte(nil), code...)
		p, err := BuildLeaf(code, 0x100000, 0x180000, 0x300000, 0x400000, 1)
		if !bytes.Equal(before, code) {
			t.Fatal("mutated source")
		}
		if err != nil {
			if len(p.Image) != 0 {
				t.Fatal("partial failed plan")
			}
			return
		}
		if len(p.Patch) < JumpSize || int(p.ExitRVA) >= len(p.Image) || p.Functions[1].End > uint32(len(p.Image)) {
			t.Fatal("invalid successful plan")
		}
	})
}

func TestLeafPlanScalarSSEPrefixAndWholeInstructions(t *testing.T) {
	code := append([]byte{0xf2, 0x0f, 0x58, 0xc1, // addsd xmm0,xmm1
		0xf2, 0x0f, 0x59, 0xc2, // mulsd xmm0,xmm2
		0xf2, 0x0f, 0x10, 0x4c, 0x24, 0x28, // movsd xmm1,[rsp+40]
		0xf2, 0x0f, 0x5c, 0xc1}, 0xc3) // subsd xmm0,xmm1; ret
	p, err := BuildLeaf(code, 0x100000, 0x180000, 0x300000, 0x400000, 1)
	if err != nil {
		t.Fatal(err)
	}
	if len(p.Patch) != 14 || !bytes.Equal(p.Image[p.BodyRVA:p.BodyRVA+18], code[:18]) {
		t.Fatal("SSE prefix lost instructions")
	}
	// The last overwritten instruction straddles byte 14: its entire disp32
	// must be saved and padded, not split by the entry jump.
	code = append(bytes.Repeat([]byte{0x90}, 12), 0x48, 0x8b, 0x81, 0x80, 0, 0, 0, 0xc3)
	p, err = BuildLeaf(code, 0x100000, 0x180000, 0x300000, 0x400000, 1)
	if err != nil || len(p.Patch) != 19 || !bytes.Equal(p.Original, code[:19]) {
		t.Fatal("split prefix instruction", p, err)
	}
}
