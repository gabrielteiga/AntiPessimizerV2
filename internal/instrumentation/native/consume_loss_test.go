package native

import (
	"bytes"
	"context"
	"errors"
	"testing"
	"time"

	"github.com/gabrielteiga/AntiPessimizerV2/internal/instrumentation"
	"github.com/gabrielteiga/AntiPessimizerV2/internal/profiling"
)

func TestCapturePreservesObservedLossAndOnlyConfirmedCalls(t *testing.T) {
	data := append(hello(), packet(1, 0, 0, [5]uint64{1, 7, 0, 0, 101}, [5]uint64{2, 7, 1, 0, 102}, [5]uint64{3, 7, 1, 0, 112}, [5]uint64{2, 7, 1, 0, 113})...)
	data = append(data, packet(2, 33, 0)...)
	reader, err := NewReader(bytes.NewReader(data), 42)
	if err != nil {
		t.Fatal(err)
	}
	procedures := []instrumentation.Procedure{{ID: 1, Location: profiling.Location{ModuleID: "lab", Module: "lab.exe", Procedure: "Lab.Work", RVA: "0x0000000000001000", Status: "resolved"}}}
	var output bytes.Buffer
	err = consumeCapture(context.Background(), reader, profiling.Target{PID: 42, Image: "lab.exe", Start: time.Unix(1, 0).UTC()}, "test-native-loss", procedures, &output, func() error { return nil }, func(context.Context) error {
		t.Fatal("loss must not proceed as a completed target")
		return nil
	})
	if !errors.Is(err, ErrLoss) {
		t.Fatal("native loss error hidden", err)
	}
	records, err := instrumentation.ReadMetrics(&output)
	if err != nil || len(records) != 1 {
		t.Fatal("invalid loss archive", err, records)
	}
	r := records[0]
	if r.Version != 3 || r.NativeLostEvents != 33 || !r.Metrics.Incomplete || r.Metrics.OpenCalls != 0 || r.Metrics.AbandonedCalls != 1 || len(r.Metrics.Rows) != 1 || r.Metrics.Rows[0].Calls != 1 {
		t.Fatal("loss fabricated calls or erased diagnostic count", r)
	}
}

func TestNativeCapturePreservesSubtickCallsWithoutInventingTime(t *testing.T) {
	data := append(hello(), packet(1, 0, 0, [5]uint64{1, 7, 0, 0, 101}, [5]uint64{2, 7, 1, 0, 102}, [5]uint64{3, 7, 1, 0, 102}, [5]uint64{4, 7, 0, 0, 103})...)
	data = append(data, packet(2, 0, 104)...)
	reader, err := NewReader(bytes.NewReader(data), 42)
	if err != nil {
		t.Fatal(err)
	}
	procedures := []instrumentation.Procedure{{ID: 1, Location: profiling.Location{ModuleID: "lab", Module: "lab.exe", Procedure: "Lab.Work", RVA: "0x0000000000001000", Status: "resolved"}}}
	var output bytes.Buffer
	err = consumeCapture(context.Background(), reader, profiling.Target{PID: 42, Image: "lab.exe", Start: time.Unix(1, 0).UTC()}, "test-native-subtick", procedures, &output, func() error { return nil }, func(context.Context) error { return nil })
	if err != nil {
		t.Fatal(err)
	}
	records, err := instrumentation.ReadMetrics(&output)
	if err != nil || len(records) != 1 {
		t.Fatal(err, records)
	}
	r := records[0]
	if r.At != 104 || r.Metrics.Incomplete || r.Metrics.OpenCalls != 0 || r.Metrics.AbandonedCalls != 0 || len(r.Metrics.Rows) != 1 {
		t.Fatal("sub-tick calls invalidated complete native capture", r)
	}
	row := r.Metrics.Rows[0]
	if row.Calls != 1 || row.InclusiveTicks != 0 || row.ExclusiveTicks != 0 || row.AverageInclusiveNS != 0 || row.AverageExclusiveNS != 0 {
		t.Fatal("native clock quantization erased a call or invented elapsed time", row)
	}
}
