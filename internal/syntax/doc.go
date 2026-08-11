// Package syntax parses InterCall interface files into a source-only AST.
//
// Parse accepts exactly the grammar, lexical rules, and UTF-8 rules defined
// in README.md, including an empty file. The AST contains only source
// structure: declarations and nested type occurrences in source order,
// exact input byte spans, and one documentation slot on every declaration,
// parameter, field, and type-specifier occurrence. Comment attachment and
// normalization are a later phase; this package captures every comment in
// source order with its exact span and raw body.
//
// Validate checks protocol semantics on a parsed file: shared global
// declaration names, per-procedure and per-record local scopes, earlier
// type references, key zero, and key collisions across procedure and
// exception kinds. Key computes the 64-bit FNV-0 procedure and exception
// keys, including the README key vectors.
//
// Positions are exact input byte offsets. Position maps an offset to its
// one-based physical line and byte column for diagnostics; EOF is offset
// len(src) under the same rules, and invalid UTF-8 points at its first
// invalid byte.
//
// This package uses only the standard library.
package syntax
