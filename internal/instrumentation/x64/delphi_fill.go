package x64

import (
	"crypto/sha256"
	"encoding/binary"
)

// The pinned Delphi fill implementation has a scalar jump table for counts
// 1..8, direct paths for counts 9..32, an SSE loop and a REP STOSB path. The
// hash covers the bounds checks and all instructions. The nine pointers are
// checked separately against the PE relocations and exact case offsets.
var delphiFillInstructionHash = [32]byte{
	0x69, 0x8b, 0xc4, 0x1c, 0x96, 0xa9, 0x2c, 0xdc, 0xe4, 0x95, 0x9f, 0x84, 0xe1, 0xfc, 0xf1, 0x52,
	0xe2, 0x6d, 0xc5, 0xb1, 0xce, 0x77, 0x61, 0xe3, 0x0c, 0xa4, 0x8f, 0x42, 0xc9, 0x9d, 0x03, 0x4a,
}

func (p *PEImage) delphiFillTable(rva uint32, code []byte, patchSize int) *indexedJumpTable {
	const load, start, end, size = 60, 72, 144, 267
	if len(code) != size || patchSize > load || !p.validPreferredBase() {
		return nil
	}
	var instructions [size]byte
	copy(instructions[:start], code[:start])
	copy(instructions[end:], code[end:])
	if sha256.Sum256(instructions[:]) != delphiFillInstructionHash {
		return nil
	}
	threshold, ok := address(uint64(rva)+11, int64(int32(binary.LittleEndian.Uint32(code[7:]))))
	if !ok || threshold >= uint64(p.imageSize) {
		return nil
	}
	if _, ok := p.mapped(uint32(threshold), 8, false); !ok {
		return nil
	}
	cases := []int{0xa9, 0xb1, 0xb7, 0xad, 0xbe, 0xbb, 0xb4, 0xaa, 0xa6}
	if !p.hasIndexedTableRelocations(rva, len(code), start, len(cases)) {
		return nil
	}
	for i, target := range cases {
		if binary.LittleEndian.Uint64(code[start+i*8:]) != p.preferredBase+uint64(rva)+uint64(target) {
			return nil
		}
	}
	return &indexedJumpTable{load: load, data: start, targets: cases}
}
