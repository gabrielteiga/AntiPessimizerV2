package session_test

import (
	"bytes"
	"context"
	"errors"
	"io"
	"testing"
	"time"

	"github.com/gabrielteiga/AntiPessimizerV2/internal/aggregation"
	"github.com/gabrielteiga/AntiPessimizerV2/internal/history"
	"github.com/gabrielteiga/AntiPessimizerV2/internal/profiling"
	"github.com/gabrielteiga/AntiPessimizerV2/internal/sampling"
	"github.com/gabrielteiga/AntiPessimizerV2/internal/session"
	"github.com/gabrielteiga/AntiPessimizerV2/internal/stream"
)

func setup(t *testing.T) (*aggregation.Aggregator, *history.Store, *sampling.Fake) {
	t.Helper()
	start := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	a, err := aggregation.New(aggregation.Config{SessionID: "test", Target: profiling.Target{PID: 1, Start: start, Image: "lab"}, Source: "synthetic", Start: start, Width: time.Second, Capacity: 10, TopN: 10, SourceLossKnown: true})
	if err != nil {
		t.Fatal(err)
	}
	h, err := history.New(2, 20, time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	f, err := sampling.NewFake(start, time.Second, 3, 10)
	if err != nil {
		t.Fatal(err)
	}
	return a, h, f
}
func TestPipeline(t *testing.T) {
	a, h, f := setup(t)
	var wire bytes.Buffer
	err := session.Run(context.Background(), f, a, h, a.End, func(s profiling.Snapshot) error { return stream.Write(&wire, s) })
	if err != nil {
		t.Fatal(err)
	}
	reader := stream.NewReader(&wire)
	for i := 1; i <= 3; i++ {
		s, err := reader.Next()
		if err != nil {
			t.Fatal(err)
		}
		if s.Sequence != uint64(i) || s.Source != "synthetic" || s.Quality.Accepted != 10 || len(s.Hotspots) != 3 {
			t.Fatalf("snapshot %+v", s)
		}
		name := "LabWorkload.CPUHotspotA"
		if i%2 == 0 {
			name = "LabWorkload.CPUHotspotB"
		}
		if s.Hotspots[0].Procedure != name || s.Hotspots[0].Samples != 7 || s.Hotspots[2].Status != "unknown" {
			t.Fatalf("hotspots %+v", s.Hotspots)
		}
	}
	if _, err := reader.Next(); !errors.Is(err, io.EOF) {
		t.Fatal(err)
	}
	got, err := h.Query(time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC), a.End(), "", 0)
	if err != nil || len(got) != 2 || got[0].Sequence != 2 {
		t.Fatal("history", got, err)
	}
}
func TestCancelAndSinkFailure(t *testing.T) {
	for _, mode := range []string{"cancel", "sink_error"} {
		t.Run(mode, func(t *testing.T) {
			a, h, f := setup(t)
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			sent := 0
			failure := errors.New("sink disconnected")
			err := session.Run(ctx, f, a, h, a.End, func(s profiling.Snapshot) error {
				sent++
				if mode == "cancel" {
					cancel()
					return nil
				}
				return failure
			})
			expected := error(context.Canceled)
			if mode == "sink_error" {
				expected = failure
			}
			if !errors.Is(err, expected) || sent != 1 {
				t.Fatal(err, sent)
			}
		})
	}
}

type oneEvent struct {
	event sampling.Event
	done  bool
}

func (s *oneEvent) Next(ctx context.Context) (sampling.Event, error) {
	if s.done {
		return sampling.Event{}, io.EOF
	}
	s.done = true
	return s.event, nil
}
func TestIncompleteAndWrongWatermark(t *testing.T) {
	for _, mode := range []string{"incomplete", "watermark"} {
		t.Run(mode, func(t *testing.T) {
			a, h, f := setup(t)
			event, err := f.Next(context.Background())
			if err != nil {
				t.Fatal(err)
			}
			if mode == "watermark" {
				event = sampling.Event{Watermark: a.End().Add(time.Second)}
			}
			if err := session.Run(context.Background(), &oneEvent{event: event}, a, h, a.End, func(profiling.Snapshot) error { return nil }); err == nil {
				t.Fatal("accepted broken source")
			}
		})
	}
}
func TestBackpressureAndCancellation(t *testing.T) {
	a, h, f := setup(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	reached := make(chan struct{})
	done := make(chan error, 1)
	go func() {
		done <- session.Run(ctx, f, a, h, a.End, func(profiling.Snapshot) error { close(reached); <-ctx.Done(); return ctx.Err() })
	}()
	select {
	case <-reached:
	case <-time.After(3 * time.Second):
		t.Fatal("sink never reached")
	}
	// A bounded synchronous sink pauses source consumption; cancellation releases it.
	cancel()
	select {
	case err := <-done:
		if !errors.Is(err, context.Canceled) {
			t.Fatal(err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("session did not terminate")
	}
}
