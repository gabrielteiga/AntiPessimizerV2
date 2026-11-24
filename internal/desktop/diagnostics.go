package desktop

import (
	"strings"
	"unicode"
)

const (
	diagnosticEntries = 32
	diagnosticRunes   = 450
)

// The viewer retains diagnostics in memory for the lifetime of the window.
// The bound also keeps even all-surrogate text within a Win32 edit control.
type diagnosticLog struct {
	lines [diagnosticEntries]string
	next  int
	count int
}

func (l *diagnosticLog) add(message string) bool {
	message = strings.TrimSpace(strings.Map(func(r rune) rune {
		if unicode.IsControl(r) {
			return ' '
		}
		return r
	}, message))
	remaining := diagnosticRunes
	for i := range message {
		if remaining == 0 {
			message = message[:i] + "…"
			break
		}
		remaining--
	}
	if message == "" || l.count != 0 && l.lines[(l.next+diagnosticEntries-1)%diagnosticEntries] == message {
		return false
	}
	l.lines[l.next] = message
	l.next = (l.next + 1) % diagnosticEntries
	l.count = min(l.count+1, diagnosticEntries)
	return true
}

func (l *diagnosticLog) String() string {
	var b strings.Builder
	for i := 0; i < l.count; i++ {
		if i != 0 {
			b.WriteString("\r\n")
		}
		b.WriteString(l.lines[(l.next+diagnosticEntries-l.count+i)%diagnosticEntries])
	}
	return b.String()
}
