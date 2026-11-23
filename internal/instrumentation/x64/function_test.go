package x64

import (
	"bytes"
	"encoding/binary"
	"errors"
	"golang.org/x/arch/x86/x86asm"
	"testing"
)

func framedPE() ([]byte, []byte) {
	image := leafPE()
	code := []byte{0x53, 0x48, 0x83, 0xec, 0x20}
	code = append(code, bytes.Repeat([]byte{0x90}, 11)...)
	code = append(code, 0x48, 0x89, 0xcb, 0xe8, 0, 0, 0, 0, 0x48, 0x01, 0xd8, 0x48, 0x83, 0xc4, 0x20, 0x5b, 0xc3)
	binary.LittleEndian.PutUint32(code[20:], 0x1080-(peLeafRVA+24))
	copy(image[0x220:], code)
	binary.LittleEndian.PutUint32(image[0x404:], peLeafRVA+uint32(len(code)))
	copy(image[0x480:], []byte{1, 5, 2, 0, 5, 0x32, 1, 0x30})
	binary.LittleEndian.PutUint32(image[0x414:], 0x2090)
	image[0x490] = 1
	return image, code
}
func TestFramedPlanPreservesPrologueCallsAndRegistersUnwindHandler(t *testing.T) {
	image, code := framedPE()
	p, err := BuildPEFunction(image, peLeafRVA, uint32(len(code)), 0x100000, 0x180000, 0x300000, 0x400000, 0x500000, 7)
	if err != nil {
		t.Fatal(err)
	}
	body := p.Image[p.BodyRVA:]
	if !bytes.Equal(body[:19], code[:19]) || !bytes.Equal(p.Source, code) {
		t.Fatal("prologue or source changed")
	}
	inst, err := x86asm.Decode(body[19:], 64)
	if err != nil || inst.Op != x86asm.CALL || int64(0x180000+p.BodyRVA+24)+int64(inst.Args[0].(x86asm.Rel)) != 0x101080 {
		t.Fatal("callee address changed", inst, err)
	}
	f := p.Functions[1]
	info := p.Image[f.Unwind:]
	if f.Begin != 512 || info[0] != 17 || !bytes.Equal(info[1:8], []byte{5, 2, 0, 5, 0x32, 1, 0x30}) {
		t.Fatal("original unwind actions changed", f, info[:8])
	}
	thunk := binary.LittleEndian.Uint32(info[8:])
	if binary.LittleEndian.Uint32(info[12:]) != 7 || !bytes.Equal(p.Image[thunk:thunk+14], jump(0x500000)) {
		t.Fatal("cleanup handler target/id incorrect")
	}
	if !bytes.Equal(body[len(code)-1:len(code)-1+14], jump(0x180000+uint64(p.ExitRVA))) {
		t.Fatal("framed return bypasses exit")
	}
	if _, err := BuildPELeaf(image, peLeafRVA, uint32(len(code)), 0x100000, 0x180000, 0x300000, 0x400000, 7); !errors.Is(err, ErrUnsupported) {
		t.Fatal("legacy leaf gate weakened", err)
	}
}
func TestFramedPlanRejectsHandlersAndInvalidUnwindActions(t *testing.T) {
	for _, mutate := range []func([]byte){
		func(b []byte) { b[0x480] = 9 }, func(b []byte) { b[0x480] = 33 }, func(b []byte) { b[0x480] = 2 },
		func(b []byte) { b[0x481] = 255 }, func(b []byte) { b[0x482] = 255 }, func(b []byte) { b[0x484] = 0 },
		func(b []byte) { b[0x484] = 6 }, func(b []byte) { b[0x485] = 0x3a }, func(b []byte) { b[0x487] = 0x40 },
		func(b []byte) { b[0x483] = 5 }, func(b []byte) { b[0x485] = 1 },
		func(b []byte) { b[0x220] = 0x75; b[0x221] = 0 },
	} {
		image, code := framedPE()
		mutate(image)
		if p, err := BuildPEFunction(image, peLeafRVA, uint32(len(code)), 0x100000, 0x180000, 0x300000, 0x400000, 0x500000, 7); !errors.Is(err, ErrUnsupported) || len(p.Image) != 0 {
			t.Fatal("invalid frame accepted", err)
		}
	}
}
func TestFunctionPlanLeafStillReceivesCleanupMetadata(t *testing.T) {
	image := leafPE()
	p, err := BuildPEFunction(image, peLeafRVA, peLeafSize, 0x100000, 0x180000, 0x300000, 0x400000, 0x500000, 1)
	if err != nil {
		t.Fatal(err)
	}
	f := p.Functions[1]
	if !bytes.Equal(p.Image[f.Unwind:f.Unwind+4], []byte{17, 0, 0, 0}) {
		t.Fatal("leaf cleanup metadata missing")
	}
}

func TestUnwindActionsMatchStackAndSavedRegisterOffsets(t *testing.T) {
	cases := []struct {
		name       string
		code, info []byte
	}{
		{"frame pointer", []byte{0x55, 0x48, 0x83, 0xec, 0x28, 0x48, 0x8d, 0x6c, 0x24, 0x20}, []byte{1, 10, 3, 0x25, 10, 3, 5, 0x42, 1, 0x50, 0, 0}},
		{"save before allocation", []byte{0x48, 0x89, 0x5c, 0x24, 8, 0x48, 0x83, 0xec, 0x28}, []byte{1, 9, 3, 0, 9, 0x42, 5, 0x34, 6, 0, 0, 0}},
		{"SSE save", []byte{0x48, 0x83, 0xec, 0x38, 0xf3, 0x0f, 0x7f, 0x74, 0x24, 0x20}, []byte{1, 10, 3, 0, 10, 0x68, 2, 0, 4, 0x62, 0, 0}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if err := validateUnwindCodes(c.info); err != nil {
				t.Fatal(err)
			}
			if err := validatePrologueActions(c.code, c.info); err != nil {
				t.Fatal(err)
			}
			bad := append([]byte(nil), c.info...)
			if c.name == "frame pointer" {
				bad[3] = 0x15
			} else if c.name == "save before allocation" {
				bad[8]++
			} else {
				bad[6]++
			}
			if err := validatePrologueActions(c.code, bad); err == nil {
				t.Fatal("wrong saved-register offset accepted")
			}
		})
	}
	code := []byte{0x53, 0x48, 0x83, 0xec, 0x20}
	if err := validatePrologueActions(code, []byte{1, 5, 1, 0, 1, 0x30, 0, 0}); err == nil {
		t.Fatal("unrecorded allocation accepted")
	}
}

func FuzzNativeFunctionPlan(f *testing.F) {
	image, code := framedPE()
	f.Add(image, uint32(len(code)))
	image, code = chainedPE()
	f.Add(image, uint32(len(code)))
	f.Fuzz(func(t *testing.T, b []byte, size uint32) {
		if len(b) > 1<<20 {
			t.Skip()
		}
		p, err := BuildPEFunction(b, peLeafRVA, size, 0x100000, 0x180000, 0x300000, 0x400000, 0x500000, 1)
		if err != nil && len(p.Image) != 0 {
			t.Fatal("failed plan returned executable bytes")
		}
	})
}

func TestFramedColdBlockCanEndInInternalJump(t *testing.T) {
	code := []byte{0x48, 0x83, 0xec, 0x28, 0x85, 0xc9, 0x74, 5, 0x48, 0x83, 0xc4, 0x28, 0xc3, 0x31, 0xc9, 0xeb, 0xf7}
	body, err := rewriteFunctionBody(code, 0x100000, 0x180000, true, 4)
	if err != nil {
		t.Fatal(err)
	}
	if len(body.code) == 0 || body.overwrite < JumpSize {
		t.Fatal("cold branch lost")
	}
	// A final conditional branch still falls through into another procedure.
	code[len(code)-2] = 0x75
	if _, err := rewriteFunctionBody(code, 0x100000, 0x180000, true, 4); err == nil {
		t.Fatal("fallthrough outside selected extent accepted")
	}
}
