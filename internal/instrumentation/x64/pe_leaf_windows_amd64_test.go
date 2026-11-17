package x64

import (
	"encoding/binary"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"unsafe"

	"golang.org/x/sys/windows"
)

// Compare the file exception directory with the native loader's function table
// for a real GCC-built DLL. No procedure is called or patched by this test.
func TestNativePELeafUnwindEligibility(t *testing.T) {
	lab := os.Getenv("AP_NATIVE_LAB")
	if lab == "" {
		t.Skip("requires the controlled native DLL built in Windows CI")
	}
	path := filepath.Join(filepath.Dir(lab), "AntiPessimizerNative.dll")
	image, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	dll, err := windows.LoadLibraryEx(path, 0, windows.LOAD_LIBRARY_SEARCH_DLL_LOAD_DIR|windows.LOAD_LIBRARY_SEARCH_SYSTEM32)
	if err != nil {
		t.Fatal(err)
	}
	defer windows.FreeLibrary(dll)
	address, err := windows.GetProcAddress(dll, "APNativeInitHooks")
	if err != nil {
		t.Fatal(err)
	}
	var base uint64
	entry, _, callErr := windows.NewLazySystemDLL("ntdll.dll").NewProc("RtlLookupFunctionEntry").Call(address, uintptr(unsafe.Pointer(&base)), 0)
	runtime.KeepAlive(&base)
	if entry == 0 || base != uint64(dll) {
		t.Fatal("native DLL has no expected runtime function", entry, base, callErr)
	}
	var row [12]byte
	var n uintptr
	if err := windows.ReadProcessMemory(windows.CurrentProcess(), entry, &row[0], 12, &n); err != nil || n != 12 {
		t.Fatal("read runtime function", n, err)
	}
	begin, end := binary.LittleEndian.Uint32(row[:]), binary.LittleEndian.Uint32(row[4:])
	if base+uint64(begin) != uint64(address) || end <= begin || end-begin > 4096 {
		t.Fatal("unexpected native procedure range", begin, end)
	}
	p, err := BuildPELeaf(image, begin, end-begin, base, 0x100000, 0x500000, 0x500100, 1)
	if !errors.Is(err, ErrUnsupported) || !strings.Contains(err.Error(), "procedure requires unwind/exception handling") || len(p.Image) != 0 {
		t.Fatal("non-leaf DLL procedure was not rejected by its real unwind metadata", p, err)
	}
	t.Logf("native loader and file table agree: APNativeInitHooks RVA=%#x length=%d; frame rejected before clone/patch", begin, end-begin)
}
