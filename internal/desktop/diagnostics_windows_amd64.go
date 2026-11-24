package desktop

import "strings"

func (a *window) setStatus(message string) {
	text(a.status, strings.ReplaceAll(message, "\x00", "�"))
	a.logDiagnostic(message)
}

func (a *window) logDiagnostic(message string) {
	if a.diagnostics.add(message) && a.diagnosticVisible {
		a.refreshDiagnostics()
	}
}

func (a *window) refreshDiagnostics() {
	text(a.diagnosticText, a.diagnostics.String())
	send.Call(a.diagnosticText, 0xb1, 0x7fff, 0x7fff) // EM_SETSEL to the end
	send.Call(a.diagnosticText, 0xb7, 0, 0)           // EM_SCROLLCARET
}

func (a *window) toggleDiagnostics() {
	a.diagnosticVisible = !a.diagnosticVisible
	label, results, diagnostic := "Diagnóstico", uintptr(5), uintptr(0)
	if a.diagnosticVisible {
		label, results, diagnostic = "Resultados", 0, 5
		a.refreshDiagnostics()
	}
	show := user.NewProc("ShowWindow")
	show.Call(a.table, results)
	show.Call(a.diagnosticText, diagnostic)
	show.Call(a.copyDiagnosticsButton, diagnostic)
	text(a.diagnosticButton, label)
}
