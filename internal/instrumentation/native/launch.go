package native

import (
	"errors"
	"time"

	"github.com/gabrielteiga/AntiPessimizerV2/internal/instrumentation/x64"
	"github.com/gabrielteiga/AntiPessimizerV2/internal/profiling"
)

type SelectedProcedure struct {
	ID   uint32 `json:"id"`
	RVA  uint32 `json:"rva"`
	Size uint32 `json:"size"`
	Name string `json:"name"`
	Unit string `json:"unit,omitempty"`
}

// MaxSelectedProcedures matches the native wire catalog and bounded installer.
const MaxSelectedProcedures = 64
const MaxProcedureBytes = x64.MaxFunctionBytes

type LaunchOptions struct {
	Control *Control
	ImageID string // optional expected SHA-256 from symbol selection

	Image, DLL string
	Args       []string
	Procedures []SelectedProcedure
	Capacity   int
	Timeout    time.Duration
}

func (o LaunchOptions) validate() error {
	if o.Image == "" || o.DLL == "" || len(o.Procedures) == 0 || len(o.Procedures) > MaxSelectedProcedures || o.Capacity < 1 || o.Capacity > 1024 || o.Timeout < time.Millisecond || o.Timeout > 24*time.Hour {
		return errors.New("invalid owned instrumented launch options")
	}
	ids := map[uint32]bool{}
	for i, p := range o.Procedures {
		if p.ID == 0 || ids[p.ID] || p.RVA == 0 || p.Size < 5 || p.Size > MaxProcedureBytes || uint64(p.RVA)+uint64(p.Size) > 1<<32 {
			return errors.New("invalid instrumented procedure selection")
		}
		for _, previous := range o.Procedures[:i] {
			if uint64(p.RVA) < uint64(previous.RVA)+uint64(previous.Size) && uint64(previous.RVA) < uint64(p.RVA)+uint64(p.Size) {
				return errors.New("instrumented procedure selections overlap; select each code interval once")
			}
		}
		if err := (profiling.Location{ModuleID: "checked-build", Module: "image.exe", Procedure: p.Name, Unit: p.Unit, RVA: "0x0000000000000000", Status: "resolved"}).Validate(); err != nil {
			return err
		}
		ids[p.ID] = true
	}
	return nil
}
