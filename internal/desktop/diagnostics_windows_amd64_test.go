package desktop

import (
	"errors"
	"fmt"
	"os"
	"strings"
	"testing"
)

func TestNativeDiagnosticsWindowSmoke(t *testing.T) {
	if os.Getenv("AP_GUI_SMOKE") != "1" {
		t.Skip("native diagnostics smoke enabled in Windows CI")
	}
	visible := func(hwnd uintptr) bool {
		result, _, _ := user.NewProc("IsWindowVisible").Call(hwnd)
		return result != 0
	}
	if err := runViewerWithPreferences(`\\.\pipe\unused-diagnostics`, "", "", true, func(w *window) error {
		if visible(w.diagnosticText) || !visible(w.table) || visible(w.copyDiagnosticsButton) {
			return errors.New("diagnostics replaced the results before opening")
		}
		w.send(update{Error: errors.New("falha de conexão ç")})
		w.send(update{Note: "captura recuperada"})
		w.tick()
		send.Call(w.diagnosticButton, 0xf5, 0, 0) // BM_CLICK
		if !visible(w.diagnosticText) || visible(w.table) || !visible(w.copyDiagnosticsButton) {
			return errors.New("diagnostic panel or its copy action is missing")
		}
		got := value(w.diagnosticText)
		if failure := strings.Index(got, "Erro / desconectado: falha de conexão ç"); failure < 0 || strings.Index(got, "captura recuperada") <= failure {
			return fmt.Errorf("unexpected diagnostic history %q", got)
		}
		w.setStatus("falha posterior")
		if !strings.HasSuffix(value(w.diagnosticText), "\r\nfalha posterior") {
			return errors.New("visible diagnostic history did not update")
		}
		send.Call(w.diagnosticButton, 0xf5, 0, 0)
		if !visible(w.table) || visible(w.diagnosticText) || visible(w.copyDiagnosticsButton) {
			return errors.New("results did not return after closing diagnostics")
		}
		return nil
	}, ""); err != nil {
		t.Fatal(err)
	}
}
