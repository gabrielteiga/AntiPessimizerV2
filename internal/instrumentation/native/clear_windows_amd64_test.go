package native

import (
	"bytes"
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/gabrielteiga/AntiPessimizerV2/internal/instrumentation"
)

type clearWhileOpenWriter struct {
	bytes.Buffer
	control   *Control
	requested bool
	result    chan error
}

func (w *clearWhileOpenWriter) Write(b []byte) (int, error) {
	var r instrumentation.Record
	if err := json.Unmarshal(b, &r); err != nil {
		return 0, err
	}
	if !w.requested && r.Metrics.OpenCalls == 6 {
		w.requested = true
		go func() {
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			if err := w.control.Clear(ctx); err != nil {
				w.result <- err
				return
			}
			w.result <- w.control.Clear(ctx)
		}()
	}
	return w.Buffer.Write(b)
}
func TestNativeProducerClearBarrierPreservesOpenCallsAndRecursiveMetrics(t *testing.T) {
	o := frameFixture(t)
	o.Args = []string{"--threads", "3", "--iterations", "1", "--delay-ms", "700"}
	o.Control = NewControl()
	out := clearWhileOpenWriter{control: o.Control, result: make(chan error, 1)}
	if err := Launch(context.Background(), o, &out); err != nil {
		t.Fatal(err, out.String())
	}
	if !out.requested {
		t.Fatal("capture never published active frames")
	}
	if err := <-out.result; err != nil {
		t.Fatal("native Clear failed", err)
	}
	records, err := instrumentation.ReadMetrics(&out.Buffer)
	if err != nil {
		t.Fatal(err)
	}
	epochs := map[uint64]bool{}
	var boundaries int
	for _, record := range records {
		if !epochs[record.Metrics.Epoch] && record.Metrics.Epoch > 0 {
			boundaries++
			if record.Metrics.OpenCalls != 6 || record.Metrics.Incomplete {
				t.Fatal("Clear changed active frame ownership", record)
			}
			for _, row := range record.Metrics.Rows {
				if row.Calls != 0 || row.InclusiveTicks != 0 || row.ExclusiveTicks != 0 {
					t.Fatal("Clear retained pre-boundary totals", row)
				}
			}
		}
		epochs[record.Metrics.Epoch] = true
	}
	if boundaries != 2 {
		t.Fatal("Clear boundaries missing", epochs)
	}
	last := records[len(records)-1].Metrics
	if last.Epoch != 2 || last.Incomplete || last.OpenCalls != 0 || last.AbandonedCalls != 0 || len(last.Rows) != 9 {
		t.Fatal(last)
	}
	for _, row := range last.Rows {
		want := map[uint32]uint64{1: 2, 2: 3, 3: 4}[row.ProcedureID]
		if row.Calls != want {
			t.Fatal("Clear lost open or subsequent native calls", row)
		}
	}
	if err := o.Control.Clear(context.Background()); err != ErrControlClosed {
		t.Fatal("Clear succeeded after process exit", err)
	}
}
