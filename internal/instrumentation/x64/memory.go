package x64

import "golang.org/x/arch/x86/x86asm"

// String instructions change implicit RSI/RDI (nonvolatile in Win64). Accept
// only in a body with verified native unwind, never in the frameless subset.
// MOVSD here is the string operation; scalar SSE has the name MOVSD_XMM.
var stringOperations = map[string]bool{
	"MOVSB": true, "MOVSW": true, "MOVSD": true, "MOVSQ": true,
	"STOSB": true, "STOSW": true, "STOSD": true, "STOSQ": true,
}

// CMPXCHG implicitly changes RAX; XADD changes its explicit operands. Neither
// introduces hidden nonvolatile state. CMPXCHG8B/16B remain unsupported.
var atomicOperations = map[string]bool{"CMPXCHG": true, "XADD": true}

func lockedMemoryOperation(inst x86asm.Inst) bool {
	if _, memory := inst.Args[0].(x86asm.Mem); !memory {
		return false
	}
	for _, prefix := range inst.Prefix {
		// HLE (XACQUIRE/XRELEASE) needs separate evidence. Do not silently treat
		// its REP/REPN prefixes as ordinary LOCK semantics.
		if prefix&0xff == x86asm.PrefixREP || prefix&0xff == x86asm.PrefixREPN {
			return false
		}
	}
	switch inst.Op {
	case x86asm.ADD, x86asm.ADC, x86asm.AND, x86asm.BTC, x86asm.BTR, x86asm.BTS,
		x86asm.CMPXCHG, x86asm.DEC, x86asm.INC, x86asm.NEG, x86asm.NOT,
		x86asm.OR, x86asm.SBB, x86asm.SUB, x86asm.XOR, x86asm.XADD, x86asm.XCHG:
		return true
	}
	return false
}
