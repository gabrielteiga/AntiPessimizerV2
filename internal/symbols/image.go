package symbols

import (
	"bytes"
	"crypto/sha256"
	"debug/pe"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/gabrielteiga/AntiPessimizerV2/internal/symbols/td32"
)

// Image holds an immutable index for one image build. PreferredBase is metadata,
// never a substitute for the actual load base supplied to Index.Resolve.
type Image struct {
	Index                  *Index
	ID, Name, SymbolSource string
	Size, PreferredBase    uint64
	ProcedureCount         int
}

// LoadImage supports the single-code-segment convention used by AntiPessimizer:
// TD32 segment 1 is relative to PE BaseOfCode. Other layouts are rejected rather
// than guessing a segment-to-section correspondence. A symbol-free image yields
// a usable empty index; malformed symbols or unsupported mappings return errors.
func LoadImage(path string) (*Image, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	data, err := io.ReadAll(io.LimitReader(f, (256<<20)+1))
	if err != nil {
		return nil, err
	}
	if len(data) > 256<<20 {
		return nil, fmt.Errorf("image exceeds 256 MiB")
	}
	return loadImage(data, path)
}

func loadImage(data []byte, path string) (*Image, error) {
	f, err := pe.NewFile(bytes.NewReader(data))
	if err != nil {
		return nil, fmt.Errorf("read PE: %w", err)
	}
	defer f.Close()
	result := &Image{ID: fmt.Sprintf("sha256:%x", sha256.Sum256(data)), Name: filepath.Base(path)}
	var code uint32
	switch h := f.OptionalHeader.(type) {
	case *pe.OptionalHeader64:
		if f.Machine != pe.IMAGE_FILE_MACHINE_AMD64 {
			return nil, fmt.Errorf("unsupported PE machine")
		}
		result.Size = uint64(h.SizeOfImage)
		result.PreferredBase = h.ImageBase
		code = h.BaseOfCode
	case *pe.OptionalHeader32:
		if f.Machine != pe.IMAGE_FILE_MACHINE_I386 {
			return nil, fmt.Errorf("unsupported PE machine")
		}
		result.Size = uint64(h.SizeOfImage)
		result.PreferredBase = uint64(h.ImageBase)
		code = h.BaseOfCode
	default:
		return nil, fmt.Errorf("PE optional header missing")
	}
	// Validate the image identity and size even in the symbol-free case.
	result.Index, err = New(result.ID, result.Name, result.Size, nil)
	if err != nil {
		return nil, err
	}
	ps, source, err := td32.LoadPE(data, strings.TrimSuffix(path, filepath.Ext(path))+".tds", path)
	if errors.Is(err, td32.ErrNoSymbols) {
		return result, nil
	}
	if err != nil {
		return nil, err
	}
	if len(ps) == 0 {
		result.SymbolSource = source
		return result, nil
	}
	var segment Segment
	executable := 0
	for _, s := range f.Sections {
		if s.Characteristics&pe.IMAGE_SCN_MEM_EXECUTE == 0 {
			continue
		}
		executable++
		if s.VirtualAddress != code || s.VirtualSize == 0 || s.Characteristics&pe.IMAGE_SCN_CNT_CODE == 0 {
			return nil, fmt.Errorf("unsupported TD32 layout: code section must start at BaseOfCode")
		}
		segment = Segment{uint64(s.VirtualAddress), uint64(s.VirtualSize)}
	}
	if executable != 1 || code == 0 {
		return nil, fmt.Errorf("unsupported TD32 layout: expected one executable code section")
	}
	result.Index, err = fromProcedures(result.ID, result.Name, result.Size, ps, map[uint16]Segment{1: segment})
	if err != nil {
		return nil, err
	}
	result.SymbolSource = source
	result.ProcedureCount = len(ps)
	return result, nil
}
