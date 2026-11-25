package instrumentation

import (
	"math"
	"strings"
	"testing"
	"time"
)

func comparisonRow(row Row, calls, inclusive, exclusive, frequency uint64) Row {
	row.Calls = calls
	row.InclusiveTicks = inclusive
	row.ExclusiveTicks = exclusive
	row.InclusiveNS, _ = NanosecondsFromTicks(inclusive, frequency)
	row.ExclusiveNS, _ = NanosecondsFromTicks(exclusive, frequency)
	row.AverageInclusiveNS = float64(inclusive) * 1e9 / float64(frequency) / float64(calls)
	row.AverageExclusiveNS = float64(exclusive) * 1e9 / float64(frequency) / float64(calls)
	return row
}
func TestComparisonUsesLastEpochSnapshotAndWeightedThreadMeans(t *testing.T) {
	final := metricFixture(t)
	older := final.Clone()
	older.At--
	older.Metrics.Rows[0] = comparisonRow(older.Metrics.Rows[0], 1, 30, 20, older.Metrics.Frequency)
	final.Sequence = older.Sequence + 1
	after := final.Clone()
	after.SessionID = "another-run"
	after.Target.PID++
	after.Target.Start = after.Target.Start.Add(time.Hour)
	parent := after.Metrics.Rows[0]
	one := comparisonRow(parent, 1, 10, 10, after.Metrics.Frequency)
	three := comparisonRow(parent, 3, 90, 60, after.Metrics.Frequency)
	three.ThreadID++
	after.Metrics.Rows = []Row{one, three} // Child was selected but never called.
	for i := range after.Selection {
		after.Selection[i].ID += 10
	}
	for i := range after.Metrics.Rows {
		after.Metrics.Rows[i].ProcedureID += 10
	}
	comparison, err := Compare([]Record{older, final}, []Record{after})
	if err != nil {
		t.Fatal(err)
	}
	parentChange, child := comparison.Changes[0], comparison.Changes[1]
	if parentChange.Before.Calls != 2 || parentChange.After.Calls != 4 || parentChange.CallDelta != "+2" || *parentChange.After.AverageExclusiveNS != 17.5e6 || *parentChange.AverageExclusiveDeltaNS != 2.5e6 {
		t.Fatal(parentChange)
	}
	if child.After.Calls != 0 || child.CallDelta != "-1" || child.After.AverageExclusiveNS != nil || child.AverageExclusiveDeltaNS != nil {
		t.Fatal("uncalled selection fabricated a measured zero average", child)
	}
	if comparison.Before.Sequence != final.Sequence || comparison.After.SessionID != "another-run" {
		t.Fatal("comparison lost as-of identity", comparison)
	}
}
func TestComparisonNormalizesFrequencyAndRejectsIncompatibleCaptures(t *testing.T) {
	a := metricFixture(t)
	b := a.Clone()
	b.Metrics.Frequency *= 2
	for i, row := range b.Metrics.Rows {
		b.Metrics.Rows[i] = comparisonRow(row, row.Calls, row.InclusiveTicks*2, row.ExclusiveTicks*2, b.Metrics.Frequency)
	}
	result, err := Compare([]Record{a}, []Record{b})
	if err != nil || *result.Changes[0].AverageExclusiveDeltaNS != 0 {
		t.Fatal(result, err)
	}
	for name, mutate := range map[string]func(*Record){
		"legacy":              func(r *Record) { r.Version = 1; r.Selection = nil },
		"open":                func(r *Record) { r.Metrics.OpenCalls = 1 },
		"loss":                func(r *Record) { r.Metrics.Incomplete = true },
		"different selection": func(r *Record) { r.Selection = r.Selection[:1]; r.Metrics.Rows = r.Metrics.Rows[:1] },
		"different symbols": func(r *Record) {
			r.Selection[0].Location.Procedure = "Changed"
			r.Metrics.Rows[0].Procedure = "Changed"
		},
		"different binary": func(r *Record) {
			for i := range r.Selection {
				r.Selection[i].Location.ModuleID = "other-build"
			}
			for i := range r.Metrics.Rows {
				r.Metrics.Rows[i].ModuleID = "other-build"
			}
		},
		"unstable native identity": func(r *Record) { r.Source = "native-hook"; r.Clock = "qpc" },
	} {
		t.Run(name, func(t *testing.T) {
			bad := a.Clone()
			mutate(&bad)
			if _, err := Compare([]Record{a}, []Record{bad}); err == nil {
				t.Fatal("incompatible comparison accepted")
			}
		})
	}
}
func TestComparisonRejectsAggregateOverflowAndSortsFullSignedCountRange(t *testing.T) {
	r := metricFixture(t)
	one := comparisonRow(r.Metrics.Rows[0], math.MaxUint64, 1, 1, r.Metrics.Frequency)
	two := comparisonRow(one, 1, 1, 1, r.Metrics.Frequency)
	two.ThreadID++
	r.Metrics.Rows = []Row{one, two}
	if _, err := Compare([]Record{r}, []Record{r}); err == nil || !strings.Contains(err.Error(), "overflow") {
		t.Fatal(err)
	}
	increase := ComparisonChange{Before: ProcedureTotals{}, After: ProcedureTotals{Calls: math.MaxUint64}}
	decrease := ComparisonChange{Before: ProcedureTotals{Calls: math.MaxUint64}, After: ProcedureTotals{}}
	if CompareCallDelta(increase, decrease) <= 0 || signedDifference(0, math.MaxUint64) != "-18446744073709551615" || signedDifference(math.MaxUint64, 0) != "+18446744073709551615" {
		t.Fatal("signed delta overflowed")
	}
}

func TestComparisonRejectsDifferentNativeClockAndKeepsInputsDetached(t *testing.T) {
	a := metricFixture(t)
	for i := range a.Selection {
		a.Selection[i].Location.ModuleID = "sha256:" + strings.Repeat("a", 64)
	}
	for i := range a.Metrics.Rows {
		a.Metrics.Rows[i].ModuleID = "sha256:" + strings.Repeat("a", 64)
	}
	a.Source = "native-hook"
	a.Clock = "qpc"
	b := a.Clone()
	b.Clock = "tsc"
	if _, err := Compare([]Record{a}, []Record{b}); err == nil || !strings.Contains(err.Error(), "clock") {
		t.Fatal(err)
	}
	result, err := Compare([]Record{a}, []Record{a})
	if err != nil {
		t.Fatal(err)
	}
	result.Changes[0].Before.Calls = 999
	*result.Changes[0].After.AverageInclusiveNS = 999
	if a.Metrics.Rows[0].Calls != 2 || a.Metrics.Rows[0].AverageInclusiveNS == 999 {
		t.Fatal("comparison aliases measured archive")
	}
}
