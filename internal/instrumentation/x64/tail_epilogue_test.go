package x64

import (
	"bytes"
	"encoding/binary"
	"errors"
	"testing"
)

func TestTailEpilogueMatchesAllocationPopOrderAndFrameBias(t *testing.T) {
	cases := []struct {
		name       string
		code, info []byte
		tail       int
	}{
		{"stack and RBX", []byte{0x53, 0x48, 0x83, 0xec, 0x20, 0x48, 0x89, 0xcb, 0x48, 0x83, 0xc4, 0x20, 0x5b, 0xe9, 0, 0, 0, 0}, []byte{1, 5, 2, 0, 5, 0x32, 1, 0x30}, 13},
		{"biased RBP", []byte{0x55, 0x48, 0x83, 0xec, 0x28, 0x48, 0x8d, 0x6c, 0x24, 0x20, 0x90, 0x48, 0x8d, 0x65, 8, 0x5d, 0xe9, 0, 0, 0, 0}, []byte{1, 10, 3, 0x25, 10, 3, 5, 0x42, 1, 0x50, 0, 0}, 16},
		{"two pops", []byte{0x53, 0x55, 0x90, 0x5d, 0x5b, 0xe9, 0, 0, 0, 0}, []byte{1, 2, 2, 0, 2, 0x50, 1, 0x30}, 5},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if err := validatePrologueActions(c.code, c.info); err != nil {
				t.Fatal(err)
			}
			if !verifiedTailEpilogues(c.code, c.info)[c.tail] {
				t.Fatal("verified epilogue rejected")
			}
			image := scalarImage(c.code)
			copy(image[0x480:], c.info)
			binary.LittleEndian.PutUint32(image[0x414:], 0x20c0)
			image[0x4c0] = 1
			binary.LittleEndian.PutUint32(image[0x220+c.tail+1:], 0x1080-(peLeafRVA+uint32(len(c.code))))
			p, err := NewPEImage(image)
			if err != nil {
				t.Fatal(err)
			}
			plan, err := p.BuildSelectedFunction(peLeafRVA, uint32(len(c.code)), 0x100000, 0x180000, 0x300000, 0x400000, 0x500000, 1, []uint32{0x1080})
			if err != nil || len(plan.TailTargets) != 1 || !bytes.Equal(plan.Image[plan.BodyRVA:plan.BodyRVA+uint32(c.tail)], c.code[:c.tail]) {
				t.Fatal("framed tail altered restoration", err)
			}
			broken := append([]byte(nil), c.code...)
			broken[c.tail-1] = 0x90
			if verifiedTailEpilogues(broken, c.info)[c.tail] {
				t.Fatal("missing register restoration accepted")
			}
		})
	}
}
func TestTailEpilogueRejectsWrongAllocationPopOrderAndBypassedRestoration(t *testing.T) {
	info := []byte{1, 5, 2, 0, 5, 0x32, 1, 0x30}
	for _, suffix := range [][]byte{
		{0x48, 0x83, 0xc4, 0x18, 0x5b, 0xe9, 0, 0, 0, 0},          // wrong allocation
		{0x48, 0x83, 0xc4, 0x20, 0x5d, 0xe9, 0, 0, 0, 0},          // wrong register
		{0x74, 5, 0x48, 0x83, 0xc4, 0x20, 0x5b, 0xe9, 0, 0, 0, 0}, // branch directly to transfer
	} {
		code := append([]byte{0x53, 0x48, 0x83, 0xec, 0x20}, suffix...)
		if verifiedTailEpilogues(code, info)[len(code)-5] {
			t.Fatal("unverified restoration accepted", suffix)
		}
	}
}

func TestTailEpilogueStillRejectsOriginalCleanupAndInterleavedFrames(t *testing.T) {
	image, code := cleanupPE()
	code = append(code[:len(code)-1], 0xe9, 0, 0, 0, 0)
	binary.LittleEndian.PutUint32(code[len(code)-4:], 0x1080-(peLeafRVA+uint32(len(code))))
	copy(image[0x220:], code)
	binary.LittleEndian.PutUint32(image[0x404:], peLeafRVA+uint32(len(code)))
	p, err := NewPEImage(image)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := p.BuildSelectedFunction(peLeafRVA, uint32(len(code)), 0x100000, 0x180000, 0x300000, 0x400000, 0x500000, 1, []uint32{0x1080}); !errors.Is(err, ErrUnsupported) {
		t.Fatal("tail bypassed original cleanup ownership", err)
	}
	info := []byte{1, 6, 3, 0, 6, 0x50, 5, 0x32, 1, 0x30, 0, 0}
	code = []byte{0x53, 0x48, 0x83, 0xec, 0x20, 0x55, 0x90, 0x5d, 0x48, 0x83, 0xc4, 0x20, 0x5b, 0xe9, 0, 0, 0, 0}
	if len(verifiedTailEpilogues(code, info)) != 0 {
		t.Fatal("interleaved allocation/push requires independent evidence")
	}
}
