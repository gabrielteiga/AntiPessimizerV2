package x64

import (
	"bytes"
	"encoding/binary"
	"errors"
	"testing"
)

func framedStringImage(op []byte) ([]byte, []byte) {
	code := []byte{0x56, 0x57} // PUSH RSI; PUSH RDI, described by native unwind
	code = append(code, bytes.Repeat([]byte{0x90}, 14)...)
	code = append(code, op...)
	code = append(code, 0x5f, 0x5e, 0xc3)
	image := scalarImage(code)
	copy(image[0x480:], []byte{1, 2, 2, 0, 2, 0x70, 1, 0x60})
	binary.LittleEndian.PutUint32(image[0x414:], 0x2090)
	image[0x490] = 1
	return image, code
}

func TestStringMemoryOperationsRequireNativeFramesAndPreservePrefixes(t *testing.T) {
	for _, op := range [][]byte{
		{0xf3, 0x48, 0xa5}, // REP MOVSQ
		{0xf3, 0xa4},       // REP MOVSB
		{0xf3, 0x48, 0xab}, // REP STOSQ
		{0xf3, 0xaa},       // REP STOSB
		{0xa5}, {0xab},     // single MOVSD/STOSD, not scalar SSE
	} {
		image, code := framedStringImage(op)
		p, err := BuildPEFunction(image, peLeafRVA, uint32(len(code)), 0x100000, 0x180000, 0x300000, 0x400000, 0x500000, 1)
		if err != nil {
			t.Fatalf("%x: %v", op, err)
		}
		if !bytes.Equal(p.Image[p.BodyRVA:p.BodyRVA+uint32(len(code))-1], code[:len(code)-1]) {
			t.Fatal("string prefix/body changed")
		}
		leaf := append(append(bytes.Repeat([]byte{0x90}, 16), op...), 0xc3)
		if _, err := BuildPEFunction(scalarImage(leaf), peLeafRVA, uint32(len(leaf)), 0x100000, 0x180000, 0x300000, 0x400000, 0x500000, 1); !errors.Is(err, ErrUnsupported) {
			t.Fatal("implicit nonvolatile changes accepted in a native leaf", op, err)
		}
	}
	for _, op := range [][]byte{{0x67, 0xf3, 0xa5}, {0xf0, 0xa5}, {0xfd}, {0x6d}, {0x6f}} {
		image, code := framedStringImage(op)
		if _, err := BuildPEFunction(image, peLeafRVA, uint32(len(code)), 0x100000, 0x180000, 0x300000, 0x400000, 0x500000, 1); !errors.Is(err, ErrUnsupported) {
			t.Fatal("unsupported string addressing/DF/I/O accepted", op, err)
		}
	}
}

func TestAtomicMemoryOperationsPreserveLockAndRejectInvalidPrefixes(t *testing.T) {
	for _, op := range [][]byte{
		{0xf0, 0x48, 0x0f, 0xc1, 0x01}, // LOCK XADD [RCX],RAX
		{0xf0, 0x4c, 0x0f, 0xb1, 0x01}, // LOCK CMPXCHG [RCX],R8
		{0xf0, 0x48, 0xff, 0x01},       // LOCK INC qword [RCX]
		{0xf0, 0x48, 0x83, 0x01, 1},    // LOCK ADD qword [RCX],1
	} {
		code := append(append(bytes.Repeat([]byte{0x90}, 16), op...), 0xc3)
		p, err := BuildPEFunction(scalarImage(code), peLeafRVA, uint32(len(code)), 0x100000, 0x180000, 0x300000, 0x400000, 0x500000, 1)
		if err != nil || !bytes.Equal(p.Image[p.BodyRVA:p.BodyRVA+uint32(len(code))-1], code[:len(code)-1]) {
			t.Fatal("atomic body changed or rejected", op, err)
		}
		if _, err := BuildPELeaf(scalarImage(code), peLeafRVA, uint32(len(code)), 0x100000, 0x180000, 0x300000, 0x400000, 1); !errors.Is(err, ErrUnsupported) {
			t.Fatal("legacy installer unexpectedly accepted LOCK", op, err)
		}
	}
	for _, op := range [][]byte{
		{0xf0, 0x48, 0x01, 0xc8},             // LOCK on a register destination
		{0xf0, 0x48, 0x89, 0x01},             // LOCK MOV
		{0xf2, 0xf0, 0x48, 0x0f, 0xc1, 0x01}, // HLE XACQUIRE
		{0xf3, 0xf0, 0x48, 0x0f, 0xc1, 0x01}, // HLE XRELEASE
		{0xf0, 0x48, 0x0f, 0xc7, 0x09},       // CMPXCHG16B implicit RBX/RCX
	} {
		code := append(append(bytes.Repeat([]byte{0x90}, 16), op...), 0xc3)
		if _, err := BuildPEFunction(scalarImage(code), peLeafRVA, uint32(len(code)), 0x100000, 0x180000, 0x300000, 0x400000, 0x500000, 1); !errors.Is(err, ErrUnsupported) {
			t.Fatal("unsupported atomic operation accepted", op, err)
		}
	}
}
