// Command peprobe inventories a PE image without loading or executing it.
// It is a Phase 0 experiment, not a Delphi symbol resolver.
package main

import (
	"crypto/sha256"
	"debug/pe"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"os"
)

type report struct {
	SHA256             string    `json:"sha256"`
	Machine            uint16    `json:"machine"`
	Format             string    `json:"format"`
	PreferredImageBase uint64    `json:"preferred_image_base"`
	ImageSize          uint32    `json:"image_size"`
	DebugDirectoryRVA  uint32    `json:"debug_directory_rva"`
	DebugDirectorySize uint32    `json:"debug_directory_size"`
	Sections           []section `json:"sections"`
}
type section struct {
	Name        string `json:"name"`
	RVA         uint32 `json:"rva"`
	VirtualSize uint32 `json:"virtual_size"`
	RawSize     uint32 `json:"raw_size"`
}

func inspect(r io.ReaderAt) (report, error) {
	f, err := pe.NewFile(r)
	if err != nil {
		return report{}, fmt.Errorf("read PE: %w", err)
	}
	result := report{Machine: f.Machine}
	var debug pe.DataDirectory
	switch h := f.OptionalHeader.(type) {
	case *pe.OptionalHeader64:
		result.Format = "PE32+"
		result.PreferredImageBase = h.ImageBase
		result.ImageSize = h.SizeOfImage
		if h.NumberOfRvaAndSizes > pe.IMAGE_DIRECTORY_ENTRY_DEBUG {
			debug = h.DataDirectory[pe.IMAGE_DIRECTORY_ENTRY_DEBUG]
		}
	case *pe.OptionalHeader32:
		result.Format = "PE32"
		result.PreferredImageBase = uint64(h.ImageBase)
		result.ImageSize = h.SizeOfImage
		if h.NumberOfRvaAndSizes > pe.IMAGE_DIRECTORY_ENTRY_DEBUG {
			debug = h.DataDirectory[pe.IMAGE_DIRECTORY_ENTRY_DEBUG]
		}
	default:
		return report{}, fmt.Errorf("missing executable optional header")
	}
	result.DebugDirectoryRVA, result.DebugDirectorySize = debug.VirtualAddress, debug.Size
	for _, s := range f.Sections {
		result.Sections = append(result.Sections, section{s.Name, s.VirtualAddress, s.VirtualSize, s.Size})
	}
	return result, nil
}

func run(args []string, out io.Writer) error {
	if len(args) != 1 {
		return fmt.Errorf("usage: peprobe <image.exe>")
	}
	f, err := os.Open(args[0])
	if err != nil {
		return err
	}
	defer f.Close()
	result, err := inspect(f)
	if err != nil {
		return err
	}
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return fmt.Errorf("hash image: %w", err)
	}
	result.SHA256 = hex.EncodeToString(h.Sum(nil))
	enc := json.NewEncoder(out)
	enc.SetIndent("", "  ")
	return enc.Encode(result)
}

func main() {
	if err := run(os.Args[1:], os.Stdout); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
