package x64

import (
	"encoding/binary"
	"errors"
	"testing"

	"golang.org/x/arch/x86/x86asm"
)

func TestEntryProbeEncodingAndUnwind(t *testing.T) {
	p, err := BuildEntryProbe(0x10000000, 0x20000000, 0x30000000, 123)
	if err != nil {
		t.Fatal(err)
	}
	if p.StackSize != 200 || p.UnwindRVA%4 != 0 || int(p.UnwindRVA)+8 != len(p.Image) {
		t.Fatal(p)
	}
	if target := binary.LittleEndian.Uint64(p.Image[p.CodeSize-8:]); target != 0x30000000 {
		t.Fatal(target)
	}
	var call, sub, add int
	for at := 0; at < int(p.CodeSize)-8; {
		inst, err := x86asm.Decode(p.Image[at:p.CodeSize], 64)
		if err != nil {
			t.Fatal(at, err)
		}
		switch inst.Op {
		case x86asm.CALL:
			call++
			if uint32(at+inst.Len) != p.HelperReturn {
				t.Fatal("wrong native helper return PC")
			}
		case x86asm.SUB:
			sub++
		case x86asm.ADD:
			add++
			if uint32(at) != p.Epilogue {
				t.Fatal("wrong epilogue PC")
			}
		case x86asm.MOV, x86asm.MOVDQU, x86asm.JMP, x86asm.STMXCSR, x86asm.LDMXCSR:
		default:
			t.Fatal("unexpected native probe operation", inst)
		}
		at += inst.Len
	}
	if call != 1 || sub != 1 || add != 1 {
		t.Fatal(call, sub, add)
	}
	u := p.Image[p.UnwindRVA:]
	if u[0] != 1 || u[1] != 7 || u[2] != 2 || u[3] != 0 || u[4] != 7 || u[5] != 1 || binary.LittleEndian.Uint16(u[6:])*8 != uint16(p.StackSize) {
		t.Fatal("invalid ALLOC_LARGE unwind", u)
	}
}
func TestEntryProbeRejectsInvalidAddresses(t *testing.T) {
	for _, args := range [][4]uint64{{0, 0x20000000, 0x30000000, 1}, {0x10000000, 0, 0x30000000, 1}, {0x10000000, 0x20000000, 0, 1}, {0x10000000, 0x20000000, 0x30000000, 0}, {0x10000000, 0x10000008, 0x30000000, 1}, {0x10000000, 0x20000000, 0x10000008, 1}, {maxUserAddress - 100, 0x20000000, 0x30000000, 1}, {0x10000000, maxUserAddress, 0x30000000, 1}} {
		if _, err := BuildEntryProbe(args[0], args[1], args[2], uint32(args[3])); !errors.Is(err, ErrUnsupported) {
			t.Fatal("unsafe probe accepted", args, err)
		}
	}
}

func TestExitProbeEncodingAndUnwind(t *testing.T) {
	p, err := BuildExitProbe(0x10000000, 0x20000000, 123)
	if err != nil {
		t.Fatal(err)
	}
	if p.StackSize != 200 || p.UnwindRVA%4 != 0 || p.Image[p.CodeSize-1] != 0xc3 || int(p.UnwindRVA)+8 != len(p.Image) {
		t.Fatal(p)
	}
	var calls, returns int
	for at := 0; at < int(p.CodeSize); {
		inst, err := x86asm.Decode(p.Image[at:p.CodeSize], 64)
		if err != nil {
			t.Fatal(at, err)
		}
		switch inst.Op {
		case x86asm.CALL:
			calls++
		case x86asm.RET:
			returns++
			if at+inst.Len != int(p.CodeSize) {
				t.Fatal("exit RET is not the final instruction")
			}
		case x86asm.SUB, x86asm.ADD, x86asm.MOV, x86asm.MOVDQU, x86asm.STMXCSR, x86asm.LDMXCSR:
		default:
			t.Fatal("unexpected native exit operation", inst)
		}
		at += inst.Len
	}
	if calls != 1 || returns != 1 {
		t.Fatal(calls, returns)
	}
	u := p.Image[p.UnwindRVA:]
	if u[0] != 1 || u[1] != 7 || u[2] != 2 || u[4] != 7 || u[5] != 1 || binary.LittleEndian.Uint16(u[6:])*8 != uint16(p.StackSize) {
		t.Fatal("invalid exit unwind", u)
	}
	for _, args := range [][3]uint64{{0, 0x20000000, 1}, {0x10000000, 0, 1}, {0x10000000, 0x10000010, 1}, {maxUserAddress - 100, 0x20000000, 1}, {0x10000000, 0x20000000, 0}} {
		if _, err := BuildExitProbe(args[0], args[1], uint32(args[2])); !errors.Is(err, ErrUnsupported) {
			t.Fatal("invalid exit probe accepted", args, err)
		}
	}
}
