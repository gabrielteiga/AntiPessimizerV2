// Command collector runs the portable sampling pipeline using an explicit demo source.
package main

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"flag"
	"fmt"
	"github.com/gabrielteiga/AntiPessimizerV2/internal/service"
	"github.com/gabrielteiga/AntiPessimizerV2/internal/transport"
	"io"
	"os"
	"os/signal"
	"time"

	"github.com/gabrielteiga/AntiPessimizerV2/internal/aggregation"
	"github.com/gabrielteiga/AntiPessimizerV2/internal/history"
	"github.com/gabrielteiga/AntiPessimizerV2/internal/profiling"
	"github.com/gabrielteiga/AntiPessimizerV2/internal/sampling"
	"github.com/gabrielteiga/AntiPessimizerV2/internal/session"
	"github.com/gabrielteiga/AntiPessimizerV2/internal/stream"
)

func run(ctx context.Context, args []string, out, diagnostics io.Writer) error {
	flags := flag.NewFlagSet("collector", flag.ContinueOnError)
	flags.SetOutput(diagnostics)
	serve := flags.Bool("serve", false, "serve local profiling requests")
	address := flags.String("address", transport.DefaultAddress(), "local collector endpoint")
	demo := flags.Bool("demo", false, "use synthetic data; never attaches to a process")
	windows := flags.Int("windows", 8, "number of synthetic windows (1..10000)")
	samples := flags.Int("samples", 1000, "samples per synthetic window (0..1000000)")
	width := flags.Duration("window", 250*time.Millisecond, "window width")
	top := flags.Int("top", 20, "maximum published hotspots (1..4096)")
	if err := flags.Parse(args); err != nil {
		return err
	}
	if flags.NArg() != 0 {
		return fmt.Errorf("unexpected positional arguments")
	}
	if *serve {
		if *demo {
			return fmt.Errorf("start demo through viewer when using --serve")
		}
		listener, err := transport.Listen(*address)
		if err != nil {
			return err
		}
		fmt.Fprintln(diagnostics, "Collector listening:", *address)
		return service.Serve(ctx, listener, service.New())
	}
	if !*demo {
		return fmt.Errorf("Windows collection is not implemented yet; use --demo explicitly")
	}
	start := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	source, err := sampling.NewFake(start, *width, *windows, *samples)
	if err != nil {
		return err
	}
	id := make([]byte, 16)
	if _, err := rand.Read(id); err != nil {
		return err
	}
	agg, err := aggregation.New(aggregation.Config{SessionID: hex.EncodeToString(id), Target: profiling.Target{PID: 1, Start: start, Image: "ProfilerLab.exe (synthetic)"}, Source: "synthetic", Start: start, Width: *width, Capacity: profiling.MaxRows, TopN: *top, SourceLossKnown: true})
	if err != nil {
		return err
	}
	store, err := history.New(120, 100000, 30*time.Second)
	if err != nil {
		return err
	}
	if _, err := fmt.Fprintln(diagnostics, "Synthetic replay: timestamps and process/thread identities are test data, not Windows measurements."); err != nil {
		return err
	}
	// Logical publication time makes this fixture deterministic. It is not latency evidence.
	clock := func() time.Time { return agg.End() }
	return session.Run(ctx, source, agg, store, clock, func(s profiling.Snapshot) error { return stream.Write(out, s) })
}
func main() {
	// Keep the OS interrupt behavior for this replay CLI: an inherited blocking
	// stdout cannot be assumed to support Close/deadline cancellation. The library
	// session accepts contexts; a live Windows adapter will own its cleanup path.
	ctx := context.Background()
	// Demo retains immediate OS interrupt behavior for inherited blocked stdout.
	for _, arg := range os.Args[1:] {
		if arg == "--serve" || arg == "-serve" {
			var cancel context.CancelFunc
			ctx, cancel = signal.NotifyContext(ctx, os.Interrupt)
			defer cancel()
			break
		}
	}
	if err := run(ctx, os.Args[1:], os.Stdout, os.Stderr); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
