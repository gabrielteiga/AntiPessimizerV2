package main

import (
	"bytes"
	"debug/pe"
	"encoding/binary"
	"testing"
)

// fixture builds only headers needed by debug/pe, not a runnable Delphi binary.
func fixture(t *testing.T, wide bool) []byte {
	t.Helper()
	var optional any = pe.OptionalHeader32{Magic: 0x10b, ImageBase: 0x400000, SizeOfImage: 0x3000, NumberOfRvaAndSizes: 16}
	machine := uint16(pe.IMAGE_FILE_MACHINE_I386)
	if wide {
		optional = pe.OptionalHeader64{Magic: 0x20b, ImageBase: 0x140000000, SizeOfImage: 0x3000, NumberOfRvaAndSizes: 16}
		machine = pe.IMAGE_FILE_MACHINE_AMD64
	}
	var b bytes.Buffer
	dos := make([]byte, 128)
	copy(dos, "MZ")
	binary.LittleEndian.PutUint32(dos[0x3c:], 128)
	b.Write(dos)
	b.WriteString("PE\x00\x00")
	for _, v := range []any{
		pe.FileHeader{Machine: machine, SizeOfOptionalHeader: uint16(binary.Size(optional))}, optional,
	} {
		if err := binary.Write(&b, binary.LittleEndian, v); err != nil {
			t.Fatal(err)
		}
	}
	return b.Bytes()
}

func TestInspect(t *testing.T) {
	for _, tc := range []struct {
		name    string
		wide    bool
		format  string
		base    uint64
		machine uint16
	}{
		{"x64", true, "PE32+", 0x140000000, pe.IMAGE_FILE_MACHINE_AMD64},
		{"x86", false, "PE32", 0x400000, pe.IMAGE_FILE_MACHINE_I386},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, err := inspect(bytes.NewReader(fixture(t, tc.wide)))
			if err != nil {
				t.Fatal(err)
			}
			if got.Format != tc.format || got.Machine != tc.machine || got.PreferredImageBase != tc.base || got.ImageSize != 0x3000 {
				t.Fatalf("unexpected metadata: %+v", got)
			}
			if got.DebugDirectorySize != 0 {
				t.Fatal("invented debug information")
			}
		})
	}
}

func TestRejectInvalid(t *testing.T) {
	valid := fixture(t, true)
	for _, tc := range []struct {
		name string
		data []byte
	}{
		{"empty", nil}, {"text", []byte("not a PE")}, {"truncated", valid[:len(valid)-1]},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := inspect(bytes.NewReader(tc.data)); err == nil {
				t.Fatal("accepted invalid image")
			}
		})
	}
}

func TestUsage(t *testing.T) {
	if err := run(nil, &bytes.Buffer{}); err == nil {
		t.Fatal("accepted missing path")
	}
}

func TestDebugDirectory(t *testing.T) {
	data := fixture(t, true)
	// PE signature + COFF header + offset of DataDirectory in PE32+.
	directory := 128 + 4 + 20 + 112 + 8*pe.IMAGE_DIRECTORY_ENTRY_DEBUG
	binary.LittleEndian.PutUint32(data[directory:], 0x2000)
	binary.LittleEndian.PutUint32(data[directory+4:], 28)
	got, err := inspect(bytes.NewReader(data))
	if err != nil {
		t.Fatal(err)
	}
	if got.DebugDirectoryRVA != 0x2000 || got.DebugDirectorySize != 28 {
		t.Fatalf("debug directory not preserved: %+v", got)
	}
	// The probe inventories the directory declaration; it does not validate its payload.
}
