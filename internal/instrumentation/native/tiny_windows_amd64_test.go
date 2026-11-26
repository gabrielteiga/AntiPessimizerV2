package native

import (
	"bytes"
	"context"
	"os/exec"
	"testing"
	"time"

	"github.com/gabrielteiga/AntiPessimizerV2/internal/instrumentation"
)

func TestNativeProducerTinyOwnedFunctionsAndApplicationBreakpoint(t *testing.T) {
	o := selectedFixture(t, "APTailTarget.exe", map[string]uint32{"APTinyRet": 1, "APTinyNop": 2, "APTinyZero": 3, "APTinyAdd": 4})
	for _, p := range o.Procedures {
		if p.Size != p.ID {
			t.Fatal("fixture did not retain the expected 1–4 byte extents", p)
		}
	}
	o.Args = []string{"--threads", "3", "--iterations", "10", "--check-tiny", "1"}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	if data, err := exec.CommandContext(ctx, o.Image, o.Args...).CombinedOutput(); err != nil {
		t.Fatal("baseline tiny functions or application breakpoint failed", err, string(data))
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
	if last.Incomplete || last.OpenCalls != 0 || last.AbandonedCalls != 0 || len(last.Rows) != 12 {
		t.Fatal(last)
	}
	for _, row := range last.Rows {
		if row.Calls != 10 || row.UnwoundCalls != 0 || row.InclusiveTicks != row.ExclusiveTicks {
			t.Fatal(row)
		}
	}
}

func TestNativeTinyGettersPreservePhysicalCallerContext(t *testing.T) {
	o := selectedFixture(t, "APTailTarget.exe", map[string]uint32{"APTinyCallerRBP": 1, "APTinyCallerRSP": 2})
	for _, p := range o.Procedures {
		if p.Size != 4 {
			t.Fatal("context getter extent changed", p)
		}
	}
	o.Args = []string{"--threads", "3", "--iterations", "10", "--check-tiny", "2"}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	if data, err := exec.CommandContext(ctx, o.Image, o.Args...).CombinedOutput(); err != nil {
		t.Fatal("baseline context getter", err, string(data))
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
	if last.Incomplete || last.OpenCalls != 0 || last.AbandonedCalls != 0 || len(last.Rows) != 6 {
		t.Fatal(last)
	}
	for _, row := range last.Rows {
		if row.Calls != 10 || row.UnwoundCalls != 0 || row.InclusiveTicks == 0 || row.InclusiveTicks != row.ExclusiveTicks {
			t.Fatal(row)
		}
	}
}
