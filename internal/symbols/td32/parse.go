// Package td32 reads the procedure subset of Borland FB09/FB0A debug data.
// Format reference: AntiPessimizer ab67a710, vendored JclTD32.pas.
// This implementation uses checked slices rather than mapped Pascal records.
package td32

import (
	"encoding/binary"
	"errors"
	"fmt"
	"unicode/utf8"
)

var ErrNoSymbols = errors.New("TD32 symbols absent")
var ErrInvalid = errors.New("invalid TD32 data")

// Procedure retains segmented addresses; converting them to RVA requires an
// explicit segment mapping from the matching image, never a fixed 0x1000 bias.
type Procedure struct {
	Name         string
	Segment      uint16
	Offset, Size uint32
	Unit         string
}

type entry struct {
	kind   uint16
	module uint16
	data   []byte
}

func u16(b []byte) uint16 { return binary.LittleEndian.Uint16(b) }
func u32(b []byte) uint32 { return binary.LittleEndian.Uint32(b) }
func signature(b []byte) bool {
	return len(b) >= 4 && (string(b[:4]) == "FB09" || string(b[:4]) == "FB0A")
}
func region(b []byte, off, size uint64) ([]byte, error) {
	if off > uint64(len(b)) || size > uint64(len(b))-off {
		return nil, ErrInvalid
	}
	return b[off : off+size], nil
}

// Parse accepts a complete .tds file or a debug-directory payload, including
// its trailer. Prefix bytes are allowed, as the trailer identifies the base.
// Unknown subsections and record kinds are skipped after validating framing.
// Errors return no partial symbol table. Input is limited to 256 MiB.
func Parse(data []byte) ([]Procedure, error) {
	if len(data) > 256<<20 {
		return nil, fmt.Errorf("%w: input exceeds 256 MiB", ErrInvalid)
	}
	if len(data) < 8 || !signature(data[len(data)-8:]) {
		return nil, ErrNoSymbols
	}
	tail := data[len(data)-8:]
	size := uint64(u32(tail[4:]))
	if size < 16 || size > uint64(len(data)) {
		return nil, ErrInvalid
	}
	b := data[uint64(len(data))-size : len(data)-8]
	if !signature(b) || string(b[:4]) != string(tail[:4]) {
		return nil, ErrInvalid
	}
	var entries []entry
	var payloadBytes uint64
	seen := map[uint32]bool{}
	off := u32(b[4:])
	for {
		if off < 8 || seen[off] {
			return nil, ErrInvalid
		}
		seen[off] = true
		h, err := region(b, uint64(off), 16)
		if err != nil {
			return nil, err
		}
		hs, es, count := uint64(u16(h)), uint64(u16(h[2:])), uint64(u32(h[4:]))
		if hs != 16 || es != 12 {
			return nil, ErrInvalid
		}
		table, err := region(b, uint64(off)+hs, count*es)
		if err != nil {
			return nil, err
		}
		for len(table) > 0 {
			e := table[:12]
			table = table[12:]
			payload, err := region(b, uint64(u32(e[4:])), uint64(u32(e[8:])))
			if err != nil {
				return nil, err
			}
			// Bound retained entries even when hostile directories repeatedly alias data.
			if len(entries) >= 1<<20 {
				return nil, ErrInvalid
			}
			payloadBytes += uint64(len(payload))
			if payloadBytes > 256<<20 {
				return nil, ErrInvalid
			}
			entries = append(entries, entry{kind: u16(e), module: u16(e[2:]), data: payload})
		}
		off = u32(h[8:])
		if off == 0 {
			break
		}
	}
	names := []string{""}
	for _, e := range entries {
		if e.kind == 0x130 {
			if len(e.data) < 4 {
				return nil, ErrInvalid
			}
			count := uint64(u32(e.data))
			rest := e.data[4:]
			if count > uint64(len(rest))/2 {
				return nil, ErrInvalid
			}
			for i := uint64(0); i < count; i++ {
				if len(rest) < 2 {
					return nil, ErrInvalid
				}
				n := int(rest[0])
				rest = rest[1:]
				for n < len(rest) && rest[n] != 0 {
					n += 256
				}
				if n >= len(rest) || !utf8.Valid(rest[:n]) {
					return nil, ErrInvalid
				}
				names = append(names, string(rest[:n]))
				rest = rest[n+1:]
				if len(names) > 1<<20 {
					return nil, ErrInvalid
				}
			}
		}
	}
	modules, err := parseModules(entries, names)
	if err != nil {
		return nil, err
	}
	var result []Procedure
	for _, e := range entries {
		if e.kind == 0x125 {
			if len(e.data) < 4 {
				return nil, ErrInvalid
			}
			rest := e.data[4:]
			for len(rest) > 0 {
				if len(rest) < 4 {
					return nil, ErrInvalid
				}
				size := int(u16(rest)) + 2
				if size < 4 || size > len(rest) {
					return nil, ErrInvalid
				}
				r := rest[:size]
				rest = rest[size:]
				if u16(r[2:]) != 0x204 && u16(r[2:]) != 0x205 {
					continue
				}
				if len(r) < 44 {
					return nil, ErrInvalid
				}
				idx := uint64(u32(r[40:]))
				if idx == 0 || idx >= uint64(len(names)) || names[idx] == "" {
					return nil, ErrInvalid
				}
				p := Procedure{Name: names[idx], Segment: u16(r[32:]), Offset: u32(r[28:]), Size: u32(r[16:])}
				if p.Segment == 0 || p.Size == 0 || uint64(p.Offset)+uint64(p.Size) > 1<<32 {
					return nil, ErrInvalid
				}
				if len(result) >= 1<<20 {
					return nil, ErrInvalid
				}
				if m, ok := modules[e.module]; ok {
					p.Unit = m.unitFor(p)
				}
				result = append(result, p)
			}
		}
	}
	return result, nil
}
