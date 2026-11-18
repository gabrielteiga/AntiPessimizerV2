package main

import (
	"bytes"
	"context"
	"encoding/json"
	"github.com/gabrielteiga/AntiPessimizerV2/internal/instrumentation/native"
	"github.com/gabrielteiga/AntiPessimizerV2/internal/testfixture"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestCatalogSelectionDoesNotLaunch(t *testing.T) {
	path := filepath.Join(t.TempDir(), "owned.exe")
	if err := os.WriteFile(path, testfixture.Image(true), 0600); err != nil {
		t.Fatal(err)
	}
	var output, diagnostics bytes.Buffer
	if err := run(context.Background(), []string{"--image", path, "--list", "--procedure", "lab.hot", "--dll", "missing.dll"}, &output, &diagnostics); err != nil {
		t.Fatal(err)
	}
	var catalog struct {
		ImageID    string `json:"image_id"`
		Procedures []struct {
			Name string
			RVA  uint32
		}
	}
	if err := json.Unmarshal(output.Bytes(), &catalog); err != nil || len(catalog.Procedures) != 1 || catalog.Procedures[0].Name != "Lab.Hot" || catalog.Procedures[0].RVA != 0x5020 || !strings.HasPrefix(catalog.ImageID, "sha256:") {
		t.Fatal(output.String(), err)
	}
	for _, args := range [][]string{{"--image", path}, {"--image", path, "--list", "--procedure", "absent"}, {"--image", path, "--list", "--unit", "absent"}} {
		output.Reset()
		if err := run(context.Background(), args, &output, &diagnostics); err == nil || output.Len() != 0 {
			t.Fatal("invalid selection launched or emitted data", args, err)
		}
	}
}

func TestCatalogPreflightReportsSupportWithoutNativeDLL(t *testing.T) {
	for _, supported := range []bool{true, false} {
		path := filepath.Join(t.TempDir(), "app.exe")
		data := testfixture.PlannableImage()
		if !supported {
			data[0x220], data[0x221] = 0x0f, 0x0b // UD2, outside accepted instructions
		}
		if err := os.WriteFile(path, data, 0600); err != nil {
			t.Fatal(err)
		}
		var out, diagnostics bytes.Buffer
		if err := run(context.Background(), []string{"--image", path, "--list", "--check", "--dll", "missing.dll"}, &out, &diagnostics); err != nil {
			t.Fatal(err)
		}
		var result struct{ Checks []native.ProcedureCheck }
		if err := json.Unmarshal(out.Bytes(), &result); err != nil || len(result.Checks) != 1 || result.Checks[0].PlanSupported != supported {
			t.Fatal("static support missing", out.String(), err)
		}
		if !supported && result.Checks[0].Reason == "" {
			t.Fatal("unsupported procedure has no explanation")
		}
		out.Reset()
		if err := run(context.Background(), []string{"--image", path, "--check"}, &out, &diagnostics); err == nil || out.Len() != 0 {
			t.Fatal("preflight without catalog mode accepted", err)
		}
	}
}

func TestAliasCatalogFilterListsOnePhysicalProcedure(t *testing.T) {
	directory := t.TempDir()
	path := filepath.Join(directory, "app.exe")
	if err := os.WriteFile(path, testfixture.Image(false), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(directory, "app.tds"), testfixture.TDSWithAlias("Lab.Alias"), 0600); err != nil {
		t.Fatal(err)
	}
	var out, diagnostics bytes.Buffer
	if err := run(context.Background(), []string{"--image", path, "--list", "--procedure", "lab.alias", "--procedure", "Lab.Hot"}, &out, &diagnostics); err != nil {
		t.Fatal(err)
	}
	var catalog native.Catalog
	if err := json.Unmarshal(out.Bytes(), &catalog); err != nil || len(catalog.Procedures) != 1 || len(catalog.Aliases) != 1 || catalog.Procedures[0].ID != catalog.Aliases[0].ID || catalog.Procedures[0].Name != "Lab.Hot" {
		t.Fatal("alias created another instrumentation selection", out.String(), err)
	}
}

func TestCaptureOutputNeverOverwritesFile(t *testing.T) {
	directory := t.TempDir()
	image := filepath.Join(directory, "app.exe")
	output := filepath.Join(directory, "metrics.ndjson")
	if err := os.WriteFile(image, testfixture.Image(true), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(output, []byte("preserve"), 0600); err != nil {
		t.Fatal(err)
	}
	var stdout, diagnostics bytes.Buffer
	err := run(context.Background(), []string{"--image", image, "--procedure", "Lab.Hot", "--output", output}, &stdout, &diagnostics)
	if !os.IsExist(err) {
		t.Fatal("existing capture output was not rejected before launch", err)
	}
	b, err := os.ReadFile(output)
	if err != nil || string(b) != "preserve" || stdout.Len() != 0 {
		t.Fatal("existing output changed", string(b), err)
	}
	if err := run(context.Background(), []string{"--image", image, "--list", "--output", output}, &stdout, &diagnostics); err == nil {
		t.Fatal("catalog/output mode accepted")
	}
}
