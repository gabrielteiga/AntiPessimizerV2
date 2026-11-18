package x64

import (
	"encoding/binary"
	"errors"
	"strings"
	"testing"
)

// A standard x64 import thunk, identified by its IAT slot and import name,
// supplies the scope-table format. An arbitrary handler never gets that format.
func w32(b []byte, at int, value uint32) { binary.LittleEndian.PutUint32(b[at:], value) }

func scopesPE() ([]byte, []byte) {
	image, code := framedPE()
	image = append(image, make([]byte, 0x400)...)
	// Enlarge .pdata raw/virtual section for a minimal bounded import table.
	w32(image, peSections+40+8, 0x600)
	w32(image, peSections+40+16, 0x600)
	image[0x480] = 9
	w32(image, 0x488, 0x1080)
	copy(image[0x280:], []byte{0xff, 0x25, 0xca, 0x10, 0, 0}) // IAT at 0x2150
	w32(image, 0x98+120, 0x2100)
	w32(image, 0x98+124, 40)
	w32(image, 0x500, 0x2140)
	w32(image, 0x50c, 0x2180)
	w32(image, 0x510, 0x2150)
	binary.LittleEndian.PutUint64(image[0x540:], 0x2160)
	copy(image[0x562:], "__C_specific_handler\x00")
	copy(image[0x580:], "msvcrt.dll\x00")
	w32(image, 0x48c, 1)
	w32(image, 0x490, peLeafRVA+5)
	w32(image, 0x494, peLeafRVA+9)
	w32(image, 0x498, 1)
	w32(image, 0x49c, peLeafRVA+9)
	return image, code
}
func TestExceptionScopesRelocateIntoClonedBody(t *testing.T) {
	image, code := scopesPE()
	p, err := BuildPEFunction(image, peLeafRVA, uint32(len(code)), 0x100000, 0x180000, 0x300000, 0x400000, 0x500000, 7)
	if err != nil {
		t.Fatal(err)
	}
	if !p.RelocatedHandler {
		t.Fatal("exception handler not marked for protocol negotiation")
	}
	info := p.Image[p.Functions[1].Unwind:]
	if info[0] != 25 {
		t.Fatal("clone must participate in search and unwind", info[0])
	}
	d := info[12:]
	if binary.LittleEndian.Uint32(d[4:]) != 2 {
		t.Fatal("wrong scope descriptor")
	}
	scope := d[binary.LittleEndian.Uint32(d[52:]):]
	for i, want := range []uint32{1, 0x80205, 0x80209, 1, 0x80209} {
		if got := binary.LittleEndian.Uint32(scope[i*4:]); got != want {
			t.Fatalf("scope[%d]=%#x want %#x", i, got, want)
		}
	}
	shadow := d[binary.LittleEndian.Uint32(d[48:]):]
	if binary.LittleEndian.Uint32(shadow) != 0x80200 || binary.LittleEndian.Uint32(shadow[4:]) != 0x80200+p.Functions[1].End-p.Functions[1].Begin {
		t.Fatal("shadow runtime range", shadow[:12])
	}
	alias := p.Image[binary.LittleEndian.Uint32(shadow[8:])-0x80000:]
	if alias[0] != 25 || binary.LittleEndian.Uint32(alias[8:]) != 0x80000+binary.LittleEndian.Uint32(info[8:]) || binary.LittleEndian.Uint32(alias[16:]) != 3 {
		t.Fatal("shadow metadata must retain wrapper during collided unwind", alias)
	}
	if binary.LittleEndian.Uint64(alias[20:]) != 0x180000+uint64(p.Functions[1].Unwind)+12 {
		t.Fatal("alias descriptor pointer")
	}
}
func TestExceptionScopeRejectsUnknownHandlersAndMalformedScopes(t *testing.T) {
	for name, mutate := range map[string]func([]byte){
		"handler name":                  func(b []byte) { b[0x562] = 'X' },
		"handler module":                func(b []byte) { b[0x580] = 'X' },
		"missing import terminator":     func(b []byte) { w32(b, 0x98+124, 20) },
		"wrong IAT slot":                func(b []byte) { b[0x282]++ },
		"ordinal import":                func(b []byte) { binary.LittleEndian.PutUint64(b[0x540:], 1<<63|1) },
		"unbounded scopes":              func(b []byte) { w32(b, 0x48c, 65) },
		"empty scopes":                  func(b []byte) { w32(b, 0x48c, 0) },
		"range before prologue":         func(b []byte) { w32(b, 0x490, peLeafRVA) },
		"backwards range":               func(b []byte) { w32(b, 0x494, peLeafRVA+5) },
		"scope end outside body":        func(b []byte) { w32(b, 0x494, peLeafRVA+100) },
		"scope middle of instruction":   func(b []byte) { w32(b, 0x494, peLeafRVA+21) },
		"landing outside body":          func(b []byte) { w32(b, 0x49c, 0x1080) },
		"landing middle of instruction": func(b []byte) { w32(b, 0x49c, peLeafRVA+21) },
		"filter inside body":            func(b []byte) { w32(b, 0x498, peLeafRVA+5) },
		"invalid filter":                func(b []byte) { w32(b, 0x498, 0) },
		"cleanup lacks unwind flag":     func(b []byte) { w32(b, 0x49c, 0); w32(b, 0x498, 0x1080) },
	} {
		t.Run(name, func(t *testing.T) {
			image, code := scopesPE()
			mutate(image)
			p, err := BuildPEFunction(image, peLeafRVA, uint32(len(code)), 0x100000, 0x180000, 0x300000, 0x400000, 0x500000, 7)
			if !errors.Is(err, ErrUnsupported) || len(p.Image) != 0 {
				t.Fatal("unsafe scope accepted", p, err)
			}
		})
	}
	image, code := scopesPE()
	for i := 19; i < 24; i++ {
		image[0x220+i] = 0x90
		code[i] = 0x90
	}
	_, err := BuildPEFunction(image, peLeafRVA, uint32(len(code)), 0x100000, 0x1000000000, 0x300000, 0x400000, 0x500000, 7)
	if err == nil || !strings.Contains(err.Error(), "image-relative") {
		t.Fatal("unrepresentable forwarding RVAs accepted", err)
	}
}
