package aggregation

import (
	"errors"
	"math"
	"testing"
	"time"

	"github.com/gabrielteiga/AntiPessimizerV2/internal/profiling"
)

func config() Config {
	start := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	return Config{SessionID: "test", Target: profiling.Target{PID: 1, Start: start, Image: "lab.exe"}, Source: "synthetic", Start: start, Width: time.Second, Capacity: 3, TopN: 2, SourceLossKnown: true}
}
func sample(at time.Time, tid uint32) profiling.Sample {
	return profiling.Sample{At: at, ThreadID: tid, Location: profiling.Location{ModuleID: "build", Module: "lab.exe", RVA: "0x0000000000001000", Procedure: "Hot", Status: "resolved"}}
}
func TestWindows(t *testing.T) {
	c := config()
	a, err := New(c)
	if err != nil {
		t.Fatal(err)
	}
	// Out-of-order samples within the active window are accepted.
	for _, s := range []profiling.Sample{sample(c.Start.Add(900*time.Millisecond), 2), sample(c.Start, 1), sample(c.Start.Add(100*time.Millisecond), 2), sample(c.Start, 3), sample(c.Start, 4), sample(c.Start.Add(-time.Nanosecond), 1)} {
		if err := a.Add(s); err != nil {
			t.Fatal(err)
		}
	}
	if err := a.Add(sample(a.End(), 1)); !errors.Is(err, ErrFuture) {
		t.Fatalf("boundary error=%v", err)
	}
	if _, err := a.Close(c.Start); err == nil {
		t.Fatal("published before window end")
	}
	s, err := a.Close(a.End())
	if err != nil {
		t.Fatal(err)
	}
	if err := s.Validate(); err != nil {
		t.Fatal(err)
	}
	if s.Quality.Observed != 6 || s.Quality.Accepted != 4 || s.Quality.Dropped != 1 || s.Quality.Late != 1 || s.OtherSamples != 1 {
		t.Fatalf("counts: %+v other=%d", s.Quality, s.OtherSamples)
	}
	if len(s.Hotspots) != 2 || s.Hotspots[0].ThreadID != 2 || s.Hotspots[0].Samples != 2 || s.Hotspots[1].ThreadID != 1 {
		t.Fatalf("ranking: %+v", s.Hotspots)
	}
	s.Hotspots[0].Samples = 999 // Published values must not alias aggregation state.
	if err := a.Add(sample(c.Start.Add(time.Second), 1)); err != nil {
		t.Fatal(err)
	}
	next, err := a.Close(a.End())
	if err != nil {
		t.Fatal(err)
	}
	if next.Sequence != 2 || next.Quality.Accepted != 1 || next.Hotspots[0].Samples != 1 || !next.WindowStart.Equal(s.WindowEnd) {
		t.Fatalf("next=%+v", next)
	}
	empty, err := a.Close(a.End())
	if err != nil {
		t.Fatal(err)
	}
	if empty.Sequence != 3 || empty.Quality.Observed != 0 || len(empty.Hotspots) != 0 {
		t.Fatalf("empty=%+v", empty)
	}
}
func TestValidationAndOverflow(t *testing.T) {
	for _, change := range []func(*Config){func(c *Config) { c.Width = 0 }, func(c *Config) { c.Capacity = 0 }, func(c *Config) { c.TopN = 4 }, func(c *Config) { c.Target.PID = 0 }, func(c *Config) { c.Source = "" }} {
		c := config()
		change(&c)
		if _, err := New(c); err == nil {
			t.Fatal("accepted invalid config")
		}
	}
	a, _ := New(config())
	if err := a.Add(sample(time.Time{}, 1)); err == nil {
		t.Fatal("accepted zero timestamp")
	}
	a.quality.Observed = math.MaxUint64
	if err := a.Add(sample(a.start, 1)); err == nil {
		t.Fatal("counter wrapped")
	}
	if a.quality.Observed != math.MaxUint64 {
		t.Fatal("mutated failed counter")
	}
	a.sequence = math.MaxUint64
	if _, err := a.Close(a.End()); err == nil {
		t.Fatal("sequence wrapped")
	}
}
func BenchmarkWindow1000(b *testing.B) {
	c := config()
	c.Capacity = 100
	c.TopN = 20
	a, _ := New(c)
	s := sample(c.Start, 1)
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		s.At = a.start
		for j := 0; j < 1000; j++ {
			s.ThreadID = uint32(j%100 + 1)
			if err := a.Add(s); err != nil {
				b.Fatal(err)
			}
		}
		if _, err := a.Close(a.End()); err != nil {
			b.Fatal(err)
		}
	}
}

func TestPartialWindowRequiresPositiveDuration(t *testing.T) {
	c := config()
	a, err := New(c)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := a.ClosePartial(a.Start()); err == nil {
		t.Fatal("zero-duration window accepted")
	}
	end := a.Start().Add(c.Width / 2)
	snapshot, err := a.ClosePartial(end)
	if err != nil {
		t.Fatal(err)
	}
	if !snapshot.WindowEnd.Equal(end) || !snapshot.WindowStart.Equal(c.Start) {
		t.Fatal(snapshot)
	}
	if err := snapshot.Validate(); err != nil {
		t.Fatal(err)
	}
}
