package td32

import (
	"bytes"
	"debug/pe"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

// Load searches IMAGE_DEBUG_TYPE_UNKNOWN entries first, then an adjacent .tds,
// following JCL's source priority. Source identifies the selected file. A
// malformed embedded candidate is reported if no valid sidecar can replace it.
// It does not establish that an external TDS belongs to this image.
func Load(path string) (procedures []Procedure, source string, err error) {
	image, err := readBounded(path)
	if err != nil {
		return nil, "", err
	}
	if strings.EqualFold(filepath.Ext(path), ".tds") {
		ps, err := Parse(image)
		return ps, path, err
	}
	return LoadPE(image, strings.TrimSuffix(path, filepath.Ext(path))+".tds", path)
}

// LoadPE searches the supplied immutable PE bytes, then the specified sidecar.
// imageSource labels embedded results; sidecar is read only if needed.
func LoadPE(image []byte, sidecar, imageSource string) ([]Procedure, string, error) {
	return loadPE(image, sidecar, imageSource, nil)
}

// LoadPEWithSource opts into source-line validation for the same debug payload
// that supplies the procedures.
func LoadPEWithSource(image []byte, sidecar, imageSource string) ([]Procedure, SourceIndex, string, error) {
	var source SourceIndex
	procedures, path, err := loadPE(image, sidecar, imageSource, &source)
	return procedures, source, path, err
}

func loadPE(image []byte, sidecar, imageSource string, source *SourceIndex) ([]Procedure, string, error) {
	if len(image) > 256<<20 {
		return nil, "", ErrInvalid
	}
	ps, embeddedErr := embedded(image, source)
	if embeddedErr == nil {
		return ps, imageSource, nil
	}
	data, err := readBounded(sidecar)
	if err == nil {
		ps, err = parse(data, source)
		return ps, sidecar, err
	}
	if !errors.Is(err, os.ErrNotExist) {
		return nil, "", err
	}
	return nil, "", embeddedErr
}

func readBounded(path string) ([]byte, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	b, err := io.ReadAll(io.LimitReader(f, (256<<20)+1))
	if err != nil {
		return nil, err
	}
	if len(b) > 256<<20 {
		return nil, fmt.Errorf("%w: file exceeds 256 MiB", ErrInvalid)
	}
	return b, nil
}

func embedded(image []byte, source *SourceIndex) ([]Procedure, error) {
	f, err := pe.NewFile(bytes.NewReader(image))
	if err != nil {
		return nil, fmt.Errorf("read PE: %w", err)
	}
	defer f.Close()
	var d pe.DataDirectory
	switch h := f.OptionalHeader.(type) {
	case *pe.OptionalHeader32:
		if h.NumberOfRvaAndSizes > 6 {
			d = h.DataDirectory[6]
		}
	case *pe.OptionalHeader64:
		if h.NumberOfRvaAndSizes > 6 {
			d = h.DataDirectory[6]
		}
	default:
		return nil, fmt.Errorf("PE optional header missing")
	}
	if d.Size == 0 {
		return nil, ErrNoSymbols
	}
	if d.Size%28 != 0 {
		return nil, ErrInvalid
	}
	var directory []byte
	for _, s := range f.Sections {
		if d.VirtualAddress >= s.VirtualAddress {
			delta := uint64(d.VirtualAddress - s.VirtualAddress)
			if delta <= uint64(s.Size) && uint64(d.Size) <= uint64(s.Size)-delta {
				directory, err = region(image, uint64(s.Offset)+delta, uint64(d.Size))
				if err != nil {
					return nil, err
				}
				break
			}
		}
	}
	if directory == nil {
		return nil, ErrInvalid
	}
	candidateErr := error(ErrNoSymbols)
	for len(directory) > 0 {
		e := directory[:28]
		directory = directory[28:]
		if u32(e[12:]) != 0 {
			continue
		}
		payload, err := region(image, uint64(u32(e[24:])), uint64(u32(e[16:])))
		if err == nil {
			var ps []Procedure
			ps, err = parse(payload, source)
			if err == nil {
				return ps, nil
			}
		}
		if !errors.Is(err, ErrNoSymbols) {
			candidateErr = err
		}
	}
	return nil, candidateErr
}
