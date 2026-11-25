package instrumentation

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"strings"
	"unicode"

	"github.com/gabrielteiga/AntiPessimizerV2/internal/profiling"
)

func (r Record) Clone() Record {
	r.Metrics.Rows = append([]Row{}, r.Metrics.Rows...)
	r.Selection = append([]Procedure(nil), r.Selection...)
	return r
}

// Validate checks the separate instrumentation contract, including redundant
// unit conversions. A valid frame can still be provisional or incomplete.
func (r Record) Validate() error {
	if r.Protocol != MetricsProtocol || (r.Version < 1 || r.Version > 3) || r.SessionID == "" || len(r.SessionID) > 128 || strings.IndexFunc(r.SessionID, unicode.IsControl) >= 0 || r.Sequence == 0 {
		return errors.New("invalid instrumentation metrics envelope")
	}
	if err := r.Target.Validate(); err != nil {
		return err
	}
	if _, offset := r.Target.Start.Zone(); offset != 0 {
		return errors.New("target time must be UTC")
	}
	if (r.Source != "synthetic" && r.Source != "native-hook") || (r.Clock != "synthetic" && r.Clock != "qpc" && r.Clock != "tsc") || (r.Source == "synthetic") != (r.Clock == "synthetic") {
		return errors.New("invalid instrumentation source/clock")
	}
	s := r.Metrics
	if r.NativeLostEvents > 0 && (r.Version < 3 || r.Source != "native-hook" || !s.Incomplete) {
		return errors.New("native loss count requires v3 native incomplete metrics")
	}
	if s.Version != 1 || s.Mode != "instrumentation" || s.Frequency == 0 || s.Frequency > 1e12 || s.OpenCalls > MaxOpenCalls || len(s.Rows) > profiling.MaxRows || (s.AbandonedCalls > 0 && !s.Incomplete) {
		return errors.New("invalid instrumentation metrics snapshot")
	}
	seen := map[key]bool{}
	procedures := map[uint32]profiling.Location{}
	locations := map[string]uint32{}
	if r.Version == 1 && len(r.Selection) > 0 || r.Version >= 2 && (len(r.Selection) == 0 || len(r.Selection) > 65536) {
		return errors.New("invalid instrumentation selection envelope")
	}
	for _, p := range r.Selection {
		if p.ID == 0 || p.Location.Status != "resolved" {
			return errors.New("invalid selected procedure")
		}
		if _, duplicate := procedures[p.ID]; duplicate {
			return errors.New("duplicate selected procedure")
		}
		if err := p.Location.Validate(); err != nil {
			return err
		}
		if err := checkProcedure(procedures, locations, p.ID, p.Location); err != nil {
			return err
		}
	}
	var active uint64
	for _, row := range s.Rows {
		if err := row.Location.Validate(); err != nil {
			return err
		}
		if row.Status != "resolved" || row.ProcedureID == 0 || row.ThreadID == 0 || row.ThreadInstance > r.At || row.Calls == 0 || row.UnwoundCalls > row.Calls {
			return errors.New("invalid instrumentation procedure row")
		}
		k := key{row.ThreadID, row.ThreadInstance, row.ProcedureID}
		if seen[k] {
			return errors.New("duplicate instrumentation row")
		}
		seen[k] = true
		if r.Version >= 2 {
			if selected, ok := procedures[row.ProcedureID]; !ok || selected != row.Location {
				return errors.New("metrics row does not match selected procedure")
			}
		}
		if err := checkProcedure(procedures, locations, row.ProcedureID, row.Location); err != nil {
			return err
		}
		active += uint64(row.ActiveFrames)
		for _, duration := range []struct {
			ticks, ns uint64
			average   float64
		}{
			{row.InclusiveTicks, row.InclusiveNS, row.AverageInclusiveNS},
			{row.ExclusiveTicks, row.ExclusiveNS, row.AverageExclusiveNS},
		} {
			ns, err := NanosecondsFromTicks(duration.ticks, s.Frequency)
			want := float64(duration.ticks) * 1e9 / float64(s.Frequency) / float64(row.Calls)
			if err != nil || ns != duration.ns || duration.average < 0 || math.IsNaN(duration.average) || math.IsInf(duration.average, 0) || math.Abs(duration.average-want) > math.Max(1e-9, math.Abs(want)*1e-12) {
				return errors.New("inconsistent instrumentation time/average")
			}
		}
	}
	// Open calls without a completed invocation have no published row yet.
	if active > s.OpenCalls {
		return errors.New("active rows exceed open calls")
	}
	return nil
}

func checkProcedure(procedures map[uint32]profiling.Location, locations map[string]uint32, id uint32, location profiling.Location) error {
	if previous, ok := procedures[id]; ok && previous != location {
		return errors.New("instrumented procedure identity changed")
	}
	identity := location.ModuleID + ":" + location.RVA
	if previous, ok := locations[identity]; ok && previous != id {
		return errors.New("multiple IDs for instrumented procedure")
	}
	if len(procedures) >= 65536 {
		if _, ok := procedures[id]; !ok {
			return errors.New("instrumented selection limit exceeded")
		}
	}
	procedures[id], locations[identity] = location, id
	return nil
}

func WriteRecord(out io.Writer, r Record) error {
	if err := r.Validate(); err != nil {
		return err
	}
	b, err := json.Marshal(r)
	if err != nil {
		return err
	}
	if len(b)+1 > MaxFrameBytes {
		return errors.New("metrics frame exceeds limit")
	}
	b = append(b, '\n')
	n, err := out.Write(b)
	if err == nil && n != len(b) {
		err = io.ErrShortWrite
	}
	return err
}

type MetricsReader struct {
	scanner *bufio.Scanner
	order   metricOrder
	failed  error
}

type metricOrder struct {
	previous     Record
	procedures   map[uint32]profiling.Location
	locations    map[string]uint32
	previousRows map[key]Row
}

func NewMetricsReader(in io.Reader) *MetricsReader {
	scanner := bufio.NewScanner(in)
	scanner.Buffer(make([]byte, 4096), MaxFrameBytes)
	return &MetricsReader{scanner: scanner}
}

func (reader *MetricsReader) Next() (Record, error) {
	if reader.failed != nil {
		return Record{}, reader.failed
	}
	r, err := reader.next()
	if err != nil {
		reader.failed = err
	}
	return r, err
}

func (reader *MetricsReader) next() (Record, error) {
	if !reader.scanner.Scan() {
		if err := reader.scanner.Err(); err != nil {
			return Record{}, fmt.Errorf("read metrics frame: %w", err)
		}
		return Record{}, io.EOF
	}
	var r Record
	if err := decode(reader.scanner.Bytes(), &r); err != nil {
		return Record{}, err
	}
	if err := r.Validate(); err != nil {
		return Record{}, err
	}
	if err := reader.order.accept(r); err != nil {
		return Record{}, err
	}
	return r, nil
}

func (reader *metricOrder) accept(r Record) error {
	if reader.procedures == nil {
		reader.procedures = map[uint32]profiling.Location{}
		reader.locations = map[string]uint32{}
	}
	p := reader.previous
	if p.Sequence != 0 && r.NativeLostEvents < p.NativeLostEvents {
		return errors.New("native event loss count regressed")
	}
	if p.Sequence != 0 && (r.Version != p.Version || !sameSelection(r.Selection, p.Selection) || r.SessionID != p.SessionID || r.Target != p.Target || r.Source != p.Source || r.Clock != p.Clock || r.Metrics.Frequency != p.Metrics.Frequency || r.Sequence <= p.Sequence || r.At < p.At || r.Metrics.Epoch < p.Metrics.Epoch) {
		return errors.New("instrumentation session, clock or snapshot order changed")
	}
	for _, row := range r.Metrics.Rows {
		if err := checkProcedure(reader.procedures, reader.locations, row.ProcedureID, row.Location); err != nil {
			return err
		}
	}
	rows := make(map[key]Row, len(r.Metrics.Rows))
	for _, row := range r.Metrics.Rows {
		rows[key{row.ThreadID, row.ThreadInstance, row.ProcedureID}] = row
	}
	if p.Sequence != 0 && r.Metrics.Epoch == p.Metrics.Epoch {
		if r.Metrics.AbandonedCalls < p.Metrics.AbandonedCalls || p.Metrics.Incomplete && !r.Metrics.Incomplete {
			return errors.New("instrumentation loss state regressed without Clear")
		}
		for k, previous := range reader.previousRows {
			row, ok := rows[k]
			if !ok || row.Calls < previous.Calls || row.UnwoundCalls < previous.UnwoundCalls || row.ExclusiveTicks < previous.ExclusiveTicks {
				return errors.New("cumulative instrumentation counts regressed without Clear")
			}
		}
	}
	reader.previousRows = rows
	reader.previous = r
	// Keep only scalar metadata, never aliases into rows returned to the caller.
	reader.previous.Metrics.Rows = nil
	reader.previous.Selection = append([]Procedure(nil), r.Selection...)
	return nil
}

// ValidateRecords also checks selection identity and session continuity across clears.
func ValidateRecords(records []Record) error {
	if len(records) == 0 || len(records) > 4096 {
		return errors.New("empty or excessive metrics history")
	}
	var validator metricOrder
	rows := 0
	for _, r := range records {
		if err := r.Validate(); err != nil {
			return err
		}
		if err := validator.accept(r); err != nil {
			return err
		}
		rows += len(r.Metrics.Rows)
		if rows > 100000 {
			return errors.New("metrics history row limit exceeded")
		}
	}
	return nil
}

// ReadMetrics retains bounded cumulative snapshots. Consumers must select a
// snapshot rather than sum records, which would count completed calls repeatedly.
func ReadMetrics(in io.Reader) ([]Record, error) {
	limited := &io.LimitedReader{R: in, N: 64 << 20}
	reader := NewMetricsReader(limited)
	var records []Record
	rows := 0
	for {
		r, err := reader.Next()
		if err == io.EOF {
			if limited.N == 0 {
				return nil, errors.New("metrics archive reaches 64 MiB limit")
			}
			break
		}
		if err != nil {
			return nil, err
		}
		rows += len(r.Metrics.Rows)
		if len(records) >= 4096 || rows > 100000 {
			return nil, errors.New("metrics archive retention limit exceeded")
		}
		records = append(records, r)
	}
	if len(records) == 0 {
		return nil, errors.New("empty metrics archive")
	}
	return records, nil
}

// MetricValidator validates a complete stream one record at a time, retaining
// procedure identities across Clear without retaining the snapshot history.
// One capture owner must serialize calls to Accept.
type MetricValidator struct{ order metricOrder }

func (v *MetricValidator) Accept(r Record) error {
	if err := r.Validate(); err != nil {
		return err
	}
	return v.order.accept(r)
}

func sameSelection(a, b []Procedure) bool {
	if len(a) != len(b) {
		return false
	}
	selected := make(map[uint32]profiling.Location, len(a))
	for _, p := range a {
		selected[p.ID] = p.Location
	}
	for _, p := range b {
		if selected[p.ID] != p.Location {
			return false
		}
	}
	return true
}
