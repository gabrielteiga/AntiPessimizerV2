package instrumentation

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"math"
	"os"
	"strings"
	"testing"
)

func metricFixture(t *testing.T) Record {
	t.Helper()
	var wire bytes.Buffer
	if err := Replay(context.Background(), strings.NewReader(input(t)), &wire, limits()); err != nil {
		t.Fatal(err)
	}
	var r Record
	if err := json.Unmarshal(wire.Bytes(), &r); err != nil {
		t.Fatal(err)
	}
	return r
}

func TestMetricsValidation(t *testing.T) {
	base := metricFixture(t)
	if err := base.Validate(); err != nil {
		t.Fatal(err)
	}
	for name, change := range map[string]func(*Record){
		"sampling":           func(r *Record) { r.Metrics.Mode = "sampling" },
		"protocol":           func(r *Record) { r.Protocol = EventProtocol },
		"source clock":       func(r *Record) { r.Source = "native-hook" },
		"no frequency":       func(r *Record) { r.Metrics.Frequency = 0 },
		"wrong conversion":   func(r *Record) { r.Metrics.Rows[0].InclusiveNS++ },
		"wrong average":      func(r *Record) { r.Metrics.Rows[0].AverageExclusiveNS++ },
		"negative average":   func(r *Record) { r.Metrics.Rows[0].AverageExclusiveNS = -1e-12 },
		"nonfinite average":  func(r *Record) { r.Metrics.Rows[0].AverageExclusiveNS = math.NaN() },
		"zero calls":         func(r *Record) { r.Metrics.Rows[0].Calls = 0 },
		"unwound subset":     func(r *Record) { r.Metrics.Rows[0].UnwoundCalls = 3 },
		"unresolved":         func(r *Record) { r.Metrics.Rows[0].Status = "unknown" },
		"future thread":      func(r *Record) { r.Metrics.Rows[0].ThreadInstance = r.At + 1 },
		"duplicate row":      func(r *Record) { r.Metrics.Rows = append(r.Metrics.Rows, r.Metrics.Rows[0]) },
		"procedure identity": func(r *Record) { r.Metrics.Rows[1].ProcedureID = r.Metrics.Rows[0].ProcedureID },
		"location alias":     func(r *Record) { r.Metrics.Rows[1].Location = r.Metrics.Rows[0].Location },
		"open mismatch":      func(r *Record) { r.Metrics.Rows[0].ActiveFrames = 1 },
		"hidden loss":        func(r *Record) { r.Metrics.AbandonedCalls = 1 },
	} {
		t.Run(name, func(t *testing.T) {
			r := base.Clone()
			change(&r)
			if r.Validate() == nil {
				t.Fatal("invalid metric accepted")
			}
		})
	}
}

func TestMetricsReaderOrderAndIsolation(t *testing.T) {
	base := metricFixture(t)
	for name, change := range map[string]func(*Record){
		"duplicate sequence": func(r *Record) { r.Sequence = 1 },
		"session":            func(r *Record) { r.SessionID = "another" },
		"target":             func(r *Record) { r.Target.PID++ },
		"clock":              func(r *Record) { r.Source, r.Clock = "native-hook", "qpc" },
		"time":               func(r *Record) { r.At-- },
		"renamed procedure":  func(r *Record) { r.Metrics.Rows[0].Procedure = "Changed" },
	} {
		t.Run(name, func(t *testing.T) {
			var wire bytes.Buffer
			first := base.Clone()
			second := base.Clone()
			second.Sequence = 2
			change(&second)
			// Encode directly to exercise the input boundary independently of WriteRecord.
			json.NewEncoder(&wire).Encode(first)
			json.NewEncoder(&wire).Encode(second)
			reader := NewMetricsReader(&wire)
			got, err := reader.Next()
			if err != nil {
				t.Fatal(err)
			}
			got.Metrics.Rows[0].Procedure = "caller mutation"
			if _, err = reader.Next(); err == nil {
				t.Fatal("incompatible stream accepted")
			}
		})
	}
	var wire bytes.Buffer
	if err := WriteRecord(&wire, base); err != nil {
		t.Fatal(err)
	}
	second := base.Clone()
	second.Sequence = 3
	second.Metrics.Epoch = 1
	if err := WriteRecord(&wire, second); err != nil {
		t.Fatal(err)
	}
	reader := NewMetricsReader(&wire)
	r, err := reader.Next()
	if err != nil {
		t.Fatal(err)
	}
	r.Metrics.Rows[0].Procedure = "caller mutation"
	if _, err := reader.Next(); err != nil {
		t.Fatal(err)
	}
	if _, err := reader.Next(); err != io.EOF {
		t.Fatal(err)
	}
	second.Sequence++
	second.Metrics.Epoch = 0
	base.Metrics.Epoch = 1
	wire.Reset()
	WriteRecord(&wire, base)
	WriteRecord(&wire, second)
	reader = NewMetricsReader(&wire)
	reader.Next()
	if _, err := reader.Next(); err == nil {
		t.Fatal("backwards clear accepted")
	}
}

func TestReadMetricsRejectsInvalidAndEmptyArchives(t *testing.T) {
	base := metricFixture(t)
	b, _ := json.Marshal(base)
	for _, data := range []string{"", "{}", string(b) + "\n{}", strings.Replace(string(b), `"protocol":`, `"typo":true,"protocol":`, 1), string(b) + " {}", strings.Repeat("x", MaxFrameBytes+1)} {
		if _, err := ReadMetrics(strings.NewReader(data)); err == nil {
			t.Fatal("invalid archive accepted")
		}
	}
	var wire bytes.Buffer
	WriteRecord(&wire, base)
	records, err := ReadMetrics(&wire)
	if err != nil || len(records) != 1 || records[0].Metrics.Rows[0].Calls != 2 {
		t.Fatalf("%v %v", records, err)
	}
}

type shortMetricWriter struct{}

func (shortMetricWriter) Write(p []byte) (int, error) { return len(p) - 1, nil }
func TestMetricsShortWrite(t *testing.T) {
	if err := WriteRecord(shortMetricWriter{}, metricFixture(t)); err != io.ErrShortWrite {
		t.Fatal(err)
	}
}

func TestMetricsArchiveSnapshotLimit(t *testing.T) {
	r := metricFixture(t)
	r.Metrics.Rows = nil
	var wire bytes.Buffer
	for i := 1; i <= 4097; i++ {
		r.Sequence = uint64(i)
		if err := WriteRecord(&wire, r); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := ReadMetrics(&wire); err == nil {
		t.Fatal("unbounded archive accepted")
	}
}

func FuzzMetricsReader(f *testing.F) {
	f.Add([]byte(`{"protocol":"antipessimizer.instrumentation.metrics","version":1}`))
	events, err := os.ReadFile("../../testdata/instrumentation/nested.ndjson")
	if err != nil {
		f.Fatal(err)
	}
	var wire bytes.Buffer
	if err := Replay(context.Background(), bytes.NewReader(events), &wire, limits()); err != nil {
		f.Fatal(err)
	}
	f.Add(wire.Bytes())
	f.Fuzz(func(t *testing.T, b []byte) {
		if len(b) > 1<<20 {
			t.Skip()
		}
		reader := NewMetricsReader(bytes.NewReader(b))
		for i := 0; i < 16; i++ {
			r, err := reader.Next()
			if err != nil {
				return
			}
			if err := r.Validate(); err != nil {
				t.Fatal(err)
			}
		}
	})
}

func TestCumulativeMetricsRejectRegressionAndAllowClear(t *testing.T) {
	base := metricFixture(t)
	base.Metrics.Incomplete = true
	base.Metrics.AbandonedCalls = 2
	for name, change := range map[string]func(*Record){
		"missing row": func(r *Record) { r.Metrics.Rows = r.Metrics.Rows[1:] },
		"call regression": func(r *Record) {
			r.Metrics.Rows[0].Calls = 1
			r.Metrics.Rows[0].AverageExclusiveNS = 30e6
			r.Metrics.Rows[0].AverageInclusiveNS = 40e6
		},
		"loss counter": func(r *Record) { r.Metrics.AbandonedCalls = 1 },
		"loss flag":    func(r *Record) { r.Metrics.AbandonedCalls = 0; r.Metrics.Incomplete = false },
	} {
		t.Run(name, func(t *testing.T) {
			second := base.Clone()
			second.Sequence++
			change(&second)
			if err := second.Validate(); err != nil {
				t.Fatal("invalid regression fixture", err)
			}
			if err := ValidateRecords([]Record{base, second}); err == nil {
				t.Fatal("cumulative regression accepted")
			}
			second.Metrics.Epoch++
			if err := ValidateRecords([]Record{base, second}); err != nil {
				t.Fatal("Clear should allow reset", err)
			}
		})
	}
}

func TestIncrementalMetricValidatorRetainsIdentityAcrossEmptyClear(t *testing.T) {
	first := metricFixture(t)
	var validator MetricValidator
	if err := validator.Accept(first); err != nil {
		t.Fatal(err)
	}
	clear := first.Clone()
	clear.Sequence++
	clear.At++
	clear.Metrics.Epoch++
	clear.Metrics.Rows = nil
	if err := validator.Accept(clear); err != nil {
		t.Fatal(err)
	}
	changed := first.Clone()
	changed.Sequence = clear.Sequence + 1
	changed.At = clear.At + 1
	changed.Metrics.Epoch = clear.Metrics.Epoch
	changed.Metrics.Rows[0].Procedure = "OtherProcedure"
	if err := validator.Accept(changed); err == nil {
		t.Fatal("procedure identity reused after Clear")
	}
}
