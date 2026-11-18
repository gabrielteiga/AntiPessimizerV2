package native

import (
	"context"
	"encoding/binary"
	"errors"
	"io"
	"net"
	"testing"
	"time"
)

func TestClearWaitsForAppliedBarrierAndNeverReusesTimedOutAcknowledgement(t *testing.T) {
	c := NewControl()
	if err := c.claim(); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	left, right := net.Pipe()
	defer left.Close()
	defer right.Close()
	ack := make(chan error, 1)
	done := make(chan struct{})
	go func() { defer close(done); runCommands(ctx, left, c, ack, cancel) }()
	firstCtx, firstCancel := context.WithCancel(ctx)
	first := make(chan error, 1)
	go func() { first <- c.Clear(firstCtx) }()
	var command [1]byte
	if _, err := io.ReadFull(right, command[:]); err != nil || command[0] != 3 {
		t.Fatal(command, err)
	}
	select {
	case err := <-first:
		t.Fatal("Clear acknowledged before barrier", err)
	default:
	}
	firstCancel()
	if err := <-first; !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	// A later request may queue, but it cannot consume the old barrier's reply.
	second := make(chan error, 1)
	go func() { second <- c.Clear(ctx) }()
	ack <- nil
	if _, err := io.ReadFull(right, command[:]); err != nil {
		t.Fatal(err)
	}
	select {
	case err := <-second:
		t.Fatal("stale barrier satisfied later Clear", err)
	default:
	}
	ack <- nil
	if err := <-second; err != nil {
		t.Fatal(err)
	}
	cancel()
	<-done
	close(c.done)
	if err := c.Clear(context.Background()); !errors.Is(err, ErrControlClosed) {
		t.Fatal(err)
	}
	if err := c.claim(); err == nil {
		t.Fatal("control reused")
	}
}
func TestClearOnUnstartedCaptureAndCanceledCaller(t *testing.T) {
	c := NewControl()
	if err := c.Clear(context.Background()); err == nil {
		t.Fatal("unstarted capture accepted Clear")
	}
	if err := c.claim(); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Millisecond)
	defer cancel()
	if err := c.Clear(ctx); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatal(err)
	}
	close(c.done)
}

func TestNativeLifecycleCommandsAreSerializedWithoutClearControl(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	left, right := net.Pipe()
	defer left.Close()
	defer right.Close()
	exits := make(chan threadExit, 3)
	done := make(chan struct{})
	go func() { defer close(done); runCommands(ctx, left, nil, nil, cancel, exits) }()
	events := []threadExit{{7, 100}, {8, 120}, {7, 300}}
	for _, event := range events {
		exits <- event
	}
	close(exits)
	for _, event := range events {
		var command [13]byte
		if _, err := io.ReadFull(right, command[:]); err != nil {
			t.Fatal(err)
		}
		if command[0] != 4 || binary.LittleEndian.Uint32(command[1:]) != event.tid || binary.LittleEndian.Uint64(command[5:]) != event.tick {
			t.Fatal("thread-end commands interleaved", command)
		}
	}
	cancel()
	<-done
}
