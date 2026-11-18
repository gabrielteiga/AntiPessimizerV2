package desktop

import (
	"fmt"
	"strconv"
)

// MetricTimeUnit controls presentation only; stored ticks, QPC frequency,
// ordering and NDJSON values never change. Raw ticks are not CPU cycles.
type MetricTimeUnit string

const (
	Nanoseconds  MetricTimeUnit = "ns"
	Microseconds MetricTimeUnit = "us"
	Milliseconds MetricTimeUnit = "ms"
	Seconds      MetricTimeUnit = "s"
	ClockTicks   MetricTimeUnit = "ticks"
)

func (u MetricTimeUnit) normalized() MetricTimeUnit {
	if u == "" {
		return Milliseconds
	}
	return u
}
func (u MetricTimeUnit) Valid() bool {
	switch u.normalized() {
	case Nanoseconds, Microseconds, Milliseconds, Seconds, ClockTicks:
		return true
	}
	return false
}
func (u MetricTimeUnit) label() string { return string(u.normalized()) }
func (u MetricTimeUnit) total(ns, ticks uint64) string {
	switch u.normalized() {
	case Nanoseconds:
		return strconv.FormatUint(ns, 10)
	case Microseconds:
		return fmt.Sprintf("%d.%03d", ns/1000, ns%1000)
	case Seconds:
		return fmt.Sprintf("%d.%09d", ns/1e9, ns%1e9)
	case ClockTicks:
		return strconv.FormatUint(ticks, 10)
	default:
		return milliseconds(ns)
	}
}
func (u MetricTimeUnit) average(ns float64, ticks, calls uint64) string {
	value := ns
	switch u.normalized() {
	case Microseconds:
		value /= 1000
	case Milliseconds:
		value /= 1e6
	case Seconds:
		return fmt.Sprintf("%.9f", value/1e9)
	case ClockTicks:
		value = 0
		if calls > 0 {
			value = float64(ticks) / float64(calls)
		}
	}
	return fmt.Sprintf("%.6f", value)
}
