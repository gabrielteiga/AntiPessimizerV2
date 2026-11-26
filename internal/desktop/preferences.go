package desktop

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"unicode"
	"unicode/utf8"
)

const maxPreferencesBytes = 128 << 10

// Preferences mirrors the original viewer's executable path, result/unit
// filters and time unit. Collection data and active target state live elsewhere.
type Preferences struct {
	CallDiscountTicks uint64            `json:"call_discount_ticks,string,omitempty"`
	Columns           columnPreferences `json:"columns,omitzero"`
	ExcludeRuntime    bool              `json:"exclude_runtime,omitempty"`
	WholeUnit         bool              `json:"whole_unit,omitempty"`
	Version           int               `json:"version"`
	ImagePath         string            `json:"image_path"`
	ResultFilter      string            `json:"result_filter"`
	SymbolFilter      string            `json:"symbol_filter"`
	UnitSelection     string            `json:"unit_selection"`
	TimeUnit          MetricTimeUnit    `json:"time_unit"`
}

func defaultPreferences() Preferences {
	return Preferences{Version: 1, TimeUnit: Milliseconds}
}

func preferencesPath() (string, error) {
	directory, err := os.UserConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(directory, "AntiPessimizerV2", "viewer.json"), nil
}

func (p Preferences) validate() error {
	if p.Version != 1 || !p.TimeUnit.Valid() {
		return errors.New("unsupported viewer preferences version/time unit")
	}
	for view, columns := range p.Columns {
		if !columns.valid(len(defaultColumnWidths[view])) {
			return errors.New("invalid viewer result columns")
		}
	}
	for _, field := range []struct {
		name, value string
		limit       int
	}{{"image_path", p.ImagePath, 32768}, {"result_filter", p.ResultFilter, 8192}, {"symbol_filter", p.SymbolFilter, 8192}, {"unit_selection", p.UnitSelection, 8192}} {
		if len(field.value) > field.limit || !utf8.ValidString(field.value) {
			return fmt.Errorf("invalid viewer preference %s", field.name)
		}
		for _, r := range field.value {
			if unicode.IsControl(r) {
				return fmt.Errorf("control character in viewer preference %s", field.name)
			}
		}
	}
	return nil
}

func loadPreferences(path string) (Preferences, error) {
	f, err := os.Open(path)
	if errors.Is(err, os.ErrNotExist) {
		return defaultPreferences(), nil
	}
	if err != nil {
		return Preferences{}, err
	}
	defer f.Close()
	data, err := io.ReadAll(io.LimitReader(f, maxPreferencesBytes+1))
	if err != nil {
		return Preferences{}, err
	}
	if len(data) > maxPreferencesBytes || !utf8.Valid(data) {
		return Preferences{}, errors.New("viewer preferences exceed size/UTF-8 limits")
	}
	d := json.NewDecoder(bytes.NewReader(data))
	d.DisallowUnknownFields()
	var p Preferences
	if err := d.Decode(&p); err != nil {
		return Preferences{}, err
	}
	var extra any
	if err := d.Decode(&extra); err != io.EOF {
		return Preferences{}, errors.New("expected one viewer preferences object")
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(data, &fields); err != nil {
		return Preferences{}, err
	}
	if _, present := fields["unit_selection"]; !present && p.WholeUnit {
		// Older viewers persisted both modes in symbol_filter. Preserve the
		// saved unit names without turning them into a procedure query.
		p.UnitSelection, p.SymbolFilter = p.SymbolFilter, ""
	}
	if err := p.validate(); err != nil {
		return Preferences{}, err
	}
	p.TimeUnit = p.TimeUnit.normalized()
	return p, nil
}

func savePreferences(path string, p Preferences) error {
	if err := p.validate(); err != nil {
		return err
	}
	p.TimeUnit = p.TimeUnit.normalized()
	data, err := json.MarshalIndent(p, "", "  ")
	if err != nil {
		return err
	}
	data = append(data, '\n')
	if len(data) > maxPreferencesBytes {
		return errors.New("viewer preferences exceed size limit")
	}
	directory := filepath.Dir(path)
	if err := os.MkdirAll(directory, 0700); err != nil {
		return err
	}
	f, err := os.CreateTemp(directory, ".viewer-*.tmp")
	if err != nil {
		return err
	}
	name := f.Name()
	defer os.Remove(name)
	if _, err := f.Write(data); err != nil {
		f.Close()
		return err
	}
	if err := f.Sync(); err != nil {
		f.Close()
		return err
	}
	if err := f.Close(); err != nil {
		return err
	}
	return os.Rename(name, path)
}

func (p Preferences) catalogQuery() string {
	if p.WholeUnit {
		return p.UnitSelection
	}
	return p.SymbolFilter
}

func (p *Preferences) setCatalogQuery(query string) {
	if p.WholeUnit {
		p.UnitSelection = query
	} else {
		p.SymbolFilter = query
	}
}

func (p *Preferences) switchCatalogMode(whole bool, query string) string {
	p.setCatalogQuery(query)
	p.WholeUnit = whole
	return p.catalogQuery()
}
