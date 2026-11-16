// Command instrument-replay validates entry/exit accounting before native hook integration.
package main

import (
	"context"
	"flag"
	"fmt"
	"io"
	"os"

	"github.com/gabrielteiga/AntiPessimizerV2/internal/instrumentation"
)

func run(ctx context.Context, args []string, in io.Reader, out, diagnostics io.Writer) error {
	flags := flag.NewFlagSet("instrument-replay", flag.ContinueOnError)
	flags.SetOutput(diagnostics)
	path := flags.String("input", "", "entry/exit NDJSON file; default stdin")
	threads := flags.Int("max-threads", 256, "maximum active threads")
	depth := flags.Int("max-depth", 1024, "maximum instrumented stack depth")
	rows := flags.Int("max-rows", 4096, "maximum retained thread/procedure rows")
	if err := flags.Parse(args); err != nil {
		return err
	}
	if flags.NArg() != 0 {
		return fmt.Errorf("unexpected positional arguments")
	}
	if *path != "" {
		f, err := os.Open(*path)
		if err != nil {
			return err
		}
		defer f.Close()
		in = f
	}
	return instrumentation.Replay(ctx, in, out, instrumentation.Config{MaxThreads: *threads, MaxDepth: *depth, MaxRows: *rows})
}
func main() {
	if err := run(context.Background(), os.Args[1:], os.Stdin, os.Stdout, os.Stderr); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
