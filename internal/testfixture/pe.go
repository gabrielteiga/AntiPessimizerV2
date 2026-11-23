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

// PlannableImage has a valid header size and one supported sixteen-byte leaf
// (MOV EAX,42; NOPs; RET) at Lab.Hot. It remains a synthetic, non-runnable PE.
func PlannableImage() []byte {
	b := Image(true)
	binary.LittleEndian.PutUint32(b[128+24+60:], 0x200) // SizeOfHeaders
	copy(b[0x220:], []byte{0xb8, 42, 0, 0, 0})
	copy(b[0x225:], bytes.Repeat([]byte{0x90}, 10))
	b[0x22f] = 0xc3
	return b
}

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

// TDSWithAlias adds another name for Lab.Hot's exact code interval. The second
// procedure record is kept in file order to exercise JCL's first-match policy.
func TDSWithAlias(name string) []byte {
	original := TDS()
	b := append([]byte(nil), original[:96]...)
	record := append([]byte(nil), original[52:96]...)
	binary.LittleEndian.PutUint32(record[40:], 2)
	b = append(b, record...)
	namesAt := len(b)
	b = append(b, original[96:109]...)
	binary.LittleEndian.PutUint32(b[namesAt:], 2)
	b = append(b, byte(len(name)))
	b = append(b, name...)
	b = append(b, 0)
	namesSize := len(b) - namesAt
	b = append(b, original[109:]...)
	binary.LittleEndian.PutUint32(b[32:], 92)
	binary.LittleEndian.PutUint32(b[40:], uint32(namesAt))
	binary.LittleEndian.PutUint32(b[44:], uint32(namesSize))
	binary.LittleEndian.PutUint32(b[len(b)-4:], uint32(len(b)))
	return b
}

// TDSWithUnit keeps sstModule in directory 7 while allowing align symbols in a
// global or different directory. This is synthetic reference-format data.
func TDSWithUnit(unit string, symbolModule uint16) []byte {
	b := append([]byte(nil), TDS()[:109]...)
	put16 := func(at int, n uint16) { binary.LittleEndian.PutUint16(b[at:], n) }
	put32 := func(at int, n uint32) { binary.LittleEndian.PutUint32(b[at:], n) }
	module := len(b)
	b = append(b, make([]byte, 40)...)
	put16(module+4, 1)
	put32(module+8, 2)
	put16(module+28, 1)
	put16(module+30, 1)
	put32(module+32, 32)
	put32(module+36, 16)
	names := len(b)
	b = append(b, 1, 0, 0, 0, byte(len(unit)))
	b = append(b, unit...)
	b = append(b, 0)
	directory := len(b)
	b = append(b, make([]byte, 40)...)
	put16(directory, 16)
	put16(directory+2, 12)
	put32(directory+4, 2)
	put16(directory+16, 0x120)
	put16(directory+18, 7)
	put32(directory+20, uint32(module))
	put32(directory+24, 40)
	put16(directory+28, 0x130)
	put32(directory+32, uint32(names))
	put32(directory+36, uint32(6+len(unit)))
	put16(26, symbolModule)
	put32(16, uint32(directory))
	b = append(b, 'F', 'B', '0', '9', 0, 0, 0, 0)
	put32(len(b)-4, uint32(len(b)))
	return b
}
