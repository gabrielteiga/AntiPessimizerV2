package native

import (
	"bytes"
	"context"
	"os/exec"
	"testing"
	"time"

	"github.com/gabrielteiga/AntiPessimizerV2/internal/instrumentation"
)

func TestNativeProducerCPUIDPreservesResultsAndRBXDuringUnwind(t *testing.T) {
	o := frameSelectionFixture(t, map[string]uint32{"APFrameCPUID": 1})
	o.Args = append(o.Args, "--check-cpuid", "1")
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	if data, err := exec.CommandContext(ctx, o.Image, o.Args...).CombinedOutput(); err != nil {
		t.Fatal("baseline CPUID values or RBX unwind differ", err, string(data))
	}
	var output bytes.Buffer
	if err := Launch(ctx, o, &output); err != nil {
		t.Fatal(err, output.String())
	}
	records, err := instrumentation.ReadMetrics(&output)
	if err != nil {
		t.Fatal(err)
	}
	last := records[len(records)-1].Metrics
	if last.Incomplete || last.OpenCalls != 0 || last.AbandonedCalls != 0 || len(last.Rows) != 3 {
		t.Fatal(last)
	}
	for _, row := range last.Rows {
		if row.Calls != 2 || row.UnwoundCalls != 1 || row.InclusiveTicks != row.ExclusiveTicks {
			t.Fatal(row)
		}
	}
}
