// Package tool implements the InterCall Go toolchain's naming
// projection, import override selectors, identifier visibility rules,
// deterministic private mangling, scope collision checks, codec
// generation, and Go source-document and directive parsing.
//
// The naming half operates on pure name strings and on parsed and
// validated interface files from internal/syntax. Name conversion is
// ASCII-only and uses the fixed initialism set: wire-to-Go projection
// (WireToGo) is PascalCase for declarations and fields and camelCase for
// parameters, and Go-to-wire conversion (GoToWire) is the checked
// inverse that rejects identifiers that do not survive the exact round
// trip. Import overrides follow the exact --go-name selector grammar.
//
// The codec half renders the generated Go binding codecs of one
// interface model, including the exact @intercall type machine lines of
// SPEC.md "Safe import and re-export metadata", and is tested through
// the compiled generated fixture.
//
// The source half parses handwritten and generated Go source files into
// a document model: generated-file marker recognition, physical
// positions that //line directives never rewrite, the complete
// logical-line InterCall directive grammar with placement, contradiction,
// duplicate, and resolution checks, and the extraction and normalization
// of retained Go documentation, including the '*/' terminator
// rejection.
//
// The discovery half loads the explicit packages of the export operands
// with golang.org/x/tools/go/packages in the active module or workspace:
// canonical-path deduplication, type-checking, importable non-main
// packages, output-package importability with internal visibility and
// import-cycle checks, eligible tagged functions, the exact
// --include/--exclude filter grammar, and the exact provider signatures
// (context.Context first, predeclared error last, named wire
// parameters, and at most one data result).
//
// The artifact half implements SPEC.md "One-file ownership and safe
// replacement" and "Diagnostics": the exact ownership lines and
// lowercase 64-hex-digit SHA-256 artifact stamps, package-name
// resolution, non-following target-leaf checks, output-directory Go
// collision checks under host filename equivalence, in-memory content
// validation, same-filesystem temp staging and rename replacement, the
// interrupted two-target export repair, deterministic bytes, and sorted
// diagnostics.
package tool
