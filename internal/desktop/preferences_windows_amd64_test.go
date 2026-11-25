package desktop

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"unsafe"

	"github.com/gabrielteiga/AntiPessimizerV2/internal/report"
)

func TestNativePreferencesWindowSmoke(t *testing.T) {
	if os.Getenv("AP_GUI_SMOKE") != "1" {
		t.Skip("native preferences smoke enabled in Windows CI")
	}
	path := filepath.Join(t.TempDir(), "viewer.json")
	p := Preferences{ExcludeRuntime: true, WholeUnit: true, Version: 1, ImagePath: `C:\Projetos\Cotação\app.exe`, ResultFilter: "Ação.Calcular", SymbolFilter: "Unidade.Ações, Unidade.Relatórios", TimeUnit: ClockTicks}
	if err := savePreferences(path, p); err != nil {
		t.Fatal(err)
	}
	if err := runViewerWithPreferences(`\\.\pipe\unused-preferences`, "", "", true, func(w *window) error {
		unit, _, _ := send.Call(w.metricUnits, 0x147, 0, 0)
		whole, _, _ := send.Call(w.wholeUnit, 0xf0, 0, 0)
		if whole != 1 || !w.excludesRuntime() || value(w.imagePath) != p.ImagePath || value(w.filter) != p.ResultFilter || value(w.symbolSearch) != p.SymbolFilter || unit != 4 || w.catalog != nil {
			return fmt.Errorf("preferences did not restore native controls or triggered a catalog load")
		}
		text(w.filter, "Unidade.Outra")
		send.Call(w.metricUnits, 0x14e, 1, 0)
		return nil
	}, path); err != nil {
		t.Fatal(err)
	}
	p.ResultFilter, p.TimeUnit = "Unidade.Outra", Microseconds
	got, err := loadPreferences(path)
	if err != nil || got != p {
		t.Fatal("native close did not save preferences", got, err)
	}
	if err := runViewerWithPreferences(`\\.\pipe\unused-preferences`, "", "", true, func(w *window) error {
		unit, _, _ := send.Call(w.metricUnits, 0x147, 0, 0)
		if !w.excludesRuntime() || value(w.filter) != p.ResultFilter || value(w.symbolSearch) != p.SymbolFilter || unit != 1 {
			return fmt.Errorf("reopening lost saved preferences")
		}
		return nil
	}, path); err != nil {
		t.Fatal(err)
	}
	corrupt := []byte(`{"version":999,"private_setting":"retained"}`)
	if err := os.WriteFile(path, corrupt, 0600); err != nil {
		t.Fatal(err)
	}
	if err := runViewerWithPreferences(`\\.\pipe\unused-preferences`, "", "", true, func(w *window) error {
		if w.preferencesProblem == nil || value(w.imagePath) != "" || value(w.filter) != "" {
			return fmt.Errorf("invalid preferences prevented fallback to defaults")
		}
		return nil
	}, path); err != nil {
		t.Fatal(err)
	}
	after, err := os.ReadFile(path)
	if err != nil || !bytes.Equal(after, corrupt) {
		t.Fatal("fallback overwrote unknown preferences", string(after), err)
	}
}

func TestNativeColumnPreferencesWindowSmoke(t *testing.T) {
	if os.Getenv("AP_GUI_SMOKE") != "1" {
		t.Skip("native column preferences smoke enabled in Windows CI")
	}
	const width = 287
	path := filepath.Join(t.TempDir(), "viewer.json")
	check := func(w *window, wantWidth uintptr, wantOrder int32) error {
		gotWidth, _, _ := send.Call(w.table, listGetColumnWidth, 2, 0)
		var order [8]int32
		ok, _, _ := send.Call(w.table, listGetColumnOrder, uintptr(len(order)), uintptr(unsafe.Pointer(&order[0])))
		if ok == 0 || gotWidth != wantWidth || order[1] != wantOrder {
			return fmt.Errorf("column layout width=%d order=%v", gotWidth, order)
		}
		return nil
	}
	if err := runViewerWithPreferences(`\\.\pipe\unused-columns`, "", "", true, func(w *window) error {
		send.Call(w.table, 0x101e, 2, width) // LVM_SETCOLUMNWIDTH
		order := [...]int32{0, 2, 1, 3, 4, 5, 6, 7}
		send.Call(w.table, listSetColumnOrder, uintptr(len(order)), uintptr(unsafe.Pointer(&order[0])))
		w.toggleResultColumn(2)
		if err := check(w, 0, 2); err != nil {
			return err
		}
		w.model.Comparison = &report.Comparison{}
		w.syncColumns() // Same size, different layout.
		if err := check(w, uintptr(defaultColumnWidths[columnSamplingComparison][2]), 1); err != nil {
			return err
		}
		w.model.Comparison = nil
		w.syncColumns()
		return check(w, 0, 2)
	}, path); err != nil {
		t.Fatal(err)
	}
	if err := runViewerWithPreferences(`\\.\pipe\unused-columns`, "", "", true, func(w *window) error {
		if err := check(w, 0, 2); err != nil {
			return err
		}
		w.toggleResultColumn(2)
		return check(w, width, 2)
	}, path); err != nil {
		t.Fatal(err)
	}
}
