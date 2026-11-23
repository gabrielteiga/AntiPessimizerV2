package native

import (
	"bytes"
	"cmp"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"testing"
	"time"

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
	t.Log("64 six-byte procedures installed together: all return values preserved, 192 distinct rows and 384 exact calls; 2 MiB code strides plus a separate bounded runtime table")
}

func sparseBatchSelection(t *testing.T) LaunchOptions {
	t.Helper()
	names := map[string]uint32{}
	for i := uint32(0); i < MaxSelectedProcedures; i++ {
		names[fmt.Sprintf("APBatch%d", i)] = i<<24 | 0x00ffffff
	}
	o := frameSelectionFixture(t, names)
	slices.SortFunc(o.Procedures, func(a, b SelectedProcedure) int { return cmp.Compare(b.ID, a.ID) })
	return o
}

func TestNativeProducerInstruments256ProceduresOn16Threads(t *testing.T) {
	o := sparseBatchSelection(t)
	// One call per selected method gives 514 events per ring, bounded even if
	// the drain does not run before every worker has returned. The 16 workers
	// exercise exactly the 4096-row epoch limit without depending on delivery.
	o.Args = []string{"--threads", "16", "--iterations", "1", "--batch-count", "256", "--batch-once", "1"}

	type resources struct {
		Version      int    `json:"version"`
		Threads      int    `json:"threads"`
		BatchCount   int    `json:"batch_count"`
		PrivateBytes uint64 `json:"private_bytes"`
		Handles      uint32 `json:"handles"`
		Frequency    uint64 `json:"frequency"`
		Ticks        uint64 `json:"ticks"`
	}
	readResources := func(path string) resources {
		t.Helper()
		b, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		var r resources
		if err := json.Unmarshal(b, &r); err != nil {
			t.Fatal(err)
		}
		if r.Version != 1 || r.Threads != 16 || r.BatchCount != 256 || r.PrivateBytes == 0 || r.Handles == 0 || r.Frequency == 0 || r.Ticks == 0 {
			t.Fatal("invalid resource evidence", string(b))
		}
		return r
	}
	baselinePath := filepath.Join(t.TempDir(), "baseline.json")
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, o.Image, append(append([]string(nil), o.Args...), "--report", baselinePath)...)
	if data, err := cmd.CombinedOutput(); err != nil {
		t.Fatal("baseline behavior", err, string(data))
	}
	baseline := readResources(baselinePath)
	reportPath := filepath.Join(t.TempDir(), "instrumented.json")
	o.Args = append(o.Args, "--report", reportPath)
	var out stressObserver
	if err := Launch(context.Background(), o, &out); err != nil {
		t.Fatal(err)
	}
	measured := readResources(reportPath)
	if measured.PrivateBytes > baseline.PrivateBytes+32<<20 || measured.Handles > baseline.Handles+16 {
		t.Fatal("expanded selection exceeds bounded resource budget", baseline, measured)
	}
	t.Logf("same-target baseline/instrumented: private bytes %d/%d, handles %d/%d, QPC ticks %d/%d; no runner-independent speed claim", baseline.PrivateBytes, measured.PrivateBytes, baseline.Handles, measured.Handles, baseline.Ticks, measured.Ticks)
	last := out.last
	if last.Metrics.Incomplete || last.Metrics.OpenCalls != 0 || last.Metrics.AbandonedCalls != 0 || last.NativeLostEvents != 0 || len(last.Selection) != 256 || len(last.Metrics.Rows) != 4096 {
		t.Fatal("full selection incomplete", last.Metrics)
	}
	seen := map[uint32]int{}
	for _, row := range last.Metrics.Rows {
		if row.Calls != 1 || row.UnwoundCalls != 0 || row.InclusiveTicks != row.ExclusiveTicks {
			t.Fatal("expanded selection changed counts", row)
		}
		if row.AverageInclusiveNS != float64(row.InclusiveTicks)*1e9/float64(last.Metrics.Frequency) {
			t.Fatal("expanded mean changed", row)
		}
		seen[row.ProcedureID]++
	}
	for i, p := range o.Procedures {
		if got := last.Selection[i]; got.ID != p.ID || got.Location.Procedure != p.Name {
			t.Fatal("native catalog changed order or identity", i, got, p)
		}
		if seen[p.ID] != 16 {
			t.Fatal("omitted method/thread", p.ID, seen[p.ID])
		}
	}
	t.Logf("256 methods x 16 workers: 4096 distinct rows/calls, complete QPC metrics, %d snapshots / %d encoded bytes; code reservation <= %d bytes", out.snapshots, out.bytes, MaxSelectedProcedures*(largeHookStride+36))
}

func TestNativeProducerRejectsUnselectedSparseID(t *testing.T) {
	o := sparseBatchSelection(t)
	// This ID falls immediately before the largest selected ID, UINT32_MAX.
	o.Args = []string{"--threads", "1", "--iterations", "1", "--batch-count", "256", "--batch-once", "1", "--unselected-id", "4294967294"}
	var out stressObserver
	err := Launch(context.Background(), o, &out)
	if !errors.Is(err, ErrLoss) || out.last.NativeLostEvents != 1 || !out.last.Metrics.Incomplete {
		t.Fatalf("unknown ID must count as exactly one lost event: %v, lost=%d, incomplete=%t", err, out.last.NativeLostEvents, out.last.Metrics.Incomplete)
	}
}
