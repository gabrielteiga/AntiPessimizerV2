// Package session composes a source, aggregation, retention and publication.
package session

import (
	"context"
	"errors"
	"io"
	"time"

	"github.com/gabrielteiga/AntiPessimizerV2/internal/aggregation"
	"github.com/gabrielteiga/AntiPessimizerV2/internal/history"
	"github.com/gabrielteiga/AntiPessimizerV2/internal/profiling"
	"github.com/gabrielteiga/AntiPessimizerV2/internal/sampling"
)

// Source belongs here, at its consumer. Implementations must honor cancellation.
type Source interface {
	Next(context.Context) (sampling.Event, error)
}

// Run is synchronous with bounded backpressure: a slow sink pauses replay.
// A future live source needs its own bounded ingest queue and loss accounting.
// EOF is successful only at a window boundary; partial windows are not fabricated.
func Run(ctx context.Context, source Source, agg *aggregation.Aggregator, store *history.Store, clock func() time.Time, emit func(profiling.Snapshot) error) error {
	if source == nil || agg == nil || store == nil || clock == nil || emit == nil {
		return errors.New("missing session dependency")
	}
	dirty := false
	for {
		if err := ctx.Err(); err != nil {
			return err
		}
		e, err := source.Next(ctx)
		if errors.Is(err, io.EOF) {
			if dirty {
				return errors.New("source ended with an incomplete window")
			}
			return nil
		}
		if err != nil {
			return err
		}
		if !e.Watermark.IsZero() {
			if !e.Sample.At.IsZero() || !e.Watermark.Equal(agg.End()) {
				return errors.New("invalid source watermark")
			}
			s, err := agg.Close(clock())
			if err != nil {
				return err
			}
			if err := store.Append(s); err != nil {
				return err
			}
			if err := ctx.Err(); err != nil {
				return err
			}
			if err := emit(s); err != nil {
				return err
			}
			dirty = false
		} else {
			if err := agg.Add(e.Sample); err != nil {
				return err
			}
			dirty = true
		}
	}
}
