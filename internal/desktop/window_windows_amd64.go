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

	"github.com/gabrielteiga/AntiPessimizerV2/internal/instrumentation"
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
	Metrics    []instrumentation.Record
	Live       bool
	Comparison *report.Comparison
	Response   service.Response
	Error      error
	History    []profiling.Snapshot
	Offline    bool
	Note       string
}
type window struct {
	followPath    string
	followCancel  context.CancelFunc
	metricUpdates chan metricFollowUpdate

	inspect                                                                                                                   func(*window) error // smoke-test observation on the UI thread before destruction
	inspection                                                                                                                error
	initialPath                                                                                                               string
	tableColumns                                                                                                              int
	threads, cursor                                                                                                           uintptr
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
	case 0x0114: // WM_HSCROLL: browse retained/archive windows
		if l == a.cursor && a.model.HistoryLength() > 0 {
			pos, _, _ := send.Call(a.cursor, 0x400, 0, 0)
			if int(pos) < a.model.HistoryLength() {
				a.model.At = a.model.SequenceAt(int(pos))
				if int(pos) == a.model.HistoryLength()-1 {
					a.model.At = 0
					a.model.Comparison = nil
				}
				a.lastBuild = ""
			}
		}
		return 0
	case 0x004e: // WM_NOTIFY, virtual ListView text and selection
		header := (*notifyHeader)(nativeParam)
		if header.Window == a.table && header.Code == -108 { // LVN_COLUMNCLICK
			clicked := (*struct {
				Header     notifyHeader
				Index, Sub int32
			})(nativeParam)
			if clicked.Sub >= 0 && int(clicked.Sub) < len(a.model.Headers()) {
				if a.model.SortColumn == int(clicked.Sub) {
					a.model.Ascending = !a.model.Ascending
				} else {
					a.model.SortColumn = int(clicked.Sub)
					a.model.Ascending = clicked.Sub >= 2
					if len(a.model.Metrics) > 0 {
						a.model.Ascending = clicked.Sub >= 7 && clicked.Sub <= 10
					}
				}
				a.lastBuild = ""
			}
			return 0
		}
		if header.Window == a.table && header.Code == -3 && a.model.Threads { // double-click a thread
			selected, _, _ := send.Call(a.table, 0x100c, ^uintptr(0), 2)
			if int(selected) >= 0 && int(selected) < len(a.model.Rows) {
				text(a.thread, a.model.Rows[selected][2])
				a.model.Threads = false
				text(a.threads, "Threads")
				a.lastBuild = ""
			}
			return 0
		}
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
func Run(address string) error                              { return run(address, nil) }
func RunArchive(address, path string) error                 { return runArchive(address, path, nil) }
func run(address string, inspect func(*window) error) error { return runArchive(address, "", inspect) }
func RunFollow(address, path string) error                  { return runViewer(address, "", path, nil) }
func runArchive(address, path string, inspect func(*window) error) error {
	return runViewer(address, path, "", inspect)
}
func runViewer(address, path, follow string, inspect func(*window) error) error {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	a := &window{followPath: follow, metricUpdates: make(chan metricFollowUpdate, 1), inspect: inspect, initialPath: path, address: address, ctx: ctx, cancel: cancel, updates: make(chan update, 8), width: 1180, height: 760}
	a.offline.Store(path != "" || follow != "")
	active = a
	defer func() { active = nil }()
	windows.NewLazySystemDLL("comctl32.dll").NewProc("InitCommonControls").Call()
	if err := a.open(); err != nil {
		return err
	}
	return nil
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
	a.syncColumns()
	a.path = child(a.hwnd, "EDIT", "session.ndjson", 0x00810080, 11)
	a.save = child(a.hwnd, "BUTTON", "Salvar histórico", 0x10000, 12)
	a.replay = child(a.hwnd, "BUTTON", "Abrir arquivo", 0x10000, 13)
	a.compare = child(a.hwnd, "BUTTON", "Comparar arquivo", 0x10000, 14)
	a.threads = child(a.hwnd, "BUTTON", "Threads", 0x10000, 16)
	a.cursor = child(a.hwnd, "msctls_trackbar32", "", 0x10000, 17)
	a.details = child(a.hwnd, "STATIC", "Selecione um procedimento. Filtros: texto, TID (0=todos), últimos segundos.", 0, 15)
	a.layout()
	user.NewProc("SetTimer").Call(a.hwnd, 1, 250, 0)
	if a.followPath != "" {
		text(a.path, a.followPath)
		ctx, cancel := context.WithCancel(a.ctx)
		a.followCancel = cancel
		a.wg.Add(1)
		go func() { defer a.wg.Done(); followMetrics(ctx, a.followPath, a.metricUpdates) }()
		for _, h := range []uintptr{a.processes, a.refresh, a.start, a.demo, a.replay} {
			user.NewProc("EnableWindow").Call(h, 0)
		}
		text(a.stop, "Desconectar")
		text(a.status, "Aguardando contagens no arquivo...")
	} else {
		a.wg.Add(1)
		go a.poll()
		if a.initialPath != "" {
			text(a.path, a.initialPath)
			a.command(13)
		} else {
			a.request(service.Request{Op: "processes"})
		}
	}
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
			if a.inspect != nil {
				a.inspection = a.inspect(a)
			}
			destroy.Call(a.hwnd)
			continue
		}
		user.NewProc("TranslateMessage").Call(uintptr(unsafe.Pointer(&m)))
		user.NewProc("DispatchMessageW").Call(uintptr(unsafe.Pointer(&m)))
	}
	return a.inspection
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
	pos(a.processes, 12, 12, w-610, 300)
	pos(a.threads, w-590, 12, 95, 28)
	pos(a.refresh, w-485, 12, 170, 28)
	pos(a.start, w-305, 12, 90, 28)
	pos(a.stop, w-205, 12, 90, 28)
	pos(a.demo, w-105, 12, 90, 28)
	pos(a.filter, 12, 52, w-300, 28)
	pos(a.thread, w-275, 52, 120, 28)
	pos(a.seconds, w-140, 52, 125, 28)
	pos(a.status, 12, 90, w-24, 42)
	pos(a.table, 12, 140, w-24, h-350)
	pos(a.cursor, 12, h-205, w-24, 25)
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
		a.send(update{Response: r, Error: err, Live: true})
	}()
}
func (a *window) poll() {
	var session string
	var last uint64
	var previousEnd time.Time
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
				if err != nil {
					a.send(update{Error: err, Live: true})
					continue
				}
				if latest := r.Snapshot; latest != nil && (latest.SessionID != session || latest.Sequence > last) {
					from := previousEnd
					if latest.SessionID != session || from.IsZero() {
						from = latest.WindowEnd.Add(-120 * time.Second)
					}
					history, e := service.Call(a.ctx, a.address, service.Request{Op: "history", From: from, To: latest.WindowEnd})
					if e != nil {
						a.send(update{Error: e, Live: true})
						continue
					}
					if len(history.History) > 0 && history.History[0].SessionID == latest.SessionID {
						a.send(update{Response: r, History: history.History, Live: true})
						session = latest.SessionID
						last = latest.Sequence
						previousEnd = latest.WindowEnd
						continue
					}
				}
				a.send(update{Response: r, Live: true})
			}
		}
	}
}
func (a *window) command(id int) {
	if a.followPath != "" {
		if id == 2 || id == 3 || id == 5 {
			return
		}
		if id == 4 {
			if a.followCancel != nil {
				a.followCancel()
				a.followCancel = nil
			}
			text(a.status, "Acompanhamento parado. O coletor continua independente.")
			user.NewProc("EnableWindow").Call(a.stop, 0)
			user.NewProc("EnableWindow").Call(a.replay, 1)
			return
		}
		if id == 13 && a.followCancel != nil {
			return
		}
	}
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
		a.model.At = 0
		a.model.Comparison = nil
		if len(a.model.Metrics) > 0 {
			a.model = Model{}
			a.lastBuild = ""
		}
		a.request(service.Request{Op: "start", Start: service.StartOptions{PID: a.targets[selected].PID}})
	case 4:
		a.request(service.Request{Op: "stop"})
	case 5:
		a.offline.Store(false)
		a.model.At = 0
		a.model.Comparison = nil
		if len(a.model.Metrics) > 0 {
			a.model = Model{}
			a.lastBuild = ""
		}
		a.request(service.Request{Op: "start", Start: service.StartOptions{Demo: true}})
	case 16:
		if len(a.model.Metrics) > 0 {
			return
		}
		a.model.Comparison = nil
		a.model.Threads = !a.model.Threads
		a.lastBuild = ""
		if a.model.Threads {
			text(a.threads, "Procedimentos")
		} else {
			text(a.threads, "Threads")
		}
	case 12, 13, 14:
		path := value(a.path)
		snapshot := append([]profiling.Snapshot(nil), a.model.History...)
		metrics := append([]instrumentation.Record(nil), a.model.Metrics...)
		if id == 14 && len(metrics) > 0 {
			text(a.status, "Comparação disponível apenas para arquivos de amostragem.")
			return
		}
		offline := a.offline.Load()
		a.wg.Add(1)
		go func() { defer a.wg.Done(); a.file(id, path, snapshot, metrics, offline) }()
	}
}
func (a *window) file(op int, path string, current []profiling.Snapshot, metrics []instrumentation.Record, offline bool) {
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
		if len(current) == 0 && len(metrics) == 0 {
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
		if len(metrics) > 0 {
			for _, r := range metrics {
				if err = instrumentation.WriteRecord(f, r); err != nil {
					break
				}
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
	archive, instrumented, err := readArchive(f)
	f.Close()
	if err != nil {
		fail(err)
		return
	}
	if op == 13 {
		a.offline.Store(true)
		a.send(update{History: archive, Metrics: instrumented, Offline: true, Note: "REPLAY: " + path})
		return
	}
	if len(instrumented) > 0 {
		fail(fmt.Errorf("comparação exige dois arquivos de amostragem"))
		return
	}
	comparison, err := report.Compare(archive, current)
	if err != nil {
		fail(err)
		return
	}
	a.offline.Store(true)
	a.send(update{Comparison: &comparison, Note: "Comparação: arquivo → série atual. Δ em amostras/s; variação observada, não causalidade."})
}
func (a *window) tick() {
	if a.followPath != "" && a.followCancel != nil {
		select {
		case u := <-a.metricUpdates:
			if u.record.Protocol != "" && (len(a.model.Metrics) == 0 || u.record.Sequence > a.model.Last) {
				first := len(a.model.Metrics) == 0
				if err := a.model.AppendMetrics(u.record); err != nil {
					text(a.status, "Erro no acompanhamento: "+err.Error())
				} else {
					if first && u.firstSequence > 0 {
						a.model.Omitted = u.record.Sequence - u.firstSequence
					}
					state := "ACOMPANHANDO CHAMADAS"
					if u.record.Metrics.Incomplete {
						state += " | DADOS INCOMPLETOS"
					}
					text(a.status, fmt.Sprintf("%s | PID %d | %s | atualizações omitidas=%d", state, u.record.Target.PID, u.record.Target.Image, a.model.Omitted))
				}
			}
			if u.err != nil {
				text(a.status, "Erro no acompanhamento: "+u.err.Error())
			}
			if u.stopped {
				a.followCancel()
				a.followCancel = nil
				user.NewProc("EnableWindow").Call(a.stop, 0)
				user.NewProc("EnableWindow").Call(a.replay, 1)
			}
		default:
		}
	}

	for draining := true; draining; {
		select {
		case u := <-a.updates:
			if u.Live && a.offline.Load() && u.Response.Processes == nil {
				continue
			}
			if u.Error != nil {
				text(a.status, "Erro / desconectado: "+u.Error.Error())
				continue
			}
			if u.Comparison != nil {
				a.model.Comparison = u.Comparison
				a.model.Threads = false
				a.model.SortColumn = 2
				a.model.Ascending = false
				a.lastBuild = ""
				text(a.status, "COMPARAÇÃO — arquivo versus série atual")
			}
			if u.Offline {
				a.model = Model{}
				var err error
				if len(u.Metrics) > 0 {
					err = a.model.ReplaceMetrics(u.Metrics)
				} else {
					err = a.model.Replace(u.History)
				}
				if err != nil {
					text(a.status, err.Error())
					continue
				}
				text(a.status, "REPLAY — use a barra para navegar pelos snapshots")
				if r, ok := a.model.CurrentMetrics(); ok {
					text(a.status, fmt.Sprintf("REPLAY | instrumentação | %s | PID %d | %s", r.Source, r.Target.PID, r.Target.Image))
				}
				a.lastBuild = ""
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
			if !a.offline.Load() && len(u.History) > 0 {
				for _, s := range u.History {
					if err := a.model.Append(s); err != nil {
						text(a.status, err.Error())
						break
					}
				}
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
		a.syncColumns()
		enabled := uintptr(1)
		if len(a.model.Metrics) > 0 {
			enabled = 0
		}
		for _, h := range []uintptr{a.seconds, a.threads, a.compare} {
			user.NewProc("EnableWindow").Call(h, enabled)
		}
		if a.model.Threads {
			text(a.threads, "Procedimentos")
		} else {
			text(a.threads, "Threads")
		}
		a.lastBuild = key
		a.lastSequence = a.model.Last
		if count := a.model.HistoryLength(); count > 0 {
			send.Call(a.cursor, 0x408, 0, uintptr(count-1)) // TBM_SETRANGEMAX
			pos := count - 1
			if a.model.At != 0 {
				for i := 0; i < count; i++ {
					if a.model.SequenceAt(i) >= a.model.At {
						pos = i
						break
					}
				}
			}
			send.Call(a.cursor, 0x405, 1, uintptr(pos))
		}
		send.Call(a.table, 0x102f, uintptr(len(a.model.Rows)), 2)
		invalidate.Call(a.table, 0, 1)
		invalidate.Call(a.hwnd, 0, 0)
	}
}
func (a *window) draw() {
	var p paint
	dc, _, _ := user.NewProc("BeginPaint").Call(a.hwnd, uintptr(unsafe.Pointer(&p)))
	defer user.NewProc("EndPaint").Call(a.hwnd, uintptr(unsafe.Pointer(&p)))
	if r, ok := a.model.CurrentMetrics(); ok {
		mode := "REPLAY"
		if a.followPath != "" {
			mode = "ARQUIVO"
			if a.followCancel != nil {
				mode = "ACOMPANHANDO"
			}
		}
		label := fmt.Sprintf("%s | instrumentação | %s | snapshot %d | Clear %d | abertas %d | abandonadas %d | incompleto=%t | omitidos %d", mode, r.Source, r.Sequence, r.Metrics.Epoch, r.Metrics.OpenCalls, r.Metrics.AbandonedCalls, r.Metrics.Incomplete, a.model.Omitted)
		chars := windows.StringToUTF16(label)
		gdi.NewProc("TextOutW").Call(dc, 12, uintptr(a.height-122), uintptr(unsafe.Pointer(&chars[0])), uintptr(len(chars)-1))
		return
	}
	s := a.model.Summary
	label := fmt.Sprintf("%d amostras | residual %d | descartes %d | fila %d | tardias %d | atualizações omitidas %d", s.Accepted, s.Other, s.Dropped, s.IngestLost, s.Late, a.model.Omitted)
	if len(a.model.History) > 0 {
		q := a.model.History[len(a.model.History)-1].Quality
		if q.ETWLossKnown {
			label += fmt.Sprintf(" | ETW global: %d eventos / %d buffers perdidos", q.ETWEventsLost, q.ETWBuffersLost)
		} else if a.model.Summary.Source == "external" {
			label += " | perdas ETW desconhecidas"
		}
		if q.CorrelationUncertain {
			label += " | CORRELAÇÃO INCERTA"
		}
	}
	if s.Accepted < 20 {
		label += " | poucas amostras"
	}
	if a.offline.Load() {
		label = "REPLAY | " + label
	}
	chars := windows.StringToUTF16(label)
	gdi.NewProc("TextOutW").Call(dc, 12, uintptr(a.height-122), uintptr(unsafe.Pointer(&chars[0])), uintptr(len(chars)-1))
	history := a.model.History
	if a.model.At != 0 {
		for i, s := range history {
			if s.Sequence > a.model.At {
				history = history[:i]
				break
			}
		}
	}
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

func (a *window) syncColumns() {
	headers := a.model.Headers()
	changedMode := a.tableColumns != len(headers)
	widths := []int32{90, 80, 90, 180, 420, 160, 100, 180}
	if len(a.model.Metrics) > 0 {
		widths = []int32{100, 140, 170, 140, 170, 90, 160, 180, 420, 160, 180, 90, 130}
	}
	for a.tableColumns > len(headers) {
		send.Call(a.table, 0x101c, uintptr(a.tableColumns-1), 0)
		a.tableColumns--
	}
	for i, name := range headers {
		col := column{Mask: 4, Text: windows.StringToUTF16Ptr(name)}
		if changedMode {
			col.Mask = 7
			col.Width = widths[i]
		}
		if i >= a.tableColumns {
			send.Call(a.table, 0x1061, uintptr(i), uintptr(unsafe.Pointer(&col)))
			a.tableColumns++
		} else {
			send.Call(a.table, 0x1060, uintptr(i), uintptr(unsafe.Pointer(&col)))
		}
	}
}
