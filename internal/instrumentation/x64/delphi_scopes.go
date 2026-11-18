package x64

import (
	"crypto/sha256"
	"encoding/binary"
	"fmt"
	"sort"

	"golang.org/x/arch/x86/x86asm"
)

// This fingerprint comes from the handler at RVA 0xd7a0 of the pinned original
// v0.3 DLL, not from a guessed compiler version. Only direct-call destinations
// and RIP-relative operand displacements are masked; the complete control flow,
// field accesses, constants and ABI must match. Typed tables additionally require the reviewed type resolver.
const delphiScopeFingerprint = "8ebb487813ebd0e56320833793e013dc48ea3e030dfd64f2157743835e165229"
const delphiScopeHandlerSize = 2282

func (p *PEImage) delphiScopeHandler(rva uint32) bool {
	return p.handlerFingerprint(rva, delphiScopeHandlerSize, delphiScopeFingerprint)
}
func (p *PEImage) handlerFingerprint(rva, size uint32, fingerprint string) bool {
	u32 := binary.LittleEndian.Uint32
	i := sort.Search(len(p.table)/12, func(i int) bool { return u32(p.table[i*12:]) >= rva })
	if i == len(p.table)/12 || u32(p.table[i*12:]) != rva || uint64(u32(p.table[i*12+4:])) != uint64(rva)+uint64(size) {
		return false
	}
	code, ok := p.mapped(rva, size, true)
	if !ok {
		return false
	}
	digest, ok := normalizedHandlerFingerprint(code)
	return ok && digest == fingerprint
}
func normalizedHandlerFingerprint(code []byte) (string, bool) {
	normalized := append([]byte(nil), code...)
	for at := 0; at < len(code); {
		inst, err := x86asm.Decode(code[at:], 64)
		if err != nil || inst.Op == 0 || inst.Len == 0 {
			return "", false
		}
		mask := inst.Op == x86asm.CALL
		for _, arg := range inst.Args {
			if m, ok := arg.(x86asm.Mem); ok && m.Base == x86asm.RIP {
				mask = true
			}
		}
		if mask && inst.PCRel != 0 {
			clear(normalized[at+inst.PCRelOff : at+inst.PCRelOff+inst.PCRel])
		}
		at += inst.Len
	}
	return fmt.Sprintf("%x", sha256.Sum256(normalized)), true
}
