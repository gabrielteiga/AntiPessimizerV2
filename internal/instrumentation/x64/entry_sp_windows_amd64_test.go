package x64

import (
	"bytes"
	"encoding/binary"
	"syscall"
	"testing"

	"golang.org/x/sys/windows"
)

func TestNativeProbeReportsSameEntryStackWithoutChangingArguments(t *testing.T) {
	code, err := windows.VirtualAlloc(0, 8192, windows.MEM_RESERVE|windows.MEM_COMMIT, windows.PAGE_READWRITE)
	if err != nil {
		t.Fatal(err)
	}
	rw, err := windows.VirtualAlloc(0, 4096, windows.MEM_RESERVE|windows.MEM_COMMIT, windows.PAGE_READWRITE)
	if err != nil {
		windows.VirtualFree(code, 0, windows.MEM_RELEASE)
		t.Fatal(err)
	}
	nt := windows.NewLazySystemDLL("ntdll.dll")
	registered := false
	defer func() {
		windows.VirtualFree(rw, 0, windows.MEM_RELEASE)
		if registered {
			if ok, _, err := nt.NewProc("RtlDeleteFunctionTable").Call(code + 4096); ok == 0 {
				t.Error(err)
				return
			}
		}
		if err := windows.VirtualFree(code, 0, windows.MEM_RELEASE); err != nil {
			t.Error(err)
		}
	}()
	write := func(at uintptr, data []byte) {
		t.Helper()
		var n uintptr
		if err := windows.WriteProcessMemory(windows.CurrentProcess(), at, &data[0], uintptr(len(data)), &n); err != nil || n != uintptr(len(data)) {
			t.Fatal(n, err)
		}
	}
	address := func(at uintptr) []byte {
		var b [8]byte
		binary.LittleEndian.PutUint64(b[:], uint64(at))
		return b[:]
	}
	in, err := BuildEntryProbe(uint64(code), uint64(code+2048), uint64(code+512), 17)
	if err != nil {
		t.Fatal(err)
	}
	out, err := BuildExitProbe(uint64(code+1024), uint64(code+2176), 17)
	if err != nil {
		t.Fatal(err)
	}
	write(code, in.Image)
	write(code+1024, out.Image)
	helper := func(destination uintptr) []byte {
		b := append([]byte{0x48, 0xb8}, address(destination)...)
		return append(b, 0x48, 0x89, 0x10, 0x89, 0x48, 8, 0xc3) // store RDX and ECX; RET
	}
	enterHelper, exitHelper := helper(rw), helper(rw+16)
	write(code+2048, enterHelper)
	write(code+2176, exitHelper)
	body := append([]byte{0x48, 0xb8}, address(rw+32)...)
	body = append(body, 0x48, 0x89, 0x10, 0x48, 0x89, 0x48, 8, 0x48, 0x89, 0xe0) // save original args; MOV RAX,RSP
	body = append(body, jump(uint64(code+1024))...)
	write(code+512, body)
	write(code+5000, []byte{1, 0, 0, 0})
	functions := []RuntimeFunction{{0, in.CodeSize, in.UnwindRVA}, {512, 512 + uint32(len(body)), 5000}, {1024, 1024 + out.CodeSize, 1024 + out.UnwindRVA}, {2048, 2048 + uint32(len(enterHelper)), 5000}, {2176, 2176 + uint32(len(exitHelper)), 5000}}
	var table bytes.Buffer
	if err := binary.Write(&table, binary.LittleEndian, functions); err != nil {
		t.Fatal(err)
	}
	write(code+4096, table.Bytes())
	var old uint32
	if err := windows.VirtualProtect(code, 8192, windows.PAGE_EXECUTE_READ, &old); err != nil {
		t.Fatal(err)
	}
	if ok, _, err := windows.NewLazySystemDLL("kernel32.dll").NewProc("FlushInstructionCache").Call(uintptr(windows.CurrentProcess()), code, 8192); ok == 0 {
		t.Fatal(err)
	}
	if ok, _, err := nt.NewProc("RtlAddFunctionTable").Call(code+4096, uintptr(len(functions)), code); ok == 0 {
		t.Fatal(err)
	}
	registered = true
	const first, second = uintptr(0x1122334455667788), uintptr(0x8877665544332211)
	sp, _, _ := syscall.SyscallN(code, first, second)
	var values [48]byte
	var n uintptr
	if err := windows.ReadProcessMemory(windows.CurrentProcess(), rw, &values[0], uintptr(len(values)), &n); err != nil || n != uintptr(len(values)) {
		t.Fatal(n, err)
	}
	u64, u32 := binary.LittleEndian.Uint64, binary.LittleEndian.Uint32
	if sp == 0 || sp%16 != 8 || u64(values[:]) != uint64(sp) || u64(values[16:]) != uint64(sp) || u32(values[8:]) != 17 || u32(values[24:]) != 17 || u64(values[32:]) != uint64(second) || u64(values[40:]) != uint64(first) {
		t.Fatal("native probes changed return slot or leaked helper arguments", sp, values)
	}
}
