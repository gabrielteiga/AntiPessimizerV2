package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"time"

	"github.com/gabrielteiga/AntiPessimizerV2/internal/service"
	"github.com/gabrielteiga/AntiPessimizerV2/internal/stream"
	"github.com/gabrielteiga/AntiPessimizerV2/internal/transport"
)

func subscribe(ctx context.Context, address string, out io.Writer) error {
	var previous uint64
	var session string
	delay := 100 * time.Millisecond
	for {
		if ctx.Err() != nil {
			return nil
		}
		conn, err := transport.Dial(ctx, address)
		if err != nil {
			select {
			case <-ctx.Done():
				return nil
			case <-time.After(delay):
			}
			if delay < 2*time.Second {
				delay *= 2
			}
			continue
		}
		stop := context.AfterFunc(ctx, func() { conn.Close() })
		err = transport.Write(conn, service.Request{Version: service.Version, Op: "subscribe"})
		for err == nil {
			var response service.Response
			err = transport.Read(conn, &response)
			if err != nil {
				break
			}
			if response.Version != service.Version || response.Error != "" {
				conn.Close()
				stop()
				return fmt.Errorf("collector protocol error: %s", response.Error)
			}
			if s := response.Snapshot; s != nil {
				if session != "" && s.SessionID != session {
					conn.Close()
					stop()
					return errors.New("session changed; start a new subscription/archive")
				}
				if s.SessionID == session && s.Sequence <= previous {
					continue
				}
				if err = stream.Write(out, *s); err != nil {
					conn.Close()
					stop()
					return err
				}
				session = s.SessionID
				previous = s.Sequence
				delay = 100 * time.Millisecond
			}
		}
		conn.Close()
		stop()
	}
}
func control(address string, q service.Request, out io.Writer) error {
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	r, err := service.Call(ctx, address, q)
	if err != nil {
		return err
	}
	return json.NewEncoder(out).Encode(r)
}
func saveHistory(address, path string, seconds int) error {
	if seconds < 1 || seconds > 120 {
		return errors.New("history seconds must be 1..120")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	now := time.Now()
	r, err := service.Call(ctx, address, service.Request{Op: "history", From: now.Add(-time.Duration(seconds) * time.Second), To: now})
	if err != nil {
		return err
	}
	if len(r.History) == 0 {
		return errors.New("no retained snapshots to save")
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if err != nil {
		return err
	}
	ok := false
	defer func() {
		f.Close()
		if !ok {
			os.Remove(path)
		}
	}()
	for _, s := range r.History {
		if err = stream.Write(f, s); err != nil {
			return err
		}
	}
	if err = f.Sync(); err != nil {
		return err
	}
	if err = f.Close(); err != nil {
		return err
	}
	ok = true
	return nil
}
