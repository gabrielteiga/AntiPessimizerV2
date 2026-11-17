// Package native consumes bounded events from the native Win64 adapter. It does
// not infer completed calls after an event loss. Startup hook installation is
// limited to an explicitly cooperating host and checked native leaf plans.
package native

import (
	"encoding/binary"
	"errors"
	"fmt"
	"io"

	"github.com/gabrielteiga/AntiPessimizerV2/internal/instrumentation"
)

var ErrLoss = errors.New("native instrumentation events lost")

type Procedure struct{ ID, RVA uint32 }
type Header struct {
	PID, ImageSize       uint32
	Frequency, StartTick uint64
	Procedures           []Procedure
}
type Reader struct {
	Header                        Header
	in                            io.Reader
	selected                      map[uint32]bool
	pending                       []byte
	sequence, latest, lost, final uint64
	failed                        error
}

func NewReader(in io.Reader, expectedPID uint32) (*Reader, error) {
	var h [48]byte
	if _, err := io.ReadFull(in, h[:]); err != nil {
		return nil, fmt.Errorf("native hello: %w", err)
	}
	u32 := binary.LittleEndian.Uint32
	u64 := binary.LittleEndian.Uint64
	count := u32(h[28:])
	header := Header{PID: u32(h[24:]), ImageSize: u32(h[32:]), Frequency: u64(h[16:]), StartTick: u64(h[40:])}
	if string(h[:8]) != "APNAT01\x00" || u32(h[8:]) != 1 || u32(h[12:]) != 24 || expectedPID == 0 || header.PID != expectedPID || count == 0 || count > 64 || u32(h[36:]) != 0 || header.Frequency == 0 || header.Frequency > 1e12 || header.StartTick == 0 || header.ImageSize == 0 {
		return nil, errors.New("invalid native identity, clock or wire layout")
	}
	r := &Reader{Header: header, in: in, selected: map[uint32]bool{}, latest: header.StartTick}
	seen := map[uint32]bool{}
	for i := uint32(0); i < count; i++ {
		var p [8]byte
		if _, err := io.ReadFull(in, p[:]); err != nil {
			return nil, fmt.Errorf("native catalog: %w", err)
		}
		id, rva := u32(p[:]), u32(p[4:])
		if id == 0 || rva == 0 || rva >= header.ImageSize || r.selected[id] || seen[rva] {
			return nil, errors.New("invalid or duplicate native procedure")
		}
		r.selected[id], seen[rva] = true, true
		r.Header.Procedures = append(r.Header.Procedures, Procedure{id, rva})
	}
	return r, nil
}
func (r *Reader) Lost() uint64       { return r.lost }
func (r *Reader) FinalTick() uint64  { return r.final }
func (r *Reader) LatestTick() uint64 { return r.latest }
func (r *Reader) Next() (instrumentation.Event, error) {
	if r.failed != nil {
		return instrumentation.Event{}, r.failed
	}
	e, err := r.next()
	if err != nil {
		r.failed = err
	}
	return e, err
}
func (r *Reader) next() (instrumentation.Event, error) {
	if len(r.pending) == 0 {
		var h [32]byte
		if _, err := io.ReadFull(r.in, h[:]); err != nil {
			if err == io.EOF {
				err = io.ErrUnexpectedEOF
			}
			return instrumentation.Event{}, fmt.Errorf("native stream ended without completion: %w", err)
		}
		u32 := binary.LittleEndian.Uint32
		u64 := binary.LittleEndian.Uint64
		count, sequence, lost, final := u32(h[4:]), u64(h[8:]), u64(h[16:]), u64(h[24:])
		if u32(h[:]) != 0x31425041 || r.sequence == ^uint64(0) || sequence != r.sequence+1 || count > 256 {
			return instrumentation.Event{}, errors.New("invalid native batch or sequence")
		}
		r.sequence = sequence
		if lost != 0 {
			r.lost = lost
			return instrumentation.Event{}, fmt.Errorf("%w: %d", ErrLoss, lost)
		}
		if final != 0 {
			if count != 0 || final < r.latest {
				return instrumentation.Event{}, errors.New("invalid native final timestamp/count")
			}
			var extra [1]byte
			if _, err := io.ReadFull(r.in, extra[:]); err != io.EOF {
				return instrumentation.Event{}, errors.New("data or read failure after native completion")
			}
			r.final = final
			return instrumentation.Event{}, io.EOF
		}
		if count == 0 {
			return instrumentation.Event{}, errors.New("empty native batch")
		}
		r.pending = make([]byte, int(count)*24)
		if _, err := io.ReadFull(r.in, r.pending); err != nil {
			if err == io.EOF {
				err = io.ErrUnexpectedEOF
			}
			return instrumentation.Event{}, fmt.Errorf("native events: %w", err)
		}
	}
	p := r.pending[:24]
	r.pending = r.pending[24:]
	kind := binary.LittleEndian.Uint32(p)
	e := instrumentation.Event{Thread: binary.LittleEndian.Uint32(p[4:]), Procedure: binary.LittleEndian.Uint32(p[8:]), KeepDepth: int(binary.LittleEndian.Uint32(p[12:])), Tick: binary.LittleEndian.Uint64(p[16:])}
	if e.Thread == 0 || e.Tick < r.Header.StartTick {
		return e, errors.New("invalid native event identity/time")
	}
	switch kind {
	case 1, 4:
		if e.Procedure != 0 || e.KeepDepth != 0 {
			return e, errors.New("invalid native lifecycle fields")
		}
		if kind == 1 {
			e.Type = "thread_start"
		} else {
			e.Type = "thread_end"
		}
	case 2, 3:
		if !r.selected[e.Procedure] || e.KeepDepth != 0 {
			return e, errors.New("unselected native procedure or invalid call fields")
		}
		if kind == 2 {
			e.Type = "enter"
		} else {
			e.Type = "exit"
		}
	case 5:
		if e.Procedure != 0 || e.KeepDepth > 4096 {
			return e, errors.New("invalid native unwind")
		}
		e.Type = "unwind"
	default:
		return e, errors.New("unsupported native event")
	}
	if e.Tick > r.latest {
		r.latest = e.Tick
	}
	return e, nil
}
