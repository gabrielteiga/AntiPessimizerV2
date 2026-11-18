package main

import (
	"bytes"
	"context"
	"io"
	"os"
	"strings"
	"testing"

	"github.com/gabrielteiga/AntiPessimizerV2/internal/instrumentation"
)

func TestViewerInstrumentedMetrics(t *testing.T) {
	events, err := os.ReadFile("../../testdata/instrumentation/nested.ndjson")
	if err != nil {
		t.Fatal(err)
	}
	var wire bytes.Buffer
	if err := instrumentation.Replay(context.Background(), bytes.NewReader(events), &wire, instrumentation.Config{MaxThreads: 16, MaxDepth: 32, MaxRows: 64}); err != nil {
		t.Fatal(err)
	}
	var out bytes.Buffer
	if err := run([]string{"--instrumentation", "--unit", "Lab", "--thread", "7"}, bytes.NewReader(wire.Bytes()), &out, io.Discard); err != nil {
		t.Fatal(err)
	}
	for _, text := range []string{"mode=instrumentation source=synthetic", "calls=2 exclusive_ms=30.000000 average_exclusive_ms=15.000000 inclusive_ms=40.000000 average_inclusive_ms=20.000000", "tid=7 thread_instance_ticks=0", "unwound_calls=0 active_frames=0"} {
		if !strings.Contains(out.String(), text) {
			t.Fatalf("missing %q in %s", text, out.String())
		}
	}
	out.Reset()
	if err := run([]string{"--instrumentation", "--thread", "8"}, bytes.NewReader(wire.Bytes()), &out, io.Discard); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(out.String(), "calls=") && strings.Contains(out.String(), "tid=") {
		t.Fatal("thread filter ignored", out.String())
	}
	if err := run([]string{"--instrumentation"}, strings.NewReader(""), io.Discard, io.Discard); err == nil {
		t.Fatal("empty metrics accepted")
	}
	if err := run([]string{"--instrumentation"}, bytes.NewReader(events), io.Discard, io.Discard); err == nil {
		t.Fatal("events accepted as metrics")
	}
	if err := run(nil, bytes.NewReader(wire.Bytes()), io.Discard, io.Discard); err == nil {
		t.Fatal("metrics accepted as samples")
	}
	for _, args := range [][]string{{"--instrumentation", "--op", "start"}, {"--instrumentation", "--save", "x"}, {"--instrumentation", "--compare", "x"}} {
		if err := run(args, strings.NewReader(""), io.Discard, io.Discard); err == nil {
			t.Fatal("ambiguous options accepted", args)
		}
	}
	path := t.TempDir() + "/metrics.ndjson"
	if err := os.WriteFile(path, wire.Bytes(), 0600); err != nil {
		t.Fatal(err)
	}
	if err := run([]string{"--instrumentation", "--replay", path}, nil, io.Discard, io.Discard); err != nil {
		t.Fatal(err)
	}
}

func TestViewerInstrumentedCumulativeAndGaps(t *testing.T) {
	events, err := os.ReadFile("../../testdata/instrumentation/nested.ndjson")
	if err != nil {
		t.Fatal(err)
	}
	var wire bytes.Buffer
	if err := instrumentation.Replay(context.Background(), bytes.NewReader(events), &wire, instrumentation.Config{MaxThreads: 16, MaxDepth: 32, MaxRows: 64}); err != nil {
		t.Fatal(err)
	}
	r, err := instrumentation.NewMetricsReader(bytes.NewReader(wire.Bytes())).Next()
	if err != nil {
		t.Fatal(err)
	}
	r.Sequence = 3
	if err := instrumentation.WriteRecord(&wire, r); err != nil {
		t.Fatal(err)
	}
	var out bytes.Buffer
	if err := run([]string{"--instrumentation"}, &wire, &out, io.Discard); err != nil {
		t.Fatal(err)
	}
	if strings.Count(out.String(), "calls=2 exclusive_ms=30.000000") != 2 || !strings.Contains(out.String(), "omitted_updates=1") || strings.Contains(out.String(), "calls=4") {
		t.Fatal(out.String())
	}
}

func TestViewerMetricTimeUnits(t *testing.T) {
	events, err := os.ReadFile("../../testdata/instrumentation/nested.ndjson")
	if err != nil {
		t.Fatal(err)
	}
	var wire bytes.Buffer
	if err := instrumentation.Replay(context.Background(), bytes.NewReader(events), &wire, instrumentation.Config{MaxThreads: 16, MaxDepth: 32, MaxRows: 64}); err != nil {
		t.Fatal(err)
	}
	for unit, want := range map[string]string{"ns": "exclusive_ns=30000000", "us": "exclusive_us=30000.000", "s": "exclusive_s=0.030000000", "ticks": "exclusive_ticks=30"} {
		var output bytes.Buffer
		if err := run([]string{"--instrumentation", "--time-unit", unit}, bytes.NewReader(wire.Bytes()), &output, io.Discard); err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(output.String(), want) || !strings.Contains(output.String(), "calls=2") {
			t.Fatal(unit, output.String())
		}
	}
	for _, args := range [][]string{{"--time-unit", "cycles"}, {"--time-unit", "ns"}} {
		if err := run(args, bytes.NewReader(wire.Bytes()), io.Discard, io.Discard); err == nil {
			t.Fatal("incompatible unit accepted", args)
		}
	}
}
