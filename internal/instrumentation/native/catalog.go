package native

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"path/filepath"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/gabrielteiga/AntiPessimizerV2/internal/symbols"
)

// Catalog identifies the exact executable used to resolve TD32/TDS procedures.
// Entries have stable IDs within this catalog, independent of later filters.
type Catalog struct {
	CatalogID    string              `json:"catalog_id"`
	ImageID      string              `json:"image_id"`
	SymbolSource string              `json:"symbol_source"`
	Procedures   []SelectedProcedure `json:"procedures"`
	Aliases      []ProcedureAlias    `json:"aliases,omitempty"`
	sourcePath   string
}

// ProcedureAlias names the same complete code interval as one primary catalog
// ID. Selection and metrics use that ID once, regardless of matching names.
type ProcedureAlias struct {
	ID   uint32 `json:"id"`
	Name string `json:"name"`
	Unit string `json:"unit,omitempty"`
}

func ReadCatalog(path string) (Catalog, error) {
	image, err := symbols.LoadImage(path)
	if err != nil {
		return Catalog{}, err
	}
	path, err = filepath.Abs(path)
	if err != nil {
		return Catalog{}, err
	}
	result := Catalog{ImageID: image.ID, SymbolSource: image.SymbolSource, sourcePath: path}
	for i, p := range image.Index.Procedures() {
		result.Procedures = append(result.Procedures, SelectedProcedure{ID: uint32(i + 1), RVA: uint32(p.RVA), Size: uint32(p.Size), Name: p.Name, Unit: p.Unit})
	}
	ids := make(map[uint32]uint32, len(result.Procedures))
	for _, p := range result.Procedures {
		ids[p.RVA] = p.ID
	}
	for _, alias := range image.Index.Aliases() {
		result.Aliases = append(result.Aliases, ProcedureAlias{ID: ids[uint32(alias.RVA)], Name: alias.Name, Unit: alias.Unit})
	}
	if len(result.Procedures) == 0 {
		return Catalog{}, errors.New("no supported TD32/TDS procedures found")
	}
	encoded, err := json.Marshal(result)
	if err != nil {
		return Catalog{}, err
	}
	hash := sha256.Sum256(encoded)
	result.CatalogID = "sha256:" + hex.EncodeToString(hash[:])
	return result, nil
}

func (c Catalog) Select(ids []uint32) ([]SelectedProcedure, error) {
	if len(ids) == 0 || len(ids) > MaxSelectedProcedures {
		return nil, fmt.Errorf("select 1..%d procedures explicitly", MaxSelectedProcedures)
	}
	requested := map[uint32]bool{}
	for _, id := range ids {
		if id == 0 || requested[id] {
			return nil, errors.New("invalid or duplicate procedure selection")
		}
		requested[id] = true
	}
	result := make([]SelectedProcedure, 0, len(ids))
	for _, p := range c.Procedures {
		if requested[p.ID] {
			result = append(result, p)
		}
	}
	if len(result) != len(ids) {
		return nil, errors.New("selection is absent from the current symbol catalog")
	}
	return result, nil
}

// SelectUnits expands exact Pascal unit names against the complete catalog,
// including aliases. It never truncates to a page or silently skips a unit.
// The final selection still requires the ordinary code/ABI preflight.
func (c Catalog) SelectUnits(units []string) ([]SelectedProcedure, error) {
	if len(units) == 0 || len(units) > MaxSelectedProcedures {
		return nil, fmt.Errorf("select 1..%d exact Pascal units", MaxSelectedProcedures)
	}
	for i, unit := range units {
		if strings.TrimSpace(unit) == "" || len(unit) > 1024 || !utf8.ValidString(unit) || strings.ContainsFunc(unit, unicode.IsControl) {
			return nil, errors.New("invalid Pascal unit selection")
		}
		for _, previous := range units[:i] {
			if strings.EqualFold(previous, unit) {
				return nil, errors.New("duplicate Pascal unit selection")
			}
		}
		if len(c.Match([]string{unit}, nil)) == 0 {
			return nil, fmt.Errorf("Pascal unit absent from the current symbol catalog: %s", unit)
		}
	}
	matched := c.Match(units, nil)
	ids := make([]uint32, 0, len(matched))
	for _, p := range matched {
		ids = append(ids, p.ID)
	}
	if len(ids) > MaxSelectedProcedures {
		return nil, fmt.Errorf("units contain %d procedures; selection limit is %d; choose fewer units or explicit IDs", len(ids), MaxSelectedProcedures)
	}
	return c.Select(ids)
}

func (c Catalog) Match(units, names []string) []SelectedProcedure {
	matches := func(values []string, name string) bool {
		if len(values) == 0 {
			return true
		}
		for _, value := range values {
			if strings.EqualFold(value, name) {
				return true
			}
		}
		return false
	}
	return c.filter(func(name, unit string) bool { return matches(units, unit) && matches(names, name) })
}

func (c Catalog) Search(text, unit string) []SelectedProcedure {
	text = strings.ToLower(text)
	return c.filter(func(name, candidateUnit string) bool {
		return (unit == "" || strings.EqualFold(unit, candidateUnit)) && (text == "" || strings.Contains(strings.ToLower(candidateUnit+" "+name), text))
	})
}

func (c Catalog) filter(match func(name, unit string) bool) []SelectedProcedure {
	matchedAliases := map[uint32]bool{}
	for _, alias := range c.Aliases {
		if match(alias.Name, alias.Unit) {
			matchedAliases[alias.ID] = true
		}
	}
	var result []SelectedProcedure
	for _, p := range c.Procedures {
		if matchedAliases[p.ID] || match(p.Name, p.Unit) {
			result = append(result, p)
		}
	}
	return result
}

// AliasesFor limits presentation metadata independently from symbol lookup.
// limit=0 returns all matching aliases. Total includes entries beyond the limit.
func (c Catalog) AliasesFor(procedures []SelectedProcedure, limit int) (aliases []ProcedureAlias, total int) {
	ids := make(map[uint32]bool, len(procedures))
	for _, p := range procedures {
		ids[p.ID] = true
	}
	for _, alias := range c.Aliases {
		if ids[alias.ID] {
			total++
			if limit == 0 || len(aliases) < limit {
				aliases = append(aliases, alias)
			}
		}
	}
	return aliases, total
}

func (c Catalog) RequireImage(id string) error {
	if id == "" || id != c.ImageID {
		return fmt.Errorf("executable changed since symbol selection; reload the catalog")
	}
	return nil
}
