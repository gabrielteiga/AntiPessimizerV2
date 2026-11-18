package instrumentation

import (
	"bytes"
	"strings"
	"testing"
)

func TestNativeLossV3RoundTripAndMonotonicCountAcrossClear(t *testing.T) {
	r := metricFixture(t)
	r.Version, r.Source, r.Clock = 3, "native-hook", "qpc"
	r.NativeLostEvents = 1<<60 + 17
	r.Metrics.Incomplete = true
	var wire bytes.Buffer
	if err := WriteRecord(&wire, r); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(wire.String(), `"native_lost_event_count":"1152921504606846993"`) {
		t.Fatal("loss count lost integer precision", wire.String())
	}
	got, err := NewMetricsReader(&wire).Next()
	if err != nil || got.NativeLostEvents != r.NativeLostEvents || !got.Metrics.Incomplete {
		t.Fatal("native loss did not survive archive", got, err)
	}
	for _, clear := range []bool{false, true} {
		next := r.Clone()
		next.Sequence++
		if clear {
			next.Metrics.Epoch++
		}
		if err := ValidateRecords([]Record{r, next}); err != nil {
			t.Fatal("unchanged native loss should remain valid", clear, err)
		}
		next.NativeLostEvents--
		if err := ValidateRecords([]Record{r, next}); err == nil {
			t.Fatal("cumulative native loss regressed", clear)
		}
	}
	for name, mutate := range map[string]func(*Record){
		"legacy":    func(r *Record) { r.Version = 2 },
		"synthetic": func(r *Record) { r.Source, r.Clock = "synthetic", "synthetic" },
		"complete":  func(r *Record) { r.Metrics.Incomplete = false },
		"selection": func(r *Record) { r.Selection = nil },
	} {
		bad := r.Clone()
		mutate(&bad)
		if bad.Validate() == nil {
			t.Fatal("invalid loss diagnostics accepted", name)
		}
	}
}

func TestComparisonAcceptsV2AndV3CompleteCaptures(t *testing.T) {
	before := metricFixture(t)
	after := before.Clone()
	after.Version = 3
	if _, err := Compare([]Record{before}, []Record{after}); err != nil {
		t.Fatal("v3 lost complete selection compatibility", err)
	}
}
