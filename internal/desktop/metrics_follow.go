package desktop

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"os"
	"time"

	"github.com/gabrielteiga/AntiPessimizerV2/internal/instrumentation"
)

const liveMetricsFrames = 256
const liveMetricsRows = 100000
const liveMetricsBytes = 64 << 20

// AppendMetrics keeps cumulative snapshots separate. Retention affects the
// timeline, never the latest call totals; input and retained rows do not alias.
func (m *Model) AppendMetrics(r instrumentation.Record) error {
	records := []instrumentation.Record{r}
	if len(m.Metrics) > 0 {
		records = []instrumentation.Record{m.Metrics[len(m.Metrics)-1], r}
	}
	if err := instrumentation.ValidateRecords(records); err != nil {
		return err
	}
	encoded, err := json.Marshal(r)
	if err != nil {
		return err
	}
	if len(encoded)+1 > instrumentation.MaxFrameBytes {
		return errors.New("instrumentation frame exceeds live retention limit")
	}
	if len(m.Metrics) > 0 && len(m.metricSizes) != len(m.Metrics) {
		// A caller may switch an already loaded archive into follow mode.
		sizes := make([]int, 0, len(m.Metrics))
		bytes, rows := 0, 0
		for _, old := range m.Metrics {
			b, err := json.Marshal(old)
			if err != nil {
				return err
			}
			sizes = append(sizes, len(b))
			bytes += len(b)
			rows += len(old.Metrics.Rows)
		}
		m.metricSizes, m.metricBytes, m.metricRows = sizes, bytes, rows
	}
	if len(m.Metrics) == 0 {
		*m = Model{MetricUnit: m.MetricUnit, Session: r.SessionID, SortColumn: 1}
	} else if m.Last > 0 {
		m.Omitted += r.Sequence - m.Last - 1
	}
	m.Metrics = append(m.Metrics, r.Clone())
	m.metricSizes = append(m.metricSizes, len(encoded))
	m.metricBytes += len(encoded)
	m.metricRows += len(r.Metrics.Rows)
	m.Last = r.Sequence
	for len(m.Metrics) > 1 && (len(m.Metrics) > liveMetricsFrames || m.metricRows > liveMetricsRows || m.metricBytes > liveMetricsBytes) {
		m.metricBytes -= m.metricSizes[0]
		m.metricRows -= len(m.Metrics[0].Metrics.Rows)
		m.Metrics[0] = instrumentation.Record{}
		m.Metrics = m.Metrics[1:]
		m.metricSizes = m.metricSizes[1:]
	}
	if m.At != 0 && m.At < m.Metrics[0].Sequence {
		m.At = m.Metrics[0].Sequence
	}
	return nil
}

type metricFollowUpdate struct {
	firstSequence uint64

	record  instrumentation.Record
	err     error
	stopped bool
}

// A single retained update coalesces UI refreshes without blocking the reader.
// Records are cumulative, so skipping display frames cannot drop call totals.
func publishMetricFollow(ctx context.Context, ch chan metricFollowUpdate, u metricFollowUpdate) {
	if ctx.Err() != nil {
		return
	}
	select {
	case ch <- u:
		return
	default:
	}
	select {
	case <-ch:
	default:
	}
	select {
	case ch <- u:
	case <-ctx.Done():
	}
}

type growingMetricsFile struct {
	ctx      context.Context
	file     *os.File
	path     string
	position int64
	tick     *time.Ticker
}

func (f *growingMetricsFile) Read(b []byte) (int, error) {
	for {
		if err := f.ctx.Err(); err != nil {
			return 0, err
		}
		n, err := f.file.Read(b)
		if n > 0 {
			f.position += int64(n)
			return n, nil
		}
		if err != io.EOF {
			return n, err
		}
		select {
		case <-f.ctx.Done():
			return 0, f.ctx.Err()
		case <-f.tick.C:
		}
		opened, err := f.file.Stat()
		if err != nil {
			return 0, err
		}
		current, err := os.Stat(f.path)
		if err != nil {
			return 0, err
		}
		if !os.SameFile(opened, current) {
			return 0, errors.New("instrumentation file was replaced while following")
		}
		if opened.Size() < f.position {
			return 0, errors.New("instrumentation file was truncated while following")
		}
	}
}

// followMetrics owns only a read handle. Canceling or closing the viewer never
// closes the collector's output or sends a command to the running application.
func followMetrics(ctx context.Context, path string, updates chan metricFollowUpdate) {
	var latest instrumentation.Record
	var firstSequence uint64
	f, err := os.Open(path)
	if err != nil {
		publishMetricFollow(ctx, updates, metricFollowUpdate{err: err, stopped: true})
		return
	}
	defer f.Close()
	ticker := time.NewTicker(50 * time.Millisecond)
	defer ticker.Stop()
	source := growingMetricsFile{ctx: ctx, file: f, path: path, tick: ticker}
	reader := instrumentation.NewMetricsReader(&source)
	for {
		r, err := reader.Next()
		if err != nil {
			if ctx.Err() == nil {
				publishMetricFollow(ctx, updates, metricFollowUpdate{record: latest, firstSequence: firstSequence, err: err, stopped: true})
			}
			return
		}
		latest = r
		if firstSequence == 0 {
			firstSequence = r.Sequence
		}
		publishMetricFollow(ctx, updates, metricFollowUpdate{record: r, firstSequence: firstSequence})
	}
}
