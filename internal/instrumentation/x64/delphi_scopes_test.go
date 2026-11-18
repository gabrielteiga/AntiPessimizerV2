package x64

import (
	"bytes"
	"encoding/binary"
	"testing"
)

func TestHandlerFingerprintMasksRelocationOperandsButPreservesBehavior(t *testing.T) {
	code := []byte{0x48, 0x8b, 0x05, 1, 2, 3, 4, 0xe8, 5, 6, 7, 8, 0x83, 0xf8, 2, 0x75, 1, 0x90, 0xc3}
	want, ok := normalizedHandlerFingerprint(code)
	if !ok {
		t.Fatal("valid code rejected")
	}
	moved := append([]byte(nil), code...)
	copy(moved[3:7], []byte{8, 7, 6, 5})
	copy(moved[8:12], []byte{4, 3, 2, 1})
	got, ok := normalizedHandlerFingerprint(moved)
	if !ok || got != want {
		t.Fatal("address placement changed handler identity")
	}
	for _, offset := range []int{0, 12, 14, 16} {
		changed := append([]byte(nil), code...)
		changed[offset] ^= 1
		got, _ := normalizedHandlerFingerprint(changed)
		if got == want {
			t.Fatal("opcode, table constant or control flow was masked", offset)
		}
	}
	if _, ok := normalizedHandlerFingerprint([]byte{0x0f}); ok {
		t.Fatal("truncated handler recognized")
	}
	if !bytes.Equal(code, []byte{0x48, 0x8b, 0x05, 1, 2, 3, 4, 0xe8, 5, 6, 7, 8, 0x83, 0xf8, 2, 0x75, 1, 0x90, 0xc3}) {
		t.Fatal("fingerprinting changed source bytes")
	}
}
func TestDelphiScopesKeepCleanupCallbacksAndMoveCatchTargets(t *testing.T) {
	image, code := scopesPE()
	w32(image, 0x48c, 2)
	w32(image, 0x498, 2)
	w32(image, 0x4a0, peLeafRVA+9)
	w32(image, 0x4a4, peLeafRVA+uint32(len(code)-1))
	w32(image, 0x4a8, 0)
	w32(image, 0x4ac, 0x1080)
	p, err := parsePEImage(image)
	if err != nil {
		t.Fatal(err)
	}
	scopes, err := p.readScopes(0x208c, peLeafRVA, uint32(len(code)), 5, 25, 2)
	if err != nil {
		t.Fatal(err)
	}
	flow, err := rewriteFunctionWithPatch(code, 0x101020, 0x180000, true, 5, 5, true)
	if err != nil {
		t.Fatal(err)
	}
	mapped, err := relocateScopes(scopes, code, flow, peLeafRVA, 0x80200, 2)
	if err != nil {
		t.Fatal(err)
	}
	if binary.LittleEndian.Uint32(mapped[12:]) != 2 || binary.LittleEndian.Uint32(mapped[16:]) != 0x80209 || binary.LittleEndian.Uint32(mapped[28:]) != 0 || binary.LittleEndian.Uint32(mapped[32:]) != 0x1080 {
		t.Fatal("Delphi cleanup and catch target fields were confused", mapped)
	}
	for _, filter := range []uint32{1, 3, 0x1080, ^uint32(0)} {
		w32(image, 0x498, filter)
		p, _ := parsePEImage(image)
		if _, err := p.readScopes(0x208c, peLeafRVA, uint32(len(code)), 5, 25, 2); err == nil {
			t.Fatal("unreviewed typed/fault scope accepted", filter)
		}
	}
}
