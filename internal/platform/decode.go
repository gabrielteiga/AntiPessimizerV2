package platform

import (
	"encoding/binary"
	"time"
	"unicode/utf16"
)

// KernelState is owned by the ETW callback. Lifecycle updates happen before
// emitting samples so queue pressure cannot drop thread/module invalidations.
type KernelState struct {
	PID     uint32
	Threads map[uint32]time.Time
	Modules []Module
}

// Feed decodes the documented classic kernel schemas: Thread v2/v3,
// Image v2 and PerfInfo SampledProfile v2. Other schemas are ignored.
func (s *KernelState) Feed(kind string, opcode, version uint8, at time.Time, b []byte) (Sample, bool) {
	u32 := binary.LittleEndian.Uint32
	u64 := binary.LittleEndian.Uint64
	switch kind {
	case "thread":
		if version < 2 || version > 3 || len(b) < 8 {
			return Sample{}, false
		}
		pid, tid := u32(b), u32(b[4:])
		if born, ok := s.Threads[tid]; ok && at.Before(born) {
			return Sample{}, false
		}
		if opcode == 1 || opcode == 3 {
			delete(s.Threads, tid)
			if pid == s.PID {
				s.Threads[tid] = at
			}
		}
		if opcode == 2 || opcode == 4 {
			delete(s.Threads, tid)
		}
	case "image":
		if version != 2 || len(b) < 56 || u32(b[16:]) != s.PID {
			return Sample{}, false
		}
		base, size := u64(b), u64(b[8:])
		if opcode != 10 && opcode != 2 && opcode != 3 && opcode != 4 {
			return Sample{}, false
		}
		for i := len(s.Modules) - 1; i >= 0; i-- {
			if s.Modules[i].Base == base {
				if at.Before(s.Modules[i].Loaded) {
					return Sample{}, false
				}
				s.Modules = append(s.Modules[:i], s.Modules[i+1:]...)
			}
		}
		if opcode == 2 || opcode == 4 {
			return Sample{}, false
		}
		if size == 0 || size > ^uint64(0)-base || len(s.Modules) >= 4096 {
			return Sample{}, false
		}
		var name []uint16
		for i := 56; i+1 < len(b); i += 2 {
			v := binary.LittleEndian.Uint16(b[i:])
			if v == 0 {
				break
			}
			name = append(name, v)
		}
		s.Modules = append(s.Modules, Module{Base: base, Size: size, Path: string(utf16.Decode(name)), Loaded: at})
	case "profile":
		if opcode != 46 || version != 2 || len(b) < 16 {
			return Sample{}, false
		}
		tid := u32(b[8:])
		born, ok := s.Threads[tid]
		if !ok || at.Before(born) {
			return Sample{}, false
		}
		sample := Sample{At: at, Thread: tid, IP: u64(b)}
		for _, m := range s.Modules {
			if sample.IP >= m.Base && sample.IP-m.Base < m.Size && !at.Before(m.Loaded) {
				sample.Module = m
				break
			}
		}
		return sample, true
	}
	return Sample{}, false
}
