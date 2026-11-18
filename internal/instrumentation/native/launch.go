package native

import (
	"errors"
	"time"

	"github.com/gabrielteiga/AntiPessimizerV2/internal/profiling"
)

type SelectedProcedure struct {
	ID   uint32 `json:"id"`
	RVA  uint32 `json:"rva"`
	Size uint32 `json:"size"`
	Name string `json:"name"`
	Unit string `json:"unit,omitempty"`
}
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
	if o.Image == "" || o.DLL == "" || len(o.Procedures) == 0 || len(o.Procedures) > 16 || o.Capacity < 1 || o.Capacity > 1024 || o.Timeout < time.Millisecond || o.Timeout > 24*time.Hour {
		return errors.New("invalid owned instrumented launch options")
	}
	ids := map[uint32]bool{}
	for _, p := range o.Procedures {
		if p.ID == 0 || ids[p.ID] || p.RVA == 0 || p.Size < 5 || p.Size > 4096 {
			return errors.New("invalid instrumented procedure selection")
		}
		if err := (profiling.Location{ModuleID: "checked-build", Module: "image.exe", Procedure: p.Name, Unit: p.Unit, RVA: "0x0000000000000000", Status: "resolved"}).Validate(); err != nil {
			return err
		}
		ids[p.ID] = true
	}
	return nil
}
