package symbols

import (
	"fmt"
	"github.com/gabrielteiga/AntiPessimizerV2/internal/symbols/td32"
)

// Segment describes a verified TD32 segment's image-relative range. The caller
// must establish the mapping for the matching PE; segment numbers are not RVAs.
type Segment struct{ RVA, Size uint64 }

// FromTD32 composes the parser with the existing ASLR-aware interval index.
// No partially resolved index is returned on an unknown segment or bad range.
func FromTD32(id, name string, imageSize uint64, data []byte, segments map[uint16]Segment) (*Index, error) {
	ps, err := td32.Parse(data)
	if err != nil {
		return nil, err
	}
	return fromProcedures(id, name, imageSize, ps, segments)
}

func fromProcedures(id, name string, imageSize uint64, ps []td32.Procedure, segments map[uint16]Segment) (*Index, error) {
	procedures := make([]Procedure, 0, len(ps))
	for _, p := range ps {
		s, ok := segments[p.Segment]
		if !ok || s.RVA >= imageSize || s.Size > imageSize-s.RVA || uint64(p.Offset) >= s.Size || uint64(p.Size) > s.Size-uint64(p.Offset) {
			return nil, fmt.Errorf("TD32 procedure %q: invalid or unmapped segment %d", p.Name, p.Segment)
		}
		procedures = append(procedures, Procedure{Unit: DisplayUnit(p.Unit), Name: DisplayName(p.Name), RVA: s.RVA + uint64(p.Offset), Size: uint64(p.Size)})
	}
	return New(id, name, imageSize, procedures)
}
