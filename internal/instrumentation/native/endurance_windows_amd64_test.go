package native

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"testing"
	"time"
)

type enduranceReport struct {
	Version          int            `json:"version"`
	ImageSHA256      string         `json:"image_sha256"`
	DLLSHA256        string         `json:"dll_sha256"`
	RequestedSeconds int            `json:"requested_seconds_per_mode"`
	ExpectedCalls    uint64         `json:"expected_calls_per_mode"`
	MeasuredCalls    uint64         `json:"instrumented_calls"`
	NativeLostEvents uint64         `json:"native_lost_events_at_least"`
	LiveSnapshots    int            `json:"live_snapshots"`
	MaxDeliveryGapNS int64          `json:"max_delivery_gap_ns"`
	GoHeapBefore     uint64         `json:"go_heap_before_bytes"`
	GoHeapAfter      uint64         `json:"go_heap_after_bytes"`
	BaselineRate     float64        `json:"baseline_worker_calls_per_second"`
	InstrumentedRate float64        `json:"instrumented_worker_calls_per_second"`
	Baseline         stressWorkload `json:"baseline"`
	Instrumented     stressWorkload `json:"instrumented"`
}

type enduranceObserver struct {
	stressObserver
	lastDelivery time.Time
	longestGap   time.Duration
}

func (out *enduranceObserver) Write(b []byte) (int, error) {
	n, err := out.stressObserver.Write(b)
	if err == nil {
		now := time.Now()
		if !out.lastDelivery.IsZero() {
			out.longestGap = max(out.longestGap, now.Sub(out.lastDelivery))
		}
		out.lastDelivery = now
	}
	return n, err
}

// Exercise the same binary in sequential baseline and instrumented trials.
// Rate and publication intervals are observations from the current runner.
func TestNativeEnduranceBaselineAndInstrumented(t *testing.T) {
	raw := os.Getenv("AP_NATIVE_ENDURANCE_SECONDS")
	if raw == "" {
		t.Skip("native endurance is enabled by the Windows workflows")
	}
	seconds, err := strconv.Atoi(raw)
	if err != nil || seconds < 6 || seconds > 7200 {
		t.Fatal("AP_NATIVE_ENDURANCE_SECONDS must be 6 through 7200")
	}
	const workers, delayMS = uint32(3), 100
	rounds := uint32(min(max(seconds/60, 3), 60))
	iterations := (uint32(seconds*1000/delayMS) + rounds - 1) / rounds
	o := launchFixture(t)
	o.Timeout = time.Duration(seconds+max(15, seconds/4)) * time.Second
	o.Args = []string{"--threads", strconv.FormatUint(uint64(workers), 10), "--rounds", strconv.FormatUint(uint64(rounds), 10),
		"--iterations", strconv.FormatUint(uint64(iterations), 10), "--delay-ms", strconv.Itoa(delayMS), "--pace", "1"}
	binaryHash := func(path string) string {
		t.Helper()
		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		return fmt.Sprintf("%x", sha256.Sum256(data))
	}
	evidence := enduranceReport{Version: 1, ImageSHA256: binaryHash(o.Image), DLLSHA256: binaryHash(o.DLL), RequestedSeconds: seconds,
		ExpectedCalls: uint64(workers)*uint64(rounds)*uint64(iterations) + 1}
	dir := t.TempDir()
	baselinePath := filepath.Join(dir, "baseline.json")
	baselineArgs := append(append([]string(nil), o.Args...), "--report", baselinePath)
	ctx, cancel := context.WithTimeout(context.Background(), o.Timeout)
	if data, err := exec.CommandContext(ctx, o.Image, baselineArgs...).CombinedOutput(); err != nil {
		cancel()
		t.Fatal("endurance baseline failed", err, string(data))
	}
	cancel()
	evidence.Baseline = readStressWorkload(t, baselinePath, workers, rounds, iterations)

	reportPath := filepath.Join(dir, "instrumented.json")
	o.Args = append(o.Args, "--report", reportPath)
	o.ImageID = "sha256:" + evidence.ImageSHA256
	var out enduranceObserver
	var stats runtime.MemStats
	runtime.GC()
	runtime.ReadMemStats(&stats)
	evidence.GoHeapBefore = stats.HeapAlloc
	if err := Launch(context.Background(), o, &out); err != nil {
		t.Fatal("instrumented endurance failed", err)
	}
	runtime.GC()
	runtime.ReadMemStats(&stats)
	runtime.KeepAlive(&out)
	evidence.GoHeapAfter = stats.HeapAlloc
	evidence.Instrumented = readStressWorkload(t, reportPath, workers, rounds, iterations)
	evidence.NativeLostEvents = out.last.NativeLostEvents
	evidence.LiveSnapshots = out.snapshots - 1 // the last snapshot is the terminal record
	evidence.MaxDeliveryGapNS = out.longestGap.Nanoseconds()

	metrics := out.last.Metrics
	if evidence.LiveSnapshots < 2 || metrics.Incomplete || metrics.OpenCalls != 0 || metrics.AbandonedCalls != 0 ||
		evidence.NativeLostEvents != 0 || len(metrics.Rows) != int(workers*rounds)+1 {
		t.Fatal("endurance capture lost live delivery or completed calls", out.snapshots, metrics)
	}
	var workerRows, finalizers int
	for _, row := range metrics.Rows {
		switch row.Calls {
		case uint64(iterations):
			workerRows++
		case 1:
			finalizers++
		default:
			t.Fatal("unexpected endurance call count", row)
		}
		if row.UnwoundCalls != 0 || row.InclusiveTicks != row.ExclusiveTicks {
			t.Fatal("unexpected unwind or nesting in endurance workload", row)
		}
		evidence.MeasuredCalls += row.Calls
	}
	if evidence.MeasuredCalls != evidence.ExpectedCalls || workerRows != int(workers*rounds) || finalizers != 1 {
		t.Fatal("endurance lost worker or finalizer calls", evidence.MeasuredCalls, evidence.ExpectedCalls)
	}
	if evidence.Baseline.Frequency != evidence.Instrumented.Frequency || evidence.Instrumented.Frequency != metrics.Frequency {
		t.Fatal("endurance trials used different QPC frequencies")
	}
	workerCalls := float64(evidence.ExpectedCalls - 1)
	evidence.BaselineRate = workerCalls * float64(evidence.Baseline.Frequency) / float64(evidence.Baseline.ElapsedTicks)
	evidence.InstrumentedRate = workerCalls * float64(evidence.Instrumented.Frequency) / float64(evidence.Instrumented.ElapsedTicks)

	for _, mode := range []struct {
		name string
		data stressWorkload
	}{{"baseline", evidence.Baseline}, {"instrumented", evidence.Instrumented}} {
		if mode.data.ElapsedTicks < uint64(seconds)*mode.data.Frequency {
			t.Fatal("workload ended before its requested duration", mode.name, mode.data.ElapsedTicks)
		}
		// One complete round warms the fixture before checking later samples.
		warm := mode.data.Memory[1]
		for _, point := range mode.data.Memory[2:] {
			if point.PrivateBytes > warm.PrivateBytes+(16<<20) || point.Handles > warm.Handles+8 {
				t.Fatal("endurance target resources grew after warmup", mode.name, warm, point)
			}
		}
	}
	if evidence.GoHeapAfter > evidence.GoHeapBefore+(16<<20) {
		t.Fatal("collector retained excessive heap after endurance", evidence.GoHeapBefore, evidence.GoHeapAfter)
	}
	t.Logf("endurance %ds per mode: exact calls %d, %d live snapshots, longest inter-snapshot interval %s",
		seconds, evidence.MeasuredCalls, evidence.LiveSnapshots, out.longestGap)
	if path := os.Getenv("AP_NATIVE_ENDURANCE_REPORT"); path != "" {
		data, err := json.MarshalIndent(evidence, "", "  ")
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, append(data, '\n'), 0600); err != nil {
			t.Fatal(err)
		}
	}
}
