package desktop

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"testing"
)

func TestNativePreferencesWindowSmoke(t *testing.T) {
	if os.Getenv("AP_GUI_SMOKE") != "1" {
		t.Skip("native preferences smoke enabled in Windows CI")
	}
	path := filepath.Join(t.TempDir(), "viewer.json")
	p := Preferences{WholeUnit: true, Version: 1, ImagePath: `C:\Projetos\Cotação\app.exe`, ResultFilter: "Ação.Calcular", SymbolFilter: "Unidade.Ações", TimeUnit: ClockTicks}
	if err := savePreferences(path, p); err != nil {
		t.Fatal(err)
	}
	if err := runViewerWithPreferences(`\\.\pipe\unused-preferences`, "", "", true, func(w *window) error {
		unit, _, _ := send.Call(w.metricUnits, 0x147, 0, 0)
		whole, _, _ := send.Call(w.wholeUnit, 0xf0, 0, 0)
		if whole != 1 || value(w.imagePath) != p.ImagePath || value(w.filter) != p.ResultFilter || value(w.symbolSearch) != p.SymbolFilter || unit != 4 || w.catalog != nil {
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
		if value(w.filter) != p.ResultFilter || unit != 1 {
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
