package desktop

import (
	"context"
	"fmt"
	"os"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"
	"unsafe"

	"github.com/gabrielteiga/AntiPessimizerV2/internal/platform"
	"github.com/gabrielteiga/AntiPessimizerV2/internal/profiling"
	"github.com/gabrielteiga/AntiPessimizerV2/internal/report"
	"github.com/gabrielteiga/AntiPessimizerV2/internal/service"
	"github.com/gabrielteiga/AntiPessimizerV2/internal/stream"
	"golang.org/x/sys/windows"
)

var user = windows.NewLazySystemDLL("user32.dll")
var gdi = windows.NewLazySystemDLL("gdi32.dll")
var create = user.NewProc("CreateWindowExW")
var send = user.NewProc("SendMessageW")
var setText = user.NewProc("SetWindowTextW")
var defWindow = user.NewProc("DefWindowProcW")
var destroy = user.NewProc("DestroyWindow")
var postQuit = user.NewProc("PostQuitMessage")
var getText = user.NewProc("GetWindowTextW")
var move = user.NewProc("MoveWindow")
var invalidate = user.NewProc("InvalidateRect")

type point struct{ X, Y int32 }
type rect struct{ Left, Top, Right, Bottom int32 }
type message struct {
	HWND           uintptr
	Message        uint32
	WParam, LParam uintptr
	Time           uint32
	Point          point
	Private        uint32
}
type windowClass struct {
	Size, Style                        uint32
	Proc                               uintptr
	ClassExtra, WindowExtra            int32
	Instance, Icon, Cursor, Background uintptr
	Menu, Name                         *uint16
	SmallIcon                          uintptr
}
type column struct {
	Mask                                        uint32
	Format, Width                               int32
	Text                                        *uint16
	Max, Sub, Image, Order, Min, Default, Ideal int32
}
type notifyHeader struct {
	Window, ID uintptr
	Code       int32
}
type item struct {
	Mask             uint32
	Index, Sub       int32
	State, StateMask uint32
	Text             *uint16
	Max, Image       int32
	Param            uintptr
	Indent, Group    int32
	Columns          uint32
	Column, Formats  uintptr
	GroupIndex       int32
}
type displayInfo struct {
	Header notifyHeader
	Item   item
}
type paint struct {
	DC                 uintptr
	Erase              int32
	Rect               rect
	Restore, IncUpdate int32
	Reserved           [32]byte
}
type update struct {
	Response service.Response
	Error    error
	History  []profiling.Snapshot
	Offline  bool
	Note     string
}
type window struct {
	hwnd, processes, start, stop, demo, refresh, filter, thread, seconds, path, save, replay, compare, status, table, details uintptr
	address                                                                                                                   string
	ctx                                                                                                                       context.Context
	cancel                                                                                                                    context.CancelFunc
	wg                                                                                                                        sync.WaitGroup
	updates                                                                                                                   chan update
	offline                                                                                                                   atomic.Bool
	model                                                                                                                     Model
	targets                                                                                                                   []platform.Process
	lastBuild                                                                                                                 string
	lastSequence                                                                                                              uint64
	width, height                                                                                                             int32
}

var active *window // only the locked OS UI thread reads/writes this pointer
var wndProc = windows.NewCallback(func(hwnd uintptr, msg uint32, w uintptr, nativeParam unsafe.Pointer) uintptr {
	l := uintptr(nativeParam)
	a := active
	if a == nil {
		r, _, _ := defWindow.Call(hwnd, uintptr(msg), w, l)
		return r
	}
	switch msg {
	case 0x0005: // WM_SIZE
		a.width = int32(uint16(l))
		a.height = int32(uint16(l >> 16))
		a.layout()
		return 0
	case 0x0111: // WM_COMMAND
		if w>>16 == 0 {
			a.command(int(w & 0xffff))
		}
		return 0
	case 0x0113:
		a.tick()
		return 0
	case 0x004e: // WM_NOTIFY, virtual ListView text and selection
		header := (*notifyHeader)(nativeParam)
		if header.Window == a.table && header.Code == -177 {
			d := (*displayInfo)(nativeParam)
			i, j := int(d.Item.Index), int(d.Item.Sub)
			if d.Item.Mask&1 != 0 && d.Item.Text != nil && d.Item.Max > 0 && i >= 0 && i < len(a.model.Rows) && j >= 0 && j < len(a.model.Rows[i]) {
				chars := windows.StringToUTF16(a.model.Rows[i][j])
				n := int(d.Item.Max)
				if n > len(chars) {
					n = len(chars)
				}
				dst := unsafe.Slice(d.Item.Text, int(d.Item.Max))
				copy(dst[:n], chars[:n])
				dst[n-1] = 0
			}
			return 0
		}
		if header.Window == a.table && header.Code == -101 { // LVN_ITEMCHANGED
			selected, _, _ := send.Call(a.table, 0x100c, ^uintptr(0), 2)
			if int(selected) >= 0 && int(selected) < len(a.model.Rows) {
				text(a.details, strings.Join(a.model.Rows[selected], "  |  "))
			}
		}
	case 0x000f:
		a.draw()
		return 0
	case 0x0010:
		destroy.Call(hwnd)
		return 0
	case 0x0002:
		a.cancel()
		postQuit.Call(0)
		return 0
	}
	r, _, _ := defWindow.Call(hwnd, uintptr(msg), w, l)
	return r
})

func text(h uintptr, s string) { setText.Call(h, uintptr(unsafe.Pointer(windows.StringToUTF16Ptr(s)))) }
func value(h uintptr) string {
	var b [32768]uint16
	getText.Call(h, uintptr(unsafe.Pointer(&b[0])), uintptr(len(b)))
	return windows.UTF16ToString(b[:])
}
func child(parent uintptr, class, label string, style uint32, id int) uintptr {
	h, _, _ := create.Call(0, uintptr(unsafe.Pointer(windows.StringToUTF16Ptr(class))), uintptr(unsafe.Pointer(windows.StringToUTF16Ptr(label))), uintptr(style|0x50000000), 0, 0, 100, 24, parent, uintptr(id), 0, 0)
	font, _, _ := gdi.NewProc("GetStockObject").Call(17)
	send.Call(h, 0x30, font, 1)
	return h
}
func Run(address string) error {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	a := &window{address: address, ctx: ctx, cancel: cancel, updates: make(chan update, 8), width: 1180, height: 760}
	active = a
	defer func() { active = nil }()
	windows.NewLazySystemDLL("comctl32.dll").NewProc("InitCommonControls").Call()
	return a.open()
}
func (a *window) open() error {
	class := windows.StringToUTF16Ptr("AntiPessimizerGoViewer")
	cursor, _, _ := user.NewProc("LoadCursorW").Call(0, 32512)
	var instance windows.Handle
	if err := windows.GetModuleHandleEx(2, nil, &instance); err != nil {
		return err
	}
	c := windowClass{Instance: uintptr(instance), Size: uint32(unsafe.Sizeof(windowClass{})), Proc: wndProc, Cursor: cursor, Background: 6, Name: class}
	atom, _, err := user.NewProc("RegisterClassExW").Call(uintptr(unsafe.Pointer(&c)))
	if atom == 0 && err != windows.ERROR_CLASS_ALREADY_EXISTS {
		return err
	}
	a.hwnd, _, err = create.Call(0, uintptr(unsafe.Pointer(class)), uintptr(unsafe.Pointer(windows.StringToUTF16Ptr("AntiPessimizer — Delphi Profiler"))), 0x10cf0000, 100, 100, 1200, 800, 0, 0, uintptr(instance), 0)
	if a.hwnd == 0 {
		return err
	}
	a.processes = child(a.hwnd, "COMBOBOX", "", 0x00200003|0x10000, 1)
	a.refresh = child(a.hwnd, "BUTTON", "Atualizar processos", 0x10000, 2)
	a.start = child(a.hwnd, "BUTTON", "Iniciar", 0x10000, 3)
	a.stop = child(a.hwnd, "BUTTON", "Parar", 0x10000, 4)
	a.demo = child(a.hwnd, "BUTTON", "Demo", 0x10000, 5)
	a.filter = child(a.hwnd, "EDIT", "", 0x00810080, 6)
	send.Call(a.filter, 0x1501, 0, uintptr(unsafe.Pointer(windows.StringToUTF16Ptr("Filtrar procedimento ou módulo"))))
	a.thread = child(a.hwnd, "EDIT", "0", 0x00810080, 7)
	a.seconds = child(a.hwnd, "EDIT", "10", 0x00810080, 8)
	a.status = child(a.hwnd, "STATIC", "Conectando ao collector...", 0, 9)
	a.table = child(a.hwnd, "SysListView32", "", 0x00811005, 10)
	send.Call(a.table, 0x1036, 0, 0x10021)
	for i, entry := range []struct {
		name  string
		width int32
	}{{"Amostras", 90}, {"Fração", 80}, {"Thread", 90}, {"Módulo", 180}, {"Procedimento", 420}, {"RVA", 160}, {"Símbolo", 100}} {
		col := column{Mask: 0x7, Width: entry.width, Text: windows.StringToUTF16Ptr(entry.name)}
		send.Call(a.table, 0x1061, uintptr(i), uintptr(unsafe.Pointer(&col)))
	}
	a.path = child(a.hwnd, "EDIT", "session.ndjson", 0x00810080, 11)
	a.save = child(a.hwnd, "BUTTON", "Salvar histórico", 0x10000, 12)
	a.replay = child(a.hwnd, "BUTTON", "Abrir arquivo", 0x10000, 13)
	a.compare = child(a.hwnd, "BUTTON", "Comparar arquivo", 0x10000, 14)
	a.details = child(a.hwnd, "STATIC", "Selecione um procedimento. Filtros: texto, TID (0=todos), últimos segundos.", 0, 15)
	a.layout()
	user.NewProc("SetTimer").Call(a.hwnd, 1, 250, 0)
	a.wg.Add(1)
	go a.poll()
	a.request(service.Request{Op: "processes"})
	if os.Getenv("AP_GUI_SMOKE") == "1" {
		user.NewProc("SetTimer").Call(a.hwnd, 2, 1500, 0)
	}
	defer func() { a.cancel(); a.wg.Wait() }()
	var m message
	for {
		r, _, err := user.NewProc("GetMessageW").Call(uintptr(unsafe.Pointer(&m)), 0, 0, 0)
		if int32(r) == -1 {
			return err
		}
		if r == 0 {
			break
		}
		if m.Message == 0x113 && m.WParam == 2 {
			destroy.Call(a.hwnd)
			continue
		}
		user.NewProc("TranslateMessage").Call(uintptr(unsafe.Pointer(&m)))
		user.NewProc("DispatchMessageW").Call(uintptr(unsafe.Pointer(&m)))
	}
	return nil
}
func (a *window) layout() {
	pos := func(h uintptr, x, y, w, hgt int32) {
		if h != 0 {
			move.Call(h, uintptr(x), uintptr(y), uintptr(w), uintptr(hgt), 1)
		}
	}
	w, h := a.width, a.height
	if w < 800 {
		w = 800
	}
	if h < 500 {
		h = 500
	}
	pos(a.processes, 12, 12, w-510, 300)
	pos(a.refresh, w-485, 12, 170, 28)
	pos(a.start, w-305, 12, 90, 28)
	pos(a.stop, w-205, 12, 90, 28)
	pos(a.demo, w-105, 12, 90, 28)
	pos(a.filter, 12, 52, w-300, 28)
	pos(a.thread, w-275, 52, 120, 28)
	pos(a.seconds, w-140, 52, 125, 28)
	pos(a.status, 12, 90, w-24, 42)
	pos(a.table, 12, 140, w-24, h-320)
	pos(a.details, 12, h-174, w-24, 44)
	pos(a.path, 12, h-52, w-480, 28)
	pos(a.save, w-455, h-52, 140, 28)
	pos(a.replay, w-305, h-52, 140, 28)
	pos(a.compare, w-155, h-52, 140, 28)
}
func (a *window) send(u update) {
	select {
	case a.updates <- u:
	case <-a.ctx.Done():
	}
}
func (a *window) request(q service.Request) {
	a.wg.Add(1)
	go func() {
		defer a.wg.Done()
		r, err := service.Call(a.ctx, a.address, q)
		a.send(update{Response: r, Error: err})
	}()
}
func (a *window) poll() {
	defer a.wg.Done()
	timer := time.NewTicker(500 * time.Millisecond)
	defer timer.Stop()
	for {
		select {
		case <-a.ctx.Done():
			return
		case <-timer.C:
			if !a.offline.Load() {
				r, err := service.Call(a.ctx, a.address, service.Request{Op: "latest"})
				a.send(update{Response: r, Error: err})
			}
		}
	}
}
func (a *window) command(id int) {
	switch id {
	case 2:
		a.request(service.Request{Op: "processes"})
	case 3:
		selected, _, _ := send.Call(a.processes, 0x147, 0, 0)
		if int(selected) < 0 || int(selected) >= len(a.targets) {
			text(a.status, "Selecione um processo.")
			return
		}
		a.offline.Store(false)
		a.request(service.Request{Op: "start", Start: service.StartOptions{PID: a.targets[selected].PID}})
	case 4:
		a.request(service.Request{Op: "stop"})
	case 5:
		a.offline.Store(false)
		a.request(service.Request{Op: "start", Start: service.StartOptions{Demo: true}})
	case 12, 13, 14:
		path := value(a.path)
		snapshot := append([]profiling.Snapshot(nil), a.model.History...)
		offline := a.offline.Load()
		a.wg.Add(1)
		go func() { defer a.wg.Done(); a.file(id, path, snapshot, offline) }()
	}
}
func (a *window) file(op int, path string, current []profiling.Snapshot, offline bool) {
	fail := func(err error) { a.send(update{Error: err}) }
	if op == 12 {
		if !offline {
			now := time.Now()
			r, err := service.Call(a.ctx, a.address, service.Request{Op: "history", From: now.Add(-120 * time.Second), To: now})
			if err != nil {
				fail(err)
				return
			}
			current = r.History
		}
		if len(current) == 0 {
			fail(fmt.Errorf("nenhum snapshot para salvar"))
			return
		}
		f, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
		if err != nil {
			fail(err)
			return
		}
		for _, s := range current {
			if err = stream.Write(f, s); err != nil {
				break
			}
		}
		if err == nil {
			err = f.Sync()
		}
		closeErr := f.Close()
		if err == nil {
			err = closeErr
		}
		if err != nil {
			os.Remove(path)
			fail(err)
			return
		}
		a.send(update{Note: "Histórico salvo: " + path})
		return
	}
	f, err := os.Open(path)
	if err != nil {
		fail(err)
		return
	}
	archive, err := report.Read(f)
	f.Close()
	if err != nil {
		fail(err)
		return
	}
	if op == 13 {
		a.offline.Store(true)
		a.send(update{History: archive, Offline: true, Note: "REPLAY: " + path})
		return
	}
	comparison, err := report.Compare(archive, current)
	if err != nil {
		fail(err)
		return
	}
	lines := []string{"Variação observada (atual - arquivo), amostras/s; não implica causalidade:"}
	for i, d := range comparison.Changes {
		if i >= 10 {
			break
		}
		lines = append(lines, fmt.Sprintf("%+.2f  %s!%s", d.DeltaPerSecond, d.Module, d.Procedure))
	}
	a.send(update{Note: strings.Join(lines, "\r\n")})
}
func (a *window) tick() {
	for draining := true; draining; {
		select {
		case u := <-a.updates:
			if u.Error != nil {
				text(a.status, "Erro / desconectado: "+u.Error.Error())
				continue
			}
			if u.Offline {
				a.model = Model{}
				for _, s := range u.History {
					a.model.Append(s)
				}
			}
			if u.Note != "" {
				text(a.details, u.Note)
			}
			if u.Response.Processes != nil {
				a.targets = u.Response.Processes
				send.Call(a.processes, 0x14b, 0, 0)
				for _, p := range a.targets {
					s := windows.StringToUTF16Ptr(fmt.Sprintf("%d — %s", p.PID, p.Name))
					send.Call(a.processes, 0x143, 0, uintptr(unsafe.Pointer(s)))
				}
				send.Call(a.processes, 0x14e, 0, 0)
			}
			if s := u.Response.Snapshot; s != nil && !a.offline.Load() {
				if err := a.model.Append(*s); err != nil {
					text(a.status, err.Error())
				}
			}
			if st := u.Response.Status; st != nil {
				text(a.status, fmt.Sprintf("%s | PID %d | %s | sampling | %s", strings.ToUpper(st.State), st.Target.PID, st.Target.Image, st.Error))
			}
		default:
			draining = false
		}
	}
	key := a.model.Session + "|" + value(a.filter) + "|" + value(a.thread) + "|" + value(a.seconds)
	if key != a.lastBuild || a.lastSequence != a.model.Last {
		seconds, err := strconv.Atoi(value(a.seconds))
		if err != nil || seconds < 1 || seconds > 120 {
			seconds = 10
		}
		tid, err := strconv.ParseUint(value(a.thread), 10, 32)
		if err != nil {
			tid = 0
		}
		if err := a.model.Rebuild(seconds, report.Filter{Text: value(a.filter), Thread: uint32(tid)}); err != nil {
			text(a.status, err.Error())
			return
		}
		a.lastBuild = key
		a.lastSequence = a.model.Last
		send.Call(a.table, 0x102f, uintptr(len(a.model.Rows)), 2)
		invalidate.Call(a.table, 0, 1)
		invalidate.Call(a.hwnd, 0, 0)
	}
}
func (a *window) draw() {
	var p paint
	dc, _, _ := user.NewProc("BeginPaint").Call(a.hwnd, uintptr(unsafe.Pointer(&p)))
	defer user.NewProc("EndPaint").Call(a.hwnd, uintptr(unsafe.Pointer(&p)))
	s := a.model.Summary
	label := fmt.Sprintf("%d amostras | residual %d | descartes %d | fila %d | tardias %d | atualizações omitidas %d | perda ETW desconhecida", s.Accepted, s.Other, s.Dropped, s.IngestLost, s.Late, a.model.Omitted)
	if s.Accepted < 20 {
		label += " | poucas amostras"
	}
	if a.offline.Load() {
		label = "REPLAY | " + label
	}
	chars := windows.StringToUTF16(label)
	gdi.NewProc("TextOutW").Call(dc, 12, uintptr(a.height-122), uintptr(unsafe.Pointer(&chars[0])), uintptr(len(chars)-1))
	history := a.model.History
	if len(history) > 120 {
		history = history[len(history)-120:]
	}
	var maximum uint64
	for _, v := range history {
		if v.Quality.Accepted > maximum {
			maximum = v.Quality.Accepted
		}
	}
	if len(history) > 1 && maximum > 0 {
		points := make([]point, len(history))
		for i, v := range history {
			points[i] = point{12 + int32(i)*(a.width-24)/int32(len(history)-1), a.height - 64 - int32(float64(v.Quality.Accepted)/float64(maximum)*30)}
		}
		gdi.NewProc("Polyline").Call(dc, uintptr(unsafe.Pointer(&points[0])), uintptr(len(points)))
	}
}
