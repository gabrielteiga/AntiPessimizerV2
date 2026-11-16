// Package symbols maps image-relative addresses to procedure intervals.
// It follows the name/offset/size model used by AntiPessimizer's ExeLoader.
package symbols

import (
	"errors"
	"fmt"
	"sort"

	"github.com/gabrielteiga/AntiPessimizerV2/internal/profiling"
)

type Procedure struct {
	Name      string
	RVA, Size uint64
	Unit      string
}
type Index struct {
	id, name   string
	size       uint64
	procedures []Procedure
}

func New(id, name string, size uint64, procedures []Procedure) (*Index, error) {
	base := profiling.Location{ModuleID: id, Module: name, RVA: fmt.Sprintf("0x%016x", uint64(0)), Status: "unknown", Reason: "no_symbol"}
	if err := base.Validate(); err != nil {
		return nil, err
	}
	if size == 0 {
		return nil, errors.New("empty image")
	}
	ps := append([]Procedure(nil), procedures...)
	sort.Slice(ps, func(i, j int) bool { return ps[i].RVA < ps[j].RVA })
	var end uint64
	for _, p := range ps {
		loc := base
		loc.Status = "resolved"
		loc.Reason = ""
		loc.Procedure = p.Name
		loc.Unit = p.Unit
		if err := loc.Validate(); err != nil {
			return nil, err
		}
		if p.Size == 0 || p.RVA >= size || p.Size > size-p.RVA || p.RVA < end {
			return nil, errors.New("invalid or overlapping procedure range")
		}
		end = p.RVA + p.Size
	}
	return &Index{id: id, name: name, size: size, procedures: ps}, nil
}

// Resolve uses the actual load base. It never assumes a fixed code offset.
// A false result means the address does not belong to this image.
func (x *Index) Resolve(base, address uint64) (profiling.Location, bool) {
	if address < base || address-base >= x.size {
		return profiling.Location{}, false
	}
	rva := address - base
	loc := profiling.Location{ModuleID: x.id, Module: x.name, RVA: fmt.Sprintf("0x%016x", rva), Status: "unknown", Reason: "no_symbol"}
	i := sort.Search(len(x.procedures), func(i int) bool { return x.procedures[i].RVA > rva }) - 1
	if i >= 0 {
		p := x.procedures[i]
		if rva-p.RVA < p.Size {
			loc.RVA = fmt.Sprintf("0x%016x", p.RVA)
			loc.Procedure = p.Name
			loc.Unit = p.Unit
			loc.Status = "resolved"
			loc.Reason = ""
		}
	}
	return loc, true
}
