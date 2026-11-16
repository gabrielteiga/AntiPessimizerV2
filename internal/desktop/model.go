// Package desktop implements the independent native Windows viewer.
package desktop

import (
	"fmt"
	"strconv"
	"time"

	"github.com/gabrielteiga/AntiPessimizerV2/internal/profiling"
	"github.com/gabrielteiga/AntiPessimizerV2/internal/report"
)

type Model struct {
	History []profiling.Snapshot
	Summary report.Summary
	Rows    [][]string
	Session string
	Last    uint64
	Omitted uint64
}

func (m *Model) Append(s profiling.Snapshot) error {
	if err := s.Validate(); err != nil {
		return err
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
	snapshots := m.History
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
	for _, h := range summary.Rows {
		name := h.Procedure
		if name == "" {
			name = h.RVA + " (" + h.Reason + ")"
		}
		share := 0.0
		if summary.Accepted > 0 {
			share = 100 * float64(h.Samples) / float64(summary.Accepted)
		}
		m.Rows = append(m.Rows, []string{strconv.FormatUint(h.Samples, 10), fmt.Sprintf("%.2f%%", share), strconv.FormatUint(uint64(h.ThreadID), 10), h.Module, name, h.RVA, h.Status})
	}
	return nil
}
