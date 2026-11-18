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
	p.ResultFilter, p.TimeUnit = "outro", ClockTicks
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
