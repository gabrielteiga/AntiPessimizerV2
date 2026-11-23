package td32

import (
	"errors"
	"testing"

	"github.com/gabrielteiga/AntiPessimizerV2/internal/testfixture"
)

func sourceSubsection(tds []byte) []byte {
	directory := u32(tds[16:])
	at := u32(tds[directory+20:])
	return tds[at:directory]
}

func TestSourceIndexUsesContributionBoundsAndSegment(t *testing.T) {
	var source SourceIndex
	tds := testfixture.TDSWithSource()
	procedures, err := parse(tds, &source)
	if err != nil || len(procedures) != 1 {
		t.Fatal(procedures, err)
	}
	for _, c := range []struct {
		segment uint16
		address uint32
		line    uint16
		delta   uint32
	}{
		{1, 31, 0, 0}, {1, 32, 101, 0}, {1, 39, 101, 7}, {1, 40, 109, 0},
		{1, 47, 109, 7}, {1, 48, 0, 0}, {2, 32, 0, 0},
	} {
		loc, ok := source.Lookup(c.segment, c.address)
		if ok != (c.line != 0) || loc.Line != c.line || loc.LineOffset != c.delta || ok && loc.File != `C:\lab\Workload.pas` {
			t.Fatal(c, loc, ok)
		}
	}
	b := sourceSubsection(tds)
	put32(b, 30, 0)
	put32(b, 34, 0)
	put16(b, 40, 2)
	if _, err := parse(tds, &source); err != nil {
		t.Fatal(err)
	}
	if _, ok := source.Lookup(2, 33); ok {
		t.Fatal("extrapolated a line without contribution bounds")
	}
	if loc, ok := source.Lookup(2, 32); !ok || loc.Line != 101 {
		t.Fatal("lost the exact address of a mapped source line", loc)
	}
	if _, ok := source.Lookup(1, 32); ok {
		t.Fatal("source record escaped its segment")
	}
}

func TestSourceIndexLeavesDuplicateAndConflictingMappingsUnknown(t *testing.T) {
	tds := testfixture.TDSWithSource()
	a := sourceSubsection(tds)
	b := append([]byte(nil), a...)
	put32(a, 22, 1)
	put16(b, 52, 12)
	names := []string{"", "original.pas", "include.inc"}
	index, err := parseSourceLines([]entry{{kind: 0x127, data: a}, {kind: 0x127, data: b}}, names)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := index.Lookup(1, 32); ok {
		t.Fatal("selected a file in an ambiguous contribution")
	}
	put32(b, 48, 32)
	index, err = parseSourceLines([]entry{{kind: 0x127, data: b}}, names)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := index.Lookup(1, 32); ok {
		t.Fatal("selected one of the conflicting numbers for the same address")
	}
}

func TestInvalidSourceMetadataDoesNotBreakProcedureOnlyParsing(t *testing.T) {
	for _, change := range []func([]byte){
		func(b []byte) { put16(b, 2, 0xffff) },
		func(b []byte) { put32(b, 4, 4) },
		func(b []byte) { put32(b, 22, 0xffffffff) },
		func(b []byte) { put32(b, 26, 55) },
		func(b []byte) { put32(b, 30, 49) },
		func(b []byte) { put16(b, 42, 0xffff) },
		func(b []byte) { put32(b, 48, 48) },
	} {
		tds := testfixture.TDSWithSource()
		change(sourceSubsection(tds))
		var source SourceIndex
		if p, err := parse(tds, &source); !errors.Is(err, ErrInvalid) || p != nil || !source.Empty() {
			t.Fatal("accepted corrupt source metadata", p, err)
		}
		if p, err := Parse(tds); err != nil || len(p) != 1 {
			t.Fatal("source metadata broke the procedure-only catalog", p, err)
		}
	}
}

func FuzzSourceLines(f *testing.F) {
	f.Add(testfixture.TDSWithSource())
	f.Fuzz(func(t *testing.T, data []byte) {
		var source SourceIndex
		if procedures, err := parse(data, &source); err != nil && (procedures != nil || !source.Empty()) {
			t.Fatal("invalid source metadata returned partial debug information")
		}
	})
}
