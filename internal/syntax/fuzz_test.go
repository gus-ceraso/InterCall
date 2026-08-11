package syntax_test

import (
	"testing"

	"github.com/cerasos/intercall/internal/syntax"
)

// FuzzParse exercises the parser on arbitrary bytes.
//
// The invariants hold for every input: Parse never panics, a non-nil error
// is always a *syntax.Error whose position is an exact in-range byte offset
// of the input (invalid UTF-8 at its first bad byte, EOF at len(src)), and
// a successful parse yields a file whose size, comment spans, and every
// declaration and type-occurrence span are exact in-range byte ranges.
func FuzzParse(f *testing.F) {
	seeds := []string{
		"",
		"   \t\r\n\f\v",
		"/* only a comment */",
		"/* a /* b */ type t uint8;",
		"type user record {\n    name string;\n    sex uint8;\n};\n\nexception unknown;\nprocedure get_user {\n    name string;\n} user;\n",
		"type a list list uint8; type b list record { x uint8; }; procedure p { xs list bytes; } record { ok uint8; };",
		"type t record {}; exception e record {}; procedure ping {};",
		"type t int8; type t int16; type t int32; type t int64; type t uint8; type t uint16; type t uint32; type t uint64; type t float32; type t float64; type t string; type t bytes;",
		"type x record {\r\n\tf uint8;\r\n};",
		"\xEF\xBB\xBFtype x uint8;",
		"type x \x80;",
		"/* unterminated",
		"procedure p { x string;",
		"type list uint8;",
		"listlist uint8;",
		"exception procedure;",
		"\x00\x01\x02",
		"type x record { f list list list list uint8; };",
		"type héllo;",
	}
	for _, seed := range seeds {
		f.Add([]byte(seed))
	}
	f.Fuzz(func(t *testing.T, src []byte) {
		file, err := syntax.Parse("fuzz", src)
		if err != nil {
			e, ok := err.(*syntax.Error)
			if !ok {
				t.Fatalf("error type %T, want *syntax.Error", err)
			}
			if e.Pos.Offset < 0 || e.Pos.Offset > len(src) {
				t.Fatalf("error offset %d out of range [0, %d]", e.Pos.Offset, len(src))
			}
			if e.Pos.Line < 1 || e.Pos.Column < 1 {
				t.Fatalf("error position %+v has non-positive line or column", e.Pos)
			}
			if e.Span.Start < 0 || e.Span.End < e.Span.Start || e.Span.End > len(src) {
				t.Fatalf("error span %v out of range for size %d", e.Span, len(src))
			}
			if file != nil {
				t.Fatal("non-nil file alongside an error")
			}
			return
		}
		if file == nil {
			t.Fatal("nil file without an error")
		}
		if file.Size != len(src) {
			t.Fatalf("file size = %d, want %d", file.Size, len(src))
		}
		for i, c := range file.Comments {
			checkSpan(t, "comment", i, c.Span, len(src))
		}
		for i, d := range file.Decls {
			checkSpan(t, "decl", i, d.Span(), len(src))
			walkType(t, "decl", i, d, len(src))
		}
	})
}

// checkSpan asserts that span is a valid in-range byte range.
func checkSpan(t *testing.T, what string, i int, span syntax.Span, size int) {
	t.Helper()
	if span.Start < 0 || span.End < span.Start || span.End > size {
		t.Fatalf("%s %d span %v out of range for size %d", what, i, span, size)
	}
}

// walkType checks every type occurrence reachable from a declaration.
func walkType(t *testing.T, what string, i int, d syntax.Decl, size int) {
	t.Helper()
	var visit func(typ syntax.TypeExpr, path string)
	visit = func(typ syntax.TypeExpr, path string) {
		checkSpan(t, what+" type", i, typ.Span(), size)
		switch typ := typ.(type) {
		case *syntax.ListType:
			visit(typ.Elem, path+".elem")
		case *syntax.RecordType:
			checkSpan(t, what+" lbrace", i, typ.LBrace, size)
			checkSpan(t, what+" rbrace", i, typ.RBrace, size)
			for j, fld := range typ.Fields {
				checkSpan(t, what+" field", j, fld.Span(), size)
				checkSpan(t, what+" field name", j, fld.Name.Span(), size)
				visit(fld.Type, path+".field")
			}
		}
	}
	switch d := d.(type) {
	case *syntax.TypeDecl:
		visit(d.Type, "type")
	case *syntax.ExceptionDecl:
		if d.Type != nil {
			visit(d.Type, "payload")
		}
	case *syntax.ProcDecl:
		for j, p := range d.Params {
			checkSpan(t, what+" param", j, p.Span(), size)
			checkSpan(t, what+" param name", j, p.Name.Span(), size)
			visit(p.Type, "param")
		}
		if d.Result != nil {
			visit(d.Result, "result")
		}
	}
}
