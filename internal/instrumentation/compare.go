package instrumentation

import (
	"cmp"
	"encoding/hex"
	"errors"
	"fmt"
	"sort"
	"strings"

	"github.com/gabrielteiga/AntiPessimizerV2/internal/profiling"
)

type ProcedureTotals struct {
	Calls              uint64   `json:"calls,string"`
	Unwound            uint64   `json:"unwound_calls,string"`
	InclusiveTicks     uint64   `json:"inclusive_ticks,string"`
	ExclusiveTicks     uint64   `json:"exclusive_ticks,string"`
	InclusiveNS        uint64   `json:"inclusive_ns,string"`
	ExclusiveNS        uint64   `json:"exclusive_ns,string"`
	AverageInclusiveNS *float64 `json:"average_inclusive_ns"`
	AverageExclusiveNS *float64 `json:"average_exclusive_ns"`
}
type ComparisonChange struct {
	profiling.Location
	Before                  ProcedureTotals `json:"before"`
	After                   ProcedureTotals `json:"after"`
	CallDelta               string          `json:"call_delta"`
	AverageExclusiveDeltaNS *float64        `json:"average_exclusive_delta_ns"`
	AverageInclusiveDeltaNS *float64        `json:"average_inclusive_delta_ns"`
}
type ComparisonPoint struct {
	SessionID string `json:"session_id"`
	Sequence  uint64 `json:"sequence"`
	Epoch     uint64 `json:"clear_epoch"`
	Frequency uint64 `json:"frequency_hz,string"`
	At        uint64 `json:"observed_at_ticks,string"`
}
type Comparison struct {
	Protocol string             `json:"protocol"`
	Version  int                `json:"version"`
	Mode     string             `json:"mode"`
	Source   string             `json:"source"`
	Clock    string             `json:"clock"`
	Before   ComparisonPoint    `json:"before"`
	After    ComparisonPoint    `json:"after"`
	Changes  []ComparisonChange `json:"changes"`
}

// Compare uses only the last cumulative snapshot of each archive. Procedures
// are aggregated across thread instances; means use summed ticks / summed calls,
// never averages of per-thread averages. Closed, loss-free snapshots with the
// same complete selection are required. Capture lengths/workloads may differ.
func Compare(before, after []Record) (Comparison, error) {
	a, err := comparisonRecord(before)
	if err != nil {
		return Comparison{}, fmt.Errorf("before: %w", err)
	}
	b, err := comparisonRecord(after)
	if err != nil {
		return Comparison{}, fmt.Errorf("after: %w", err)
	}
	if a.Source != b.Source || a.Clock != b.Clock {
		return Comparison{}, errors.New("instrumentation method/clock differs")
	}
	identity := func(p profiling.Location) string { return p.ModuleID + ":" + p.RVA }
	selected := map[string]profiling.Location{}
	for _, p := range a.Selection {
		selected[identity(p.Location)] = p.Location
	}
	if len(a.Selection) != len(b.Selection) {
		return Comparison{}, errors.New("instrumented selections differ")
	}
	for _, p := range b.Selection {
		if previous, ok := selected[identity(p.Location)]; !ok || previous != p.Location {
			return Comparison{}, errors.New("binary, symbols or instrumented selection differs")
		}
	}
	x, err := aggregateComparison(a)
	if err != nil {
		return Comparison{}, err
	}
	y, err := aggregateComparison(b)
	if err != nil {
		return Comparison{}, err
	}
	point := func(r Record) ComparisonPoint {
		return ComparisonPoint{r.SessionID, r.Sequence, r.Metrics.Epoch, r.Metrics.Frequency, r.At}
	}
	result := Comparison{Protocol: MetricsProtocol + ".comparison", Version: 1, Mode: "instrumentation", Source: a.Source, Clock: a.Clock, Before: point(a), After: point(b)}
	for key, location := range selected {
		old, next := x[key], y[key]
		change := ComparisonChange{Location: location, Before: old, After: next, CallDelta: signedDifference(next.Calls, old.Calls)}
		if old.Calls > 0 && next.Calls > 0 {
			exclusive := *next.AverageExclusiveNS - *old.AverageExclusiveNS
			inclusive := *next.AverageInclusiveNS - *old.AverageInclusiveNS
			change.AverageExclusiveDeltaNS = &exclusive
			change.AverageInclusiveDeltaNS = &inclusive
		}
		result.Changes = append(result.Changes, change)
	}
	sort.Slice(result.Changes, func(i, j int) bool {
		a, b := result.Changes[i], result.Changes[j]
		if a.ModuleID != b.ModuleID {
			return a.ModuleID < b.ModuleID
		}
		return a.RVA < b.RVA
	})
	return result, nil
}
func comparisonRecord(records []Record) (Record, error) {
	if err := ValidateRecords(records); err != nil {
		return Record{}, err
	}
	r := records[len(records)-1]
	if r.Version < 2 {
		return Record{}, errors.New("archive lacks complete procedure selection; capture again with metrics v2 or newer")
	}
	if r.Metrics.Incomplete || r.Metrics.OpenCalls != 0 || r.Metrics.AbandonedCalls != 0 {
		return Record{}, errors.New("comparison requires closed calls and no capture loss")
	}
	if r.Source == "native-hook" {
		for _, p := range r.Selection {
			hash := strings.TrimPrefix(p.Location.ModuleID, "sha256:")
			if len(hash) != 64 || hash == p.Location.ModuleID {
				return Record{}, errors.New("comparison requires a verified binary hash")
			}
			if _, err := hex.DecodeString(hash); err != nil {
				return Record{}, errors.New("invalid binary hash")
			}
		}
	}
	return r, nil
}
func aggregateComparison(r Record) (map[string]ProcedureTotals, error) {
	result := map[string]ProcedureTotals{}
	for _, p := range r.Selection {
		result[p.Location.ModuleID+":"+p.Location.RVA] = ProcedureTotals{}
	}
	for _, row := range r.Metrics.Rows {
		key := row.ModuleID + ":" + row.RVA
		t := result[key]
		fields := []struct {
			out   *uint64
			value uint64
		}{{&t.Calls, row.Calls}, {&t.Unwound, row.UnwoundCalls}, {&t.InclusiveTicks, row.InclusiveTicks}, {&t.ExclusiveTicks, row.ExclusiveTicks}}
		for _, field := range fields {
			value, err := profiling.Sum(*field.out, field.value)
			if err != nil {
				return nil, fmt.Errorf("comparison total overflow: %w", err)
			}
			*field.out = value
		}
		result[key] = t
	}
	for key, t := range result {
		var err error
		t.InclusiveNS, err = NanosecondsFromTicks(t.InclusiveTicks, r.Metrics.Frequency)
		if err != nil {
			return nil, err
		}
		t.ExclusiveNS, err = NanosecondsFromTicks(t.ExclusiveTicks, r.Metrics.Frequency)
		if err != nil {
			return nil, err
		}
		if t.Calls > 0 {
			inclusive := float64(t.InclusiveTicks) * 1e9 / float64(r.Metrics.Frequency) / float64(t.Calls)
			exclusive := float64(t.ExclusiveTicks) * 1e9 / float64(r.Metrics.Frequency) / float64(t.Calls)
			t.AverageInclusiveNS = &inclusive
			t.AverageExclusiveNS = &exclusive
		}
		result[key] = t
	}
	return result, nil
}
func signedDifference(after, before uint64) string {
	if after < before {
		return fmt.Sprintf("-%d", before-after)
	}
	return fmt.Sprintf("+%d", after-before)
}

// CompareCallDelta sorts exact signed differences without int64/float overflow.
func CompareCallDelta(a, b ComparisonChange) int {
	negativeA, negativeB := a.After.Calls < a.Before.Calls, b.After.Calls < b.Before.Calls
	if negativeA != negativeB {
		if negativeA {
			return -1
		}
		return 1
	}
	var magnitudeA, magnitudeB uint64
	if negativeA {
		magnitudeA = a.Before.Calls - a.After.Calls
		magnitudeB = b.Before.Calls - b.After.Calls
	} else {
		magnitudeA = a.After.Calls - a.Before.Calls
		magnitudeB = b.After.Calls - b.Before.Calls
	}
	order := cmp.Compare(magnitudeA, magnitudeB)
	if negativeA {
		return -order
	}
	return order
}
