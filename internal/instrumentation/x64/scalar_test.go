package x64

import (
	"bytes"
	"encoding/binary"
	"errors"
	"testing"
)

func scalarImage(code []byte) []byte {
	image := leafPE()
	copy(image[0x220:], code)
	binary.LittleEndian.PutUint32(image[0x404:], peLeafRVA+uint32(len(code)))
	return image
}
func TestFunctionLeafHomeSlotsAndScalarOperationsPreserveBody(t *testing.T) {
	code := []byte{0x48, 0x89, 0x4c, 0x24, 8, 0x48, 0x8b, 0x44, 0x24, 8, 0x48, 0x6b, 0xc0, 3, 0x48, 0xd1, 0xc8, 0x48, 0xd1, 0xc0, 0x48, 0x83, 0xf8, 0, 0x0f, 0x9f, 0xc1, 0xc3}
	image := scalarImage(code)
	p, err := BuildPEFunction(image, peLeafRVA, uint32(len(code)), 0x100000, 0x180000, 0x300000, 0x400000, 0x500000, 1)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(p.Source, code) || !bytes.Equal(p.Image[p.BodyRVA:p.BodyRVA+uint32(len(code))-1], code[:len(code)-1]) {
		t.Fatal("scalar/home-slot body changed")
	}
	if _, err := BuildPELeaf(image, peLeafRVA, uint32(len(code)), 0x100000, 0x180000, 0x300000, 0x400000, 1); !errors.Is(err, ErrUnsupported) {
		t.Fatal("legacy stack-store policy changed", err)
	}
}
func TestFunctionLeafRejectsReturnSlotAndHomeOverrun(t *testing.T) {
	for _, offset := range []byte{0, 4, 36, 40} {
		code := []byte{0x48, 0x89, 0x4c, 0x24, offset, 0xc3}
		if _, err := BuildPEFunction(scalarImage(code), peLeafRVA, uint32(len(code)), 0x100000, 0x180000, 0x300000, 0x400000, 0x500000, 1); !errors.Is(err, ErrUnsupported) {
			t.Fatal("invalid stack store accepted", offset, err)
		}
	}
	// CMP reads its first memory operand: the fifth argument is outside the home
	// area but reading it does not overwrite the caller's storage or return slot.
	code := []byte{0x48, 0x39, 0x4c, 0x24, 40, 0x0f, 0x9f, 0xc0, 0xc3}
	if _, err := BuildPEFunction(scalarImage(code), peLeafRVA, uint32(len(code)), 0x100000, 0x180000, 0x300000, 0x400000, 0x500000, 1); err != nil {
		t.Fatal("read-only stack argument rejected", err)
	}
}
func TestScalarExpansionStillRejectsImplicitNonvolatileAndVectorState(t *testing.T) {
	for _, op := range [][]byte{{0x0f, 0xa2}, {0xf3, 0x48, 0xa5}, {0xc5, 0xfc, 0x57, 0xc0}, {0x0f, 0xae, 0x14, 0x24}} {
		code := append(append(bytes.Repeat([]byte{0x90}, 16), op...), 0xc3)
		if _, err := BuildPEFunction(scalarImage(code), peLeafRVA, uint32(len(code)), 0x100000, 0x180000, 0x300000, 0x400000, 0x500000, 1); !errors.Is(err, ErrUnsupported) {
			t.Fatal("unsafe implicit state accepted", op, err)
		}
	}
}

func TestNativeTimestampPreservesOriginalEncoding(t *testing.T) {
	code := []byte{0x0f, 0x31, 0x48, 0xc1, 0xe2, 0x20, 0x48, 0x09, 0xd0, 0xc3}
	p, err := BuildPEFunction(scalarImage(code), peLeafRVA, uint32(len(code)), 0x100000, 0x180000, 0x300000, 0x400000, 0x500000, 1)
	if err != nil || !bytes.Equal(p.Image[p.BodyRVA:p.BodyRVA+9], code[:9]) {
		t.Fatal("native timestamp instruction changed", err)
	}
	prefixed := append([]byte{0x66}, code...)
	for _, bad := range [][]byte{prefixed, {0x0f, 0x01, 0xf9, 0x90, 0x90, 0xc3}} {
		if _, err := BuildPEFunction(scalarImage(bad), peLeafRVA, uint32(len(bad)), 0x100000, 0x180000, 0x300000, 0x400000, 0x500000, 1); !errors.Is(err, ErrUnsupported) {
			t.Fatal("accepted a prefixed or unverified timestamp variant", bad, err)
		}
	}
}

func TestSignedAddStackAllocationMatchesReferenceDelphiUnwind(t *testing.T) {
	// Prologue/unwind shape observed at RVA 0x80460 in the original distributed
	// Delphi DLL: PUSH RBP; ADD RSP,-128; MOV RBP,RSP, ALLOC_SMALL 128 + SET_FPREG.
	code := []byte{0x55, 0x48, 0x83, 0xc4, 0x80, 0x48, 0x8b, 0xec}
	info := []byte{1, 8, 3, 5, 8, 3, 5, 0xf2, 1, 0x50, 0, 0}
	if err := validateUnwindCodes(info); err != nil {
		t.Fatal(err)
	}
	if err := validatePrologueActions(code, info); err != nil {
		t.Fatal(err)
	}
	for _, immediate := range []byte{0x7f, 0x81, 0x00} {
		bad := append([]byte(nil), code...)
		bad[4] = immediate
		if err := validatePrologueActions(bad, info); err == nil {
			t.Fatal("different signed stack allocation accepted", immediate)
		}
	}
}

func TestStackBitWritesCannotIndexIntoReturnOrCallerSlots(t *testing.T) {
	// BTS [RSP+8],RCX can address outside that word, including the return slot
	// when RCX is negative; checking only the encoded displacement is insufficient.
	code := []byte{0x48, 0x0f, 0xab, 0x4c, 0x24, 8, 0xc3}
	if _, err := BuildPEFunction(scalarImage(code), peLeafRVA, uint32(len(code)), 0x100000, 0x180000, 0x300000, 0x400000, 0x500000, 1); !errors.Is(err, ErrUnsupported) {
		t.Fatal("dynamic stack bit offset accepted", err)
	}
}

func TestNativeThreadStackBaseReadKeepsExactEncoding(t *testing.T) {
	source := []byte{0x65, 0x48, 0xa1, 8, 0, 0, 0, 0, 0, 0, 0, 0xc3}
	plan := func(code []byte) (FunctionPlan, error) {
		return BuildPEFunction(scalarImage(code), peLeafRVA, uint32(len(code)), 0x100000, 0x180000, 0x300000, 0x400000, 0x500000, 1)
	}
	p, err := plan(source)
	if err != nil || !bytes.Equal(p.Image[p.BodyRVA:p.BodyRVA+11], source[:11]) {
		t.Fatal("StackBase operand changed", err)
	}
	for _, change := range []struct {
		i int
		b byte
	}{{0, 0x64}, {2, 0xa3}, {3, 0x10}} {
		code := bytes.Clone(source)
		code[change.i] = change.b
		if _, err := plan(code); err == nil {
			t.Fatal("other TEB access accepted", code)
		}
	}
}
