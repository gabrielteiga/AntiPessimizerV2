// Package desktop implements the independent native Windows viewer.
package desktop

import (
	"errors"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/gabrielteiga/AntiPessimizerV2/internal/instrumentation"
	"github.com/gabrielteiga/AntiPessimizerV2/internal/profiling"
	"github.com/gabrielteiga/AntiPessimizerV2/internal/report"
)

type Model struct {
	MetricComparison        *instrumentation.Comparison
	MetricUnit              MetricTimeUnit
	metricSizes             []int
	metricBytes, metricRows int

	Metrics    []instrumentation.Record
	Comparison *report.Comparison
	At         uint64 // zero follows the latest window
	SortColumn int
	Ascending  bool
	Threads    bool
	History    []profiling.Snapshot
	Summary    report.Summary
	Rows       [][]string
	Session    string
	Last       uint64
	Omitted    uint64
}

func (m *Model) Append(s profiling.Snapshot) error {
	if err := s.Validate(); err != nil {
		return err
	}
	if len(m.Metrics) > 0 {
		*m = Model{}
	}
	if s.SessionID != m.Session {
		m.History = nil
		m.Session = s.SessionID
		m.Last = 0
		m.Omitted = 0
	}
	if s.Sequence <= m.Last {
		return nil
	}
	if m.Last > 0 {
		m.Omitted += s.Sequence - m.Last - 1
	}
	m.Last = s.Sequence
	m.History = append(m.History, s.Clone())
	cutoff := s.WindowEnd.Add(-120 * time.Second)
	rows := 0
	for i := len(m.History) - 1; i >= 0; i-- {
		rows += len(m.History[i].Hotspots)
		if rows > 100000 || len(m.History)-i > 480 || !m.History[i].WindowEnd.After(cutoff) {
			m.History = append([]profiling.Snapshot(nil), m.History[i+1:]...)
			break
		}
	}
	return nil
}
func (m *Model) Rebuild(seconds int, f report.Filter) error {
	if m.MetricComparison != nil {
		return m.rebuildMetricComparison(f)
	}
	if len(m.Metrics) > 0 {
		return m.rebuildMetrics(f)
	}
	if m.Comparison != nil {
		m.Rows = nil
		m.Summary = m.Comparison.After
		for _, d := range m.Comparison.Changes {
			if f.Text != "" && !strings.Contains(strings.ToLower(d.Module+" "+d.Unit+" "+d.Procedure+" "+d.RVA), strings.ToLower(f.Text)) {
				continue
			}
			state := "presente"
			if d.Before == 0 {
				state = "novo"
			} else if d.After == 0 {
				state = "ausente"
			}
			m.Rows = append(m.Rows, []string{strconv.FormatUint(d.Before, 10), strconv.FormatUint(d.After, 10), fmt.Sprintf("%+.3f", d.DeltaPerSecond), d.Module, d.Procedure, d.RVA, state, d.Unit})
		}
		m.sortRows()
		return nil
	}

	snapshots := m.History
	if m.At != 0 {
		end := sort.Search(len(snapshots), func(i int) bool { return snapshots[i].Sequence > m.At })
		snapshots = snapshots[:end]
	}
	if len(snapshots) > 0 {
		end := snapshots[len(snapshots)-1].WindowEnd
		cutoff := end.Add(-time.Duration(seconds) * time.Second)
		i := 0
		for i < len(snapshots) && !snapshots[i].WindowEnd.After(cutoff) {
			i++
		}
		snapshots = snapshots[i:]
	}
	summary, err := report.Summarize(snapshots, f)
	if err != nil {
		return err
	}
	m.Summary = summary
	m.Rows = nil
	if m.Threads {
		type row struct {
			samples    uint64
			procedures int
		}
		threads := map[uint32]row{}
		for _, h := range summary.Rows {
			r := threads[h.ThreadID]
			r.samples += h.Samples
			r.procedures++
			threads[h.ThreadID] = r
		}
		for tid, r := range threads {
			share := 0.0
			if summary.Accepted > 0 {
				share = 100 * float64(r.samples) / float64(summary.Accepted)
			}
			m.Rows = append(m.Rows, []string{strconv.FormatUint(r.samples, 10), fmt.Sprintf("%.2f%%", share), strconv.FormatUint(uint64(tid), 10), "", fmt.Sprintf("%d procedimentos/endereços", r.procedures), "", "thread", ""})
		}
		m.sortRows()
		return nil
	}
	for _, h := range summary.Rows {
		name := h.Procedure
		if name == "" {
			name = h.RVA + " (" + h.Reason + ")"
		}
		share := 0.0
		if summary.Accepted > 0 {
			share = 100 * float64(h.Samples) / float64(summary.Accepted)
		}
		m.Rows = append(m.Rows, []string{strconv.FormatUint(h.Samples, 10), fmt.Sprintf("%.2f%%", share), strconv.FormatUint(uint64(h.ThreadID), 10), h.Module, name, h.RVA, h.Status, h.Unit})
	}
	m.sortRows()
	return nil
}

// Replace installs a complete, ordered history response atomically. Unlike live
// Append, it keeps the bounded archive passed by report.Read for cursor browsing.
func (m *Model) Replace(history []profiling.Snapshot) error {
	if len(history) == 0 {
		return errors.New("empty history")
	}
	if _, err := report.Summarize(history, report.Filter{}); err != nil {
		return err
	}
	clone := make([]profiling.Snapshot, len(history))
	var omitted uint64
	for i, s := range history {
		clone[i] = s.Clone()
		if i > 0 {
			omitted += s.Sequence - history[i-1].Sequence - 1
		}
	}
	m.History = clone
	m.Metrics = nil
	m.Session = history[0].SessionID
	m.Last = history[len(history)-1].Sequence
	m.Omitted = omitted
	if m.At > m.Last {
		m.At = 0
	}
	return nil
}
func (m *Model) sortRows() {
	col := m.SortColumn
	if col < 0 || col >= 8 {
		col = 0
	}
	sort.SliceStable(m.Rows, func(i, j int) bool {
		a, b := m.Rows[i], m.Rows[j]
		cmp := 0
		if m.Comparison != nil && col == 2 {
			av, _ := strconv.ParseFloat(a[col], 64)
			bv, _ := strconv.ParseFloat(b[col], 64)
			if av < bv {
				cmp = -1
			} else if av > bv {
				cmp = 1
			}
		} else if col == 0 || col == 2 || (m.Comparison != nil && col == 1) {
			av, _ := strconv.ParseUint(a[col], 10, 64)
			bv, _ := strconv.ParseUint(b[col], 10, 64)
			if av < bv {
				cmp = -1
			} else if av > bv {
				cmp = 1
			}
		} else if col == 1 {
			av, _ := strconv.ParseFloat(strings.TrimSuffix(a[col], "%"), 64)
			bv, _ := strconv.ParseFloat(strings.TrimSuffix(b[col], "%"), 64)
			if av < bv {
				cmp = -1
			} else if av > bv {
				cmp = 1
			}
		} else {
			cmp = strings.Compare(a[col], b[col])
		}
		if cmp == 0 {
			return strings.Join(a, "\x00") < strings.Join(b, "\x00")
		}
		if m.Ascending {
			return cmp < 0
		}
		return cmp > 0
	})
}
