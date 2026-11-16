package symbols_test

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/gabrielteiga/AntiPessimizerV2/internal/aggregation"
	"github.com/gabrielteiga/AntiPessimizerV2/internal/profiling"
	"github.com/gabrielteiga/AntiPessimizerV2/internal/symbols"
	"github.com/gabrielteiga/AntiPessimizerV2/internal/testfixture"
)

func TestImageSymbolsFeedHotspotAggregation(t *testing.T) {
	path := filepath.Join(t.TempDir(), "lab.exe")
	if err := os.WriteFile(path, testfixture.Image(true), 0600); err != nil {
		t.Fatal(err)
	}
	image, err := symbols.LoadImage(path)
	if err != nil {
		t.Fatal(err)
	}
	start := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	agg, err := aggregation.New(aggregation.Config{SessionID: "td32-integration", Target: profiling.Target{PID: 1, Start: start, Image: "lab.exe"}, Source: "synthetic", Start: start, Width: time.Second, Capacity: 10, TopN: 10, SourceLossKnown: true})
	if err != nil {
		t.Fatal(err)
	}
	// Two instruction pointers inside one procedure must aggregate into one row.
	for _, rva := range []uint64{0x5020, 0x502f, 0x5030} {
		loc, ok := image.Index.Resolve(0x7ff000000000, 0x7ff000000000+rva)
		if !ok {
			t.Fatal("address missing from image")
		}
		if err := agg.Add(profiling.Sample{At: start, ThreadID: 42, Location: loc}); err != nil {
			t.Fatal(err)
		}
	}
	snapshot, err := agg.Close(agg.End())
	if err != nil {
		t.Fatal(err)
	}
	if err := snapshot.Validate(); err != nil {
		t.Fatal(err)
	}
	if len(snapshot.Hotspots) != 2 || snapshot.Quality.Accepted != 3 {
		t.Fatalf("%+v", snapshot)
	}
	if snapshot.Hotspots[0].Procedure != "Lab.Hot" || snapshot.Hotspots[0].Samples != 2 || snapshot.Hotspots[1].Status != "unknown" {
		t.Fatal(snapshot.Hotspots)
	}
}
