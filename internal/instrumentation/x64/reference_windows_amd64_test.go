package x64

import (
	"bytes"
	"crypto/sha256"
	"debug/pe"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"syscall"
	"testing"
	"unsafe"

	"golang.org/x/sys/windows"
)

const referenceDLLSHA = "1c1bf9288a3bfd05303f2b9a86d29693c0a449e716f575a25a59059f1c0d02b5"

type referenceCase struct {
	RVA  uint32 `json:"rva"`
	Hex  string `json:"source_hex"`
	Kind string `json:"case"`
}

// Only reviewed, exact bytes from the original v0.3 DLL are executable here.
// These small routines use no imports, globals, initialized RTL or private ABI.
// There is no claim about names (the release has no TDS) or compiler version.
var referenceCases = []referenceCase{
	{0x19210, "488d042560020000c3", "constant"},
	{0x7a9e0, "488b4120c3", "field32"},
	{0x8c460, "488b4108c3", "field8"},
	{0x3ffe0, "488b41084885c07404488b40f8c3", "nullable"},
	{0x8e900, "488b4108480fb7400ac3", "word"},
	{0x98700, "488b41088038ff0f95c0c3", "boolean"},
	{0x18020, "554883ec10488bec488b0148894508488b0248894500488b4508483b45000f94c0488d65105dc3", "equal-framed"},
	{0x18050, "554883ec10488bec488b0148894508488b0248894500488b4508483b45000f95c0488d65105dc3", "not-equal-framed"},
	{0x18570, "554883ec10488bec488b024a8b040048894508488b4508488d65105dc3", "indexed-framed"},
	{0x18700, "554883ec10488bec48895508488b4508488d65105dc3", "argument-framed"},
}

func referenceAllocation(t *testing.T, loaded uint64, imageSize uint32) uintptr {
	t.Helper()
	const size = uintptr(16384)
	const memoryFree = 0x10000 // MEM_FREE (VirtualQuery state)
	for address := (uintptr(loaded) + uintptr(imageSize) + 0xffff) &^ 0xffff; uint64(address) < min(maxUserAddress-uint64(size), loaded+0x70000000); {
		var memory windows.MemoryBasicInformation
		if err := windows.VirtualQuery(address, &memory, unsafe.Sizeof(memory)); err != nil {
			t.Fatal(err)
		}
		if memory.State == memoryFree && memory.RegionSize >= size {
			if p, err := windows.VirtualAlloc(address, size, windows.MEM_COMMIT|windows.MEM_RESERVE, windows.PAGE_READWRITE); err == nil {
				return p
			}
		}
		next := (memory.BaseAddress + memory.RegionSize + 0xffff) &^ 0xffff
		if next <= address {
			break
		}
		address = next
	}
	t.Fatal("no nearby allocation for reviewed five-byte routines")
	return 0
}

func TestReferenceDelphiReadOnlyRoutinesPreserveNativeResults(t *testing.T) {
	path := os.Getenv("AP_REFERENCE_DELPHI_DLL")
	if path == "" {
		t.Skip("pinned original Delphi release is downloaded in a separate Windows CI step")
	}
	image, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if got := fmt.Sprintf("%x", sha256.Sum256(image)); got != referenceDLLSHA {
		t.Fatal("original DLL hash changed; refuse all execution", got)
	}
	f, err := pe.NewFile(bytes.NewReader(image))
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	h := f.OptionalHeader.(*pe.OptionalHeader64)
	// DONT_RESOLVE_DLL_REFERENCES deliberately skips DllMain, TLS initialization
	// and dependencies. Never call anything except the exact pure-byte corpus.
	path, err = filepath.Abs(path)
	if err != nil {
		t.Fatal(err)
	}
	dll, err := windows.LoadLibraryEx(path, 0, windows.DONT_RESOLVE_DLL_REFERENCES)
	if err != nil {
		t.Fatal(err)
	}
	defer windows.FreeLibrary(dll)
	rw, err := windows.VirtualAlloc(0, 4096, windows.MEM_COMMIT|windows.MEM_RESERVE, windows.PAGE_READWRITE)
	if err != nil {
		t.Fatal(err)
	}
	defer windows.VirtualFree(rw, 0, windows.MEM_RELEASE)
	write := func(address uintptr, data []byte) {
		t.Helper()
		var n uintptr
		if err := windows.WriteProcessMemory(windows.CurrentProcess(), address, &data[0], uintptr(len(data)), &n); err != nil || n != uintptr(len(data)) {
			t.Fatal("write controlled memory", n, err)
		}
	}
	read := func(address uintptr, size int) []byte {
		t.Helper()
		data := make([]byte, size)
		var n uintptr
		if err := windows.ReadProcessMemory(windows.CurrentProcess(), address, &data[0], uintptr(size), &n); err != nil || n != uintptr(size) {
			t.Fatal("read reviewed code", n, err)
		}
		return data
	}
	var calls uint64
	for _, c := range referenceCases {
		t.Run(fmt.Sprintf("RVA-%x-%s", c.RVA, c.Kind), func(t *testing.T) {
			code, err := hex.DecodeString(c.Hex)
			if err != nil || !bytes.Equal(read(uintptr(dll)+uintptr(c.RVA), len(code)), code) {
				t.Fatal("loaded code differs from reviewed bytes", err)
			}
			memory := referenceAllocation(t, uint64(dll), h.SizeOfImage)
			nt := windows.NewLazySystemDLL("ntdll.dll")
			registered := false
			defer func() {
				if registered {
					if ok, _, err := nt.NewProc("RtlDeleteFunctionTable").Call(memory + 12288); ok == 0 {
						t.Error("retire original corpus table", err)
						return // no freeing registered executable memory
					}
				}
				if err := windows.VirtualFree(memory, 0, windows.MEM_RELEASE); err != nil {
					t.Error(err)
				}
			}()
			plan, err := BuildPEFunction(image, c.RVA, uint32(len(code)), uint64(dll), uint64(memory), uint64(memory+8192), uint64(memory+8192), uint64(memory+8192), 17)
			if err != nil {
				t.Fatal(err)
			}
			if !bytes.Equal(plan.Source, code) || len(plan.Image) >= 8192 {
				t.Fatal("source extent or clone layout differs")
			}
			write(memory, plan.Image)
			// A native Win64 leaf helper clobbers every volatile GPR. It ends
			// with ContinueSearch for the cleanup ABI; no Go callback is involved.
			var helper []byte
			for _, reg := range []byte{0, 1, 2, 8, 9, 10, 11} {
				rex := byte(0x48)
				if reg >= 8 {
					rex |= 1
				}
				helper = append(helper, rex, 0xb8|(reg&7))
				helper = append(helper, bytes.Repeat([]byte{0x77}, 8)...)
			}
			helper = append(helper, 0xb8, 1, 0, 0, 0, 0xc3)
			write(memory+8192, helper)
			write(memory+8300, []byte{1, 0, 0, 0})
			functions := append(plan.Functions[:], RuntimeFunction{8192, 8192 + uint32(len(helper)), 8300})
			var table bytes.Buffer
			for _, function := range functions {
				if err := binary.Write(&table, binary.LittleEndian, function); err != nil {
					t.Fatal(err)
				}
			}
			write(memory+12288, table.Bytes())
			var old uint32
			if err := windows.VirtualProtect(memory, 16384, windows.PAGE_EXECUTE_READ, &old); err != nil {
				t.Fatal(err)
			}
			if ok, _, err := windows.NewLazySystemDLL("kernel32.dll").NewProc("FlushInstructionCache").Call(uintptr(windows.CurrentProcess()), memory, 16384); ok == 0 {
				t.Fatal(err)
			}
			if ok, _, err := nt.NewProc("RtlAddFunctionTable").Call(memory+12288, uintptr(len(functions)), memory); ok == 0 {
				t.Fatal("register original corpus table", err)
			}
			registered = true
			for trial := uint64(0); trial < 100; trial++ {
				input := make([]byte, 256)
				put := binary.LittleEndian.PutUint64
				put(input, 0x123456789abcd000+trial)
				put(input[8:], uint64(rw+512))
				put(input[32:], 0xfedcba9876543200+trial)
				secondary := make([]byte, 32)
				secondary[0] = 0xff
				if trial%2 != 0 {
					secondary[0] = 0x7f
				}
				binary.LittleEndian.PutUint16(secondary[10:], uint16(trial+1000))
				var mask, want uint64 = ^uint64(0), 0
				var peer [8]byte
				put(peer[:], 0x123456789abcd000+trial+trial%2)
				arguments := []uintptr{rw + 128, rw + 384, uintptr(trial % 4 * 8)}
				switch c.Kind {
				case "constant":
					want = 0x260
				case "field32":
					want = 0xfedcba9876543200 + trial
				case "field8":
					want = uint64(rw + 512)
				case "nullable":
					if trial%2 == 0 {
						put(input[8:], 0)
					} else {
						want = 0x123456789abcdef0 + trial
					}
				case "word":
					want = trial + 1000
				case "boolean":
					mask, want = 0xff, trial%2
				case "equal-framed":
					mask, want = 0xff, 1-trial%2
				case "not-equal-framed":
					mask, want = 0xff, trial%2
				case "indexed-framed":
					put(peer[:], uint64(rw+512))
					for i := uint64(0); i < 4; i++ {
						put(secondary[i*8:], 0x123456789000+i+trial)
					}
					want = 0x123456789000 + trial%4 + trial
				case "argument-framed":
					want = 0xfedcba9876543000 + trial
					arguments[1] = uintptr(want)
				}
				write(rw+128, input)
				write(rw+512, secondary)
				write(rw+384, peer[:])
				var preceding [8]byte
				put(preceding[:], want)
				write(rw+504, preceding[:])
				original, _, _ := syscall.SyscallN(uintptr(dll)+uintptr(c.RVA), arguments...)
				cloned, _, _ := syscall.SyscallN(memory, arguments...)
				if uint64(original)&mask != want || uint64(cloned)&mask != want {
					t.Fatalf("original/relocated result changed: trial=%d original=%#x clone=%#x want=%#x mask=%#x", trial, original, cloned, want, mask)
				}
				calls++
			}
			if !bytes.Equal(read(uintptr(dll)+uintptr(c.RVA), len(code)), code) {
				t.Fatal("test modified original Delphi code")
			}
		})
	}
	runtime.KeepAlive(image)
	t.Logf("pinned original Delphi DLL: %d reviewed routines, %d original/clone pairs, native volatile-GPR clobber, no DLL initialization or source patch", len(referenceCases), calls)
	if path := os.Getenv("AP_REFERENCE_DELPHI_REPORT"); path != "" {
		data, err := json.MarshalIndent(struct {
			Version int             `json:"version"`
			Source  string          `json:"source"`
			SHA256  string          `json:"dll_sha256"`
			Cases   []referenceCase `json:"cases"`
			Pairs   uint64          `json:"original_clone_pairs"`
		}{1, "Hoshoyo/AntiPessimizer v0.3", referenceDLLSHA, referenceCases, calls}, "", "  ")
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, append(data, '\n'), 0600); err != nil {
			t.Fatal(err)
		}
	}
}
