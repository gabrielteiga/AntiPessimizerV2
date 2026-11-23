package main

import (
	"bytes"
	"encoding/binary"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/gabrielteiga/AntiPessimizerV2/internal/profiling"
	"github.com/gabrielteiga/AntiPessimizerV2/internal/symbols/td32"
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

func TestCLIResolvesSourceFromEmbeddedAndSidecar(t *testing.T) {
	for _, source := range []string{"embedded", "sidecar"} {
		t.Run(source, func(t *testing.T) {
			dir := t.TempDir()
			path := filepath.Join(dir, "lab.exe")
			tds := testfixture.TDSWithSource()
			image := testfixture.Image(source == "embedded")
			if source == "embedded" {
				at := binary.LittleEndian.Uint32(image[0x318:])
				image = append(image[:at], tds...)
				binary.LittleEndian.PutUint32(image[0x310:], uint32(len(tds)))
			} else if err := os.WriteFile(filepath.Join(dir, "lab.tds"), tds, 0600); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(path, image, 0600); err != nil {
				t.Fatal(err)
			}
			var out, diag bytes.Buffer
			if err := run([]string{"--source", "--image", path, "--base", "0x7ff000000000", "0x7ff000005027", "0x7ff00000502b", "0x7ff000005030"}, &out, &diag); err != nil {
				t.Fatal(err)
			}
			var report struct {
				Resolutions []struct {
					Location profiling.Location   `json:"location"`
					Source   *td32.SourceLocation `json:"source"`
				} `json:"resolutions"`
			}
			if err := json.Unmarshal(out.Bytes(), &report); err != nil {
				t.Fatal(err)
			}
			rows := report.Resolutions
			first := td32.SourceLocation{File: `C:\lab\Workload.pas`, Line: 101, LineOffset: 7}
			second := td32.SourceLocation{File: first.File, Line: 109, LineOffset: 3}
			if len(rows) != 3 || rows[0].Source == nil || *rows[0].Source != first ||
				rows[1].Source == nil || *rows[1].Source != second || rows[2].Source != nil ||
				rows[0].Location.Procedure != "Lab.Hot" || rows[0].Location != rows[1].Location {
				t.Fatal(out.String())
			}
		})
	}
}
