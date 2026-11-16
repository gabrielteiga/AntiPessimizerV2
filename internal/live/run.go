// Package live joins bounded OS sampling, symbol resolution and windowing.
package live

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"sync/atomic"
	"time"

	"github.com/gabrielteiga/AntiPessimizerV2/internal/aggregation"
	"github.com/gabrielteiga/AntiPessimizerV2/internal/platform"
	"github.com/gabrielteiga/AntiPessimizerV2/internal/profiling"
	"github.com/gabrielteiga/AntiPessimizerV2/internal/symbols"
)

type Capture func(context.Context, platform.Process, func(platform.Sample), func(platform.Health)) error

// Run owns the capture goroutine and waits for its release on every exit path.
// emit must return promptly; the service hub coalesces independently per viewer.
func Run(ctx context.Context, c aggregation.Config, capture Capture, emit func(profiling.Snapshot) error) error {
	if c.Width < 10*time.Millisecond || c.Width > 10*time.Second {
		return errors.New("live window must be 10ms..10s")
	}
	agg, err := aggregation.New(c)
	if err != nil {
		return err
	}
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	queue := make(chan platform.Sample, 8192)
	var dropped atomic.Uint64
	var health atomic.Pointer[platform.Health]
	done := make(chan error, 1)
	finished := false
	go func() {
		done <- capture(ctx, platform.Process{PID: c.Target.PID, Start: c.Target.Start, Name: c.Target.Image}, func(s platform.Sample) {
			select {
			case queue <- s:
			default:
				dropped.Add(1)
			}
		}, func(h platform.Health) { health.Store(&h) })
	}()
	defer func() {
		cancel()
		if !finished {
			<-done
		}
	}()
	cache := map[platform.Module]*symbols.Image{}
	resolve := func(s platform.Sample) profiling.Location {
		m := s.Module
		if m.Size == 0 {
			return profiling.Location{ModuleID: "unmapped", Module: "unknown", RVA: fmt.Sprintf("0x%016x", s.IP), Status: "unknown", Reason: "module_unavailable"}
		}
		image, exists := cache[m]
		if !exists {
			if len(cache) >= 8 {
				clear(cache)
			}
			image, _ = symbols.LoadImage(platform.ImagePath(m.Path))
			cache[m] = image
		}
		if image != nil && image.Size == m.Size {
			if loc, ok := image.Index.Resolve(m.Base, s.IP); ok {
				return loc
			}
		}
		name := filepath.Base(m.Path)
		if name == "" || name == "." {
			name = "unknown"
		}
		if len(name) > 256 {
			name = "unknown"
		}
		return profiling.Location{ModuleID: fmt.Sprintf("runtime:%x:%x", m.Base, m.Size), Module: name, RVA: fmt.Sprintf("0x%016x", s.IP-m.Base), Status: "unknown", Reason: "symbols_unavailable"}
	}
	publish := func(partialEnd time.Time) error {
		var snapshot profiling.Snapshot
		var err error
		if !partialEnd.IsZero() {
			snapshot, err = agg.ClosePartial(partialEnd)
			snapshot.Published = time.Now().UTC()
		} else {
			snapshot, err = agg.Close(time.Now())
		}
		if err != nil {
			return err
		}
		snapshot.Quality.IngestLost = dropped.Swap(0)
		if h := health.Load(); h != nil {
			snapshot.Quality.ETWLossKnown = h.Known
			snapshot.Quality.ETWEventsLost = h.EventsLost
			snapshot.Quality.ETWBuffersLost = h.BuffersLost
			snapshot.Quality.CorrelationUncertain = h.CorrelationUncertain
		}
		return emit(snapshot)
	}
	add := func(s platform.Sample) error {
		for !s.At.Before(agg.End()) {
			if s.At.After(time.Now().Add(time.Second)) {
				return errors.New("sample timestamp is in the future")
			}
			if err := publish(time.Time{}); err != nil {
				return err
			}
		}
		return agg.Add(profiling.Sample{At: s.At, ThreadID: s.Thread, Location: resolve(s)})
	}
	ticker := time.NewTicker(50 * time.Millisecond)
	defer ticker.Stop()
	for {
		select {
		case s := <-queue:
			if err := add(s); err != nil {
				return err
			}
		case <-ticker.C:
			// ETW flushes buffers every second. Hold windows for two seconds before
			// publishing; arrivals after this allowance are counted as late.
			for time.Now().After(agg.End().Add(2 * time.Second)) {
				if err := publish(time.Time{}); err != nil {
					return err
				}
			}
		case err := <-done:
			finished = true
			for len(queue) > 0 {
				if e := add(<-queue); e != nil {
					return e
				}
			}
			end := time.Now()
			for !end.Before(agg.End()) {
				if e := publish(time.Time{}); e != nil {
					return e
				}
			}
			if e := publish(end); e != nil {
				return e
			}
			if errors.Is(err, context.Canceled) {
				return nil
			}
			return err
		case <-ctx.Done():
			// Keep draining until Capture releases its ETW handles.
			cancel()
			err := <-done
			finished = true
			for len(queue) > 0 {
				if e := add(<-queue); e != nil {
					return e
				}
			}
			end := time.Now()
			for !end.Before(agg.End()) {
				if e := publish(time.Time{}); e != nil {
					return e
				}
			}
			if e := publish(end); e != nil {
				return e
			}
			if errors.Is(err, context.Canceled) {
				return nil
			}
			return err
		}
	}
}
