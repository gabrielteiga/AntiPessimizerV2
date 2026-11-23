package x64

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"sort"
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

type functionUnwind struct {
	info                            []byte
	runtimeRVA, infoRVA, handlerRVA uint32
	handlerDataRVA                  uint32
	handlerFlags                    byte
	scopeStyle                      uint32
	scopes                          []byte
	typeTables                      []delphiTypeTable
	chainBoundaries                 []int
}

// readPELeaf deliberately reads only bounded headers, sections and .pdata.
// A general COFF parser also loads symbol tables, which are unnecessary here.
func readPELeaf(image []byte, rva, size uint32) ([]byte, uint32, error) {
	return readPECode(image, rva, size, nil)
}

// PEImage is an immutable snapshot of a validated Windows x64 image. Preparing
// it once avoids scanning the complete exception directory for every procedure.
// It proves static structure only, never live bytes, quiescence or runtime ABI.
type PEImage struct {
	data                    []byte
	sections                []peLeafSection
	imageSize, directoryRVA uint32
	table                   []byte
}

// NewPEImage owns a detached snapshot. Subsequent changes to image or previously
// returned plans cannot affect this prepared image. Concurrent plans are safe.
func NewPEImage(image []byte) (*PEImage, error) {
	p, err := parsePEImage(image)
	if err != nil {
		return nil, err
	}
	p.data = bytes.Clone(image)
	if len(p.table) > 0 {
		p.table, _ = p.mapped(p.directoryRVA, uint32(len(p.table)), false)
	}
	return p, nil
}
func (p *PEImage) ImageSize() uint32 {
	if p == nil {
		return 0
	}
	return p.imageSize
}
func readPECode(image []byte, rva, size uint32, unwind *functionUnwind) ([]byte, uint32, error) {
	p, err := parsePEImage(image)
	if err != nil {
		return nil, 0, err
	}
	return p.readCode(rva, size, unwind)
}
func parsePEImage(image []byte) (*PEImage, error) {
	fail := func(reason string) (*PEImage, error) { return nil, fmt.Errorf("%w: PE: %s", ErrUnsupported, reason) }
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

	p := &PEImage{data: image, sections: sections, imageSize: imageSize}
	var directorySize uint32
	if directories > 3 {
		p.directoryRVA = u32(h[136:])
		directorySize = u32(h[140:])
	}
	if p.directoryRVA == 0 && directorySize == 0 {
		return p, nil
	}
	if p.directoryRVA == 0 || directorySize == 0 || directorySize%12 != 0 || directorySize > 12<<20 {
		return fail("invalid exception directory")
	}
	table, ok := p.mapped(p.directoryRVA, directorySize, false)
	if !ok {
		return fail("exception directory is not raw-backed")
	}
	var previousEnd uint32
	for offset := 0; offset < len(table); offset += 12 {
		begin, end, unwind := u32(table[offset:]), u32(table[offset+4:]), u32(table[offset+8:])
		if begin >= end || begin < previousEnd || unwind == 0 || unwind%4 != 0 {
			return fail("invalid or overlapping runtime functions")
		}
		if _, ok := p.mapped(begin, end-begin, true); !ok {
			return fail("runtime function outside executable section")
		}
		if _, ok := p.mapped(unwind, 4, false); !ok {
			return fail("unwind info is not raw-backed")
		}
		previousEnd = end
	}
	p.table = table
	return p, nil
}
func (p *PEImage) mapped(start, length uint32, executable bool) ([]byte, bool) {
	if length == 0 || uint64(start)+uint64(length) > uint64(p.imageSize) {
		return nil, false
	}
	for _, s := range p.sections {
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
		return p.data[offset : offset+uint64(length)], true
	}
	return nil, false
}

func (p *PEImage) readCode(rva, size uint32, collectedUnwind *functionUnwind) ([]byte, uint32, error) {
	fail := func(reason string) ([]byte, uint32, error) {
		return nil, 0, fmt.Errorf("%w: PE: %s", ErrUnsupported, reason)
	}
	if p == nil || len(p.data) == 0 {
		return fail("uninitialized image")
	}
	if (collectedUnwind == nil && size <= JumpSize) || (collectedUnwind != nil && size < NearJumpSize) || (collectedUnwind == nil && size > 4096) || size > MaxFunctionBytes {
		return fail("invalid leaf extent")
	}
	code, ok := p.mapped(rva, size, true)
	if !ok {
		return fail("leaf is not wholly in one executable raw-backed section")
	}
	u32 := binary.LittleEndian.Uint32
	table := p.table
	index := sort.Search(len(table)/12, func(i int) bool { return u32(table[i*12+4:]) > rva })
	if index < len(table)/12 {
		offset := index * 12
		begin, end, unwind := u32(table[offset:]), u32(table[offset+4:]), u32(table[offset+8:])
		info, _ := p.mapped(unwind, 4, false) // validated for every entry during preparation
		procedureEnd := rva + size            // mapped above, so the sum fits in imageSize
		if begin < procedureEnd && rva < end {
			if begin != rva || end > procedureEnd {
				return fail("partial runtime-function overlap")
			}
			var chainBoundaries []int
			if end != procedureEnd {
				if collectedUnwind == nil || info[0] != 1 || uint32(info[1]) > end-begin {
					return fail("partial runtime-function overlap")
				}
				chainBoundaries = p.inheritedUnwindBoundaries(offset, procedureEnd, info[3])
				if len(chainBoundaries) == 0 {
					return fail("procedure extent is not covered by an inherited unwind chain")
				}
			}
			// Empty version-1 metadata has no prologue, codes, frame or handler.
			// The legacy leaf path requires empty metadata. Full plans also allow
			// verified v1 actions and UHANDLER-only cleanup forwarding.
			if collectedUnwind == nil {
				if info[0] != 1 || info[1] != 0 || info[2] != 0 || info[3] != 0 {
					return fail("procedure requires unwind/exception handling")
				}
			} else {
				if (info[0] != 1 && info[0] != 9 && info[0] != 17 && info[0] != 25) || uint32(info[1]) > size {
					return fail("unsupported language handler, chained or non-v1 unwind info")
				}
				length := uint32(4 + ((uint32(info[2])+1)&^1)*2)
				complete, ok := p.mapped(binary.LittleEndian.Uint32(table[offset+8:]), length, false)
				if !ok {
					return fail("truncated function unwind codes")
				}
				original := append([]byte(nil), complete...)
				original[0] = 1 // validate actions separately from original UHANDLER
				if err := validateUnwindCodes(original); err != nil {
					return nil, 0, err
				}
				*collectedUnwind = functionUnwind{info: original, runtimeRVA: p.directoryRVA + uint32(offset), infoRVA: unwind, chainBoundaries: chainBoundaries}
				if info[0] != 1 {
					if uint64(unwind)+uint64(length)+4 > uint64(p.imageSize) {
						return fail("truncated original unwind handler")
					}
					handler, ok := p.mapped(unwind+length, 4, false)
					if !ok {
						return fail("original unwind handler is not raw-backed")
					}
					collectedUnwind.handlerRVA = u32(handler)
					collectedUnwind.handlerDataRVA = unwind + length + 4
					collectedUnwind.handlerFlags = info[0]
					if _, ok := p.mapped(collectedUnwind.handlerRVA, 1, true); !ok || collectedUnwind.handlerRVA >= rva && uint64(collectedUnwind.handlerRVA) < uint64(rva)+uint64(size) {
						return fail("invalid or self-referential original unwind handler")
					}
					if info[0]&8 != 0 {
						switch {
						case p.cSpecificHandler(collectedUnwind.handlerRVA):
							collectedUnwind.scopeStyle = 1
						case info[0] == 25 && p.delphiScopeHandler(collectedUnwind.handlerRVA):
							collectedUnwind.scopeStyle = 2
						default:
							return fail("unrecognized exception handler; requires a recognized CRT or reviewed Delphi handler")
						}
						// Probe the bounded table header first; its typed branches need a
						// separately recognized resolver before interpreting nested tables.
						if collectedUnwind.scopeStyle == 2 {
							header, ok := p.mapped(collectedUnwind.handlerDataRVA, 4, false)
							if ok && u32(header) > 0 && u32(header) <= 64 {
								raw, ok := p.mapped(collectedUnwind.handlerDataRVA, 4+u32(header)*16, false)
								if ok {
									for at := 4; at < len(raw); at += 16 {
										if u32(raw[at+8:]) > 2 {
											if !p.delphiTypeMatcher(collectedUnwind.handlerRVA) {
												return fail("unrecognized Delphi typed exception matcher")
											}
											collectedUnwind.scopeStyle = 3
											break
										}
									}
								}
							}
						}
						scopes, err := p.readScopes(collectedUnwind.handlerDataRVA, rva, size, info[1], info[0], collectedUnwind.scopeStyle)
						if err != nil {
							return nil, 0, err
						}
						collectedUnwind.scopes = scopes
						if collectedUnwind.scopeStyle == 3 {
							tables, err := p.delphiTypeTables(scopes, rva, size, info[1])
							if err != nil {
								return nil, 0, err
							}
							collectedUnwind.typeTables = tables
						}
					}
				}
			}
		}
	}
	return bytes.Clone(code), p.imageSize, nil
}

// A secondary may inherit the already-established frame without adding any
// unwind actions. In this case one copied record with the primary's actions
// describes all of the relocated code. Match the complete embedded runtime
// entry to the primary or the previous (already checked) fragment.
func (p *PEImage) inheritedUnwindBoundaries(primaryAt int, procedureEnd uint32, frame byte) []int {
	u32 := binary.LittleEndian.Uint32
	primary := p.table[primaryAt : primaryAt+12]
	previous, end := primary, u32(primary[4:])
	start := u32(primary)
	var boundaries []int
	for at := primaryAt + 12; end < procedureEnd; at += 12 {
		if at >= len(p.table) {
			return nil
		}
		entry := p.table[at : at+12]
		nextEnd := u32(entry[4:])
		if u32(entry) != end || nextEnd > procedureEnd {
			return nil
		}
		info, ok := p.mapped(u32(entry[8:]), 16, false)
		if !ok || info[0] != 33 || info[1] != 0 || info[2] != 0 || info[3] != frame {
			return nil
		}
		if !bytes.Equal(info[4:], primary) && !bytes.Equal(info[4:], previous) {
			return nil
		}
		boundaries = append(boundaries, int(end-start))
		previous, end = entry, nextEnd
	}
	return boundaries
}
