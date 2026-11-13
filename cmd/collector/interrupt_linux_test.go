package main

import (
	"bufio"
	"bytes"
	"os"
	"os/exec"
	"testing"
	"time"
)

// Exercise the real CLI with a pipe whose consumer stops reading. Closing an
// inherited blocking stdout from another goroutine is not a portable escape.
func TestInterruptStalledOutput(t *testing.T) {
	if os.Getenv("PROFILER_INTERRUPT_TEST_HELPER") == "1" {
		os.Args = []string{"collector", "--demo", "--windows", "10000", "--samples", "1"}
		main()
		os.Exit(0)
	}
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()
	cmd := exec.Command(os.Args[0], "-test.run=^TestInterruptStalledOutput$")
	cmd.Env = append(os.Environ(), "PROFILER_INTERRUPT_TEST_HELPER=1")
	cmd.Stdout = w
	var diagnostic bytes.Buffer
	cmd.Stderr = &diagnostic
	if err := cmd.Start(); err != nil {
		w.Close()
		t.Fatal(err)
	}
	w.Close()
	defer func() { _ = cmd.Process.Kill() }()
	ready := make(chan error, 1)
	go func() { _, err := bufio.NewReader(r).ReadString('\n'); ready <- err }()
	select {
	case err := <-ready:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("no first snapshot")
	}
	// No further reads: the stream is much larger than the OS pipe capacity.
	time.Sleep(100 * time.Millisecond)
	if err := cmd.Process.Signal(os.Interrupt); err != nil {
		t.Fatal(err)
	}
	exited := make(chan error, 1)
	go func() { exited <- cmd.Wait() }()
	select {
	case err := <-exited:
		if err == nil {
			t.Fatal("expected interrupted process")
		}
	case <-time.After(5 * time.Second):
		t.Fatal("interrupt failed to release blocked output")
	}
}
