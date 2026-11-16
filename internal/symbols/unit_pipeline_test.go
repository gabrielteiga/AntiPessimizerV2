package symbols_test

import (
	"bytes"
	"github.com/gabrielteiga/AntiPessimizerV2/internal/aggregation"
	"github.com/gabrielteiga/AntiPessimizerV2/internal/profiling"
	"github.com/gabrielteiga/AntiPessimizerV2/internal/stream"
	"github.com/gabrielteiga/AntiPessimizerV2/internal/symbols"
	"testing"
	"time"
)

func TestUnitSurvivesAggregationAndSerialization(t *testing.T) {
	index, err := symbols.New("id", "lab.exe", 0x6000, []symbols.Procedure{{Name: "Hot", Unit: "Pricing", RVA: 0x5000, Size: 32}})
	if err != nil {
		t.Fatal(err)
	}
	loc, ok := index.Resolve(0x100000, 0x105010)
	if !ok {
		t.Fatal("missing module")
	}
	start := time.Now().UTC()
	agg, err := aggregation.New(aggregation.Config{SessionID: "unit", Target: profiling.Target{PID: 1, Start: start, Image: "lab.exe"}, Source: "synthetic", Start: start, Width: time.Second, Capacity: 10, TopN: 10})
	if err != nil {
		t.Fatal(err)
	}
	if err := agg.Add(profiling.Sample{At: start, ThreadID: 7, Location: loc}); err != nil {
		t.Fatal(err)
	}
	snapshot, err := agg.Close(agg.End())
	if err != nil {
		t.Fatal(err)
	}
	var buf bytes.Buffer
	if err := stream.Write(&buf, snapshot); err != nil {
		t.Fatal(err)
	}
	got, err := stream.NewReader(&buf).Next()
	if err != nil || got.Hotspots[0].Unit != "Pricing" {
		t.Fatal(got, err)
	}
}
