// Command viewer is a text consumer of snapshots; it has no access to collection APIs.
package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"github.com/gabrielteiga/AntiPessimizerV2/internal/desktop"
	"github.com/gabrielteiga/AntiPessimizerV2/internal/report"
	"github.com/gabrielteiga/AntiPessimizerV2/internal/service"
	"github.com/gabrielteiga/AntiPessimizerV2/internal/transport"
	"io"
	"os"
	"os/signal"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/gabrielteiga/AntiPessimizerV2/internal/stream"
)

func run(args []string, in io.Reader, out, diagnostics io.Writer) error {
	flags := flag.NewFlagSet("viewer", flag.ContinueOnError)
	flags.SetOutput(diagnostics)
	gui := flags.Bool("gui", false, "open native Windows desktop")
	instrumented := flags.Bool("instrumentation", false, "read explicit call/time metrics instead of CPU samples")
	address := flags.String("address", transport.DefaultAddress(), "local collector endpoint")
	op := flags.String("op", "", "processes, start, stop, status, latest, history, subscribe, instrument-catalog, instrument-start, instrument-history, instrument-subscribe")
	image := flags.String("image", "", "owned executable for instrument-catalog/start")
	catalogID := flags.String("catalog-id", "", "exact catalog_id returned by instrument-catalog")
	imageID := flags.String("image-id", "", "exact image_id returned by instrument-catalog")
	selection := flags.String("procedure-ids", "", "comma-separated catalog IDs to instrument (1..16)")
	catalogText := flags.String("symbol-filter", "", "catalog search text")
	catalogOffset := flags.Int("catalog-offset", 0, "catalog page offset")
	timeout := flags.Duration("timeout", time.Minute, "owned instrumented target timeout")
	var programArgs cliArgs
	flags.Var(&programArgs, "arg", "owned program argument (repeatable)")
	pid := flags.Uint("pid", 0, "target process ID")
	demo := flags.Bool("demo", false, "start synthetic service session")
	window := flags.Int("window-ms", 250, "collection window in milliseconds")
	seconds := flags.Int("seconds", 30, "history interval (1..120 seconds)")
	save := flags.String("save", "", "save retained snapshots to a new NDJSON file")
	replay := flags.String("replay", "", "read an NDJSON archive")
	follow := flags.String("follow", "", "follow an instrumentation NDJSON file in the native GUI")
	compare := flags.String("compare", "", "compare replay archive with another archive")
	unit := flags.String("unit", "", "exact Pascal unit name filter")
	module := flags.String("module", "", "exact module name filter")
	thread := flags.Uint64("thread", 0, "thread ID filter; 0 means all")
	if err := flags.Parse(args); err != nil {
		return err
	}
	if flags.NArg() != 0 || *thread > 1<<32-1 || *pid > 1<<32-1 {
		return errors.New("invalid arguments/thread ID")
	}
	if *instrumented && (*op != "" || *save != "" || *compare != "" || *demo || *pid != 0) {
		return errors.New("instrumentation replay cannot be combined with sampling control, save or compare")
	}
	if *follow != "" && (!*gui || !*instrumented || *replay != "") {
		return errors.New("--follow requires --gui --instrumentation and cannot be combined with --replay")
	}
	if *gui {
		if *follow != "" {
			return desktop.RunFollow(*address, *follow)
		}
		if *replay != "" {
			return desktop.RunArchive(*address, *replay)
		}
		if *instrumented {
			return desktop.RunInstrumentation(*address)
		}
		return desktop.Run(*address)
	}
	if *save != "" {
		return saveHistory(*address, *save, *seconds)
	}
	if *compare != "" {
		if *replay == "" {
			return errors.New("--compare requires --replay")
		}
		a, err := os.Open(*replay)
		if err != nil {
			return err
		}
		defer a.Close()
		b, err := os.Open(*compare)
		if err != nil {
			return err
		}
		defer b.Close()
		left, err := report.Read(a)
		if err != nil {
			return err
		}
		right, err := report.Read(b)
		if err != nil {
			return err
		}
		result, err := report.Compare(left, right)
		if err != nil {
			return err
		}
		return json.NewEncoder(out).Encode(result)
	}
	if *op != "" {
		if *op == "subscribe" {
			ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt)
			defer cancel()
			return subscribe(ctx, *address, out)
		}
		if *op == "instrument-subscribe" {
			ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt)
			defer cancel()
			return subscribeInstrumented(ctx, *address, out)
		}
		now := time.Now()
		request := service.Request{Op: *op, Start: service.StartOptions{PID: uint32(*pid), Demo: *demo, WindowMS: *window}, From: now.Add(-time.Duration(*seconds) * time.Second), To: now, Module: *module, Thread: uint32(*thread)}
		if *op == "instrument-catalog" {
			request.Catalog = service.CatalogQuery{Image: *image, Text: *catalogText, Unit: *unit, Offset: *catalogOffset}
		}
		if *op == "instrument-start" {
			ids, err := parseProcedureIDs(*selection)
			if err != nil {
				return err
			}
			if *timeout < time.Millisecond || *timeout > 24*time.Hour {
				return errors.New("invalid instrumented timeout")
			}
			path, err := filepath.Abs(*image)
			if err != nil {
				return err
			}
			request.Instrument = service.InstrumentOptions{Image: path, ImageID: *imageID, CatalogID: *catalogID, Procedures: ids, Args: programArgs, TimeoutMS: timeout.Milliseconds()}
		}
		return control(*address, request, out)
	}
	if *replay != "" {
		f, err := os.Open(*replay)
		if err != nil {
			return err
		}
		defer f.Close()
		in = f
	}
	if *instrumented {
		return viewInstrumentation(in, out, report.Filter{Unit: *unit, Module: *module, Thread: uint32(*thread)})
	}
	reader := stream.NewReader(in)
	var previous uint64
	for {
		s, err := reader.Next()
		if errors.Is(err, io.EOF) {
			if previous == 0 {
				return errors.New("no snapshots received")
			}
			return nil
		}
		if err != nil {
			return err
		}
		gap := uint64(0)
		if previous > 0 {
			gap = s.Sequence - previous - 1
		}
		previous = s.Sequence
		if _, err := fmt.Fprintf(out, "snapshot=%d source=%s window=[%s,%s) accepted=%d dropped=%d late=%d omitted_updates=%d\n", s.Sequence, s.Source, s.WindowStart.Format("15:04:05.000"), s.WindowEnd.Format("15:04:05.000"), s.Quality.Accepted, s.Quality.Dropped, s.Quality.Late, gap); err != nil {
			return err
		}
		for _, h := range s.Hotspots {
			if *unit != "" && h.Unit != *unit || *module != "" && h.Module != *module || *thread != 0 && uint64(h.ThreadID) != *thread {
				continue
			}
			name := h.Procedure
			if h.Unit != "" {
				name = h.Unit + "::" + name
			}
			if name == "" {
				name = h.RVA + " (" + h.Reason + ")"
			}
			if _, err := fmt.Fprintf(out, "  %8d samples  tid=%d  %s!%s\n", h.Samples, h.ThreadID, h.Module, name); err != nil {
				return err
			}
		}
		if s.OtherSamples > 0 {
			if _, err := fmt.Fprintf(out, "  %d accepted samples outside published top-N\n", s.OtherSamples); err != nil {
				return err
			}
		}
	}
}
func main() {
	if err := run(os.Args[1:], os.Stdin, os.Stdout, os.Stderr); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

type cliArgs []string

func (a *cliArgs) String() string         { return strings.Join(*a, ", ") }
func (a *cliArgs) Set(value string) error { *a = append(*a, value); return nil }
func parseProcedureIDs(selection string) ([]uint32, error) {
	var result []uint32
	seen := map[uint32]bool{}
	for _, part := range strings.Split(selection, ",") {
		id, err := strconv.ParseUint(strings.TrimSpace(part), 10, 32)
		if err != nil || id == 0 || seen[uint32(id)] || len(result) >= 16 {
			return nil, errors.New("select 1..16 distinct catalog IDs")
		}
		result = append(result, uint32(id))
		seen[uint32(id)] = true
	}
	return result, nil
}
