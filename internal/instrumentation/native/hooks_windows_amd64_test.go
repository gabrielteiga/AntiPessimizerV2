package native

import (
	"bytes"
	"context"
	"errors"
	"path/filepath"
	"testing"
	"time"

	"github.com/gabrielteiga/AntiPessimizerV2/internal/instrumentation"
)

func TestNativeProducerInstallsHooksInOwnedProcess(t *testing.T) {
	path := filepath.Join(filepath.Dir(labPath(t)), "APHookLab.exe")
	var out bytes.Buffer
	if err := RunLab(context.Background(), LabOptions{Workload: path, Threads: 3, Iterations: 10, Capacity: 1024, Timeout: 15 * time.Second, InstallHooks: true}, &out); err != nil {
		t.Fatal(err, out.String())
	}
	records, err := instrumentation.ReadMetrics(&out)
	if err != nil {
		t.Fatal(err)
	}
	r := records[len(records)-1]
	if r.Metrics.Incomplete || r.Metrics.OpenCalls != 0 || r.Metrics.AbandonedCalls != 0 || len(r.Metrics.Rows) != 3 {
		t.Fatal("incomplete installed-hook capture", r)
	}
	seen := map[uint32]bool{}
	for _, row := range r.Metrics.Rows {
		if row.Calls != 10 || row.Location.Procedure != "APHookSum" || row.InclusiveTicks == 0 || row.ExclusiveTicks != row.InclusiveTicks || row.ActiveFrames != 0 || row.AverageInclusiveNS != float64(row.InclusiveTicks)*1e9/float64(r.Metrics.Frequency)/10 || seen[row.ThreadID] {
			t.Fatal("wrong installed-hook metrics", row)
		}
		seen[row.ThreadID] = true
	}
	t.Log("Go controller installed native hooks in its owned process: 10 real calls in each of 3 threads; child verified arguments, LastError, RIP data, return values and complete code restoration")
}
func TestNativeProducerInstalledHookOverflowIsIncomplete(t *testing.T) {
	path := filepath.Join(filepath.Dir(labPath(t)), "APHookLab.exe")
	var out bytes.Buffer
	err := RunLab(context.Background(), LabOptions{Workload: path, Threads: 2, Iterations: 10000, Capacity: 1, Timeout: 15 * time.Second, InstallHooks: true}, &out)
	if !errors.Is(err, ErrLoss) {
		t.Fatal("installed-hook overflow hidden", err)
	}
	records, err := instrumentation.ReadMetrics(&out)
	if err != nil || len(records) == 0 {
		t.Fatal(err)
	}
	if !records[len(records)-1].Metrics.Incomplete {
		t.Fatal("lost installed-hook events advertised exact")
	}
}
