package x64

import (
	"bytes"
	"encoding/binary"
	"errors"
	"testing"

	"golang.org/x/arch/x86/x86asm"
)

func TestLeafFlowRemapsConditionalAndBothReturns(t *testing.T) {
	code := append(bytes.Repeat([]byte{0x90}, 16), []byte{
		0x48, 0x85, 0xc9, // test rcx,rcx
		0x74, 4, // je second result
		0x48, 0x89, 0xc8, 0xc3, // mov rax,rcx; ret
		0x48, 0xc7, 0xc0, 1, 0, 0, 0, 0xc3, // mov rax,1; ret
	}...)
	p, err := BuildLeaf(code, 0x100000, 0x180000, 0x300000, 0x400000, 1)
	if err != nil {
		t.Fatal(err)
	}
	body := p.Image[p.BodyRVA:]
	inst, err := x86asm.Decode(body[19:], 64)
	if err != nil || inst.Op != x86asm.JE || inst.Len != 6 || int(inst.Args[0].(x86asm.Rel))+19+6 != 42 {
		t.Fatal("condition no longer reaches the second result", inst, err)
	}
	for _, offset := range []int{28, 49} {
		if !bytes.Equal(body[offset:offset+JumpSize], jump(0x180000+uint64(p.ExitRVA))) {
			t.Fatal("a return bypasses accounting", offset)
		}
	}
	if !bytes.Equal(p.Source, code) {
		t.Fatal("expected source lost original branches")
	}
}
func TestLeafFlowBackwardLoopAndAllConditions(t *testing.T) {
	code := append(bytes.Repeat([]byte{0x90}, 16), []byte{
		0x41, 0xba, 3, 0, 0, 0, // mov r10d,3
		0x49, 0x83, 0xea, 1, // sub r10,1
		0x75, 0xfa, // jne to subtraction
		0x48, 0x89, 0xc8, 0xc3,
	}...)
	p, err := BuildLeaf(code, 0x100000, 0x180000, 0x300000, 0x400000, 1)
	if err != nil {
		t.Fatal(err)
	}
	body := p.Image[p.BodyRVA:]
	inst, err := x86asm.Decode(body[26:], 64)
	if err != nil || inst.Op != x86asm.JNE || int(inst.Args[0].(x86asm.Rel))+26+inst.Len != 22 {
		t.Fatal("back edge changed", inst, err)
	}
	for condition := byte(0); condition < 16; condition++ {
		code = leafCode(0x70|condition, 0)
		p, err = BuildLeaf(code, 0x100000, 0x180000, 0x300000, 0x400000, 1)
		if err != nil {
			t.Fatal(err)
		}
		if p.Image[p.BodyRVA+16] != 0x0f || p.Image[p.BodyRVA+17] != 0x80|condition {
			t.Fatal("condition flags changed", condition)
		}
	}
}
func TestLeafFlowRelocatesRIPAfterExpandedReturn(t *testing.T) {
	code := leafCode(0xc3, 0x48, 0x8b, 0x05, 0, 0, 0, 0)
	binary.LittleEndian.PutUint32(code[20:], 0x1000-24)
	p, err := BuildLeaf(code, 0x100000, 0x180000, 0x300000, 0x400000, 1)
	if err != nil {
		t.Fatal(err)
	}
	inst, err := x86asm.Decode(p.Image[p.BodyRVA+30:], 64)
	if err != nil {
		t.Fatal(err)
	}
	mem := inst.Args[1].(x86asm.Mem)
	if int64(0x180000+p.BodyRVA+30+uint32(inst.Len))+int64(int32(mem.Disp)) != 0x101000 {
		t.Fatal("data read changed after return expansion", inst)
	}
}

func TestLeafFlowSignExtendsNegativeOriginalRIPDisplacement(t *testing.T) {
	code := leafCode(0x48, 0x8b, 0x05, 0, 0, 0, 0)
	disp := int32(-0x1000 - 23)
	binary.LittleEndian.PutUint32(code[19:], uint32(disp))
	p, err := BuildLeaf(code, 0x100000, 0x180000, 0x300000, 0x400000, 1)
	if err != nil {
		t.Fatal(err)
	}
	got := int64(int32(binary.LittleEndian.Uint32(p.Image[p.BodyRVA+19:]))) + int64(0x180000+p.BodyRVA+23)
	if got != 0xff000 {
		t.Fatalf("negative RIP target changed to %#x", got)
	}
}
func TestLeafFlowRejectsUnsafeTargetsAndExpansion(t *testing.T) {
	for _, code := range [][]byte{
		leafCode(0xeb, 1, 0x48, 0x89, 0xc8),           // into a MOV encoding
		leafCode(0xe9, 0xff, 0xff, 0x7f, 0),           // external jump
		leafCode(0xe2, 0xfe),                          // LOOP has different counter semantics
		leafCode(0xe3, 0),                             // JRCXZ cannot be widened like ordinary Jcc
		leafCode(0x66, 0x75, 0),                       // nonstandard prefixed branch
		append(bytes.Repeat([]byte{0xc3}, 440), 0xc3), // too large after RET expansion
	} {
		p, err := BuildLeaf(code, 0x100000, 0x180000, 0x300000, 0x400000, 1)
		if !errors.Is(err, ErrUnsupported) || len(p.Image) != 0 {
			t.Fatal("unsafe flow accepted", err)
		}
	}
}
