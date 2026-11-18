package native

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"path/filepath"
	"strings"

	"github.com/gabrielteiga/AntiPessimizerV2/internal/symbols"
)

// Catalog identifies the exact executable used to resolve TD32/TDS procedures.
// Entries have stable IDs within this catalog, independent of later filters.
type Catalog struct {
	CatalogID    string              `json:"catalog_id"`
	ImageID      string              `json:"image_id"`
	SymbolSource string              `json:"symbol_source"`
	Procedures   []SelectedProcedure `json:"procedures"`
	sourcePath   string
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
	if len(ids) == 0 || len(ids) > 16 {
		return nil, errors.New("select 1..16 procedures explicitly")
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
	var result []SelectedProcedure
	for _, p := range c.Procedures {
		if matches(units, p.Unit) && matches(names, p.Name) {
			result = append(result, p)
		}
	}
	return result
}

func (c Catalog) RequireImage(id string) error {
	if id == "" || id != c.ImageID {
		return fmt.Errorf("executable changed since symbol selection; reload the catalog")
	}
	return nil
}
