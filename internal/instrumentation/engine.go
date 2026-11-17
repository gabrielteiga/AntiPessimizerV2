// Package instrumentation accounts explicit procedure entry/exit events.
// It is not an ETW estimator and is not called from foreign native hooks.
// One event-draining owner must serialize access to an Engine.
package instrumentation

import (
	"errors"
	"fmt"
	"math"
	"math/bits"
	"sort"

	"github.com/gabrielteiga/AntiPessimizerV2/internal/profiling"
)

type Config struct {
	Frequency                     uint64
	MaxThreads, MaxDepth, MaxRows int
}
type Procedure struct {
	ID       uint32             `json:"id"`
	Location profiling.Location `json:"location"`
}
type key struct {
	thread   uint32
	instance uint64
	proc     uint32
}
type totals struct{ Calls, Unwound, Inclusive, Exclusive uint64 }
type frame struct {
	key               key
	start, children   uint64
	previousInclusive uint64
}
type thread struct {
	instance, last uint64
	stack          []frame
	depth          map[uint32]int
}

const MaxOpenCalls = 65536

type Engine struct {
	latest           uint64
	floor            uint64
	open             uint64
	config           Config
	procedures       map[uint32]profiling.Location
	threads          map[uint32]*thread
	rows             map[key]totals
	epoch, abandoned uint64
	incomplete       bool
}

type Row struct {
	ActiveFrames uint32 `json:"active_frame_count"`
	profiling.Location
	ThreadID           uint32  `json:"thread_id"`
	ThreadInstance     uint64  `json:"thread_instance_ticks,string"`
	ProcedureID        uint32  `json:"procedure_id"`
	Calls              uint64  `json:"call_count"`
	UnwoundCalls       uint64  `json:"unwound_call_count"`
	InclusiveTicks     uint64  `json:"inclusive_ticks,string"`
	ExclusiveTicks     uint64  `json:"exclusive_ticks,string"`
	InclusiveNS        uint64  `json:"inclusive_ns,string"`
	ExclusiveNS        uint64  `json:"exclusive_ns,string"`
	AverageInclusiveNS float64 `json:"average_inclusive_ns"`
	AverageExclusiveNS float64 `json:"average_exclusive_ns"`
}

// Snapshot contains cumulative completed calls since Clear. Recursive inclusive
// totals count the outermost interval for a procedure on each thread, matching
// CoreProfiler's saved previous-inclusive semantics. Open calls are not fabricated.
type Snapshot struct {
	Version        int    `json:"schema_version"`
	Mode           string `json:"collection_mode"`
	Frequency      uint64 `json:"clock_frequency_hz,string"`
	Epoch          uint64 `json:"clear_epoch"`
	OpenCalls      uint64 `json:"open_call_count"`
	AbandonedCalls uint64 `json:"abandoned_call_count"`
	Incomplete     bool   `json:"incomplete"`
	Rows           []Row  `json:"procedures"`
}

func New(c Config, procedures []Procedure) (*Engine, error) {
	if c.Frequency == 0 || c.Frequency > 1e12 || c.MaxThreads < 1 || c.MaxThreads > 4096 || c.MaxDepth < 1 || c.MaxDepth > 4096 || c.MaxRows < 1 || c.MaxRows > profiling.MaxRows || len(procedures) == 0 || len(procedures) > 65536 {
		return nil, errors.New("invalid instrumentation limits/clock/selection")
	}
	e := &Engine{config: c, procedures: map[uint32]profiling.Location{}, threads: map[uint32]*thread{}, rows: map[key]totals{}}
	seen := map[string]bool{}
	for _, p := range procedures {
		if p.ID == 0 || p.Location.Status != "resolved" {
			return nil, errors.New("selected procedure must have nonzero ID and resolved location")
		}
		if err := p.Location.Validate(); err != nil {
			return nil, err
		}
		identity := p.Location.ModuleID + ":" + p.Location.RVA
		if _, ok := e.procedures[p.ID]; ok || seen[identity] {
			return nil, errors.New("duplicate selected procedure")
		}
		seen[identity] = true
		e.procedures[p.ID] = p.Location
	}
	return e, nil
}
func (e *Engine) StartThread(id uint32, instance uint64) error {
	if id == 0 || instance < e.floor || e.threads[id] != nil || len(e.threads) >= e.config.MaxThreads {
		return errors.New("invalid, duplicate or excessive thread")
	}
	// Reusing a lifetime identity would silently merge different threads.
	for k := range e.rows {
		if k.thread == id && k.instance >= instance {
			return errors.New("thread instance must increase after reuse")
		}
	}
	e.threads[id] = &thread{instance: instance, last: instance, depth: map[uint32]int{}}
	e.observe(instance)
	return nil
}
func (e *Engine) Enter(id, proc uint32, tick uint64) error {
	t := e.threads[id]
	if t == nil || tick < t.last {
		return errors.New("unknown thread or backwards timestamp")
	}
	if _, ok := e.procedures[proc]; !ok {
		return errors.New("procedure not selected")
	}
	if len(t.stack) >= e.config.MaxDepth || e.open >= MaxOpenCalls {
		return errors.New("instrumentation stack limit exceeded")
	}
	k := key{id, t.instance, proc}
	if _, ok := e.rows[k]; !ok {
		if len(e.rows) >= e.config.MaxRows {
			return errors.New("instrumentation row limit exceeded")
		}
		e.rows[k] = totals{}
	}
	t.stack = append(t.stack, frame{key: k, start: tick, previousInclusive: e.rows[k].Inclusive})
	t.depth[proc]++
	e.open++
	t.last = tick
	e.observe(tick)
	return nil
}
func (e *Engine) Exit(id, proc uint32, tick uint64) error {
	t := e.threads[id]
	if t == nil || len(t.stack) == 0 || t.stack[len(t.stack)-1].key.proc != proc {
		return errors.New("exit does not match active procedure")
	}
	return e.closeFrames(t, len(t.stack)-1, tick, false)
}

// ExitUnwound closes exactly the top procedure reported by a native Windows
// cleanup handler. A handler for a different frame must not corrupt the stack.
func (e *Engine) ExitUnwound(id, proc uint32, tick uint64) error {
	t := e.threads[id]
	if t == nil || len(t.stack) == 0 || t.stack[len(t.stack)-1].key.proc != proc {
		return errors.New("unwind exit does not match active procedure")
	}
	return e.closeFrames(t, len(t.stack)-1, tick, true)
}

// Unwind closes instrumented frames above keepDepth at the same timestamp and
// records their calls as unwound. The native adapter must establish that depth
// from actual Windows unwind events; this method does not guess it from IPs.
func (e *Engine) Unwind(id uint32, keepDepth int, tick uint64) error {
	t := e.threads[id]
	if t == nil || keepDepth < 0 || keepDepth > len(t.stack) {
		return errors.New("invalid unwind depth")
	}
	return e.closeFrames(t, keepDepth, tick, true)
}
func (e *Engine) closeFrames(t *thread, keep int, tick uint64, unwound bool) error {
	if tick < t.last {
		return errors.New("backwards timestamp")
	}
	// Stage the affected stack/rows. Overflow or malformed timestamps must leave
	// the original state intact, including multi-frame exception unwinds.
	stack := append([]frame(nil), t.stack...)
	changed := map[key]totals{}
	for i := len(stack) - 1; i >= keep; i-- {
		f := stack[i]
		elapsed := tick - f.start
		if f.children > elapsed {
			return errors.New("child time exceeds frame duration")
		}
		v, ok := changed[f.key]
		if !ok {
			v = e.rows[f.key]
		}
		var err error
		v.Calls, err = profiling.Sum(v.Calls, 1)
		if err != nil {
			return err
		}
		if unwound {
			v.Unwound, err = profiling.Sum(v.Unwound, 1)
			if err != nil {
				return err
			}
		}
		v.Inclusive, err = profiling.Sum(f.previousInclusive, elapsed)
		if err != nil {
			return err
		}
		v.Exclusive, err = profiling.Sum(v.Exclusive, elapsed-f.children)
		if err != nil {
			return err
		}
		changed[f.key] = v
		if i > 0 {
			stack[i-1].children, err = profiling.Sum(stack[i-1].children, elapsed)
			if err != nil {
				return err
			}
		}
	}
	for k, v := range changed {
		e.rows[k] = v
	}
	for i := keep; i < len(t.stack); i++ {
		t.depth[t.stack[i].key.proc]--
	}
	e.open -= uint64(len(t.stack) - keep)
	t.stack = stack[:keep]
	t.last = tick
	e.observe(tick)
	return nil
}

// EndThread abandons open frames without inventing return events/durations.
func (e *Engine) EndThread(id uint32, tick uint64) error {
	t := e.threads[id]
	if t == nil || tick < t.last {
		return errors.New("invalid thread end")
	}
	n, err := profiling.Sum(e.abandoned, uint64(len(t.stack)))
	if err != nil {
		return err
	}
	e.abandoned = n
	e.open -= uint64(len(t.stack))
	if len(t.stack) > 0 {
		e.incomplete = true
	}
	delete(e.threads, id)
	e.observe(tick)
	return nil
}

// MarkLoss records that exactness was lost and discards active stacks. Completed
// metrics remain available, but no later event can bridge an unknown interval.
func (e *Engine) MarkLoss() error {
	n := e.abandoned
	for _, t := range e.threads {
		var err error
		n, err = profiling.Sum(n, uint64(len(t.stack)))
		if err != nil {
			return err
		}
	}
	e.abandoned = n
	e.open = 0
	e.incomplete = true
	clear(e.threads)
	return nil
}

// Clear follows the original reset policy: active frames restart at the clear
// tick, then count when they exit. Identity, selection and stack nesting remain.
func (e *Engine) Clear(tick uint64) error {
	if tick < e.latest {
		return errors.New("clear predates an observed event")
	}
	if e.epoch == math.MaxUint64 {
		return errors.New("clear epoch overflow")
	}
	for _, t := range e.threads {
		if tick < t.last {
			return errors.New("clear predates a thread event")
		}
	}
	clear(e.rows)
	for _, t := range e.threads {
		for i := range t.stack {
			t.stack[i].start = tick
			t.stack[i].children = 0
			t.stack[i].previousInclusive = 0
			e.rows[t.stack[i].key] = totals{}
		}
		t.last = tick
	}
	e.floor = tick
	e.observe(tick)
	e.epoch++
	e.abandoned = 0
	e.incomplete = false
	return nil
}
func nanoseconds(ticks, frequency uint64) (uint64, error) {
	if frequency == 0 {
		return 0, errors.New("zero clock frequency")
	}
	hi, lo := bits.Mul64(ticks, 1e9)
	if hi >= frequency {
		return 0, errors.New("nanosecond duration overflow")
	}
	n, _ := bits.Div64(hi, lo, frequency)
	return n, nil
}
func (e *Engine) Snapshot() (Snapshot, error) {
	s := Snapshot{Version: 1, Mode: "instrumentation", Frequency: e.config.Frequency, Epoch: e.epoch, AbandonedCalls: e.abandoned, Incomplete: e.incomplete, Rows: []Row{}}
	for _, t := range e.threads {
		s.OpenCalls += uint64(len(t.stack))
	}
	for k, v := range e.rows {
		if v.Calls == 0 {
			continue
		}
		inclusive, err := nanoseconds(v.Inclusive, e.config.Frequency)
		if err != nil {
			return Snapshot{}, fmt.Errorf("inclusive: %w", err)
		}
		exclusive, err := nanoseconds(v.Exclusive, e.config.Frequency)
		if err != nil {
			return Snapshot{}, fmt.Errorf("exclusive: %w", err)
		}
		active := uint32(0)
		if t := e.threads[k.thread]; t != nil && t.instance == k.instance {
			active = uint32(t.depth[k.proc])
		}
		s.Rows = append(s.Rows, Row{ActiveFrames: active, Location: e.procedures[k.proc], ThreadID: k.thread, ThreadInstance: k.instance, ProcedureID: k.proc, Calls: v.Calls, UnwoundCalls: v.Unwound, InclusiveTicks: v.Inclusive, ExclusiveTicks: v.Exclusive, InclusiveNS: inclusive, ExclusiveNS: exclusive, AverageInclusiveNS: float64(v.Inclusive) * 1e9 / float64(e.config.Frequency) / float64(v.Calls), AverageExclusiveNS: float64(v.Exclusive) * 1e9 / float64(e.config.Frequency) / float64(v.Calls)})
	}
	sort.Slice(s.Rows, func(i, j int) bool {
		a, b := s.Rows[i], s.Rows[j]
		if a.ExclusiveTicks != b.ExclusiveTicks {
			return a.ExclusiveTicks > b.ExclusiveTicks
		}
		if a.ThreadID != b.ThreadID {
			return a.ThreadID < b.ThreadID
		}
		if a.ThreadInstance != b.ThreadInstance {
			return a.ThreadInstance < b.ThreadInstance
		}
		return a.ProcedureID < b.ProcedureID
	})
	return s, nil
}

func (e *Engine) observe(tick uint64) {
	if tick > e.latest {
		e.latest = tick
	}
}
