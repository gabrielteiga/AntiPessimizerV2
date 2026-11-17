package x64

import (
	"bytes"
	"encoding/binary"
	"errors"
	"testing"
)

func TestEntryJumpSignedDisplacementsAndAbsoluteFallback(t *testing.T) {
	source := uint64(0x400000000)
	for _, delta := range []int64{-1 << 31, -1, 0, 1, 1<<31 - 1} {
		target := uint64(int64(source+5) + delta)
		code := entryJump(source, target)
		if len(code) != 5 || code[0] != 0xe9 || int64(source+5)+int64(int32(binary.LittleEndian.Uint32(code[1:]))) != int64(target) {
			t.Fatal("near target changed", delta, code)
		}
	}
	for _, delta := range []int64{-1<<31 - 1, 1 << 31} {
		target := uint64(int64(source+5) + delta)
		code := entryJump(source, target)
		if !bytes.Equal(code, jump(target)) {
			t.Fatal("out-of-range target truncated", delta)
		}
	}
}
func TestTinyPEFunctionNeverBorrowsNeighboringBytes(t *testing.T) {
	image := leafPE()
	code := []byte{0xb8, 77, 0, 0, 0, 0xc3}
	copy(image[0x220:], code)
	image[0x226] = 0xcc
	binary.LittleEndian.PutUint32(image[0x404:], peLeafRVA+uint32(len(code)))
	before := append([]byte(nil), image...)
	p, err := BuildPEFunction(image, peLeafRVA, uint32(len(code)), 0x100000, 0x180000, 0x300000, 0x400000, 0x500000, 1)
	if err != nil {
		t.Fatal(err)
	}
	if len(p.Patch) != 5 || len(p.Original) != 5 || !bytes.Equal(p.Source, code) || !bytes.Equal(before, image) {
		t.Fatal("small method consumed neighboring code", p)
	}
	if !bytes.Equal(p.Image[p.BodyRVA:p.BodyRVA+5], code[:5]) || !bytes.Equal(p.Image[p.BodyRVA+5:p.BodyRVA+19], jump(0x180000+uint64(p.ExitRVA))) {
		t.Fatal("tiny method return bypasses exit probe")
	}
	if _, err = BuildPEFunction(image, peLeafRVA, 6, 0x100000, 0x100000000, 0x300000, 0x400000, 0x500000, 1); !errors.Is(err, ErrUnsupported) {
		t.Fatal("tiny method accepted far 14-byte patch", err)
	}
	if _, err = BuildPEFunction(image, peLeafRVA, 4, 0x100000, 0x180000, 0x300000, 0x400000, 0x500000, 1); !errors.Is(err, ErrUnsupported) {
		t.Fatal("sub-five-byte method accepted", err)
	}
}
