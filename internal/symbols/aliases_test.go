package symbols

import (
	"testing"

	"github.com/gabrielteiga/AntiPessimizerV2/internal/testfixture"
)

func TestExactAliasesPreserveFirstNameAndOneResolvedInterval(t *testing.T) {
	primary := Procedure{Name: "Primary", Unit: "First", RVA: 0x1000, Size: 16}
	alias := Procedure{Name: "Alias", Unit: "Second", RVA: 0x1000, Size: 16}
	otherUnit := primary
	otherUnit.Unit = "Other"
	x, err := New("build", "lab.exe", 0x4000, []Procedure{{Name: "Later", RVA: 0x2000, Size: 16}, primary, alias, primary, alias, otherUnit})
	if err != nil {
		t.Fatal(err)
	}
	if rows := x.Procedures(); len(rows) != 2 || rows[0] != primary {
		t.Fatal("aliases became duplicate physical procedures", rows)
	}
	aliases := x.Aliases()
	if len(aliases) != 2 || aliases[0] != alias || aliases[1] != otherUnit {
		t.Fatal("alternate names/units lost or duplicated", aliases)
	}
	aliases[0].Name = "mutated"
	for _, base := range []uint64{0x140000000, 0x7ff100000000} {
		for _, offset := range []uint64{0x1000, 0x100f} {
			loc, ok := x.Resolve(base, base+offset)
			if !ok || loc.Procedure != "Primary" || loc.Unit != "First" || x.Aliases()[0].Name != "Alias" {
				t.Fatal("first-match or immutable alias policy lost", loc)
			}
		}
		loc, _ := x.Resolve(base, base+0x1010)
		if loc.Status != "unknown" {
			t.Fatal("alias expanded a half-open interval", loc)
		}
	}
	for _, bad := range []Procedure{{Name: "Partial", RVA: 0x1008, Size: 16}, {Name: "DifferentSize", RVA: 0x1000, Size: 8}, {Name: "", RVA: 0x1000, Size: 16}} {
		if _, err := New("build", "lab.exe", 0x4000, []Procedure{primary, bad}); err == nil {
			t.Fatal("ambiguous/malformed alias accepted", bad)
		}
	}
}

func TestTD32ExactAliasesSurviveSegmentMapping(t *testing.T) {
	x, err := FromTD32("build", "lab.exe", 0x7000, testfixture.TDSWithAlias("Lab.Alias"), map[uint16]Segment{1: {0x5000, 0x200}})
	if err != nil || len(x.Procedures()) != 1 || len(x.Aliases()) != 1 || x.Aliases()[0].Name != "Lab.Alias" {
		t.Fatal(x, err)
	}
	loc, _ := x.Resolve(0x140000000, 0x140005028)
	if loc.Procedure != "Lab.Hot" || loc.RVA != "0x0000000000005020" {
		t.Fatal("TD32 file-order primary name changed", loc)
	}
}

func FuzzTD32AliasIndex(f *testing.F) {
	f.Add(testfixture.TDSWithAlias("Lab.Alias"))
	f.Add(testfixture.TDS())
	f.Add([]byte{})
	f.Fuzz(func(t *testing.T, data []byte) {
		x, err := FromTD32("build", "lab.exe", 0x7000, data, map[uint16]Segment{1: {0x5000, 0x200}})
		if err != nil {
			return
		}
		primary := map[uint64]Procedure{}
		for _, p := range x.Procedures() {
			primary[p.RVA] = p
			loc, ok := x.Resolve(0x140000000, 0x140000000+p.RVA+p.Size-1)
			if !ok || loc.Procedure != p.Name || loc.Unit != p.Unit {
				t.Fatal("accepted interval does not resolve to its primary name", p, loc)
			}
		}
		for _, alias := range x.Aliases() {
			p, ok := primary[alias.RVA]
			if !ok || p.Size != alias.Size || p == alias {
				t.Fatal("alias is not a distinct name of one exact primary interval", alias)
			}
		}
	})
}
