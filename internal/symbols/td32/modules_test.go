package td32

import (
	"errors"
	"testing"
)

// Add an sstModule describing Pascal unit LabUnit to the existing procedure
// fixture. Directory module index 7 connects align symbols to that unit.
func moduleFixture() []byte {
	b := fixture("Hot")
	oldEnd := len(b) - 8
	b = b[:oldEnd]
	moduleOffset := len(b)
	b = append(b, make([]byte, 40)...)
	put16(b, moduleOffset+4, 1)
	put32(b, moduleOffset+8, 2)
	put16(b, moduleOffset+28, 1)
	put16(b, moduleOffset+30, 1)
	put32(b, moduleOffset+32, 32)
	put32(b, moduleOffset+36, 16)
	// Append a second names subsection, then the directory for both new entries.
	namesOffset := len(b)
	b = append(b, 1, 0, 0, 0, 7, 'L', 'a', 'b', 'U', 'n', 'i', 't', 0)
	directory := len(b)
	b = append(b, make([]byte, 40)...)
	put16(b, directory, 16)
	put16(b, directory+2, 12)
	put32(b, directory+4, 2)
	put16(b, directory+16, 0x120)
	put16(b, directory+18, 7)
	put32(b, directory+20, uint32(moduleOffset))
	put32(b, directory+24, 40)
	put16(b, directory+28, 0x130)
	put32(b, directory+32, uint32(namesOffset))
	put32(b, directory+36, 13)
	put32(b, 16, uint32(directory))
	put16(b, 26, 7)
	b = append(b, 'F', 'B', '0', '9', 0, 0, 0, 0)
	put32(b, len(b)-4, uint32(len(b)))
	return b
}
func TestModuleUnitAssociation(t *testing.T) {
	b := moduleFixture()
	ps, err := Parse(b)
	if err != nil || len(ps) != 1 || ps[0].Unit != "LabUnit" || ps[0].Name != "Hot" {
		t.Fatal(ps, err)
	}
	// Wrong segment/range must not lend its unit name to this procedure.
	for _, change := range []func([]byte){func(b []byte) { put16(b, 84, 2) }, func(b []byte) { put32(b, 80, 31) }, func(b []byte) { put32(b, 68, 17) }, func(b []byte) { put16(b, 26, 99) }} {
		b := moduleFixture()
		change(b)
		ps, err := Parse(b)
		if err != nil || len(ps) != 1 || ps[0].Unit != "" {
			t.Fatal(ps, err)
		}
	}
}
func TestMalformedModules(t *testing.T) {
	for name, change := range map[string]func([]byte){
		"short":          func(b []byte) { directory := int(u32(b[16:])); put32(b, directory+24, 27) },
		"segment count":  func(b []byte) { off := len(fixture("Hot")) - 8; put16(b, off+4, 65535) },
		"bad name index": func(b []byte) { off := len(fixture("Hot")) - 8; put32(b, off+8, 99) },
		"overflow range": func(b []byte) { off := len(fixture("Hot")) - 8; put32(b, off+32, 0xffffffff) },
	} {
		t.Run(name, func(t *testing.T) {
			b := moduleFixture()
			change(b)
			ps, err := Parse(b)
			if !errors.Is(err, ErrInvalid) || ps != nil {
				t.Fatal(ps, err)
			}
		})
	}
}
