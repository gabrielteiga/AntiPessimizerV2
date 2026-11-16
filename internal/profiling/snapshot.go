// Package profiling defines sampling data independently of Windows and presentation.
package profiling

import (
	"errors"
	"fmt"
	"math"
	"strings"
	"time"
	"unicode"
)

const SchemaVersion = 1
const MaxRows = 4096

// Location identifies a procedure start RVA, or the observed RVA when unresolved.
// ModuleID identifies the image build, not its ASLR-dependent load address.
type Location struct {
	ModuleID  string `json:"module_id"`
	Module    string `json:"module"`
	RVA       string `json:"rva"`
	Procedure string `json:"procedure,omitempty"`
	Status    string `json:"symbol_status"`
	Reason    string `json:"reason,omitempty"`
}

func (l Location) Validate() error {
	for _, s := range []string{l.ModuleID, l.Module, l.Procedure, l.Reason} {
		if len(s) > 256 || strings.IndexFunc(s, unicode.IsControl) >= 0 {
			return errors.New("invalid location text")
		}
	}
	if l.ModuleID == "" || l.Module == "" || len(l.RVA) != 18 || !strings.HasPrefix(l.RVA, "0x") {
		return errors.New("invalid module or RVA")
	}
	for _, c := range l.RVA[2:] {
		if !(c >= '0' && c <= '9' || c >= 'a' && c <= 'f') {
			return errors.New("invalid RVA digit")
		}
	}
	if l.Status == "resolved" && l.Procedure != "" && l.Reason == "" {
		return nil
	}
	if l.Status == "unknown" && l.Procedure == "" && l.Reason != "" {
		return nil
	}
	return errors.New("inconsistent symbol status")
}

type Target struct {
	PID   uint32    `json:"pid"`
	Start time.Time `json:"process_start_utc"`
	Image string    `json:"image_name"`
}

func (t Target) Validate() error {
	if t.PID == 0 || t.Start.IsZero() || t.Image == "" || len(t.Image) > 256 || strings.IndexFunc(t.Image, unicode.IsControl) >= 0 {
		return errors.New("invalid target identity")
	}
	return nil
}

type Sample struct {
	At       time.Time
	ThreadID uint32
	Location Location
}
type Hotspot struct {
	Location
	ThreadID uint32 `json:"thread_id"`
	Samples  uint64 `json:"sample_count"`
}
type Quality struct {
	ETWLossKnown         bool   `json:"etw_session_loss_known,omitempty"`
	ETWEventsLost        uint64 `json:"etw_session_events_lost_total,omitempty"`
	ETWBuffersLost       uint64 `json:"etw_session_buffers_lost_total,omitempty"`
	CorrelationUncertain bool   `json:"correlation_uncertain,omitempty"`
	IngestLost           uint64 `json:"samples_lost_ingest_queue,omitempty"`
	Observed             uint64 `json:"samples_observed"`
	Accepted             uint64 `json:"samples_accepted"`
	Dropped              uint64 `json:"samples_dropped_capacity"`
	Late                 uint64 `json:"samples_late"`
	SourceLossKnown      bool   `json:"source_loss_known"`
	SourceLost           uint64 `json:"source_samples_lost"`
}
type Snapshot struct {
	SchemaVersion int       `json:"schema_version"`
	SessionID     string    `json:"session_id"`
	Sequence      uint64    `json:"sequence"`
	Target        Target    `json:"target"`
	Mode          string    `json:"collection_mode"`
	Source        string    `json:"source"`
	WindowStart   time.Time `json:"window_start_utc"`
	WindowEnd     time.Time `json:"window_end_utc"`
	Published     time.Time `json:"published_at_utc"`
	Quality       Quality   `json:"quality"`
	Hotspots      []Hotspot `json:"hotspots"`
	OtherSamples  uint64    `json:"other_samples"`
}

func (s Snapshot) Clone() Snapshot {
	s.Hotspots = append([]Hotspot{}, s.Hotspots...)
	return s
}
func (s Snapshot) Validate() error {
	if s.SchemaVersion != SchemaVersion {
		return fmt.Errorf("unsupported schema version %d", s.SchemaVersion)
	}
	if s.SessionID == "" || len(s.SessionID) > 128 || strings.IndexFunc(s.SessionID, unicode.IsControl) >= 0 || s.Sequence == 0 {
		return errors.New("invalid session or sequence")
	}
	if err := s.Target.Validate(); err != nil {
		return err
	}
	if s.Mode != "sampling" || (s.Source != "synthetic" && s.Source != "external") {
		return errors.New("unsupported collection mode/source")
	}
	if s.WindowStart.IsZero() || !s.WindowEnd.After(s.WindowStart) || s.WindowStart.Before(s.Target.Start) || s.Published.Before(s.WindowEnd) {
		return errors.New("invalid snapshot times")
	}
	for _, t := range []time.Time{s.WindowStart, s.WindowEnd, s.Published, s.Target.Start} {
		_, off := t.Zone()
		if off != 0 {
			return errors.New("snapshot times must be UTC")
		}
	}
	if len(s.Hotspots) > MaxRows {
		return errors.New("too many hotspots")
	}
	if !s.Quality.SourceLossKnown && s.Quality.SourceLost != 0 {
		return errors.New("unknown source loss must not carry a count")
	}
	accounted, err := Sum(s.Quality.Accepted, s.Quality.Dropped, s.Quality.Late)
	if err != nil || accounted != s.Quality.Observed {
		return errors.New("inconsistent observation counts")
	}
	total := s.OtherSamples
	seen := make(map[Hotspot]bool, len(s.Hotspots))
	for _, h := range s.Hotspots {
		if err := h.Location.Validate(); err != nil {
			return err
		}
		if h.ThreadID == 0 || h.Samples == 0 {
			return errors.New("invalid hotspot")
		}
		key := h
		key.Samples = 0
		if seen[key] {
			return errors.New("duplicate hotspot")
		}
		seen[key] = true
		total, err = Sum(total, h.Samples)
		if err != nil {
			return err
		}
	}
	if total != s.Quality.Accepted {
		return errors.New("hotspot counts do not match accepted samples")
	}
	return nil
}

// Sum checks counters rather than silently wrapping them.
func Sum(values ...uint64) (uint64, error) {
	var n uint64
	for _, v := range values {
		if v > math.MaxUint64-n {
			return 0, errors.New("counter overflow")
		}
		n += v
	}
	return n, nil
}
