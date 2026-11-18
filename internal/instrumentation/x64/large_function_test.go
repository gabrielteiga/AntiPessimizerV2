package x64

import (
	"bytes"
	"errors"
	"testing"
)

func largeFunctionPE(size int) ([]byte, []byte) {
	b := leafPE()
	b = append(b, make([]byte, 0x3400-len(b))...)
	w32(b, peOptional+56, 0x6000)
	w32(b, peOptional+136, 0x5000)
	w32(b, peOptional+140, 12)
	w32(b, peSections+8, 0x3000)
	w32(b, peSections+16, 0x3000)
	w32(b, peSections+40+12, 0x5000)
	w32(b, peSections+40+20, 0x3200)
	clear(b[0x200:])
	code := bytes.Repeat([]byte{0x90}, size)
	copy(code, []byte{0x48, 0x83, 0xec, 0x28, 0xe8, 0xd7, 0xff, 0xff, 0xff}) // call executable RVA 0x1000
	copy(code[len(code)-5:], []byte{0x48, 0x83, 0xc4, 0x28, 0xc3})
	copy(b[0x220:], code)
	b[0x200] = 0xc3
	w32(b, 0x3200, peLeafRVA)
	w32(b, 0x3204, peLeafRVA+uint32(size))
	w32(b, 0x3208, 0x5080)
	copy(b[0x3280:], []byte{1, 4, 1, 0, 4, 0x42, 0, 0})
	return b, code
}
func TestLargeFunctionPlanPreservesExtentAndUnwind(t *testing.T) {
	for _, size := range []int{4097, 8000, 8192} {
		b, code := largeFunctionPE(size)
		before := append([]byte(nil), b...)
		p, err := BuildPEFunction(b, peLeafRVA, uint32(size), 0x100000, 0x180000, 0x300000, 0x400000, 0x500000, 17)
		if err != nil {
			t.Fatal(size, err)
		}
		if !bytes.Equal(p.Source, code) || !bytes.Equal(b, before) || len(p.Image) > MaxFunctionImageBytes || len(p.Image) <= 8192 && size == 8192 {
			t.Fatal("large source/image extent", size, len(p.Image))
		}
		if p.Image[p.Functions[1].Unwind] != 17 || p.Functions[1].End <= p.Functions[1].Begin {
			t.Fatal("large frame lost native unwind")
		}
	}
	b, _ := largeFunctionPE(8193)
	p, err := BuildPEFunction(b, peLeafRVA, 8193, 0x100000, 0x180000, 0x300000, 0x400000, 0x500000, 17)
	if !errors.Is(err, ErrUnsupported) || len(p.Image) != 0 {
		t.Fatal("oversized method returned executable code", err)
	}
}
