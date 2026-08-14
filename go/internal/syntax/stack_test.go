package syntax_test

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"runtime/debug"
	"strings"
	"testing"
	"time"

	"github.com/cerasos/intercall/go/internal/syntax"
)

// deepSubprocessEnv selects the subprocess role of
// TestDeepTypeProcessingUsesBoundedStack. The parent role spawns the test
// binary again with this variable set and a limited -test.run filter; the
// subprocess role lowers the maximum Go stack and runs the deep pipeline.
const deepSubprocessEnv = "INTERCALL_RM04_DEEP_SUBPROCESS"

// deepMaxStackBytes is the maximum Go stack the subprocess allows. The
// bounded-stack pipeline needs far less than this, while every recursive
// walk of the 100,000-level list chain below needs several megabytes, so
// any reintroduced recursion — including a recursive (*ListType).Span() —
// crashes the subprocess with a stack overflow and fails the parent test.
const deepMaxStackBytes = 512 << 10

// deepListDepth is the list-chain nesting depth exercised by the
// subprocess. The measured failure band of the old recursive attachment
// walk (per-node (*ListType).Span()) was 20,000–30,000 levels under the
// 512 KiB budget, so 100,000 levels sits several times beyond it: the
// test cannot mask residual call-stack growth. The bounded-stack pipeline
// completes the same input in well under a second.
const deepListDepth = 100000

// deepRecordDepth is the record-chain nesting depth. The canonical
// formatter indents every enclosing record by four spaces, so the
// formatted record body grows quadratically; 2,000 levels keep that body
// at a few tens of megabytes while still exceeding the subprocess stack
// budget for any recursive walk.
const deepRecordDepth = 2000

// TestDeepTypeProcessingUsesBoundedStack proves that every syntax-owned
// walk — parse, validate, documentation attachment, and canonical
// formatting — uses Go call-stack space independent of unrestricted type
// nesting. The test re-executes itself as a subprocess that lowers its
// maximum Go stack to deepMaxStackBytes, constructs deeply nested lists
// and records in memory, and runs the complete pipeline: parse, validate,
// attach, format, and reparse. Any call-stack growth proportional to
// nesting crashes the subprocess, failing this test.
func TestDeepTypeProcessingUsesBoundedStack(t *testing.T) {
	if os.Getenv(deepSubprocessEnv) == "1" {
		runDeepTypeProcessingSubprocess()
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, os.Args[0], "-test.run=^TestDeepTypeProcessingUsesBoundedStack$")
	cmd.Env = append(os.Environ(), deepSubprocessEnv+"=1")
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("deep-processing subprocess failed: %v\n%s", err, out)
	}
}

// runDeepTypeProcessingSubprocess runs the complete syntax pipeline on
// deeply nested inputs constructed in memory while the maximum Go stack
// is lowered: parse, validate, attach documentation, format, and reparse.
// Malformed deep inputs must return the same *syntax.Error class and
// exact source position as their shallow equivalents instead of a panic
// or fatal exit.
func runDeepTypeProcessingSubprocess() {
	debug.SetMaxStack(deepMaxStackBytes)

	// Deep list chain: "type t list list ... list uint8;".
	listSrc := "type t " + strings.Repeat("list ", deepListDepth) + "uint8;"
	f := deepMustParse(listSrc)
	deepMustValidate(f)
	// The outer list's span covers the whole chain; Span() must stay flat
	// at this depth.
	if got, want := f.Decls[0].(*syntax.TypeDecl).Type.Span(), (syntax.Span{7, len(listSrc) - 1}); got != want {
		deepFatalf("deep list type span = %v, want %v", got, want)
	}
	syntax.AttachDocs(f)
	listOut := syntax.Format(f)
	f = deepMustParse(string(listOut))
	deepMustValidate(f)
	if got := syntax.Format(f); string(got) != string(listOut) {
		deepFatalf("deep list body is not canonical across the round trip")
	}
	if n := strings.Count(string(listOut), "list"); n != deepListDepth {
		deepFatalf("deep list body has %d list occurrences, want %d", n, deepListDepth)
	}
	if !strings.HasSuffix(string(listOut), "uint8;\n") {
		deepFatalf("deep list body does not end with the element and semicolon")
	}

	// Deep record chain: "type t record { f record { f ... uint8; }; };".
	recSrc := "type t " + strings.Repeat("record { f ", deepRecordDepth) + "uint8;" + strings.Repeat("};", deepRecordDepth)
	f = deepMustParse(recSrc)
	deepMustValidate(f)
	if got, want := f.Decls[0].(*syntax.TypeDecl).Type.Span(), (syntax.Span{7, len(recSrc) - 1}); got != want {
		deepFatalf("deep record type span = %v, want %v", got, want)
	}
	syntax.AttachDocs(f)
	recOut := syntax.Format(f)
	f = deepMustParse(string(recOut))
	deepMustValidate(f)
	if got := syntax.Format(f); string(got) != string(recOut) {
		deepFatalf("deep record body is not canonical across the round trip")
	}
	if n := strings.Count(string(recOut), "record"); n != deepRecordDepth {
		deepFatalf("deep record body has %d record occurrences, want %d", n, deepRecordDepth)
	}

	// Malformed deep input returns the same error class and source
	// position as the shallow equivalent, never a panic or fatal exit.
	// A deep list with no element ends at EOF inside the element type.
	badList := "type t " + strings.Repeat("list ", deepListDepth)
	e := deepMustParseError(badList)
	if want := deepShallowError("type t list"); e.Msg != want {
		deepFatalf("malformed deep list message %q, want %q", e.Msg, want)
	}
	if e.Pos.Offset != len(badList) {
		deepFatalf("malformed deep list error offset %d, want %d", e.Pos.Offset, len(badList))
	}

	// A deep record chain that never closes ends at EOF inside the
	// innermost record's field loop.
	badRec := "type t " + strings.Repeat("record { f ", deepRecordDepth) + "record {"
	e = deepMustParseError(badRec)
	if want := deepShallowError("type t record {"); e.Msg != want {
		deepFatalf("unterminated deep record message %q, want %q", e.Msg, want)
	}
	if e.Pos.Offset != len(badRec) {
		deepFatalf("unterminated deep record error offset %d, want %d", e.Pos.Offset, len(badRec))
	}

	// A deep record chain ending after a field name ends at EOF inside
	// the pending field type.
	badField := "type t record { f " + strings.Repeat("record { f ", deepRecordDepth)
	e = deepMustParseError(badField)
	if want := deepShallowError("type t record { f"); e.Msg != want {
		deepFatalf("deep field type message %q, want %q", e.Msg, want)
	}
	if e.Pos.Offset != len(badField) {
		deepFatalf("deep field type error offset %d, want %d", e.Pos.Offset, len(badField))
	}

	// A broken reference at depth reports the exact offending identifier
	// at its exact position, like the shallow form.
	ref := "type a uint8; type t " + strings.Repeat("list ", deepListDepth) + "missing;"
	f = deepMustParse(ref)
	err := syntax.Validate(f)
	e, ok := err.(*syntax.Error)
	if !ok {
		deepFatalf("deep unresolved reference error type %T, want *syntax.Error", err)
	}
	if want := `unresolved type reference "missing" in type t`; e.Msg != want {
		deepFatalf("deep unresolved reference message %q, want %q", e.Msg, want)
	}
	if want := strings.Index(ref, "missing"); e.Pos.Offset != want {
		deepFatalf("deep unresolved reference offset %d, want %d", e.Pos.Offset, want)
	}
}

// deepMustParse parses src in the subprocess, failing it on error.
func deepMustParse(src string) *syntax.File {
	f, err := syntax.Parse("deep", []byte(src))
	if err != nil {
		deepFatalf("Parse failed: %v", err)
	}
	return f
}

// deepMustValidate validates f in the subprocess, failing it on error.
func deepMustValidate(f *syntax.File) {
	if err := syntax.Validate(f); err != nil {
		deepFatalf("Validate failed: %v", err)
	}
}

// deepMustParseError parses src and returns the *syntax.Error, failing the
// subprocess when parsing succeeds or the error is not a *syntax.Error.
func deepMustParseError(src string) *syntax.Error {
	f, err := syntax.Parse("deep", []byte(src))
	if err == nil {
		deepFatalf("malformed input parsed with %d declarations", len(f.Decls))
	}
	e, ok := err.(*syntax.Error)
	if !ok {
		deepFatalf("error type %T, want *syntax.Error", err)
	}
	return e
}

// deepShallowError parses the shallow equivalent and returns its message.
func deepShallowError(src string) string {
	return deepMustParseError(src).Msg
}

// deepFatalf reports a subprocess failure on stderr and exits nonzero,
// which the parent test reports as a failure.
func deepFatalf(format string, args ...any) {
	fmt.Fprintf(os.Stderr, "deep subprocess: "+format+"\n", args...)
	os.Exit(1)
}
