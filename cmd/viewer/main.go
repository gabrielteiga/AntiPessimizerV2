// Command viewer is a text consumer of snapshots; it has no access to collection APIs.
package main

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"os"

	"github.com/gabrielteiga/AntiPessimizerV2/internal/stream"
)

func run(args []string, in io.Reader, out, diagnostics io.Writer) error {
	flags := flag.NewFlagSet("viewer", flag.ContinueOnError)
	flags.SetOutput(diagnostics)
	module := flags.String("module", "", "exact module name filter")
	thread := flags.Uint64("thread", 0, "thread ID filter; 0 means all")
	if err := flags.Parse(args); err != nil {
		return err
	}
	if flags.NArg() != 0 || *thread > 1<<32-1 {
		return errors.New("invalid arguments/thread ID")
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
			if *module != "" && h.Module != *module || *thread != 0 && uint64(h.ThreadID) != *thread {
				continue
			}
			name := h.Procedure
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
