// Package history retains immutable copies under explicit time, count and row limits.
package history

import (
	"errors"
	"sync"
	"time"

	"github.com/gabrielteiga/AntiPessimizerV2/internal/profiling"
)

type Store struct {
	mu                   sync.RWMutex
	count, rows, maxRows int
	age                  time.Duration
	items                []profiling.Snapshot
	session              string
	last                 uint64
	end                  time.Time
}

func New(count, maxRows int, age time.Duration) (*Store, error) {
	if count < 1 || count > 4096 || maxRows < 1 || maxRows > 1000000 || age <= 0 {
		return nil, errors.New("invalid retention limits")
	}
	return &Store{count: count, maxRows: maxRows, age: age}, nil
}
func (s *Store) Append(v profiling.Snapshot) error {
	if err := v.Validate(); err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if len(v.Hotspots) > s.maxRows {
		return errors.New("snapshot exceeds history row budget")
	}
	if s.session != "" && (v.SessionID != s.session || v.Sequence <= s.last || v.WindowStart.Before(s.end)) {
		return errors.New("history session/sequence/window conflict")
	}
	s.session = v.SessionID
	s.last = v.Sequence
	s.end = v.WindowEnd
	cutoff := v.WindowEnd.Add(-s.age)
	for len(s.items) > 0 && (len(s.items) >= s.count || s.rows+len(v.Hotspots) > s.maxRows || !s.items[0].WindowEnd.After(cutoff)) {
		s.rows -= len(s.items[0].Hotspots)
		s.items[0] = profiling.Snapshot{}
		s.items = s.items[1:]
	}
	s.items = append(s.items, v.Clone())
	s.rows += len(v.Hotspots)
	return nil
}

// Query returns overlapping windows in [from,to), with optional exact module/TID filters.
// Quality retains window totals; filtered rows move into OtherSamples.
func (s *Store) Query(from, to time.Time, module string, thread uint32) ([]profiling.Snapshot, error) {
	if !to.After(from) {
		return nil, errors.New("invalid history range")
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	result := []profiling.Snapshot{}
	for _, v := range s.items {
		if !v.WindowEnd.After(from) || !v.WindowStart.Before(to) {
			continue
		}
		snapshot := v.Clone()
		if module != "" || thread != 0 {
			snapshot.Hotspots = snapshot.Hotspots[:0]
			for _, h := range v.Hotspots {
				if (module == "" || h.Module == module) && (thread == 0 || h.ThreadID == thread) {
					snapshot.Hotspots = append(snapshot.Hotspots, h)
				} else {
					snapshot.OtherSamples += h.Samples
				}
			}
		}
		result = append(result, snapshot)
	}
	return result, nil
}
