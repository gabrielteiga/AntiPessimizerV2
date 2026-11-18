package x64

import (
	"bufio"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"
)

func TestReferenceDelphiExceptionHandlerPreservesCatchAndReraise(t *testing.T) {
	path := os.Getenv("AP_REFERENCE_DELPHI_DLL")
	if path == "" {
		t.Skip("pinned original Delphi release is downloaded in a separate Windows CI step")
	}
	image, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if got := fmt.Sprintf("%x", sha256.Sum256(image)); got != referenceDLLSHA {
		t.Fatal("original hash changed; refuse execution", got)
	}
	lab := os.Getenv("AP_NATIVE_LAB")
	if lab == "" {
		t.Fatal("controlled native reference child is required")
	}
	directory := filepath.Dir(lab)
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	child := exec.CommandContext(ctx, filepath.Join(directory, "APReferenceException.exe"), path, filepath.Join(directory, "APReferenceAdapter.dll"))
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
	var offer struct{ Loaded, Allocation, Enter, Exit, Unwind uint64 }
	if err := json.Unmarshal(line, &offer); err != nil {
		t.Fatal(err, string(line))
	}
	plan, err := BuildPEFunction(image, 0xa930, 155, offer.Loaded, offer.Allocation, offer.Enter, offer.Exit, offer.Unwind, 17)
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
		Caught            uint32 `json:"caught"`
		ExceptionCode     uint32 `json:"exception_code"`
	}
	if err := json.Unmarshal(result, &report); err != nil {
		t.Fatal(err, string(result))
	}
	if report.BaselinePairs != 4 || report.InstrumentedPairs != 100 || report.Entries != 200 || report.Returns != 100 || report.Caught != 104 {
		t.Fatal("real Delphi exception counts", report)
	}
	if output := os.Getenv("AP_REFERENCE_DELPHI_EXCEPTION_REPORT"); output != "" {
		evidence := struct {
			Version            int    `json:"version"`
			ImageSHA           string `json:"image_sha256"`
			HandlerFingerprint string `json:"handler_fingerprint"`
			ProcedureRVA       uint32 `json:"procedure_rva"`
			Result             any    `json:"result"`
		}{1, referenceDLLSHA, delphiScopeFingerprint, 0xa930, report}
		data, err := json.MarshalIndent(evidence, "", "  ")
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(output, append(data, '\n'), 0600); err != nil {
			t.Fatal(err)
		}
	}
	t.Logf("original Delphi EH/UH: %d baseline pairs, %d instrumented normal/fault pairs, original handler capture/resume/reraise, %d entry probes and %d returns", report.BaselinePairs, report.InstrumentedPairs, report.Entries, report.Returns)
}
