package x64

import (
	"bytes"
	"encoding/binary"
	"errors"
	"testing"
)

const peOptional = 0x80 + 24
const peSections = peOptional + 240
const peLeafRVA = 0x1020
const peLeafSize = 20

func leafPE() []byte {
	b := make([]byte, 0x600)
	w16 := func(off int, v uint16) { binary.LittleEndian.PutUint16(b[off:], v) }
	w32 := func(off int, v uint32) { binary.LittleEndian.PutUint32(b[off:], v) }
	copy(b, "MZ")
	w32(60, 0x80)
	copy(b[0x80:], "PE\x00\x00")
	w16(0x84, 0x8664)
	w16(0x86, 2)
	w16(0x94, 240)
	w16(peOptional, 0x20b)
	w32(peOptional+56, 0x3000)
	w32(peOptional+60, 0x200)
	w32(peOptional+108, 16)
	w32(peOptional+136, 0x2000)
	w32(peOptional+140, 24)
	for i, name := range []string{".text", ".pdata"} {
		s := peSections + i*40
		copy(b[s:], name)
		w32(s+8, 0x200)
		w32(s+12, uint32((i+1)*0x1000))
		w32(s+16, 0x200)
		w32(s+20, uint32((i+1)*0x200))
		flags := uint32(0x40000040)
		if i == 0 {
			flags = 0x60000020
		}
		w32(s+36, flags)
	}
	copy(b[0x220:], leafCode(0x48, 0x89, 0xc8))
	for i, begin := range []uint32{peLeafRVA, 0x1080} {
		at := 0x400 + i*12
		w32(at, begin)
		w32(at+4, begin+peLeafSize)
		w32(at+8, uint32(0x2080+i*4))
		b[0x480+i*4] = 1
	}
	return b
}

func TestPELeafChecksExceptionDirectoryAndActualBase(t *testing.T) {
	for _, noTable := range []bool{false, true} {
		image := leafPE()
		if noTable {
			clear(image[peOptional+136 : peOptional+144])
		}
		before := append([]byte(nil), image...)
		for _, base := range []uint64{0x100000, 0x7ff600000000} {
			p, err := BuildPELeaf(image, peLeafRVA, peLeafSize, base, base+0x10000, 0x500000, 0x500100, 1)
			if err != nil {
				t.Fatal(err)
			}
			if !bytes.Equal(p.Original, before[0x220:0x220+len(p.Patch)]) || binary.LittleEndian.Uint64(p.Patch[6:]) != base+0x10000 {
				t.Fatal("incorrect source/base mapping", p)
			}
			p.Original[0] = 0xcc
			if !bytes.Equal(image, before) {
				t.Fatal("plan aliases PE source")
			}
		}
	}
}

func TestPELeafRejectsUnsafeOrAmbiguousMetadata(t *testing.T) {
	w32 := func(b []byte, off int, v uint32) { binary.LittleEndian.PutUint32(b[off:], v) }
	cases := map[string]func([]byte){
		"bad DOS":                    func(b []byte) { b[0] = 0 },
		"bad NT":                     func(b []byte) { w32(b, 60, 0xfffffff0) },
		"NT in DOS":                  func(b []byte) { w32(b, 60, 2) },
		"bad signature":              func(b []byte) { b[0x80] = 0 },
		"wrong machine":              func(b []byte) { b[0x84] = 0x4c },
		"PE32":                       func(b []byte) { b[peOptional] = 0x0b; b[peOptional+1] = 1 },
		"too many sections":          func(b []byte) { b[0x86] = 97 },
		"missing sections":           func(b []byte) { b[0x86] = 0 },
		"short optional":             func(b []byte) { b[0x94] = 100 },
		"too many directories":       func(b []byte) { w32(b, peOptional+108, 17) },
		"directory exceeds optional": func(b []byte) { binary.LittleEndian.PutUint16(b[0x94:], 144) },
		"zero image":                 func(b []byte) { w32(b, peOptional+56, 0) },
		"small headers":              func(b []byte) { w32(b, peOptional+60, 0x100) },
		"headers past file":          func(b []byte) { w32(b, peOptional+60, 0x1000) },
		"section past image":         func(b []byte) { w32(b, peSections+8, 0xfffffff0) },
		"section raw truncated":      func(b []byte) { w32(b, peSections+16, 0xfffffff0) },
		"section into headers":       func(b []byte) { w32(b, peSections+20, 0x100) },
		"section virtual alias":      func(b []byte) { w32(b, peSections+40+12, 0x1100) },
		"section raw alias":          func(b []byte) { w32(b, peSections+40+20, 0x300) },
		"code nonexecutable":         func(b []byte) { w32(b, peSections+36, 0x40000040) },
		"code virtual only":          func(b []byte) { w32(b, peSections+16, 0) },
		"code raw padding":           func(b []byte) { w32(b, peSections+8, 0x21) },
		"missing directory size":     func(b []byte) { w32(b, peOptional+140, 0) },
		"missing directory RVA":      func(b []byte) { w32(b, peOptional+136, 0) },
		"fractional table row":       func(b []byte) { w32(b, peOptional+140, 23) },
		"table in unbacked data":     func(b []byte) { w32(b, peOptional+136, 0x2200) },
		"table oversized":            func(b []byte) { w32(b, peOptional+140, 12*(1<<20)+12) },
		"function inverted":          func(b []byte) { w32(b, 0x404, peLeafRVA-1) },
		"function unsorted":          func(b []byte) { w32(b, 0x40c, 0x1000) },
		"function overlaps":          func(b []byte) { w32(b, 0x40c, peLeafRVA+1) },
		"function outside code":      func(b []byte) { w32(b, 0x40c, 0x2000); w32(b, 0x410, 0x2014) },
		"unwind zero":                func(b []byte) { w32(b, 0x408, 0) },
		"unwind unaligned":           func(b []byte) { w32(b, 0x408, 0x2081) },
		"unwind unbacked":            func(b []byte) { w32(b, 0x408, 0x3000) },
		"unwind unknown version":     func(b []byte) { b[0x480] = 3 },
		"unwind version2":            func(b []byte) { b[0x480] = 2 },
		"exception handler":          func(b []byte) { b[0x480] = 9 },
		"unwind handler":             func(b []byte) { b[0x480] = 17 },
		"chained info":               func(b []byte) { b[0x480] = 33 },
		"unwind prologue":            func(b []byte) { b[0x481] = 4 },
		"unwind codes":               func(b []byte) { b[0x482] = 2 },
		"unwind frame register":      func(b []byte) { b[0x483] = 5 },
		"partial overlap before":     func(b []byte) { w32(b, 0x400, peLeafRVA-1) },
		"partial overlap after":      func(b []byte) { w32(b, 0x404, peLeafRVA+peLeafSize+1) },
		"stack modifying code":       func(b []byte) { copy(b[0x220:], []byte{0x48, 0x83, 0xec, 0x28}) },
	}
	for name, mutate := range cases {
		t.Run(name, func(t *testing.T) {
			image := leafPE()
			mutate(image)
			if p, err := BuildPELeaf(image, peLeafRVA, peLeafSize, 0x100000, 0x200000, 0x500000, 0x500100, 1); !errors.Is(err, ErrUnsupported) || len(p.Image) != 0 {
				t.Fatal("accepted unsafe PE", p, err)
			}
		})
	}
	for _, v := range []struct {
		name             string
		rva, size        uint32
		base, allocation uint64
	}{
		{"zero base", peLeafRVA, peLeafSize, 0, 0x200000},
		{"base overflow", peLeafRVA, peLeafSize, maxUserAddress - 0x1000, 0x200000},
		{"allocation in another section", peLeafRVA, peLeafSize, 0x100000, 0x102000},
		{"allocation crosses image", peLeafRVA, peLeafSize, 0x100000, 0xfff00},
		{"header source", 0x100, peLeafSize, 0x100000, 0x200000},
		{"extent crosses section", 0x11f0, peLeafSize, 0x100000, 0x200000},
		{"size zero", peLeafRVA, 0, 0x100000, 0x200000},
		{"size huge", peLeafRVA, 4097, 0x100000, 0x200000},
		{"RVA overflow", 0xfffffff0, peLeafSize, 0x100000, 0x200000},
	} {
		t.Run(v.name, func(t *testing.T) {
			if p, err := BuildPELeaf(leafPE(), v.rva, v.size, v.base, v.allocation, 0x500000, 0x500100, 1); !errors.Is(err, ErrUnsupported) || len(p.Image) != 0 {
				t.Fatal(p, err)
			}
		})
	}
	for n := 0; n < len(leafPE()); n++ {
		if p, err := BuildPELeaf(leafPE()[:n], peLeafRVA, peLeafSize, 0x100000, 0x200000, 0x500000, 0x500100, 1); !errors.Is(err, ErrUnsupported) || len(p.Image) != 0 {
			t.Fatalf("truncated at %d accepted: %v", n, err)
		}
	}
}

func FuzzPELeafPlan(f *testing.F) {
	f.Add(leafPE(), uint32(peLeafRVA), uint32(peLeafSize))
	f.Add([]byte("MZ"), uint32(0), uint32(16))
	f.Fuzz(func(t *testing.T, b []byte, rva, size uint32) {
		before := append([]byte(nil), b...)
		p, err := BuildPELeaf(b, rva, size, 0x100000, 0x200000, 0x500000, 0x500100, 1)
		if !bytes.Equal(before, b) {
			t.Fatal("mutated input")
		}
		if err != nil && len(p.Image) != 0 {
			t.Fatal("partial failed plan")
		}
	})
}
