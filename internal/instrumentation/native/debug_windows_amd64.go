package native

import (
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"path/filepath"
	"runtime"
	"strings"
	"unsafe"

	"golang.org/x/sys/windows"
)

type debugLaunch struct {
	process windows.ProcessInformation
	job     windows.Handle
	done    chan struct{}
	err     error
	exit    uint32
}
type debugEvent struct {
	code, pid, tid, pad uint32
	data                [20]uint64
}

var debugKernel = windows.NewLazySystemDLL("kernel32.dll")

// The creating OS thread owns every debugger API call until detach/exit. Only
// the newly created process is debugged, already assigned to its lifetime job.
func launchStopped(ctx context.Context, path string, args []string, dll, pipe string, entryRVA uint32, selected []debugSelection, capacity uint32) (*debugLaunch, error) {
	ready := make(chan struct{})
	d := &debugLaunch{done: make(chan struct{})}
	go func() {
		runtime.LockOSThread()
		defer runtime.UnlockOSThread()
		defer close(d.done)
		p, job, err := createOwnedFlags(path, args, 2) // DEBUG_ONLY_THIS_PROCESS
		d.process, d.job = p, job
		if err != nil {
			d.err = err
			close(ready)
			return
		}
		close(ready)
		d.err = debugOwned(ctx, d, dll, pipe, entryRVA, selected, capacity)
	}()
	<-ready
	if d.job == 0 {
		<-d.done
		return nil, d.err
	}
	return d, nil
}
func debugOwned(ctx context.Context, d *debugLaunch, dll, pipe string, entryRVA uint32, selected []debugSelection, capacity uint32) (resultErr error) {
	threads := map[uint32]windows.Handle{}
	suspended := map[uint32]bool{}
	modules := map[string]uintptr{}
	var remoteAllocations []uintptr
	var remoteHandles []windows.Handle
	defer func() {
		if resultErr != nil {
			windows.TerminateJobObject(d.job, 1)
			debugKernel.NewProc("DebugActiveProcessStop").Call(uintptr(d.process.ProcessId))
		}
		for _, h := range threads {
			windows.CloseHandle(h)
		}
		for _, h := range remoteHandles {
			windows.CloseHandle(h)
		}
	}()
	call := func(name string, args ...uintptr) (uintptr, error) {
		r, _, err := debugKernel.NewProc(name).Call(args...)
		if r == 0 {
			return 0, fmt.Errorf("%s: %w", name, err)
		}
		return r, nil
	}
	read := func(address uintptr, size int) ([]byte, error) {
		b := make([]byte, size)
		var n uintptr
		err := windows.ReadProcessMemory(d.process.Process, address, &b[0], uintptr(size), &n)
		if err != nil || n != uintptr(size) {
			return nil, fmt.Errorf("read target startup: %d: %w", n, err)
		}
		return b, nil
	}
	write := func(address uintptr, b []byte) error {
		var n uintptr
		if err := windows.WriteProcessMemory(d.process.Process, address, &b[0], uintptr(len(b)), &n); err != nil || n != uintptr(len(b)) {
			return fmt.Errorf("write target startup: %d: %w", n, err)
		}
		return nil
	}
	protectWrite := func(address uintptr, b []byte) error {
		var old uint32
		if _, err := call("VirtualProtectEx", uintptr(d.process.Process), address, uintptr(len(b)), windows.PAGE_READWRITE, uintptr(unsafe.Pointer(&old))); err != nil {
			return err
		}
		err := write(address, b)
		var ignored uint32
		_, restore := call("VirtualProtectEx", uintptr(d.process.Process), address, uintptr(len(b)), uintptr(old), uintptr(unsafe.Pointer(&ignored)))
		_, flush := call("FlushInstructionCache", uintptr(d.process.Process), address, uintptr(len(b)))
		return errors.Join(err, restore, flush)
	}
	allocate := func(b []byte) (uintptr, error) {
		address, err := call("VirtualAllocEx", uintptr(d.process.Process), 0, uintptr(len(b)), windows.MEM_RESERVE|windows.MEM_COMMIT, windows.PAGE_READWRITE)
		if err != nil {
			return 0, err
		}
		remoteAllocations = append(remoteAllocations, address)
		if err := write(address, b); err != nil {
			return 0, err
		}
		return address, nil
	}
	thread := func(start, arg uintptr) (uint32, error) {
		var tid uint32
		handle, err := call("CreateRemoteThread", uintptr(d.process.Process), 0, 0, start, arg, 0, uintptr(unsafe.Pointer(&tid)))
		if err == nil {
			remoteHandles = append(remoteHandles, windows.Handle(handle))
		}
		return tid, err
	}
	contextPage, err := windows.VirtualAlloc(0, 4096, windows.MEM_RESERVE|windows.MEM_COMMIT, windows.PAGE_READWRITE)
	if err != nil {
		return err
	}
	defer windows.VirtualFree(contextPage, 0, windows.MEM_RELEASE)
	localWrite := func(at uintptr, b []byte) error {
		var n uintptr
		return windows.WriteProcessMemory(windows.CurrentProcess(), at, &b[0], uintptr(len(b)), &n)
	}
	localRead := func(at uintptr, size int) ([]byte, error) {
		b := make([]byte, size)
		var n uintptr
		err := windows.ReadProcessMemory(windows.CurrentProcess(), at, &b[0], uintptr(size), &n)
		return b, err
	}
	getIP := func(h windows.Handle) (uint64, error) {
		var flags [4]byte
		binary.LittleEndian.PutUint32(flags[:], 0x100001) // AMD64 CONTEXT_CONTROL
		if err := localWrite(contextPage+48, flags[:]); err != nil {
			return 0, err
		}
		if _, err := call("GetThreadContext", uintptr(h), contextPage); err != nil {
			return 0, err
		}
		b, err := localRead(contextPage+248, 8)
		if err != nil {
			return 0, err
		}
		return binary.LittleEndian.Uint64(b), nil
	}
	suspend := func(tid uint32, h windows.Handle) error {
		if suspended[tid] {
			return nil
		}
		n, _, err := debugKernel.NewProc("SuspendThread").Call(uintptr(h))
		if uint32(n) == 0xffffffff {
			return fmt.Errorf("suspend startup thread: %w", err)
		}
		suspended[tid] = true
		return nil
	}
	var base, entry, producer uintptr
	var original byte
	var loadTID, bootTID uint32
	initialBreak, atEntry, booted := false, false, false
	ownDLL, err := windows.LoadLibraryEx(dll, 0, windows.LOAD_LIBRARY_SEARCH_DLL_LOAD_DIR|windows.LOAD_LIBRARY_SEARCH_SYSTEM32)
	if err != nil {
		return err
	}
	bootstrap, err := windows.GetProcAddress(ownDLL, "APNativeBootstrap")
	windows.FreeLibrary(ownDLL)
	if err != nil {
		return err
	}
	bootstrapRVA := bootstrap - uintptr(ownDLL)
	remoteSystemProcedure := func(name string) (uintptr, error) {
		address := debugKernel.NewProc(name).Addr()
		var module windows.Handle
		if _, err := call("GetModuleHandleExW", 6, address, uintptr(unsafe.Pointer(&module))); err != nil {
			return 0, err
		}
		var filename [1024]uint16
		n, err := windows.GetModuleFileName(module, &filename[0], uint32(len(filename)))
		if err != nil {
			return 0, err
		}
		key := strings.ToLower(filepath.Base(windows.UTF16ToString(filename[:n])))
		remote, ok := modules[key]
		if !ok {
			return 0, fmt.Errorf("startup system module missing: %s", key)
		}
		return remote + (address - uintptr(module)), nil
	}
	continueEvent := func(e *debugEvent, status uintptr) error {
		_, err := call("ContinueDebugEvent", uintptr(e.pid), uintptr(e.tid), status)
		return err
	}
	for {
		if err := ctx.Err(); err != nil {
			return err
		}
		var event debugEvent
		ok, _, waitErr := debugKernel.NewProc("WaitForDebugEventEx").Call(uintptr(unsafe.Pointer(&event)), 100)
		if ok == 0 {
			if errors.Is(waitErr, windows.ERROR_SEM_TIMEOUT) {
				continue
			}
			return fmt.Errorf("wait for owned process: %w", waitErr)
		}
		status := uintptr(0x10002) // DBG_CONTINUE
		eventErr := func() error {
			switch event.code {
			case 3: // CREATE_PROCESS_DEBUG_EVENT
				if event.pid != d.process.ProcessId {
					return errors.New("unexpected debug process")
				}
				if event.data[0] != 0 {
					windows.CloseHandle(windows.Handle(event.data[0]))
				}
				if event.data[1] != 0 && windows.Handle(event.data[1]) != d.process.Process {
					defer windows.CloseHandle(windows.Handle(event.data[1]))
				}
				threads[event.tid] = windows.Handle(event.data[2])
				base = uintptr(event.data[3])
				entry = base + uintptr(entryRVA)
				b, err := read(entry, 1)
				if err != nil {
					return err
				}
				original = b[0]
				return protectWrite(entry, []byte{0xcc})
			case 2:
				threads[event.tid] = windows.Handle(event.data[0])
			case 6: // LOAD_DLL_DEBUG_EVENT
				h := windows.Handle(event.data[0])
				moduleBase := uintptr(event.data[1])
				if h != 0 {
					defer windows.CloseHandle(h)
					var name [2048]uint16
					n, err := windows.GetFinalPathNameByHandle(h, &name[0], uint32(len(name)), 0)
					if err == nil && n < uint32(len(name)) {
						filename := windows.UTF16ToString(name[:n])
						key := strings.ToLower(filepath.Base(filename))
						modules[key] = moduleBase
						if strings.EqualFold(filepath.Clean(strings.TrimPrefix(filename, `\\?\`)), filepath.Clean(dll)) {
							producer = moduleBase
						}
					}
				}
			case 1: // EXCEPTION_DEBUG_EVENT
				exception := uint32(event.data[0])
				address := uintptr(event.data[2])
				if exception == 0x80000003 && address == entry && !atEntry {
					atEntry = true
					if err := protectWrite(entry, []byte{original}); err != nil {
						return err
					}
					h := threads[event.tid]
					if _, err := getIP(h); err != nil {
						return err
					}
					var ip [8]byte
					binary.LittleEndian.PutUint64(ip[:], uint64(entry))
					if err := localWrite(contextPage+248, ip[:]); err != nil {
						return err
					}
					if _, err := call("SetThreadContext", uintptr(h), contextPage); err != nil {
						return err
					}
					if err := suspend(event.tid, h); err != nil {
						return err
					}
					dllString, err := windows.UTF16FromString(dll)
					if err != nil {
						return err
					}
					data := make([]byte, len(dllString)*2)
					for i, c := range dllString {
						binary.LittleEndian.PutUint16(data[i*2:], c)
					}
					argument, err := allocate(data)
					if err != nil {
						return err
					}
					load, err := remoteSystemProcedure("LoadLibraryW")
					if err != nil {
						return err
					}
					loadTID, err = thread(load, argument)
					return err
				}
				if exception == 0x80000003 && !initialBreak {
					initialBreak = true
					return nil
				}
				status = 0x80010001 // DBG_EXCEPTION_NOT_HANDLED: preserve application SEH
			case 4: // EXIT_THREAD_DEBUG_EVENT
				code := uint32(event.data[0])
				if h := threads[event.tid]; h != 0 {
					windows.CloseHandle(h)
					delete(threads, event.tid)
				}
				if event.tid == loadTID {
					if producer == 0 {
						return fmt.Errorf("native helper did not load (exit %#x)", code)
					}
					for tid, h := range threads {
						if err := suspend(tid, h); err != nil {
							return err
						}
						ip, err := getIP(h)
						if err != nil {
							return err
						}
						for _, p := range selected {
							if ip >= uint64(base)+uint64(p.RVA) && ip < uint64(base)+uint64(p.RVA)+uint64(p.Size) && !(tid == d.process.ThreadId && ip == uint64(entry)) {
								return errors.New("selected procedure already running during startup")
							}
						}
					}
					request, err := bootstrapRequest(pipe, selected, capacity)
					if err != nil {
						return err
					}
					argument, err := allocate(request)
					if err != nil {
						return err
					}
					bootTID, err = thread(producer+bootstrapRVA, argument)
					return err
				}
				if event.tid == bootTID {
					if code != 0 {
						return fmt.Errorf("native startup rejected: Windows error %d", code)
					}
					for _, address := range remoteAllocations {
						if _, err := call("VirtualFreeEx", uintptr(d.process.Process), address, 0, windows.MEM_RELEASE); err != nil {
							return err
						}
					}
					remoteAllocations = nil
					for tid := range suspended {
						h := threads[tid]
						if h == 0 {
							continue
						}
						n, _, err := debugKernel.NewProc("ResumeThread").Call(uintptr(h))
						if uint32(n) == 0xffffffff {
							return fmt.Errorf("resume owned target: %w", err)
						}
					}
					clear(suspended)
					booted = true
				}
			case 5:
				d.exit = uint32(event.data[0])
				if !booted {
					return errors.New("target exited before instrumentation startup")
				}
			}
			return nil
		}()
		continueErr := continueEvent(&event, status)
		if eventErr != nil || continueErr != nil {
			return errors.Join(eventErr, continueErr)
		}
		if event.code == 5 {
			return nil
		}
	}
}
