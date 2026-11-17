package x64

import (
	"encoding/binary"
	"fmt"
)

// BuildPELeaf checks the on-disk Windows x64 section and exception directory
// before building a restricted leaf plan. A matching RUNTIME_FUNCTION is allowed
// only when its exact range has empty version-1 unwind info and no handler.
// loadedBase must be the actual base, never PE's preferred ImageBase. This does
// not authenticate external TDS, inspect dynamic function tables, compare live
// bytes or establish quiescence. Those remain installer responsibilities.
func BuildPELeaf(image []byte, rva, size uint32, loadedBase, allocation, enter, exit uint64, id uint32) (LeafPlan, error) {
	code, imageSize, err := readPELeaf(image, rva, size)
	if err != nil {
		return LeafPlan{}, err
	}
	if loadedBase == 0 || loadedBase >= maxUserAddress || uint64(imageSize) >= maxUserAddress-loadedBase {
		return LeafPlan{}, fmt.Errorf("%w: PE: invalid loaded image base", ErrUnsupported)
	}
	plan, err := BuildLeaf(code, loadedBase+uint64(rva), allocation, enter, exit, id)
	if err != nil {
		return LeafPlan{}, err
	}
	if allocation < loadedBase+uint64(imageSize) && allocation+uint64(len(plan.Image)) > loadedBase {
		return LeafPlan{}, fmt.Errorf("%w: PE: allocation overlaps loaded image", ErrUnsupported)
	}
	return plan, nil
}

type peLeafSection struct{ rva, virtual, raw, offset, flags uint32 }

// readPELeaf deliberately reads only bounded headers, sections and .pdata.
// A general COFF parser also loads symbol tables, which are unnecessary here.
func readPELeaf(image []byte, rva, size uint32) ([]byte, uint32, error) {
	fail := func(reason string) ([]byte, uint32, error) {
		return nil, 0, fmt.Errorf("%w: PE: %s", ErrUnsupported, reason)
	}
	if len(image) < 64 || len(image) > 256<<20 || string(image[:2]) != "MZ" {
		return fail("invalid DOS image")
	}
	u16 := binary.LittleEndian.Uint16
	u32 := binary.LittleEndian.Uint32
	nt := uint64(u32(image[60:]))
	if nt < 64 || nt+24 > uint64(len(image)) || string(image[nt:nt+4]) != "PE\x00\x00" {
		return fail("invalid NT header")
	}
	if u16(image[nt+4:]) != 0x8664 {
		return fail("only Windows x64 images are supported")
	}
	count := uint64(u16(image[nt+6:]))
	optional := nt + 24
	optionalSize := uint64(u16(image[nt+20:]))
	if count == 0 || count > 96 || optionalSize < 112 || optional+optionalSize+count*40 > uint64(len(image)) {
		return fail("invalid header/section count")
	}
	h := image[optional : optional+optionalSize]
	if u16(h) != 0x20b {
		return fail("PE32+ optional header required")
	}
	imageSize, headersSize := u32(h[56:]), u32(h[60:])
	if imageSize == 0 || headersSize == 0 || uint64(headersSize) > uint64(len(image)) || uint64(headersSize) < optional+optionalSize+count*40 || headersSize > imageSize {
		return fail("invalid image/header size")
	}
	directories := uint64(u32(h[108:]))
	if directories > 16 || 112+directories*8 > optionalSize {
		return fail("invalid data directory count")
	}
	var sections []peLeafSection
	for i := uint64(0); i < count; i++ {
		at := optional + optionalSize + i*40
		b := image[at : at+40]
		s := peLeafSection{rva: u32(b[12:]), virtual: u32(b[8:]), raw: u32(b[16:]), offset: u32(b[20:]), flags: u32(b[36:])}
		if s.virtual == 0 {
			s.virtual = s.raw
		}
		// Raw section padding is mapped too. Reject ambiguous virtual/raw aliases.
		span := s.virtual
		if s.raw > span {
			span = s.raw
		}
		if span == 0 || s.rva < headersSize || uint64(s.rva)+uint64(span) > uint64(imageSize) {
			return fail("invalid section mapping")
		}
		if s.raw != 0 && (s.offset < headersSize || uint64(s.offset)+uint64(s.raw) > uint64(len(image))) {
			return fail("truncated section data")
		}
		for _, old := range sections {
			oldSpan := old.virtual
			if old.raw > oldSpan {
				oldSpan = old.raw
			}
			if uint64(s.rva) < uint64(old.rva)+uint64(oldSpan) && uint64(old.rva) < uint64(s.rva)+uint64(span) {
				return fail("overlapping virtual sections")
			}
			if s.raw != 0 && old.raw != 0 && uint64(s.offset) < uint64(old.offset)+uint64(old.raw) && uint64(old.offset) < uint64(s.offset)+uint64(s.raw) {
				return fail("overlapping raw sections")
			}
		}
		sections = append(sections, s)
	}
	mapped := func(start, length uint32, executable bool) ([]byte, bool) {
		if length == 0 || uint64(start)+uint64(length) > uint64(imageSize) {
			return nil, false
		}
		for _, s := range sections {
			if start < s.rva {
				continue
			}
			delta := uint64(start) - uint64(s.rva)
			if delta+uint64(length) > uint64(s.virtual) || delta+uint64(length) > uint64(s.raw) {
				continue
			}
			if executable && s.flags&0x20000000 == 0 {
				return nil, false
			}
			offset := uint64(s.offset) + delta
			return image[offset : offset+uint64(length)], true
		}
		return nil, false
	}
	if size <= JumpSize || size > 4096 {
		return fail("invalid leaf extent")
	}
	code, ok := mapped(rva, size, true)
	if !ok {
		return fail("leaf is not wholly in one executable raw-backed section")
	}
	var directoryRVA, directorySize uint32
	if directories > 3 {
		directoryRVA = u32(h[136:])
		directorySize = u32(h[140:])
	}
	if directoryRVA == 0 && directorySize == 0 {
		return append([]byte(nil), code...), imageSize, nil
	}
	if directoryRVA == 0 || directorySize == 0 || directorySize%12 != 0 || directorySize > 12<<20 {
		return fail("invalid exception directory")
	}
	table, ok := mapped(directoryRVA, directorySize, false)
	if !ok {
		return fail("exception directory is not raw-backed")
	}
	var previousEnd uint32
	for offset := 0; offset < len(table); offset += 12 {
		begin, end, unwind := u32(table[offset:]), u32(table[offset+4:]), u32(table[offset+8:])
		if begin >= end || begin < previousEnd || unwind == 0 || unwind%4 != 0 {
			return fail("invalid or overlapping runtime functions")
		}
		if _, ok := mapped(begin, end-begin, true); !ok {
			return fail("runtime function outside executable section")
		}
		info, ok := mapped(unwind, 4, false)
		if !ok {
			return fail("unwind info is not raw-backed")
		}
		if uint64(begin) < uint64(rva)+uint64(size) && rva < end {
			if begin != rva || uint64(end) != uint64(rva)+uint64(size) {
				return fail("partial runtime-function overlap")
			}
			// Empty version-1 metadata has no prologue, codes, frame or handler.
			// All other versions, chains and language-specific handlers are rejected.
			if info[0] != 1 || info[1] != 0 || info[2] != 0 || info[3] != 0 {
				return fail("procedure requires unwind/exception handling")
			}
		}
		previousEnd = end
	}
	return append([]byte(nil), code...), imageSize, nil
}
