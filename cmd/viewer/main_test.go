package main

import (
	"bytes"
	"io"
	"strings"
	"testing"
	"time"

	"github.com/gabrielteiga/AntiPessimizerV2/internal/aggregation"
	"github.com/gabrielteiga/AntiPessimizerV2/internal/profiling"
	"github.com/gabrielteiga/AntiPessimizerV2/internal/stream"
)

func TestViewerFilterAndGap(t *testing.T) {
	start := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	a, err := aggregation.New(aggregation.Config{SessionID: "test", Target: profiling.Target{PID: 1, Start: start, Image: "lab"}, Source: "synthetic", Start: start, Width: time.Second, Capacity: 2, TopN: 2})
	if err != nil {
		t.Fatal(err)
	}
	var wire bytes.Buffer
	for i := 0; i < 3; i++ {
		for _, tid := range []uint32{1, 2} {
			if err := a.Add(profiling.Sample{At: a.End().Add(-time.Second), ThreadID: tid, Location: profiling.Location{ModuleID: "build", Module: "lab", RVA: "0x0000000000000000", Status: "unknown", Reason: "no_symbol"}}); err != nil {
				t.Fatal(err)
			}
		}
		s, err := a.Close(a.End())
		if err != nil {
			t.Fatal(err)
		}
		if i != 1 {
			if err := stream.Write(&wire, s); err != nil {
				t.Fatal(err)
			}
		}
	}
	var out bytes.Buffer
	if err := run([]string{"--thread", "2"}, &wire, &out, io.Discard); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(out.String(), "tid=1") || !strings.Contains(out.String(), "tid=2") || !strings.Contains(out.String(), "omitted_updates=1") || !strings.Contains(out.String(), "no_symbol") {
		t.Fatal(out.String())
	}
	if err := run(nil, strings.NewReader(""), io.Discard, io.Discard); err == nil {
		t.Fatal("empty stream accepted")
	}
}
