package desktop

import (
	"testing"

	"github.com/gabrielteiga/AntiPessimizerV2/internal/instrumentation"
	"github.com/gabrielteiga/AntiPessimizerV2/internal/report"
)

func TestMetricComparisonPresentationRetainsExactCallsAndTimeUnits(t *testing.T) {
	records := metricRecords(t)
	comparison, err := instrumentation.Compare(records[:1], records)
	if err != nil {
		t.Fatal(err)
	}
	var model Model
	if err = model.ReplaceMetrics(records); err != nil {
		t.Fatal(err)
	}
	model.MetricComparison = &comparison
	model.SortColumn = 2
	if err = model.Rebuild(0, report.Filter{}); err != nil {
		t.Fatal(err)
	}
	if len(model.Headers()) != 17 || model.Rows[0][0] != "1" || model.Rows[0][1] != "2" || model.Rows[0][2] != "+1" || model.Rows[0][5] != "-5.000000" || model.Rows[0][13] != "Lab.Parent" {
		t.Fatal(model.Rows)
	}
	model.MetricUnit = Nanoseconds
	if err = model.Rebuild(0, report.Filter{Text: "parent"}); err != nil || len(model.Rows) != 1 || model.Rows[0][5] != "-5000000.000000" {
		t.Fatal(model.Rows, err)
	}
	if err = model.Rebuild(0, report.Filter{Thread: 7}); err == nil {
		t.Fatal("reused thread IDs compared across runs")
	}
	model.MetricUnit = ClockTicks
	comparison.After.Frequency++
	if err = model.Rebuild(0, report.Filter{}); err == nil {
		t.Fatal("raw tick difference with different frequencies accepted")
	}
}
