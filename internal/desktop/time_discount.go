package desktop

import (
	"github.com/gabrielteiga/AntiPessimizerV2/internal/instrumentation"
	"strconv"
	"strings"
)

// The original gui_cycles_to_unit subtracts a fixed amount per completed call
// and saturates at zero. QPC ticks use the frequency of each capture.
func discountedTicks(ticks, calls, discount uint64) uint64 {
	if discount != 0 && calls > ticks/discount {
		return 0
	}
	return ticks - calls*discount
}

func discountedMetricTotals(t instrumentation.ProcedureTotals, frequency, discount uint64) (instrumentation.ProcedureTotals, error) {
	if discount == 0 {
		return t, nil
	}
	t.InclusiveTicks = discountedTicks(t.InclusiveTicks, t.Calls, discount)
	t.ExclusiveTicks = discountedTicks(t.ExclusiveTicks, t.Calls, discount)
	var err error
	t.InclusiveNS, err = instrumentation.NanosecondsFromTicks(t.InclusiveTicks, frequency)
	if err != nil {
		return t, err
	}
	t.ExclusiveNS, err = instrumentation.NanosecondsFromTicks(t.ExclusiveTicks, frequency)
	if err != nil {
		return t, err
	}
	if t.Calls > 0 {
		inclusive := float64(t.InclusiveTicks) * 1e9 / float64(frequency) / float64(t.Calls)
		exclusive := float64(t.ExclusiveTicks) * 1e9 / float64(frequency) / float64(t.Calls)
		t.AverageInclusiveNS, t.AverageExclusiveNS = &inclusive, &exclusive
	}
	return t, nil
}

func parseCallDiscount(text string) (uint64, error) {
	text = strings.TrimSpace(text)
	if text == "" {
		return 0, nil
	}
	return strconv.ParseUint(text, 10, 64)
}

func (m *Model) metricTimeLabel() string {
	label := m.MetricUnit.label()
	if m.CallDiscountTicks != 0 {
		label += ", ajustado"
	}
	return label
}
