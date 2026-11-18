package x64

import (
	"encoding/binary"
	"fmt"
)

// Probe describes a standard Win64/SSE2 native helper bridge, not an installer.
// Helper uses the native Win64 ABI: void Helper(uint32 procedureID).
// It must preserve LastError (as the native producer does), nonvolatile registers
// and XMM6..15. The bridge preserves complete MXCSR control/status across Helper.
// Arithmetic flags and AVX upper halves are volatile in this ABI;
// custom assembly conventions/vectorcall/AVX arguments are not supported.
// Image includes code and aligned UNWIND_INFO. The caller must register a native
// RUNTIME_FUNCTION for it before execution and retire it only when quiescent.
type Probe struct {
	Image                                                  []byte
	CodeSize, UnwindRVA, HelperReturn, Epilogue, StackSize uint32
}

type EntryProbe = Probe

type ExitProbe = Probe

func BuildEntryProbe(base, helper, continuation uint64, procedureID uint32) (EntryProbe, error) {
	return buildProbe(base, helper, continuation, procedureID, false)
}

// BuildExitProbe is entered after the original epilogue has restored the entry
// RSP and nonvolatile registers, with the original return address still at [RSP].
// It preserves integer/SSE2 return values across Helper(id), then executes RET.
// It does not replace return addresses or locate/patch arbitrary return sites.
func BuildExitProbe(base, helper uint64, procedureID uint32) (ExitProbe, error) {
	return buildProbe(base, helper, 0, procedureID, true)
}

func buildProbe(base, helper, continuation uint64, procedureID uint32, exiting bool) (Probe, error) {
	invalid := func(reason string) (Probe, error) {
		return Probe{}, fmt.Errorf("%w: native probe: %s", ErrUnsupported, reason)
	}
	if procedureID == 0 {
		return invalid("zero procedure ID")
	}
	addresses := []uint64{base, helper}
	if !exiting {
		addresses = append(addresses, continuation)
	}
	for _, a := range addresses {
		if a == 0 || a >= maxUserAddress {
			return invalid("address outside supported user space")
		}
	}
	if base > maxUserAddress-512 {
		return invalid("code range overflow")
	}
	if helper >= base && helper < base+512 || continuation >= base && continuation < base+512 || helper == continuation {
		return invalid("overlapping code/helper/continuation")
	}
	const stackSize = 200                                // 32-byte home area + GPR/SIMD saves + alignment
	code := []byte{0x48, 0x81, 0xec, stackSize, 0, 0, 0} // sub rsp,200; entry RSP=8 mod 16
	append32 := func(n uint32) { var b [4]byte; binary.LittleEndian.PutUint32(b[:], n); code = append(code, b[:]...) }
	gpr := []byte{0, 1, 2, 8, 9, 10, 11} // RAX,RCX,RDX,R8,R9,R10,R11
	moveGPR := func(load bool, reg byte, offset uint32) {
		rex := byte(0x48)
		if reg >= 8 {
			rex |= 4
		}
		op := byte(0x89)
		if load {
			op = 0x8b
		}
		code = append(code, rex, op, 0x84|((reg&7)<<3), 0x24)
		append32(offset)
	}
	moveSIMD := func(load bool, reg byte, offset uint32) {
		op := byte(0x7f)
		if load {
			op = 0x6f
		}
		code = append(code, 0xf3, 0x0f, op, 0x84|(reg<<3), 0x24)
		append32(offset)
	}
	for i, reg := range gpr {
		moveGPR(false, reg, 32+uint32(i)*8)
	}
	for i := byte(0); i < 6; i++ {
		moveSIMD(false, i, 96+uint32(i)*16)
	}
	// Four unused bytes between GPR and SIMD saves hold MXCSR. Preserve status
	// too: an inserted helper may change these volatile flags even when the
	// application only observes them with STMXCSR after an ordinary instruction.
	code = append(code, 0x0f, 0xae, 0x9c, 0x24) // stmxcsr [rsp+88]
	append32(88)
	code = append(code, 0xb9)
	append32(procedureID) // mov ecx,id
	code = append(code, 0x48, 0xb8)
	var addr [8]byte
	binary.LittleEndian.PutUint64(addr[:], helper)
	code = append(code, addr[:]...)
	code = append(code, 0xff, 0xd0) // call rax with aligned RSP and independent home slots
	returnPC := uint32(len(code))
	code = append(code, 0x0f, 0xae, 0x94, 0x24) // ldmxcsr [rsp+88]
	append32(88)
	for i := byte(0); i < 6; i++ {
		moveSIMD(true, i, 96+uint32(i)*16)
	}
	for i, reg := range gpr {
		moveGPR(true, reg, 32+uint32(i)*8)
	}
	epilogue := uint32(len(code))
	code = append(code, 0x48, 0x81, 0xc4, stackSize, 0, 0, 0) // standard Windows epilogue
	if exiting {
		code = append(code, 0xc3)
	} else {
		code = append(code, jump(continuation)...)
	}
	codeSize := uint32(len(code))
	for len(code)%4 != 0 {
		code = append(code, 0x90)
	}
	unwind := uint32(len(code))
	// Version 1, prologue length 7, two slots: UWOP_ALLOC_LARGE(200/8).
	code = append(code, 1, 7, 2, 0, 7, 1, stackSize/8, 0)
	return Probe{Image: code, CodeSize: codeSize, UnwindRVA: unwind, HelperReturn: returnPC, Epilogue: epilogue, StackSize: stackSize}, nil
}
