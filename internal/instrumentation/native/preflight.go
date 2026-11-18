package native

import (
	"bytes"
	"crypto/sha256"
	"debug/pe"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/gabrielteiga/AntiPessimizerV2/internal/instrumentation/x64"
)

// ProcedureCheck reports whether static planning succeeds with a nearby probe
// allocation. It does not certify runtime relocation, ABI or Delphi compatibility;
// installation must still validate actual addresses and original target bytes.
type ProcedureCheck struct {
	ID            uint32 `json:"id"`
	PlanSupported bool   `json:"plan_supported"`
	Reason        string `json:"reason,omitempty"`
}

// Check reads the exact build selected by this catalog, without executing it or
// loading the native DLL. Callers may check one catalog page or a final selection.
func (c Catalog) Check(procedures []SelectedProcedure) ([]ProcedureCheck, error) {
	if c.sourcePath == "" {
		return nil, errors.New("preflight requires a catalog loaded from an executable")
	}
	f, err := os.Open(c.sourcePath)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	stat, err := f.Stat()
	if err != nil {
		return nil, err
	}
	if !stat.Mode().IsRegular() || stat.Size() > 128<<20 {
		return nil, errors.New("preflight executable exceeds the native capture limit of 128 MiB or is not a regular file")
	}
	data, err := io.ReadAll(io.LimitReader(f, (128<<20)+1))
	if err != nil {
		return nil, err
	}
	if len(data) > 128<<20 {
		return nil, errors.New("preflight executable exceeds 128 MiB")
	}
	if err := c.RequireImage(fmt.Sprintf("sha256:%x", sha256.Sum256(data))); err != nil {
		return nil, err
	}
	known := make(map[uint32]SelectedProcedure, len(c.Procedures))
	for _, p := range c.Procedures {
		known[p.ID] = p
	}
	for _, p := range procedures {
		if original, ok := known[p.ID]; !ok || original != p {
			return nil, errors.New("preflight selection differs from the symbol catalog")
		}
	}
	return CheckImageProcedures(data, procedures)
}

// CheckImageProcedures uses the same planner as native installation. No code is
// emitted into another process, and the input executable is never modified.
func CheckImageProcedures(data []byte, procedures []SelectedProcedure) ([]ProcedureCheck, error) {
	f, err := pe.NewFile(bytes.NewReader(data))
	if err != nil {
		return nil, err
	}
	defer f.Close()
	header, ok := f.OptionalHeader.(*pe.OptionalHeader64)
	if !ok || f.Machine != pe.IMAGE_FILE_MACHINE_AMD64 {
		return nil, errors.New("instrumentation preflight requires a Windows x64 image")
	}
	const loadedBase uint64 = 0x100000000
	probe := loadedBase + ((uint64(header.SizeOfImage) + 0xffff) &^ 0xffff) + 0x10000
	helper := probe + 0x1000000
	checks := make([]ProcedureCheck, 0, len(procedures))
	for _, p := range procedures {
		check := ProcedureCheck{ID: p.ID}
		if p.ID == 0 || p.RVA == 0 || p.Size < 5 || p.Size > 4096 {
			check.Reason = "procedure requires a nonzero ID/RVA and a size of 5..4096 bytes"
		} else if _, err := x64.BuildPEFunction(data, p.RVA, p.Size, loadedBase, probe, helper, helper+0x100, helper+0x200, p.ID); err != nil {
			check.Reason = err.Error()
		} else {
			check.PlanSupported = true
		}
		checks = append(checks, check)
	}
	return checks, nil
}

func RequireSupported(checks []ProcedureCheck) error {
	var reasons []string
	for _, check := range checks {
		if !check.PlanSupported {
			reasons = append(reasons, fmt.Sprintf("procedure %d: %s", check.ID, check.Reason))
		}
	}
	if len(reasons) != 0 {
		return fmt.Errorf("instrumentation preflight rejected selection: %s", strings.Join(reasons, "; "))
	}
	return nil
}
