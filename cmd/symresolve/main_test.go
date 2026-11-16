package main

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/gabrielteiga/AntiPessimizerV2/internal/profiling"
	"github.com/gabrielteiga/AntiPessimizerV2/internal/testfixture"
)

func TestResolveCLI(t *testing.T) {
	path := filepath.Join(t.TempDir(), "lab.exe")
	if err := os.WriteFile(path, testfixture.Image(true), 0600); err != nil {
		t.Fatal(err)
	}
	var out, diag bytes.Buffer
	if err := run([]string{"--image", path, "--base", "0x7ff000000000", "0x7ff00000502f", "0x7ff000005030", "0x7ff000007000"}, &out, &diag); err != nil {
		t.Fatal(err)
	}
	var report struct {
		ProcedureCount int `json:"procedure_count"`
		Resolutions    []struct {
			InImage  bool                `json:"in_image"`
			Location *profiling.Location `json:"location"`
		} `json:"resolutions"`
	}
	if err := json.Unmarshal(out.Bytes(), &report); err != nil {
		t.Fatal(err)
	}
	if report.ProcedureCount != 1 || len(report.Resolutions) != 3 {
		t.Fatal(out.String())
	}
	a, b, c := report.Resolutions[0], report.Resolutions[1], report.Resolutions[2]
	if !a.InImage || a.Location == nil || a.Location.Procedure != "Lab.Hot" || b.Location == nil || b.Location.Status != "unknown" || c.InImage || c.Location != nil {
		t.Fatal(out.String())
	}
	if err := a.Location.Validate(); err != nil {
		t.Fatal(err)
	}
}

func TestCLIRejectsInvalidInputs(t *testing.T) {
	path := filepath.Join(t.TempDir(), "lab.exe")
	if err := os.WriteFile(path, testfixture.Image(false), 0600); err != nil {
		t.Fatal(err)
	}
	for _, args := range [][]string{
		nil, {"--image", path, "1"}, {"--image", path, "--base", "invalid", "1"},
		{"--image", path, "--base", "0", "-1"}, {"--image", path, "--base", "0xffffffffffffffff", "1"},
	} {
		var out, diag bytes.Buffer
		if err := run(args, &out, &diag); err == nil || out.Len() != 0 {
			t.Fatalf("%v: %v", args, err)
		}
	}
}
