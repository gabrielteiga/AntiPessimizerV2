package x64

import (
	"bytes"
	"encoding/binary"
	"syscall"
	"testing"

	"golang.org/x/sys/windows"
)

// This fixture only patches executable memory it allocated itself, with all
// calls quiescent. It does not install hooks in a target process or validate
// concurrent patching, exception unwind, CFG metadata or a Delphi ABI bridge.
func TestControlledNativeTrampolineAndRestore(t *testing.T) {
	const pageSize = 4096
	base, err := windows.VirtualAlloc(0, pageSize, windows.MEM_COMMIT|windows.MEM_RESERVE, windows.PAGE_READWRITE)
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := windows.VirtualFree(base, 0, windows.MEM_RELEASE); err != nil {
			t.Error(err)
		}
	}()
	code := append([]byte{0x48, 0x8b, 0x05, 57, 0, 0, 0}, append(bytes.Repeat([]byte{0x90}, 7), 0xc3)...)
	plan, err := Build(code, uint64(base), uint64(base+128), uint64(base+256))
	if err != nil {
		t.Fatal(err)
	}
	write := func(offset uintptr, b []byte) {
		t.Helper()
		var n uintptr
		if err := windows.WriteProcessMemory(windows.CurrentProcess(), base+offset, &b[0], uintptr(len(b)), &n); err != nil || n != uintptr(len(b)) {
			t.Fatalf("write fixture: %d %v", n, err)
		}
	}
	protect := func(mode uint32) {
		t.Helper()
		var old uint32
		if err := windows.VirtualProtect(base, pageSize, mode, &old); err != nil {
			t.Fatal(err)
		}
		if mode == windows.PAGE_EXECUTE_READ {
			rc, _, err := windows.NewLazySystemDLL("kernel32.dll").NewProc("FlushInstructionCache").Call(uintptr(windows.CurrentProcess()), base, pageSize)
			if rc == 0 {
				t.Fatal(err)
			}
		}
	}
	var value [8]byte
	binary.LittleEndian.PutUint64(value[:], 42)
	write(0, code)
	write(64, value[:])
	write(128, plan.Trampoline)
	write(256, []byte{0xb8, 99, 0, 0, 0, 0xc3}) // mov eax,99; ret
	protect(windows.PAGE_EXECUTE_READ)
	invoke := func(addr uintptr, want uintptr) {
		t.Helper()
		got, _, _ := syscall.SyscallN(addr)
		if got != want {
			t.Fatalf("native result %d, expected %d", got, want)
		}
	}
	invoke(base, 42)
	invoke(base+128, 42)
	protect(windows.PAGE_READWRITE)
	write(0, plan.Patch)
	protect(windows.PAGE_EXECUTE_READ)
	invoke(base, 99)
	invoke(base+128, 42)
	protect(windows.PAGE_READWRITE)
	write(0, plan.Original)
	protect(windows.PAGE_EXECUTE_READ)
	invoke(base, 42)
	t.Log("controlled native RIP relocation, entry jump and restoration returned expected values")
}
