package desktop

import (
	"context"
	"fmt"
	"github.com/gabrielteiga/AntiPessimizerV2/internal/instrumentation"
	"github.com/gabrielteiga/AntiPessimizerV2/internal/profiling"
	"github.com/gabrielteiga/AntiPessimizerV2/internal/service"
	"github.com/gabrielteiga/AntiPessimizerV2/internal/transport"
	"golang.org/x/sys/windows"
	"os"
	"testing"
	"time"
	"unsafe"
)

func TestNativeLayouts(t *testing.T) {
	for name, v := range map[string][2]uintptr{"class": {unsafe.Sizeof(windowClass{}), 80}, "message": {unsafe.Sizeof(message{}), 48}, "item": {unsafe.Sizeof(item{}), 88}, "display": {unsafe.Sizeof(displayInfo{}), 112}, "paint": {unsafe.Sizeof(paint{}), 72}} {
		if v[0] != v[1] {
			t.Fatalf("%s: %d != %d", name, v[0], v[1])
		}
	}
}

func TestNativeInstrumentedWindowSmoke(t *testing.T) {
	if os.Getenv("AP_GUI_SMOKE") != "1" {
		t.Skip("enabled in Windows CI")
	}
	path := t.TempDir() + "/instrumented.ndjson"
	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	for _, r := range metricRecords(t) {
		if err := instrumentation.WriteRecord(f, r); err != nil {
			f.Close()
			t.Fatal(err)
		}
	}
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}
	if err := runArchive(`\\.\pipe\unused-offline-metrics`, path, func(w *window) error {
		if len(w.model.Rows) != 2 || w.tableColumns != 13 || w.model.Rows[0][0] != "2" || w.model.Rows[0][1] != "30.000000" || w.model.Rows[0][2] != "15.000000" || w.model.Rows[0][3] != "40.000000" || w.model.Rows[0][4] != "20.000000" {
			return fmt.Errorf("instrumented metrics did not reach native table: %v", w.model.Rows)
		}
		if !w.offline.Load() || len(w.model.History) != 0 {
			return fmt.Errorf("sampling state contaminated metrics replay")
		}
		count, _, _ := send.Call(w.table, 0x1004, 0, 0)  // LVM_GETITEMCOUNT
		header, _, _ := send.Call(w.table, 0x101f, 0, 0) // LVM_GETHEADER
		columns, _, _ := send.Call(header, 0x1200, 0, 0) // HDM_GETITEMCOUNT
		var buffer [64]uint16
		// LVS_OWNERDATA does not support LVM_GETITEMTEXT. Exercise the native
		// LVN_GETDISPINFOW request the virtual control uses when painting a cell.
		cell := displayInfo{Header: notifyHeader{Window: w.table, Code: -177}, Item: item{Mask: 1, Index: 0, Sub: 2, Text: &buffer[0], Max: int32(len(buffer))}}
		send.Call(w.hwnd, 0x004e, 0, uintptr(unsafe.Pointer(&cell)))
		if count != 2 || columns != 13 || windows.UTF16ToString(buffer[:]) != "15.000000" {
			return fmt.Errorf("native controls did not expose metrics: rows=%d columns=%d average=%q", count, columns, windows.UTF16ToString(buffer[:]))
		}
		text(w.filter, "child")
		w.tick()
		if len(w.model.Rows) != 1 || w.model.Rows[0][8] != "Lab.Child" {
			return fmt.Errorf("native text filter ignored")
		}
		text(w.filter, "")
		w.tick()
		send.Call(w.cursor, 0x405, 1, 0) // TBM_SETPOS: first cumulative snapshot
		send.Call(w.hwnd, 0x0114, 0, w.cursor)
		w.tick()
		if w.model.Rows[0][0] != "1" || w.model.Rows[0][3] != "30.000000" {
			return fmt.Errorf("native cursor added cumulative totals: %v", w.model.Rows)
		}
		records := w.model.Metrics
		start := records[0].Target.Start
		sample := profiling.Snapshot{SchemaVersion: 1, SessionID: "sampling-replay", Sequence: 1, Target: records[0].Target, Mode: "sampling", Source: "synthetic", WindowStart: start, WindowEnd: start.Add(time.Second), Published: start.Add(time.Second)}
		if err := w.model.Replace([]profiling.Snapshot{sample}); err != nil {
			return err
		}
		w.lastBuild = ""
		w.tick()
		columns, _, _ = send.Call(header, 0x1200, 0, 0)
		if columns != 8 {
			return fmt.Errorf("sampling retained instrumentation columns: %d", columns)
		}
		if err := w.model.ReplaceMetrics(records); err != nil {
			return err
		}
		w.lastBuild = ""
		w.tick()
		columns, _, _ = send.Call(header, 0x1200, 0, 0)
		if columns != 13 {
			return fmt.Errorf("instrumentation columns not restored: %d", columns)
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
}

func TestInstrumentedArchiveSave(t *testing.T) {
	w := &window{ctx: context.Background(), updates: make(chan update, 1)}
	path := t.TempDir() + "/saved.ndjson"
	w.file(12, path, nil, metricRecords(t), true)
	if u := <-w.updates; u.Error != nil {
		t.Fatal(u.Error)
	}
	f, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	records, err := instrumentation.ReadMetrics(f)
	f.Close()
	if err != nil || len(records) != 2 || records[1].Metrics.Rows[0].Calls != 2 {
		t.Fatalf("invalid saved archive: %v %v", records, err)
	}
	w.file(12, path, nil, nil, true)
	if u := <-w.updates; u.Error == nil {
		t.Fatal("empty archive accepted")
	}
	w.file(12, path, nil, records, true)
	if u := <-w.updates; u.Error == nil {
		t.Fatal("existing archive overwritten")
	}
}
func TestNativeWindowSmoke(t *testing.T) {
	if os.Getenv("AP_GUI_SMOKE") != "1" {
		t.Skip("enabled in Windows CI")
	}
	address := fmt.Sprintf(`\\.\pipe\AntiPessimizer-gui-test-%d`, time.Now().UnixNano())
	listener, err := transport.Listen(address)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	controller := service.New()
	done := make(chan error, 1)
	go func() { done <- service.Serve(ctx, listener, controller) }()
	defer func() {
		cancel()
		if err := <-done; err != nil {
			t.Error(err)
		}
	}()
	if err := controller.Start(ctx, service.StartOptions{Demo: true, WindowMS: 50}); err != nil {
		t.Fatal(err)
	}
	if err := run(address, func(w *window) error {
		if len(w.model.Rows) == 0 || w.model.Summary.Accepted == 0 {
			return fmt.Errorf("native viewer did not consume service snapshots")
		}
		if w.model.Omitted != 0 {
			return fmt.Errorf("viewer skipped retained windows: %d", w.model.Omitted)
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
}
