package desktop

import (
	"bytes"
	"context"
	"os"
	"strings"
	"testing"

	"github.com/gabrielteiga/AntiPessimizerV2/internal/instrumentation"
	"github.com/gabrielteiga/AntiPessimizerV2/internal/report"
)

func metricRecords(t *testing.T) []instrumentation.Record {
	t.Helper()
	b, err := os.ReadFile("../../testdata/instrumentation/nested.ndjson")
	if err != nil {
		t.Fatal(err)
	}
	marker := `{"type":"enter","thread_id":7,"procedure_id":1,"ticks":"50"}`
	events := strings.Replace(string(b), marker, "{\"type\":\"snapshot\",\"ticks\":\"45\"}\n"+marker, 1)
	var wire bytes.Buffer
	if err := instrumentation.Replay(context.Background(), strings.NewReader(events), &wire, instrumentation.Config{MaxThreads: 16, MaxDepth: 32, MaxRows: 64}); err != nil {
		t.Fatal(err)
	}
	records, err := instrumentation.ReadMetrics(&wire)
	if err != nil {
		t.Fatal(err)
	}
	return records
}

func TestMetricModelCumulativeCursorClearAndFilters(t *testing.T) {
	records := metricRecords(t)
	var m Model
	if err := m.ReplaceMetrics(records); err != nil {
		t.Fatal(err)
	}
	records[1].Metrics.Rows[0].Procedure = "caller mutation"
	if err := m.Rebuild(120, report.Filter{}); err != nil {
		t.Fatal(err)
	}
	if len(m.Rows) != 2 || len(m.Headers()) != 13 || m.Rows[0][0] != "2" || m.Rows[0][1] != "30.000000" || m.Rows[0][2] != "15.000000" || m.Rows[0][3] != "40.000000" || m.Rows[0][4] != "20.000000" || m.Rows[0][8] != "Lab.Parent" {
		t.Fatal(m.Rows)
	}
	m.At = 1
	m.Rebuild(1, report.Filter{Thread: 7, Text: "parent", Unit: "Lab"})
	if len(m.Rows) != 1 || m.Rows[0][0] != "1" || m.Rows[0][3] != "30.000000" {
		t.Fatal(m.Rows)
	}
	m.Rebuild(120, report.Filter{Thread: 8})
	if len(m.Rows) != 0 {
		t.Fatal("thread filter ignored")
	}
	clear := m.Metrics[1].Clone()
	clear.Sequence = 4
	clear.At = 70
	clear.Metrics.Epoch = 1
	clear.Metrics.Rows = nil
	if err := m.ReplaceMetrics(append(m.Metrics, clear)); err != nil {
		t.Fatal(err)
	}
	m.Rebuild(120, report.Filter{})
	if len(m.Rows) != 0 || m.Omitted != 1 {
		t.Fatal("clear mixed old totals", m.Rows, m.Omitted)
	}
	m.At = 3 // omitted snapshot: last known cumulative snapshot at or before cursor
	m.Rebuild(120, report.Filter{})
	if len(m.Rows) != 2 || m.Rows[0][0] != "2" || m.HistoryLength() != 3 || m.SequenceAt(2) != 4 {
		t.Fatal(m.Rows)
	}
	bad := clear.Clone()
	bad.Sequence = 5
	bad.SessionID = "another"
	if err := m.ReplaceMetrics([]instrumentation.Record{clear, bad}); err == nil || m.Last != 4 {
		t.Fatal("invalid history changed model")
	}
}

func TestMetricModelNumericSortingAndThreadReuse(t *testing.T) {
	r := metricRecords(t)[1]
	// Adjacent counts above float64's exact integer range must still sort correctly.
	r.Metrics.Rows[0].Calls = 1 << 53
	r.Metrics.Rows[1].Calls = 1<<53 + 1
	for i := range r.Metrics.Rows {
		row := &r.Metrics.Rows[i]
		row.AverageInclusiveNS = float64(row.InclusiveTicks) * 1e9 / float64(r.Metrics.Frequency) / float64(row.Calls)
		row.AverageExclusiveNS = float64(row.ExclusiveTicks) * 1e9 / float64(r.Metrics.Frequency) / float64(row.Calls)
	}
	var m Model
	if err := m.ReplaceMetrics([]instrumentation.Record{r}); err != nil {
		t.Fatal(err)
	}
	m.SortColumn = 0
	m.Rebuild(1, report.Filter{})
	if m.Rows[0][8] != "Lab.Child" {
		t.Fatal("counts rounded during sorting", m.Rows)
	}
	m.SortColumn = 3
	m.Ascending = true
	m.Rebuild(1, report.Filter{})
	if m.Rows[0][8] != "Lab.Child" {
		t.Fatal("times sorted lexically", m.Rows)
	}
	m.SortColumn = 2
	m.Ascending = false
	m.Rebuild(1, report.Filter{})
	if m.Rows[0][8] != "Lab.Parent" {
		t.Fatal("average sorting incorrect", m.Rows)
	}
	r = metricRecords(t)[1]
	reused := r.Metrics.Rows[0]
	reused.ThreadInstance = 60
	r.Metrics.Rows = append(r.Metrics.Rows, reused)
	if err := m.ReplaceMetrics([]instrumentation.Record{r}); err != nil {
		t.Fatal(err)
	}
	m.Rebuild(1, report.Filter{Text: "parent"})
	if len(m.Rows) != 2 || m.Rows[0][6] == m.Rows[1][6] {
		t.Fatal("thread lifetimes merged", m.Rows)
	}
}

func TestMetricArchiveProbeAndPreservedProvisionalState(t *testing.T) {
	r := metricRecords(t)[1]
	r.Metrics.OpenCalls = 2
	r.Metrics.Rows[0].ActiveFrames = 1
	r.Metrics.Incomplete = true
	r.Metrics.AbandonedCalls = 3
	var wire bytes.Buffer
	if err := instrumentation.WriteRecord(&wire, r); err != nil {
		t.Fatal(err)
	}
	history, records, err := readArchive(bytes.NewReader(wire.Bytes()))
	if err != nil || len(history) != 0 || len(records) != 1 {
		t.Fatalf("%v %v %v", history, records, err)
	}
	var m Model
	if err := m.ReplaceMetrics(records); err != nil {
		t.Fatal(err)
	}
	m.Rebuild(1, report.Filter{})
	got, ok := m.CurrentMetrics()
	if !ok || !got.Metrics.Incomplete || got.Metrics.OpenCalls != 2 || got.Metrics.AbandonedCalls != 3 || m.Rows[0][12] != "1" {
		t.Fatal(got, m.Rows)
	}
	for _, input := range []string{`{"protocol":"other"}`, `{"protocol":"antipessimizer.instrumentation.events"}`, wire.String() + "{}"} {
		if _, _, err := readArchive(strings.NewReader(input)); err == nil {
			t.Fatal("invalid archive accepted")
		}
	}
}
