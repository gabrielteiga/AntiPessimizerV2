package symbols

import (
	"crypto/sha256"
	"fmt"
	"strconv"
	"strings"
	"unicode/utf8"
)

// DisplayName recognizes the length-prefixed _ZN names handled by the original
// JclPeImage CPU64 implementation. Unknown/malformed encodings remain verbatim.
// Parameter encodings are not decoded; RVA remains the procedure's identity.
func DisplayName(raw string) string {
	name := raw
	if strings.HasPrefix(raw, "_ZN") {
		rest := raw[3:]
		var parts []string
		for len(rest) > 0 && rest[0] >= '0' && rest[0] <= '9' {
			digits := 0
			for digits < len(rest) && rest[digits] >= '0' && rest[digits] <= '9' {
				digits++
			}
			n, err := strconv.ParseUint(rest[:digits], 10, 32)
			if err != nil || n == 0 || n > uint64(len(rest)-digits) {
				parts = nil
				break
			}
			part := rest[digits : digits+int(n)]
			if !utf8.ValidString(part) {
				parts = nil
				break
			}
			parts = append(parts, part)
			rest = rest[digits+int(n):]
		}
		if len(parts) > 0 && (rest == "" || strings.HasPrefix(rest, "E")) {
			name = strings.Join(parts, ".")
		}
	}
	// Snapshot v1 limits display strings to 256 bytes. Keep full raw names in
	// td32dump; a stable suffix distinguishes shortened names without splitting UTF-8.
	if len(name) > 256 {
		hash := sha256.Sum256([]byte(raw))
		suffix := fmt.Sprintf("…#%x", hash[:6])
		end := 256 - len(suffix)
		for end > 0 && !utf8.RuneStart(name[end]) {
			end--
		}
		name = name[:end] + suffix
	}
	return name
}
