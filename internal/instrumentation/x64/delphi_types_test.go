package x64

import (
	"encoding/binary"
	"errors"
	"testing"
)

func typedScopePE() ([]byte, []byte) {
	b, code := scopesPE()
	w32(b, 0x498, 0x21c0)
	w32(b, 0x49c, 0)
	w32(b, 0x5c0, 2)
	w32(b, 0x5c4, 0x21f0)
	w32(b, 0x5c8, peLeafRVA+9)
	w32(b, 0x5cc, 0)
	w32(b, 0x5d0, peLeafRVA+16)
	binary.LittleEndian.PutUint64(b[0x5f0:], 0x102880)
	return b, code
}
func TestDelphiTypedCatchMovesOnlyLandingAddresses(t *testing.T) {
	b, code := typedScopePE()
	p, err := parsePEImage(b)
	if err != nil {
		t.Fatal(err)
	}
	scopes, err := p.readScopes(0x208c, peLeafRVA, uint32(len(code)), 5, 25, 3)
	if err != nil {
		t.Fatal(err)
	}
	tables, err := p.delphiTypeTables(scopes, peLeafRVA, uint32(len(code)), 5)
	if err != nil || len(tables) != 1 {
		t.Fatal(tables, err)
	}
	flow, err := rewriteFunctionWithPatch(code, 0x101020, 0x180000, true, 5, 5, true)
	if err != nil {
		t.Fatal(err)
	}
	blob, targets, err := relocateTypeTables(tables, code, flow, peLeafRVA, 0x80200, 0x80800)
	if err != nil {
		t.Fatal(err)
	}
	if targets[0x21c0] != 0x8080c || binary.LittleEndian.Uint32(blob) != 1 {
		t.Fatal("typed scope table address", blob, targets)
	}
	table := blob[12:]
	for i, want := range []uint32{2, 0x21f0, 0x80209, 0, 0x80210} {
		if binary.LittleEndian.Uint32(table[i*4:]) != want {
			t.Fatal("class reference or landing changed", table)
		}
	}
	// Multiple scopes referring to the same table share one immutable copy.
	doubled := append(append([]byte(nil), scopes...), scopes[4:]...)
	w32(doubled, 0, 2)
	tables, err = p.delphiTypeTables(doubled, peLeafRVA, uint32(len(code)), 5)
	if err != nil || len(tables) != 1 {
		t.Fatal("duplicate typed table was not coalesced", tables, err)
	}
}
func TestDelphiTypedCatchRejectsMalformedMetadata(t *testing.T) {
	for name, mutate := range map[string]func([]byte){
		"empty table":                    func(b []byte) { w32(b, 0x5c0, 0) },
		"too many types":                 func(b []byte) { w32(b, 0x5c0, 65) },
		"bad class slot":                 func(b []byte) { w32(b, 0x5c4, 0xfffffffc) },
		"class slot inside patched code": func(b []byte) { w32(b, 0x5c4, peLeafRVA) },
		"else before final entry":        func(b []byte) { w32(b, 0x5c4, 0) },
		"target before prologue":         func(b []byte) { w32(b, 0x5c8, peLeafRVA) },
		"target outside body":            func(b []byte) { w32(b, 0x5c8, peLeafRVA+100) },
		"typed table inside code":        func(b []byte) { w32(b, 0x498, peLeafRVA+9) },
		"unused scope target":            func(b []byte) { w32(b, 0x49c, peLeafRVA+9) },
	} {
		t.Run(name, func(t *testing.T) {
			b, code := typedScopePE()
			mutate(b)
			p, err := parsePEImage(b)
			if err != nil {
				t.Fatal(err)
			}
			if _, err = p.readScopes(0x208c, peLeafRVA, uint32(len(code)), 5, 25, 3); !errors.Is(err, ErrUnsupported) {
				t.Fatal("invalid typed table accepted", err)
			}
		})
	}
	b, code := typedScopePE()
	w32(b, 0x5c8, peLeafRVA+21)
	p, _ := parsePEImage(b)
	scopes, err := p.readScopes(0x208c, peLeafRVA, uint32(len(code)), 5, 25, 3)
	if err != nil {
		t.Fatal(err)
	}
	tables, err := p.delphiTypeTables(scopes, peLeafRVA, uint32(len(code)), 5)
	if err != nil {
		t.Fatal(err)
	}
	flow, err := rewriteFunctionWithPatch(code, 0x101020, 0x180000, true, 5, 5, true)
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := relocateTypeTables(tables, code, flow, peLeafRVA, 0x80200, 0x80800); !errors.Is(err, ErrUnsupported) {
		t.Fatal("typed target in instruction interior accepted", err)
	}
}
