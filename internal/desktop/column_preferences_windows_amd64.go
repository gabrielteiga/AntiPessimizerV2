package desktop

import (
	"unsafe"

	"golang.org/x/sys/windows"
)

const (
	listGetColumnWidth = 0x101d
	listSetColumnWidth = 0x101e
	listGetHeader      = 0x101f
	listSetColumnOrder = 0x103a
	listGetColumnOrder = 0x103b
)

func (a *window) storeColumnLayout() {
	if a.tableColumns == 0 {
		return
	}
	old := a.preferences.Columns[a.tableView]
	layout := columnLayout{Hidden: old.Hidden}
	if ok, _, _ := send.Call(a.table, listGetColumnOrder, uintptr(a.tableColumns), uintptr(unsafe.Pointer(&layout.Order[0]))); ok == 0 {
		return
	}
	defaults := defaultColumnWidths[a.tableView]
	custom := layout.Hidden != 0
	for i, width := range defaults {
		if layout.Hidden&(1<<i) != 0 {
			layout.Widths[i] = old.Widths[i]
		} else {
			current, _, _ := send.Call(a.table, listGetColumnWidth, uintptr(i), 0)
			layout.Widths[i] = int32(min(max(current, 1), maxColumnWidth))
		}
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

func (a *window) toggleResultColumn(index int) {
	if index < 0 || index >= a.tableColumns {
		return
	}
	a.storeColumnLayout()
	layout := a.preferences.Columns[a.tableView]
	next := layout.Hidden ^ (1 << index)
	if next == columnMask(a.tableColumns) {
		return
	}
	if layout == (columnLayout{}) {
		for i, width := range defaultColumnWidths[a.tableView] {
			layout.Widths[i], layout.Order[i] = width, int32(i)
		}
	}
	width := layout.Widths[index]
	if next&(1<<index) != 0 {
		width = 0
	}
	if ok, _, _ := send.Call(a.table, listSetColumnWidth, uintptr(index), uintptr(width)); ok != 0 {
		layout.Hidden = next
		a.preferences.Columns[a.tableView] = layout
	}
}

func (a *window) resultColumnMenu(header uintptr, x, y int32) {
	names := a.model.Headers()
	if len(names) != a.tableColumns || len(names) == 0 {
		return
	}
	menu, _, _ := user.NewProc("CreatePopupMenu").Call()
	if menu == 0 {
		return
	}
	defer user.NewProc("DestroyMenu").Call(menu)

	if x == -1 && y == -1 {
		var bounds rect
		if ok, _, _ := user.NewProc("GetWindowRect").Call(header, uintptr(unsafe.Pointer(&bounds))); ok == 0 {
			return
		}
		x, y = bounds.Left, bounds.Bottom
	}
	appendMenu := user.NewProc("AppendMenuW")
	view := a.tableView
	hidden := a.preferences.Columns[view].Hidden
	for i, name := range names {
		var flags uintptr
		if hidden&(1<<i) == 0 {
			flags = 0x0008 // MF_CHECKED
			if hidden|(1<<i) == columnMask(len(names)) {
				flags |= 0x0001 // MF_GRAYED
			}
		}
		label := windows.StringToUTF16Ptr(name)
		if ok, _, _ := appendMenu.Call(menu, flags, uintptr(i+1), uintptr(unsafe.Pointer(label))); ok == 0 {
			return
		}
	}
	// TPM_RETURNCMD | TPM_NONOTIFY | TPM_RIGHTBUTTON
	command, _, _ := user.NewProc("TrackPopupMenu").Call(menu, 0x0182, uintptr(x), uintptr(y), 0, a.hwnd, 0)
	if command != 0 && a.tableView == view {
		a.toggleResultColumn(int(command) - 1)
	}
}
