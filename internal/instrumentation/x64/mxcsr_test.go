package x64

import (
	"bytes"
	"errors"
	"testing"

	"golang.org/x/arch/x86/x86asm"
)

func TestNativeProbesSaveMXCSRBeforeHelperAndRestoreAfterward(t *testing.T) {
	entry, err := BuildEntryProbe(0x100000, 0x300000, 0x400000, 1)
	if err != nil {
		t.Fatal(err)
	}
	exit, err := BuildExitProbe(0x100000, 0x300000, 1)
	if err != nil {
		t.Fatal(err)
	}
	for _, probe := range []Probe{entry, exit} {
		saved, restored := false, false
		for at := 0; at < int(probe.CodeSize); {
			inst, err := x86asm.Decode(probe.Image[at:probe.CodeSize], 64)
			if err != nil {
				t.Fatal(err)
			}
			if inst.Op == x86asm.STMXCSR || inst.Op == x86asm.LDMXCSR {
				mem, ok := inst.Args[0].(x86asm.Mem)
				if !ok || mem.Base != x86asm.RSP || mem.Disp != 88 || inst.MemBytes != 4 || probe.StackSize != 200 {
					t.Fatal("MXCSR save overlaps home/GPR/SIMD areas", inst)
				}
				if inst.Op == x86asm.STMXCSR {
					saved = uint32(at) < probe.HelperReturn
				} else {
					restored = saved && uint32(at) >= probe.HelperReturn && uint32(at) < probe.Epilogue
				}
			}
			at += inst.Len
			if inst.Op == x86asm.JMP || inst.Op == x86asm.RET {
				break // entry jump's absolute target is data
			}
		}
		if !saved || !restored {
			t.Fatal("helper is not surrounded by MXCSR preservation")
		}
	}
}

func TestMXCSRBodyUsesNormalMemoryAndHomeSlotChecks(t *testing.T) {
	for _, op := range [][]byte{{0x0f, 0xae, 0x11}, {0x0f, 0xae, 0x1a}, {0x0f, 0xae, 0x54, 0x24, 48}, {0x0f, 0xae, 0x5c, 0x24, 8}} {
		code := append(append(bytes.Repeat([]byte{0x90}, 16), op...), 0xc3)
		if _, err := BuildPEFunction(scalarImage(code), peLeafRVA, uint32(len(code)), 0x100000, 0x180000, 0x300000, 0x400000, 0x500000, 1); err != nil {
			t.Fatal("valid MXCSR body rejected", op, err)
		}
		if _, err := BuildPELeaf(scalarImage(code), peLeafRVA, uint32(len(code)), 0x100000, 0x180000, 0x300000, 0x400000, 1); !errors.Is(err, ErrUnsupported) {
			t.Fatal("legacy MXCSR body gate changed", op, err)
		}
	}
	for _, op := range [][]byte{{0x0f, 0xae, 0x14, 0x24}, {0x0f, 0xae, 0x5c, 0x24, 40}} {
		code := append(append(bytes.Repeat([]byte{0x90}, 16), op...), 0xc3)
		if _, err := BuildPEFunction(scalarImage(code), peLeafRVA, uint32(len(code)), 0x100000, 0x180000, 0x300000, 0x400000, 0x500000, 1); !errors.Is(err, ErrUnsupported) {
			t.Fatal("MXCSR accesses return/caller slots", op, err)
		}
	}
}
