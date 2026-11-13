// Package sampling contains the deterministic source used to exercise the pipeline.
package sampling

import (
	"context"
	"errors"
	"io"
	"time"

	"github.com/gabrielteiga/AntiPessimizerV2/internal/profiling"
	"github.com/gabrielteiga/AntiPessimizerV2/internal/symbols"
)

// Event is either a sample or a watermark. A watermark declares a completed window.
type Event struct {
	Sample    profiling.Sample
	Watermark time.Time
}
type Fake struct {
	start                           time.Time
	width                           time.Duration
	windows, samples, window, index int
	symbols                         *symbols.Index
}

func NewFake(start time.Time, width time.Duration, windows, samples int) (*Fake, error) {
	if start.IsZero() || width <= 0 || width > time.Hour || windows < 1 || windows > 10000 || samples < 0 || samples > 1000000 {
		return nil, errors.New("invalid synthetic source configuration")
	}
	index, err := symbols.New("synthetic-lab-v1", "ProfilerLab.exe", 0x5000, []symbols.Procedure{{Name: "LabWorkload.CPUHotspotA", RVA: 0x1000, Size: 0x100}, {Name: "LabWorkload.CPUHotspotB", RVA: 0x2000, Size: 0x100}})
	if err != nil {
		return nil, err
	}
	return &Fake{start: start.UTC(), width: width, windows: windows, samples: samples, symbols: index}, nil
}
func (f *Fake) Next(ctx context.Context) (Event, error) {
	if err := ctx.Err(); err != nil {
		return Event{}, err
	}
	if f.window == f.windows {
		return Event{}, io.EOF
	}
	start := f.start.Add(time.Duration(f.window) * f.width)
	if f.index == f.samples {
		f.index = 0
		f.window++
		return Event{Watermark: start.Add(f.width)}, nil
	}
	rva := uint64(0x1010)
	thread := uint32(101)
	if f.index%10 >= 7 {
		rva = 0x2010
		thread = 102
	}
	if f.window%2 == 1 {
		if thread == 101 {
			rva = 0x2010
			thread = 102
		} else {
			rva = 0x1010
			thread = 101
		}
	}
	if f.index%10 == 9 {
		rva = 0x3000
		thread = 103
	}
	loc, _ := f.symbols.Resolve(0x140000000, 0x140000000+rva)
	at := start.Add(time.Duration(int64(f.index) * int64(f.width) / int64(f.samples)))
	f.index++
	return Event{Sample: profiling.Sample{At: at, ThreadID: thread, Location: loc}}, nil
}
