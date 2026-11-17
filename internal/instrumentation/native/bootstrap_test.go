package native

import (
	"encoding/binary"
	"testing"
	"unicode/utf16"
)

func TestBootstrapRequestContainsBoundedUTF16AndSelectedExtents(t *testing.T) {
	pipe := `\\.\pipe\AntiPessimizer-ação-😀`
	b, err := bootstrapRequest(pipe, []debugSelection{{1, 0x5020, 64}, {7, 0x6080, 32}}, 1024)
	if err != nil {
		t.Fatal(err)
	}
	if string(b[:8]) != "APBOOT1\x00" || binary.LittleEndian.Uint32(b[8:]) != uint32(len(b)) || binary.LittleEndian.Uint32(b[12:]) != 2 || binary.LittleEndian.Uint32(b[16:]) != 1024 || binary.LittleEndian.Uint64(b[24:]) != 0 {
		t.Fatal("invalid bootstrap frame")
	}
	if binary.LittleEndian.Uint32(b[44:]) != 7 || binary.LittleEndian.Uint32(b[48:]) != 0x6080 || binary.LittleEndian.Uint32(b[52:]) != 32 {
		t.Fatal("selection changed")
	}
	var chars []uint16
	for at := 56; at < len(b)-2; at += 2 {
		chars = append(chars, binary.LittleEndian.Uint16(b[at:]))
	}
	if string(utf16.Decode(chars)) != pipe || binary.LittleEndian.Uint16(b[len(b)-2:]) != 0 {
		t.Fatal("UTF16 pipe changed")
	}
	for _, count := range []int{0, 17} {
		if _, err := bootstrapRequest(pipe, make([]debugSelection, count), 1024); err == nil {
			t.Fatal("invalid count accepted")
		}
	}
	for _, capacity := range []uint32{0, 1025} {
		if _, err := bootstrapRequest(pipe, []debugSelection{{1, 1, 16}}, capacity); err == nil {
			t.Fatal("invalid capacity accepted")
		}
	}
}
