package x64

import (
	"bytes"
	"encoding/binary"
	"errors"
	"testing"
)

const testTableImageBase = 0x140000000

func indexedTablePE() ([]byte, []byte) {
	code := []byte{
		0x48, 0x83, 0xf9, 1, // CMP RCX,1
		0x77, 37, // JA default
		0x4c, 0x8d, 0x1d, 4, 0, 0, 0, // LEA R11,table
		0x41, 0xff, 0x24, 0xcb, // JMP [R11+RCX*8]
	}
	put := binary.LittleEndian.PutUint64
	code = append(code, make([]byte, 16)...)
	put(code[17:], testTableImageBase+peLeafRVA+33)
	put(code[25:], testTableImageBase+peLeafRVA+39)
	code = append(code, 0xb8, 11, 0, 0, 0, 0xc3, 0x48, 0x8b, 2, 0xc3, 0xb8, 17, 0, 0, 0, 0xc3)
	image := scalarImage(code)
	put(image[peOptional+24:], testTableImageBase)
	binary.LittleEndian.PutUint32(image[peOptional+152:], 0x20f0)
	binary.LittleEndian.PutUint32(image[peOptional+156:], 12)
	binary.LittleEndian.PutUint32(image[0x4f0:], 0x1000)
	binary.LittleEndian.PutUint32(image[0x4f4:], 12)
	binary.LittleEndian.PutUint16(image[0x4f8:], 0xa000|(peLeafRVA+17)&0xfff)
	binary.LittleEndian.PutUint16(image[0x4fa:], 0xa000|(peLeafRVA+25)&0xfff)
	return image, code
}

func TestIndexedJumpTableUsesLiveSourceAndRelocatedTargets(t *testing.T) {
	image, code := indexedTablePE()
	before := bytes.Clone(image)
	const loaded, clone = 0x150000000, 0x150180000
	plan, err := BuildPEFunction(image, peLeafRVA, uint32(len(code)), loaded, clone, 0x150300000, 0x150400000, 0x150500000, 1)
	if err != nil {
		t.Fatal(err)
	}
	body := plan.Image[plan.BodyRVA:plan.Functions[1].End]
	u64 := binary.LittleEndian.Uint64
	if !bytes.Equal(body[10:21], code[6:17]) || u64(body[21:]) != clone+uint64(plan.BodyRVA)+37 || u64(body[29:]) != clone+uint64(plan.BodyRVA)+56 {
		t.Fatal("pointer table does not target the relocated cases", body)
	}
	if !bytes.Equal(image, before) || !bytes.Equal(plan.Source[:17], code[:17]) || !bytes.Equal(plan.Source[33:], code[33:]) ||
		u64(plan.Source[17:]) != loaded+peLeafRVA+33 || u64(plan.Source[25:]) != loaded+peLeafRVA+39 {
		t.Fatal("source verification lost Windows DIR64 relocation", plan.Source)
	}
}

func TestIndexedJumpTableRejectsUncheckedIndicesPointersAndBranches(t *testing.T) {
	cases := map[string]func([]byte){
		"signed bounds":             func(b []byte) { b[0x224] = 0x7f },
		"narrow index":              func(b []byte) { b[0x220] = 0x40 },
		"fallback points into data": func(b []byte) { b[0x225] = 11 },
		"pointer into instruction":  func(b []byte) { b[0x231]++ },
		"pointer relocation absent": func(b []byte) { b[0x4fb] = 0 },
		"duplicate relocation":      func(b []byte) { copy(b[0x4fa:], b[0x4f8:0x4fa]) },
		"dispatch index differs":    func(b []byte) { b[0x230] = 0xd3 },
		"case reads modified R11":   func(b []byte) { copy(b[0x247:], []byte{0x49, 0x8b, 3}) },
		"branch back into table":    func(b []byte) { copy(b[0x247:], []byte{0xeb, 0xe8, 0x90}) },
	}
	for name, mutate := range cases {
		t.Run(name, func(t *testing.T) {
			image, code := indexedTablePE()
			mutate(image)
			if _, err := BuildPEFunction(image, peLeafRVA, uint32(len(code)), 0x150000000, 0x150180000, 0x150300000, 0x150400000, 0x150500000, 1); !errors.Is(err, ErrUnsupported) {
				t.Fatal("unchecked table accepted", err)
			}
		})
	}
}
