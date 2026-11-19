package native

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

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

func TestFullProcedureSelectionHas256DistinctPhysicalRanges(t *testing.T) {
	var catalog Catalog
	var ids []uint32
	for id := uint32(1); id <= MaxSelectedProcedures; id++ {
		catalog.Procedures = append(catalog.Procedures, SelectedProcedure{ID: id, RVA: 0x1000 + id*16, Size: 6, Name: fmt.Sprintf("Batch%d", id)})
		ids = append(ids, id)
	}
	selected, err := catalog.Select(ids)
	if err != nil || len(selected) != MaxSelectedProcedures {
		t.Fatal(selected, err)
	}
	options := LaunchOptions{Image: "owned.exe", DLL: "adapter.dll", Procedures: selected, Capacity: 1024, Timeout: time.Minute}
	if err := options.validate(); err != nil {
		t.Fatal("full selection rejected before startup", err)
	}
	if _, err := catalog.Select(append(ids, MaxSelectedProcedures+1)); err == nil {
		t.Fatal("selection exceeds 256 procedures")
	}
	options.Procedures = append(options.Procedures, SelectedProcedure{ID: MaxSelectedProcedures + 1, RVA: 0x3000, Size: 6, Name: "TooMany"})
	if err := options.validate(); err == nil {
		t.Fatal("native launch exceeds capacity")
	}
}

func TestCatalogAliasesShareIDSelectionAndBuildIdentity(t *testing.T) {
	directory := t.TempDir()
	path := filepath.Join(directory, "owned.exe")
	sidecar := filepath.Join(directory, "owned.tds")
	if err := os.WriteFile(path, testfixture.Image(false), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(sidecar, testfixture.TDSWithAlias("Lab.Alias"), 0600); err != nil {
		t.Fatal(err)
	}
	catalog, err := ReadCatalog(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(catalog.Procedures) != 1 || len(catalog.Aliases) != 1 || catalog.Aliases[0].ID != catalog.Procedures[0].ID {
		t.Fatal("alias got a second instrumentation ID", catalog)
	}
	for _, names := range [][]string{{"lab.alias"}, {"lab.hot", "Lab.Alias"}} {
		selected := catalog.Match(nil, names)
		if len(selected) != 1 || selected[0] != catalog.Procedures[0] {
			t.Fatal("alias selection duplicates or renames physical procedure", selected)
		}
	}
	if found := catalog.Search("ALIAS", ""); len(found) != 1 || found[0] != catalog.Procedures[0] {
		t.Fatal("alias search absent", found)
	}
	if err := os.WriteFile(sidecar, testfixture.TDSWithAlias("Lab.Other"), 0600); err != nil {
		t.Fatal(err)
	}
	after, err := ReadCatalog(path)
	if err != nil || after.ImageID != catalog.ImageID || after.CatalogID == catalog.CatalogID || after.Procedures[0] != catalog.Procedures[0] {
		t.Fatal("alias change did not invalidate symbol selection", after, err)
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

func TestUnitSelectionUsesCompleteCatalogAndPhysicalAliases(t *testing.T) {
	catalog := Catalog{Procedures: []SelectedProcedure{{ID: 1, Name: "A.One", Unit: "A"}, {ID: 2, Name: "B.Two", Unit: "B"}, {ID: 3, Name: "A.Three", Unit: "A"}}, Aliases: []ProcedureAlias{{ID: 1, Name: "B.OneAlias", Unit: "B"}, {ID: 1, Name: "Other", Unit: "B"}}}
	selected, err := catalog.SelectUnits([]string{"b", "A"})
	if err != nil || len(selected) != 3 || selected[0].ID != 1 || selected[1].ID != 2 || selected[2].ID != 3 {
		t.Fatal(selected, err)
	}
	selected[0].Name = "changed"
	if catalog.Procedures[0].Name == "changed" {
		t.Fatal("selection aliases catalog")
	}
	for _, units := range [][]string{nil, {""}, {" "}, {"A", "a"}, {"A", "missing"}, {"A\n"}, {string([]byte{255})}} {
		if _, err := catalog.SelectUnits(units); err == nil {
			t.Fatal("invalid unit selection", units)
		}
	}
	catalog = Catalog{}
	for i := uint32(1); i <= MaxSelectedProcedures+1; i++ {
		catalog.Procedures = append(catalog.Procedures, SelectedProcedure{ID: i, Unit: "Large"})
	}
	if _, err := catalog.SelectUnits([]string{"Large"}); err == nil {
		t.Fatal("unit selection silently truncated")
	}
	catalog.Procedures = catalog.Procedures[:MaxSelectedProcedures]
	if selected, err := catalog.SelectUnits([]string{"Large"}); err != nil || len(selected) != MaxSelectedProcedures {
		t.Fatal("full unit omitted procedures", len(selected), err)
	}
}
