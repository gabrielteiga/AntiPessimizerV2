package platform

import (
	"context"
	"os"
	"os/exec"
	"runtime"
	"sync/atomic"
	"testing"
	"time"
)

func TestETWControlledWorkload(t *testing.T) {
	if os.Getenv("AP_ETW_HELPER") == "1" {
		deadline := time.Now().Add(12 * time.Second)
		var v uint64 = 1
		for time.Now().Before(deadline) {
			for i := 0; i < 100000; i++ {
				v = v*6364136223846793005 + 1
			}
			runtime.KeepAlive(v)
		}
		return
	}
	if os.Getenv("AP_ETW_INTEGRATION") != "1" {
		t.Skip("enabled by Windows integration CI")
	}
	cmd := exec.Command(os.Args[0], "-test.run=^TestETWControlledWorkload$")
	cmd.Env = append(os.Environ(), "AP_ETW_HELPER=1")
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	defer func() { cmd.Process.Kill(); cmd.Wait() }()
	pid := uint32(cmd.Process.Pid)
	p, err := Inspect(pid)
	if err != nil {
		t.Fatal(err)
	}
	if p.PID != pid || p.Start.IsZero() {
		t.Fatal(p)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	var count atomic.Uint64
	err = Capture(ctx, pid, func(s Sample) {
		if s.Thread != 0 && s.IP != 0 {
			count.Add(1)
		}
	})
	if err != nil && ctx.Err() == nil {
		t.Fatal(err)
	}
	if count.Load() == 0 {
		t.Fatal("no sampled instruction pointers from controlled workload", err)
	}
	t.Logf("captured %d target samples", count.Load())
}
