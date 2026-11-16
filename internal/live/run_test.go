package live

import (
	"context"
	"github.com/gabrielteiga/AntiPessimizerV2/internal/aggregation"
	"github.com/gabrielteiga/AntiPessimizerV2/internal/platform"
	"github.com/gabrielteiga/AntiPessimizerV2/internal/profiling"
	"testing"
	"time"
)

func TestRunJoinsCaptureAndFlushesPartial(t *testing.T) {
	start := time.Now().Add(-100 * time.Millisecond)
	c := aggregation.Config{SessionID: "live-test", Target: profiling.Target{PID: 7, Start: start, Image: "lab.exe"}, Source: "external", Start: start, Width: time.Second, Capacity: 10, TopN: 10}
	ended := false
	capture := func(ctx context.Context, pid uint32, emit func(platform.Sample)) error {
		defer func() { ended = true }()
		emit(platform.Sample{At: start, Thread: 1, IP: 0x1234})
		return nil
	}
	var got []profiling.Snapshot
	if err := Run(context.Background(), c, capture, func(v profiling.Snapshot) error { got = append(got, v); return v.Validate() }); err != nil {
		t.Fatal(err)
	}
	if !ended || len(got) != 1 || got[0].Quality.Accepted != 1 || !got[0].WindowEnd.Before(start.Add(time.Second)) {
		t.Fatal(got)
	}
}
