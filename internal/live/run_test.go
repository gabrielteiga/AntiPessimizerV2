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
	capture := func(ctx context.Context, target platform.Process, emit func(platform.Sample), health func(platform.Health)) error {
		defer func() { ended = true }()
		if target.PID != 7 || !target.Start.Equal(start) {
			t.Error("target identity lost")
		}
		health(platform.Health{Known: true, EventsLost: 3, BuffersLost: 1, CorrelationUncertain: true})
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

func TestHealthCountersAreNotTargetLoss(t *testing.T) {
	start := time.Now().Add(-time.Millisecond)
	c := aggregation.Config{SessionID: "loss", Target: profiling.Target{PID: 1, Start: start, Image: "lab.exe"}, Source: "external", Start: start, Width: time.Second, Capacity: 1, TopN: 1}
	capture := func(ctx context.Context, target platform.Process, emit func(platform.Sample), health func(platform.Health)) error {
		health(platform.Health{Known: true, EventsLost: 7, CorrelationUncertain: true})
		return nil
	}
	if err := Run(context.Background(), c, capture, func(s profiling.Snapshot) error {
		if s.Quality.SourceLossKnown || s.Quality.SourceLost != 0 || s.Quality.ETWEventsLost != 7 || !s.Quality.CorrelationUncertain {
			t.Errorf("%+v", s.Quality)
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
}

func TestOutOfOrderCPUBufferDelivery(t *testing.T) {
	start := time.Now().Add(-100 * time.Millisecond)
	c := aggregation.Config{SessionID: "reorder", Target: profiling.Target{PID: 1, Start: start, Image: "lab.exe"}, Source: "external", Start: start, Width: 20 * time.Millisecond, Capacity: 10, TopN: 10}
	capture := func(ctx context.Context, target platform.Process, emit func(platform.Sample), health func(platform.Health)) error {
		emit(platform.Sample{At: start.Add(25 * time.Millisecond), Thread: 1, IP: 0x1234})
		emit(platform.Sample{At: start.Add(5 * time.Millisecond), Thread: 1, IP: 0x1234})
		return nil
	}
	var accepted, late uint64
	if err := Run(context.Background(), c, capture, func(s profiling.Snapshot) error {
		accepted += s.Quality.Accepted
		late += s.Quality.Late
		return s.Validate()
	}); err != nil {
		t.Fatal(err)
	}
	if accepted != 2 || late != 0 {
		t.Fatalf("accepted=%d late=%d", accepted, late)
	}
}
