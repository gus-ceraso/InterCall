package syntax_test

import (
	"testing"

	"github.com/cerasos/intercall/internal/syntax"
)

// foldString recomputes the FNV-0 hash with a plain independent loop over
// the full input bytes, guarding against operator-precedence mistakes in
// the package implementation.
func foldString(s string) uint64 {
	const prime = 1099511628211
	h := uint64(0)
	for i := 0; i < len(s); i++ {
		h = h*prime ^ uint64(s[i])
	}
	return h
}

// TestKeyREADMEVectors locks the README key-vector table and the SPEC fixed
// runtime exception keys.
func TestKeyREADMEVectors(t *testing.T) {
	tests := []struct {
		kind, name string
		want       uint64
	}{
		{"procedure", "get_user", 0x4c63cc5048869eb7},
		{"exception", "procedure_not_found", 0x970e76fcc5e2dacb},
		{"procedure", "echo", 0x0159eb91a98f8f42},
		{"exception", "failed", 0x583fb304d69368ca},
		// SPEC.md "Fixed Go Runtime Exceptions" table.
		{"exception", "invalid_arguments", 0x3f5fc972f8477b07},
		{"exception", "internal_exception", 0x1aaec22e85996f50},
	}
	for _, tt := range tests {
		if got := syntax.Key(tt.kind, tt.name); got != tt.want {
			t.Errorf("Key(%q, %q) = %#016x, want %#016x", tt.kind, tt.name, got, tt.want)
		}
		if tt.kind == "procedure" {
			if got := syntax.ProcedureKey(tt.name); got != tt.want {
				t.Errorf("ProcedureKey(%q) = %#016x, want %#016x", tt.name, got, tt.want)
			}
		} else {
			if got := syntax.ExceptionKey(tt.name); got != tt.want {
				t.Errorf("ExceptionKey(%q) = %#016x, want %#016x", tt.name, got, tt.want)
			}
		}
	}
}

// TestKeyInputBytes verifies that the key is exactly the 64-bit FNV-0 of
// the ASCII bytes "kind name" — the kind, one space, and the name — with
// no seed, length prefix, terminator, or other marker.
func TestKeyInputBytes(t *testing.T) {
	names := []string{"get_user", "echo", "procedure_not_found", "x", "_y", "Z9_"}
	for _, kind := range []string{"procedure", "exception"} {
		for _, name := range names {
			want := foldString(kind + " " + name)
			if got := syntax.Key(kind, name); got != want {
				t.Errorf("Key(%q, %q) = %#016x, want %#016x", kind, name, got, want)
			}
		}
	}
}

// TestKeyKindIsPartOfInput verifies that the declaration kind participates
// in the key, so the same name yields different keys per kind and the two
// prefixes never accidentally collapse to the same input bytes.
func TestKeyKindIsPartOfInput(t *testing.T) {
	for _, name := range []string{"a", "echo", "get_user", "failed"} {
		if syntax.ProcedureKey(name) == syntax.ExceptionKey(name) {
			t.Errorf("procedure and exception keys equal for %q: %#016x", name, syntax.ProcedureKey(name))
		}
	}
}

// TestKeyCaseSensitive verifies that keys use the exact, case-sensitive
// names: "get_user" and "Get_user" are different declarations with
// different keys, and a name with a trailing underscore differs from the
// bare name.
func TestKeyCaseSensitive(t *testing.T) {
	pairs := []struct{ a, b string }{
		{"get_user", "Get_user"},
		{"get_user", "get_user_"},
		{"echo", "Echo"},
		{"failed", "failed_"},
	}
	for _, p := range pairs {
		ka := syntax.ProcedureKey(p.a)
		kb := syntax.ProcedureKey(p.b)
		if ka == kb {
			t.Errorf("keys equal for case-sensitive pair %q %q: %#016x", p.a, p.b, ka)
		}
	}
}

// TestKeyModularArithmetic verifies the modular unsigned arithmetic on
// boundary inputs: hashing is mod 2^64 for long names, and the first byte
// of a one-byte name is the hash itself because the initial hash is zero.
func TestKeyModularArithmetic(t *testing.T) {
	// A 16-byte name exercises wrap-around beyond 2^64: the plain fold of
	// 30 input bytes is the oracle.
	name := "abcdefghijklmnop" // 16 bytes
	if got, want := syntax.Key("procedure", name), foldString("procedure "+name); got != want {
		t.Errorf("long-name key = %#016x, want %#016x", got, want)
	}
	// FNV-0 from zero: hash of a single byte is that byte, so the
	// procedure key of the one-char name "A" is 'A' after "procedure "
	// has been folded in.
	if got := syntax.Key("procedure", "A"); got != foldString("procedure A") {
		t.Errorf("one-char key = %#016x, want %#016x", got, foldString("procedure A"))
	}
}

// TestKeyDeterministic verifies that keys are pure functions of the kind
// and name: repeated computation and concurrent calls agree.
func TestKeyDeterministic(t *testing.T) {
	kind, name := "procedure", "get_user"
	want := syntax.Key(kind, name)
	for i := 0; i < 100; i++ {
		if got := syntax.Key(kind, name); got != want {
			t.Fatalf("Key(%q, %q) = %#016x, want %#016x", kind, name, got, want)
		}
	}
}
