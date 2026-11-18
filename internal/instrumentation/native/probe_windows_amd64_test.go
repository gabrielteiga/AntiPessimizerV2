package native_test

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/binary"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"syscall"
	"testing"
	"time"
	"unsafe"

	"github.com/gabrielteiga/AntiPessimizerV2/internal/instrumentation"
	"github.com/gabrielteiga/AntiPessimizerV2/internal/instrumentation/native"
	"github.com/gabrielteiga/AntiPessimizerV2/internal/instrumentation/native/fixture"
	"github.com/gabrielteiga/AntiPessimizerV2/internal/instrumentation/x64"
	"github.com/gabrielteiga/AntiPessimizerV2/internal/profiling"
	"github.com/gabrielteiga/AntiPessimizerV2/internal/transport"
	"golang.org/x/sys/windows"
)

// This is an explicitly prepared private assembly fixture, not an installer for
// arbitrary procedures. Its entry/exit addresses are configured while quiescent.
func TestNativeProducerGeneratedProbesEndToEnd(t *testing.T) {
	t.Run("prepared-leaf", func(t *testing.T) { nativeProbeCapture(t, false) })
	t.Run("relocated-leaf", func(t *testing.T) { nativeProbeCapture(t, true) })
}

func nativeProbeCapture(t *testing.T, relocate bool) {
	lab := os.Getenv("AP_NATIVE_LAB")
	if lab == "" {
		t.Skip("requires the native DLL built in Windows CI")
	}
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	dllPath := filepath.Join(filepath.Dir(lab), "AntiPessimizerNative.dll")
	dll, err := windows.LoadLibraryEx(dllPath, 0, windows.LOAD_LIBRARY_SEARCH_DLL_LOAD_DIR|windows.LOAD_LIBRARY_SEARCH_SYSTEM32)
	if err != nil {
		t.Fatal(err)
	}
	canUnload := true
	defer func() {
		if canUnload {
			windows.FreeLibrary(dll)
		}
	}()
	proc := func(name string) uintptr {
		t.Helper()
		p, err := windows.GetProcAddress(dll, name)
		if err != nil {
			t.Fatal(err)
		}
		return p
	}
	initProc, enterProc, exitProc := proc("APNativeInit"), proc("APNativeEnter"), proc("APNativeExit")
	endProc, shutdownProc := proc("APNativeThreadEnd"), proc("APNativeShutdown")
	var module windows.Handle
	if err := windows.GetModuleHandleEx(2, nil, &module); err != nil {
		t.Fatal(err)
	}
	entry, body := fixture.Addresses()
	if entry <= uintptr(module) || uint64(entry-uintptr(module)) > 1<<32-1 {
		t.Fatal("private native fixture is outside the image RVA range")
	}
	rva := uint32(entry - uintptr(module))
	code, err := windows.VirtualAlloc(0, 4096, windows.MEM_RESERVE|windows.MEM_COMMIT, windows.PAGE_READWRITE)
	if err != nil {
		t.Fatal(err)
	}
	nt := windows.NewLazySystemDLL("ntdll.dll")
	registered := false
	defer func() {
		fixture.SetProbes(0, 0)
		if registered {
			if ok, _, err := nt.NewProc("RtlDeleteFunctionTable").Call(code + 1024); ok == 0 {
				t.Error("retire native bridge unwind table", err)
				return
			}
		}
		windows.VirtualFree(code, 0, windows.MEM_RELEASE)
	}()
	write := func(address uintptr, data []byte) {
		t.Helper()
		var n uintptr
		if err := windows.WriteProcessMemory(windows.CurrentProcess(), address, &data[0], uintptr(len(data)), &n); err != nil || n != uintptr(len(data)) {
			t.Fatalf("write native probe: %d %v", n, err)
		}
	}
	var functions [2]x64.RuntimeFunction
	var leaf x64.LeafPlan
	wantReturn := uintptr(90)
	if relocate {
		// An ordinary native leaf with its original RET, in private memory.
		// Only the entry prefix is patched; the clone routes RET to the exit.
		original := append(bytes.Repeat([]byte{0x90}, 16), []byte{
			0x41, 0xba, 0, 0x10, 0, 0, // mov r10d,4096
			0x49, 0x83, 0xea, 1, 0x75, 0xfa, // sub r10,1; jne back
			0x48, 0x8d, 0x04, 0x11, // lea rax,[rcx+rdx]
			0x4c, 0x01, 0xc0, 0x4c, 0x01, 0xc8, // add rax,r8; add rax,r9
			0x48, 0x03, 0x44, 0x24, 0x28, // add rax,[rsp+40]
			0x48, 0x03, 0x44, 0x24, 0x30, 0xc3, // add rax,[rsp+48]; ret
		}...)
		// Read a constant beyond the source function through RIP. Its cloned
		// displacement must change while retaining the same data address.
		original = append(original[:len(original)-1], 0x48, 0x03, 0x05, 0, 0, 0, 0, 0xc3)
		binary.LittleEndian.PutUint32(original[len(original)-5:], uint32(3072-(2048+len(original)-1)))
		var constant [8]byte
		binary.LittleEndian.PutUint64(constant[:], 35)
		write(code+3072, constant[:])
		wantReturn += 35
		leaf, err = x64.BuildLeaf(original, uint64(code+2048), uint64(code), uint64(enterProc), uint64(exitProc), 1)
		if err != nil {
			t.Fatal(err)
		}
		write(code+2048, original)
		write(code+2048, leaf.Patch)
		write(code, leaf.Image)
		functions = leaf.Functions
		fixture.SetProbes(code+2048, 0)
	} else {
		in, err := x64.BuildEntryProbe(uint64(code), uint64(enterProc), uint64(body), 1)
		if err != nil {
			t.Fatal(err)
		}
		out, err := x64.BuildExitProbe(uint64(code+512), uint64(exitProc), 1)
		if err != nil {
			t.Fatal(err)
		}
		write(code, in.Image)
		write(code+512, out.Image)
		functions = [2]x64.RuntimeFunction{{Begin: 0, End: in.CodeSize, Unwind: in.UnwindRVA}, {Begin: 512, End: 512 + out.CodeSize, Unwind: 512 + out.UnwindRVA}}
		fixture.SetProbes(code, code+512)
	}
	var table [24]byte
	for i, f := range functions {
		binary.LittleEndian.PutUint32(table[i*12:], f.Begin)
		binary.LittleEndian.PutUint32(table[i*12+4:], f.End)
		binary.LittleEndian.PutUint32(table[i*12+8:], f.Unwind)
	}
	write(code+1024, table[:])
	var old uint32
	if err := windows.VirtualProtect(code, 4096, windows.PAGE_EXECUTE_READ, &old); err != nil {
		t.Fatal(err)
	}
	if ok, _, err := windows.NewLazySystemDLL("kernel32.dll").NewProc("FlushInstructionCache").Call(uintptr(windows.CurrentProcess()), code, 4096); ok == 0 {
		t.Fatal(err)
	}
	if ok, _, err := nt.NewProc("RtlAddFunctionTable").Call(code+1024, 2, code); ok == 0 {
		t.Fatal(err)
	}
	registered = true

	var nonce [16]byte
	if _, err := rand.Read(nonce[:]); err != nil {
		t.Fatal(err)
	}
	address := `\\.\pipe\AntiPessimizer-Native-Bridge-` + hex.EncodeToString(nonce[:])
	listener, err := transport.Listen(address)
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	stopListener := context.AfterFunc(ctx, func() { listener.Close() })
	defer stopListener()
	pipeName := windows.StringToUTF16Ptr(address)
	catalog := [2]uint32{1, rva}
	initDone := make(chan struct{})
	var initialized uintptr
	var initErr error
	canUnload = false
	go func() {
		initialized, _, initErr = syscall.SyscallN(initProc, uintptr(unsafe.Pointer(pipeName)), uintptr(unsafe.Pointer(&catalog[0])), 1, 1024)
		runtime.KeepAlive(pipeName)
		runtime.KeepAlive(&catalog)
		close(initDone)
		if initialized == 0 {
			listener.Close()
		}
	}()
	// Closing the peer unblocks startup/drain before waiting for DLL retirement.
	running := false
	defer func() {
		listener.Close()
		<-initDone
		if !canUnload {
			if running || initialized != 0 {
				syscall.SyscallN(endProc)
			}
			ok, _, _ := syscall.SyscallN(shutdownProc, 5000)
			canUnload = ok != 0
			if !canUnload {
				t.Error("native bridge did not finish shutdown; keeping DLL loaded")
			}
		}
	}()
	// The listener initially reserves the name without a connectable instance.
	// Force Init to start before Accept to exercise ERROR_PIPE_BUSY startup.
	select {
	case <-initDone:
		t.Fatalf("Init returned before the server accepted: %v", initErr)
	case <-time.After(50 * time.Millisecond):
	}
	conn, err := listener.Accept()
	if err != nil {
		<-initDone
		t.Fatalf("native accept: %v; Init=%d, error=%v, selected RVA=%#x", err, initialized, initErr, rva)
	}
	defer conn.Close()
	stopConn := context.AfterFunc(ctx, func() { conn.Close() })
	defer stopConn()
	reader, err := native.NewReader(conn, uint32(os.Getpid()))
	if err != nil {
		t.Fatal(err)
	}
	if len(reader.Header.Procedures) != 1 || reader.Header.Procedures[0].RVA != rva {
		t.Fatal("native DLL changed the selected assembly procedure")
	}
	engine, err := instrumentation.New(instrumentation.Config{Frequency: reader.Header.Frequency, MaxThreads: 4, MaxDepth: 16, MaxRows: 4}, []instrumentation.Procedure{{ID: 1, Location: profiling.Location{ModuleID: "controlled-native-asm-v1", Module: filepath.Base(os.Args[0]), Procedure: "Fixture.Sum", RVA: fmt.Sprintf("0x%016x", rva), Status: "resolved"}}})
	if err != nil {
		t.Fatal(err)
	}
	type result struct {
		snapshot instrumentation.Snapshot
		err      error
	}
	received := make(chan result, 1)
	completed := make(chan uint64, 1)
	receiveDone := make(chan struct{})
	go func() {
		defer close(receiveDone)
		defer conn.Close()
		var calls uint64
		for {
			e, err := reader.Next()
			if err == io.EOF {
				s, err := engine.Snapshot()
				received <- result{s, err}
				return
			}
			if err != nil {
				received <- result{err: err}
				return
			}
			switch e.Type {
			case "thread_start":
				err = engine.StartThread(e.Thread, e.Tick)
			case "enter":
				err = engine.Enter(e.Thread, e.Procedure, e.Tick)
			case "exit":
				err = engine.Exit(e.Thread, e.Procedure, e.Tick)
			case "thread_end":
				err = engine.EndThread(e.Thread, e.Tick)
			default:
				err = fmt.Errorf("unexpected assembly fixture event: %s", e.Type)
			}
			if err != nil {
				received <- result{err: err}
				return
			}
			if e.Type == "exit" {
				calls++
				completed <- calls
			}
		}
	}()
	defer func() {
		conn.Close()
		<-receiveDone
	}()
	if n, err := conn.Write([]byte{1}); err != nil || n != 1 {
		t.Fatal("native startup ACK", err)
	}
	<-initDone
	if initialized == 0 {
		t.Fatal("native DLL did not initialize", initErr)
	}
	running = true
	for i := uintptr(0); i < 10; i++ {
		if got, _, _ := syscall.SyscallN(entry, 7+i, 11, 13, 17, 19, 23); got != wantReturn+i {
			t.Fatalf("real native helper changed arguments/return: %d", got)
		}
		// Receive each completed call while capture is still open. Cooperative
		// draining waits indefinitely when idle, so a missing publication wakeup
		// cannot be masked by polling or by Shutdown's final drain.
		select {
		case calls := <-completed:
			if calls != uint64(i+1) {
				t.Fatal("live native completion changed count", calls)
			}
		case capture := <-received:
			t.Fatal("native stream ended before live completion", capture.err)
		case <-ctx.Done():
			t.Fatal("native publication did not wake the drain worker", ctx.Err())
		}
	}
	syscall.SyscallN(endProc)
	ok, _, _ := syscall.SyscallN(shutdownProc, 5000)
	running = false
	canUnload = ok != 0
	if !canUnload {
		t.Fatal("native producer shutdown failed")
	}
	capture := <-received
	if capture.err != nil {
		t.Fatal(capture.err)
	}
	s := capture.snapshot
	if s.Incomplete || s.OpenCalls != 0 || s.AbandonedCalls != 0 || len(s.Rows) != 1 {
		t.Fatal("incomplete native probe capture", s)
	}
	row := s.Rows[0]
	if row.Calls != 10 || row.ExclusiveTicks != row.InclusiveTicks || row.ActiveFrames != 0 {
		t.Fatal("entry/exit probes did not produce exact native calls", row)
	}
	if row.AverageInclusiveNS != float64(row.InclusiveTicks)*1e9/float64(s.Frequency)/10 {
		t.Fatal("native bridge average mismatch", row)
	}
	if relocate {
		// All selected calls and the DLL worker are now quiescent. Restore only
		// the saved entry prefix, then execute the original body/RET again.
		if err := windows.VirtualProtect(code, 4096, windows.PAGE_READWRITE, &old); err != nil {
			t.Fatal(err)
		}
		write(code+2048, leaf.Original)
		if err := windows.VirtualProtect(code, 4096, windows.PAGE_EXECUTE_READ, &old); err != nil {
			t.Fatal(err)
		}
		if ok, _, err := windows.NewLazySystemDLL("kernel32.dll").NewProc("FlushInstructionCache").Call(uintptr(windows.CurrentProcess()), code, 4096); ok == 0 {
			t.Fatal(err)
		}
		if got, _, _ := syscall.SyscallN(entry, 7, 11, 13, 17, 19, 23); got != wantReturn {
			t.Fatal("restored native leaf changed its result", got)
		}
	}
	t.Logf("Go-generated native probes -> C DLL -> QPC/ring/pipe -> Go accounting: %d calls, each received before shutdown; %d inclusive ticks", row.Calls, row.InclusiveTicks)
}
