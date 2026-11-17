package main

import (
	"bytes"
	"context"
	"encoding/json"
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
