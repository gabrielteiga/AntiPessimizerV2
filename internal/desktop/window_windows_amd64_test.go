package desktop

import (
	"os"
	"testing"
	"unsafe"
)

func TestNativeLayouts(t *testing.T) {
	for name, v := range map[string][2]uintptr{"class": {unsafe.Sizeof(windowClass{}), 80}, "message": {unsafe.Sizeof(message{}), 48}, "item": {unsafe.Sizeof(item{}), 88}, "display": {unsafe.Sizeof(displayInfo{}), 112}, "paint": {unsafe.Sizeof(paint{}), 72}} {
		if v[0] != v[1] {
			t.Fatalf("%s: %d != %d", name, v[0], v[1])
		}
	}
}
func TestNativeWindowSmoke(t *testing.T) {
	if os.Getenv("AP_GUI_SMOKE") != "1" {
		t.Skip("enabled in Windows CI")
	}
	if err := Run(`\\.\pipe\AntiPessimizer-smoke-unused`); err != nil {
		t.Fatal(err)
	}
}
