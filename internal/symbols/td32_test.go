package symbols

import (
	"encoding/binary"
	"testing"
)

func TestFromTD32(t *testing.T) {
	b := make([]byte, 110)
	p16 := func(n int, v uint16) { binary.LittleEndian.PutUint16(b[n:], v) }
	p32 := func(n int, v uint32) { binary.LittleEndian.PutUint32(b[n:], v) }
	copy(b, "FB09")
	p32(4, 8)
	p16(8, 16)
	p16(10, 12)
	p32(12, 2)
	p16(24, 0x125)
	p32(28, 48)
	p32(32, 48)
	p16(36, 0x130)
	p32(40, 96)
	p32(44, 7)
	p32(48, 1)
	p16(52, 42)
	p16(54, 0x204)
	p32(68, 16)
	p32(80, 32)
	p16(84, 2)
	p32(92, 1)
	p32(96, 1)
	b = append(b[:100], 1, 'H', 0, 'F', 'B', '0', '9', 0, 0, 0, 0)
	p32(44, 7)
	p32(len(b)-4, uint32(len(b)))
	index, err := FromTD32("lab", "lab.exe", 0x9000, b, map[uint16]Segment{2: {0x5000, 0x100}})
	if err != nil {
		t.Fatal(err)
	}
	for _, base := range []uint64{0x140000000, 0x7ff000000000} {
		loc, ok := index.Resolve(base, base+0x502f)
		if !ok || loc.Procedure != "H" {
			t.Fatalf("%+v", loc)
		}
		loc, ok = index.Resolve(base, base+0x5030)
		if !ok || loc.Status != "unknown" {
			t.Fatalf("%+v", loc)
		}
	}
	for _, segments := range []map[uint16]Segment{nil, {2: {0x5000, 40}}, {2: {0x8fff, 100}}} {
		if _, err := FromTD32("lab", "lab.exe", 0x9000, b, segments); err == nil {
			t.Fatal("accepted invalid mapping")
		}
	}
}
