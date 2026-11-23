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
	ProcedureCount         int // raw TD32 procedure records, before exact-alias deduplication
	source                 td32.SourceIndex
	code                   Segment
}

// LoadImage supports the single-code-segment convention used by AntiPessimizer:
// TD32 segment 1 is relative to PE BaseOfCode. Other layouts are rejected rather
// than guessing a segment-to-section correspondence. A symbol-free image yields
// a usable empty index; malformed symbols or unsupported mappings return errors.
func LoadImage(path string) (*Image, error) {
	return loadImageFile(path, false)
}

// LoadImageWithSource also prepares the optional TD32 source-line index.
func LoadImageWithSource(path string) (*Image, error) {
	return loadImageFile(path, true)
}

func loadImageFile(path string, includeSource bool) (*Image, error) {
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
	return loadImage(data, path, includeSource)
}

func loadImage(data []byte, path string, includeSource bool) (*Image, error) {
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
	sidecar := strings.TrimSuffix(path, filepath.Ext(path)) + ".tds"
	var ps []td32.Procedure
	var source string
	if includeSource {
		ps, result.source, source, err = td32.LoadPEWithSource(data, sidecar, path)
	} else {
		ps, source, err = td32.LoadPE(data, sidecar, path)
	}
	if errors.Is(err, td32.ErrNoSymbols) {
		return result, nil
	}
	if err != nil {
		return nil, err
	}
	if len(ps) == 0 && result.source.Empty() {
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
	result.code = segment
	result.Index, err = fromProcedures(result.ID, result.Name, result.Size, ps, map[uint16]Segment{1: segment})
	if err != nil {
		return nil, err
	}
	result.SymbolSource = source
	result.ProcedureCount = len(ps)
	return result, nil
}

// SourceAt reports the file/line for the observed address, independently of
// the procedure start used for aggregation.
func (image *Image) SourceAt(base, address uint64) (td32.SourceLocation, bool) {
	if address < base || address-base >= image.Size || address-base < image.code.RVA {
		return td32.SourceLocation{}, false
	}
	offset := address - base - image.code.RVA
	if offset >= image.code.Size {
		return td32.SourceLocation{}, false
	}
	return image.source.Lookup(1, uint32(offset))
}
