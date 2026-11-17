package native

import (
	"bytes"
	"encoding/binary"
	"errors"
	"io"
	"testing"
)

func hello() []byte {
	b := make([]byte, 56)
	copy(b, "APNAT01\x00")
	binary.LittleEndian.PutUint32(b[8:], 1)
	binary.LittleEndian.PutUint32(b[12:], 24)
	binary.LittleEndian.PutUint64(b[16:], 1000)
	binary.LittleEndian.PutUint32(b[24:], 42)
	binary.LittleEndian.PutUint32(b[28:], 1)
	binary.LittleEndian.PutUint32(b[32:], 0x10000)
	binary.LittleEndian.PutUint64(b[40:], 100)
	binary.LittleEndian.PutUint32(b[48:], 1)
	binary.LittleEndian.PutUint32(b[52:], 0x1000)
	return b
}
func packet(sequence, lost, final uint64, events ...[5]uint64) []byte {
	b := make([]byte, 32+len(events)*24)
	binary.LittleEndian.PutUint32(b, 0x31425041)
	binary.LittleEndian.PutUint32(b[4:], uint32(len(events)))
	binary.LittleEndian.PutUint64(b[8:], sequence)
	binary.LittleEndian.PutUint64(b[16:], lost)
	binary.LittleEndian.PutUint64(b[24:], final)
	for i, e := range events {
		p := b[32+i*24:]
		for j := 0; j < 4; j++ {
			binary.LittleEndian.PutUint32(p[j*4:], uint32(e[j]))
		}
		binary.LittleEndian.PutUint64(p[16:], e[4])
	}
	return b
}
func TestNativeWireLifecycleAndCompletion(t *testing.T) {
	data := hello()
	data = append(data, packet(1, 0, 0, [5]uint64{1, 7, 0, 0, 101}, [5]uint64{2, 7, 1, 0, 102}, [5]uint64{3, 7, 1, 0, 112}, [5]uint64{4, 7, 0, 0, 113})...)
	data = append(data, packet(2, 0, 114)...)
	r, err := NewReader(bytes.NewReader(data), 42)
	if err != nil {
		t.Fatal(err)
	}
	if r.Header.Frequency != 1000 || r.Header.Procedures[0].RVA != 0x1000 {
		t.Fatal(r.Header)
	}
	for _, kind := range []string{"thread_start", "enter", "exit", "thread_end"} {
		e, err := r.Next()
		if err != nil || e.Type != kind || e.Thread != 7 {
			t.Fatalf("%+v %v", e, err)
		}
	}
	if _, err := r.Next(); err != io.EOF || r.FinalTick() != 114 {
		t.Fatalf("%d %v", r.FinalTick(), err)
	}
}
func TestNativeWireRejectsIdentityAndLayout(t *testing.T) {
	for name, change := range map[string]func([]byte){
		"magic":   func(b []byte) { b[0] = 'X' },
		"version": func(b []byte) { b[8] = 2 },
		"size":    func(b []byte) { b[12] = 25 },
		"pid":     func(b []byte) { b[24] = 43 },
		"clock": func(b []byte) {
			for i := 16; i < 24; i++ {
				b[i] = 0
			}
		},
		"count":    func(b []byte) { b[28] = 65 },
		"reserved": func(b []byte) { b[36] = 1 },
		"RVA":      func(b []byte) { binary.LittleEndian.PutUint32(b[52:], 0x10000) },
		"zero ID":  func(b []byte) { b[48] = 0 },
	} {
		t.Run(name, func(t *testing.T) {
			b := hello()
			change(b)
			if _, err := NewReader(bytes.NewReader(b), 42); err == nil {
				t.Fatal("invalid handshake accepted")
			}
		})
	}
}
func TestNativeWireRejectsLossTruncationAndBadEvents(t *testing.T) {
	for name, tail := range map[string][]byte{
		"no final packet":      packet(1, 0, 0, [5]uint64{1, 7, 0, 0, 101}),
		"sequence gap":         packet(2, 0, 101),
		"lost events":          packet(1, 3, 101),
		"unselected procedure": packet(1, 0, 0, [5]uint64{2, 7, 2, 0, 101}),
		"zero thread":          packet(1, 0, 0, [5]uint64{1, 0, 0, 0, 101}),
		"back before start":    packet(1, 0, 0, [5]uint64{1, 7, 0, 0, 99}),
		"wrong event fields":   packet(1, 0, 0, [5]uint64{1, 7, 1, 0, 101}),
		"unknown event":        packet(1, 0, 0, [5]uint64{6, 7, 0, 0, 101}),
		"trailing data":        append(packet(1, 0, 101), 'x'),
		"empty batch":          packet(1, 0, 0),
	} {
		t.Run(name, func(t *testing.T) {
			r, err := NewReader(bytes.NewReader(append(hello(), tail...)), 42)
			if err != nil {
				t.Fatal(err)
			}
			for i := 0; i < 8; i++ {
				_, err = r.Next()
				if err != nil {
					break
				}
			}
			if err == nil || err == io.EOF {
				t.Fatal("invalid/incomplete stream accepted")
			}
			if name == "lost events" && (!errors.Is(err, ErrLoss) || r.Lost() != 3) {
				t.Fatal(err)
			}
		})
	}
}
func FuzzNativeWire(f *testing.F) {
	f.Add(append(hello(), packet(1, 0, 101)...))
	f.Fuzz(func(t *testing.T, b []byte) {
		if len(b) > 1<<20 {
			t.Skip()
		}
		r, err := NewReader(bytes.NewReader(b), 42)
		if err != nil {
			return
		}
		for i := 0; i < 4096; i++ {
			if _, err := r.Next(); err != nil {
				return
			}
		}
	})
}
