package native

import (
	"fmt"
	"runtime"
	"unsafe"

	"golang.org/x/sys/windows"
)

// Diagnostic only: enumerate this process's own handle table. Query type names,
// never object/file names or data, and never close enumerated handles. This
// distinguishes runtime thread/event growth from leaked owned target resources.
func collectorHandleTypes() (map[string]int, error) {
	type handleEntry struct {
		Handle                             windows.Handle
		HandleCount, PointerCount          uintptr
		Access, Type, Attributes, Reserved uint32
	}
	var buffer []byte
	var needed uint32 = 64 << 10
	for attempt := 0; attempt < 4; attempt++ {
		if needed > 16<<20 {
			return nil, fmt.Errorf("handle diagnostic exceeds limit")
		}
		buffer = make([]byte, needed)
		err := windows.NtQueryInformationProcess(windows.CurrentProcess(), windows.ProcessHandleInformation, unsafe.Pointer(&buffer[0]), uint32(len(buffer)), &needed)
		if err == nil {
			break
		}
		if attempt == 3 || needed <= uint32(len(buffer)) {
			return nil, err
		}
	}
	count := *(*uintptr)(unsafe.Pointer(&buffer[0]))
	entrySize := unsafe.Sizeof(handleEntry{})
	if uint64(count) > uint64(len(buffer)-16)/uint64(entrySize) {
		return nil, fmt.Errorf("invalid handle diagnostic count")
	}
	entries := unsafe.Slice((*handleEntry)(unsafe.Pointer(&buffer[16])), int(count))
	query := windows.NewLazySystemDLL("ntdll.dll").NewProc("NtQueryObject")
	threadPID := debugKernel.NewProc("GetProcessIdOfThread")
	names := map[uint32]string{}
	result := map[string]int{}
	for _, entry := range entries {
		name := names[entry.Type]
		if name == "" {
			object := make([]byte, 4096)
			var length uint32
			status, _, _ := query.Call(uintptr(entry.Handle), 2, uintptr(unsafe.Pointer(&object[0])), uintptr(len(object)), uintptr(unsafe.Pointer(&length)))
			if uint32(status) != 0 {
				result["unavailable"]++
				continue
			}
			size := *(*uint16)(unsafe.Pointer(&object[0]))
			ptr := *(*uintptr)(unsafe.Pointer(&object[8]))
			base := uintptr(unsafe.Pointer(&object[0]))
			if size%2 != 0 || ptr < base || ptr-base+uintptr(size) > uintptr(len(object)) {
				return nil, fmt.Errorf("invalid handle type string")
			}
			start := int(ptr - base)
			name = windows.UTF16ToString(unsafe.Slice((*uint16)(unsafe.Pointer(&object[start])), int(size/2)))
			runtime.KeepAlive(object)
			names[entry.Type] = name
		}
		if name == "Thread" {
			pid, _, _ := threadPID.Call(uintptr(entry.Handle))
			if uint32(pid) == windows.GetCurrentProcessId() {
				name += " (self)"
			} else if pid != 0 {
				name += " (other process)"
			} else {
				name += " (unavailable owner)"
			}
		}
		result[name]++
	}
	runtime.KeepAlive(buffer)
	return result, nil
}
