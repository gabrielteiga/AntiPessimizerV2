package report

import (
	"bytes"
	"github.com/gabrielteiga/AntiPessimizerV2/internal/aggregation"
	"github.com/gabrielteiga/AntiPessimizerV2/internal/profiling"
	"github.com/gabrielteiga/AntiPessimizerV2/internal/stream"
	"testing"
	"time"
)

func fixture(t *testing.T, id string, tid uint32, n int, width time.Duration) profiling.Snapshot {
	t.Helper()
	start := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	a, err := aggregation.New(aggregation.Config{SessionID: id, Target: profiling.Target{PID: 1, Start: start, Image: "lab.exe"}, Source: "synthetic", Start: start, Width: width, Capacity: 10, TopN: 10})
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < n; i++ {
		if err := a.Add(profiling.Sample{At: start, ThreadID: tid, Location: profiling.Location{ModuleID: "sha256:fixture", Module: "lab.exe", RVA: "0x0000000000001000", Procedure: "Lab.Hot", Status: "resolved"}}); err != nil {
			t.Fatal(err)
		}
	}
	s, err := a.Close(a.End())
	if err != nil {
		t.Fatal(err)
	}
	return s
}
func TestArchiveComparisonAndFilters(t *testing.T) {
	a := fixture(t, "a", 1, 10, time.Second)
	b := fixture(t, "b", 2, 30, 2*time.Second)
	var buf bytes.Buffer
	if err := stream.Write(&buf, a); err != nil {
		t.Fatal(err)
	}
	archive, err := Read(&buf)
	if err != nil {
		t.Fatal(err)
	}
	c, err := Compare(archive, []profiling.Snapshot{b})
	if err != nil {
		t.Fatal(err)
	}
	if len(c.Changes) != 1 || c.Changes[0].DeltaPerSecond != 5 {
		t.Fatal(c)
	}
	filtered, err := Summarize(archive, Filter{Thread: 99})
	if err != nil || filtered.Other != 10 || len(filtered.Rows) != 0 {
		t.Fatal(filtered, err)
	}
	b.Hotspots[0].ModuleID = "different-build"
	if _, err := Compare(archive, []profiling.Snapshot{b}); err == nil {
		t.Fatal("different builds compared")
	}
	if _, err := Summarize([]profiling.Snapshot{a, a}, Filter{}); err == nil {
		t.Fatal("duplicate accepted")
	}
}

func TestComparisonRejectsUnstableUnknownImages(t *testing.T) {
	a := fixture(t, "a", 1, 10, time.Second)
	b := fixture(t, "b", 1, 10, time.Second)
	a.Hotspots[0].ModuleID = "runtime:1000:1000"
	b.Hotspots[0].ModuleID = a.Hotspots[0].ModuleID
	if _, err := Compare([]profiling.Snapshot{a}, []profiling.Snapshot{b}); err == nil {
		t.Fatal("ASLR-dependent identities compared across sessions")
	}
}

func TestUnitFiltersPreserveTotalsAndComparison(t *testing.T) {
	a := fixture(t, "unit", 7, 10, time.Second)
	a.Hotspots[0].Unit = "Pricing"
	summary, err := Summarize([]profiling.Snapshot{a}, Filter{Unit: "Pricing"})
	if err != nil || len(summary.Rows) != 1 {
		t.Fatal(summary, err)
	}
	summary, err = Summarize([]profiling.Snapshot{a}, Filter{Text: "pricing"})
	if err != nil || len(summary.Rows) != 1 {
		t.Fatal(summary, err)
	}
	summary, err = Summarize([]profiling.Snapshot{a}, Filter{Unit: "Other"})
	if err != nil || summary.Accepted != 10 || summary.Other != 10 || len(summary.Rows) != 0 {
		t.Fatal(summary, err)
	}
	compared, err := Compare([]profiling.Snapshot{a}, []profiling.Snapshot{a})
	if err != nil || compared.Changes[0].Unit != "Pricing" {
		t.Fatal(compared, err)
	}
}
