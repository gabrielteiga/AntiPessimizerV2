package native

import (
	"bytes"
	"context"
	"debug/pe"
	"encoding/binary"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
	"unsafe"

	"github.com/gabrielteiga/AntiPessimizerV2/internal/instrumentation"
	"github.com/gabrielteiga/AntiPessimizerV2/internal/testfixture"
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
	marker := filepath.Join(t.TempDir(), "finalizer.txt")
	o.Args = append(o.Args, "--marker", marker)
	var output bytes.Buffer
	if err := Launch(context.Background(), o, &output); err != nil {
		t.Fatal(err, output.String())
	}
	records, err := instrumentation.ReadMetrics(&output)
	if err != nil {
		t.Fatal(err)
	}
	last := records[len(records)-1]
	if last.Metrics.Incomplete || last.Metrics.OpenCalls != 0 || last.Metrics.AbandonedCalls != 0 || len(last.Metrics.Rows) != 4 {
		t.Fatal("unmodified application capture incomplete", last)
	}
	finalizers, workers := 0, 0
	for _, row := range last.Metrics.Rows {
		if row.Calls == 1 {
			finalizers++
		} else if row.Calls == 10 {
			workers++
		}
		if (row.Calls != 10 && row.Calls != 1) || row.InclusiveTicks == 0 || row.ExclusiveTicks != row.InclusiveTicks {
			t.Fatal("wrong unmodified application count/time", row)
		}
	}
	if workers != 3 || finalizers != 1 {
		t.Fatal("finalizer/worker calls not preserved", workers, finalizers)
	}
	if got, err := os.ReadFile(marker); err != nil || string(got) != "finalized" {
		t.Fatal("application finalizers skipped", string(got), err)
	}
	t.Log("unmodified target: 30 worker calls + 1 atexit call; application finalizer ran and natural exit drained")
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

type cancelAfterSnapshot struct {
	bytes.Buffer
	cancel context.CancelFunc
}

func (w *cancelAfterSnapshot) Write(b []byte) (int, error) {
	n, err := w.Buffer.Write(b)
	w.cancel()
	return n, err
}
func TestNativeProducerDebugLaunchCancellationIsIncomplete(t *testing.T) {
	o := launchFixture(t)
	o.Args = []string{"--threads", "2", "--iterations", "10000"}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	output := cancelAfterSnapshot{cancel: cancel}
	err := Launch(ctx, o, &output)
	if !errors.Is(err, context.Canceled) {
		t.Fatal("owned launch cancellation missing", err)
	}
	records, err := instrumentation.ReadMetrics(&output.Buffer)
	if err != nil || len(records) == 0 {
		t.Fatal(err, output.String())
	}
	if !records[len(records)-1].Metrics.Incomplete {
		t.Fatal("cancelled launch advertised complete")
	}
}

func TestNativeProducerDebugLaunchUnicodePaths(t *testing.T) {
	o := launchFixture(t)
	directory := filepath.Join(t.TempDir(), "caminho ação 空格")
	if err := os.Mkdir(directory, 0700); err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{o.Image, o.DLL} {
		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(directory, filepath.Base(path)), data, 0600); err != nil {
			t.Fatal(err)
		}
	}
	o.Image = filepath.Join(directory, filepath.Base(o.Image))
	o.DLL = filepath.Join(directory, filepath.Base(o.DLL))
	marker := filepath.Join(directory, "finalização.txt")
	o.Args = append(o.Args, "--marker", marker)
	var output bytes.Buffer
	if err := Launch(context.Background(), o, &output); err != nil {
		t.Fatal(err)
	}
	records, err := instrumentation.ReadMetrics(&output)
	if err != nil || len(records[len(records)-1].Metrics.Rows) != 4 || records[len(records)-1].Metrics.Incomplete {
		t.Fatal("unicode capture failed", err)
	}
	if _, err := os.Stat(marker); err != nil {
		t.Fatal("unicode application argument lost", err)
	}
}
func TestNativeProducerDebugLaunchRejectsChangedBuildBeforeStartup(t *testing.T) {
	o := launchFixture(t)
	o.ImageID = "sha256:changed"
	o.DLL = "missing.dll"
	var output bytes.Buffer
	if err := Launch(context.Background(), o, &output); err == nil || !strings.Contains(err.Error(), "changed after symbol selection") || output.Len() != 0 {
		t.Fatal("build identity check did not precede startup", err)
	}
}

func TestNativeProducerTD32AliasesSelectPhysicalFunctionOnce(t *testing.T) {
	o := launchFixture(t)
	f, err := pe.Open(o.Image)
	if err != nil {
		t.Fatal(err)
	}
	code := f.OptionalHeader.(*pe.OptionalHeader64).BaseOfCode
	f.Close()
	data, err := os.ReadFile(o.Image)
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "alias-target.exe")
	if err := os.WriteFile(path, data, 0600); err != nil {
		t.Fatal(err)
	}
	// Synthetic TDS aliases reference the actual COFF extent of the owned C
	// target. This exercises parser -> catalog -> selection -> native capture.
	payload := testfixture.TDSWithAlias("Lab.Alias")
	for _, record := range []int{52, 96} {
		binary.LittleEndian.PutUint32(payload[record+16:], o.Procedures[0].Size)
		binary.LittleEndian.PutUint32(payload[record+28:], o.Procedures[0].RVA-code)
	}
	if err := os.WriteFile(filepath.Join(filepath.Dir(path), "alias-target.tds"), payload, 0600); err != nil {
		t.Fatal(err)
	}
	catalog, err := ReadCatalog(path)
	if err != nil {
		t.Fatal(err)
	}
	o.Image, o.ImageID = path, catalog.ImageID
	o.Procedures = catalog.Match(nil, []string{"Lab.Alias", "Lab.Hot"})
	if len(o.Procedures) != 1 || len(catalog.Aliases) != 1 {
		t.Fatal("alias produced multiple physical selections", o.Procedures, catalog.Aliases)
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
	if last.Metrics.Incomplete || len(last.Selection) != 1 || len(last.Metrics.Rows) != 4 {
		t.Fatal("alias duplicated capture metadata/rows", last)
	}
	var calls uint64
	for _, row := range last.Metrics.Rows {
		if row.Procedure != "Lab.Hot" {
			t.Fatal("alias changed canonical result identity", row)
		}
		calls += row.Calls
	}
	if calls != 31 {
		t.Fatal("two names counted the same calls twice", calls)
	}
}

func TestNativeProducerPreflightRejectsUnsupportedBeforeStartup(t *testing.T) {
	o := launchFixture(t)
	data, err := os.ReadFile(o.Image)
	if err != nil {
		t.Fatal(err)
	}
	f, err := pe.Open(o.Image)
	if err != nil {
		t.Fatal(err)
	}
	patched := false
	for _, section := range f.Sections {
		rva := o.Procedures[0].RVA
		if rva >= section.VirtualAddress && uint64(rva)+2 <= uint64(section.VirtualAddress)+uint64(section.Size) {
			at := uint64(section.Offset) + uint64(rva-section.VirtualAddress)
			data[at], data[at+1] = 0x0f, 0x0b // UD2 is rejected by the planner.
			patched = true
		}
	}
	f.Close()
	if !patched {
		t.Fatal("fixture procedure is not mapped")
	}
	o.Image = filepath.Join(t.TempDir(), "unsupported.exe")
	if err := os.WriteFile(o.Image, data, 0600); err != nil {
		t.Fatal(err)
	}
	o.DLL = "missing.dll"
	var output bytes.Buffer
	if err := Launch(context.Background(), o, &output); err == nil || !strings.Contains(err.Error(), "preflight rejected") || output.Len() != 0 {
		t.Fatal("unsupported code reached DLL startup", err, output.String())
	}
}
