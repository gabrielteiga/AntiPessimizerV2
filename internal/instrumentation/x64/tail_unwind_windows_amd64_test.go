package x64

import (
	"encoding/binary"
	"golang.org/x/sys/windows"
	"testing"
)

// Test every restoration phase on a private native stack. No Go or application
// stack is unwound, and the original PE/target address is never executed.
func TestNativeTailEpiloguesVirtualUnwindRestoresCaller(t *testing.T) {
	cases := []struct {
		code, info []byte
		reg, depth int
		phases     []int
	}{
		{[]byte{0x53, 0x48, 0x83, 0xec, 0x20, 0x48, 0x89, 0xcb, 0x48, 0x83, 0xc4, 0x20, 0x5b, 0xe9, 0, 0, 0, 0}, []byte{1, 5, 2, 0, 5, 0x32, 1, 0x30}, 144, 40, []int{5, 8, 12, 13}},
		{[]byte{0x55, 0x48, 0x83, 0xec, 0x28, 0x48, 0x8d, 0x6c, 0x24, 0x20, 0x90, 0x48, 0x8d, 0x65, 8, 0x5d, 0xe9, 0, 0, 0, 0}, []byte{1, 10, 3, 0x25, 10, 3, 5, 0x42, 1, 0x50, 0, 0}, 160, 48, []int{10, 11, 15, 16}},
	}
	for _, c := range cases {
		t.Run(string(rune('0'+c.reg/8)), func(t *testing.T) {
			memory, err := windows.VirtualAlloc(0, 16384, windows.MEM_RESERVE|windows.MEM_COMMIT, windows.PAGE_READWRITE)
			if err != nil {
				t.Fatal(err)
			}
			defer windows.VirtualFree(memory, 0, windows.MEM_RELEASE)
			rw, err := windows.VirtualAlloc(0, 8192, windows.MEM_RESERVE|windows.MEM_COMMIT, windows.PAGE_READWRITE)
			if err != nil {
				t.Fatal(err)
			}
			defer windows.VirtualFree(rw, 0, windows.MEM_RELEASE)
			write := func(address uintptr, data []byte) {
				t.Helper()
				var n uintptr
				if err := windows.WriteProcessMemory(windows.CurrentProcess(), address, &data[0], uintptr(len(data)), &n); err != nil || n != uintptr(len(data)) {
					t.Fatal("write private stack/code", err)
				}
			}
			read := func(address uintptr, size int) []byte {
				t.Helper()
				data := make([]byte, size)
				var n uintptr
				if err := windows.ReadProcessMemory(windows.CurrentProcess(), address, &data[0], uintptr(size), &n); err != nil || n != uintptr(size) {
					t.Fatal("read private context", err)
				}
				return data
			}
			image := scalarImage(c.code)
			copy(image[0x480:], c.info)
			binary.LittleEndian.PutUint32(image[0x414:], 0x20c0)
			image[0x4c0] = 1
			binary.LittleEndian.PutUint32(image[0x220+len(c.code)-4:], 0x1080-(peLeafRVA+uint32(len(c.code))))
			p, err := NewPEImage(image)
			if err != nil {
				t.Fatal(err)
			}
			plan, err := p.BuildSelectedFunction(peLeafRVA, uint32(len(c.code)), uint64(memory)-0x20000, uint64(memory), uint64(memory+8192), uint64(memory+8192), uint64(memory+8192), 1, []uint32{0x1080})
			if err != nil {
				t.Fatal(err)
			}
			if len(plan.Image) >= 8192 {
				t.Fatal("unexpected plan extent")
			}
			write(memory, plan.Image)
			var table [36]byte
			for i, f := range plan.Functions {
				binary.LittleEndian.PutUint32(table[i*12:], f.Begin)
				binary.LittleEndian.PutUint32(table[i*12+4:], f.End)
				binary.LittleEndian.PutUint32(table[i*12+8:], f.Unwind)
			}
			write(memory+12288, table[:])
			var old uint32
			if err := windows.VirtualProtect(memory, 16384, windows.PAGE_EXECUTE_READ, &old); err != nil {
				t.Fatal(err)
			}
			const returned, saved uint64 = 0x12345678, 0x777788889999aaaa
			original := rw + 4096 + 512
			var word [8]byte
			binary.LittleEndian.PutUint64(word[:], returned)
			write(original, word[:])
			binary.LittleEndian.PutUint64(word[:], saved)
			write(original-8, word[:])
			unwind := windows.NewLazySystemDLL("ntdll.dll").NewProc("RtlVirtualUnwind")
			for i, offset := range c.phases {
				sp := original - uintptr(c.depth)
				reg := uint64(0xdeadbeef)
				if c.reg == 160 {
					reg = uint64(sp + 32)
				}
				if i == 2 {
					sp = original - 8
				}
				if i == 3 {
					sp = original
					reg = saved
				}
				context := make([]byte, 1232)
				binary.LittleEndian.PutUint32(context[48:], 0x0010000b)
				binary.LittleEndian.PutUint64(context[152:], uint64(sp))
				binary.LittleEndian.PutUint64(context[c.reg:], reg)
				binary.LittleEndian.PutUint64(context[248:], uint64(memory)+uint64(plan.BodyRVA)+uint64(offset))
				write(rw, context)
				unwind.Call(0, memory, memory+uintptr(plan.BodyRVA)+uintptr(offset), memory+12288+12, rw, rw+1280, rw+1288, 0)
				context = read(rw, 1232)
				if binary.LittleEndian.Uint64(context[152:]) != uint64(original+8) || binary.LittleEndian.Uint64(context[248:]) != returned || binary.LittleEndian.Uint64(context[c.reg:]) != saved {
					t.Fatalf("unwind at %d did not restore caller: SP=%x RIP=%x reg=%x", offset, binary.LittleEndian.Uint64(context[152:]), binary.LittleEndian.Uint64(context[248:]), binary.LittleEndian.Uint64(context[c.reg:]))
				}
			}
		})
	}
}
