package native

import (
	"bytes"
	"encoding/binary"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gabrielteiga/AntiPessimizerV2/internal/testfixture"
)

func TestStaticPreflightUsesPlannerWithoutMutatingImage(t *testing.T) {
	selected := []SelectedProcedure{{ID: 17, RVA: 0x5020, Size: 16, Name: "Lab.Hot"}}
	for _, tc := range []struct {
		name, reason string
		mutate       func([]byte)
	}{
		{"supported leaf", "", func([]byte) {}},
		{"unsupported instruction", "unsupported operation: UD2", func(b []byte) { b[0x220] = 0x0f; b[0x221] = 0x0b }},
		{"Delphi language handler", "language handler", func(b []byte) {
			binary.LittleEndian.PutUint32(b[128+24+136:], 0x5080)
			binary.LittleEndian.PutUint32(b[128+24+140:], 12)
			binary.LittleEndian.PutUint32(b[0x280:], 0x5020)
			binary.LittleEndian.PutUint32(b[0x284:], 0x5030)
			binary.LittleEndian.PutUint32(b[0x288:], 0x50a0)
			b[0x2a0] = 9 // version 1 with UNW_FLAG_EHANDLER; never accepted
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			data := testfixture.PlannableImage()
			tc.mutate(data)
			before := append([]byte(nil), data...)
			checks, err := CheckImageProcedures(data, selected)
			if err != nil || len(checks) != 1 || checks[0].ID != 17 {
				t.Fatal(checks, err)
			}
			if tc.reason == "" {
				if !checks[0].PlanSupported || checks[0].Reason != "" || RequireSupported(checks) != nil {
					t.Fatal("supported function rejected", checks)
				}
			} else if checks[0].PlanSupported || !strings.Contains(checks[0].Reason, tc.reason) || RequireSupported(checks) == nil {
				t.Fatal("unsupported function not explained", checks)
			}
			if !bytes.Equal(data, before) {
				t.Fatal("preflight changed executable bytes")
			}
		})
	}
	if _, err := CheckImageProcedures(testfixture.Image32(true), selected); err == nil {
		t.Fatal("32-bit image accepted")
	}
	checks, err := CheckImageProcedures(testfixture.PlannableImage(), []SelectedProcedure{{ID: 9, RVA: 0x5020, Size: 4}})
	if err != nil || len(checks) != 1 || checks[0].PlanSupported || !strings.Contains(checks[0].Reason, "5..4096") {
		t.Fatal("size limit missing", checks, err)
	}
}

func TestCatalogPreflightRejectsChangedBuildAndSelection(t *testing.T) {
	path := filepath.Join(t.TempDir(), "owned.exe")
	data := testfixture.PlannableImage()
	if err := os.WriteFile(path, data, 0600); err != nil {
		t.Fatal(err)
	}
	catalog, err := ReadCatalog(path)
	if err != nil {
		t.Fatal(err)
	}
	checks, err := catalog.Check(catalog.Procedures)
	if err != nil || len(checks) != 1 || !checks[0].PlanSupported {
		t.Fatal(checks, err)
	}
	wrong := append([]SelectedProcedure(nil), catalog.Procedures...)
	wrong[0].RVA++
	if _, err := catalog.Check(wrong); err == nil {
		t.Fatal("selection changed after catalog loading")
	}
	data[0x221]++
	if err := os.WriteFile(path, data, 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := catalog.Check(catalog.Procedures); err == nil || !strings.Contains(err.Error(), "changed") {
		t.Fatal("stale image planned", err)
	}
}
