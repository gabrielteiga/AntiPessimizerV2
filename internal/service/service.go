// Package service owns profiling lifecycle; clients never call platform APIs.
package service

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/gabrielteiga/AntiPessimizerV2/internal/aggregation"
	"github.com/gabrielteiga/AntiPessimizerV2/internal/history"
	"github.com/gabrielteiga/AntiPessimizerV2/internal/live"
	"github.com/gabrielteiga/AntiPessimizerV2/internal/platform"
	"github.com/gabrielteiga/AntiPessimizerV2/internal/profiling"
	"github.com/gabrielteiga/AntiPessimizerV2/internal/sampling"
	"github.com/gabrielteiga/AntiPessimizerV2/internal/session"
)

type Status struct {
	State     string           `json:"state"`
	SessionID string           `json:"session_id,omitempty"`
	Error     string           `json:"error,omitempty"`
	Target    profiling.Target `json:"target"`
	Sequence  uint64           `json:"sequence"`
}
type StartOptions struct {
	PID      uint32 `json:"pid"`
	Demo     bool   `json:"demo"`
	WindowMS int    `json:"window_ms"`
	Top      int    `json:"top"`
}
type Controller struct {
	mu          sync.Mutex
	state       Status
	cancel      context.CancelFunc
	done        chan struct{}
	latest      *profiling.Snapshot
	store       *history.Store
	subscribers map[chan profiling.Snapshot]struct{}
	Inspect     func(uint32) (platform.Process, error)
	Capture     live.Capture
}

func New() *Controller {
	return &Controller{state: Status{State: "idle"}, subscribers: map[chan profiling.Snapshot]struct{}{}, Inspect: platform.Inspect, Capture: platform.Capture}
}
func (c *Controller) Status() Status { c.mu.Lock(); defer c.mu.Unlock(); return c.state }
func (c *Controller) Latest() *profiling.Snapshot {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.latest == nil {
		return nil
	}
	v := c.latest.Clone()
	return &v
}
func (c *Controller) History(from, to time.Time, module string, thread uint32) ([]profiling.Snapshot, error) {
	c.mu.Lock()
	store := c.store
	c.mu.Unlock()
	if store == nil {
		return []profiling.Snapshot{}, nil
	}
	return store.Query(from, to, module, thread)
}
func (c *Controller) Subscribe() (chan profiling.Snapshot, func()) {
	c.mu.Lock()
	defer c.mu.Unlock()
	ch := make(chan profiling.Snapshot, 1)
	c.subscribers[ch] = struct{}{}
	if c.latest != nil {
		ch <- c.latest.Clone()
	}
	return ch, func() { c.mu.Lock(); defer c.mu.Unlock(); delete(c.subscribers, ch) }
}
func (c *Controller) publish(v profiling.Snapshot) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if err := c.store.Append(v); err != nil {
		return err
	}
	v = v.Clone()
	c.latest = &v
	c.state.Sequence = v.Sequence
	c.state.State = "running"
	for ch := range c.subscribers {
		select {
		case <-ch:
		default:
		}
		ch <- v.Clone()
	}
	return nil
}
func (c *Controller) Start(parent context.Context, o StartOptions) error {
	if o.WindowMS == 0 {
		o.WindowMS = 250
	}
	if o.Top == 0 {
		o.Top = 200
	}
	if o.WindowMS < 10 || o.WindowMS > 10000 || o.Top < 1 || o.Top > profiling.MaxRows {
		return errors.New("invalid window/top")
	}
	if o.Demo && o.PID != 0 || !o.Demo && o.PID == 0 {
		return errors.New("choose demo or a nonzero PID")
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.cancel != nil {
		return errors.New("a session is already active")
	}
	var target profiling.Target
	now := time.Now().UTC()
	if o.Demo {
		target = profiling.Target{PID: 1, Start: now, Image: "ProfilerLab.exe (synthetic)"}
	} else {
		p, err := c.Inspect(o.PID)
		if err != nil {
			return err
		}
		target = profiling.Target{PID: p.PID, Start: p.Start, Image: p.Name}
	}
	var id [16]byte
	if _, err := rand.Read(id[:]); err != nil {
		return err
	}
	store, err := history.New(480, 100000, 120*time.Second)
	if err != nil {
		return err
	}
	c.store = store
	c.latest = nil
	c.state = Status{State: "starting", SessionID: hex.EncodeToString(id[:]), Target: target}
	ctx, cancel := context.WithCancel(parent)
	c.cancel = cancel
	c.done = make(chan struct{})
	done := c.done
	config := aggregation.Config{SessionID: c.state.SessionID, Target: target, Start: now, Width: time.Duration(o.WindowMS) * time.Millisecond, Capacity: profiling.MaxRows, TopN: o.Top, Source: "external"}
	if o.Demo {
		config.Source = "synthetic"
		config.SourceLossKnown = true
	}
	go func() {
		var err error
		if o.Demo {
			source, e := sampling.NewFake(now, config.Width, 10000, 1000)
			if e != nil {
				err = e
			} else {
				agg, e := aggregation.New(config)
				if e != nil {
					err = e
				} else {
					// Run uses a separate retention store because publish owns the service one.
					retention, _ := history.New(1, profiling.MaxRows, time.Hour)
					err = session.Run(ctx, source, agg, retention, func() time.Time { return agg.End() }, func(s profiling.Snapshot) error {
						timer := time.NewTimer(time.Until(s.WindowEnd))
						defer timer.Stop()
						select {
						case <-ctx.Done():
							return ctx.Err()
						case <-timer.C:
						}
						return c.publish(s)
					})
				}
			}
		} else {
			err = live.Run(ctx, config, c.Capture, c.publish)
		}
		c.mu.Lock()
		c.cancel = nil
		c.state.State = "stopped"
		if err != nil && !errors.Is(err, context.Canceled) {
			c.state.State = "error"
			c.state.Error = err.Error()
		}
		close(done)
		c.mu.Unlock()
	}()
	return nil
}
func (c *Controller) Stop() error {
	c.mu.Lock()
	cancel, done := c.cancel, c.done
	if cancel != nil {
		c.state.State = "stopping"
	}
	c.mu.Unlock()
	if cancel == nil {
		return nil
	}
	cancel()
	<-done
	return nil
}
func (c *Controller) String() string { return fmt.Sprint(c.Status()) }
