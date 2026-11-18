package desktop

import (
	"cmp"
	"errors"
	"fmt"
	"sort"
	"strconv"
	"strings"

	"github.com/gabrielteiga/AntiPessimizerV2/internal/instrumentation"
	"github.com/gabrielteiga/AntiPessimizerV2/internal/report"
)

func (m *Model) ReplaceMetrics(records []instrumentation.Record) error {
	if err := instrumentation.ValidateRecords(records); err != nil {
		return err
	}
	clone := make([]instrumentation.Record, len(records))
	var omitted uint64
	for i, r := range records {
		clone[i] = r.Clone()
		if i > 0 {
			omitted += r.Sequence - records[i-1].Sequence - 1
		}
	}
	*m = Model{MetricUnit: m.MetricUnit, Metrics: clone, Session: clone[0].SessionID, Last: clone[len(clone)-1].Sequence, Omitted: omitted, SortColumn: 1}
	return nil
}

// CurrentMetrics selects a cumulative snapshot. Neither cursor navigation nor
// the sampling interval control can add cumulative metrics together.
func (m *Model) CurrentMetrics() (instrumentation.Record, bool) {
	if len(m.Metrics) == 0 {
		return instrumentation.Record{}, false
	}
	i := len(m.Metrics) - 1
	if m.At != 0 {
		i = sort.Search(len(m.Metrics), func(i int) bool { return m.Metrics[i].Sequence > m.At }) - 1
		if i < 0 {
			return instrumentation.Record{}, false
		}
	}
	return m.Metrics[i], true
}

func (m *Model) Headers() []string {
	if m.MetricComparison != nil {
		return m.metricComparisonHeaders()
	}
	if len(m.Metrics) > 0 {
		unit := m.MetricUnit.label()
		return []string{"Chamadas", "Exclusivo (" + unit + ")", "Média exclusiva (" + unit + ")", "Inclusivo (" + unit + ")", "Média inclusiva (" + unit + ")", "Thread", "Instância (ticks)", "Módulo", "Procedimento", "RVA", "Unidade Pascal", "Unwind", "Frames abertos"}
	}
	if m.Comparison != nil {
		return []string{"Antes", "Depois", "Δ amostras/s", "Módulo", "Procedimento", "RVA", "Presença", "Unidade Pascal"}
	}
	return []string{"Amostras", "Fração", "Thread", "Módulo", "Procedimento", "RVA", "Símbolo", "Unidade Pascal"}
}

func (m *Model) HistoryLength() int {
	if len(m.Metrics) > 0 {
		return len(m.Metrics)
	}
	return len(m.History)
}
func (m *Model) SequenceAt(index int) uint64 {
	if index < 0 || index >= m.HistoryLength() {
		return 0
	}
	if len(m.Metrics) > 0 {
		return m.Metrics[index].Sequence
	}
	return m.History[index].Sequence
}

func milliseconds(ns uint64) string { return fmt.Sprintf("%d.%06d", ns/1e6, ns%1e6) }

func (m *Model) rebuildMetrics(f report.Filter) error {
	unit := m.MetricUnit.normalized()
	if !unit.Valid() {
		return errors.New("unknown instrumentation time unit")
	}
	m.Rows = nil
	r, ok := m.CurrentMetrics()
	if !ok {
		return nil
	}
	var rows []instrumentation.Row
	for _, row := range r.Metrics.Rows {
		if f.Unit != "" && row.Unit != f.Unit || f.Module != "" && row.Module != f.Module || f.Thread != 0 && row.ThreadID != f.Thread || f.Text != "" && !strings.Contains(strings.ToLower(row.Module+" "+row.Unit+" "+row.Procedure+" "+row.RVA), strings.ToLower(f.Text)) {
			continue
		}
		rows = append(rows, row)
	}
	sort.Slice(rows, func(i, j int) bool {
		a, b := rows[i], rows[j]
		order := 0
		switch m.SortColumn {
		case 0:
			order = cmp.Compare(a.Calls, b.Calls)
		case 1:
			order = cmp.Compare(a.ExclusiveTicks, b.ExclusiveTicks)
		case 2:
			order = cmp.Compare(a.AverageExclusiveNS, b.AverageExclusiveNS)
		case 3:
			order = cmp.Compare(a.InclusiveTicks, b.InclusiveTicks)
		case 4:
			order = cmp.Compare(a.AverageInclusiveNS, b.AverageInclusiveNS)
		case 5:
			order = cmp.Compare(a.ThreadID, b.ThreadID)
		case 6:
			order = cmp.Compare(a.ThreadInstance, b.ThreadInstance)
		case 7:
			order = strings.Compare(a.Module, b.Module)
		case 8:
			order = strings.Compare(a.Procedure, b.Procedure)
		case 9:
			order = strings.Compare(a.RVA, b.RVA)
		case 10:
			order = strings.Compare(a.Unit, b.Unit)
		case 11:
			order = cmp.Compare(a.UnwoundCalls, b.UnwoundCalls)
		case 12:
			order = cmp.Compare(a.ActiveFrames, b.ActiveFrames)
		}
		if order == 0 {
			if a.ThreadID != b.ThreadID {
				return a.ThreadID < b.ThreadID
			}
			if a.ThreadInstance != b.ThreadInstance {
				return a.ThreadInstance < b.ThreadInstance
			}
			return a.ProcedureID < b.ProcedureID
		}
		if m.Ascending {
			return order < 0
		}
		return order > 0
	})
	for _, row := range rows {
		m.Rows = append(m.Rows, []string{strconv.FormatUint(row.Calls, 10), unit.total(row.ExclusiveNS, row.ExclusiveTicks), unit.average(row.AverageExclusiveNS, row.ExclusiveTicks, row.Calls), unit.total(row.InclusiveNS, row.InclusiveTicks), unit.average(row.AverageInclusiveNS, row.InclusiveTicks, row.Calls), strconv.FormatUint(uint64(row.ThreadID), 10), strconv.FormatUint(row.ThreadInstance, 10), row.Module, row.Procedure, row.RVA, row.Unit, strconv.FormatUint(row.UnwoundCalls, 10), strconv.FormatUint(uint64(row.ActiveFrames), 10)})
	}
	return nil
}
