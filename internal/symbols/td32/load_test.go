package td32

import (
	"bytes"
	"debug/pe"
	"encoding/binary"
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func peFixture(t *testing.T, wide bool, payload []byte) []byte {
	t.Helper()
	dirs := [16]pe.DataDirectory{}
	dirs[6] = pe.DataDirectory{VirtualAddress: 0x2000, Size: 28}
	var optional any = pe.OptionalHeader32{Magic: 0x10b, NumberOfRvaAndSizes: 16, DataDirectory: dirs}
	if wide {
		optional = pe.OptionalHeader64{Magic: 0x20b, NumberOfRvaAndSizes: 16, DataDirectory: dirs}
	}
	var b bytes.Buffer
	dos := make([]byte, 128)
	copy(dos, "MZ")
	put32(dos, 0x3c, 128)
	b.Write(dos)
	b.WriteString("PE\x00\x00")
	for _, v := range []any{pe.FileHeader{Machine: pe.IMAGE_FILE_MACHINE_AMD64, NumberOfSections: 1, SizeOfOptionalHeader: uint16(binary.Size(optional))}, optional, pe.SectionHeader32{VirtualAddress: 0x2000, VirtualSize: 28, SizeOfRawData: 28, PointerToRawData: 512}} {
		if err := binary.Write(&b, binary.LittleEndian, v); err != nil {
			t.Fatal(err)
		}
	}
	image := make([]byte, 540)
	copy(image, b.Bytes())
	put32(image, 528, uint32(len(payload)))
	put32(image, 536, 540)
	return append(image, payload...)
}
func TestLoadImage(t *testing.T) {
	for _, wide := range []bool{false, true} {
		dir := t.TempDir()
		path := filepath.Join(dir, "lab.exe")
		sidecar := filepath.Join(dir, "lab.tds")
		write := func(p string, b []byte) {
			t.Helper()
			if err := os.WriteFile(p, b, 0600); err != nil {
				t.Fatal(err)
			}
		}
		write(path, peFixture(t, wide, fixture("Embedded")))
		write(sidecar, fixture("External"))
		ps, source, err := Load(path)
		if err != nil || source != path || ps[0].Name != "Embedded" {
			t.Fatalf("%v %q %v", ps, source, err)
		}
		write(path, peFixture(t, wide, nil))
		ps, source, err = Load(path)
		if err != nil || source != sidecar || ps[0].Name != "External" {
			t.Fatalf("%v %q %v", ps, source, err)
		}
		if err := os.Remove(sidecar); err != nil {
			t.Fatal(err)
		}
		if _, _, err = Load(path); !errors.Is(err, ErrNoSymbols) {
			t.Fatal(err)
		}
		image := peFixture(t, wide, fixture("Bad"))
		put32(image, 536, 0xffffffff)
		write(path, image)
		if _, _, err = Load(path); !errors.Is(err, ErrInvalid) {
			t.Fatal(err)
		}
	}
}

func TestLoadSourcePriority(t *testing.T) {
	// CheckDebugData in the reference only falls back to TDS when embedded
	// debug information is invalid/absent. An invalid unused TDS must not win.
	for _, tc := range []struct {
		name                 string
		badImage, badSidecar bool
		wantSource           string
		wantErr              bool
	}{
		{"valid embedded ignores invalid sidecar", false, true, "lab.exe", false},
		{"invalid embedded uses valid sidecar", true, false, "lab.tds", false},
		{"both invalid return error", true, true, "", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			imageData, sidecarData := fixture("Embedded"), fixture("External")
			if tc.badImage {
				put32(imageData, len(imageData)-4, 0xffffffff)
			}
			if tc.badSidecar {
				put32(sidecarData, len(sidecarData)-4, 0xffffffff)
			}
			dir := t.TempDir()
			for name, data := range map[string][]byte{"lab.exe": peFixture(t, true, imageData), "lab.tds": sidecarData} {
				if err := os.WriteFile(filepath.Join(dir, name), data, 0600); err != nil {
					t.Fatal(err)
				}
			}
			ps, source, err := Load(filepath.Join(dir, "lab.exe"))
			if tc.wantErr {
				if !errors.Is(err, ErrInvalid) || ps != nil {
					t.Fatalf("%v %v", ps, err)
				}
				return
			}
			wantName := "Embedded"
			if tc.wantSource == "lab.tds" {
				wantName = "External"
			}
			if err != nil || filepath.Base(source) != tc.wantSource || len(ps) != 1 || ps[0].Name != wantName {
				t.Fatalf("%v %q %v", ps, source, err)
			}
		})
	}
}
