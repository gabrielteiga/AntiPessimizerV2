package history

import (
	"sync"
	"testing"
	"time"

	"github.com/gabrielteiga/AntiPessimizerV2/internal/aggregation"
	"github.com/gabrielteiga/AntiPessimizerV2/internal/profiling"
)

func snapshots(t *testing.T) []profiling.Snapshot {
	t.Helper()
	start := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	a, err := aggregation.New(aggregation.Config{SessionID: "test", Target: profiling.Target{PID: 1, Start: start, Image: "lab"}, Source: "synthetic", Start: start, Width: time.Second, Capacity: 10, TopN: 10})
	if err != nil {
		t.Fatal(err)
	}
	result := []profiling.Snapshot{}
	for i := 0; i < 4; i++ {
		for tid := uint32(1); tid <= 2; tid++ {
			err := a.Add(profiling.Sample{At: a.End().Add(-time.Second), ThreadID: tid, Location: profiling.Location{ModuleID: "hash", Module: "lab", RVA: "0x0000000000000001", Status: "unknown", Reason: "no_symbol"}})
			if err != nil {
				t.Fatal(err)
			}
		}
		s, err := a.Close(a.End())
		if err != nil {
			t.Fatal(err)
		}
		result = append(result, s)
	}
	return result
}
func TestRetentionOwnershipAndFilters(t *testing.T) {
	ss := snapshots(t)
	for _, tc := range []struct {
		name        string
		count, rows int
		age         time.Duration
	}{{"count", 2, 20, time.Hour}, {"rows", 10, 4, time.Hour}, {"age", 10, 20, 2 * time.Second}} {
		t.Run(tc.name, func(t *testing.T) {
			store, _ := New(tc.count, tc.rows, tc.age)
			for _, s := range ss {
				if err := store.Append(s); err != nil {
					t.Fatal(err)
				}
			}
			got, err := store.Query(ss[0].WindowStart, ss[3].WindowEnd, "", 0)
			if err != nil {
				t.Fatal(err)
			}
			if len(got) != 2 || got[0].Sequence != 3 {
				t.Fatalf("retained: %+v", got)
			}
			got[0].Hotspots[0].Samples = 999
			got, err = store.Query(ss[2].WindowStart, ss[2].WindowEnd, "lab", 1)
			if err != nil {
				t.Fatal(err)
			}
			if len(got) != 1 || len(got[0].Hotspots) != 1 || got[0].Hotspots[0].Samples != 1 || got[0].OtherSamples != 1 {
				t.Fatal("ownership or filter failure")
			}
			if err := got[0].Validate(); err != nil {
				t.Fatal(err)
			}
			if err := store.Append(ss[0]); err == nil {
				t.Fatal("accepted sequence regression")
			}
		})
	}
	store, _ := New(2, 10, time.Hour)
	s := ss[0].Clone()
	if err := store.Append(s); err != nil {
		t.Fatal(err)
	}
	s.Hotspots[0].Samples = 50
	got, _ := store.Query(s.WindowStart, s.WindowEnd, "", 0)
	if got[0].Hotspots[0].Samples != 1 {
		t.Fatal("store aliases input")
	}
}
func TestConcurrentQueries(t *testing.T) {
	store, _ := New(4, 20, time.Hour)
	ss := snapshots(t)
	var wg sync.WaitGroup
	for i := 0; i < 4; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < 100; j++ {
				if _, err := store.Query(ss[0].WindowStart, ss[3].WindowEnd, "", 0); err != nil {
					t.Error(err)
				}
			}
		}()
	}
	for _, s := range ss {
		if err := store.Append(s); err != nil {
			t.Fatal(err)
		}
	}
	wg.Wait()
}
