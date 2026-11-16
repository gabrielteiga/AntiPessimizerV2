package platform

import (
	"testing"
	"unsafe"
)

func TestETWABI(t *testing.T) {
	for name, v := range map[string][2]uintptr{"wnode": {unsafe.Sizeof(wnode{}), 48}, "properties": {unsafe.Offsetof(traceProperties{}.Name), 120}, "logfile": {unsafe.Sizeof(logfile{}), 448}, "callback": {unsafe.Offsetof(logfile{}.Callback), 424}, "record": {unsafe.Sizeof(eventRecord{}), 112}, "data": {unsafe.Offsetof(eventRecord{}.Data), 96}} {
		if v[0] != v[1] {
			t.Fatalf("%s: %d != %d", name, v[0], v[1])
		}
	}
}
