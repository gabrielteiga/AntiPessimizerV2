package native

import (
	"bufio"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"github.com/gabrielteiga/AntiPessimizerV2/internal/instrumentation"
	"github.com/gabrielteiga/AntiPessimizerV2/internal/instrumentation/x64"
	"github.com/gabrielteiga/AntiPessimizerV2/internal/profiling"
	"golang.org/x/sys/windows"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"
)

const referenceExceptionDLLSHA = "1c1bf9288a3bfd05303f2b9a86d29693c0a449e716f575a25a59059f1c0d02b5"
const referenceExceptionFingerprint = "8ebb487813ebd0e56320833793e013dc48ea3e030dfd64f2157743835e165229"

func TestReferenceDelphiExceptionHandlerPreservesCatchAndReraise(t *testing.T) {
	path := os.Getenv("AP_REFERENCE_DELPHI_DLL")
	if path == "" {
		t.Skip("pinned original Delphi release is downloaded in a separate Windows CI step")
	}
	image, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if got := fmt.Sprintf("%x", sha256.Sum256(image)); got != referenceExceptionDLLSHA {
		t.Fatal("original hash changed; refuse execution", got)
	}
	lab := os.Getenv("AP_NATIVE_LAB")
	if lab == "" {
		t.Fatal("controlled native reference child is required")
	}
	directory := filepath.Dir(lab)
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	eventPath := filepath.Join(t.TempDir(), "original-delphi-events.bin")
	child := exec.CommandContext(ctx, filepath.Join(directory, "APReferenceException.exe"), path, filepath.Join(directory, "APReferenceAdapter.dll"), eventPath)
	stdout, err := child.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	stdin, err := child.StdinPipe()
	if err != nil {
		t.Fatal(err)
	}
	var diagnostics bytes.Buffer
	child.Stderr = &diagnostics
	if err := child.Start(); err != nil {
		t.Fatal(err)
	}
	// Own and reap the child even when planning or decoding an offer fails.
	waited := false
	defer func() {
		if !waited {
			cancel()
			_ = child.Wait()
		}
		t.Log(diagnostics.String())
	}()
	reader := bufio.NewReader(io.LimitReader(stdout, 16<<10))
	line, err := reader.ReadBytes('\n')
	if err != nil {
		t.Fatal("original baseline did not complete", err)
	}
	var offer struct {
		PID                                              uint32
		Created, Loaded, Allocation, Enter, Exit, Unwind uint64
	}
	if err := json.Unmarshal(line, &offer); err != nil {
		t.Fatal(err, string(line))
	}
	plan, err := x64.BuildPEFunction(image, 0xa930, 155, offer.Loaded, offer.Allocation, offer.Enter, offer.Exit, offer.Unwind, 17)
	if err != nil {
		t.Fatal(err)
	}
	if !plan.RelocatedHandler || plan.HandlerScopeStyle != 2 || plan.OriginalHandlerRVA != 0xd7a0 {
		t.Fatal("reviewed Delphi scopes were not recognized")
	}
	if err := binary.Write(stdin, binary.LittleEndian, uint32(len(plan.Image))); err != nil {
		t.Fatal(err)
	}
	if err := binary.Write(stdin, binary.LittleEndian, plan.Functions); err != nil {
		t.Fatal(err)
	}
	if _, err := stdin.Write(plan.Image); err != nil {
		t.Fatal(err)
	}
	if err := stdin.Close(); err != nil {
		t.Fatal(err)
	}
	result, readErr := reader.ReadBytes('\n')
	waitErr := child.Wait()
	waited = true
	if readErr != nil || waitErr != nil {
		t.Fatal("real Delphi exception flow failed", readErr, waitErr, string(result))
	}
	var report struct {
		BaselinePairs     uint32 `json:"baseline_pairs"`
		InstrumentedPairs uint32 `json:"instrumented_pairs"`
		Entries           uint32 `json:"entries"`
		Returns           uint32 `json:"returns"`
		Unwinds           uint32 `json:"unwinds"`
		Caught            uint32 `json:"caught"`
		ExceptionCode     uint32 `json:"exception_code"`
	}
	if err := json.Unmarshal(result, &report); err != nil {
		t.Fatal(err, string(result))
	}
	if report.BaselinePairs != 4 || report.InstrumentedPairs != 100 || report.Entries != 200 || report.Returns != 100 || report.Caught != 104 || report.Unwinds != 100 || report.ExceptionCode != 0x0eedfade {
		t.Fatal("real Delphi exception counts", report)
	}
	events, err := os.ReadFile(eventPath)
	if err != nil {
		t.Fatal(err)
	}
	readerNative, err := NewReader(bytes.NewReader(events), uint32(child.Process.Pid))
	if err != nil {
		t.Fatal(err)
	}
	if offer.PID != uint32(child.Process.Pid) || offer.Created == 0 {
		t.Fatal("child identity", offer)
	}
	creation := windows.Filetime{LowDateTime: uint32(offer.Created), HighDateTime: uint32(offer.Created >> 32)}
	target := profiling.Target{PID: offer.PID, Start: time.Unix(0, creation.Nanoseconds()).UTC(), Image: "APReferenceException.exe"}
	var metrics bytes.Buffer
	if err := consumeCapture(ctx, readerNative, target, "original-delphi-exceptions", []instrumentation.Procedure{{ID: 17, Location: profiling.Location{ModuleID: "sha256:" + referenceExceptionDLLSHA, Module: "AntiPessimizerDLL.dll", RVA: "0x000000000000a930", Procedure: "reviewed RVA 0xa930 (no TDS)", Status: "resolved"}}}, &metrics, func() error { return nil }, func(context.Context) error { return nil }); err != nil {
		t.Fatal("real Delphi native accounting", err)
	}
	records, err := instrumentation.ReadMetrics(bytes.NewReader(metrics.Bytes()))
	if err != nil {
		t.Fatal(err)
	}
	snapshot := records[len(records)-1].Metrics
	if snapshot.Incomplete || snapshot.OpenCalls != 0 || snapshot.AbandonedCalls != 0 || len(snapshot.Rows) != 1 {
		t.Fatal("original Delphi native accounting incomplete", snapshot)
	}
	row := snapshot.Rows[0]
	if row.Calls != 200 || row.UnwoundCalls != 100 || row.InclusiveTicks != row.ExclusiveTicks || row.InclusiveTicks == 0 || row.AverageInclusiveNS != float64(row.InclusiveTicks)*1e9/float64(snapshot.Frequency)/200 {
		t.Fatal("original Delphi call counts/times/means", row)
	}
	if output := os.Getenv("AP_REFERENCE_DELPHI_EXCEPTION_REPORT"); output != "" {
		evidence := struct {
			Version            int                      `json:"version"`
			ImageSHA           string                   `json:"image_sha256"`
			HandlerFingerprint string                   `json:"handler_fingerprint"`
			ProcedureRVA       uint32                   `json:"procedure_rva"`
			Result             any                      `json:"result"`
			Metrics            instrumentation.Snapshot `json:"metrics"`
		}{1, referenceExceptionDLLSHA, referenceExceptionFingerprint, 0xa930, report, snapshot}
		data, err := json.MarshalIndent(evidence, "", "  ")
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(output, append(data, '\n'), 0600); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(filepath.Dir(output), "reference-delphi-metrics.ndjson"), metrics.Bytes(), 0600); err != nil {
			t.Fatal(err)
		}
	}
	t.Logf("real Delphi native Go metrics: calls=%d unwind=%d inclusive=%d exclusive=%d QPC ticks; mean=%.3f ns frequency=%d Hz", row.Calls, row.UnwoundCalls, row.InclusiveTicks, row.ExclusiveTicks, row.AverageInclusiveNS, snapshot.Frequency)
	t.Logf("original Delphi EH/UH: %d baseline pairs, %d instrumented normal/fault pairs, original handler capture/resume/reraise, %d entry probes and %d returns", report.BaselinePairs, report.InstrumentedPairs, report.Entries, report.Returns)
}
