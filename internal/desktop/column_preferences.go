package desktop

const (
	columnSampling = iota
	columnSamplingComparison
	columnCalls
	columnCallComparison
	columnViewCount
	maxResultColumns = 17
	maxColumnWidth   = 16384
)

var defaultColumnWidths = [columnViewCount][]int32{
	{90, 80, 90, 180, 420, 160, 100, 180},
	{90, 80, 90, 180, 420, 160, 100, 180},
	{100, 140, 170, 140, 170, 90, 160, 180, 420, 160, 180, 90, 130},
	{120, 130, 120, 160, 170, 180, 160, 170, 180, 180, 180, 180, 180, 420, 180, 180, 160},
}

type columnLayout struct {
	Widths [maxResultColumns]int32 `json:"widths"`
	Order  [maxResultColumns]int32 `json:"order"`
	Hidden uint32                  `json:"hidden,omitempty"`
}

type columnPreferences [columnViewCount]columnLayout

func (l columnLayout) valid(columns int) bool {
	if l == (columnLayout{}) {
		return true
	}
	if l.Hidden >= columnMask(columns) {
		return false
	}
	var seen [maxResultColumns]bool
	for i := 0; i < columns; i++ {
		at := l.Order[i]
		if l.Widths[i] < 1 || l.Widths[i] > maxColumnWidth || at < 0 || at >= int32(columns) || seen[at] {
			return false
		}
		seen[at] = true
	}
	for i := columns; i < maxResultColumns; i++ {
		if l.Widths[i] != 0 || l.Order[i] != 0 {
			return false
		}
	}
	return true
}

func columnMask(columns int) uint32 {
	return (1 << columns) - 1
}

func (m *Model) columnView() int {
	switch {
	case m.MetricComparison != nil:
		return columnCallComparison
	case len(m.Metrics) > 0:
		return columnCalls
	case m.Comparison != nil:
		return columnSamplingComparison
	default:
		return columnSampling
	}
}
