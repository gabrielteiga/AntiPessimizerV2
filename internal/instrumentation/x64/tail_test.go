package x64

import (
	"bytes"
	"encoding/binary"
	"errors"
	"golang.org/x/arch/x86/x86asm"
	"testing"
)

func selectedTailImage(target uint32) ([]byte, []byte) {
	code := []byte{0x48, 0x83, 0xc1, 1, 0xe9, 0, 0, 0, 0}
	binary.LittleEndian.PutUint32(code[5:], target-(peLeafRVA+uint32(len(code))))
	return scalarImage(code), code
}
func TestSelectedTailJumpKeepsOriginalStackAndExactEntry(t *testing.T) {
	image, source := selectedTailImage(0x1080)
	p, err := NewPEImage(image)
	if err != nil {
		t.Fatal(err)
	}
	plan, err := p.BuildSelectedFunction(peLeafRVA, uint32(len(source)), 0x100000, 0x180000, 0x300000, 0x400000, 0x500000, 1, []uint32{peLeafRVA, 0x1080})
	if err != nil {
		t.Fatal(err)
	}
	body := plan.Image[plan.BodyRVA:plan.Functions[1].End]
	inst, err := x86asm.Decode(body[4:], 64)
	if err != nil || inst.Op != x86asm.JMP || int64(0x180000+plan.BodyRVA+9)+int64(inst.Args[0].(x86asm.Rel)) != 0x101080 || !bytes.Equal(body[:4], source[:4]) || len(plan.TailTargets) != 1 || plan.TailTargets[0] != 0x1080 {
		t.Fatal("tail transfer altered stack/target", inst, plan.TailTargets, err)
	}
	if _, err := p.BuildFunction(peLeafRVA, uint32(len(source)), 0x100000, 0x180000, 0x300000, 0x400000, 0x500000, 1); !errors.Is(err, ErrUnsupported) {
		t.Fatal("standalone plan bypassed selection dependency", err)
	}
	for _, selected := range [][]uint32{nil, {peLeafRVA}, {0x1081}, {0xffffffff}} {
		if _, err := p.BuildSelectedFunction(peLeafRVA, uint32(len(source)), 0x100000, 0x180000, 0x300000, 0x400000, 0x500000, 1, selected); !errors.Is(err, ErrUnsupported) {
			t.Fatal("missing/exact target gate weakened", selected, err)
		}
	}
}
func TestSelectedTailJumpRejectsUnrestoredFramedIndirectOrConditionalTransfers(t *testing.T) {
	for _, code := range [][]byte{
		{0x48, 0x83, 0xec, 0x28, 0xe9, 0x57, 0, 0, 0}, // stack modification with empty metadata
		{0x0f, 0x85, 0x5a, 0, 0, 0},                   // conditional external branch
		{0x48, 0x89, 0xc8, 0xff, 0xe0},                // indirect JMP
	} {
		p, err := NewPEImage(scalarImage(code))
		if err != nil {
			t.Fatal(err)
		}
		if _, err := p.BuildSelectedFunction(peLeafRVA, uint32(len(code)), 0x100000, 0x180000, 0x300000, 0x400000, 0x500000, 1, []uint32{0x1080}); !errors.Is(err, ErrUnsupported) {
			t.Fatal("unsupported tail transfer", code, err)
		}
	}
	image, code := framedPE()
	image[0x220+19] = 0xe9
	p, err := NewPEImage(image)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := p.BuildSelectedFunction(peLeafRVA, uint32(len(code)), 0x100000, 0x180000, 0x300000, 0x400000, 0x500000, 1, []uint32{0x1080}); !errors.Is(err, ErrUnsupported) {
		t.Fatal("framed tail caller accepted", err)
	}
}

func TestSelectedIndirectTailRequiresAnUnskippableExactAddress(t *testing.T) {
	const loaded, clone = uint64(0x100000), uint64(0x180000)
	code := []byte{0x4c, 0x8d, 0x1d, 0, 0, 0, 0, 0x41, 0xff, 0xe3}
	binary.LittleEndian.PutUint32(code[3:], 0x1080-(peLeafRVA+7))
	build := func(code []byte, selection []uint32) (FunctionPlan, error) {
		p, err := NewPEImage(scalarImage(code))
		if err != nil {
			t.Fatal(err)
		}
		return p.BuildSelectedFunction(peLeafRVA, uint32(len(code)), loaded, clone, 0x300000, 0x400000, 0x500000, 1, selection)
	}
	selected := []uint32{0x1080}
	plan, err := build(code, selected)
	if err != nil {
		t.Fatal(err)
	}
	body := plan.Image[plan.BodyRVA:plan.Functions[1].End]
	leaTarget := int64(clone+uint64(plan.BodyRVA)+7) + int64(int32(binary.LittleEndian.Uint32(body[3:])))
	jumpTarget := int64(clone+uint64(plan.BodyRVA)+12) + int64(int32(binary.LittleEndian.Uint32(body[8:])))
	if body[7] != 0xe9 || leaTarget != 0x101080 || jumpTarget != leaTarget || len(plan.TailTargets) != 1 || plan.TailTargets[0] != 0x1080 {
		t.Fatal("indirect tail changed the register or transfer destination", body, plan.TailTargets)
	}
	for _, unsupported := range [][]uint32{nil, {0x1081}, {peLeafRVA}} {
		if _, err := build(code, unsupported); !errors.Is(err, ErrUnsupported) {
			t.Fatal("indirect tail accepted without exact selection", unsupported, err)
		}
	}
	bypass := append([]byte{0x75, 7}, code...)
	binary.LittleEndian.PutUint32(bypass[5:], 0x1080-(peLeafRVA+9))
	changedRegister := bytes.Clone(code)
	changedRegister[2] = 0x15 // LEA R10 followed by JMP R11
	for _, bad := range [][]byte{bypass, changedRegister, append([]byte{0x90, 0x90}, code[7:]...)} {
		if _, err := build(bad, selected); !errors.Is(err, ErrUnsupported) {
			t.Fatal("unproven indirect destination accepted", bad, err)
		}
	}
}

func TestConditionalIndirectTailRequiresBothExactAddresses(t *testing.T) {
	const loaded, clone = uint64(0x100000), uint64(0x180000)
	code := []byte{
		0x75, 0x00, // JNE to the first LEA, so both destination assignments run
		0x4c, 0x8d, 0x1d, 0, 0, 0, 0,
		0x4c, 0x8d, 0x15, 0, 0, 0, 0,
		0x4d, 0x0f, 0x45, 0xda,
		0x41, 0xff, 0xe3,
	}
	binary.LittleEndian.PutUint32(code[5:], 0x1080-(peLeafRVA+9))
	binary.LittleEndian.PutUint32(code[12:], 0x10a0-(peLeafRVA+16))
	build := func(code []byte, selection []uint32) (FunctionPlan, error) {
		p, err := NewPEImage(scalarImage(code))
		if err != nil {
			t.Fatal(err)
		}
		return p.BuildSelectedFunction(peLeafRVA, uint32(len(code)), loaded, clone, 0x300000, 0x400000, 0x500000, 1, selection)
	}
	selected := []uint32{0x1080, 0x10a0}
	plan, err := build(code, selected)
	if err != nil {
		t.Fatal(err)
	}
	body := plan.Image[plan.BodyRVA:plan.Functions[1].End]
	for i, end := range []int{13, 20} {
		address := int64(clone+uint64(plan.BodyRVA)+uint64(end)) + int64(int32(binary.LittleEndian.Uint32(body[end-4:])))
		if address != int64(loaded)+int64(selected[i]) {
			t.Fatal("conditional destination relocation changed the LEA", i, address)
		}
	}
	if len(plan.TailTargets) != 2 || plan.TailTargets[0] != selected[0] || plan.TailTargets[1] != selected[1] || !bytes.Equal(body[20:], code[16:]) {
		t.Fatal("conditional move or indirect transfer changed", body, plan.TailTargets)
	}
	for _, target := range []uint32{0x1080, 0x10a0} {
		if _, err := build(code, []uint32{target}); !errors.Is(err, ErrUnsupported) {
			t.Fatal("one missing destination was accepted", target, err)
		}
	}
	for _, displacement := range []byte{7, 14, 18} {
		bypass := bytes.Clone(code)
		bypass[1] = displacement
		if _, err := build(bypass, selected); !errors.Is(err, ErrUnsupported) {
			t.Fatal("branch could bypass part of the destination assignment", displacement, err)
		}
	}
}

func TestFramedIndirectTailRequiresDestinationAndRestoration(t *testing.T) {
	image, _ := framedPE()
	code := []byte{
		0x53, 0x48, 0x83, 0xec, 0x20, // PUSH RBX; SUB RSP,32
		0x75, 0, // both paths enter the destination assignment
		0x4c, 0x8d, 0x1d, 0, 0, 0, 0, // LEA R11
		0x48, 0x83, 0xc4, 0x20, 0x5b, // ADD RSP,32; POP RBX
		0x41, 0xff, 0xe3, // JMP R11
	}
	binary.LittleEndian.PutUint32(code[10:], 0x1080-(peLeafRVA+14))
	binary.LittleEndian.PutUint32(image[0x404:], peLeafRVA+uint32(len(code)))
	build := func(code []byte, selected uint32) (FunctionPlan, error) {
		copy(image[0x220:], code)
		p, err := NewPEImage(image)
		if err != nil {
			t.Fatal(err)
		}
		return p.BuildSelectedFunction(peLeafRVA, uint32(len(code)), 0x100000, 0x180000, 0x300000, 0x400000, 0x500000, 1, []uint32{selected})
	}
	plan, err := build(code, 0x1080)
	if err != nil || len(plan.TailTargets) != 1 || plan.TailTargets[0] != 0x1080 {
		t.Fatal("verified indirect tail was not linked", plan.TailTargets, err)
	}
	body := plan.Image[plan.BodyRVA:plan.Functions[1].End]
	if !bytes.Equal(body[len(body)-10:len(body)-5], code[14:19]) || body[len(body)-5] != 0xe9 {
		t.Fatal("stack restoration or direct transfer changed", body)
	}
	if _, err := build(code, 0x1081); !errors.Is(err, ErrUnsupported) {
		t.Fatal("non-exact target accepted", err)
	}
	for _, change := range []struct {
		offset int
		value  byte
	}{
		{6, 7},     // branch bypasses LEA but runs the full epilogue
		{6, 11},    // branch bypasses stack release
		{17, 0x28}, // mismatched allocation
		{18, 0x5d}, // incorrect nonvolatile register
	} {
		bad := bytes.Clone(code)
		bad[change.offset] = change.value
		if _, err := build(bad, 0x1080); !errors.Is(err, ErrUnsupported) {
			t.Fatal("unsafe framed jump accepted", change, err)
		}
	}
}
