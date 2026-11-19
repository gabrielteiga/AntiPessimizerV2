package native

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"runtime"
	"testing"
	"time"
	"unsafe"

	"golang.org/x/sys/windows"
)

type collectorResourcePoint struct {
	Capture        int    `json:"completed_captures"`
	Heap           uint64 `json:"go_heap_bytes"`
	Handles        uint32 `json:"process_handles"`
	Goroutines     int    `json:"goroutines"`
	OSThreads      int    `json:"os_threads"`
	CreatedThreads int    `json:"go_created_threads"`
}

type collectorResourceEvidence struct {
	Version       int                      `json:"version"`
	Scenario      string                   `json:"scenario"`
	ImageID       string                   `json:"image_id"`
	GoVersion     string                   `json:"go_version"`
	Captures      int                      `json:"completed_captures"`
	ExpectedCalls uint64                   `json:"expected_calls"`
	MeasuredCalls uint64                   `json:"measured_calls"`
	ElapsedNS     int64                    `json:"elapsed_ns"`
	Completed     bool                     `json:"completed"`
	Error         string                   `json:"error,omitempty"`
	Samples       []collectorResourcePoint `json:"samples"`
}

var collectorHandleCount = windows.NewLazySystemDLL("kernel32.dll").NewProc("GetProcessHandleCount")

func collectorResources(t *testing.T, captures int) collectorResourcePoint {
	t.Helper()
	runtime.GC()
	var memory runtime.MemStats
	runtime.ReadMemStats(&memory)
	var count uint32
	if ok, _, err := collectorHandleCount.Call(uintptr(windows.CurrentProcess()), uintptr(unsafe.Pointer(&count))); ok == 0 {
		t.Fatal("cannot observe collector handle ownership", err)
	}
	runtime.KeepAlive(&count)
	if count == 0 {
		t.Fatal("invalid collector handle count")
	}
	snapshot, err := windows.CreateToolhelp32Snapshot(windows.TH32CS_SNAPTHREAD, 0)
	if err != nil {
		t.Fatal("snapshot collector threads", err)
	}
	var thread windows.ThreadEntry32
	thread.Size = uint32(unsafe.Sizeof(thread))
	osThreads := 0
	for err = windows.Thread32First(snapshot, &thread); err == nil; err = windows.Thread32Next(snapshot, &thread) {
		if thread.OwnerProcessID == windows.GetCurrentProcessId() {
			osThreads++
		}
	}
	closeErr := windows.CloseHandle(snapshot)
	if !errors.Is(err, windows.ERROR_NO_MORE_FILES) || closeErr != nil {
		t.Fatal("enumerate collector threads", err, closeErr)
	}
	createdThreads, _ := runtime.ThreadCreateProfile(nil)
	return collectorResourcePoint{Capture: captures, Heap: memory.HeapAlloc, Handles: count, Goroutines: runtime.NumGoroutine(), OSThreads: osThreads, CreatedThreads: createdThreads}
}

func TestNativeStressRepeatedOwnedCapturesReleaseResources(t *testing.T) {
	if os.Getenv("AP_NATIVE_STRESS") != "1" {
		t.Skip("collector lifetime/resource evidence runs separately in Windows CI")
	}
	o := launchFixture(t)
	o.Args = []string{"--threads", "1", "--iterations", "1", "--delay-ms", "0"}
	o.Timeout = 10 * time.Second
	image, err := os.ReadFile(o.Image)
	if err != nil {
		t.Fatal(err)
	}
	const captures = 32
	e := collectorResourceEvidence{Version: 1, Scenario: "repeated-owned-captures", ImageID: fmt.Sprintf("sha256:%x", sha256.Sum256(image)), GoVersion: runtime.Version(), ExpectedCalls: captures * 2}
	started := time.Now()
	if path := os.Getenv("AP_NATIVE_RESOURCE_REPORT"); path != "" {
		file, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
		if err != nil {
			t.Fatal(err)
		}
		defer func() {
			e.ElapsedNS = time.Since(started).Nanoseconds()
			if err := json.NewEncoder(file).Encode(e); err != nil {
				t.Error(err)
			}
			if err := file.Close(); err != nil {
				t.Error(err)
			}
		}()
	}
	e.Samples = append(e.Samples, collectorResources(t, 0))
	var warm collectorResourcePoint
	var warmTypes map[string]int
	for i := 1; i <= captures; i++ {
		var output stressObserver
		if err := Launch(context.Background(), o, &output); err != nil {
			e.Error = err.Error()
			t.Fatal(err)
		}
		r := output.last
		s := r.Metrics
		if output.snapshots == 0 || s.Incomplete || s.OpenCalls != 0 || s.AbandonedCalls != 0 || r.NativeLostEvents != 0 || len(s.Rows) != 2 {
			t.Fatal("repeated capture lost completeness", i, r)
		}
		for _, row := range s.Rows {
			if row.Calls != 1 || row.UnwoundCalls != 0 || row.ActiveFrames != 0 || row.InclusiveTicks != row.ExclusiveTicks {
				t.Fatal("repeated capture lost a worker/finalizer or leaked old metrics", i, row)
			}
			e.MeasuredCalls += row.Calls
		}
		e.Captures = i
		point := collectorResources(t, i)
		runtime.KeepAlive(&output) // retain a genuine latest-only observer at measurement
		e.Samples = append(e.Samples, point)
		if i == 4 {
			warm = point
			warmTypes, err = collectorHandleTypes()
			if err != nil {
				t.Fatal("warm handle diagnostics", err)
			}
			if warmTypes["DebugObject"] != 0 {
				t.Fatal("completed captures retained Windows debugger objects", warmTypes)
			}
		}
		if i > 4 && (point.Handles > warm.Handles+8 || point.Goroutines > warm.Goroutines+8 || point.Heap > warm.Heap+(16<<20)) {
			types, typeErr := collectorHandleTypes()
			// Keep the original bounds. Only a complete, independently
			// observed runtime M bundle can account for extra handles.
			runtimeOnly := typeErr == nil && exactRuntimeHandleBundle(warmTypes, types, point.CreatedThreads-warm.CreatedThreads, point.OSThreads-warm.OSThreads, int(point.Handles)-int(warm.Handles))
			if runtimeOnly && point.Goroutines <= warm.Goroutines+8 && point.Heap <= warm.Heap+(16<<20) {
				t.Logf("additional Go runtime threads=%d, exact native handle bundle=%d; owned handle baseline unchanged", point.CreatedThreads-warm.CreatedThreads, int(point.Handles)-int(warm.Handles))
				continue
			}
			t.Logf("handle types warm=%v current=%v error=%v", warmTypes, types, typeErr)
			t.Logf("resource samples: %+v", e.Samples)
			t.Fatal("collector resources grew after session warmup", warm, point)
		}
	}
	if e.MeasuredCalls != e.ExpectedCalls {
		t.Fatal("repeated captures changed exact totals", e)
	}
	e.Completed = true
	last := e.Samples[len(e.Samples)-1]
	lastTypes, typeErr := collectorHandleTypes()
	if typeErr != nil || lastTypes["DebugObject"] != 0 {
		t.Fatal("completed captures retained Windows debugger objects", lastTypes, typeErr)
	}
	t.Logf("handle types warm=%v final=%v error=%v", warmTypes, lastTypes, typeErr)
	t.Logf("32 owned captures, %d exact worker/finalizer calls; after warmup handles %d -> %d, goroutines %d -> %d, heap %d -> %d bytes", e.MeasuredCalls, warm.Handles, last.Handles, warm.Goroutines, last.Goroutines, warm.Heap, last.Heap)
}
