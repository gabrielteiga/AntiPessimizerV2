package symbols

import (
	"strings"
	"testing"
	"unicode/utf8"
)

func TestDisplayName(t *testing.T) {
	for raw, want := range map[string]string{"_ZN3Lab7HotspotEv": "Lab.Hotspot", "_ZN3Lab6TClass3RunEii": "Lab.TClass.Run", "Lab.Original": "Lab.Original", "_ZN999x": "_ZN999x", "_ZN0": "_ZN0", "_ZN3LabIbad": "_ZN3LabIbad"} {
		if got := DisplayName(raw); got != want {
			t.Fatalf("%q: %q != %q", raw, got, want)
		}
	}
	a := DisplayName(strings.Repeat("ação", 100))
	b := DisplayName(strings.Repeat("ação", 100) + "x")
	if len(a) > 256 || !utf8.ValidString(a) || a == b {
		t.Fatal(a, b)
	}
}
