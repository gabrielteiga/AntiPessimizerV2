package x64

import (
	"bytes"
	"errors"
	"testing"
)

func TestSelectedTinyLeavesPatchOnlyTheEntryByte(t *testing.T) {
	for _, code := range [][]byte{{0xc3}, {0x90, 0xc3}, {0x31, 0xc0, 0xc3}, {0x8d, 0x41, 1, 0xc3}} {
		image := scalarImage(code)
		p, err := NewPEImage(image)
		if err != nil {
			t.Fatal(err)
		}
		plan, err := p.BuildSelectedFunction(peLeafRVA, uint32(len(code)), 0x100000, 0x180000, 0x300000, 0x400000, 0x500000, 1, []uint32{peLeafRVA})
		if err != nil || !bytes.Equal(plan.Source, code) || !bytes.Equal(plan.Patch, []byte{0xcc}) || !bytes.Equal(plan.Original, code[:1]) {
			t.Fatal("unexpected tiny plan", code, err)
		}
		if !bytes.Equal(plan.Image[plan.BodyRVA:plan.BodyRVA+uint32(len(code))-1], code[:len(code)-1]) {
			t.Fatal("tiny body changed", code)
		}
		if _, err := p.BuildFunction(peLeafRVA, uint32(len(code)), 0x100000, 0x180000, 0x300000, 0x400000, 0x500000, 1); !errors.Is(err, ErrUnsupported) {
			t.Fatal("standalone plan accepted breakpoint entry", err)
		}
	}
}
