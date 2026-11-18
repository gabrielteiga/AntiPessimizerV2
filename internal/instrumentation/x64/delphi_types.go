package x64

import (
	"encoding/binary"
	"fmt"
)

const delphiTypeMatcherSize = 231
const delphiTypeMatcherFingerprint = "1c2d9651897321d9ac889f7535f0e89dbfdff2fd9b11cdcd46b8e7fdf0ce442a"

type delphiTypeTable struct {
	rva  uint32
	data []byte
}

// The reviewed handler's typed branch calls this resolver. Its separate
// fingerprint is required because call destinations in the handler are masked.
func (p *PEImage) delphiTypeMatcher(handler uint32) bool {
	const callOffset = 0x199
	call, ok := p.mapped(handler+callOffset, 5, true)
	if !ok || call[0] != 0xe8 {
		return false
	}
	target := int64(handler) + callOffset + 5 + int64(int32(binary.LittleEndian.Uint32(call[1:])))
	if target < 0 || target > int64(^uint32(0)) {
		return false
	}
	return p.handlerFingerprint(uint32(target), delphiTypeMatcherSize, delphiTypeMatcherFingerprint)
}
func (p *PEImage) readDelphiTypeTable(address, rva, size uint32, prologue byte) (delphiTypeTable, error) {
	fail := func() (delphiTypeTable, error) {
		return delphiTypeTable{}, fmt.Errorf("%w: invalid Delphi typed catch table", ErrUnsupported)
	}
	outside := func(at, length uint32) bool {
		return uint64(at)+uint64(length) <= uint64(rva) || uint64(at) >= uint64(rva)+uint64(size)
	}
	header, ok := p.mapped(address, 4, false)
	if !ok || !outside(address, 4) {
		return fail()
	}
	count := binary.LittleEndian.Uint32(header)
	if count == 0 || count > 64 {
		return fail()
	}
	data, ok := p.mapped(address, 4+count*8, false)
	if !ok || !outside(address, uint32(len(data))) {
		return fail()
	}
	for i := uint32(0); i < count; i++ {
		entry := data[4+i*8:]
		class, target := binary.LittleEndian.Uint32(entry), binary.LittleEndian.Uint32(entry[4:])
		if class == 0 {
			if i+1 != count {
				return fail()
			}
		} else if _, ok := p.mapped(class, 8, false); !ok || !outside(class, 8) {
			return fail()
		}
		if uint64(target) < uint64(rva)+uint64(prologue) || uint64(target) >= uint64(rva)+uint64(size) {
			return fail()
		}
	}
	return delphiTypeTable{address, append([]byte(nil), data...)}, nil
}
func (p *PEImage) delphiTypeTables(scopes []byte, rva, size uint32, prologue byte) ([]delphiTypeTable, error) {
	seen := map[uint32]bool{}
	var tables []delphiTypeTable
	total := 4
	for at := 4; at < len(scopes); at += 16 {
		address := binary.LittleEndian.Uint32(scopes[at+8:])
		if address <= 2 || seen[address] {
			continue
		}
		table, err := p.readDelphiTypeTable(address, rva, size, prologue)
		if err != nil {
			return nil, err
		}
		total += 8 + len(table.data)
		if total > 2048 {
			return nil, fmt.Errorf("%w: Delphi typed catch metadata exceeds 2048 bytes", ErrUnsupported)
		}
		tables = append(tables, table)
		seen[address] = true
	}
	return tables, nil
}

// Blocks retain source type-slot RVAs; only landing RVAs are translated. The
// original resolver still reads initialized class pointers from its own image.
func relocateTypeTables(tables []delphiTypeTable, code []byte, flow leafBody, rva uint32, bodyRVA, blockRVA uint64) ([]byte, map[uint32]uint32, error) {
	translate, err := scopePCMap(code, flow, rva, bodyRVA)
	if err != nil {
		return nil, nil, err
	}
	blob := make([]byte, 4)
	binary.LittleEndian.PutUint32(blob, uint32(len(tables)))
	addresses := map[uint32]uint32{}
	for _, table := range tables {
		at := len(blob)
		blob = append(blob, make([]byte, 8+len(table.data))...)
		binary.LittleEndian.PutUint32(blob[at:], table.rva)
		binary.LittleEndian.PutUint32(blob[at+4:], uint32(len(table.data)))
		copy(blob[at+8:], table.data)
		addresses[table.rva] = uint32(blockRVA) + uint32(at) + 8
		for off := at + 8 + 8; off < len(blob); off += 8 {
			target, ok := translate(binary.LittleEndian.Uint32(blob[off:]))
			if !ok {
				return nil, nil, fmt.Errorf("%w: typed catch target is not an instruction boundary", ErrUnsupported)
			}
			binary.LittleEndian.PutUint32(blob[off:], target)
		}
	}
	return blob, addresses, nil
}
