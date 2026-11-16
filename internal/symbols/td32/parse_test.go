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

// JCL AnalyseNames stores length modulo 256 and advances to the NUL at
// length + 256*k. Check exact rollover points and the following name index.
func TestNameLengthRollover(t *testing.T) {
	for _, n := range []int{255, 256, 257, 511, 512, 513} {
		name := strings.Repeat("x", n)
		b := fixture(name)
		namesEnd := len(b) - 8
		tail := append([]byte(nil), b[namesEnd:]...)
		b = append(b[:namesEnd], 4, 'N', 'e', 'x', 't', 0)
		b = append(b, tail...)
		put32(b, 96, 2)
		put32(b, 44, uint32(n+12))
		put32(b, len(b)-4, uint32(len(b)))
		ps, err := Parse(b)
		if err != nil || len(ps) != 1 || ps[0].Name != name {
			t.Fatalf("length %d: %v %v", n, ps, err)
		}
		put32(b, 92, 2)
		ps, err = Parse(b)
		if err != nil || len(ps) != 1 || ps[0].Name != "Next" {
			t.Fatalf("following name after %d: %v %v", n, ps, err)
		}
	}
}

// JCL AnalyseAlignSymbols advances by record.Size + sizeof(WORD), including
// records it does not recognize. A later procedure must retain its own fields.
func TestMixedProcedureRecords(t *testing.T) {
	b := fixture("Hot")
	second := append([]byte(nil), b[52:96]...)
	put16(second, 2, 0x204) // local procedure after the global one
	put32(second, 16, 8)
	put32(second, 28, 64)
	put16(second, 32, 2)
	suffix := append([]byte(nil), b[96:]...)
	b = append(b[:96], 2, 0, 0xff, 0x7f) // unknown record with no payload
	b = append(b, second...)
	b = append(b, suffix...)
	put32(b, 32, 96)
	put32(b, 40, 144)
	put32(b, len(b)-4, uint32(len(b)))
	ps, err := Parse(b)
	if err != nil || len(ps) != 2 {
		t.Fatalf("%v %v", ps, err)
	}
	if ps[0] != (Procedure{"Hot", 1, 32, 16}) || ps[1] != (Procedure{"Hot", 2, 64, 8}) {
		t.Fatal(ps)
	}
	// A corrupt later procedure must not leak the first procedure as partial data.
	put32(b, 140, 2)
	ps, err = Parse(b)
	if !errors.Is(err, ErrInvalid) || ps != nil {
		t.Fatalf("partial result: %v %v", ps, err)
	}
}
