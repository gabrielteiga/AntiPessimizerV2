package desktop

import (
	"github.com/gabrielteiga/AntiPessimizerV2/internal/aggregation"
	"github.com/gabrielteiga/AntiPessimizerV2/internal/profiling"
	"github.com/gabrielteiga/AntiPessimizerV2/internal/report"
	"testing"
	"time"
)

func TestModelFilteringAndSessionReset(t *testing.T) {
	start := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	a, err := aggregation.New(aggregation.Config{SessionID: "a", Target: profiling.Target{PID: 1, Start: start, Image: "lab.exe"}, Source: "synthetic", Start: start, Width: time.Second, Capacity: 10, TopN: 10})
	if err != nil {
		t.Fatal(err)
	}
	loc := profiling.Location{ModuleID: "id", Module: "lab.exe", Procedure: "Lab.Hot", RVA: "0x0000000000001000", Status: "resolved"}
	if err := a.Add(profiling.Sample{At: start, ThreadID: 7, Location: loc}); err != nil {
		t.Fatal(err)
	}
	s, err := a.Close(a.End())
	if err != nil {
		t.Fatal(err)
	}
	m := Model{}
	if err = m.Append(s); err != nil {
		t.Fatal(err)
	}
	s.Hotspots[0].Procedure = "mutated"
	if err = m.Rebuild(10, report.Filter{Text: "hot", Thread: 7}); err != nil {
		t.Fatal(err)
	}
	if len(m.Rows) != 1 || m.Rows[0][4] != "Lab.Hot" || m.Rows[0][1] != "100.00%" {
		t.Fatal(m.Rows)
	}
	s.SessionID = "b"
	if err = m.Append(s); err != nil {
		t.Fatal(err)
	}
	if len(m.History) != 1 || m.Session != "b" {
		t.Fatal(m)
	}
	if err := m.ReplaceMetrics(metricRecords(t)); err != nil {
		t.Fatal(err)
	}
	if err := m.Append(s); err != nil {
		t.Fatal(err)
	}
	if len(m.Metrics) != 0 || len(m.Headers()) != 8 || len(m.History) != 1 {
		t.Fatal("instrumentation state leaked into sampling")
	}
}

func TestHistoryRecoveryAndArchiveCursor(t *testing.T) {
	start := time.Now().UTC()
	a, err := aggregation.New(aggregation.Config{SessionID: "recover", Target: profiling.Target{PID: 1, Start: start, Image: "lab.exe"}, Source: "synthetic", Start: start, Width: time.Second, Capacity: 10, TopN: 10})
	if err != nil {
		t.Fatal(err)
	}
	var snapshots []profiling.Snapshot
	for i := 0; i < 4; i++ {
		s, err := a.Close(a.End())
		if err != nil {
			t.Fatal(err)
		}
		snapshots = append(snapshots, s)
	}
	var m Model
	m.Append(snapshots[0])
	m.Append(snapshots[3])
	if m.Omitted != 2 {
		t.Fatal(m.Omitted)
	}
	if err := m.Replace(snapshots); err != nil {
		t.Fatal(err)
	}
	if m.Omitted != 0 || len(m.History) != 4 {
		t.Fatal("history not recovered")
	}
	m.At = snapshots[1].Sequence
	if err := m.Rebuild(1, report.Filter{}); err != nil {
		t.Fatal(err)
	}
	if m.Summary.Duration != time.Second {
		t.Fatal(m.Summary.Duration)
	}
	bad := append([]profiling.Snapshot(nil), snapshots...)
	bad[1].SessionID = "other"
	if err := m.Replace(bad); err == nil {
		t.Fatal("mixed history accepted")
	}
	if m.Last != 4 {
		t.Fatal("failed update changed model")
	}
}

func TestComparisonRowsAndSignedSorting(t *testing.T) {
	m := Model{Comparison: &report.Comparison{Changes: []report.Delta{{Module: "lab", Procedure: "A", Before: 20, After: 10, DeltaPerSecond: -10}, {Module: "lab", Procedure: "B", Before: 0, After: 5, DeltaPerSecond: 5}}}, SortColumn: 2}
	if err := m.Rebuild(10, report.Filter{}); err != nil {
		t.Fatal(err)
	}
	if len(m.Rows) != 2 || m.Rows[0][4] != "B" || m.Rows[0][6] != "novo" {
		t.Fatal(m.Rows)
	}
	m.Ascending = true
	m.Rebuild(10, report.Filter{})
	if m.Rows[0][4] != "A" {
		t.Fatal(m.Rows)
	}
}
