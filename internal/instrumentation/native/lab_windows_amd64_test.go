package native

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/gabrielteiga/AntiPessimizerV2/internal/instrumentation"
	"golang.org/x/sys/windows"
)

func labPath(t *testing.T) string {
	t.Helper()
	p := os.Getenv("AP_NATIVE_LAB")
	if p == "" {
		t.Skip("enabled after building the controlled native producer in Windows CI")
	}
	return p
}

func TestNativeProducerUnicodeWorkloadPath(t *testing.T) {
	original := labPath(t)
	dir := filepath.Join(t.TempDir(), "carga nativa – ação")
	if err := os.Mkdir(dir, 0700); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{filepath.Base(original), "AntiPessimizerNative.dll"} {
		b, err := os.ReadFile(filepath.Join(filepath.Dir(original), name))
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, name), b, 0600); err != nil {
			t.Fatal(err)
		}
	}
	var out bytes.Buffer
	if err := RunLab(context.Background(), LabOptions{Workload: filepath.Join(dir, filepath.Base(original)), Threads: 1, Iterations: 1, Capacity: 1024, Timeout: 10 * time.Second}, &out); err != nil {
		t.Fatal(err)
	}
	records, err := instrumentation.ReadMetrics(&out)
	if err != nil {
		t.Fatal(err)
	}
	if len(records[len(records)-1].Metrics.Rows) != 4 {
		t.Fatal("native path capture lost procedures")
	}
}
func TestNativeProducerCallsTimesAndThreadIsolation(t *testing.T) {
	path := labPath(t)
	var out bytes.Buffer
	if err := RunLab(context.Background(), LabOptions{Workload: path, Threads: 3, Iterations: 10, Capacity: 1024, Timeout: 10 * time.Second}, &out); err != nil {
		t.Fatal(err, out.String())
	}
	records, err := instrumentation.ReadMetrics(&out)
	if err != nil {
		t.Fatal(err)
	}
	r := records[len(records)-1]
	if r.Source != "native-hook" || r.Clock != "qpc" || r.Metrics.Incomplete || r.Metrics.OpenCalls != 0 || r.Metrics.AbandonedCalls != 0 || len(r.Metrics.Rows) != 12 {
		t.Fatalf("invalid native capture: %+v", r)
	}
	threads := map[uint32]map[uint32]instrumentation.Row{}
	want := map[uint32]uint64{1: 10, 2: 20, 3: 40, 4: 10}
	for _, row := range r.Metrics.Rows {
		if row.Calls != want[row.ProcedureID] || row.ThreadInstance == 0 {
			t.Fatalf("unexpected native count/identity: %+v", row)
		}
		if threads[row.ThreadID] == nil {
			threads[row.ThreadID] = map[uint32]instrumentation.Row{}
		}
		threads[row.ThreadID][row.ProcedureID] = row
	}
	if len(threads) != 3 {
		t.Fatal("native threads merged", threads)
	}
	for _, rows := range threads {
		p, c, recursive, f := rows[1], rows[2], rows[3], rows[4]
		if p.InclusiveTicks == 0 || c.InclusiveTicks == 0 || p.InclusiveTicks != p.ExclusiveTicks+c.InclusiveTicks+recursive.InclusiveTicks+f.InclusiveTicks {
			t.Fatal("native nesting times do not balance", rows)
		}
		if c.ExclusiveTicks != c.InclusiveTicks || recursive.InclusiveTicks != recursive.ExclusiveTicks || f.ExclusiveTicks != f.InclusiveTicks {
			t.Fatal("native leaf/recursive time mismatch", rows)
		}
		if p.AverageInclusiveNS != float64(p.InclusiveTicks)*1e9/float64(r.Metrics.Frequency)/10 {
			t.Fatal("wrong native average", p)
		}
	}
	t.Logf("real native callbacks/QPC: %d rows across %d threads, parent=10 child=20 recursive=40 float=10 per thread", len(r.Metrics.Rows), len(threads))
}
func TestNativeProducerCancellationIsIncomplete(t *testing.T) {
	path := labPath(t)
	var out bytes.Buffer
	err := RunLab(context.Background(), LabOptions{Workload: path, Threads: 2, Iterations: 1000, Capacity: 1024, Timeout: 500 * time.Millisecond}, &out)
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatal("native cancellation not propagated", err)
	}
	records, readErr := instrumentation.ReadMetrics(&out)
	if readErr != nil {
		t.Fatal(readErr, err)
	}
	last := records[len(records)-1]
	if !last.Metrics.Incomplete || last.Metrics.OpenCalls != 0 {
		t.Fatal("cancelled native capture advertised complete", last)
	}
}
func TestNativeProducerOverflowInvalidatesExactness(t *testing.T) {
	path := labPath(t)
	var out bytes.Buffer
	err := RunLab(context.Background(), LabOptions{Workload: path, Threads: 2, Iterations: 100, Capacity: 1, Timeout: 10 * time.Second}, &out)
	if !errors.Is(err, ErrLoss) {
		t.Fatal("native overflow not detected", err)
	}
	records, readErr := instrumentation.ReadMetrics(&out)
	if readErr != nil {
		t.Fatal(readErr, err)
	}
	r := records[len(records)-1]
	if !r.Metrics.Incomplete || r.Metrics.OpenCalls != 0 || r.NativeLostEvents == 0 || r.Version != 3 {
		t.Fatal("lost native events advertised exact", r)
	}
}

func TestNativeProducerControllerExitKillsChild(t *testing.T) {
	path := labPath(t)
	if os.Getenv("AP_NATIVE_CONTROLLER_HELPER") == "1" {
		err := RunLab(context.Background(), LabOptions{Workload: path, Threads: 2, Iterations: 10000, Capacity: 1024, Timeout: 20 * time.Second}, os.Stdout)
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		os.Exit(0)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, os.Args[0], "-test.run=^TestNativeProducerControllerExitKillsChild$")
	cmd.Env = append(os.Environ(), "AP_NATIVE_CONTROLLER_HELPER=1")
	var diagnostics bytes.Buffer
	cmd.Stderr = &diagnostics
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	done := make(chan struct{})
	go func() { cmd.Wait(); close(done) }()
	defer func() { cmd.Process.Kill(); <-done }()
	r, err := instrumentation.NewMetricsReader(stdout).Next()
	if err != nil {
		cmd.Process.Kill()
		<-done
		t.Fatalf("helper capture: %v %s", err, diagnostics.String())
	}
	// Abrupt termination bypasses Go defers; kernel job ownership must still
	// terminate the native target and its drain/worker threads.
	if err := cmd.Process.Kill(); err != nil {
		t.Fatal(err)
	}
	<-done
	h, err := windows.OpenProcess(windows.SYNCHRONIZE|windows.PROCESS_QUERY_LIMITED_INFORMATION, false, r.Target.PID)
	if errors.Is(err, windows.ERROR_INVALID_PARAMETER) {
		return
	}
	if err != nil {
		t.Fatal(err)
	}
	defer windows.CloseHandle(h)
	var created, exit, kernel, user windows.Filetime
	if err := windows.GetProcessTimes(h, &created, &exit, &kernel, &user); err != nil {
		t.Fatal(err)
	}
	if !time.Unix(0, created.Nanoseconds()).UTC().Equal(r.Target.Start) {
		return
	} // PID already reused
	if result, err := windows.WaitForSingleObject(h, 5000); err != nil || result != windows.WAIT_OBJECT_0 {
		t.Fatalf("owned native target survived controller termination: %d %v", result, err)
	}
}
