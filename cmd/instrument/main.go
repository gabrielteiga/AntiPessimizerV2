// Command instrument launches an owned Windows x64 executable and measures an
// explicit TD32/TDS procedure selection. Native frames and calls require verified
// version-1 unwind; original language handlers remain unsupported.
package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"time"

	"github.com/gabrielteiga/AntiPessimizerV2/internal/instrumentation/native"
)

type repeated []string

func (r *repeated) String() string     { return strings.Join(*r, ", ") }
func (r *repeated) Set(s string) error { *r = append(*r, s); return nil }
func run(ctx context.Context, args []string, out, diagnostics io.Writer) (resultErr error) {
	flags := flag.NewFlagSet("instrument", flag.ContinueOnError)
	flags.SetOutput(diagnostics)
	path := flags.String("image", "", "Windows x64 executable to launch and own")
	exe, err := os.Executable()
	if err != nil {
		return err
	}
	dll := flags.String("dll", filepath.Join(filepath.Dir(exe), "AntiPessimizerNative.dll"), "native adapter DLL")
	list := flags.Bool("list", false, "list the TD32/TDS catalog without executing anything")
	check := flags.Bool("check", false, "with --list, check static instrumentation support without launching")
	outputPath := flags.String("output", "", "write live metrics to a new NDJSON file (never overwrite)")
	timeout := flags.Duration("timeout", time.Minute, "capture timeout; expiry terminates this owned target")
	capacity := flags.Int("capacity", 1024, "native events per thread (1..1024)")
	var units, names, programArgs repeated
	flags.Var(&units, "unit", "exact Pascal unit to select (repeatable)")
	flags.Var(&names, "procedure", "exact procedure to select (repeatable)")
	flags.Var(&programArgs, "arg", "argument passed to the owned target (repeatable)")
	if err := flags.Parse(args); err != nil {
		return err
	}
	if *path == "" || flags.NArg() != 0 {
		return errors.New("usage: instrument --image app.exe --list | --procedure Name [--unit Unit] [--arg value]")
	}
	if *check && !*list {
		return errors.New("--check requires --list")
	}
	catalog, err := native.ReadCatalog(*path)
	if err != nil {
		return err
	}
	selected := catalog.Match(units, names)
	if len(selected) == 0 {
		return errors.New("no procedures match the requested selection")
	}
	if *list && *outputPath != "" {
		return errors.New("--output is for capture; --list writes the catalog to stdout")
	}
	if *list {
		aliases, _ := catalog.AliasesFor(selected, 0)
		var checks []native.ProcedureCheck
		if *check {
			checks, err = catalog.Check(selected)
			if err != nil {
				return err
			}
		}
		return json.NewEncoder(out).Encode(struct {
			ImageID      string                     `json:"image_id"`
			SymbolSource string                     `json:"symbol_source"`
			Procedures   []native.SelectedProcedure `json:"procedures"`
			Checks       []native.ProcedureCheck    `json:"checks,omitempty"`
			Aliases      []native.ProcedureAlias    `json:"aliases,omitempty"`
		}{catalog.ImageID, catalog.SymbolSource, selected, checks, aliases})
	}
	if len(units) == 0 && len(names) == 0 {
		return errors.New("select --procedure or --unit explicitly before launching")
	}
	if len(selected) > 16 {
		return fmt.Errorf("selection has %d procedures; this increment supports at most 16", len(selected))
	}
	if *outputPath != "" {
		f, err := os.OpenFile(*outputPath, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
		if err != nil {
			return err
		}
		defer func() { resultErr = errors.Join(resultErr, f.Close()) }()
		out = f
	}
	return native.Launch(ctx, native.LaunchOptions{Image: *path, ImageID: catalog.ImageID, DLL: *dll, Args: programArgs, Procedures: selected, Capacity: *capacity, Timeout: *timeout}, out)
}
func main() {
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt)
	defer cancel()
	if err := run(ctx, os.Args[1:], os.Stdout, os.Stderr); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
