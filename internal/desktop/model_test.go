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
}
