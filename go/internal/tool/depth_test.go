package tool

import (
	"fmt"
	"strings"
	"testing"

	"github.com/cerasos/intercall/go/internal/syntax"
)

// This file tests the strict Go projection depth preflight of SPEC.md
// "Strict Go projection depth": the exact 4,096-occurrence ceiling,
// the stable first-over-limit physical diagnostics of the import
// interface and of the export physical Go graph, the preflight
// coverage of every recursive tool path, and the unchanged behavior of
// recursive graphs (which keep their existing recursive-type
// diagnostic) and of shapes the mapping rejects before descending
// (which keep their ordinary errors).

// depthErrorOf fails unless err is the depth preflight diagnostic and
// returns it.
func depthErrorOf(t *testing.T, err error) *Error {
	t.Helper()
	if err == nil {
		t.Fatal("preflight succeeded, want a depth diagnostic")
	}
	e, ok := err.(*Error)
	if !ok {
		t.Fatalf("error type %T, want *Error: %v", err, err)
	}
	if !strings.Contains(e.Msg, "exceeds the strict Go projection depth limit of 4096 occurrences") {
		t.Fatalf("error %q does not carry the stable depth diagnostic", e.Msg)
	}
	return e
}

// wantSyntaxDepthPosition fails unless the depth diagnostic points at
// the byte offset of the nth occurrence of substr in src (one-based).
func wantSyntaxDepthPosition(t *testing.T, e *Error, src, substr string, nth int) {
	t.Helper()
	off := 0
	for i := 0; i < nth; i++ {
		j := strings.Index(src[off:], substr)
		if j < 0 {
			t.Fatalf("substring %q not found", substr)
		}
		off += j + 1
	}
	off--
	line := 1 + strings.Count(src[:off], "\n")
	col := off - strings.LastIndex(src[:off], "\n")
	if e.Pos.Offset != off || e.Pos.Line != line || e.Pos.Column != col {
		t.Errorf("depth diagnostic position = %+v, want offset %d line %d column %d", e.Pos, off, line, col)
	}
}

// mustParseValidate parses, attaches, and validates one interface
// source, failing the test on any error.
func mustParseValidate(t *testing.T, src string) *syntax.File {
	t.Helper()
	f, err := syntax.Parse("depth.intercall", []byte(src))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	syntax.AttachDocs(f)
	if err := syntax.Validate(f); err != nil {
		t.Fatalf("validate: %v", err)
	}
	return f
}

// importListSource renders "type t list^k uint8;", whose deepest
// occurrence (the primitive) sits at depth k+1.
func importListSource(k int) string {
	return "type t " + strings.Repeat("list ", k) + "uint8;\n"
}

// importRecordSource renders "type t record { f ... uint8; };", whose
// deepest occurrence sits at depth k+1.
func importRecordSource(k int) string {
	return "type t " + strings.Repeat("record { f ", k) + "uint8;" + strings.Repeat("};", k) + "\n"
}

// importNamedChainSource renders k chained type declarations whose
// deepest occurrence sits at depth k. InterCall resolves type
// references to earlier declarations only, so the declarations appear
// in reverse chain order: the final uint8 first and the chain head
// last.
func importNamedChainSource(k int) string {
	var b strings.Builder
	fmt.Fprintf(&b, "type t%d uint8;\n", k-1)
	for i := k - 2; i >= 0; i-- {
		fmt.Fprintf(&b, "type t%d t%d;\n", i, i+1)
	}
	return b.String()
}

// importHybridSource renders k chained declarations whose deepest
// occurrence sits at depth 2k: every type but the head is a list of
// the previous, and the head is a one-field record. The shape mixes
// lists and a record, and every list element and record field is a
// named reference, so the full generation at the boundary stays linear
// in the declaration count. Declarations appear in reverse chain order
// because InterCall resolves references to earlier declarations only.
func importHybridSource(k int) string {
	var b strings.Builder
	fmt.Fprintf(&b, "type t0 record { f uint8; };\n")
	for i := 1; i < k; i++ {
		fmt.Fprintf(&b, "type t%d list t%d;\n", i, i-1)
	}
	return b.String()
}

// exportChainPackage renders one synthetic provider package whose
// parameter type is the head of a chain of k defined types
// T0..T{k-1}: every type but the last is a slice of the next, and the
// last is uint8. Each declaration contributes a
// defined-type-to-underlying edge and a slice-element edge, so the
// deepest occurrence sits at depth 2k (the parameter root plus 2k-1
// edges). Slice underlyings keep go/types checking linear; a plain
// defined-type chain resolves each underlying eagerly in go/types and
// is cubic in the declaration count.
func exportChainPackage(k int) string {
	var b strings.Builder
	b.WriteString("package synth\n\nimport \"context\"\n\n")
	for i := 0; i < k-1; i++ {
		fmt.Fprintf(&b, "// @intercall type t%d\ntype T%d []T%d\n\n", i, i, i+1)
	}
	fmt.Fprintf(&b, "// @intercall type t%d\ntype T%d uint8\n\n", k-1, k-1)
	b.WriteString("// @intercall procedure p\nfunc P(ctx context.Context, x T0) error { return nil }\n")
	return b.String()
}

// exportAliasPackage renders one synthetic provider package whose
// parameter type is the head of a chain of k aliases A0..A{k-1}; the
// deepest occurrence (the primitive) sits at depth k+1.
func exportAliasPackage(k int) string {
	var b strings.Builder
	b.WriteString("package synth\n\nimport \"context\"\n\n")
	for i := 0; i < k-1; i++ {
		fmt.Fprintf(&b, "type A%d = A%d\n", i, i+1)
	}
	fmt.Fprintf(&b, "type A%d = uint8\n", k-1)
	b.WriteString("// @intercall procedure p\nfunc P(ctx context.Context, x A0) error { return nil }\n")
	return b.String()
}

// exportSlicePackage renders one synthetic provider package whose
// parameter is a chain of k nested slices; the deepest occurrence (the
// primitive) sits at depth k+1.
func exportSlicePackage(k int) string {
	return "package synth\n\nimport \"context\"\n\n" +
		"// @intercall procedure p\nfunc P(ctx context.Context, x " + strings.Repeat("[]", k) + "uint8) error { return nil }\n"
}

// exportHybridPackage renders one synthetic provider package whose
// parameter is the head of a chain of k declarations T0..T{k-1}: even
// declarations are one-field structs whose field is the next type and
// odd declarations are slices of the next type, with the last
// declaration uint8. The deepest occurrence sits at depth 2k (the
// parameter root plus a struct-field and a named-reference edge per
// struct, and a slice-element and a named-reference edge per slice).
// The alternating shape keeps go/types checking linear: every struct
// field resolves to a slice whose element is not eagerly resolved.
func exportHybridPackage(k int) string {
	var b strings.Builder
	b.WriteString("package synth\n\nimport \"context\"\n\n")
	for i := 0; i < k-1; i++ {
		if i%2 == 0 {
			fmt.Fprintf(&b, "// @intercall type t%d\ntype T%d struct { F T%d }\n\n", i, i, i+1)
		} else {
			fmt.Fprintf(&b, "// @intercall type t%d\ntype T%d []T%d\n\n", i, i, i+1)
		}
	}
	fmt.Fprintf(&b, "// @intercall type t%d\ntype T%d uint8\n\n", k-1, k-1)
	b.WriteString("// @intercall procedure p\nfunc P(ctx context.Context, x T0) error { return nil }\n")
	return b.String()
}

// TestGoProjectionDepthLimit proves the exact 4,096-occurrence depth
// counting of the strict Go projection at the boundary: shapes whose
// deepest occurrence sits at depth 4,096 pass the preflight, and the
// same shapes one occurrence deeper are rejected with the stable
// physical diagnostic at the exact position of the first over-limit
// occurrence. Import counts type-declaration underlyings, exception
// payloads, parameters, and returns as roots; export additionally
// counts the parameter or return occurrence itself as the root.
func TestGoProjectionDepthLimit(t *testing.T) {
	t.Run("ImportListChain", func(t *testing.T) {
		// k lists put the primitive at depth k+1: 4,095 lists sit at
		// exactly 4,096, and 4,096 lists exceed it by one.
		ok := mustParseValidate(t, importListSource(maxProjectionDepth-1))
		if err := checkSyntaxProjectionDepth(ok); err != nil {
			t.Fatalf("depth-4096 list chain rejected: %v", err)
		}
		if _, err := MapImport(ok, nil); err != nil {
			t.Fatalf("MapImport at the boundary: %v", err)
		}
		bad := mustParseValidate(t, importListSource(maxProjectionDepth))
		e := depthErrorOf(t, checkSyntaxProjectionDepth(bad))
		if !strings.Contains(e.Msg, `type t exceeds`) {
			t.Errorf("message %q does not name the declaration", e.Msg)
		}
		wantSyntaxDepthPosition(t, e, importListSource(maxProjectionDepth), "uint8", 1)
		if _, err := MapImport(bad, nil); err != nil {
			depthErrorOf(t, err)
		} else {
			t.Fatal("MapImport accepted the depth-4097 list chain")
		}
	})

	t.Run("ImportRecordChain", func(t *testing.T) {
		ok := mustParseValidate(t, importRecordSource(maxProjectionDepth-1))
		if err := checkSyntaxProjectionDepth(ok); err != nil {
			t.Fatalf("depth-4096 record chain rejected: %v", err)
		}
		if _, err := MapImport(ok, nil); err != nil {
			t.Fatalf("MapImport at the boundary: %v", err)
		}
		bad := mustParseValidate(t, importRecordSource(maxProjectionDepth))
		e := depthErrorOf(t, checkSyntaxProjectionDepth(bad))
		wantSyntaxDepthPosition(t, e, importRecordSource(maxProjectionDepth), "uint8", 1)
		if _, err := MapImport(bad, nil); err != nil {
			depthErrorOf(t, err)
		} else {
			t.Fatal("MapImport accepted the depth-4097 record chain")
		}
	})

	t.Run("ImportNamedChain", func(t *testing.T) {
		// k chained declarations put the final primitive at depth k.
		ok := mustParseValidate(t, importNamedChainSource(maxProjectionDepth))
		if err := checkSyntaxProjectionDepth(ok); err != nil {
			t.Fatalf("depth-4096 named chain rejected: %v", err)
		}
		if _, err := MapImport(ok, nil); err != nil {
			t.Fatalf("MapImport at the boundary: %v", err)
		}
		badSrc := importNamedChainSource(maxProjectionDepth + 1)
		bad := mustParseValidate(t, badSrc)
		e := depthErrorOf(t, checkSyntaxProjectionDepth(bad))
		if !strings.Contains(e.Msg, `type t4096 exceeds`) {
			t.Errorf("message %q does not name the deepest declaration", e.Msg)
		}
		// The over-limit occurrence is the final primitive of the chain
		// head's walk, whose declaration opens the reverse-ordered file.
		wantSyntaxDepthPosition(t, e, badSrc, "uint8", 1)
	})

	t.Run("ImportMixedChain", func(t *testing.T) {
		// A list inside a record inside a named reference: the edges add
		// across shapes. The primitive of a list^N chain inside c sits
		// at depth 5+N; the declarations appear in reverse reference
		// order.
		okSrc := "type c " + strings.Repeat("list ", 4091) + "uint8; type b record { f c; }; type a list b;\n"
		ok := mustParseValidate(t, okSrc)
		if err := checkSyntaxProjectionDepth(ok); err != nil {
			t.Fatalf("mixed chain at depth 4096 rejected: %v", err)
		}
		badSrc := "type c " + strings.Repeat("list ", 4092) + "uint8; type b record { f c; }; type a list b;\n"
		bad := mustParseValidate(t, badSrc)
		e := depthErrorOf(t, checkSyntaxProjectionDepth(bad))
		if !strings.Contains(e.Msg, `type c exceeds`) {
			t.Errorf("message %q does not name the deepest declaration", e.Msg)
		}
		wantSyntaxDepthPosition(t, e, badSrc, "uint8", 1)
	})

	t.Run("ExportDefinedChain", func(t *testing.T) {
		// Each slice declaration contributes two edges, so 2,048
		// declarations put the deepest occurrence at exactly 4,096 and
		// 2,049 exceed the ceiling.
		if _, err := mapOne(t, "example.com/synth", exportChainPackage(maxProjectionDepth/2)); err != nil {
			t.Fatalf("depth-4096 defined chain rejected: %v", err)
		}
		_, err := mapOne(t, "example.com/synth", exportChainPackage(maxProjectionDepth/2+1))
		e := depthErrorOf(t, err)
		if !strings.Contains(e.Msg, `type "T2047" exceeds`) {
			t.Errorf("message %q does not name the deepest type", e.Msg)
		}
	})

	t.Run("ExportAliasChain", func(t *testing.T) {
		if _, err := mapOne(t, "example.com/synth", exportAliasPackage(maxProjectionDepth-1)); err != nil {
			t.Fatalf("depth-4096 alias chain rejected: %v", err)
		}
		_, err := mapOne(t, "example.com/synth", exportAliasPackage(maxProjectionDepth))
		e := depthErrorOf(t, err)
		if !strings.Contains(e.Msg, "procedure") {
			t.Errorf("message %q does not name the parameter context", e.Msg)
		}
	})

	t.Run("ExportSliceChain", func(t *testing.T) {
		if _, err := mapOne(t, "example.com/synth", exportSlicePackage(maxProjectionDepth-1)); err != nil {
			t.Fatalf("depth-4096 slice chain rejected: %v", err)
		}
		_, err := mapOne(t, "example.com/synth", exportSlicePackage(maxProjectionDepth))
		depthErrorOf(t, err)
	})

	t.Run("ExportStructHybrid", func(t *testing.T) {
		// The alternating struct/slice chain contributes two edges per
		// declaration, so 2,048 declarations put the deepest occurrence
		// at exactly 4,096 and 2,049 exceed it.
		if _, err := mapOne(t, "example.com/synth", exportHybridPackage(maxProjectionDepth/2)); err != nil {
			t.Fatalf("depth-4096 struct chain rejected: %v", err)
		}
		_, err := mapOne(t, "example.com/synth", exportHybridPackage(maxProjectionDepth/2+1))
		depthErrorOf(t, err)
	})

	t.Run("RecursiveGraphKeepsExistingDiagnostic", func(t *testing.T) {
		// A deep ring of named types aborts the preflight without a
		// depth diagnostic: the existing recursive-type diagnostic owns
		// the cycle. Slice underlyings keep go/types checking linear.
		const ring = 5000
		var b strings.Builder
		b.WriteString("package synth\n\nimport \"context\"\n\n")
		for i := 0; i < ring; i++ {
			fmt.Fprintf(&b, "// @intercall type t%d\ntype T%d []T%d\n\n", i, i, (i+1)%ring)
		}
		b.WriteString("// @intercall procedure p\nfunc P(ctx context.Context, x T0) error { return nil }\n")
		_, err := mapOne(t, "example.com/synth", b.String())
		wantErr(t, err, "recursive type graph")
	})

	t.Run("NonRecursedShapesKeepOrdinaryErrors", func(t *testing.T) {
		// A chain beyond the ceiling that passes through an unexported
		// type is a leaf for the preflight, because the mapper reports
		// the exportness violation without descending; the ordinary
		// diagnostic survives.
		var b strings.Builder
		b.WriteString("package synth\n\nimport \"context\"\n\n// @intercall type t0\ntype T0 t1\n\n")
		for i := 1; i < 5000; i++ {
			fmt.Fprintf(&b, "type t%d []t%d\n", i, i+1)
		}
		b.WriteString("type t5000 uint8\n")
		b.WriteString("// @intercall procedure p\nfunc P(ctx context.Context, x T0) error { return nil }\n")
		_, err := mapOne(t, "example.com/synth", b.String())
		wantErr(t, err, "must be exported")
	})

	t.Run("MalformedDeepInputReturnsOrdinaryError", func(t *testing.T) {
		// A deep chain ending in an unresolved name is an ordinary
		// validation error, never a panic or a depth diagnostic.
		src := "type t " + strings.Repeat("list ", maxProjectionDepth) + "missing;\n"
		f, err := syntax.Parse("depth.intercall", []byte(src))
		if err != nil {
			t.Fatalf("parse: %v", err)
		}
		err = syntax.Validate(f)
		wantErr(t, err, `unresolved type reference "missing"`)
	})
}
