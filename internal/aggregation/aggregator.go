// Package aggregation owns a single bounded sampling window. It has no goroutines.
package aggregation

import (
	"errors"
	"math"
	"sort"
	"time"

	"github.com/gabrielteiga/AntiPessimizerV2/internal/profiling"
)

var ErrFuture = errors.New("sample belongs to a future window")

type Config struct {
	SessionID       string
	Target          profiling.Target
	Source          string
	Start           time.Time
	Width           time.Duration
	Capacity, TopN  int
	SourceLossKnown bool
}
type key struct {
	Location profiling.Location
	Thread   uint32
}
type Aggregator struct {
	config   Config
	start    time.Time
	sequence uint64
	counts   map[key]uint64
	quality  profiling.Quality
}

func New(c Config) (*Aggregator, error) {
	if c.Width <= 0 || c.Width > time.Hour || c.Capacity < 1 || c.Capacity > profiling.MaxRows || c.TopN < 1 || c.TopN > c.Capacity {
		return nil, errors.New("invalid window or capacity")
	}
	c.Start = c.Start.UTC()
	c.Target.Start = c.Target.Start.UTC()
	s := profiling.Snapshot{
		SchemaVersion: profiling.SchemaVersion,
		SessionID:     c.SessionID,
		Sequence:      1,
		Target:        c.Target,
		Mode:          "sampling",
		Source:        c.Source,
		WindowStart:   c.Start,
		WindowEnd:     c.Start.Add(c.Width),
		Published:     c.Start.Add(c.Width),
	}
	if err := s.Validate(); err != nil {
		return nil, err
	}
	return &Aggregator{config: c, start: c.Start, counts: make(map[key]uint64), quality: profiling.Quality{SourceLossKnown: c.SourceLossKnown}}, nil
}
func (a *Aggregator) End() time.Time { return a.start.Add(a.config.Width) }
func (a *Aggregator) Add(s profiling.Sample) error {
	if s.At.IsZero() || s.ThreadID == 0 {
		return errors.New("invalid sample time/thread")
	}
	if err := s.Location.Validate(); err != nil {
		return err
	}
	if !s.At.Before(a.End()) {
		return ErrFuture
	}
	if a.quality.Observed == math.MaxUint64 {
		return errors.New("observation counter overflow")
	}
	a.quality.Observed++
	if s.At.Before(a.start) {
		a.quality.Late++
		return nil
	}
	k := key{s.Location, s.ThreadID}
	if _, exists := a.counts[k]; !exists && len(a.counts) == a.config.Capacity {
		a.quality.Dropped++
		return nil
	}
	a.counts[k]++
	a.quality.Accepted++
	return nil
}

// Close advances exactly one window, including empty windows. Published is injected.
func (a *Aggregator) Close(published time.Time) (profiling.Snapshot, error) {
	if published.Before(a.End()) {
		return profiling.Snapshot{}, errors.New("cannot publish before window end")
	}
	if a.sequence == math.MaxUint64 {
		return profiling.Snapshot{}, errors.New("sequence overflow")
	}
	rows := make([]profiling.Hotspot, 0, len(a.counts))
	for k, n := range a.counts {
		rows = append(rows, profiling.Hotspot{Location: k.Location, ThreadID: k.Thread, Samples: n})
	}
	sort.Slice(rows, func(i, j int) bool {
		x, y := rows[i], rows[j]
		if x.Samples != y.Samples {
			return x.Samples > y.Samples
		}
		if x.ModuleID != y.ModuleID {
			return x.ModuleID < y.ModuleID
		}
		if x.RVA != y.RVA {
			return x.RVA < y.RVA
		}
		if x.ThreadID != y.ThreadID {
			return x.ThreadID < y.ThreadID
		}
		if x.Procedure != y.Procedure {
			return x.Procedure < y.Procedure
		}
		if x.Status != y.Status {
			return x.Status < y.Status
		}
		if x.Reason != y.Reason {
			return x.Reason < y.Reason
		}
		return x.Module < y.Module
	})
	var other uint64
	if len(rows) > a.config.TopN {
		for _, r := range rows[a.config.TopN:] {
			other += r.Samples
		}
		rows = rows[:a.config.TopN]
	}
	s := profiling.Snapshot{
		SchemaVersion: profiling.SchemaVersion,
		SessionID:     a.config.SessionID,
		Sequence:      a.sequence + 1,
		Target:        a.config.Target,
		Mode:          "sampling",
		Source:        a.config.Source,
		WindowStart:   a.start,
		WindowEnd:     a.End(),
		Published:     published.UTC(),
		Quality:       a.quality,
		Hotspots:      rows,
		OtherSamples:  other,
	}
	a.sequence++
	a.start = a.End()
	clear(a.counts)
	a.quality = profiling.Quality{SourceLossKnown: a.config.SourceLossKnown}
	return s, nil
}
