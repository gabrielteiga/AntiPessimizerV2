package x64

import (
	"encoding/binary"
	"fmt"
	"strings"

	"golang.org/x/arch/x86/x86asm"
)

const scopeDescriptorSize = 56

// cSpecificHandler recognizes the standard import thunk and exact IAT slot,
// not a lookalike scope table following an unknown language handler. The CRT
// name is restricted because the imported symbol defines the table's ABI.
func (p *PEImage) cSpecificHandler(rva uint32) bool {
	code, ok := p.mapped(rva, 6, true)
	if !ok || code[0] != 0xff || code[1] != 0x25 {
		return false
	}
	slot := int64(rva) + 6 + int64(int32(binary.LittleEndian.Uint32(code[2:])))
	if slot < 0 || slot > int64(^uint32(0)) {
		return false
	}
	nt := binary.LittleEndian.Uint32(p.data[60:])
	h := p.data[uint64(nt)+24:]
	if binary.LittleEndian.Uint32(h[108:]) < 2 {
		return false
	}
	directory, size := binary.LittleEndian.Uint32(h[120:]), binary.LittleEndian.Uint32(h[124:])
	if size < 40 || size > 4096*20 {
		return false
	}
	table, ok := p.mapped(directory, size, false)
	if !ok {
		return false
	}
	found, terminated := false, false
	for at := 0; at+20 <= len(table); at += 20 {
		d := table[at : at+20]
		zero := true
		for _, v := range d {
			if v != 0 {
				zero = false
				break
			}
		}
		if zero {
			terminated = true
			break
		}
		lookup, iat := binary.LittleEndian.Uint32(d), binary.LittleEndian.Uint32(d[16:])
		if lookup == 0 || iat == 0 || uint64(slot) < uint64(iat) || (uint64(slot)-uint64(iat))%8 != 0 {
			continue
		}
		index := (uint64(slot) - uint64(iat)) / 8
		if index >= 65536 || uint64(lookup)+index*8+8 > uint64(p.imageSize) {
			continue
		}
		module, ok := p.peString(binary.LittleEndian.Uint32(d[12:]), 128)
		if !ok {
			continue
		}
		module = strings.ToLower(module)
		if module != "msvcrt.dll" && module != "ucrtbase.dll" && module != "api-ms-win-crt-private-l1-1-0.dll" {
			continue
		}
		thunk, ok := p.mapped(lookup+uint32(index*8), 8, false)
		if !ok {
			continue
		}
		nameRVA := binary.LittleEndian.Uint64(thunk)
		if nameRVA == 0 || nameRVA > uint64(p.imageSize)-2 {
			continue
		}
		name, ok := p.peString(uint32(nameRVA)+2, 128)
		if ok && name == "__C_specific_handler" {
			found = true
		}
	}
	return found && terminated
}
func (p *PEImage) peString(rva, limit uint32) (string, bool) {
	var name []byte
	for i := uint32(0); i < limit; i++ {
		if uint64(rva)+uint64(i) > uint64(^uint32(0)) {
			return "", false
		}
		b, ok := p.mapped(rva+i, 1, false)
		if !ok {
			return "", false
		}
		if b[0] == 0 {
			return string(name), true
		}
		if b[0] < 32 || b[0] > 126 {
			return "", false
		}
		name = append(name, b[0])
	}
	return "", false
}
func (p *PEImage) readScopes(dataRVA, rva, size uint32, prologue, flags byte) ([]byte, error) {
	fail := func() ([]byte, error) {
		return nil, fmt.Errorf("%w: invalid or unsupported exception scope table", ErrUnsupported)
	}
	header, ok := p.mapped(dataRVA, 4, false)
	if !ok {
		return fail()
	}
	count := binary.LittleEndian.Uint32(header)
	if count == 0 || count > 64 {
		return fail()
	}
	table, ok := p.mapped(dataRVA, 4+count*16, false)
	if !ok {
		return fail()
	}
	external := func(address uint32) bool {
		_, ok := p.mapped(address, 1, true)
		return ok && (address < rva || uint64(address) >= uint64(rva)+uint64(size))
	}
	for i := uint32(0); i < count; i++ {
		d := table[4+i*16:]
		begin, end, filter, target := binary.LittleEndian.Uint32(d), binary.LittleEndian.Uint32(d[4:]), binary.LittleEndian.Uint32(d[8:]), binary.LittleEndian.Uint32(d[12:])
		if uint64(begin) < uint64(rva)+uint64(prologue) || begin >= end || uint64(end) > uint64(rva)+uint64(size) {
			return fail()
		}
		if target == 0 {
			if flags&16 == 0 || !external(filter) {
				return fail()
			}
		} else if target < rva+uint32(prologue) || uint64(target) >= uint64(rva)+uint64(size) || filter != 1 && !external(filter) {
			return fail()
		}
	}
	return append([]byte(nil), table...), nil
}

// Scope PCs must be exact instruction boundaries, including exclusive ends.
// Forwarding a PC inside an expanded instruction would manufacture a landing.
func relocateScopes(table, code []byte, flow leafBody, rva uint32, bodyRVA uint64) ([]byte, error) {
	fail := func() ([]byte, error) {
		return nil, fmt.Errorf("%w: exception scope is not an instruction boundary", ErrUnsupported)
	}
	boundaries := map[uint32]bool{uint32(len(code)): true}
	for at := 0; at < len(code); {
		boundaries[uint32(at)] = true
		inst, err := x86asm.Decode(code[at:], 64)
		if err != nil || inst.Len == 0 {
			return fail()
		}
		at += inst.Len
	}
	translate := func(pc uint32) (uint32, bool) {
		offset := pc - rva
		if !boundaries[offset] {
			return 0, false
		}
		delta := uint32(0)
		for _, m := range flow.pcMap {
			if uint32(m.original) > offset {
				break
			}
			delta = uint32(m.relocated - m.original)
		}
		return uint32(bodyRVA) + offset + delta, true
	}
	out := append([]byte(nil), table...)
	for at := 4; at < len(out); at += 16 {
		for _, field := range []int{0, 4, 12} {
			pc := binary.LittleEndian.Uint32(out[at+field:])
			if field == 12 && pc == 0 {
				continue
			}
			mapped, ok := translate(pc)
			if !ok {
				return fail()
			}
			binary.LittleEndian.PutUint32(out[at+field:], mapped)
		}
	}
	return out, nil
}
