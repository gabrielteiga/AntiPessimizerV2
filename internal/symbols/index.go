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
	aliases    []Procedure
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
	// The original JCL FindProc returns the first matching symbol in file
	// order. Stable sorting preserves that primary name for exact aliases.
	sort.SliceStable(ps, func(i, j int) bool { return ps[i].RVA < ps[j].RVA })
	unique := ps[:0]
	var aliases []Procedure
	seenAliases := map[Procedure]bool{}
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
		if p.Size == 0 || p.RVA >= size || p.Size > size-p.RVA {
			return nil, errors.New("invalid or overlapping procedure range")
		}
		if len(unique) > 0 {
			previous := unique[len(unique)-1]
			if p.RVA == previous.RVA && p.Size == previous.Size {
				if p != previous && !seenAliases[p] {
					aliases = append(aliases, p)
					seenAliases[p] = true
				}
				continue
			}
		}
		if p.RVA < end {
			return nil, errors.New("invalid or overlapping procedure range")
		}
		unique = append(unique, p)
		end = p.RVA + p.Size
	}
	return &Index{id: id, name: name, size: size, procedures: unique, aliases: aliases}, nil
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

// Procedures returns a detached, RVA-ordered catalog for explicit selection.
// Mutating this copy cannot affect address resolution in an active session.
func (x *Index) Procedures() []Procedure { return append([]Procedure(nil), x.procedures...) }

// Aliases returns detached alternate names for exactly identical ranges.
// Partial overlaps and different extents at one address remain invalid.
func (x *Index) Aliases() []Procedure { return append([]Procedure(nil), x.aliases...) }
