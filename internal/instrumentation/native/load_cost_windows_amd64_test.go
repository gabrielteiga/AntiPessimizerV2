package native

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"testing"

	"github.com/gabrielteiga/AntiPessimizerV2/internal/instrumentation"
)

type loadCostTrial struct {
	Version      int          `json:"version"`
	ImageID      string       `json:"image_id"`
	Trial        int          `json:"trial"`
	First        string       `json:"first"`
	RMW          workloadCost `json:"rmw_load_instrumented"`
	Atomic       workloadCost `json:"atomic_load_instrumented"`
	CallsPerMode uint64       `json:"measured_calls_per_mode"`
}

func TestNativeCostAtomicLoadsComparedWithRMWLoads(t *testing.T) {
	if os.Getenv("AP_NATIVE_COST") != "1" {
		t.Skip("paired native load comparison runs separately in Windows CI")
	}
	o := launchFixture(t)
	image, err := os.ReadFile(o.Image)
	if err != nil {
		t.Fatal(err)
	}
	imageID := fmt.Sprintf("sha256:%x", sha256.Sum256(image))
	var artifact *os.File
	if path := os.Getenv("AP_NATIVE_LOAD_COST_REPORT"); path != "" {
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
	const iterations = uint32(300)
	for _, workers := range []uint32{1, 3} {
		var ratios, deltas []float64
		for trial := 0; trial < 3; trial++ {
			e := loadCostTrial{Version: 1, ImageID: imageID, Trial: trial + 1, First: "rmw", CallsPerMode: uint64(workers*iterations) + 1}
			run := func(atomic bool) {
				options := o
				if !atomic {
					options.DLL = filepath.Join(filepath.Dir(o.DLL), "AntiPessimizerNativeRMW.dll")
				}
				path := filepath.Join(t.TempDir(), "load-cost.json")
				options.Args = []string{"--threads", strconv.Itoa(int(workers)), "--iterations", "300", "--delay-ms", "0", "--report", path}
				var output bytes.Buffer
				if err := Launch(context.Background(), options, &output); err != nil {
					t.Fatal(err, output.String())
				}
				records, err := instrumentation.ReadMetrics(&output)
				if err != nil {
					t.Fatal(err)
				}
				r := records[len(records)-1]
				s := r.Metrics
				if s.Incomplete || s.OpenCalls != 0 || s.AbandonedCalls != 0 || r.NativeLostEvents != 0 || len(s.Rows) != int(workers)+1 {
					t.Fatal("paired loads lost capture exactness", s)
				}
				var calls uint64
				var workerRows, finalizers int
				for _, row := range s.Rows {
					if row.Calls == uint64(iterations) {
						workerRows++
					} else if row.Calls == 1 {
						finalizers++
					} else {
						t.Fatal("paired loads changed call count", row)
					}
					calls += row.Calls
				}
				if calls != e.CallsPerMode || workerRows != int(workers) || finalizers != 1 {
					t.Fatal("paired loads changed thread/finalizer identity", calls, workerRows, finalizers)
				}
				cost := readCost(t, path, workers, iterations)
				if atomic {
					e.Atomic = cost
				} else {
					e.RMW = cost
				}
			}
			if trial%2 == 0 {
				run(false)
				run(true)
			} else {
				e.First = "atomic"
				run(true)
				run(false)
			}
			if e.RMW.Frequency != e.Atomic.Frequency {
				t.Fatal("paired native loads changed QPC clock")
			}
			rmw, atomic := workerNSPerCall(e.RMW), workerNSPerCall(e.Atomic)
			ratios = append(ratios, atomic/rmw)
			deltas = append(deltas, atomic-rmw)
			t.Logf("workers=%d pair=%d first=%s RMW=%.1f atomic=%.1f ns/call delta=%+.1f ratio=%.3f calls/mode=%d", workers, trial+1, e.First, rmw, atomic, atomic-rmw, atomic/rmw, e.CallsPerMode)
			if artifact != nil {
				if err := json.NewEncoder(artifact).Encode(e); err != nil {
					t.Fatal(err)
				}
			}
		}
		sort.Float64s(ratios)
		sort.Float64s(deltas)
		t.Logf("workers=%d paired native-load median atomic/RMW=%.3f delta=%+.1f ns/call; same C binary/runner, not Delphi", workers, ratios[1], deltas[1])
	}
}
