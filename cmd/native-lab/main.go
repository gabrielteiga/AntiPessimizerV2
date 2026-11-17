// Command native-lab measures a deliberately cooperating native workload.
// It does not attach to or instrument an unmodified Delphi executable.
package main

import (
	"context"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"time"

	"github.com/gabrielteiga/AntiPessimizerV2/internal/instrumentation/native"
)

func run(ctx context.Context, args []string, out, diagnostics io.Writer) error {
	flags := flag.NewFlagSet("native-lab", flag.ContinueOnError)
	flags.SetOutput(diagnostics)
	executable, err := os.Executable()
	if err != nil {
		return err
	}
	workload := flags.String("workload", filepath.Join(filepath.Dir(executable), "APNativeLab.exe"), "controlled cooperating APNativeLab.exe (not an arbitrary Delphi application)")
	threads := flags.Int("threads", 3, "controlled worker threads (1..16)")
	iterations := flags.Int("iterations", 10, "calls per worker (1..100000)")
	capacity := flags.Int("capacity", 1024, "per-thread native event capacity (1..1024)")
	timeout := flags.Duration("timeout", 15*time.Second, "owned capture timeout (up to 1m)")
	if err := flags.Parse(args); err != nil {
		return err
	}
	if flags.NArg() != 0 {
		return fmt.Errorf("unexpected positional arguments")
	}
	return native.RunLab(ctx, native.LabOptions{Workload: *workload, Threads: *threads, Iterations: *iterations, Capacity: *capacity, Timeout: *timeout}, out)
}
func main() {
	if err := run(context.Background(), os.Args[1:], os.Stdout, os.Stderr); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
