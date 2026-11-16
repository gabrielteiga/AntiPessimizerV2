// Package report computes deterministic filtered rankings and session comparisons.
package report

import (
	"errors"
	"fmt"
	"io"
	"math"
	"sort"
	"strings"
	"time"

	"github.com/gabrielteiga/AntiPessimizerV2/internal/profiling"
	"github.com/gabrielteiga/AntiPessimizerV2/internal/stream"
)

type Filter struct {
	Text, Module string
	Thread       uint32
}
type Summary struct {
	Source                                     string        `json:"source"`
	Image                                      string        `json:"image"`
	Duration                                   time.Duration `json:"duration_ns"`
	Accepted, Other, Dropped, Late, IngestLost uint64
	Rows                                       []profiling.Hotspot `json:"rows"`
	MissingWindows                             uint64              `json:"missing_windows"`
}

func add(a *uint64, b uint64) error {
	if b > math.MaxUint64-*a {
		return errors.New("counter overflow")
	}
	*a += b
	return nil
}
func Summarize(snapshots []profiling.Snapshot, f Filter) (Summary, error) {
	var result Summary
	type key struct {
		Location profiling.Location
		Thread   uint32
	}
	counts := map[key]uint64{}
	var session string
	var sequence uint64
	var end time.Time
	for _, s := range snapshots {
		if err := s.Validate(); err != nil {
			return result, err
		}
		if session != "" && (s.SessionID != session || s.Sequence <= sequence || s.WindowStart.Before(end)) {
			return result, errors.New("incompatible session/window order")
		}
		if session != "" && (s.Source != result.Source || s.Target.Image != result.Image) {
			return result, errors.New("source or target changed")
		}
		if sequence != 0 {
			if err := add(&result.MissingWindows, s.Sequence-sequence-1); err != nil {
				return result, err
			}
		}
		session = s.SessionID
		sequence = s.Sequence
		end = s.WindowEnd
		result.Source = s.Source
		result.Image = s.Target.Image
		duration := s.WindowEnd.Sub(s.WindowStart)
		if duration > time.Duration(math.MaxInt64)-result.Duration {
			return result, errors.New("duration overflow")
		}
		result.Duration += duration
		for _, pair := range []struct {
			p *uint64
			n uint64
		}{{&result.Accepted, s.Quality.Accepted}, {&result.Dropped, s.Quality.Dropped}, {&result.Late, s.Quality.Late}, {&result.IngestLost, s.Quality.IngestLost}, {&result.Other, s.OtherSamples}} {
			if err := add(pair.p, pair.n); err != nil {
				return result, err
			}
		}
		for _, h := range s.Hotspots {
			if f.Module != "" && h.Module != f.Module || f.Thread != 0 && h.ThreadID != f.Thread || f.Text != "" && !strings.Contains(strings.ToLower(h.Module+" "+h.Procedure+" "+h.RVA), strings.ToLower(f.Text)) {
				if err := add(&result.Other, h.Samples); err != nil {
					return result, err
				}
				continue
			}
			k := key{h.Location, h.ThreadID}
			n := counts[k]
			if err := add(&n, h.Samples); err != nil {
				return result, err
			}
			counts[k] = n
			if len(counts) > 100000 {
				return result, errors.New("ranking exceeds 100000 rows")
			}
		}
	}
	for k, n := range counts {
		result.Rows = append(result.Rows, profiling.Hotspot{Location: k.Location, ThreadID: k.Thread, Samples: n})
	}
	sort.Slice(result.Rows, func(i, j int) bool {
		a, b := result.Rows[i], result.Rows[j]
		if a.Samples != b.Samples {
			return a.Samples > b.Samples
		}
		if a.ModuleID != b.ModuleID {
			return a.ModuleID < b.ModuleID
		}
		if a.RVA != b.RVA {
			return a.RVA < b.RVA
		}
		if a.ThreadID != b.ThreadID {
			return a.ThreadID < b.ThreadID
		}
		return a.Procedure < b.Procedure
	})
	return result, nil
}

// Read loads a bounded archive. Each frame retains the normal snapshot validator.
func Read(in io.Reader) ([]profiling.Snapshot, error) {
	limited := &io.LimitedReader{R: in, N: 256 << 20}
	r := stream.NewReader(limited)
	var snapshots []profiling.Snapshot
	rows := 0
	for {
		s, err := r.Next()
		if err == io.EOF {
			if limited.N == 0 {
				return nil, errors.New("archive reaches 256 MiB limit")
			}
			break
		}
		if err != nil {
			return nil, err
		}
		rows += len(s.Hotspots)
		if len(snapshots) >= 100000 || rows > 1000000 {
			return nil, errors.New("archive retention limit exceeded")
		}
		snapshots = append(snapshots, s)
	}
	if len(snapshots) == 0 {
		return nil, errors.New("empty archive")
	}
	if _, err := Summarize(snapshots, Filter{}); err != nil {
		return nil, err
	}
	return snapshots, nil
}

type Delta struct {
	Module, ModuleID, RVA, Procedure      string
	Before, After                         uint64
	BeforeRate, AfterRate, DeltaPerSecond float64
}
type Comparison struct {
	Before, After Summary
	Changes       []Delta
}

func Compare(a, b []profiling.Snapshot) (Comparison, error) {
	var result Comparison
	var err error
	result.Before, err = Summarize(a, Filter{})
	if err != nil {
		return result, err
	}
	result.After, err = Summarize(b, Filter{})
	if err != nil {
		return result, err
	}
	x, y := result.Before, result.After
	if x.Duration == 0 || y.Duration == 0 || x.Source != y.Source || x.Image != y.Image {
		return result, errors.New("comparison requires same source/target and nonempty durations")
	}
	// Compare procedure activity across threads; TIDs are not stable across sessions.
	rows := map[string]*Delta{}
	builds := map[string]string{}
	for side, s := range []Summary{x, y} {
		for _, h := range s.Rows {
			if old, ok := builds[h.Module]; ok && old != h.ModuleID {
				return result, fmt.Errorf("different image builds for %s", h.Module)
			}
			builds[h.Module] = h.ModuleID
			key := h.ModuleID + ":" + h.RVA
			d := rows[key]
			if d == nil {
				d = &Delta{Module: h.Module, ModuleID: h.ModuleID, RVA: h.RVA, Procedure: h.Procedure}
				rows[key] = d
			}
			if side == 0 {
				if err := add(&d.Before, h.Samples); err != nil {
					return result, err
				}
			} else {
				if err := add(&d.After, h.Samples); err != nil {
					return result, err
				}
			}
		}
	}
	for _, d := range rows {
		d.BeforeRate = float64(d.Before) / x.Duration.Seconds()
		d.AfterRate = float64(d.After) / y.Duration.Seconds()
		d.DeltaPerSecond = d.AfterRate - d.BeforeRate
		result.Changes = append(result.Changes, *d)
	}
	sort.Slice(result.Changes, func(i, j int) bool {
		a, b := result.Changes[i], result.Changes[j]
		if a.DeltaPerSecond != b.DeltaPerSecond {
			return a.DeltaPerSecond > b.DeltaPerSecond
		}
		if a.ModuleID != b.ModuleID {
			return a.ModuleID < b.ModuleID
		}
		return a.RVA < b.RVA
	})
	return result, nil
}
