package native

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"

	"github.com/gabrielteiga/AntiPessimizerV2/internal/instrumentation"
)

type memoryPoint struct {
	Round          uint32 `json:"round"`
	PrivateBytes   uint64 `json:"private_bytes"`
	WorkingSet     uint64 `json:"working_set_bytes"`
	PeakWorkingSet uint64 `json:"peak_working_set_bytes"`
	Handles        uint32 `json:"handles"`
}

type stressWorkload struct {
	workloadCost
	Rounds uint32        `json:"rounds"`
	Memory []memoryPoint `json:"memory"`
}

type stressEvidence struct {
	Version          int            `json:"version"`
	Scenario         string         `json:"scenario"`
	ImageID          string         `json:"image_id"`
	Workload         stressWorkload `json:"workload"`
	ExpectedCalls    uint64         `json:"expected_calls"`
	MeasuredCalls    uint64         `json:"measured_calls"`
	Rows             int            `json:"rows"`
	Snapshots        int            `json:"snapshots"`
	EncodedBytes     uint64         `json:"encoded_bytes"`
	GoHeapBefore     uint64         `json:"go_heap_before_bytes"`
	GoHeapAfter      uint64         `json:"go_heap_after_bytes"`
	ElapsedNS        int64          `json:"elapsed_ns"`
	Incomplete       bool           `json:"incomplete"`
	NativeLostEvents uint64         `json:"native_lost_events_at_least"`
	Error            string         `json:"error,omitempty"`
}

// Keep only the latest cumulative snapshot, never an archive proportional to
// elapsed time. This also measures the collector's live heap after collection.
type stressObserver struct {
	last      instrumentation.Record
	snapshots int
	bytes     uint64
}

func (w *stressObserver) Write(b []byte) (int, error) {
	if err := json.Unmarshal(b, &w.last); err != nil {
		return 0, err
	}
	w.snapshots++
	w.bytes += uint64(len(b))
	return len(b), nil
}

func readStressWorkload(t *testing.T, path string, workers, rounds, iterations uint32) stressWorkload {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var result stressWorkload
	if err := json.Unmarshal(data, &result); err != nil {
		t.Fatal(err)
	}
	if result.Version != 1 || result.Clock != "qpc" || result.Frequency == 0 || result.ElapsedTicks == 0 || result.Threads != workers || result.Rounds != rounds || result.Iterations != iterations || len(result.Workers) != int(workers*rounds) || len(result.Memory) != int(rounds+1) {
		t.Fatal("invalid stress report", result)
	}
	var checksum uint64
	for i := uint32(0); i < iterations; i++ {
		if i%2 != 0 {
			checksum += 777
		} else {
			checksum += uint64(125 + i)
		}
	}
	for _, worker := range result.Workers {
		if worker.Ticks == 0 || worker.Ticks > result.ElapsedTicks || worker.Checksum != checksum {
			t.Fatal("stress changed a worker result", worker)
		}
	}
	for i, point := range result.Memory {
		if point.Round != uint32(i) || point.PrivateBytes == 0 || point.WorkingSet == 0 || point.PeakWorkingSet < point.WorkingSet || point.Handles == 0 {
			t.Fatal("invalid process memory sample", point)
		}
	}
	return result
}

func TestNativeStressThreadReuseMemoryAndSaturation(t *testing.T) {
	if os.Getenv("AP_NATIVE_STRESS") != "1" {
		t.Skip("stress evidence enabled separately in Windows CI")
	}
	image, err := os.ReadFile(launchFixture(t).Image)
	if err != nil {
		t.Fatal(err)
	}
	imageID := fmt.Sprintf("sha256:%x", sha256.Sum256(image))
	var artifact *os.File
	if path := os.Getenv("AP_NATIVE_STRESS_REPORT"); path != "" {
		var err error
		artifact, err = os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
		if err != nil {
			t.Fatal(err)
		}
		defer func() {
			if err := artifact.Close(); err != nil {
				t.Error(err)
			}
		}()
	}
	writeEvidence := func(e stressEvidence) {
		t.Helper()
		t.Logf("%s: calls=%d/%d rows=%d snapshots=%d incomplete=%t elapsed=%s Go heap=%d -> %d bytes", e.Scenario, e.MeasuredCalls, e.ExpectedCalls, e.Rows, e.Snapshots, e.Incomplete, time.Duration(e.ElapsedNS), e.GoHeapBefore, e.GoHeapAfter)
		if artifact != nil {
			if err := json.NewEncoder(artifact).Encode(e); err != nil {
				t.Fatal(err)
			}
		}
	}
	const workers, rounds, iterations = uint32(16), uint32(20), uint32(100)
	for _, instrumented := range []bool{false, true} {
		t.Run(fmt.Sprint("reuse-instrumented-", instrumented), func(t *testing.T) {
			o := launchFixture(t)
			path := filepath.Join(t.TempDir(), "memory.json")
			o.Args = []string{"--threads", "16", "--rounds", "20", "--iterations", "100", "--delay-ms", "0", "--round-delay-ms", "5", "--report", path}
			o.Timeout = 20 * time.Second
			e := stressEvidence{Version: 1, ImageID: imageID, Scenario: fmt.Sprint("thread-reuse-instrumented-", instrumented), ExpectedCalls: uint64(workers*rounds*iterations) + 1}
			var output stressObserver
			var before, after runtime.MemStats
			runtime.GC()
			runtime.ReadMemStats(&before)
			started := time.Now()
			if instrumented {
				if err := Launch(context.Background(), o, &output); err != nil {
					t.Fatal(err)
				}
				s := output.last.Metrics
				if s.Incomplete || s.OpenCalls != 0 || s.AbandonedCalls != 0 || len(s.Rows) != int(workers*rounds)+1 {
					t.Fatal("reuse lost a thread life", s)
				}
				instances := map[string]bool{}
				var workerRows, finalizers int
				for _, row := range s.Rows {
					key := fmt.Sprintf("%d/%d", row.ThreadID, row.ThreadInstance)
					if instances[key] || row.UnwoundCalls != 0 || row.ExclusiveTicks != row.InclusiveTicks {
						t.Fatal("reuse merged thread instances or corrupted times", row)
					}
					instances[key] = true
					if row.Calls == uint64(iterations) {
						workerRows++
					} else if row.Calls == 1 {
						finalizers++
					} else {
						t.Fatal("reuse changed a call count", row)
					}
					e.MeasuredCalls += row.Calls
				}
				if workerRows != int(workers*rounds) || finalizers != 1 || e.MeasuredCalls != e.ExpectedCalls {
					t.Fatal("reuse lost workers/finalizer", workerRows, finalizers, e)
				}
				e.Rows, e.Snapshots, e.EncodedBytes = len(s.Rows), output.snapshots, output.bytes
			} else {
				runCostBaseline(t, o)
			}
			e.ElapsedNS = time.Since(started).Nanoseconds()
			e.Workload = readStressWorkload(t, path, workers, rounds, iterations)
			runtime.GC()
			runtime.ReadMemStats(&after)
			runtime.KeepAlive(&output)
			e.GoHeapBefore, e.GoHeapAfter = before.HeapAlloc, after.HeapAlloc
			writeEvidence(e)
			// Tolerances cover process/heap/loader warming, not speed on a shared
			// runner. Compare after four rounds, once all native rings can be reused.
			warm := e.Workload.Memory[4]
			var maxPrivateGrowth int64
			var maxHandleGrowth int64
			for _, point := range e.Workload.Memory[5:] {
				maxPrivateGrowth = max(maxPrivateGrowth, int64(point.PrivateBytes)-int64(warm.PrivateBytes))
				maxHandleGrowth = max(maxHandleGrowth, int64(point.Handles)-int64(warm.Handles))
				if point.PrivateBytes > warm.PrivateBytes+(16<<20) || point.Handles > warm.Handles+8 {
					t.Fatal("target memory/handles grew after buffer warmup", warm, point)
				}
			}
			t.Logf("after round 4: warm private=%d handles=%d; max later growth private=%d handles=%d", warm.PrivateBytes, warm.Handles, maxPrivateGrowth, maxHandleGrowth)
			if instrumented && after.HeapAlloc > before.HeapAlloc+(16<<20) {
				t.Fatal("bounded live observer retained excessive Go heap", before.HeapAlloc, after.HeapAlloc)
			}
		})
	}
	t.Run("saturation-reports-loss", func(t *testing.T) {
		o := launchFixture(t)
		o.Capacity = 1
		o.Args = []string{"--threads", "16", "--iterations", "100000", "--delay-ms", "0"}
		var output stressObserver
		started := time.Now()
		err := Launch(context.Background(), o, &output)
		e := stressEvidence{Version: 1, ImageID: imageID, Scenario: "saturation", ExpectedCalls: 1600001, ElapsedNS: time.Since(started).Nanoseconds(), Incomplete: output.last.Metrics.Incomplete, Rows: len(output.last.Metrics.Rows), Snapshots: output.snapshots, EncodedBytes: output.bytes}
		if err != nil {
			e.Error = err.Error()
		}
		e.NativeLostEvents = output.last.NativeLostEvents
		for _, row := range output.last.Metrics.Rows {
			e.MeasuredCalls += row.Calls
		}
		writeEvidence(e)
		if !errors.Is(err, ErrLoss) || !e.Incomplete || e.NativeLostEvents == 0 || output.snapshots == 0 || e.MeasuredCalls >= e.ExpectedCalls {
			t.Fatal("saturation advertised exact/complete results or failed to report native loss", err, e)
		}
	})
}
