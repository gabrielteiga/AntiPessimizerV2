package native

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"testing"

	"github.com/gabrielteiga/AntiPessimizerV2/internal/instrumentation"
	"golang.org/x/sys/windows"
)

type workerCost struct {
	Ticks    uint64 `json:"ticks"`
	Checksum uint64 `json:"checksum"`
}
type workloadCost struct {
	Version      int          `json:"version"`
	Clock        string       `json:"clock"`
	Frequency    uint64       `json:"frequency_hz"`
	Iterations   uint32       `json:"iterations"`
	Threads      uint32       `json:"threads"`
	ElapsedTicks uint64       `json:"elapsed_ticks"`
	Workers      []workerCost `json:"workers"`
}
type costTrial struct {
	Version       int          `json:"version"`
	ImageID       string       `json:"image_id"`
	Trial         int          `json:"trial"`
	First         string       `json:"first"`
	Baseline      workloadCost `json:"baseline"`
	Instrumented  workloadCost `json:"instrumented"`
	MeasuredCalls uint64       `json:"measured_calls"`
}

func runCostBaseline(t *testing.T, options LaunchOptions) {
	t.Helper()
	p, job, err := createOwned(options.Image, options.Args)
	if err != nil {
		t.Fatal(err)
	}
	defer windows.CloseHandle(p.Process)
	defer windows.CloseHandle(job)
	defer windows.TerminateJobObject(job, 1)
	if state, err := windows.WaitForSingleObject(p.Process, 5000); err != nil || state != windows.WAIT_OBJECT_0 {
		t.Fatal("baseline did not exit", state, err)
	}
	var code uint32
	if err := windows.GetExitCodeProcess(p.Process, &code); err != nil || code != 0 {
		t.Fatal("baseline results changed", code, err)
	}
}

func readCost(t *testing.T, path string, workers, iterations uint32) workloadCost {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var result workloadCost
	if err := json.Unmarshal(data, &result); err != nil {
		t.Fatal(err)
	}
	if result.Version != 1 || result.Clock != "qpc" || result.Frequency == 0 || result.ElapsedTicks == 0 || result.Threads != workers || result.Iterations != iterations || len(result.Workers) != int(workers) {
		t.Fatal("invalid workload timing report", result)
	}
	var expected uint64
	for i := uint32(0); i < iterations; i++ {
		if i%2 != 0 {
			expected += 777
		} else {
			expected += uint64(125 + i)
		}
	}
	for _, worker := range result.Workers {
		if worker.Ticks == 0 || worker.Ticks > result.ElapsedTicks || worker.Checksum != expected {
			t.Fatal("timing interval or function checksum changed", worker, expected)
		}
	}
	return result
}

func workerNSPerCall(cost workloadCost) float64 {
	var total float64
	for _, worker := range cost.Workers {
		total += float64(worker.Ticks)
	}
	return total * 1e9 / float64(cost.Frequency) / float64(uint64(cost.Threads)*uint64(cost.Iterations))
}

// This measures the controlled C loop, including checks and capture/debugger
// pauses, but excluding process startup and hook installation. It has no speed
// threshold: counts/completeness/checksums are invariants, host timings are data.
func TestNativeCostBaselineAndInstrumentedWorkload(t *testing.T) {
	if os.Getenv("AP_NATIVE_COST") != "1" {
		t.Skip("controlled cost measurement enabled separately in Windows CI")
	}
	o := launchFixture(t)
	image, err := os.ReadFile(o.Image)
	if err != nil {
		t.Fatal(err)
	}
	o.ImageID = fmt.Sprintf("sha256:%x", sha256.Sum256(image))
	var artifact *os.File
	if path := os.Getenv("AP_NATIVE_COST_REPORT"); path != "" {
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
	// At most 602 events per worker fit in one 1024-event ring even if the
	// drain thread is descheduled for the complete measured loop.
	const iterations uint32 = 300
	for _, workers := range []uint32{1, 3} {
		var ratios, deltas []float64
		for trial := 0; trial < 3; trial++ {
			result := costTrial{Version: 1, ImageID: o.ImageID, Trial: trial + 1, First: "baseline"}
			run := func(instrumented bool) {
				path := filepath.Join(t.TempDir(), "worker-cost.json")
				options := o
				options.Args = []string{"--threads", strconv.Itoa(int(workers)), "--iterations", strconv.Itoa(int(iterations)), "--delay-ms", "0", "--report", path}
				if instrumented {
					var output bytes.Buffer
					if err := Launch(context.Background(), options, &output); err != nil {
						t.Fatal(err, output.String())
					}
					records, err := instrumentation.ReadMetrics(&output)
					if err != nil {
						t.Fatal(err)
					}
					last := records[len(records)-1].Metrics
					if last.Incomplete || last.OpenCalls != 0 || last.AbandonedCalls != 0 || len(last.Rows) != int(workers)+1 {
						t.Fatal("cost measurement lost native events", last)
					}
					var workerRows, finalizers int
					for _, row := range last.Rows {
						result.MeasuredCalls += row.Calls
						if row.Calls == uint64(iterations) {
							workerRows++
						} else if row.Calls == 1 {
							finalizers++
						} else {
							t.Fatal("cost capture changed call counts", row)
						}
					}
					if workerRows != int(workers) || finalizers != 1 {
						t.Fatal("cost capture lost worker/finalizer identities", last)
					}
					result.Instrumented = readCost(t, path, workers, iterations)
				} else {
					runCostBaseline(t, options)
					result.Baseline = readCost(t, path, workers, iterations)
				}
				if instrumented && result.MeasuredCalls != uint64(workers)*uint64(iterations)+1 {
					t.Fatal("unexpected total native calls", result.MeasuredCalls)
				}
			}
			if trial%2 == 0 {
				run(false)
				run(true)
			} else {
				result.First = "instrumented"
				run(true)
				run(false)
			}
			if result.Baseline.Frequency != result.Instrumented.Frequency {
				t.Fatal("cost pair uses different QPC frequencies")
			}
			baseline, measured := workerNSPerCall(result.Baseline), workerNSPerCall(result.Instrumented)
			if baseline <= 0 || measured <= 0 || math.IsInf(measured, 0) || math.IsNaN(measured) {
				t.Fatal("invalid measured cost", baseline, measured)
			}
			ratios = append(ratios, measured/baseline)
			deltas = append(deltas, measured-baseline)
			t.Logf("workers=%d pair=%d first=%s baseline=%.1f ns/call instrumented=%.1f ns/call delta=%+.1f ratio=%.3f calls=%d", workers, trial+1, result.First, baseline, measured, measured-baseline, measured/baseline, result.MeasuredCalls)
			if artifact != nil {
				if err := json.NewEncoder(artifact).Encode(result); err != nil {
					t.Fatal(err)
				}
			}
		}
		sort.Float64s(ratios)
		sort.Float64s(deltas)
		t.Logf("workers=%d paired median cost ratio=%.3f delta=%+.1f ns/call; controlled C loop, not Delphi", workers, ratios[1], deltas[1])
	}
}
