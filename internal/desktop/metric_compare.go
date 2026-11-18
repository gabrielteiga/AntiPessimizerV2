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

func (m *Model) metricComparisonHeaders() []string {
	unit := m.MetricUnit.label()
	return []string{"Chamadas antes", "Chamadas depois", "Δ chamadas", "Exclusivo antes (" + unit + ")", "Exclusivo depois (" + unit + ")", "Δ média exclusiva (" + unit + ")", "Inclusivo antes (" + unit + ")", "Inclusivo depois (" + unit + ")", "Δ média inclusiva (" + unit + ")", "Média exclusiva antes (" + unit + ")", "Média exclusiva depois (" + unit + ")", "Média inclusiva antes (" + unit + ")", "Média inclusiva depois (" + unit + ")", "Procedimento", "Unidade Pascal", "Módulo", "RVA"}
}
func (m *Model) rebuildMetricComparison(f report.Filter) error {
	comparison := m.MetricComparison
	unit := m.MetricUnit.normalized()
	if !unit.Valid() {
		return errors.New("unknown comparison time unit")
	}
	if f.Thread != 0 {
		return errors.New("comparison aggregates all thread instances; clear the TID filter")
	}
	if unit == ClockTicks && comparison.Before.Frequency != comparison.After.Frequency {
		return errors.New("raw ticks require matching clock frequencies; choose ns/us/ms/s")
	}
	var changes []instrumentation.ComparisonChange
	for _, change := range comparison.Changes {
		if f.Module != "" && change.Module != f.Module || f.Unit != "" && change.Unit != f.Unit || f.Text != "" && !strings.Contains(strings.ToLower(change.Module+" "+change.Unit+" "+change.Procedure+" "+change.RVA), strings.ToLower(f.Text)) {
			continue
		}
		changes = append(changes, change)
	}
	value := func(p *float64) float64 {
		if p == nil {
			return 0
		}
		return *p
	}
	sort.Slice(changes, func(i, j int) bool {
		a, b := changes[i], changes[j]
		order := 0
		switch m.SortColumn {
		case 0:
			order = cmp.Compare(a.Before.Calls, b.Before.Calls)
		case 1:
			order = cmp.Compare(a.After.Calls, b.After.Calls)
		case 2:
			order = instrumentation.CompareCallDelta(a, b)
		case 3:
			order = cmp.Compare(a.Before.ExclusiveTicks, b.Before.ExclusiveTicks)
		case 4:
			order = cmp.Compare(a.After.ExclusiveTicks, b.After.ExclusiveTicks)
		case 5:
			order = cmp.Compare(value(a.AverageExclusiveDeltaNS), value(b.AverageExclusiveDeltaNS))
		case 6:
			order = cmp.Compare(a.Before.InclusiveTicks, b.Before.InclusiveTicks)
		case 7:
			order = cmp.Compare(a.After.InclusiveTicks, b.After.InclusiveTicks)
		case 8:
			order = cmp.Compare(value(a.AverageInclusiveDeltaNS), value(b.AverageInclusiveDeltaNS))
		case 9:
			order = cmp.Compare(value(a.Before.AverageExclusiveNS), value(b.Before.AverageExclusiveNS))
		case 10:
			order = cmp.Compare(value(a.After.AverageExclusiveNS), value(b.After.AverageExclusiveNS))
		case 11:
			order = cmp.Compare(value(a.Before.AverageInclusiveNS), value(b.Before.AverageInclusiveNS))
		case 12:
			order = cmp.Compare(value(a.After.AverageInclusiveNS), value(b.After.AverageInclusiveNS))
		case 13:
			order = strings.Compare(a.Procedure, b.Procedure)
		case 14:
			order = strings.Compare(a.Unit, b.Unit)
		case 15:
			order = strings.Compare(a.Module, b.Module)
		case 16:
			order = strings.Compare(a.RVA, b.RVA)
		}
		if order == 0 {
			if a.ModuleID != b.ModuleID {
				return a.ModuleID < b.ModuleID
			}
			return a.RVA < b.RVA
		}
		if m.Ascending {
			return order < 0
		}
		return order > 0
	})
	average := func(value *float64, ticks, calls uint64) string {
		if value == nil {
			return "--"
		}
		return unit.average(*value, ticks, calls)
	}
	delta := func(value *float64) string {
		if value == nil {
			return "--"
		}
		number := *value
		switch unit {
		case Microseconds:
			number /= 1000
		case Milliseconds:
			number /= 1e6
		case Seconds:
			return fmt.Sprintf("%+.9f", number/1e9)
		case ClockTicks:
			number = number * float64(comparison.Before.Frequency) / 1e9
		}
		return fmt.Sprintf("%+.6f", number)
	}
	m.Rows = nil
	for _, c := range changes {
		a, b := c.Before, c.After
		m.Rows = append(m.Rows, []string{strconv.FormatUint(a.Calls, 10), strconv.FormatUint(b.Calls, 10), c.CallDelta, unit.total(a.ExclusiveNS, a.ExclusiveTicks), unit.total(b.ExclusiveNS, b.ExclusiveTicks), delta(c.AverageExclusiveDeltaNS), unit.total(a.InclusiveNS, a.InclusiveTicks), unit.total(b.InclusiveNS, b.InclusiveTicks), delta(c.AverageInclusiveDeltaNS), average(a.AverageExclusiveNS, a.ExclusiveTicks, a.Calls), average(b.AverageExclusiveNS, b.ExclusiveTicks, b.Calls), average(a.AverageInclusiveNS, a.InclusiveTicks, a.Calls), average(b.AverageInclusiveNS, b.InclusiveTicks, b.Calls), c.Procedure, c.Unit, c.Module, c.RVA})
	}
	return nil
}
