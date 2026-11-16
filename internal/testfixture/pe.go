// Package testfixture builds synthetic wire-format inputs for integration tests.
// These files are not runnable Delphi executables.
package testfixture

import (
	"bytes"
	"debug/pe"
	"encoding/binary"
)

// Image produces an AMD64 PE with a single executable section at RVA 0x5000.
// With symbols it contains Lab.Hot at offset 0x20, size 0x10, TD32 segment 1.
func Image(symbols bool) []byte { return image(symbols, true) }

// Image32 is the I386 equivalent of Image.
func Image32(symbols bool) []byte { return image(symbols, false) }

func image(symbols, wide bool) []byte {
	var b bytes.Buffer
	dos := make([]byte, 128)
	copy(dos, "MZ")
	binary.LittleEndian.PutUint32(dos[0x3c:], 128)
	b.Write(dos)
	b.WriteString("PE\x00\x00")
	h := pe.OptionalHeader64{Magic: 0x20b, ImageBase: 0x140000000, BaseOfCode: 0x5000, SizeOfImage: 0x7000, NumberOfRvaAndSizes: 16}
	if symbols {
		h.DataDirectory[6] = pe.DataDirectory{VirtualAddress: 0x5100, Size: 28}
	}
	var optional any = h
	machine := uint16(pe.IMAGE_FILE_MACHINE_AMD64)
	if !wide {
		optional = pe.OptionalHeader32{Magic: 0x10b, ImageBase: 0x400000, BaseOfCode: h.BaseOfCode, SizeOfImage: h.SizeOfImage, NumberOfRvaAndSizes: 16, DataDirectory: h.DataDirectory}
		machine = pe.IMAGE_FILE_MACHINE_I386
	}
	for _, v := range []any{
		pe.FileHeader{Machine: machine, NumberOfSections: 1, SizeOfOptionalHeader: uint16(binary.Size(optional))}, optional,
		pe.SectionHeader32{Name: [8]byte{'.', 't', 'e', 'x', 't'}, VirtualSize: 0x200, VirtualAddress: 0x5000, SizeOfRawData: 0x200, PointerToRawData: 0x200, Characteristics: pe.IMAGE_SCN_CNT_CODE | pe.IMAGE_SCN_MEM_EXECUTE | pe.IMAGE_SCN_MEM_READ},
	} {
		if err := binary.Write(&b, binary.LittleEndian, v); err != nil {
			panic(err)
		}
	}
	image := make([]byte, 0x400)
	copy(image, b.Bytes())
	if symbols {
		payload := TDS()
		binary.LittleEndian.PutUint32(image[0x310:], uint32(len(payload)))
		binary.LittleEndian.PutUint32(image[0x318:], 0x400)
		image = append(image, payload...)
	}
	return image
}

func TDS() []byte {
	b := make([]byte, 117)
	p16 := func(n int, v uint16) { binary.LittleEndian.PutUint16(b[n:], v) }
	p32 := func(n int, v uint32) { binary.LittleEndian.PutUint32(b[n:], v) }
	copy(b, "FB09")
	p32(4, 8)
	p16(8, 16)
	p16(10, 12)
	p32(12, 2)
	p16(24, 0x125)
	p32(28, 48)
	p32(32, 48)
	p16(36, 0x130)
	p32(40, 96)
	p32(44, 13)
	p32(48, 1)
	p16(52, 42)
	p16(54, 0x205)
	p32(68, 16)
	p32(80, 32)
	p16(84, 1)
	p32(92, 1)
	p32(96, 1)
	b[100] = 7
	copy(b[101:], "Lab.Hot")
	copy(b[109:], "FB09")
	p32(113, uint32(len(b)))
	return b
}
