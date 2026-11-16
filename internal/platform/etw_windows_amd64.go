package platform

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"runtime"
	"sync"
	"sync/atomic"
	"time"
	"unsafe"

	"golang.org/x/sys/windows"
)

// Layouts follow the Windows SDK evntrace.h and evntcons.h (amd64).
type wnode struct {
	Size, Provider        uint32
	Historical, Timestamp uint64
	GUID                  windows.GUID
	Clock, Flags          uint32
}
type traceProperties struct {
	Node                                                       wnode
	BufferSize, Minimum, Maximum, MaxFile, Mode, Flush, Enable uint32
	Age                                                        int32
	Buffers, Free, Lost, Written, LogLost, RealLost            uint32
	Thread                                                     uintptr
	FileOffset, NameOffset                                     uint32
	Name                                                       [128]uint16
}
type logfile struct {
	File, Name               *uint16
	Current                  int64
	Read, Mode               uint32
	Event                    [88]byte
	Header                   [280]byte
	BufferCallback           uintptr
	BufferSize, Filled, Lost uint32
	Callback                 uintptr
	Kernel                   uint32
	Context                  uintptr
}
type descriptor struct {
	ID                              uint16
	Version, Channel, Level, Opcode uint8
	Task                            uint16
	Keyword                         uint64
}
type eventHeader struct {
	Size, Type, Flags, Property uint16
	Thread, Process             uint32
	Timestamp                   uint64
	Provider                    windows.GUID
	Descriptor                  descriptor
	CPU                         uint64
	Activity                    windows.GUID
}
type eventRecord struct {
	Header                eventHeader
	Buffer                uint32
	ExtendedCount, Length uint16
	Extended, Data        unsafe.Pointer
	Context               uintptr
}

var advapi = windows.NewLazySystemDLL("advapi32.dll")
var startTrace = advapi.NewProc("StartTraceW")
var controlTrace = advapi.NewProc("ControlTraceW")
var openTrace = advapi.NewProc("OpenTraceW")
var processTrace = advapi.NewProc("ProcessTrace")
var closeTrace = advapi.NewProc("CloseTrace")
var consumers sync.Map
var nextConsumer atomic.Uint64
var callback = windows.NewCallback(func(r *eventRecord) uintptr {
	v, ok := consumers.Load(r.Context)
	if !ok {
		return 0
	}
	c := v.(*consumer)
	// Only 64-bit classic records; no unsafe decoding of unknown schemas.
	if r.Header.Flags&0x20 != 0 || r.Data == nil {
		return 0
	}
	kind := ""
	switch r.Header.Provider {
	case windows.GUID{Data1: 0x3d6fa8d1, Data2: 0xfe05, Data3: 0x11d0, Data4: [8]byte{0x9d, 0xda, 0, 0xc0, 0x4f, 0xd7, 0xba, 0x7c}}:
		kind = "thread"
	case windows.GUID{Data1: 0x2cb15d1d, Data2: 0x5fc1, Data3: 0x11d2, Data4: [8]byte{0xab, 0xe1, 0, 0xa0, 0xc9, 0x11, 0xf5, 0x18}}:
		kind = "image"
	case windows.GUID{Data1: 0xce1dbfb4, Data2: 0x137e, Data3: 0x4da6, Data4: [8]byte{0x87, 0xb0, 0x3f, 0x59, 0xaa, 0x10, 0x2c, 0xbc}}:
		kind = "profile"
	default:
		return 0
	}
	ft := windows.Filetime{LowDateTime: uint32(r.Header.Timestamp), HighDateTime: uint32(r.Header.Timestamp >> 32)}
	if sample, ok := c.state.Feed(kind, r.Header.Descriptor.Opcode, r.Header.Descriptor.Version, fileTime(ft), unsafe.Slice((*byte)(r.Data), int(r.Length))); ok {
		c.emit(sample)
	}
	return 0
})

type consumer struct {
	state KernelState
	emit  func(Sample)
}

func profilePrivilege() (func(), error) {
	var token windows.Token
	if err := windows.OpenProcessToken(windows.CurrentProcess(), windows.TOKEN_ADJUST_PRIVILEGES|windows.TOKEN_QUERY, &token); err != nil {
		return nil, err
	}
	var luid windows.LUID
	if err := windows.LookupPrivilegeValue(nil, windows.StringToUTF16Ptr("SeSystemProfilePrivilege"), &luid); err != nil {
		token.Close()
		return nil, err
	}
	enable := windows.Tokenprivileges{PrivilegeCount: 1}
	enable.Privileges[0] = windows.LUIDAndAttributes{Luid: luid, Attributes: windows.SE_PRIVILEGE_ENABLED}
	var previous windows.Tokenprivileges
	var n uint32
	if err := windows.AdjustTokenPrivileges(token, false, &enable, uint32(unsafe.Sizeof(previous)), &previous, &n); err != nil {
		token.Close()
		return nil, fmt.Errorf("enable profiling privilege (run elevated): %w", err)
	}
	return func() { windows.AdjustTokenPrivileges(token, false, &previous, 0, nil, nil); token.Close() }, nil
}

// Capture creates and owns a uniquely named kernel ETW session. It never stops
// a pre-existing session. emit must be nonblocking. No process is suspended.
func Capture(ctx context.Context, pid uint32, emit func(Sample)) error {
	restore, err := profilePrivilege()
	if err != nil {
		return err
	}
	defer restore()
	target, err := windows.OpenProcess(windows.PROCESS_QUERY_LIMITED_INFORMATION|windows.SYNCHRONIZE, false, pid)
	if err != nil {
		return err
	}
	defer windows.CloseHandle(target)
	var nonce [12]byte
	if _, err = rand.Read(nonce[:]); err != nil {
		return err
	}
	name := windows.StringToUTF16Ptr("AntiPessimizer-" + hex.EncodeToString(nonce[:]))
	p := traceProperties{}
	p.Node.Size = uint32(unsafe.Sizeof(p))
	p.Node.Clock = 1
	p.Node.Flags = 0x20000
	p.Node.GUID, err = windows.GenerateGUID()
	if err != nil {
		return err
	}
	p.BufferSize = 64
	p.Minimum = 64
	p.Maximum = 128
	p.Mode = 0x100 | 0x02000000
	p.Flush = 1
	p.Enable = 0x01000007
	p.NameOffset = uint32(unsafe.Offsetof(p.Name))
	var session uint64
	rc, _, _ := startTrace.Call(uintptr(unsafe.Pointer(&session)), uintptr(unsafe.Pointer(name)), uintptr(unsafe.Pointer(&p)))
	if rc != 0 {
		return fmt.Errorf("StartTrace: %w", windows.Errno(rc))
	}
	defer controlTrace.Call(uintptr(session), uintptr(unsafe.Pointer(name)), uintptr(unsafe.Pointer(&p)), 1)
	threads, modules, err := initial(pid)
	if err != nil {
		return err
	}
	id := uintptr(nextConsumer.Add(1))
	consumers.Store(id, &consumer{KernelState{pid, threads, modules}, emit})
	defer consumers.Delete(id)
	log := logfile{Name: name, Mode: 0x10000100, Callback: callback, Context: id}
	trace, _, callErr := openTrace.Call(uintptr(unsafe.Pointer(&log)))
	if trace == ^uintptr(0) {
		return fmt.Errorf("OpenTrace: %w", callErr)
	}
	var once sync.Once
	closeConsumer := func() { once.Do(func() { closeTrace.Call(trace) }) }
	defer closeConsumer()
	done := make(chan uint32, 1)
	go func() { rc, _, _ := processTrace.Call(uintptr(unsafe.Pointer(&trace)), 1, 0, 0); done <- uint32(rc) }()
	ticker := time.NewTicker(200 * time.Millisecond)
	defer ticker.Stop()
	for {
		select {
		case rc := <-done:
			runtime.KeepAlive(&log)
			runtime.KeepAlive(name)
			if rc != 0 && rc != 1223 {
				return fmt.Errorf("ProcessTrace: %w", windows.Errno(rc))
			}
			return nil
		case <-ctx.Done():
			closeConsumer()
			<-done
			return ctx.Err()
		case <-ticker.C:
			status, e := windows.WaitForSingleObject(target, 0)
			if e != nil {
				closeConsumer()
				<-done
				return e
			}
			if status == windows.WAIT_OBJECT_0 {
				closeConsumer()
				<-done
				return nil
			}
		}
	}
}
