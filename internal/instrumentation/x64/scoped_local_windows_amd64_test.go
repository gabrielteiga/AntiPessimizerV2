package x64

import (
	"encoding/binary"
	"golang.org/x/sys/windows"
	"testing"
)

func TestScopedLocalShadowRestoresCallerThroughWindowsVirtualUnwind(t *testing.T) {
	memory, err := windows.VirtualAlloc(0, 16384, windows.MEM_COMMIT|windows.MEM_RESERVE, windows.PAGE_READWRITE)
	if err != nil {
		t.Fatal(err)
	}
	defer windows.VirtualFree(memory, 0, windows.MEM_RELEASE)
	scratch, err := windows.VirtualAlloc(0, 4096, windows.MEM_COMMIT|windows.MEM_RESERVE, windows.PAGE_READWRITE)
	if err != nil {
		t.Fatal(err)
	}
	defer windows.VirtualFree(scratch, 0, windows.MEM_RELEASE)
	write := func(address uintptr, data []byte) {
		t.Helper()
		var count uintptr
		if err := windows.WriteProcessMemory(windows.CurrentProcess(), address, &data[0], uintptr(len(data)), &count); err != nil || count != uintptr(len(data)) {
			t.Fatal("write controlled context", err, count)
		}
	}
	read := func(address uintptr, size int) []byte {
		t.Helper()
		data := make([]byte, size)
		var count uintptr
		if err := windows.ReadProcessMemory(windows.CurrentProcess(), address, &data[0], uintptr(size), &count); err != nil || count != uintptr(size) {
			t.Fatal("read controlled context", err, count)
		}
		return data
	}
	image, code := scopedLocalPE()
	loaded := uint64(memory) - 0x8000
	plan, err := BuildPEFunction(image, peLeafRVA, uint32(len(code)), loaded, uint64(memory), uint64(memory+8192), uint64(memory+8200), uint64(memory+8300), 7)
	if err != nil {
		t.Fatal(err)
	}
	write(memory, plan.Image)
	info := plan.Image[plan.Functions[1].Unwind:]
	infoBytes := 4 + ((int(info[2])+1)&^1)*2
	descriptor := info[infoBytes+4:]
	u32, u64 := binary.LittleEndian.Uint32, binary.LittleEndian.Uint64
	shadowAt := u32(descriptor[48:])
	scope := descriptor[u32(descriptor[52:]):]
	pc := uintptr(loaded) + uintptr(u32(scope[4:]))
	shadow := memory + uintptr(plan.Functions[1].Unwind) + uintptr(infoBytes+4) + uintptr(shadowAt)
	frameSP, returnPC := scratch+2048, memory+8192
	put := func(address uintptr, value uint64) {
		var word [8]byte
		binary.LittleEndian.PutUint64(word[:], value)
		write(address, word[:])
	}
	const savedRBX = uint64(0x123456789abcdef0)
	const savedRSI = uint64(0xfedcba9876543210)
	put(frameSP+48, savedRBX)
	put(frameSP+40, savedRSI)
	put(frameSP+56, uint64(returnPC))
	xmm := []byte{1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13, 14, 15, 16}
	write(frameSP+16, xmm)
	context := make([]byte, 1232)
	binary.LittleEndian.PutUint32(context[48:], 0x0010000b)
	binary.LittleEndian.PutUint64(context[152:], uint64(frameSP))
	binary.LittleEndian.PutUint64(context[248:], uint64(pc))
	write(scratch, context)
	windows.NewLazySystemDLL("ntdll.dll").NewProc("RtlVirtualUnwind").Call(0, uintptr(loaded), pc, shadow, scratch, scratch+1600, scratch+1608, 0)
	restored := read(scratch, len(context))
	if u64(restored[152:]) != uint64(frameSP+64) || u64(restored[248:]) != uint64(returnPC) || u64(restored[144:]) != savedRBX || u64(restored[168:]) != savedRSI {
		t.Fatal("shadow lost primary/local GPR or caller SP/PC")
	}
	for i, value := range xmm {
		if restored[512+i] != value {
			t.Fatal("shadow lost part of XMM6", i)
		}
	}
	if u64(read(scratch+1608, 8)) != uint64(frameSP) {
		t.Fatal("shadow changed the establisher frame")
	}
}
