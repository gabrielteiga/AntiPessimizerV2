package x64

import (
	"bytes"
	"encoding/binary"
	"errors"
	"testing"
)

func cleanupPE() ([]byte, []byte) {
	image, code := framedPE()
	image[0x480] = 17
	binary.LittleEndian.PutUint32(image[0x488:], 0x1080)
	copy(image[0x48c:], []byte{0xde, 0xad, 0xbe, 0xef}) // opaque original data
	return image, code
}

func TestOriginalCleanupKeepsSourceMetadataAndCompactPCMap(t *testing.T) {
	image, code := cleanupPE()
	before := append([]byte(nil), image...)
	p, err := BuildPEFunction(image, peLeafRVA, uint32(len(code)), 0x100000, 0x180000, 0x300000, 0x400000, 0x500000, 7)
	if err != nil {
		t.Fatal(err)
	}
	if p.OriginalHandlerRVA != 0x1080 || !bytes.Equal(image, before) {
		t.Fatal("original image or handler changed")
	}
	info := p.Image[p.Functions[1].Unwind:]
	data := info[12:]
	want := []uint32{7, 1, peLeafRVA, uint32(len(code)), 0x2000, 0x2080, 0x1080, 0x208c, 2}
	for i, value := range want {
		if got := binary.LittleEndian.Uint32(data[i*4:]); got != value {
			t.Fatalf("descriptor[%d]=%#x want %#x", i, got, value)
		}
	}
	if !bytes.Equal(data[36:40], []byte{0, 0, 0, 0}) || binary.LittleEndian.Uint16(data[40:]) != uint16(len(code)) || binary.LittleEndian.Uint16(data[42:]) != uint16(len(code)+13) {
		t.Fatal("RET expansion missing from source PC map", data[36:])
	}
	if _, err := BuildPELeaf(image, peLeafRVA, uint32(len(code)), 0x100000, 0x180000, 0x300000, 0x400000, 7); !errors.Is(err, ErrUnsupported) {
		t.Fatal("legacy planner accepted original cleanup", err)
	}
}

func TestOriginalCleanupRejectsInvalidHandlers(t *testing.T) {
	for _, handler := range []uint32{0, peLeafRVA, peLeafRVA + 3, 0x208c, 0xffffffff} {
		image, code := cleanupPE()
		binary.LittleEndian.PutUint32(image[0x488:], handler)
		if p, err := BuildPEFunction(image, peLeafRVA, uint32(len(code)), 0x100000, 0x180000, 0x300000, 0x400000, 0x500000, 7); !errors.Is(err, ErrUnsupported) || len(p.Image) != 0 {
			t.Fatalf("invalid original handler %#x accepted: %v", handler, err)
		}
	}
}

func TestPCMapIncludesExpandedBranchBeforeProtectedCode(t *testing.T) {
	code := []byte{0x48, 0x83, 0xec, 0x28, 0x85, 0xc9, 0x75, 5, 0x48, 0x83, 0xc4, 0x28, 0xc3, 0x90, 0x48, 0x83, 0xc4, 0x28, 0xc3}
	flow, err := rewriteFunctionWithPatch(code, 0x100000, 0x180000, true, 4, 5, true)
	if err != nil {
		t.Fatal(err)
	}
	want := []pcMapping{{0, 0}, {8, 12}, {13, 30}, {19, 49}}
	if len(flow.pcMap) != len(want) {
		t.Fatal("wrong expanded PC ranges", flow.pcMap)
	}
	for i, mapping := range flow.pcMap {
		if mapping != want[i] {
			t.Fatal("wrong source translation", flow.pcMap)
		}
	}
}
