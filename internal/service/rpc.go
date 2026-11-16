package service

import (
	"context"
	"errors"
	"fmt"
	"net"
	"sync"
	"time"

	"github.com/gabrielteiga/AntiPessimizerV2/internal/platform"
	"github.com/gabrielteiga/AntiPessimizerV2/internal/profiling"
	"github.com/gabrielteiga/AntiPessimizerV2/internal/transport"
)

const Version = 1

type Request struct {
	Version  int          `json:"version"`
	Op       string       `json:"op"`
	Start    StartOptions `json:"start,omitempty"`
	From, To time.Time
	Module   string
	Thread   uint32
}
type Response struct {
	Done      bool                 `json:"done,omitempty"`
	Version   int                  `json:"version"`
	Error     string               `json:"error,omitempty"`
	Status    *Status              `json:"status,omitempty"`
	Snapshot  *profiling.Snapshot  `json:"snapshot,omitempty"`
	History   []profiling.Snapshot `json:"history,omitempty"`
	Processes []platform.Process   `json:"processes,omitempty"`
}

func Call(ctx context.Context, address string, q Request) (Response, error) {
	conn, err := transport.Dial(ctx, address)
	if err != nil {
		return Response{}, err
	}
	defer conn.Close()
	stop := context.AfterFunc(ctx, func() { conn.Close() })
	defer stop()
	conn.SetDeadline(time.Now().Add(15 * time.Second))
	q.Version = Version
	if err = transport.Write(conn, q); err != nil {
		return Response{}, err
	}
	var r Response
	if q.Op == "history" {
		var all []profiling.Snapshot
		for {
			if err = transport.Read(conn, &r); err != nil {
				return r, err
			}
			if r.Version != Version || r.Error != "" {
				return r, fmt.Errorf("history response: %s", r.Error)
			}
			all = append(all, r.History...)
			if len(all) > 480 {
				return r, errors.New("history exceeds retention limit")
			}
			if r.Done {
				r.History = all
				return r, nil
			}
		}
	}
	if err = transport.Read(conn, &r); err != nil {
		return r, err
	}
	if r.Version != Version {
		return r, errors.New("unsupported response version")
	}
	if r.Error != "" {
		return r, errors.New(r.Error)
	}
	return r, nil
}
func Serve(ctx context.Context, l net.Listener, c *Controller) error {
	ctx, cancel := context.WithCancel(ctx)
	var wg sync.WaitGroup
	defer func() { cancel(); l.Close(); c.Stop(); wg.Wait() }()
	stop := context.AfterFunc(ctx, func() { l.Close() })
	defer stop()
	slots := make(chan struct{}, 16)
	for {
		conn, err := l.Accept()
		if err != nil {
			if ctx.Err() != nil {
				return nil
			}
			return err
		}
		select {
		case slots <- struct{}{}:
		default:
			conn.Close()
			continue
		}
		wg.Add(1)
		go func() {
			defer wg.Done()
			defer func() { <-slots }()
			defer conn.Close()
			stop := context.AfterFunc(ctx, func() { conn.Close() })
			defer stop()
			handle(ctx, conn, c)
		}()
	}
}
func handle(ctx context.Context, conn net.Conn, c *Controller) {
	conn.SetDeadline(time.Now().Add(15 * time.Second))
	var q Request
	if err := transport.Read(conn, &q); err != nil {
		return
	}
	r := Response{Version: Version}
	var err error
	if q.Version != Version {
		r.Error = "unsupported protocol version"
		transport.Write(conn, r)
		return
	}
	switch q.Op {
	case "start":
		err = c.Start(ctx, q.Start)
	case "stop":
		err = c.Stop()
	case "status":
	case "latest":
		r.Snapshot = c.Latest()
	case "history":
		r.History, err = c.History(q.From, q.To, q.Module, q.Thread)
		if err == nil {
			for _, snapshot := range r.History {
				if transport.Write(conn, Response{Version: Version, History: []profiling.Snapshot{snapshot}}) != nil {
					return
				}
			}
			r.History = nil
			r.Done = true
		}
	case "processes":
		r.Processes, err = platform.Processes()
	case "subscribe":
		conn.SetReadDeadline(time.Time{})
		disconnected := make(chan struct{})
		go func() { var b [1]byte; conn.Read(b[:]); close(disconnected) }()
		defer func() { conn.Close(); <-disconnected }()
		ch, unsubscribe := c.Subscribe()
		defer unsubscribe()
		for {
			select {
			case <-ctx.Done():
				return
			case <-disconnected:
				return
			case s := <-ch:
				conn.SetWriteDeadline(time.Now().Add(5 * time.Second))
				if transport.Write(conn, Response{Version: Version, Snapshot: &s}) != nil {
					return
				}
			}
		}
	default:
		err = fmt.Errorf("unknown operation %q", q.Op)
	}
	if err != nil {
		r.Error = err.Error()
	}
	s := c.Status()
	r.Status = &s
	if err := transport.Write(conn, r); err != nil {
		return
	}
}
