package x64

import "encoding/binary"

const NearJumpSize = 5

// A nearby allocation needs only E9 rel32 at the original entry. Preserve the
// absolute jump fallback when distance exceeds the signed displacement range.
func entryJump(source, target uint64) []byte {
	delta := int64(target) - int64(source+NearJumpSize)
	if delta < -1<<31 || delta > 1<<31-1 {
		return jump(target)
	}
	out := make([]byte, NearJumpSize)
	out[0] = 0xe9
	binary.LittleEndian.PutUint32(out[1:], uint32(int32(delta)))
	return out
}
