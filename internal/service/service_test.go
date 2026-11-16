package service

import (
	"context"
	"fmt"
	"github.com/gabrielteiga/AntiPessimizerV2/internal/transport"
	"path/filepath"
	"runtime"
	"testing"
	"time"
)

func TestLifecycleAndCoalescing(t *testing.T) {
	c := New()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	defer c.Stop()
	slow, unsubscribe := c.Subscribe()
	defer unsubscribe()
	if err := c.Start(ctx, StartOptions{Demo: true, WindowMS: 10}); err != nil {
		t.Fatal(err)
	}
	if err := c.Start(ctx, StartOptions{Demo: true}); err == nil {
		t.Fatal("concurrent start")
	}
	deadline := time.After(2 * time.Second)
	for c.Status().Sequence < 3 {
		select {
		case <-deadline:
			t.Fatal("no snapshots")
		case <-time.After(5 * time.Millisecond):
		}
	}
	if len(slow) != 1 {
		t.Fatal("unbounded queue")
	}
	if err := c.Stop(); err != nil {
		t.Fatal(err)
	}
	if c.Status().State != "stopped" {
		t.Fatal(c.Status())
	}
	old := c.Status().SessionID
	if err := c.Start(ctx, StartOptions{Demo: true}); err != nil {
		t.Fatal(err)
	}
	if c.Status().SessionID == old {
		t.Fatal("session reused")
	}
}
func TestRPC(t *testing.T) {
	path := filepath.Join(t.TempDir(), "collector.sock")
	if runtime.GOOS == "windows" {
		path = fmt.Sprintf(`\\.\pipe\antipessimizer-test-%d`, time.Now().UnixNano())
	}
	l, err := transport.Listen(path)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	c := New()
	done := make(chan error, 1)
	go func() { done <- Serve(ctx, l, c) }()
	defer func() {
		cancel()
		if err := <-done; err != nil {
			t.Error(err)
		}
	}()
	if _, err = Call(ctx, path, Request{Op: "start", Start: StartOptions{Demo: true, WindowMS: 10}}); err != nil {
		t.Fatal(err)
	}
	if _, err = Call(ctx, path, Request{Op: "stop"}); err != nil {
		t.Fatal(err)
	}
	r, err := Call(ctx, path, Request{Op: "status"})
	if err != nil || r.Status.State != "stopped" {
		t.Fatal(r, err)
	}
	if _, err = Call(ctx, path, Request{Op: "unknown"}); err == nil {
		t.Fatal("unknown operation accepted")
	}
}
