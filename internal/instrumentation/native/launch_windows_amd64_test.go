package native

import (
	"bytes"
	"context"
	"debug/pe"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
	"unsafe"

	"github.com/gabrielteiga/AntiPessimizerV2/internal/instrumentation"
)

func launchFixture(t *testing.T) LaunchOptions {
	t.Helper()
	directory := filepath.Dir(labPath(t))
	path := filepath.Join(directory, "APLaunchTarget.exe")
	image, err := pe.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer image.Close()
	var begin, end uint32
	for _, symbol := range image.Symbols {
		if symbol.SectionNumber < 1 || int(symbol.SectionNumber) > len(image.Sections) {
			continue
		}
		rva := image.Sections[symbol.SectionNumber-1].VirtualAddress + symbol.Value
		if symbol.Name == "APHookSum" {
			begin = rva
		}
		if symbol.Name == "APHookSumEnd" {
			end = rva
		}
	}
	if begin == 0 || end <= begin {
		t.Fatal("controlled native symbols missing")
	}
	return LaunchOptions{Image: path, DLL: filepath.Join(directory, "AntiPessimizerNative.dll"), Args: []string{"--threads", "3", "--iterations", "10"}, Procedures: []SelectedProcedure{{ID: 1, RVA: begin, Size: end - begin, Name: "APHookSum"}}, Capacity: 1024, Timeout: 20 * time.Second}
}
func TestNativeProducerDebugLaunchWithoutApplicationCooperation(t *testing.T) {
	o := launchFixture(t)
	data, err := os.ReadFile(o.Image)
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(data, []byte("AntiPessimizerNative.dll")) || bytes.Contains(data, []byte("APNativeInitHooks")) {
		t.Fatal("target is linked to the profiler")
	}
	var output bytes.Buffer
	if err := Launch(context.Background(), o, &output); err != nil {
		t.Fatal(err, output.String())
	}
	records, err := instrumentation.ReadMetrics(&output)
	if err != nil {
		t.Fatal(err)
	}
	last := records[len(records)-1]
	if last.Metrics.Incomplete || last.Metrics.OpenCalls != 0 || last.Metrics.AbandonedCalls != 0 || len(last.Metrics.Rows) != 3 {
		t.Fatal("unmodified application capture incomplete", last)
	}
	for _, row := range last.Metrics.Rows {
		if row.Calls != 10 || row.InclusiveTicks == 0 || row.ExclusiveTicks != row.InclusiveTicks {
			t.Fatal("wrong unmodified application count/time", row)
		}
	}
	t.Log("owned unmodified target stopped at entry, native DLL loaded, hooks installed, 30 calls collected from 3 threads, natural exit drained")
}
func TestNativeProducerDebugEventLayout(t *testing.T) {
	if unsafe.Sizeof(debugEvent{}) != 176 || unsafe.Offsetof(debugEvent{}.data) != 16 {
		t.Fatal("Windows x64 DEBUG_EVENT layout changed")
	}
}
func TestNativeProducerDebugLaunchMissingDLLPreservesStartupFailure(t *testing.T) {
	o := launchFixture(t)
	o.DLL = filepath.Join(t.TempDir(), "missing.dll")
	var output bytes.Buffer
	if err := Launch(context.Background(), o, &output); err == nil || strings.Contains(output.String(), `"incomplete":false`) {
		t.Fatal("failed startup advertised successful capture", err, output.String())
	}
}
