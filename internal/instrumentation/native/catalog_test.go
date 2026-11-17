package native

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"

	"github.com/gabrielteiga/AntiPessimizerV2/internal/testfixture"
)

func TestCatalogIDsStayStableAcrossFiltersAndSelection(t *testing.T) {
	path := filepath.Join(t.TempDir(), "owned.exe")
	if err := os.WriteFile(path, testfixture.Image(true), 0600); err != nil {
		t.Fatal(err)
	}
	catalog, err := ReadCatalog(path)
	if err != nil {
		t.Fatal(err)
	}
	match := catalog.Match(nil, []string{"lab.hot"})
	if len(match) != 1 || match[0].ID != catalog.Procedures[0].ID || match[0].RVA != 0x5020 {
		t.Fatal(match)
	}
	selected, err := catalog.Select([]uint32{match[0].ID})
	if err != nil || len(selected) != 1 || selected[0] != match[0] {
		t.Fatal(selected, err)
	}
	selected[0].Name = "changed"
	if catalog.Procedures[0].Name == "changed" {
		t.Fatal("selection aliases catalog")
	}
	if err = catalog.RequireImage(catalog.ImageID); err != nil {
		t.Fatal(err)
	}
	for _, bad := range []string{"", "old-build"} {
		if catalog.RequireImage(bad) == nil {
			t.Fatal("changed image accepted")
		}
	}
	for _, bad := range [][]uint32{nil, {0}, {1, 1}, {99}} {
		if _, err = catalog.Select(bad); err == nil {
			t.Fatal("invalid IDs accepted", bad)
		}
	}
}

func TestCatalogIdentityDetectsChangedExternalSymbolsForSameExecutable(t *testing.T) {
	directory := t.TempDir()
	path := filepath.Join(directory, "owned.exe")
	sidecar := filepath.Join(directory, "owned.tds")
	if err := os.WriteFile(path, testfixture.Image(false), 0600); err != nil {
		t.Fatal(err)
	}
	data := testfixture.TDS()
	if err := os.WriteFile(sidecar, data, 0600); err != nil {
		t.Fatal(err)
	}
	before, err := ReadCatalog(path)
	if err != nil {
		t.Fatal(err)
	}
	changed := bytes.Replace(data, []byte("Lab.Hot"), []byte("Lab.Dot"), 1)
	if bytes.Equal(data, changed) {
		t.Fatal("fixture name absent")
	}
	if err := os.WriteFile(sidecar, changed, 0600); err != nil {
		t.Fatal(err)
	}
	after, err := ReadCatalog(path)
	if err != nil {
		t.Fatal(err)
	}
	if before.ImageID != after.ImageID || before.CatalogID == after.CatalogID || before.Procedures[0].ID != after.Procedures[0].ID {
		t.Fatal("catalog cannot detect stale IDs from a changed TDS")
	}
}
