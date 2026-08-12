package tool

import (
	"fmt"
	"strings"
)

// fnv0Prime is the FNV-0 64-bit prime, the same prime that produces the
// protocol keys in internal/syntax. The mangling hash is an independent
// use of the same primitive; it never equals or feeds a wire key because
// its input is prefixed differently.
const fnv0Prime = 1099511628211

// fnv0 computes the 64-bit FNV-0 hash of b.
func fnv0(b []byte) uint64 {
	h := uint64(0)
	for _, c := range b {
		h = h*fnv0Prime ^ uint64(c)
	}
	return h
}

// manglePrefix introduces every deterministically mangled private name.
const manglePrefix = "_intercall"

// ManglePrivate returns a deterministic, unexported Go identifier derived
// from the given parts, for generated private helpers and import aliases.
//
// The result is manglePrefix, then each part with every byte outside
// [A-Za-z0-9] replaced by "_" (parts joined by "_"), then "_" and the
// 16-digit lowercase hexadecimal FNV-0 hash of the parts joined by NUL
// bytes. The scheme is deterministic, always yields a valid non-keyword
// unexported identifier, never equals the fixed "_intercallSemantic"
// constant, and the hash keeps distinct inputs distinct. Public and
// parameter names are never mangled: collisions there are errors, not
// escapes.
func ManglePrivate(parts ...string) string {
	var body strings.Builder
	for i, p := range parts {
		if i > 0 {
			body.WriteByte('_')
		}
		for j := 0; j < len(p); j++ {
			c := p[j]
			if isASCIILetter(c) || isDigit(c) {
				body.WriteByte(c)
			} else {
				body.WriteByte('_')
			}
		}
	}
	hash := fnv0([]byte(strings.Join(parts, "\x00")))
	return manglePrefix + body.String() + "_" + fmt.Sprintf("%016x", hash)
}
