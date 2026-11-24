package td32

import "sort"

// sstModule layout follows JCL's TModuleInfo/TSegmentInfo. These modules are
// Pascal units/object modules, distinct from the executable image.
type unitRange struct {
	segment    uint16
	start, end uint64
}
type unitInfo struct {
	name   string
	ranges []unitRange
}

type unitContribution struct {
	unitRange
	name string
}
type moduleUnits struct {
	directory map[uint16]unitInfo
	coverage  []unitContribution
	indexed   bool
}

func parseModules(entries []entry, names []string) (*moduleUnits, error) {
	result := map[uint16]unitInfo{}
	total := 0
	for _, e := range entries {
		if e.kind != 0x120 {
			continue
		}
		if len(e.data) < 28 {
			return nil, ErrInvalid
		}
		idx := uint64(u32(e.data[8:]))
		count := int(u16(e.data[4:]))
		if idx == 0 || idx >= uint64(len(names)) || names[idx] == "" || count > (len(e.data)-28)/12 {
			return nil, ErrInvalid
		}
		// 0 and 0xffff identify non-module/global directories and cannot provide units.
		if e.module == 0 || e.module == 0xffff {
			return nil, ErrInvalid
		}
		if _, exists := result[e.module]; exists {
			return nil, ErrInvalid
		}
		m := unitInfo{name: names[idx]}
		for i := 0; i < count; i++ {
			total++
			if total > 1<<20 {
				return nil, ErrInvalid
			}
			r := e.data[28+12*i:]
			start, size := uint64(u32(r[4:])), uint64(u32(r[8:]))
			if u16(r) == 0 || start+size > 1<<32 {
				return nil, ErrInvalid
			}
			if u16(r[2:])&1 != 0 && size > 0 {
				m.ranges = append(m.ranges, unitRange{u16(r), start, start + size})
			}
		}
		sort.Slice(m.ranges, func(i, j int) bool {
			a, b := m.ranges[i], m.ranges[j]
			if a.segment != b.segment {
				return a.segment < b.segment
			}
			return a.start < b.start
		})
		for i := 1; i < len(m.ranges); i++ {
			a, b := m.ranges[i-1], m.ranges[i]
			if a.segment == b.segment && b.start < a.end {
				return nil, ErrInvalid
			}
		}
		result[e.module] = m
	}
	return &moduleUnits{directory: result}, nil
}
func (m unitInfo) unitFor(p Procedure) string {
	i := sort.Search(len(m.ranges), func(i int) bool {
		r := m.ranges[i]
		return r.segment > p.Segment || r.segment == p.Segment && r.start > uint64(p.Offset)
	}) - 1
	if i >= 0 {
		r := m.ranges[i]
		if r.segment == p.Segment && uint64(p.Offset) >= r.start && uint64(p.Offset) < r.end {
			return m.name
		}
	}
	return ""
}

// Build disjoint coverage once, rather than scanning all modules for every
// procedure. Overlapping contributions from different unit names are unknown;
// a matching directory's own contribution still takes precedence.
func unitCoverage(modules map[uint16]unitInfo) []unitContribution {
	type boundary struct {
		offset  uint64
		unit    int32
		segment uint16
	}
	var boundaries []boundary
	names := []string{""}
	unitIDs := make(map[string]int32, len(modules))
	for _, m := range modules {
		if len(m.ranges) == 0 {
			continue
		}
		id := unitIDs[m.name]
		if id == 0 {
			id = int32(len(names))
			unitIDs[m.name] = id
			names = append(names, m.name)
		}
		for _, r := range m.ranges {
			boundaries = append(boundaries, boundary{r.start, id, r.segment}, boundary{r.end, -id, r.segment})
		}
	}
	sort.Slice(boundaries, func(i, j int) bool {
		a, b := boundaries[i], boundaries[j]
		if a.segment != b.segment {
			return a.segment < b.segment
		}
		return a.offset < b.offset
	})
	active := make([]int, len(names))
	var activeID int32
	distinct := 0
	var result []unitContribution
	for i, edge := range boundaries {
		id := edge.unit
		if id > 0 {
			if active[id] == 0 {
				distinct++
				activeID ^= id
			}
			active[id]++
		} else {
			id = -id
			active[id]--
			if active[id] == 0 {
				distinct--
				activeID ^= id
			}
		}
		if i+1 == len(boundaries) || distinct != 1 {
			continue
		}
		next := boundaries[i+1]
		if next.segment != edge.segment || next.offset == edge.offset {
			continue
		}
		// The XOR of active distinct IDs equals the unique ID when exactly
		// one unit remains. Same-name overlaps affect only its reference count.
		name := names[activeID]
		if len(result) != 0 {
			last := &result[len(result)-1]
			if last.segment == edge.segment && last.end == edge.offset && last.name == name {
				last.end = next.offset
				continue
			}
		}
		result = append(result, unitContribution{unitRange{edge.segment, edge.offset, next.offset}, name})
	}
	return result
}
func (m *moduleUnits) unitFor(directory uint16, p Procedure) string {
	if unit := m.directory[directory].unitFor(p); unit != "" {
		return unit
	}
	if !m.indexed {
		m.coverage = unitCoverage(m.directory)
		m.indexed = true
	}
	i := sort.Search(len(m.coverage), func(i int) bool {
		r := m.coverage[i]
		return r.segment > p.Segment || r.segment == p.Segment && r.start > uint64(p.Offset)
	}) - 1
	if i >= 0 {
		r := m.coverage[i]
		if r.segment == p.Segment && uint64(p.Offset) >= r.start && uint64(p.Offset) < r.end {
			return r.name
		}
	}
	return ""
}
