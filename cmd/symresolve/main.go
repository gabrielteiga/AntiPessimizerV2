// Command symresolve resolves observed addresses without executing the image.
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"strconv"

	"github.com/gabrielteiga/AntiPessimizerV2/internal/profiling"
	"github.com/gabrielteiga/AntiPessimizerV2/internal/symbols"
	"github.com/gabrielteiga/AntiPessimizerV2/internal/symbols/td32"
)

func run(args []string, out, diagnostics io.Writer) error {
	flags := flag.NewFlagSet("symresolve", flag.ContinueOnError)
	flags.SetOutput(diagnostics)
	path := flags.String("image", "", "PE image to inspect")
	baseText := flags.String("base", "", "actual load base (decimal or 0x hexadecimal)")
	source := flags.Bool("source", false, "resolve optional TD32 file and line for each address")
	if err := flags.Parse(args); err != nil {
		return err
	}
	if *path == "" || *baseText == "" || flags.NArg() == 0 {
		return fmt.Errorf("usage: symresolve --image app.exe --base 0x140000000 [--source] <address>...")
	}
	base, err := strconv.ParseUint(*baseText, 0, 64)
	if err != nil {
		return fmt.Errorf("invalid load base: %w", err)
	}
	addresses := make([]uint64, flags.NArg())
	for i, s := range flags.Args() {
		addresses[i], err = strconv.ParseUint(s, 0, 64)
		if err != nil {
			return fmt.Errorf("invalid address %q: %w", s, err)
		}
	}
	load := symbols.LoadImage
	if *source {
		load = symbols.LoadImageWithSource
	}
	image, err := load(*path)
	if err != nil {
		return err
	}
	if image.Size-1 > ^uint64(0)-base {
		return fmt.Errorf("load range overflows address space")
	}
	type resolution struct {
		Address  string               `json:"address"`
		InImage  bool                 `json:"in_image"`
		Location *profiling.Location  `json:"location,omitempty"`
		Source   *td32.SourceLocation `json:"source,omitempty"`
	}
	rows := make([]resolution, 0, len(addresses))
	for _, addr := range addresses {
		loc, ok := image.Index.Resolve(base, addr)
		row := resolution{Address: fmt.Sprintf("0x%016x", addr), InImage: ok}
		if ok {
			row.Location = &loc
		}
		if *source {
			if line, ok := image.SourceAt(base, addr); ok {
				row.Source = &line
			}
		}
		rows = append(rows, row)
	}
	enc := json.NewEncoder(out)
	enc.SetIndent("", "  ")
	return enc.Encode(struct {
		SymbolSource   string       `json:"symbol_source,omitempty"`
		ProcedureCount int          `json:"procedure_count"`
		Resolutions    []resolution `json:"resolutions"`
	}{image.SymbolSource, image.ProcedureCount, rows})
}
func main() {
	if err := run(os.Args[1:], os.Stdout, os.Stderr); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
