package desktop

import (
	"fmt"
	"strings"
	"testing"
	"unicode/utf16"
)

func TestDiagnosticLogRetainsRecentDistinctMessages(t *testing.T) {
	var log diagnosticLog
	if log.add("\n\t") || !log.add("Falha\nno EXE\x00") || log.add("Falha no EXE") {
		t.Fatal("empty or repeated diagnostic changed the log", log.String())
	}
	for i := 1; i <= diagnosticEntries+3; i++ {
		log.add(fmt.Sprintf("Mensagem %d", i))
	}
	lines := strings.Split(log.String(), "\r\n")
	if len(lines) != diagnosticEntries || lines[0] != "Mensagem 4" || lines[len(lines)-1] != fmt.Sprintf("Mensagem %d", diagnosticEntries+3) {
		t.Fatal("recent messages lost chronological order", lines)
	}
	for i := 0; i < diagnosticEntries; i++ {
		log.add(fmt.Sprintf("%02d %s", i, strings.Repeat("😀", diagnosticRunes+1)))
	}
	content := log.String()
	if !strings.HasSuffix(content, "😀…") || len(utf16.Encode([]rune(content))) >= 32767 {
		t.Fatal("bounded Unicode lines exceed the Win32 edit limit")
	}
}
