package desktop

import (
	"context"
	"fmt"
	"github.com/gabrielteiga/AntiPessimizerV2/internal/service"
	"github.com/gabrielteiga/AntiPessimizerV2/internal/transport"
	"os"
	"testing"
	"time"
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
	address := fmt.Sprintf(`\\.\pipe\AntiPessimizer-gui-test-%d`, time.Now().UnixNano())
	listener, err := transport.Listen(address)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	controller := service.New()
	done := make(chan error, 1)
	go func() { done <- service.Serve(ctx, listener, controller) }()
	defer func() {
		cancel()
		if err := <-done; err != nil {
			t.Error(err)
		}
	}()
	if err := controller.Start(ctx, service.StartOptions{Demo: true, WindowMS: 50}); err != nil {
		t.Fatal(err)
	}
	if err := run(address, func(w *window) error {
		if len(w.model.Rows) == 0 || w.model.Summary.Accepted == 0 {
			return fmt.Errorf("native viewer did not consume service snapshots")
		}
		if w.model.Omitted != 0 {
			return fmt.Errorf("viewer skipped retained windows: %d", w.model.Omitted)
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
}
