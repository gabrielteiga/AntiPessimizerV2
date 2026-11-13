package stream

import (
	"bytes"
	"errors"
	"io"
	"strings"
	"testing"
	"time"

	"github.com/gabrielteiga/AntiPessimizerV2/internal/profiling"
)

func snapshot() profiling.Snapshot {
	start := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	return profiling.Snapshot{SchemaVersion: 1, SessionID: "test", Sequence: 1, Target: profiling.Target{PID: 1, Start: start, Image: "lab"}, Mode: "sampling", Source: "synthetic", WindowStart: start, WindowEnd: start.Add(time.Second), Published: start.Add(time.Second)}
}

type fragment struct{ r io.Reader }

func (f fragment) Read(p []byte) (int, error) {
	if len(p) > 1 {
		p = p[:1]
	}
	return f.r.Read(p)
}

type shortWriter struct{}

func (shortWriter) Write(p []byte) (int, error) { return len(p) - 1, nil }
func TestFramingAndVersions(t *testing.T) {
	var b bytes.Buffer
	if err := Write(&b, snapshot()); err != nil {
		t.Fatal(err)
	}
	wire := b.String()
	r := NewReader(fragment{strings.NewReader(wire)})
	if s, err := r.Next(); err != nil || s.Sequence != 1 {
		t.Fatal(s, err)
	}
	if _, err := r.Next(); !errors.Is(err, io.EOF) {
		t.Fatal(err)
	}
	for _, bad := range []string{wire[:len(wire)/2], strings.Replace(wire, "\"schema_version\":1", "\"schema_version\":2", 1), strings.Repeat("x", MaxFrameBytes+1)} {
		if _, err := NewReader(strings.NewReader(bad)).Next(); err == nil {
			t.Fatal("accepted invalid frame")
		}
	}
	r = NewReader(strings.NewReader(wire + wire))
	if _, err := r.Next(); err != nil {
		t.Fatal(err)
	}
	if _, err := r.Next(); err == nil {
		t.Fatal("accepted repeated sequence")
	}
	compatible := strings.Replace(wire, "{", "{\"future_optional\":true,", 1)
	if _, err := NewReader(strings.NewReader(compatible)).Next(); err != nil {
		t.Fatal("optional field rejected", err)
	}
	if err := Write(shortWriter{}, snapshot()); !errors.Is(err, io.ErrShortWrite) {
		t.Fatal(err)
	}
}
func TestDisconnectedWriter(t *testing.T) {
	r, w := io.Pipe()
	r.Close()
	if err := Write(w, snapshot()); !errors.Is(err, io.ErrClosedPipe) {
		t.Fatal(err)
	}
	w.Close()
}
func BenchmarkEncodeSnapshot(b *testing.B) {
	s := snapshot()
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		if err := Write(io.Discard, s); err != nil {
			b.Fatal(err)
		}
	}
}
