package desktop

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/gabrielteiga/AntiPessimizerV2/internal/instrumentation"
)

func TestLiveMetricsAreCumulativeAndRetainedWithinBounds(t *testing.T) {
	records := metricRecords(t)
	var m Model
	for _, r := range records {
		if err := m.AppendMetrics(r); err != nil {
			t.Fatal(err)
		}
	}
	current, _ := m.CurrentMetrics()
	if current.Metrics.Rows[0].Calls != 2 || current.Metrics.Rows[0].InclusiveNS != 40000000 {
		t.Fatal("cumulative records were added", current)
	}
	records[1].Metrics.Rows[0].Calls = 99
	if m.Metrics[1].Metrics.Rows[0].Calls != 2 {
		t.Fatal("live model aliases producer")
	}
	baseline := m.Metrics[1].Clone()
	m.At = 1
	for sequence := uint64(3); sequence <= 300; sequence++ {
		r := baseline.Clone()
		r.Sequence = sequence
		r.At += sequence
		if err := m.AppendMetrics(r); err != nil {
			t.Fatal(err)
		}
	}
	if len(m.Metrics) != liveMetricsFrames || m.Last != 300 || m.At != m.Metrics[0].Sequence || m.metricRows > liveMetricsRows || m.metricBytes > liveMetricsBytes {
		t.Fatal("retention/cursor unbounded", len(m.Metrics), m.At, m.Last)
	}
	r := baseline.Clone()
	r.Sequence = 302
	r.At += 302
	if err := m.AppendMetrics(r); err != nil {
		t.Fatal(err)
	}
	if m.Omitted != 1 {
		t.Fatal("coalesced update not reported", m.Omitted)
	}
	invalid := r.Clone()
	invalid.Sequence++
	invalid.SessionID = "other"
	if err := m.AppendMetrics(invalid); err == nil || m.Last != 302 {
		t.Fatal("invalid live update changed state", err)
	}
	var archive Model
	if err := archive.ReplaceMetrics(metricRecords(t)); err != nil {
		t.Fatal(err)
	}
	for sequence := uint64(3); sequence <= 300; sequence++ {
		r := baseline.Clone()
		r.Sequence = sequence
		r.At += sequence
		if err := archive.AppendMetrics(r); err != nil {
			t.Fatal(err)
		}
	}
	if len(archive.Metrics) != liveMetricsFrames {
		t.Fatal("archive/follow transition unbounded")
	}
}
func receiveFollow(t *testing.T, ch <-chan metricFollowUpdate) metricFollowUpdate {
	t.Helper()
	select {
	case u := <-ch:
		return u
	case <-time.After(3 * time.Second):
		t.Fatal("metrics follower did not update")
		return metricFollowUpdate{}
	}
}
func TestFollowGrowingFileWaitsForCompleteFramesAndDoesNotOwnWriter(t *testing.T) {
	records := metricRecords(t)
	path := filepath.Join(t.TempDir(), "live.ndjson")
	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0600)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	ch := make(chan metricFollowUpdate, 1)
	done := make(chan struct{})
	go func() { defer close(done); followMetrics(ctx, path, ch) }()
	if err := instrumentation.WriteRecord(f, records[0]); err != nil {
		t.Fatal(err)
	}
	if u := receiveFollow(t, ch); u.err != nil || u.record.Sequence != 1 {
		t.Fatal(u)
	}
	var wire bytes.Buffer
	if err := instrumentation.WriteRecord(&wire, records[1]); err != nil {
		t.Fatal(err)
	}
	data := wire.Bytes()
	if _, err := f.Write(data[:len(data)/2]); err != nil {
		t.Fatal(err)
	}
	select {
	case u := <-ch:
		t.Fatal("partial append became a record", u)
	case <-time.After(80 * time.Millisecond):
	}
	if _, err := f.Write(data[len(data)/2:]); err != nil {
		t.Fatal(err)
	}
	if u := receiveFollow(t, ch); u.err != nil || u.record.Sequence != 2 || u.record.Metrics.Rows[0].Calls != 2 {
		t.Fatal(u)
	}
	cancel()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("closing viewer stranded follower")
	}
	// The same writer remains open and usable after the viewer disconnects.
	if _, err := f.Write([]byte("\n")); err != nil {
		t.Fatal("viewer closed collector output", err)
	}
}
func TestFollowRejectsTruncationAndInvalidUpdates(t *testing.T) {
	records := metricRecords(t)
	for _, truncate := range []bool{false, true} {
		path := filepath.Join(t.TempDir(), "live.ndjson")
		var wire bytes.Buffer
		if err := instrumentation.WriteRecord(&wire, records[0]); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, wire.Bytes(), 0600); err != nil {
			t.Fatal(err)
		}
		ctx, cancel := context.WithCancel(context.Background())
		ch := make(chan metricFollowUpdate, 1)
		done := make(chan struct{})
		go func() { defer close(done); followMetrics(ctx, path, ch) }()
		if u := receiveFollow(t, ch); u.err != nil {
			t.Fatal(u.err)
		}
		if truncate {
			if err := os.Truncate(path, 0); err != nil {
				t.Fatal(err)
			}
		} else {
			f, err := os.OpenFile(path, os.O_WRONLY|os.O_APPEND, 0600)
			if err != nil {
				t.Fatal(err)
			}
			bad := records[1].Clone()
			bad.SessionID = "different"
			b, err := json.Marshal(bad)
			if err != nil {
				t.Fatal(err)
			}
			_, err = f.Write(append(b, '\n'))
			f.Close()
			if err != nil {
				t.Fatal(err)
			}
		}
		u := receiveFollow(t, ch)
		if u.err == nil || !u.stopped || u.record.Sequence != 1 {
			t.Fatal("invalid append replaced valid metrics", u)
		}
		cancel()
		<-done
	}
}

func TestLiveMetricsMailboxKeepsNewestCumulativeTotals(t *testing.T) {
	records := metricRecords(t)
	ch := make(chan metricFollowUpdate, 1)
	publishMetricFollow(context.Background(), ch, metricFollowUpdate{record: records[0], firstSequence: 1})
	publishMetricFollow(context.Background(), ch, metricFollowUpdate{record: records[1], firstSequence: 1})
	u := <-ch
	if u.record.Sequence != 2 || u.record.Metrics.Rows[0].Calls != 2 || u.firstSequence != 1 || len(ch) != 0 {
		t.Fatal("slow UI lost latest totals", u)
	}
}
