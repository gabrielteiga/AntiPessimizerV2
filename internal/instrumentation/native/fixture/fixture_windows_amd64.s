#include "textflag.h"

// Ordinary Go assembly getter; callers use its Go ABI wrapper.
TEXT ·Addresses(SB), NOSPLIT, $0-16
	LEAQ nativeEntry<>(SB), AX
	MOVQ AX, entry+0(FP)
	LEAQ nativeBody<>(SB), AX
	MOVQ AX, body+8(FP)
	RET

// Private native Win64 leaf procedures. No Go ABI entry, TLS access, calls,
// stack growth or Go callbacks occur in these routines.
TEXT nativeEntry<>(SB), NOSPLIT|NOFRAME, $0
	MOVQ ·entryProbe(SB), AX
	JMP AX

TEXT nativeBody<>(SB), NOSPLIT|NOFRAME, $0
	MOVQ $4096, R10
bodyloop:
	SUBQ $1, R10
	JNZ bodyloop
	LEAQ (CX)(DX*1), AX
	ADDQ R8, AX
	ADDQ R9, AX
	ADDQ 40(SP), AX
	ADDQ 48(SP), AX
	MOVQ ·exitProbe(SB), R11
	JMP R11
