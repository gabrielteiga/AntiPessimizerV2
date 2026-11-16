package x64

import (
	"bytes"
	"encoding/binary"
	"errors"
	"testing"

	"golang.org/x/arch/x86/x86asm"
)

func ordinary() []byte {
	return append([]byte{0x55, 0x48, 0x89, 0xe5, 0x48, 0x83, 0xec, 0x20}, append(bytes.Repeat([]byte{0x90}, 6), 0xc9, 0xc3)...)
}
func TestPlanCopiesWholeInstructionsAndRegisterFreeJumps(t *testing.T) {
	code := ordinary()
	before := append([]byte(nil), code...)
	p, err := Build(code, 0x1000, 0x2000, 0x3000)
	if err != nil {
		t.Fatal(err)
	}
	if p.Overwrite != 14 || p.Resume != 0x100e || !bytes.Equal(p.Original, before[:14]) || !bytes.Equal(p.Trampoline[:14], p.Original) {
		t.Fatal(p)
	}
	if !bytes.Equal(p.Patch[:6], []byte{0xff, 0x25, 0, 0, 0, 0}) || binary.LittleEndian.Uint64(p.Patch[6:]) != 0x3000 {
		t.Fatal(p.Patch)
	}
	if binary.LittleEndian.Uint64(p.Trampoline[len(p.Trampoline)-8:]) != p.Resume {
		t.Fatal(p)
	}
	p.Original[0] = 0
	p.Patch[0] = 0
	if !bytes.Equal(before, code) {
		t.Fatal("plan mutated target bytes")
	}
}
func TestRelocateRIPRelativeMemory(t *testing.T) {
	code := append([]byte{0x48, 0x8b, 0x05, 0x10, 0, 0, 0}, append(bytes.Repeat([]byte{0x90}, 7), 0xc3)...)
	for _, trampoline := range []uint64{0x2000, 0x800} {
		p, err := Build(code, 0x1000, trampoline, 0x4000)
		if err != nil {
			t.Fatal(err)
		}
		inst, err := x86asm.Decode(p.Trampoline, 64)
		if err != nil {
			t.Fatal(err)
		}
		mem := inst.Args[1].(x86asm.Mem)
		if int64(trampoline)+int64(inst.Len)+int64(int32(mem.Disp)) != 0x1017 {
			t.Fatal("RIP target changed", inst)
		}
	}
	if _, err := Build(code, 0x1000, 0x700000000000, 0x4000); !errors.Is(err, ErrUnsupported) {
		t.Fatal("accepted out-of-range RIP displacement", err)
	}
}
func TestWholeInstructionBoundary(t *testing.T) {
	code := append(bytes.Repeat([]byte{0x90}, 13), 0x48, 0x83, 0xec, 0x20, 0xc3)
	p, err := Build(code, 0x1000, 0x2000, 0x3000)
	if err != nil {
		t.Fatal(err)
	}
	if p.Overwrite != 17 || len(p.Patch) != 17 || !bytes.Equal(p.Patch[14:], []byte{0x90, 0x90, 0x90}) {
		t.Fatal(p)
	}
}
func TestRejectUnsafePlans(t *testing.T) {
	cases := map[string][]byte{
		"small":                        bytes.Repeat([]byte{0x90}, 14),
		"call prefix":                  append([]byte{0xe8, 0, 0, 0, 0}, ordinary()...),
		"jump prefix":                  append([]byte{0xeb, 0}, ordinary()...),
		"return prefix":                append([]byte{0xc3}, ordinary()...),
		"self-referencing RIP":         append([]byte{0x48, 0x8d, 0x05, 0, 0, 0, 0}, ordinary()...),
		"truncated body":               append(ordinary(), 0x0f),
		"unsupported prefix op":        append([]byte{0x0f, 0x31}, ordinary()...), // rdtsc
		"jump into overwritten prefix": append(bytes.Repeat([]byte{0x90}, 14), 0xe9, 0xf0, 0xff, 0xff, 0xff, 0xc3),
		"indirect jump in body":        append(bytes.Repeat([]byte{0x90}, 14), 0xff, 0xe0),
	}
	for name, code := range cases {
		t.Run(name, func(t *testing.T) {
			before := append([]byte(nil), code...)
			p, err := Build(code, 0x1000, 0x2000, 0x3000)
			if !errors.Is(err, ErrUnsupported) || p.Overwrite != 0 {
				t.Fatal(p, err)
			}
			if !bytes.Equal(before, code) {
				t.Fatal("failed plan mutated input")
			}
		})
	}
	for _, addresses := range [][3]uint64{{0, 0x2000, 0x3000}, {0x1000, 0x1001, 0x3000}, {0x1000, 0x2000, 0x1005}, {^uint64(0), 0x2000, 0x3000}} {
		if _, err := Build(ordinary(), addresses[0], addresses[1], addresses[2]); err == nil {
			t.Fatal("invalid/overlapping addresses accepted", addresses)
		}
	}
}
func TestLocalTailJumpOutsidePatchIsPreserved(t *testing.T) {
	code := append(bytes.Repeat([]byte{0x90}, 14), 0xeb, 0xfe)
	if _, err := Build(code, 0x1000, 0x2000, 0x3000); err != nil {
		t.Fatal(err)
	}
}
func FuzzPlan(f *testing.F) {
	f.Add(ordinary(), uint64(0x1000), uint64(0x2000))
	f.Add(append([]byte{0x48, 0x8b, 0x05, 0x10, 0, 0, 0}, append(bytes.Repeat([]byte{0x90}, 7), 0xc3)...), uint64(0x1000), uint64(0x2000))
	f.Fuzz(func(t *testing.T, b []byte, source, trampoline uint64) {
		if len(b) > 4096 {
			t.Skip()
		}
		before := append([]byte(nil), b...)
		p, err := Build(b, source, trampoline, 0x4000)
		if !bytes.Equal(b, before) {
			t.Fatal("input modified")
		}
		if err == nil {
			if p.Overwrite < 14 || p.Overwrite >= len(b) || p.Resume != source+uint64(p.Overwrite) || len(p.Patch) != p.Overwrite {
				t.Fatal(p)
			}
			for offset := 0; offset < p.Overwrite; {
				a, err := x86asm.Decode(b[offset:], 64)
				if err != nil {
					t.Fatal(err)
				}
				if a.PCRel == 4 {
					oldDisp := int64(int32(binary.LittleEndian.Uint32(b[offset+a.PCRelOff:])))
					newDisp := int64(int32(binary.LittleEndian.Uint32(p.Trampoline[offset+a.PCRelOff:])))
					oldTarget, ok1 := address(source+uint64(offset+a.Len), oldDisp)
					newTarget, ok2 := address(trampoline+uint64(offset+a.Len), newDisp)
					if !ok1 || !ok2 || oldTarget != newTarget {
						t.Fatal("relocation changed effective address")
					}
				}
				offset += a.Len
			}
		}
	})
}

func TestNegativeOriginalRIPDisplacement(t *testing.T) {
	code := append([]byte{0x48, 0x8b, 0x05, 0, 0xf0, 0xff, 0xff}, append(bytes.Repeat([]byte{0x90}, 7), 0xc3)...)
	p, err := Build(code, 0x5000, 0x6000, 0x7000)
	if err != nil {
		t.Fatal(err)
	}
	inst, err := x86asm.Decode(p.Trampoline, 64)
	if err != nil {
		t.Fatal(err)
	}
	disp := int64(int32(binary.LittleEndian.Uint32(p.Trampoline[inst.PCRelOff:])))
	if 0x6000+int64(inst.Len)+disp != 0x4007 {
		t.Fatal("negative RIP target changed", disp)
	}
}
