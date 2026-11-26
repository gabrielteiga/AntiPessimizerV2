package platform

import (
	"errors"
	"fmt"
	"path/filepath"
	"time"
	"unsafe"

	"golang.org/x/sys/windows"
)

func fileTime(v windows.Filetime) time.Time { return time.Unix(0, v.Nanoseconds()).UTC() }
func Inspect(pid uint32) (Process, error) {
	h, err := windows.OpenProcess(windows.PROCESS_QUERY_LIMITED_INFORMATION, false, pid)
	if err != nil {
		return Process{}, err
	}
	defer windows.CloseHandle(h)
	var created, exit, kernel, user windows.Filetime
	if err = windows.GetProcessTimes(h, &created, &exit, &kernel, &user); err != nil {
		return Process{}, err
	}
	name := make([]uint16, 32768)
	n := uint32(len(name))
	if err = windows.QueryFullProcessImageName(h, 0, &name[0], &n); err != nil {
		return Process{}, err
	}
	return Process{pid, filepath.Base(windows.UTF16ToString(name[:n])), fileTime(created)}, nil
}
func Processes() ([]Process, error) {
	h, err := createToolhelpSnapshot(windows.TH32CS_SNAPPROCESS, 0)
	if err != nil {
		return nil, err
	}
	defer windows.CloseHandle(h)
	e := windows.ProcessEntry32{Size: uint32(unsafe.Sizeof(windows.ProcessEntry32{}))}
	var result []Process
	for err = windows.Process32First(h, &e); err == nil; err = windows.Process32Next(h, &e) {
		if p, err := Inspect(e.ProcessID); err == nil {
			result = append(result, p)
		}
	}
	if !errors.Is(err, windows.ERROR_NO_MORE_FILES) {
		return nil, err
	}
	return result, nil
}
func initial(pid uint32) (map[uint32]time.Time, []Module, error) {
	h, err := createToolhelpSnapshot(windows.TH32CS_SNAPTHREAD, 0)
	if err != nil {
		return nil, nil, err
	}
	defer windows.CloseHandle(h)
	threads := map[uint32]time.Time{}
	e := windows.ThreadEntry32{Size: uint32(unsafe.Sizeof(windows.ThreadEntry32{}))}
	for err = windows.Thread32First(h, &e); err == nil; err = windows.Thread32Next(h, &e) {
		if e.OwnerProcessID != pid {
			continue
		}
		th, err := windows.OpenThread(windows.THREAD_QUERY_LIMITED_INFORMATION, false, e.ThreadID)
		if err != nil {
			continue
		}
		var c, x, k, u windows.Filetime
		rc, _, callErr := windows.NewLazySystemDLL("kernel32.dll").NewProc("GetThreadTimes").Call(uintptr(th), uintptr(unsafe.Pointer(&c)), uintptr(unsafe.Pointer(&x)), uintptr(unsafe.Pointer(&k)), uintptr(unsafe.Pointer(&u)))
		err = nil
		if rc == 0 {
			err = callErr
		}
		windows.CloseHandle(th)
		if err == nil {
			threads[e.ThreadID] = fileTime(c)
		}
	}
	if !errors.Is(err, windows.ERROR_NO_MORE_FILES) {
		return nil, nil, err
	}
	mh, err := createToolhelpSnapshot(windows.TH32CS_SNAPMODULE|windows.TH32CS_SNAPMODULE32, pid)
	if err != nil {
		return nil, nil, err
	}
	defer windows.CloseHandle(mh)
	m := windows.ModuleEntry32{Size: uint32(unsafe.Sizeof(windows.ModuleEntry32{}))}
	var modules []Module
	for err = windows.Module32First(mh, &m); err == nil; err = windows.Module32Next(mh, &m) {
		modules = append(modules, Module{Base: uint64(m.ModBaseAddr), Size: uint64(m.ModBaseSize), Path: windows.UTF16ToString(m.ExePath[:])})
	}
	if !errors.Is(err, windows.ERROR_NO_MORE_FILES) {
		return nil, nil, err
	}
	return threads, modules, nil
}

// Windows documents ERROR_BAD_LENGTH while the loader changes the module list.
// Retry snapshot creation, never enumeration against a partially valid handle.
// Bound the wait so continuous loader churn cannot stall capture startup.
func createToolhelpSnapshot(flags, pid uint32) (windows.Handle, error) {
	return snapshotWithRetry(flags, pid, windows.CreateToolhelp32Snapshot, time.Sleep)
}

func snapshotWithRetry(flags, pid uint32, create func(uint32, uint32) (windows.Handle, error), pause func(time.Duration)) (windows.Handle, error) {
	const attempts = 8
	for attempt := 0; attempt < attempts; attempt++ {
		handle, err := create(flags, pid)
		if err == nil {
			return handle, nil
		}
		if !errors.Is(err, windows.ERROR_BAD_LENGTH) || attempt == attempts-1 {
			return windows.InvalidHandle, fmt.Errorf("CreateToolhelp32Snapshot(flags=%#x, pid=%d): %w", flags, pid, err)
		}
		pause(time.Millisecond << attempt)
	}
	panic("unreachable snapshot retry")
}
