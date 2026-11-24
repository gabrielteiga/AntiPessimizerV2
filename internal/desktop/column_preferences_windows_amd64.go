package desktop

import "unsafe"

const (
	listGetColumnWidth = 0x101d
	listSetColumnOrder = 0x103a
	listGetColumnOrder = 0x103b
)

func (a *window) storeColumnLayout() {
	if a.tableColumns == 0 {
		return
	}
	var layout columnLayout
	if ok, _, _ := send.Call(a.table, listGetColumnOrder, uintptr(a.tableColumns), uintptr(unsafe.Pointer(&layout.Order[0]))); ok == 0 {
		return
	}
	defaults := defaultColumnWidths[a.tableView]
	custom := false
	for i, width := range defaults {
		current, _, _ := send.Call(a.table, listGetColumnWidth, uintptr(i), 0)
		layout.Widths[i] = int32(min(max(current, 1), maxColumnWidth))
		custom = custom || layout.Widths[i] != width || layout.Order[i] != int32(i)
	}
	if !custom {
		layout = columnLayout{}
	}
	if layout.valid(a.tableColumns) {
		a.preferences.Columns[a.tableView] = layout
	}
}

func (a *window) applyColumnOrder(view int) {
	order := a.preferences.Columns[view].Order
	if a.preferences.Columns[view] == (columnLayout{}) {
		for i := 0; i < a.tableColumns; i++ {
			order[i] = int32(i)
		}
	}
	send.Call(a.table, listSetColumnOrder, uintptr(a.tableColumns), uintptr(unsafe.Pointer(&order[0])))
}
