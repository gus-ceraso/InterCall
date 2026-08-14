package syntax

// fnv0Prime is the FNV-0 64-bit prime 1099511628211 = 2^40 + 0x1B3.
const fnv0Prime = 1099511628211

// Key returns the 64-bit FNV-0 key of the ASCII bytes of kind, one space,
// and name, following README.md "Procedure and Exception Keys":
//
//	hash = 0
//	prime = 1099511628211
//	for each input byte:
//	    hash = hash * prime modulo 2^64
//	    hash = hash XOR byte
//
// kind is the exact declaration kind ("procedure" or "exception") and name
// is the exact, case-sensitive declaration name. The space after the kind
// is part of the input; there is no length prefix, terminator, initial
// offset, seed, or other marker. Identifiers are ASCII, so ASCII and UTF-8
// encoding produce the same bytes.
func Key(kind, name string) uint64 {
	h := uint64(0)
	for i := 0; i < len(kind); i++ {
		h = h*fnv0Prime ^ uint64(kind[i])
	}
	h = h*fnv0Prime ^ ' '
	for i := 0; i < len(name); i++ {
		h = h*fnv0Prime ^ uint64(name[i])
	}
	return h
}

// ProcedureKey returns the procedure key of name: the 64-bit FNV-0 hash of
// the ASCII bytes "procedure name".
func ProcedureKey(name string) uint64 { return Key("procedure", name) }

// ExceptionKey returns the exception key of name: the 64-bit FNV-0 hash of
// the ASCII bytes "exception name".
func ExceptionKey(name string) uint64 { return Key("exception", name) }
