package x64

import (
	"bytes"
	"errors"
	"testing"
)

func TestSelectedCallerContextGettersPreserveReadOnlyRestriction(t *testing.T) {
	for _, code := range [][]byte{{0x48, 0x89, 0xe8, 0xc3}, {0x48, 0x89, 0xe0, 0xc3}} {
		image, err := NewPEImage(scalarImage(code))
		if err != nil {
			t.Fatal(err)
		}
		plan, err := image.BuildSelectedFunction(peLeafRVA, 4, 0x100000, 0x180000, 0x300000, 0x400000, 0x500000, 7, []uint32{peLeafRVA})
		if err != nil || !bytes.Equal(plan.Source, code) || !bytes.Equal(plan.Patch, []byte{0xcc}) {
			t.Fatal("context read rejected", err)
		}
	}
	for _, code := range [][]byte{{0x48, 0x89, 0xc5, 0xc3}, {0x48, 0x89, 0xc4, 0xc3}, {0x48, 0x01, 0xe8, 0xc3}, {0x48, 0x87, 0xe8, 0xc3}} {
		image, err := NewPEImage(scalarImage(code))
		if err != nil {
			t.Fatal(err)
		}
		if _, err := image.BuildSelectedFunction(peLeafRVA, 4, 0x100000, 0x180000, 0x300000, 0x400000, 0x500000, 7, []uint32{peLeafRVA}); !errors.Is(err, ErrUnsupported) {
			t.Fatal("context modification accepted", code, err)
		}
	}
}
