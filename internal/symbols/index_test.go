package symbols

import (
	"fmt"
	"testing"
)

func TestResolveIntervalsAndASLR(t *testing.T) {
	ps := []Procedure{{"B", 0x2000, 0x10}, {"A", 0x1000, 0x10}}
	idx, err := New("hash", "lab.exe", 0x4000, ps)
	if err != nil {
		t.Fatal(err)
	}
	ps[1].Name = "mutated"
	for _, base := range []uint64{0x140000000, 0x7ff100000000} {
		for _, tc := range []struct {
			offset       uint64
			name, status string
		}{{0xfff, "", "unknown"}, {0x1000, "A", "resolved"}, {0x100f, "A", "resolved"}, {0x1010, "", "unknown"}, {0x2000, "B", "resolved"}, {0x3fff, "", "unknown"}} {
			got, ok := idx.Resolve(base, base+tc.offset)
			if !ok || got.Procedure != tc.name || got.Status != tc.status {
				t.Fatalf("offset=%x result=%+v", tc.offset, got)
			}
			if err := got.Validate(); err != nil {
				t.Fatal(err)
			}
			if tc.name == "A" && got.RVA != "0x0000000000001000" {
				t.Fatal("RVA depends on instruction or ASLR")
			}
		}
		for _, address := range []uint64{base - 1, base + 0x4000} {
			if _, ok := idx.Resolve(base, address); ok {
				t.Fatal("address outside module resolved")
			}
		}
	}
}
func TestInvalidRanges(t *testing.T) {
	for _, ps := range [][]Procedure{{{"A", 0, 0}}, {{"A", 0xfff, 2}}, {{"A", 1, 10}, {"B", 5, 10}}, {{"A", 0, 10}, {"B", 0, 10}}, {{"", 0, 1}}} {
		if _, err := New("hash", "lab.exe", 0x1000, ps); err == nil {
			t.Fatalf("accepted %+v", ps)
		}
	}
	idx, err := New("hash", "lab.exe", 0x1000, nil)
	if err != nil {
		t.Fatal(err)
	}
	loc, ok := idx.Resolve(0, 0)
	if !ok || loc.Status != "unknown" {
		t.Fatal("missing symbol fallback")
	}
}
func BenchmarkResolve10000(b *testing.B) {
	ps := make([]Procedure, 10000)
	for i := range ps {
		ps[i] = Procedure{fmt.Sprintf("P%d", i), uint64(i * 16), 16}
	}
	idx, _ := New("hash", "lab.exe", 160000, ps)
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, ok := idx.Resolve(0x140000000, 0x140000000+uint64(i%10000*16+1)); !ok {
			b.Fatal("not resolved")
		}
	}
}
