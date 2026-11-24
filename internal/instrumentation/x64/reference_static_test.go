package x64

import (
	"bytes"
	"crypto/sha256"
	"debug/pe"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"testing"
)

const referenceDLLSHA = "1c1bf9288a3bfd05303f2b9a86d29693c0a449e716f575a25a59059f1c0d02b5"

// This scans static eligibility only. It never calls the accepted procedures
// and must not be reported as execution coverage, compiler certification or a
// percentage of functional parity.
func TestReferenceDelphiStaticPlanCoverage(t *testing.T) {
	path := os.Getenv("AP_REFERENCE_DELPHI_DLL")
	if path == "" {
		t.Skip("pinned original release is downloaded separately")
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if fmt.Sprintf("%x", sha256.Sum256(data)) != referenceDLLSHA {
		t.Fatal("original reference hash changed")
	}
	file, err := pe.NewFile(bytes.NewReader(data))
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	directory := file.OptionalHeader.(*pe.OptionalHeader64).DataDirectory[3]
	var table []byte
	for _, section := range file.Sections {
		if directory.VirtualAddress >= section.VirtualAddress && uint64(directory.VirtualAddress-section.VirtualAddress)+uint64(directory.Size) <= uint64(section.Size) {
			raw, err := section.Data()
			if err != nil {
				t.Fatal(err)
			}
			at := directory.VirtualAddress - section.VirtualAddress
			table = raw[at : at+directory.Size]
		}
	}
	if len(table) == 0 || len(table)%12 != 0 {
		t.Fatal("original runtime table missing")
	}
	image, err := NewPEImage(data)
	if err != nil {
		t.Fatal(err)
	}
	const loaded = uint64(0x100000000)
	allocation := loaded + ((uint64(image.ImageSize()) + 0xffff) &^ 0xffff) + 0x10000
	evidence := struct {
		Version  int            `json:"version"`
		ImageSHA string         `json:"image_sha256"`
		Total    int            `json:"runtime_functions"`
		Accepted int            `json:"static_plans_accepted"`
		Delphi   int            `json:"delphi_scopes_accepted"`
		Rejected map[string]int `json:"rejected"`
	}{Version: 1, ImageSHA: referenceDLLSHA, Total: len(table) / 12, Rejected: map[string]int{}}
	for at := 0; at < len(table); at += 12 {
		rva, end := binary.LittleEndian.Uint32(table[at:]), binary.LittleEndian.Uint32(table[at+4:])
		plan, err := image.BuildFunction(rva, end-rva, loaded, allocation, allocation+0x10000000, allocation+0x10000100, allocation+0x10000200, uint32(at/12+1))
		if err != nil {
			evidence.Rejected[err.Error()]++
			continue
		}
		evidence.Accepted++
		if plan.HandlerScopeStyle >= 2 {
			evidence.Delphi++
		}
	}
	if evidence.Total != 8777 || evidence.Accepted != 8653 || evidence.Delphi != 523 {
		t.Fatal("static eligibility changed; review the actual corpus before changing expectations", evidence)
	}
	if output := os.Getenv("AP_REFERENCE_DELPHI_REPORT"); output != "" {
		b, err := json.MarshalIndent(evidence, "", "  ")
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(filepath.Dir(output), "reference-delphi-static.json"), append(b, '\n'), 0600); err != nil {
			t.Fatal(err)
		}
	}
	t.Logf("static only: %d/%d plans, including %d reviewed Delphi scope methods; no runtime parity percentage", evidence.Accepted, evidence.Total, evidence.Delphi)
}
