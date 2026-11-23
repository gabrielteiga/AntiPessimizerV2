package td32

import "sort"

type SourceLocation struct {
	File       string `json:"file"`
	Line       uint16 `json:"line"`
	LineOffset uint32 `json:"line_offset"`
}

// SourceIndex is immutable after parsing. Lookups use the same segmented
// offsets as procedure records and leave ambiguous source locations unknown.
type SourceIndex struct {
	ranges []sourceRange
}

type sourceRange struct {
	file    string
	start   uint32
	end     uint64
	segment uint16
	line    uint16
}

func (s SourceIndex) Empty() bool { return len(s.ranges) == 0 }

func (s SourceIndex) Lookup(segment uint16, offset uint32) (SourceLocation, bool) {
	i := sort.Search(len(s.ranges), func(i int) bool {
		r := s.ranges[i]
		return r.segment > segment || r.segment == segment && r.start > offset
	}) - 1
	if i >= 0 {
		r := s.ranges[i]
		if r.segment == segment && uint64(offset) < r.end && r.line != 0 {
			return SourceLocation{r.file, r.line, offset - r.start}, true
		}
	}
	return SourceLocation{}, false
}

// The Borland file and line offsets in sstSrcModule are relative to that
// subsection. The packed headers follow the vendored JCL records.
func parseSourceLines(entries []entry, names []string) (SourceIndex, error) {
	budget := uint64(1 << 20)
	var ranges []sourceRange
	for _, e := range entries {
		if e.kind != 0x127 {
			continue
		}
		b := e.data
		if len(b) < 4 {
			return SourceIndex{}, ErrInvalid
		}
		files, segments := uint64(u16(b)), uint64(u16(b[2:]))
		moduleSize := 4 + 4*files + 8*segments + 2*((segments+1)&^1)
		if moduleSize > uint64(len(b)) || files > budget {
			return SourceIndex{}, ErrInvalid
		}
		budget -= files
		for i := uint64(0); i < files; i++ {
			off := uint64(u32(b[4+4*i:]))
			header, err := region(b, off, 6)
			if err != nil || off < moduleSize {
				return SourceIndex{}, ErrInvalid
			}
			count, name := uint64(u16(header)), uint64(u32(header[2:]))
			if name == 0 {
				continue
			}
			file, err := region(b, off, 6+12*count)
			if err != nil || name >= uint64(len(names)) || names[name] == "" || count > budget {
				return SourceIndex{}, ErrInvalid
			}
			budget -= count
			for j := uint64(0); j < count; j++ {
				at := uint64(u32(file[6+4*j:]))
				bounds := file[6+4*count+8*j:]
				if at < moduleSize {
					return SourceIndex{}, ErrInvalid
				}
				ranges, err = appendSourceRanges(ranges, b, at, names[name], u32(bounds), u32(bounds[4:]), &budget)
				if err != nil {
					return SourceIndex{}, err
				}
			}
		}
	}
	sort.Slice(ranges, func(i, j int) bool {
		a, b := ranges[i], ranges[j]
		if a.segment != b.segment {
			return a.segment < b.segment
		}
		return a.start < b.start
	})
	result := ranges[:0]
	for _, next := range ranges {
		if len(result) > 0 {
			last := &result[len(result)-1]
			if next.segment == last.segment && uint64(next.start) < last.end {
				last.end = max(last.end, next.end)
				if next.start != last.start || next.file != last.file || next.line != last.line {
					last.file, last.line = "", 0
				}
				continue
			}
		}
		result = append(result, next)
	}
	return SourceIndex{ranges: result}, nil
}

func appendSourceRanges(result []sourceRange, b []byte, at uint64, file string, start, end uint32, budget *uint64) ([]sourceRange, error) {
	header, err := region(b, at, 4)
	if err != nil {
		return nil, err
	}
	segment, count := u16(header), uint64(u16(header[2:]))
	bounded := start != 0 || end != 0
	lines, err := region(b, at+4, 4*count+2*((count+1)&^1))
	if err != nil || segment == 0 || count > *budget || bounded && start >= end {
		return nil, ErrInvalid
	}
	*budget -= count
	type point struct {
		offset uint32
		line   uint16
	}
	points := make([]point, 0, count)
	for i := uint64(0); i < count; i++ {
		p := point{u32(lines[4*i:]), u16(lines[4*count+2*i:])}
		if bounded && (p.offset < start || p.offset >= end) {
			return nil, ErrInvalid
		}
		points = append(points, p)
	}
	sort.Slice(points, func(i, j int) bool { return points[i].offset < points[j].offset })
	for i := 0; i < len(points); {
		p := points[i]
		i++
		for i < len(points) && points[i].offset == p.offset {
			if points[i].line != p.line {
				p.line = 0
			}
			i++
		}
		to := uint64(p.offset) + 1
		if bounded {
			to = uint64(end)
			if i < len(points) {
				to = uint64(points[i].offset)
			}
		}
		result = append(result, sourceRange{file: file, start: p.offset, end: to, segment: segment, line: p.line})
	}
	return result, nil
}
