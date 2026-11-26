package desktop

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestViewerPreferencesRoundTripAndReplacement(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config", "viewer.json")
	p, err := loadPreferences(path)
	if err != nil || p != defaultPreferences() {
		t.Fatal("missing file should use defaults", p, err)
	}
	p = Preferences{Version: 1, ImagePath: `C:\Projetos\Cotação\app.exe`, ResultFilter: "Ação.Calcular", SymbolFilter: "Unidade.Ações", TimeUnit: Microseconds}
	if err := savePreferences(path, p); err != nil {
		t.Fatal(err)
	}
	got, err := loadPreferences(path)
	if err != nil || got != p {
		t.Fatal("Unicode preferences changed", got, err)
	}
	p.ResultFilter, p.TimeUnit, p.WholeUnit = "outro", ClockTicks, true
	if err := savePreferences(path, p); err != nil {
		t.Fatal("replacement failed", err)
	}
	got, err = loadPreferences(path)
	if err != nil || got != p {
		t.Fatal("replacement did not persist", got, err)
	}
	before, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	p.TimeUnit = "invalid"
	if err := savePreferences(path, p); err == nil {
		t.Fatal("invalid value overwrote valid preferences")
	}
	after, _ := os.ReadFile(path)
	if !bytes.Equal(before, after) {
		t.Fatal("failed save modified existing preferences")
	}
	leftovers, _ := filepath.Glob(filepath.Join(filepath.Dir(path), ".viewer-*.tmp"))
	if len(leftovers) != 0 {
		t.Fatal("save leaked temporary files", leftovers)
	}
}

func TestViewerPreferencesRejectInvalidOrOversizedInput(t *testing.T) {
	for _, data := range []string{
		`{}`, `{"version":2}`, `{"version":1,"time_unit":"cycles"}`,
		`{"version":1,"unexpected":1}`, `{"version":1} {}`,
		`{"version":1,"image_path":"x\u0000y"}`,
		`{"version":1,"result_filter":"x\ny"}`,
		`{"version":1,"symbol_filter":"` + strings.Repeat("x", 8193) + `"}`,
		string([]byte{0xff}), strings.Repeat(" ", maxPreferencesBytes+1),
	} {
		path := filepath.Join(t.TempDir(), "viewer.json")
		if err := os.WriteFile(path, []byte(data), 0600); err != nil {
			t.Fatal(err)
		}
		if _, err := loadPreferences(path); err == nil {
			t.Fatal("invalid preferences accepted", data[:min(len(data), 64)])
		}
	}
}

func TestViewerColumnPreferencesKeepWidthsAndPermutation(t *testing.T) {
	path := filepath.Join(t.TempDir(), "viewer.json")
	p := defaultPreferences()
	layout := &p.Columns[columnSamplingComparison]
	for i, width := range defaultColumnWidths[columnSamplingComparison] {
		layout.Widths[i], layout.Order[i] = width, int32(i)
	}
	layout.Widths[2] = 273
	layout.Hidden = 1 << 2
	layout.Order[1], layout.Order[2] = layout.Order[2], layout.Order[1]
	if err := savePreferences(path, p); err != nil {
		t.Fatal(err)
	}
	if got, err := loadPreferences(path); err != nil || got != p {
		t.Fatal("column widths or order changed", got, err)
	}
	layout.Order[1] = layout.Order[2]
	if err := savePreferences(path, p); err == nil {
		t.Fatal("duplicate column accepted")
	}
}

func TestColumnPreferencesRejectHiddenLastColumn(t *testing.T) {
	var layout columnLayout
	for i, width := range defaultColumnWidths[columnSampling] {
		layout.Widths[i], layout.Order[i] = width, int32(i)
	}
	columns := len(defaultColumnWidths[columnSampling])
	for _, hidden := range []uint32{columnMask(columns), 1 << columns} {
		layout.Hidden = hidden
		if layout.valid(columns) {
			t.Fatal("accepted unusable table", hidden)
		}
	}
}

func TestCatalogModeKeepsUnitSelectionSeparateAcrossSaveAndReopen(t *testing.T) {
	p := defaultPreferences()
	p.SymbolFilter = "Buscar.Procedimento"
	p.UnitSelection = "Unidade.A, Unidade.B"
	if query := p.switchCatalogMode(true, "Outro.Procedimento"); query != "Unidade.A, Unidade.B" {
		t.Fatal("lost selected units", query)
	}
	if query := p.switchCatalogMode(false, "Unidade.C, Unidade.D"); query != "Outro.Procedimento" {
		t.Fatal("lost procedure search", query)
	}
	path := filepath.Join(t.TempDir(), "viewer.json")
	if err := savePreferences(path, p); err != nil {
		t.Fatal(err)
	}
	loaded, err := loadPreferences(path)
	if err != nil || loaded != p {
		t.Fatal("catalog modes did not roundtrip", loaded, err)
	}
	if query := loaded.switchCatalogMode(true, loaded.catalogQuery()); query != "Unidade.C, Unidade.D" {
		t.Fatal("reopening forgot selected units", query)
	}
}

func TestLegacyUnitSelectionMigratesWithoutReplacingAnExplicitEmptySelection(t *testing.T) {
	path := filepath.Join(t.TempDir(), "viewer.json")
	data := []byte(`{"version":1,"whole_unit":true,"symbol_filter":"Unidade.A, Unidade.B","time_unit":"ms"}`)
	if err := os.WriteFile(path, data, 0600); err != nil {
		t.Fatal(err)
	}
	p, err := loadPreferences(path)
	if err != nil {
		t.Fatal(err)
	}
	if p.UnitSelection != "Unidade.A, Unidade.B" || p.SymbolFilter != "" || p.catalogQuery() != p.UnitSelection {
		t.Fatal("legacy selected units lost", p)
	}
	p.SymbolFilter = "Buscar.Procedimento"
	p.UnitSelection = ""
	if err := savePreferences(path, p); err != nil {
		t.Fatal(err)
	}
	loaded, err := loadPreferences(path)
	if err != nil || loaded != p || loaded.catalogQuery() != "" {
		t.Fatal("explicit empty selection was replaced by procedure search", loaded, err)
	}
}

func TestUnitSelectionPreferencesKeepTextLimits(t *testing.T) {
	for _, query := range []string{strings.Repeat("x", 8193), "Unidade\x00Outra", string([]byte{0xff})} {
		p := defaultPreferences()
		p.UnitSelection = query
		if err := savePreferences(filepath.Join(t.TempDir(), "viewer.json"), p); err == nil {
			t.Fatal("invalid unit selection accepted")
		}
	}
}
