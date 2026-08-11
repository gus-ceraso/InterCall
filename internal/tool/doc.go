// Package tool implements the InterCall Go toolchain's naming projection,
// import override selectors, identifier visibility rules, deterministic
// private mangling, and scope collision checks from SPEC.md "Names and
// native overrides".
//
// The package operates on pure name strings and on parsed and validated
// interface files from internal/syntax. It never loads Go source, maps Go
// types, parses source directives, or emits code; those phases live in
// later tasks.
//
// Name conversion is ASCII-only and uses the fixed initialism set:
// wire-to-Go projection (WireToGo) is PascalCase for declarations and
// fields and camelCase for parameters, and Go-to-wire conversion
// (GoToWire) is the checked inverse that rejects identifiers that do not
// survive the exact round trip.
//
// Import overrides follow the exact --go-name selector grammar. A selector
// names one generated Go identifier: a declaration root, a parameter, or
// the final field of an inline record reached through a field path.
// ProjectNames computes the complete projected name table for one
// interface, applies the overrides, validates every resulting name, and
// rejects collisions in each actual scope (package declarations, record
// fields, and procedure parameters).
package tool
