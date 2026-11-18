package native

import (
	"bytes"
	"context"
	"fmt"
	"testing"

	"github.com/gabrielteiga/AntiPessimizerV2/internal/instrumentation"
)

func TestNativeProducerInstruments64ProceduresTogether(t *testing.T) {
	names := map[string]uint32{}
	for i := uint32(0); i < 64; i++ {
		names[fmt.Sprintf("APBatch%d", i)] = i + 1
	}
	o := frameSelectionFixture(t, names)
	// Two calls per method per worker: 258 events fit in each ring even if
	// the drain thread never gets CPU during the complete measured section.
	o.Args = []string{"--threads", "3", "--iterations", "1", "--delay-ms", "0"}
	var output bytes.Buffer
	if err := Launch(context.Background(), o, &output); err != nil {
		t.Fatal(err, output.String())
	}
	records, err := instrumentation.ReadMetrics(&output)
	if err != nil {
		t.Fatal(err)
	}
	last := records[len(records)-1]
	if last.Metrics.Incomplete || last.Metrics.OpenCalls != 0 || last.Metrics.AbandonedCalls != 0 || len(last.Selection) != 64 || len(last.Metrics.Rows) != 192 {
		t.Fatal("full selection lost functions/threads", last)
	}
	seen := map[uint32]int{}
	for _, row := range last.Metrics.Rows {
		if row.Calls != 2 || row.UnwoundCalls != 0 || row.InclusiveTicks != row.ExclusiveTicks {
			t.Fatal("full selection changed counts/times", row)
		}
		seen[row.ProcedureID]++
	}
	for id := uint32(1); id <= 64; id++ {
		if seen[id] != 3 {
			t.Fatal("selection omitted a procedure/thread", id, seen[id])
		}
	}
	t.Log("64 six-byte procedures installed together: all return values preserved, 192 distinct rows and 384 exact calls; bounded 512 KiB probe allocation")
}
