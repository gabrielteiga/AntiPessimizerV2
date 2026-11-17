package x64

import (
	"encoding/binary"
	"math"
	"syscall"
	"testing"

	"golang.org/x/sys/windows"
)

// All executable/context/stack memory belongs to this fixture. The helper is
// native code, not a Go callback. VirtualUnwind operates on a private fake stack.
func TestNativeEntryProbeABIAndVirtualUnwind(t *testing.T) {
	code, err := windows.VirtualAlloc(0, 4096, windows.MEM_RESERVE|windows.MEM_COMMIT, windows.PAGE_READWRITE)
	if err != nil {
		t.Fatal(err)
	}
	rw, err := windows.VirtualAlloc(0, 8192, windows.MEM_RESERVE|windows.MEM_COMMIT, windows.PAGE_READWRITE)
	if err != nil {
		windows.VirtualFree(code, 0, windows.MEM_RELEASE)
		t.Fatal(err)
	}
	nt := windows.NewLazySystemDLL("ntdll.dll")
	registered := false
	defer func() {
		windows.VirtualFree(rw, 0, windows.MEM_RELEASE)
		if registered {
			if ok, _, err := nt.NewProc("RtlDeleteFunctionTable").Call(code + 512); ok == 0 {
				t.Error("delete unwind table", err)
				return
			}
		}
		if err := windows.VirtualFree(code, 0, windows.MEM_RELEASE); err != nil {
			t.Error(err)
		}
	}()
	write := func(address uintptr, b []byte) {
		t.Helper()
		var n uintptr
		if err := windows.WriteProcessMemory(windows.CurrentProcess(), address, &b[0], uintptr(len(b)), &n); err != nil || n != uintptr(len(b)) {
			t.Fatalf("write native fixture: %d %v", n, err)
		}
	}
	read := func(address uintptr, size int) []byte {
		t.Helper()
		b := make([]byte, size)
		var n uintptr
		if err := windows.ReadProcessMemory(windows.CurrentProcess(), address, &b[0], uintptr(size), &n); err != nil || n != uintptr(size) {
			t.Fatalf("read native fixture: %d %v", n, err)
		}
		return b
	}
	p, err := BuildEntryProbe(uint64(code), uint64(code+1024), uint64(code+1536), 17)
	if err != nil {
		t.Fatal(err)
	}
	f, err := BuildEntryProbe(uint64(code+2560), uint64(code+1024), uint64(code+1792), 29)
	if err != nil {
		t.Fatal(err)
	}
	write(code, p.Image)
	write(code+2560, f.Image)
	var table [24]byte
	binary.LittleEndian.PutUint32(table[4:], p.CodeSize)
	binary.LittleEndian.PutUint32(table[8:], p.UnwindRVA)
	binary.LittleEndian.PutUint32(table[12:], 2560)
	binary.LittleEndian.PutUint32(table[16:], 2560+f.CodeSize)
	binary.LittleEndian.PutUint32(table[20:], 2560+f.UnwindRVA)
	write(code+512, table[:])
	// Helper records ECX/RSP, uses all four home slots, and clobbers every
	// volatile integer register and XMM0..5, as a conforming native helper may.
	helper := []byte{0x48, 0xb8}
	var addr [8]byte
	binary.LittleEndian.PutUint64(addr[:], uint64(rw+2048))
	helper = append(helper, addr[:]...)
	helper = append(helper, 0x89, 0x08, 0x48, 0x89, 0x60, 0x08)
	for _, reg := range []byte{0, 1, 2, 8, 9, 10, 11} {
		rex := byte(0x48)
		if reg >= 8 {
			rex |= 1
		}
		helper = append(helper, rex, 0xb8|(reg&7))
		binary.LittleEndian.PutUint64(addr[:], 0xfeed0000+uint64(reg))
		helper = append(helper, addr[:]...)
	}
	for _, off := range []byte{8, 16, 24, 32} {
		helper = append(helper, 0x48, 0x89, 0x44, 0x24, off)
	}
	for i := byte(0); i < 6; i++ {
		helper = append(helper, 0x66, 0x0f, 0xef, 0xc0|(i<<3)|i)
	}
	helper = append(helper, 0xc3)
	write(code+1024, helper)
	// Sum six Win64 arguments (including the two stack arguments).
	write(code+1536, []byte{0x48, 0x8d, 0x04, 0x11, 0x4c, 0x01, 0xc0, 0x4c, 0x01, 0xc8, 0x48, 0x03, 0x44, 0x24, 40, 0x48, 0x03, 0x44, 0x24, 48, 0xc3})
	// Caller seeds XMM0..5 with six distinct bit patterns before jumping to f.
	var caller []byte
	for i := byte(0); i < 6; i++ {
		caller = append(caller, 0x48, 0x8d, 0x41, i, 0x66, 0x48, 0x0f, 0x6e, 0xc0|(i<<3))
	}
	caller = append(caller, jump(uint64(code+2560))...)
	write(code+2048, caller)
	vector := []byte{0x48, 0xb8}
	binary.LittleEndian.PutUint64(addr[:], uint64(rw+2080))
	vector = append(vector, addr[:]...)
	for i := byte(0); i < 6; i++ {
		vector = append(vector, 0xf3, 0x0f, 0x7f, 0x80|(i<<3))
		var d [4]byte
		binary.LittleEndian.PutUint32(d[:], uint32(i)*16)
		vector = append(vector, d[:]...)
	}
	vector = append(vector, 0x66, 0x48, 0x0f, 0x7e, 0xc0, 0xc3)
	write(code+1792, vector)
	var old uint32
	if err := windows.VirtualProtect(code, 4096, windows.PAGE_EXECUTE_READ, &old); err != nil {
		t.Fatal(err)
	}
	if ok, _, err := windows.NewLazySystemDLL("kernel32.dll").NewProc("FlushInstructionCache").Call(uintptr(windows.CurrentProcess()), code, 4096); ok == 0 {
		t.Fatal(err)
	}
	if ok, _, err := nt.NewProc("RtlAddFunctionTable").Call(code+512, 2, code); ok == 0 {
		t.Fatal("register unwind table", err)
	}
	registered = true
	if got, _, _ := syscall.SyscallN(code, 1, 2, 3, 4, 5, 6); got != 21 {
		t.Fatalf("native arguments changed: %d", got)
	}
	observed := read(rw+2048, 16)
	if binary.LittleEndian.Uint32(observed) != 17 || binary.LittleEndian.Uint64(observed[8:])%16 != 8 {
		t.Fatal("native ID/stack alignment", observed)
	}
	bits := math.Float64bits(3.5)
	if got, _, _ := syscall.SyscallN(code+2048, uintptr(bits)); uint64(got) != bits {
		t.Fatalf("native SIMD return changed: %#x", got)
	}
	lanes := read(rw+2080, 96)
	for i := 0; i < 6; i++ {
		if binary.LittleEndian.Uint64(lanes[i*16:]) != bits+uint64(i) || binary.LittleEndian.Uint64(lanes[i*16+8:]) != 0 {
			t.Fatal("native SIMD argument changed", i)
		}
	}
	// Synthetic CONTEXT AMD64 (1232 bytes) on aligned native memory. Exercise
	// entry, body and both epilogue states without unwinding any Go stack.
	activeSP := rw + 4096 + 512
	originalSP := activeSP + uintptr(p.StackSize)
	binary.LittleEndian.PutUint64(addr[:], 0x12345678)
	write(originalSP, addr[:])
	for _, phase := range []struct {
		offset uint32
		sp     uintptr
	}{{0, originalSP}, {7, activeSP}, {p.HelperReturn, activeSP}, {p.Epilogue, activeSP}, {p.Epilogue + 7, originalSP}} {
		context := make([]byte, 1232)
		binary.LittleEndian.PutUint32(context[48:], 0x0010000b)
		binary.LittleEndian.PutUint64(context[152:], uint64(phase.sp))
		binary.LittleEndian.PutUint64(context[248:], uint64(code)+uint64(phase.offset))
		write(rw, context)
		nt.NewProc("RtlVirtualUnwind").Call(0, code, code+uintptr(phase.offset), code+512, rw, rw+1280, rw+1288, 0)
		context = read(rw, 1232)
		if rsp, rip := binary.LittleEndian.Uint64(context[152:]), binary.LittleEndian.Uint64(context[248:]); rsp != uint64(originalSP+8) || rip != 0x12345678 {
			t.Fatalf("native unwind at %d: RSP=%#x RIP=%#x", phase.offset, rsp, rip)
		}
	}
	t.Log("native entry probe preserved Win64 register/stack arguments, home slots and XMM0..5; Windows unwound prologue, body and epilogue contexts")
}
