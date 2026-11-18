package instrumentation

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"
	"unicode"

	"github.com/gabrielteiga/AntiPessimizerV2/internal/profiling"
)

const EventProtocol = "antipessimizer.instrumentation.events"
const MetricsProtocol = "antipessimizer.instrumentation.metrics"
const MaxFrameBytes = 8 << 20

type Header struct {
	Protocol   string           `json:"protocol"`
	Version    int              `json:"version"`
	SessionID  string           `json:"session_id"`
	Target     profiling.Target `json:"target"`
	Clock      string           `json:"clock"`
	Frequency  uint64           `json:"clock_frequency_hz,string"`
	Source     string           `json:"source"`
	Procedures []Procedure      `json:"procedures"`
}
type Event struct {
	Type      string `json:"type"`
	Thread    uint32 `json:"thread_id,omitempty"`
	Procedure uint32 `json:"procedure_id,omitempty"`
	Tick      uint64 `json:"ticks,string"`
	KeepDepth int    `json:"keep_depth,omitempty"`
}
type Record struct {
	// NativeLostEvents is the cumulative count observed in the last native
	// batch header. Capture stops on loss, so this is a lower bound, not an
	// estimate of events lost after the target was cancelled. Added in v3.
	NativeLostEvents uint64           `json:"native_lost_event_count,string,omitempty"`
	Selection        []Procedure      `json:"selection,omitempty"`
	Protocol         string           `json:"protocol"`
	Version          int              `json:"version"`
	SessionID        string           `json:"session_id"`
	Target           profiling.Target `json:"target"`
	Source           string           `json:"source"`
	Clock            string           `json:"clock"`
	Sequence         uint64           `json:"sequence"`
	At               uint64           `json:"observed_at_ticks,string"`
	Metrics          Snapshot         `json:"metrics"`
}

func decode(b []byte, v any) error {
	d := json.NewDecoder(bytes.NewReader(b))
	d.DisallowUnknownFields()
	if err := d.Decode(v); err != nil {
		return err
	}
	var extra any
	if err := d.Decode(&extra); err != io.EOF {
		return errors.New("expected exactly one JSON object")
	}
	return nil
}

// Replay consumes ordered entry/exit events, not CPU samples. It provides a
// deterministic contract for the future native adapter without claiming live
// Delphi hooks already exist. Caller owns cancellation of blocked I/O.
func Replay(ctx context.Context, in io.Reader, out io.Writer, limits Config) error {
	reader := bufio.NewScanner(in)
	reader.Buffer(make([]byte, 4096), MaxFrameBytes)
	if err := ctx.Err(); err != nil {
		return err
	}
	if !reader.Scan() {
		if err := reader.Err(); err != nil {
			return err
		}
		return errors.New("missing instrumentation header")
	}
	var h Header
	if err := decode(reader.Bytes(), &h); err != nil {
		return fmt.Errorf("header: %w", err)
	}
	if h.Protocol != EventProtocol || h.Version != 1 || h.SessionID == "" || len(h.SessionID) > 128 || strings.IndexFunc(h.SessionID, unicode.IsControl) >= 0 {
		return errors.New("unsupported or invalid instrumentation header")
	}
	if h.Source != "synthetic" && h.Source != "native-hook" {
		return errors.New("event source must be synthetic or native-hook")
	}
	if h.Clock != "synthetic" && h.Clock != "qpc" && h.Clock != "tsc" {
		return errors.New("unsupported instrumentation clock")
	}
	if (h.Source == "synthetic") != (h.Clock == "synthetic") {
		return errors.New("synthetic/native source and clock are inconsistent")
	}
	_, zoneOffset := h.Target.Start.Zone()
	if zoneOffset != 0 {
		return errors.New("target creation time must be UTC")
	}
	if err := h.Target.Validate(); err != nil {
		return err
	}
	limits.Frequency = h.Frequency
	engine, err := New(limits, h.Procedures)
	if err != nil {
		return err
	}
	sequence := uint64(0)
	latest := uint64(0)
	dirty := true
	emit := func() error {
		if sequence == ^uint64(0) {
			return errors.New("metrics sequence overflow")
		}
		snapshot, err := engine.Snapshot()
		if err != nil {
			return err
		}
		record := Record{Protocol: MetricsProtocol, Version: 2, Selection: append([]Procedure(nil), h.Procedures...), SessionID: h.SessionID, Target: h.Target, Source: h.Source, Clock: h.Clock, Sequence: sequence + 1, At: latest, Metrics: snapshot}
		if err := WriteRecord(out, record); err != nil {
			return err
		}
		sequence++
		dirty = false
		return nil
	}
	line := 1
	for reader.Scan() {
		line++
		if err := ctx.Err(); err != nil {
			return err
		}
		var e Event
		if err := decode(reader.Bytes(), &e); err != nil {
			return fmt.Errorf("event line %d: %w", line, err)
		}
		if e.KeepDepth < 0 {
			return fmt.Errorf("event line %d: negative unwind depth", line)
		}
		if err := e.validate(); err != nil {
			return fmt.Errorf("event line %d: %w", line, err)
		}
		if (e.Type == "snapshot" || e.Type == "clear" || e.Type == "loss") && e.Tick < latest {
			return fmt.Errorf("event line %d: global event predates observed events", line)
		}
		if e.Tick > latest {
			latest = e.Tick
		}
		switch e.Type {
		case "thread_start":
			err = engine.StartThread(e.Thread, e.Tick)
		case "enter":
			err = engine.Enter(e.Thread, e.Procedure, e.Tick)
		case "exit":
			err = engine.Exit(e.Thread, e.Procedure, e.Tick)
		case "unwind_exit":
			err = engine.ExitUnwound(e.Thread, e.Procedure, e.Tick)
		case "unwind":
			err = engine.Unwind(e.Thread, e.KeepDepth, e.Tick)
		case "thread_end":
			err = engine.EndThread(e.Thread, e.Tick)
		case "clear":
			err = engine.Clear(e.Tick)
		case "loss":
			err = engine.MarkLoss()
		case "snapshot":
			err = emit()
		default:
			err = fmt.Errorf("unsupported event type %q", e.Type)
		}
		if err != nil {
			return fmt.Errorf("event line %d: %w", line, err)
		}
		if e.Type != "snapshot" {
			dirty = true
		}
	}
	if err := reader.Err(); err != nil {
		return err
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if engine.open != 0 {
		return errors.New("event stream ended with open calls; supply actual exits/unwind or an explicit loss event")
	}
	if dirty {
		return emit()
	}
	return nil
}

func (e Event) validate() error {
	switch e.Type {
	case "thread_start", "thread_end":
		if e.Thread == 0 || e.Procedure != 0 || e.KeepDepth != 0 {
			return errors.New("invalid thread lifecycle fields")
		}
	case "enter", "exit", "unwind_exit":
		if e.Thread == 0 || e.Procedure == 0 || e.KeepDepth != 0 {
			return errors.New("invalid entry/exit fields")
		}
	case "unwind":
		if e.Thread == 0 || e.Procedure != 0 || e.KeepDepth < 0 {
			return errors.New("invalid unwind fields")
		}
	case "clear", "loss", "snapshot":
		if e.Thread != 0 || e.Procedure != 0 || e.KeepDepth != 0 {
			return errors.New("invalid global event fields")
		}
	default:
		return fmt.Errorf("unsupported event type %q", e.Type)
	}
	return nil
}
