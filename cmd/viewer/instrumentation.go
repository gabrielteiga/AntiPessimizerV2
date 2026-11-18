package main

import (
	"fmt"
	"io"

	"github.com/gabrielteiga/AntiPessimizerV2/internal/desktop"
	"github.com/gabrielteiga/AntiPessimizerV2/internal/instrumentation"
	"github.com/gabrielteiga/AntiPessimizerV2/internal/report"
)

func viewInstrumentation(in io.Reader, out io.Writer, filter report.Filter, units ...desktop.MetricTimeUnit) error {
	unit := desktop.Milliseconds
	if len(units) > 0 {
		unit = units[0]
	}
	if !unit.Valid() {
		return fmt.Errorf("invalid instrumentation time unit")
	}
	reader := instrumentation.NewMetricsReader(in)
	var previous uint64
	for {
		r, err := reader.Next()
		if err == io.EOF {
			if previous == 0 {
				return fmt.Errorf("no instrumentation metrics received")
			}
			return nil
		}
		if err != nil {
			return err
		}
		var gap uint64
		if previous > 0 {
			gap = r.Sequence - previous - 1
		}
		previous = r.Sequence
		if _, err := fmt.Fprintf(out, "snapshot=%d mode=instrumentation source=%s clock=%s frequency_hz=%d epoch=%d ticks=%d open_calls=%d abandoned_calls=%d incomplete=%t omitted_updates=%d\n", r.Sequence, r.Source, r.Clock, r.Metrics.Frequency, r.Metrics.Epoch, r.At, r.Metrics.OpenCalls, r.Metrics.AbandonedCalls, r.Metrics.Incomplete, gap); err != nil {
			return err
		}
		var model desktop.Model
		if err := model.ReplaceMetrics([]instrumentation.Record{r}); err != nil {
			return err
		}
		model.MetricUnit = unit
		if err := model.Rebuild(0, filter); err != nil {
			return err
		}
		for _, row := range model.Rows {
			if _, err := fmt.Fprintf(out, "  calls=%s exclusive_%s=%s average_exclusive_%s=%s inclusive_%s=%s average_inclusive_%s=%s tid=%s thread_instance_ticks=%s %s!%s unit=%s rva=%s unwound_calls=%s active_frames=%s\n", row[0], unit, row[1], unit, row[2], unit, row[3], unit, row[4], row[5], row[6], row[7], row[8], row[10], row[9], row[11], row[12]); err != nil {
				return err
			}
		}
	}
}
