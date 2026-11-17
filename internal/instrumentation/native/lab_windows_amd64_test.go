package native

import (
	"bytes"
	"context"
	"errors"
	"os"
	"testing"
	"time"

	"github.com/gabrielteiga/AntiPessimizerV2/internal/instrumentation"
)

func labPath(t *testing.T) string {
	t.Helper()
	p := os.Getenv("AP_NATIVE_LAB")
	if p == "" {
		t.Skip("enabled after building the controlled native producer in Windows CI")
	}
	return p
}
func TestNativeProducerCallsTimesAndThreadIsolation(t *testing.T) {
	path := labPath(t)
	var out bytes.Buffer
	if err := RunLab(context.Background(), LabOptions{Workload: path, Threads: 3, Iterations: 10, Capacity: 1024, Timeout: 10 * time.Second}, &out); err != nil {
		t.Fatal(err, out.String())
	}
	records, err := instrumentation.ReadMetrics(&out)
	if err != nil {
		t.Fatal(err)
	}
	r := records[len(records)-1]
	if r.Source != "native-hook" || r.Clock != "qpc" || r.Metrics.Incomplete || r.Metrics.OpenCalls != 0 || r.Metrics.AbandonedCalls != 0 || len(r.Metrics.Rows) != 12 {
		t.Fatalf("invalid native capture: %+v", r)
	}
	threads := map[uint32]map[uint32]instrumentation.Row{}
	want := map[uint32]uint64{1: 10, 2: 20, 3: 40, 4: 10}
	for _, row := range r.Metrics.Rows {
		if row.Calls != want[row.ProcedureID] || row.ThreadInstance == 0 {
			t.Fatalf("unexpected native count/identity: %+v", row)
		}
		if threads[row.ThreadID] == nil {
			threads[row.ThreadID] = map[uint32]instrumentation.Row{}
		}
		threads[row.ThreadID][row.ProcedureID] = row
	}
	if len(threads) != 3 {
		t.Fatal("native threads merged", threads)
	}
	for _, rows := range threads {
		p, c, recursive, f := rows[1], rows[2], rows[3], rows[4]
		if p.InclusiveTicks == 0 || c.InclusiveTicks == 0 || p.InclusiveTicks != p.ExclusiveTicks+c.InclusiveTicks+recursive.InclusiveTicks+f.InclusiveTicks {
			t.Fatal("native nesting times do not balance", rows)
		}
		if c.ExclusiveTicks != c.InclusiveTicks || recursive.InclusiveTicks != recursive.ExclusiveTicks || f.ExclusiveTicks != f.InclusiveTicks {
			t.Fatal("native leaf/recursive time mismatch", rows)
		}
		if p.AverageInclusiveNS != float64(p.InclusiveTicks)*1e9/float64(r.Metrics.Frequency)/10 {
			t.Fatal("wrong native average", p)
		}
	}
	t.Logf("real native callbacks/QPC: %d rows across %d threads, parent=10 child=20 recursive=40 float=10 per thread", len(r.Metrics.Rows), len(threads))
}
func TestNativeProducerCancellationIsIncomplete(t *testing.T) {
	path := labPath(t)
	var out bytes.Buffer
	err := RunLab(context.Background(), LabOptions{Workload: path, Threads: 2, Iterations: 1000, Capacity: 1024, Timeout: 500 * time.Millisecond}, &out)
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatal("native cancellation not propagated", err)
	}
	records, readErr := instrumentation.ReadMetrics(&out)
	if readErr != nil {
		t.Fatal(readErr, err)
	}
	last := records[len(records)-1]
	if !last.Metrics.Incomplete || last.Metrics.OpenCalls != 0 {
		t.Fatal("cancelled native capture advertised complete", last)
	}
}
func TestNativeProducerOverflowInvalidatesExactness(t *testing.T) {
	path := labPath(t)
	var out bytes.Buffer
	err := RunLab(context.Background(), LabOptions{Workload: path, Threads: 2, Iterations: 100, Capacity: 1, Timeout: 10 * time.Second}, &out)
	if !errors.Is(err, ErrLoss) {
		t.Fatal("native overflow not detected", err)
	}
	r, readErr := instrumentation.NewMetricsReader(&out).Next()
	if readErr != nil {
		t.Fatal(readErr, err)
	}
	if !r.Metrics.Incomplete || r.Metrics.OpenCalls != 0 {
		t.Fatal("lost native events advertised exact", r)
	}
}
