package instrumentation

import (
	"bytes"
	"context"
	"encoding/json"
	"github.com/gabrielteiga/AntiPessimizerV2/internal/stream"
	"io"
	"os"
	"strings"
	"testing"
)

func limits() Config { return Config{MaxThreads: 16, MaxDepth: 32, MaxRows: 64} }
func input(t *testing.T) string {
	t.Helper()
	b, err := os.ReadFile("../../testdata/instrumentation/nested.ndjson")
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}
func TestReplayMetrics(t *testing.T) {
	var out bytes.Buffer
	if err := Replay(context.Background(), strings.NewReader(input(t)), &out, limits()); err != nil {
		t.Fatal(err)
	}
	var result Record
	if err := json.Unmarshal(out.Bytes(), &result); err != nil {
		t.Fatal(err)
	}
	if result.Protocol != MetricsProtocol || result.Source != "synthetic" || len(result.Metrics.Rows) != 2 {
		t.Fatal(result)
	}
	if _, err := stream.NewReader(bytes.NewReader(out.Bytes())).Next(); err == nil {
		t.Fatal("instrumentation metrics accepted as sampling snapshot")
	}
	a := result.Metrics.Rows[0]
	if a.Procedure != "Lab.Parent" || a.Calls != 2 || a.InclusiveTicks != 40 || a.ExclusiveTicks != 30 || a.AverageInclusiveNS != 20e6 || a.AverageExclusiveNS != 15e6 {
		t.Fatal(a)
	}
}
func TestReplayRejectsUnmatchedAndWrongModes(t *testing.T) {
	base := input(t)
	for name, s := range map[string]string{
		"sampling data":        `{"schema_version":1,"collection_mode":"sampling"}`,
		"unknown field":        strings.Replace(base, `"type":"enter"`, `"typo":true,"type":"enter"`, 1),
		"mismatched exit":      strings.Replace(base, `"procedure_id":2,"ticks":"25"`, `"procedure_id":1,"ticks":"25"`, 1),
		"open EOF":             strings.Split(base, `{"type":"exit"`)[0],
		"invalid extra fields": strings.Replace(base, `"type":"thread_start","thread_id":7`, `"type":"thread_start","procedure_id":1,"thread_id":7`, 1),
		"wrong source":         strings.Replace(base, `"source":"synthetic"`, `"source":"external"`, 1),
	} {
		t.Run(name, func(t *testing.T) {
			var out bytes.Buffer
			if err := Replay(context.Background(), strings.NewReader(s), &out, limits()); err == nil {
				t.Fatal("invalid event stream accepted")
			}
		})
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := Replay(ctx, strings.NewReader(base), &bytes.Buffer{}, limits()); err == nil {
		t.Fatal("cancellation ignored")
	}
}
func TestExplicitLossIsNotFabricatedCall(t *testing.T) {
	s := strings.Split(input(t), `{"type":"exit"`)[0] + "{\"type\":\"loss\",\"ticks\":\"25\"}\n"
	var out bytes.Buffer
	if err := Replay(context.Background(), strings.NewReader(s), &out, limits()); err != nil {
		t.Fatal(err)
	}
	var result Record
	if err := json.Unmarshal(out.Bytes(), &result); err != nil {
		t.Fatal(err)
	}
	if !result.Metrics.Incomplete || result.Metrics.AbandonedCalls != 2 || len(result.Metrics.Rows) != 0 {
		t.Fatal(result.Metrics)
	}
}
func FuzzReplay(f *testing.F) {
	b, _ := os.ReadFile("../../testdata/instrumentation/nested.ndjson")
	f.Add(b)
	f.Fuzz(func(t *testing.T, b []byte) {
		if len(b) > 1<<20 {
			t.Skip()
		}
		Replay(context.Background(), bytes.NewReader(b), io.Discard, limits())
	})
}

func TestReplayExplicitNativeCleanup(t *testing.T) {
	data := strings.Replace(input(t), `"type":"exit","thread_id":7,"procedure_id":2`, `"type":"unwind_exit","thread_id":7,"procedure_id":2`, 1)
	if data == input(t) {
		t.Fatal("fixture cleanup not replaced")
	}
	var out bytes.Buffer
	if err := Replay(context.Background(), strings.NewReader(data), &out, limits()); err != nil {
		t.Fatal(err)
	}
	var result Record
	if err := json.Unmarshal(out.Bytes(), &result); err != nil {
		t.Fatal(err)
	}
	if result.Metrics.Incomplete || result.Metrics.OpenCalls != 0 || result.Metrics.Rows[1].UnwoundCalls != 1 {
		t.Fatal(result)
	}
}
