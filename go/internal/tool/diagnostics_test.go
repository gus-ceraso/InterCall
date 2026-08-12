package tool

import (
	"errors"
	"io/fs"
	"os"
	"strings"
	"testing"
)

// This file tests the diagnostic ordering of SPEC.md "Diagnostics":
// when a phase produces several diagnostics, they sort by logical path,
// line, column, and message, and errors without a source span use line
// 1, column 1 of the relevant operand.

func TestDiagnosticsSort(t *testing.T) {
	diags := []*Error{
		{Filename: "b.go", Pos: Position{Line: 2, Column: 1}, Msg: "z"},
		{Filename: "a.go", Pos: Position{Line: 3, Column: 9}, Msg: "a"},
		{Filename: "a.go", Pos: Position{Line: 1, Column: 5}, Msg: "m"},
		{Filename: "a.go", Pos: Position{Line: 1, Column: 2}, Msg: "m"},
		{Filename: "a.go", Pos: Position{Line: 1, Column: 2}, Msg: "b"},
		{Filename: "a.go", Pos: Position{Line: 2, Column: 1}, Msg: "a"},
		{Filename: "", Pos: Position{Line: 9, Column: 9}, Msg: "no path"},
		{Filename: "a.go", Pos: Position{Line: 1, Column: 1}, Msg: "first"},
	}
	SortDiagnostics(diags)
	var got []string
	for _, d := range diags {
		got = append(got, d.Error())
	}
	want := []string{
		"9:9: no path",
		"a.go:1:1: first",
		"a.go:1:2: b",
		"a.go:1:2: m",
		"a.go:1:5: m",
		"a.go:2:1: a",
		"a.go:3:9: a",
		"b.go:2:1: z",
	}
	if len(got) != len(want) {
		t.Fatalf("sorted = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("sorted[%d] = %q, want %q", i, got[i], want[i])
		}
	}
}

func TestFirstErrorOrdering(t *testing.T) {
	if err := firstError(nil); err != nil {
		t.Errorf("firstError(nil) = %v, want nil", err)
	}
	err := firstError([]*Error{
		{Filename: "z.go", Pos: Position{Line: 1, Column: 1}, Msg: "z"},
		{Filename: "a.go", Pos: Position{Line: 2, Column: 1}, Msg: "b"},
		{Filename: "a.go", Pos: Position{Line: 1, Column: 1}, Msg: "a"},
	})
	if err == nil || err.Error() != "a.go:1:1: a" {
		t.Errorf("firstError = %v, want a.go:1:1: a", err)
	}
}

func TestOperandDiagnostics(t *testing.T) {
	// The underlying cause is reported without the operand paths: a
	// staging failure never names the staging path, and no diagnostic
	// embeds a resolved physical directory (SPEC.md "Diagnostics":
	// "It never reports a staging path").
	pe := &fs.PathError{Op: "open", Path: "/tmp/secret/out/.binding_gen.go.tmp-12345", Err: fs.ErrPermission}
	if got := pathErrorCause(pe); got != "permission denied" {
		t.Errorf("pathErrorCause(PathError) = %q, want %q", got, "permission denied")
	}
	le := &os.LinkError{Op: "rename", Old: "/tmp/secret/.api.tmp-99", New: "/tmp/secret/api.intercall", Err: fs.ErrPermission}
	if got := pathErrorCause(le); got != "permission denied" {
		t.Errorf("pathErrorCause(LinkError) = %q, want %q", got, "permission denied")
	}
	if got := pathErrorCause(errors.New("plain failure")); got != "plain failure" {
		t.Errorf("pathErrorCause(plain) = %q", got)
	}
	// The staging diagnostic is a 1:1 operand diagnostic naming the
	// logical target; the random staging suffix and the physical
	// directory never appear, so the message is deterministic.
	e := operandError("out/binding_gen.go", "staging", pe)
	if got, want := e.Error(), "out/binding_gen.go:1:1: staging out/binding_gen.go: permission denied"; got != want {
		t.Errorf("operandError = %q, want %q", got, want)
	}
	r := renameError("out/api.intercall", le)
	if got, want := r.Error(), "out/api.intercall:1:1: replacing out/api.intercall: permission denied; the existing file was not deleted"; got != want {
		t.Errorf("renameError = %q, want %q", got, want)
	}
}

func TestErrorRendering(t *testing.T) {
	e := &Error{Filename: "pkg/file.go", Pos: Position{Line: 1, Column: 1}, Msg: "boom"}
	if got, want := e.Error(), "pkg/file.go:1:1: boom"; got != want {
		t.Errorf("Error() = %q, want %q", got, want)
	}
	// Errors without a source span use line 1, column 1 of the operand
	// and keep the operand path.
	operand := &Error{Filename: "out/binding_gen.go", Pos: Position{Line: 1, Column: 1}, Msg: "not replaceable"}
	if !strings.HasPrefix(operand.Error(), "out/binding_gen.go:1:1: ") {
		t.Errorf("operand error = %q", operand.Error())
	}
}
