// Command td32dump inspects symbols without executing the target image.
package main

import (
	"encoding/json"
	"fmt"
	"github.com/gabrielteiga/AntiPessimizerV2/internal/symbols"
	"github.com/gabrielteiga/AntiPessimizerV2/internal/symbols/td32"
	"io"
	"os"
)

func run(args []string, out io.Writer) error {
	if len(args) != 1 {
		return fmt.Errorf("usage: td32dump <image.exe|symbols.tds>")
	}
	ps, source, err := td32.Load(args[0])
	if err != nil {
		return err
	}
	display := make([]string, len(ps))
	for i, p := range ps {
		display[i] = symbols.DisplayName(p.Name)
	}
	enc := json.NewEncoder(out)
	enc.SetIndent("", "  ")
	return enc.Encode(struct {
		DisplayNames []string
		Source       string
		Procedures   []td32.Procedure
	}{display, source, ps})
}
func main() {
	if err := run(os.Args[1:], os.Stdout); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
