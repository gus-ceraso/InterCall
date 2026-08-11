// Package intercall is the shared runtime for InterCall-generated Go
// bindings.
//
// InterCall is an interface language and wire protocol; README.md defines
// both. Generated import and export bindings are thin, statically generated
// packages that use this runtime's opaque binding handles, callback types,
// and fixed error sentinels to communicate over a single bidirectional byte
// stream. The Go mapping, runtime, and generated-code SPI are defined in
// SPEC.md.
//
// This package uses only the standard library.
package intercall
