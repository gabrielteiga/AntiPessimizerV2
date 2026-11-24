package symbols

import (
	"encoding/binary"
	"os"
	"path/filepath"
	"testing"

	"github.com/gabrielteiga/AntiPessimizerV2/internal/testfixture"
)

func TestLoadImageASLR(t *testing.T) {
	path := filepath.Join(t.TempDir(), "lab.exe")
	if err := os.WriteFile(path, testfixture.Image(true), 0600); err != nil {
		t.Fatal(err)
	}
	image, err := LoadImage(path)
	if err != nil {
		t.Fatal(err)
	}
	if image.ProcedureCount != 1 || image.Size != 0x7000 || image.SymbolSource != path {
		t.Fatalf("%+v", image)
	}
	for _, base := range []uint64{image.PreferredBase, 0x7ff000000000} {
		for _, tc := range []struct {
			rva    uint64
			status string
			inside bool
		}{
			{0x501f, "unknown", true}, {0x5020, "resolved", true}, {0x502f, "resolved", true}, {0x5030, "unknown", true}, {0x7000, "", false},
		} {
			loc, ok := image.Index.Resolve(base, base+tc.rva)
			if ok != tc.inside || loc.Status != tc.status {
				t.Fatalf("RVA %x: %+v %v", tc.rva, loc, ok)
			}
			if ok && loc.ModuleID != image.ID {
				t.Fatal("identity changed")
			}
			if tc.status == "resolved" && (loc.Procedure != "Lab.Hot" || loc.RVA != "0x0000000000005020") {
				t.Fatal(loc)
			}
		}
	}
}

func TestImageSymbolSources(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "lab.exe")
	data := testfixture.Image(false)
	first, err := loadImage(data, path, false)
	if err != nil {
		t.Fatal(err)
	}
	if first.ProcedureCount != 0 || first.SymbolSource != "" {
		t.Fatal(first)
	}
	loc, ok := first.Index.Resolve(0x100000, 0x105020)
	if !ok || loc.Status != "unknown" {
		t.Fatal(loc)
	}
	if err := os.WriteFile(filepath.Join(dir, "lab.tds"), testfixture.TDS(), 0600); err != nil {
		t.Fatal(err)
	}
	second, err := loadImage(data, path, false)
	if err != nil {
		t.Fatal(err)
	}
	if second.ID != first.ID || second.ProcedureCount != 1 || second.SymbolSource != filepath.Join(dir, "lab.tds") {
		t.Fatal(second)
	}
	data[0x220] = 0xcc
	third, err := loadImage(data, path, false)
	if err != nil {
		t.Fatal(err)
	}
	if third.ID == first.ID {
		t.Fatal("different build retained identity")
	}
}

func TestRejectUnsupportedImageMappings(t *testing.T) {
	const optional = 128 + 4 + 20
	const section = optional + 240
	for name, mutate := range map[string]func([]byte){
		"different BaseOfCode":   func(b []byte) { binary.LittleEndian.PutUint32(b[optional+20:], 0x1000) },
		"non executable":         func(b []byte) { binary.LittleEndian.PutUint32(b[section+36:], peCodeRead) },
		"section outside image":  func(b []byte) { binary.LittleEndian.PutUint32(b[section+8:], 0x3000) },
		"procedure outside code": func(b []byte) { binary.LittleEndian.PutUint32(b[0x400+80:], 0x1ff) },
		"unknown segment":        func(b []byte) { binary.LittleEndian.PutUint16(b[0x400+84:], 2) },
		"corrupt symbols":        func(b []byte) { binary.LittleEndian.PutUint32(b[len(b)-4:], 0xffffffff) },
	} {
		t.Run(name, func(t *testing.T) {
			b := testfixture.Image(true)
			mutate(b)
			if _, err := loadImage(b, filepath.Join(t.TempDir(), "lab.exe"), false); err == nil {
				t.Fatal("accepted invalid mapping")
			}
		})
	}
}

const peCodeRead = 0x40000020

func TestPE32Mapping(t *testing.T) {
	image, err := loadImage(testfixture.Image32(true), filepath.Join(t.TempDir(), "lab.exe"), false)
	if err != nil {
		t.Fatal(err)
	}
	loc, ok := image.Index.Resolve(0x800000, 0x805020)
	if !ok || loc.Procedure != "Lab.Hot" || image.PreferredBase != 0x400000 {
		t.Fatalf("%+v %+v", image, loc)
	}
}

func TestPrimaryCodeSectionInImageWithAdditionalCode(t *testing.T) {
	b := append(testfixture.Image(false), make([]byte, 0x200)...)
	binary.LittleEndian.PutUint16(b[128+4+2:], 2)
	const second = 128 + 4 + 20 + 240 + 40
	copy(b[second:second+40], b[second-40:second])
	copy(b[second:], ".apcode\x00")
	binary.LittleEndian.PutUint32(b[second+12:], 0x6000)
	binary.LittleEndian.PutUint32(b[second+20:], 0x400)
	path := filepath.Join(t.TempDir(), "lab.exe")
	sidecar := filepath.Join(filepath.Dir(path), "lab.tds")
	if err := os.WriteFile(sidecar, testfixture.TDSWithSource(), 0600); err != nil {
		t.Fatal(err)
	}
	image, err := loadImage(b, path, true)
	if err != nil || image.ProcedureCount != 1 {
		t.Fatal("additional code prevented primary mapping", image, err)
	}
	const base = 0x7ff000000000
	primary, ok := image.Index.Resolve(base, base+0x5020)
	if !ok || primary.Procedure != "Lab.Hot" {
		t.Fatal(primary)
	}
	if line, ok := image.SourceAt(base, base+0x5028); !ok || line.Line != 109 {
		t.Fatal("primary source mapping changed", line, ok)
	}
	other, ok := image.Index.Resolve(base, base+0x6020)
	if !ok || other.Status != "unknown" {
		t.Fatal("additional code inherited TD32 procedure", other)
	}
	if _, ok := image.SourceAt(base, base+0x6028); ok {
		t.Fatal("additional code inherited TD32 source")
	}
	for name, change := range map[string]func([]byte){
		"duplicate base":  func(b []byte) { binary.LittleEndian.PutUint32(b[second+12:], 0x5000) },
		"virtual overlap": func(b []byte) { binary.LittleEndian.PutUint32(b[second+12:], 0x5100) },
		"raw overlap":     func(b []byte) { binary.LittleEndian.PutUint32(b[second+20:], 0x200) },
	} {
		t.Run(name, func(t *testing.T) {
			broken := append([]byte(nil), b...)
			change(broken)
			if _, err := loadImage(broken, path, true); err == nil {
				t.Fatal("ambiguous primary section accepted")
			}
		})
	}
	data := testfixture.TDS()
	binary.LittleEndian.PutUint16(data[84:], 2)
	if err := os.WriteFile(sidecar, data, 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := loadImage(b, path, false); err == nil {
		t.Fatal("inferred segment 2 from a second PE section")
	}
}

func FuzzLoadImage(f *testing.F) {
	f.Add(testfixture.Image(true))
	f.Add(testfixture.Image32(true))
	f.Add(testfixture.Image(false))
	f.Add([]byte{})
	path := filepath.Join(f.TempDir(), "lab.exe")
	f.Fuzz(func(t *testing.T, b []byte) {
		image, err := loadImage(b, path, false)
		if err == nil {
			loc, ok := image.Index.Resolve(0, 0)
			if !ok {
				t.Fatal("valid image did not contain RVA zero")
			}
			if err := loc.Validate(); err != nil {
				t.Fatal(err)
			}
		}
	})
}
