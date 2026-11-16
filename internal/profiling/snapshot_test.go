package profiling

import (
	"math"
	"testing"
	"time"
)

func valid() Snapshot {
	start := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	return Snapshot{SchemaVersion: 1, SessionID: "test", Sequence: 1, Target: Target{PID: 1, Start: start, Image: "lab"}, Mode: "sampling", Source: "synthetic", WindowStart: start, WindowEnd: start.Add(time.Second), Published: start.Add(time.Second), Quality: Quality{Observed: 2, Accepted: 2}, OtherSamples: 2}
}
func TestSnapshotValidation(t *testing.T) {
	if err := valid().Validate(); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name   string
		change func(*Snapshot)
	}{
		{"version", func(s *Snapshot) { s.SchemaVersion = 2 }},
		{"sequence", func(s *Snapshot) { s.Sequence = 0 }},
		{"pid", func(s *Snapshot) { s.Target.PID = 0 }},
		{"time", func(s *Snapshot) { s.WindowEnd = s.WindowStart }},
		{"publication", func(s *Snapshot) { s.Published = s.WindowStart }},
		{"quality", func(s *Snapshot) { s.Quality.Observed = 1 }},
		{"loss", func(s *Snapshot) { s.Quality.SourceLost = 1 }},
		{"total", func(s *Snapshot) { s.OtherSamples = 1 }},
		{"overflow", func(s *Snapshot) { s.Quality.Accepted = math.MaxUint64; s.Quality.Dropped = 1 }},
		{"mode", func(s *Snapshot) { s.Mode = "instrumentation" }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s := valid()
			tc.change(&s)
			if err := s.Validate(); err == nil {
				t.Fatal("accepted invalid snapshot")
			}
		})
	}
}
func TestSum(t *testing.T) {
	n, err := Sum(math.MaxUint64-1, 1)
	if err != nil || n != math.MaxUint64 {
		t.Fatal(n, err)
	}
	if _, err := Sum(math.MaxUint64, 1); err == nil {
		t.Fatal("overflow accepted")
	}
}

func TestOptionalPascalUnitValidation(t *testing.T) {
	loc := Location{ModuleID: "id", Module: "lab.exe", Procedure: "Hot", Unit: "LabWorkload", RVA: "0x0000000000001000", Status: "resolved"}
	if err := loc.Validate(); err != nil {
		t.Fatal(err)
	}
	loc.Unit = "bad\nunit"
	if err := loc.Validate(); err == nil {
		t.Fatal("control characters accepted")
	}
	loc.Unit = "LabWorkload"
	loc.Status = "unknown"
	loc.Procedure = ""
	loc.Reason = "no_symbol"
	if err := loc.Validate(); err == nil {
		t.Fatal("unknown procedure claimed a Pascal unit")
	}
}
