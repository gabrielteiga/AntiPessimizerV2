package desktop

import (
	"math"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/gabrielteiga/AntiPessimizerV2/internal/instrumentation"
	"github.com/gabrielteiga/AntiPessimizerV2/internal/report"
)

func TestCallDiscountSaturatesWithoutMultiplicationOverflow(t *testing.T) {
	for _, tc := range []struct{ ticks, calls, discount, want uint64 }{
		{30, 2, 5, 20}, {30, 2, 16, 0}, {30, 0, math.MaxUint64, 30},
		{math.MaxUint64, math.MaxUint64, math.MaxUint64, 0}, {math.MaxUint64, 1, 1, math.MaxUint64 - 1},
		{30, 2, 0, 30},
	} {
		if got := discountedTicks(tc.ticks, tc.calls, tc.discount); got != tc.want {
			t.Fatal(tc, got)
		}
	}
	totals, err := discountedMetricTotals(instrumentation.ProcedureTotals{Calls: 2, InclusiveTicks: 3, ExclusiveTicks: 3}, 2, 1)
	if err != nil || totals.InclusiveTicks != 1 || totals.InclusiveNS != 500000000 || *totals.AverageInclusiveNS != 250000000 {
		t.Fatal(totals, err)
	}
}

func TestCallDiscountPreservesArchiveAndCumulativeHistory(t *testing.T) {
	records := metricRecords(t)
	var m Model
	m.CallDiscountTicks = 5
	if err := m.ReplaceMetrics(records); err != nil {
		t.Fatal(err)
	}
	raw := m.Metrics[len(m.Metrics)-1].Clone()
	for _, tc := range []struct {
		unit           MetricTimeUnit
		total, average string
	}{
		{ClockTicks, "20", "10.000000"}, {Nanoseconds, "20000000", "10000000.000000"},
		{Microseconds, "20000.000", "10000.000000"}, {Milliseconds, "20.000000", "10.000000"}, {Seconds, "0.020000000", "0.010000000"},
	} {
		m.MetricUnit = tc.unit
		if err := m.Rebuild(0, report.Filter{}); err != nil {
			t.Fatal(err)
		}
		if m.Rows[0][0] != "2" || m.Rows[0][1] != tc.total || m.Rows[0][2] != tc.average || m.Rows[0][8] != "Lab.Parent" || !strings.Contains(m.Headers()[1], "ajustado") {
			t.Fatal(tc, m.Rows, m.Headers())
		}
	}
	m.MetricUnit = ClockTicks
	m.At = 1
	if err := m.Rebuild(0, report.Filter{Text: "Parent"}); err != nil || m.Rows[0][3] != "25" {
		t.Fatal(m.Rows, err)
	}
	m.At = 0
	m.CallDiscountTicks = 16
	if err := m.Rebuild(0, report.Filter{}); err != nil || m.Rows[0][1] != "0" || m.Rows[0][3] != "8" {
		t.Fatal(m.Rows, err)
	}
	if !reflect.DeepEqual(raw, m.Metrics[len(m.Metrics)-1]) {
		t.Fatal("display discount changed archive")
	}
	m.CallDiscountTicks = 0
	if err := m.Rebuild(0, report.Filter{}); err != nil || m.Rows[0][1] != "30" || strings.Contains(m.Headers()[1], "ajustado") {
		t.Fatal(m.Rows, err)
	}
	var live Model
	live.CallDiscountTicks = 5
	if err := live.AppendMetrics(records[0]); err != nil || live.CallDiscountTicks != 5 {
		t.Fatal("follow lost discount", err)
	}
	cleared := raw.Clone()
	cleared.Sequence++
	cleared.At++
	cleared.Metrics.Epoch++
	cleared.Metrics.Rows = nil
	if err := live.AppendMetrics(cleared); err != nil {
		t.Fatal(err)
	}
	if err := live.Rebuild(0, report.Filter{}); err != nil || len(live.Rows) != 0 || live.CallDiscountTicks != 5 {
		t.Fatal("Clear lost presentation preference or mixed epochs", live.Rows, err)
	}
}

func TestCallDiscountRecomputesComparisonAfterSaturation(t *testing.T) {
	records := metricRecords(t)
	comparison, err := instrumentation.Compare(records[:1], records)
	if err != nil {
		t.Fatal(err)
	}
	before := *comparison.Changes[0].AverageExclusiveDeltaNS
	m := Model{MetricComparison: &comparison, MetricUnit: Milliseconds, CallDiscountTicks: 16}
	if err := m.Rebuild(0, report.Filter{Text: "Parent"}); err != nil {
		t.Fatal(err)
	}
	if len(m.Rows) != 1 || m.Rows[0][3] != "4.000000" || m.Rows[0][4] != "0.000000" || m.Rows[0][5] != "-4.000000" || m.Rows[0][8] != "-10.000000" {
		t.Fatal(m.Rows)
	}
	if *comparison.Changes[0].AverageExclusiveDeltaNS != before {
		t.Fatal("comparison input changed")
	}
}

func TestCallDiscountInputRange(t *testing.T) {
	for input, want := range map[string]uint64{"": 0, "  5 ": 5, "18446744073709551615": math.MaxUint64} {
		if got, err := parseCallDiscount(input); err != nil || got != want {
			t.Fatal(input, got, err)
		}
	}
	for _, input := range []string{"-1", "1.5", "18446744073709551616"} {
		if _, err := parseCallDiscount(input); err == nil {
			t.Fatal("invalid discount accepted", input)
		}
	}
}

func TestCallDiscountPreferencesPreserveUint64AndLegacyDefault(t *testing.T) {
	path := filepath.Join(t.TempDir(), "viewer.json")
	if err := os.WriteFile(path, []byte(`{"version":1,"time_unit":"ms"}`), 0600); err != nil {
		t.Fatal(err)
	}
	legacy, err := loadPreferences(path)
	if err != nil || legacy.CallDiscountTicks != 0 {
		t.Fatal("legacy preferences changed discount", legacy, err)
	}
	legacy.CallDiscountTicks = math.MaxUint64
	if err := savePreferences(path, legacy); err != nil {
		t.Fatal(err)
	}
	restored, err := loadPreferences(path)
	if err != nil || restored != legacy {
		t.Fatal(restored, err)
	}
	data, err := os.ReadFile(path)
	if err != nil || !strings.Contains(string(data), `"call_discount_ticks": "18446744073709551615"`) {
		t.Fatal(string(data), err)
	}
	if err := os.WriteFile(path, []byte(`{"version":1,"time_unit":"ms","call_discount_ticks":"18446744073709551616"}`), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := loadPreferences(path); err == nil {
		t.Fatal("overflowing discount preference accepted")
	}
}
