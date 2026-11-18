package desktop

import (
	"reflect"
	"strings"
	"testing"

	"github.com/gabrielteiga/AntiPessimizerV2/internal/report"
)

func TestMetricUnitsChangePresentationWithoutChangingCountsOrderOrArchive(t *testing.T) {
	records := metricRecords(t)
	var m Model
	if err := m.ReplaceMetrics(records); err != nil {
		t.Fatal(err)
	}
	before := m.Metrics[len(m.Metrics)-1].Clone()
	cases := []struct {
		unit           MetricTimeUnit
		total, average string
	}{
		{Milliseconds, "30.000000", "15.000000"},
		{Microseconds, "30000.000", "15000.000000"},
		{Nanoseconds, "30000000", "15000000.000000"},
		{Seconds, "0.030000000", "0.015000000"},
		{ClockTicks, "30", "15.000000"},
	}
	for _, c := range cases {
		m.MetricUnit = c.unit
		if err := m.Rebuild(0, report.Filter{}); err != nil {
			t.Fatal(err)
		}
		if m.Rows[0][0] != "2" || m.Rows[0][1] != c.total || m.Rows[0][2] != c.average || m.Rows[0][8] != "Lab.Parent" {
			t.Fatal(c.unit, m.Rows)
		}
		if !strings.Contains(m.Headers()[1], "("+string(c.unit)+")") {
			t.Fatal("unit missing from header", m.Headers())
		}
		if !reflect.DeepEqual(before, m.Metrics[len(m.Metrics)-1]) {
			t.Fatal("display changed measured snapshot")
		}
	}
	m.MetricUnit = "cycles"
	if err := m.Rebuild(0, report.Filter{}); err == nil {
		t.Fatal("QPC ticks mislabeled as CPU cycles")
	}
}
func TestUnitTotalsPreserveIntegersAboveFloatPrecision(t *testing.T) {
	const ns = uint64(9007199254740993)
	for unit, want := range map[MetricTimeUnit]string{Nanoseconds: "9007199254740993", Microseconds: "9007199254740.993", Milliseconds: "9007199254.740993", Seconds: "9007199.254740993", ClockTicks: "18446744073709551615"} {
		if got := unit.total(ns, ^uint64(0)); got != want {
			t.Fatal(unit, got, want)
		}
	}
	if got := ClockTicks.average(0, 0, 0); got != "0.000000" {
		t.Fatal("open-call average is not zero", got)
	}
}
