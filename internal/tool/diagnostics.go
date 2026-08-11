package tool

import "sort"

// This file implements the deterministic diagnostic ordering of SPEC.md
// "Diagnostics": when a phase produces several diagnostics, the CLI
// sorts them by logical path, line, column, and message. The order is a
// total order: ties are broken by message, so identical diagnostics are
// adjacent.

// SortDiagnostics sorts a collection of source diagnostics by logical
// path, line, column, and message (SPEC.md "Diagnostics"). The path is
// the logical path of the operand — a canonical import path plus the
// file's package-relative path for Go sources, the exact interface
// operand for interface files, or an artifact target path — never a
// staging path.
func SortDiagnostics(diags []*Error) {
	sort.Slice(diags, func(i, j int) bool {
		a, b := diags[i], diags[j]
		if a.Filename != b.Filename {
			return a.Filename < b.Filename
		}
		if a.Pos.Line != b.Pos.Line {
			return a.Pos.Line < b.Pos.Line
		}
		if a.Pos.Column != b.Pos.Column {
			return a.Pos.Column < b.Pos.Column
		}
		return a.Msg < b.Msg
	})
}

// firstError returns the earliest diagnostic of a deterministic
// collection, ordered by path, line, column, and message, or nil for an
// empty collection.
func firstError(diags []*Error) error {
	if len(diags) == 0 {
		return nil
	}
	SortDiagnostics(diags)
	return diags[0]
}
