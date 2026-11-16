package td32

import (
	"encoding/binary"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func put16(b []byte, n int, v uint16) { binary.LittleEndian.PutUint16(b[n:], v) }
func put32(b []byte, n int, v uint32) { binary.LittleEndian.PutUint32(b[n:], v) }

// fixture is generated from the documented wire layout, not a Delphi binary.
func fixture(name string) []byte {
	names := make([]byte, 4)
	put32(names, 0, 1)
	names = append(names, byte(len(name)))
	names = append(names, []byte(name)...)
	names = append(names, 0)
	b := make([]byte, 96)
	copy(b, "FB09")
	put32(b, 4, 8)
	put16(b, 8, 16)
	put16(b, 10, 12)
	put32(b, 12, 2)
	put16(b, 24, 0x125)
	put32(b, 28, 48)
	put32(b, 32, 48)
	put16(b, 36, 0x130)
	put32(b, 40, 96)
	put32(b, 44, uint32(len(names)))
	put32(b, 48, 1)
	put16(b, 52, 42)
	put16(b, 54, 0x205)
	put32(b, 68, 16)
	put32(b, 80, 32)
	put16(b, 84, 1)
	put32(b, 92, 1)
	b = append(b, names...)
	b = append(b, make([]byte, 8)...)
	copy(b[len(b)-8:], "FB09")
	put32(b, len(b)-4, uint32(len(b)))
	return b
}
func TestParse(t *testing.T) {
	for _, name := range []string{"Unit.Hotspot", strings.Repeat("a", 300), "Unidade.Ação"} {
		for _, sig := range []string{"FB09", "FB0A"} {
			b := fixture(name)
			copy(b, sig)
			copy(b[len(b)-8:], sig)
			b = append([]byte("prefix"), b...)
			ps, err := Parse(b)
			if err != nil || len(ps) != 1 {
				t.Fatalf("%v: %v", ps, err)
			}
			if ps[0] != (Procedure{name, 1, 32, 16}) {
				t.Fatal(ps)
			}
		}
	}
}
func TestCorrupt(t *testing.T) {
	cases := map[string]func([]byte){
		"trailer":           func(b []byte) { put32(b, len(b)-4, 0xffffffff) },
		"directory cycle":   func(b []byte) { put32(b, 16, 8) },
		"directory bounds":  func(b []byte) { put32(b, 12, 0xffffffff) },
		"subsection bounds": func(b []byte) { put32(b, 44, 0xffffffff) },
		"record zero":       func(b []byte) { put16(b, 52, 0) },
		"record short":      func(b []byte) { put16(b, 52, 4) },
		"name index":        func(b []byte) { put32(b, 92, 2) },
		"name count":        func(b []byte) { put32(b, 96, 0xffffffff) },
		"unterminated":      func(b []byte) { b[len(b)-9] = 1 },
		"zero size":         func(b []byte) { put32(b, 68, 0) },
		"zero segment":      func(b []byte) { put16(b, 84, 0) },
	}
	for name, mutate := range cases {
		t.Run(name, func(t *testing.T) {
			b := fixture("Hot")
			mutate(b)
			ps, err := Parse(b)
			if !errors.Is(err, ErrInvalid) || ps != nil {
				t.Fatalf("%v %v", ps, err)
			}
		})
	}
	b := fixture("Hot")
	for n := 0; n < len(b); n++ {
		if _, err := Parse(b[:n]); err == nil {
			t.Fatalf("accepted truncation %d", n)
		}
	}
}
func TestLoadTDS(t *testing.T) {
	path := filepath.Join(t.TempDir(), "lab.tds")
	if err := os.WriteFile(path, fixture("Hot"), 0600); err != nil {
		t.Fatal(err)
	}
	ps, source, err := Load(path)
	if err != nil || source != path || len(ps) != 1 {
		t.Fatalf("%v %q %v", ps, source, err)
	}
}
func FuzzParse(f *testing.F) {
	f.Add(fixture("Hot"))
	f.Add([]byte{})
	f.Add(fixture(strings.Repeat("a", 300)))
	f.Fuzz(func(t *testing.T, b []byte) { Parse(b) })
}

func TestChainedDirectoriesAndUnknownRecords(t *testing.T) {
	b := fixture("Hot")
	// Move the names entry into a second directory, inserted before the trailer.
	second := len(b) - 8
	tail := append([]byte(nil), b[second:]...)
	b = append(b[:second], make([]byte, 28)...)
	b = append(b, tail...)
	put32(b, 12, 1)
	put32(b, 16, uint32(second))
	put16(b, second, 16)
	put16(b, second+2, 12)
	put32(b, second+4, 1)
	copy(b[second+16:second+28], b[36:48])
	put32(b, len(b)-4, uint32(len(b)))
	ps, err := Parse(b)
	if err != nil || len(ps) != 1 {
		t.Fatalf("%v %v", ps, err)
	}
	// A well framed unknown symbol is skipped; it must not create a procedure.
	put16(b, 54, 0x7777)
	ps, err = Parse(b)
	if err != nil || len(ps) != 0 {
		t.Fatalf("%v %v", ps, err)
	}
}
