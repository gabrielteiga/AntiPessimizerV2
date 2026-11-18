package native

import (
	"context"
	"encoding/binary"
	"errors"
	"net"
	"os"
	"sync/atomic"
	"time"
)

var ErrControlClosed = errors.New("instrumented capture is not active")

type threadExit struct {
	tid  uint32
	tick uint64
}

type clearRequest struct {
	ctx    context.Context
	result chan error
}

// Control belongs to one owned launch. A Clear reply acknowledges a validated,
// published native barrier, not merely a command written to a pipe.
type Control struct {
	claimed  atomic.Bool
	requests chan clearRequest
	done     chan struct{}
}

func NewControl() *Control {
	return &Control{requests: make(chan clearRequest), done: make(chan struct{})}
}
func (c *Control) claim() error {
	if c.requests == nil || c.done == nil {
		return errors.New("use NewControl for an owned capture")
	}
	if !c.claimed.CompareAndSwap(false, true) {
		return errors.New("instrumentation control cannot be reused")
	}
	return nil
}
func (c *Control) Clear(ctx context.Context) error {
	if !c.claimed.Load() {
		return errors.New("instrumented capture is not ready")
	}
	request := clearRequest{ctx: ctx, result: make(chan error, 1)}
	select {
	case c.requests <- request:
	case <-c.done:
		return ErrControlClosed
	case <-ctx.Done():
		return ctx.Err()
	}
	select {
	case err := <-request.result:
		return err
	case <-c.done:
		select {
		case err := <-request.result:
			return err
		default:
			return ErrControlClosed
		}
	case <-ctx.Done():
		return ctx.Err()
	}
}
func runCommands(ctx context.Context, conn net.Conn, control *Control, ack <-chan error, cancel context.CancelFunc, exited ...<-chan threadExit) {
	var exits <-chan threadExit
	if len(exited) > 0 {
		exits = exited[0]
	}
	var requests <-chan clearRequest
	if control != nil {
		requests = control.requests
	}
	for {
		var request clearRequest
		select {
		case <-ctx.Done():
			return
		case event, open := <-exits:
			if !open {
				exits = nil
				continue
			}
			var command [13]byte
			command[0] = 4
			binary.LittleEndian.PutUint32(command[1:], event.tid)
			binary.LittleEndian.PutUint64(command[5:], event.tick)
			conn.SetWriteDeadline(time.Now().Add(5 * time.Second))
			err := writeHookBytes(conn, command[:])
			conn.SetWriteDeadline(time.Time{})
			if err != nil {
				// Natural ExitProcess can close the duplex pipe after its final batch.
				// The reader verifies completion; a write deadline is a real control loss.
				if errors.Is(err, os.ErrDeadlineExceeded) {
					cancel()
				}
				return
			}
			continue
		case request = <-requests:
		}
		if err := request.ctx.Err(); err != nil {
			request.result <- err
			continue
		}
		deadline := time.Now().Add(5 * time.Second)
		if d, ok := request.ctx.Deadline(); ok && d.Before(deadline) {
			deadline = d
		}
		conn.SetWriteDeadline(deadline)
		err := writeHookBytes(conn, []byte{3})
		conn.SetWriteDeadline(time.Time{})
		if err != nil {
			request.result <- err
			cancel()
			return
		}
		// Even if the requester times out, finish this barrier before accepting the
		// next request. An old acknowledgement can never satisfy a newer Clear.
		timer := time.NewTimer(5 * time.Second)
		select {
		case err = <-ack:
			timer.Stop()
			request.result <- err
		case <-ctx.Done():
			timer.Stop()
			request.result <- ctx.Err()
			return
		case <-timer.C:
			request.result <- context.DeadlineExceeded
			cancel()
			return
		}
	}
}
