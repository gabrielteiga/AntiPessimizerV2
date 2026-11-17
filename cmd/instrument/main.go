// Command instrument launches an owned Windows x64 executable and measures an
// explicit TD32/TDS procedure selection. Only checked native leaf functions are
// supported; functions requiring frames or language handlers are rejected.
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
	"github.com/gabrielteiga/AntiPessimizerV2/internal/symbols"
)

type repeated []string

func (r *repeated) String() string     { return strings.Join(*r, ", ") }
func (r *repeated) Set(s string) error { *r = append(*r, s); return nil }
func run(ctx context.Context, args []string, out, diagnostics io.Writer) error {
	flags := flag.NewFlagSet("instrument", flag.ContinueOnError)
	flags.SetOutput(diagnostics)
	path := flags.String("image", "", "Windows x64 executable to launch and own")
	exe, err := os.Executable()
	if err != nil {
		return err
	}
	dll := flags.String("dll", filepath.Join(filepath.Dir(exe), "AntiPessimizerNative.dll"), "native adapter DLL")
	list := flags.Bool("list", false, "list the TD32/TDS catalog without executing anything")
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
	image, err := symbols.LoadImage(*path)
	if err != nil {
		return err
	}
	catalog := image.Index.Procedures()
	if len(catalog) == 0 {
		return errors.New("no supported TD32/TDS procedures found")
	}
	matches := func(values []string, name string) bool {
		if len(values) == 0 {
			return true
		}
		for _, v := range values {
			if strings.EqualFold(v, name) {
				return true
			}
		}
		return false
	}
	var selected []native.SelectedProcedure
	for _, p := range catalog {
		if matches(units, p.Unit) && matches(names, p.Name) {
			selected = append(selected, native.SelectedProcedure{ID: uint32(len(selected) + 1), RVA: uint32(p.RVA), Size: uint32(p.Size), Name: p.Name, Unit: p.Unit})
		}
	}
	if len(selected) == 0 {
		return errors.New("no procedures match the requested selection")
	}
	if *list {
		return json.NewEncoder(out).Encode(struct {
			ImageID      string                     `json:"image_id"`
			SymbolSource string                     `json:"symbol_source"`
			Procedures   []native.SelectedProcedure `json:"procedures"`
		}{image.ID, image.SymbolSource, selected})
	}
	if len(units) == 0 && len(names) == 0 {
		return errors.New("select --procedure or --unit explicitly before launching")
	}
	if len(selected) > 16 {
		return fmt.Errorf("selection has %d procedures; this increment supports at most 16", len(selected))
	}
	return native.Launch(ctx, native.LaunchOptions{Image: *path, ImageID: image.ID, DLL: *dll, Args: programArgs, Procedures: selected, Capacity: *capacity, Timeout: *timeout}, out)
}
func main() {
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt)
	defer cancel()
	if err := run(ctx, os.Args[1:], os.Stdout, os.Stderr); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
