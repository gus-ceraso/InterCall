package syntax_test

import (
	"strings"
	"testing"

	"github.com/cerasos/intercall/internal/syntax"
)

// mustParse parses src, failing the test on error.
func mustParse(t *testing.T, src string) *syntax.File {
	t.Helper()
	f, err := syntax.Parse("", []byte(src))
	if err != nil {
		t.Fatalf("Parse(%q) failed: %v", src, err)
	}
	return f
}

// parseErr parses src and returns the *syntax.Error, failing the test when
// parsing succeeds.
func parseErr(t *testing.T, src string) *syntax.Error {
	t.Helper()
	f, err := syntax.Parse("", []byte(src))
	if err == nil {
		t.Fatalf("Parse(%q) succeeded with %d declarations", src, len(f.Decls))
	}
	e, ok := err.(*syntax.Error)
	if !ok {
		t.Fatalf("Parse(%q) error type %T, want *syntax.Error", src, err)
	}
	return e
}

func TestParseEmptyAndTriviaOnly(t *testing.T) {
	for _, src := range []string{"", "   ", "\t\r\n\f\v", " \n \t \n ", "/* only a comment */", "/* a */\n/* b */\n", "/* leading */  \n/* trailing */"} {
		f := mustParse(t, src)
		if len(f.Decls) != 0 {
			t.Errorf("src %q: %d declarations, want 0", src, len(f.Decls))
		}
		if f.Size != len(src) {
			t.Errorf("src %q: Size = %d, want %d", src, f.Size, len(src))
		}
	}
	// A file containing only whitespace has no comments either.
	f := mustParse(t, " \t\r\n")
	if len(f.Comments) != 0 {
		t.Errorf("trivia-only file has %d comments", len(f.Comments))
	}
}

func TestParseEveryPrimitive(t *testing.T) {
	prims := []string{"int8", "int16", "int32", "int64", "uint8", "uint16", "uint32", "uint64", "float32", "float64", "string", "bytes"}
	for _, p := range prims {
		src := "type t " + p + ";"
		f := mustParse(t, src)
		if len(f.Decls) != 1 {
			t.Fatalf("src %q: %d declarations", src, len(f.Decls))
		}
		d := f.Decls[0].(*syntax.TypeDecl)
		prim, ok := d.Type.(*syntax.PrimType)
		if !ok {
			t.Fatalf("src %q: type is %T, want *syntax.PrimType", src, d.Type)
		}
		if got := prim.Span(); f.Text(got) != p {
			t.Errorf("src %q: prim span text = %q, want %q", src, f.Text(got), p)
		}
	}
}

func TestParseEveryTypeNestingForm(t *testing.T) {
	tests := []struct {
		name string
		src  string
	}{
		{"list of primitive", "type t list uint8;"},
		{"list of list", "type t list list uint8;"},
		{"list of list of list", "type t list list list float64;"},
		{"list of record", "type t list record { x uint8; };"},
		{"list of named", "type u uint8; type t list u;"},
		{"record empty", "type t record {};"},
		{"record all prims", "type t record { a int8; b uint16; c float32; d string; e bytes; };"},
		{"record nested record", "type t record { r record { inner list string; }; };"},
		{"record list of record", "type t record { xs list record { y int64; }; };"},
		{"named of record", "type r record {}; type t r;"},
		{"named of list of named", "type u uint8; type t list u;"},
		{"param list of list", "procedure p { xs list list bytes; };"},
		{"return record", "procedure p {} record { ok uint8; };"},
		{"param and return named", "type u uint8; procedure p { x u; } u;"},
		{"exception payload record", "exception e record {};"},
		{"exception payload list", "exception e list string;"},
		{"exception payload named", "type u uint8; exception e u;"},
		{"field list of record of list", "type t record { a list record { b list uint16; }; };"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := mustParse(t, tt.src)
			if len(f.Decls) == 0 {
				t.Fatal("no declarations")
			}
		})
	}
}

func TestParseDeclarationSpans(t *testing.T) {
	src := "type user uint8;"
	f := mustParse(t, src)
	d := f.Decls[0].(*syntax.TypeDecl)
	if got, want := d.Span(), (syntax.Span{0, 16}); got != want {
		t.Errorf("decl span = %v, want %v", got, want)
	}
	if got, want := d.TypeSpan, (syntax.Span{0, 4}); got != want {
		t.Errorf("type keyword span = %v, want %v", got, want)
	}
	if got, want := d.Semi, (syntax.Span{15, 16}); got != want {
		t.Errorf("semi span = %v, want %v", got, want)
	}
	if got, want := d.Name.Span(), (syntax.Span{5, 9}); got != want {
		t.Errorf("name span = %v, want %v", got, want)
	}
	if got, want := d.Name.Name, "user"; got != want {
		t.Errorf("name = %q, want %q", got, want)
	}
}

func TestParseExceptionForms(t *testing.T) {
	// Without payload.
	f := mustParse(t, "exception unavailable;")
	d := f.Decls[0].(*syntax.ExceptionDecl)
	if d.Type != nil {
		t.Errorf("payload = %v, want nil", d.Type)
	}
	if got, want := d.Span(), (syntax.Span{0, 22}); got != want {
		t.Errorf("span = %v, want %v", got, want)
	}

	// With each payload kind.
	for _, src := range []string{
		"exception e uint8;",
		"exception e record { field string; };",
		"exception e list int32;",
		"type u bytes; exception e u;",
	} {
		f := mustParse(t, src)
		d := f.Decls[len(f.Decls)-1].(*syntax.ExceptionDecl)
		if d.Type == nil {
			t.Errorf("src %q: payload omitted, want a type", src)
		}
	}
}

func TestParseProcedureForms(t *testing.T) {
	tests := []struct {
		src       string
		params    int
		hasResult bool
	}{
		{"procedure ping {};", 0, false},
		{"procedure ping {} uint8;", 0, true},
		{"procedure notify { message string; };", 1, false},
		{"procedure add { a int32; b int32; } int32;", 2, true},
		{"procedure p { x list list uint8; y record { z bytes; }; };", 2, false},
	}
	for _, tt := range tests {
		f := mustParse(t, tt.src)
		d := f.Decls[0].(*syntax.ProcDecl)
		if len(d.Params) != tt.params {
			t.Errorf("src %q: %d params, want %d", tt.src, len(d.Params), tt.params)
		}
		if (d.Result != nil) != tt.hasResult {
			t.Errorf("src %q: hasResult = %v, want %v", tt.src, d.Result != nil, tt.hasResult)
		}
	}
	// Parameter spans cover name through semicolon.
	f := mustParse(t, "procedure p { a int32; b string; };")
	d := f.Decls[0].(*syntax.ProcDecl)
	if got, want := d.Params[0].Span(), (syntax.Span{14, 22}); got != want {
		t.Errorf("param 0 span = %v, want %v", got, want)
	}
	if got, want := d.Params[1].Span(), (syntax.Span{23, 32}); got != want {
		t.Errorf("param 1 span = %v, want %v", got, want)
	}
}

func TestParseListAndRecordSpans(t *testing.T) {
	f := mustParse(t, "type t list list uint8;")
	d := f.Decls[0].(*syntax.TypeDecl)
	outer, ok := d.Type.(*syntax.ListType)
	if !ok {
		t.Fatalf("type is %T, want *syntax.ListType", d.Type)
	}
	if got, want := outer.Span(), (syntax.Span{7, 22}); got != want {
		t.Errorf("outer list span = %v, want %v", got, want)
	}
	if got, want := outer.ListSpan, (syntax.Span{7, 11}); got != want {
		t.Errorf("outer list keyword span = %v, want %v", got, want)
	}
	inner, ok := outer.Elem.(*syntax.ListType)
	if !ok {
		t.Fatalf("elem is %T, want *syntax.ListType", outer.Elem)
	}
	if got, want := inner.Span(), (syntax.Span{12, 22}); got != want {
		t.Errorf("inner list span = %v, want %v", got, want)
	}
	prim, ok := inner.Elem.(*syntax.PrimType)
	if !ok || prim.Kind.String() != "uint8" {
		t.Fatalf("inner elem = %#v, want uint8 primitive", inner.Elem)
	}

	f = mustParse(t, "type t record { x uint8; };")
	d = f.Decls[0].(*syntax.TypeDecl)
	rec, ok := d.Type.(*syntax.RecordType)
	if !ok {
		t.Fatalf("type is %T, want *syntax.RecordType", d.Type)
	}
	if got, want := rec.Span(), (syntax.Span{7, 26}); got != want {
		t.Errorf("record span = %v, want %v", got, want)
	}
	if got, want := rec.RecordSpan, (syntax.Span{7, 13}); got != want {
		t.Errorf("record keyword span = %v, want %v", got, want)
	}
	if got, want := rec.LBrace, (syntax.Span{14, 15}); got != want {
		t.Errorf("lbrace span = %v, want %v", got, want)
	}
	if got, want := rec.RBrace, (syntax.Span{25, 26}); got != want {
		t.Errorf("rbrace span = %v, want %v", got, want)
	}
	if len(rec.Fields) != 1 {
		t.Fatalf("%d fields, want 1", len(rec.Fields))
	}
	field := rec.Fields[0]
	if got, want := field.Span(), (syntax.Span{16, 24}); got != want {
		t.Errorf("field span = %v, want %v", got, want)
	}
	if got, want := field.Name.Name, "x"; got != want {
		t.Errorf("field name = %q, want %q", got, want)
	}
}

func TestParseCommentsCapturedInOrder(t *testing.T) {
	src := "/* a */ type t /* b */ uint8; /* c */"
	f := mustParse(t, src)
	if len(f.Comments) != 3 {
		t.Fatalf("%d comments, want 3", len(f.Comments))
	}
	want := []struct {
		span syntax.Span
		text string
	}{
		{syntax.Span{0, 7}, " a "},
		{syntax.Span{15, 22}, " b "},
		{syntax.Span{30, 37}, " c "},
	}
	for i, w := range want {
		if f.Comments[i].Span != w.span {
			t.Errorf("comment %d span = %v, want %v", i, f.Comments[i].Span, w.span)
		}
		if f.Comments[i].Text != w.text {
			t.Errorf("comment %d text = %q, want %q", i, f.Comments[i].Text, w.text)
		}
	}
}

func TestParseCommentsInsideNestedStructures(t *testing.T) {
	src := "procedure p /* c1 */ { /* c2 */ x /* c3 */ list /* c4 */ record { /* c5 */ f bytes; } /* c6 */ ; };"
	f := mustParse(t, src)
	if len(f.Comments) != 6 {
		t.Fatalf("%d comments, want 6", len(f.Comments))
	}
	for i, c := range f.Comments {
		want := " c" + string(rune('1'+i)) + " "
		if c.Text != want {
			t.Errorf("comment %d text = %q, want %q", i, c.Text, want)
		}
	}
}

func TestParseNonNestingComments(t *testing.T) {
	// The inner "/*" is ordinary text; the comment ends at the first "*/".
	src := "/* a /* b */ type t uint8;"
	f := mustParse(t, src)
	if len(f.Comments) != 1 {
		t.Fatalf("%d comments, want 1", len(f.Comments))
	}
	if got, want := f.Comments[0].Text, " a /* b "; got != want {
		t.Errorf("comment text = %q, want %q", got, want)
	}
	if got, want := f.Comments[0].Span, (syntax.Span{0, 12}); got != want {
		t.Errorf("comment span = %v, want %v", got, want)
	}
	if len(f.Decls) != 1 {
		t.Errorf("%d declarations, want 1", len(f.Decls))
	}
}

func TestParseDocSlotsEmpty(t *testing.T) {
	// Documentation attachment is a later phase; every slot must exist and
	// be empty after parsing.
	src := "/* doc */ type t /* x */ record { /* y */ f list uint8; };\n" +
		"/* doc */ exception e /* p */ string;\n" +
		"/* doc */ procedure p { /* d */ x /* t */ record {}; } /* r */ bytes;"
	f := mustParse(t, src)
	if len(f.Decls) != 3 {
		t.Fatalf("%d declarations, want 3", len(f.Decls))
	}
	var docs []string
	addDocs := func(doc string) { docs = append(docs, doc) }
	for _, d := range f.Decls {
		switch d := d.(type) {
		case *syntax.TypeDecl:
			addDocs(d.Doc)
			collectTypeDocs(d.Type, &docs)
		case *syntax.ExceptionDecl:
			addDocs(d.Doc)
			if d.Type != nil {
				collectTypeDocs(d.Type, &docs)
			}
		case *syntax.ProcDecl:
			addDocs(d.Doc)
			for _, p := range d.Params {
				addDocs(p.Doc)
				collectTypeDocs(p.Type, &docs)
			}
			if d.Result != nil {
				collectTypeDocs(d.Result, &docs)
			}
		}
	}
	if len(docs) == 0 {
		t.Fatal("no documentation slots found")
	}
	for i, doc := range docs {
		if doc != "" {
			t.Errorf("doc slot %d = %q, want empty", i, doc)
		}
	}
}

// collectTypeDocs appends the doc slot of every type occurrence in t.
func collectTypeDocs(t syntax.TypeExpr, docs *[]string) {
	switch t := t.(type) {
	case *syntax.PrimType:
		*docs = append(*docs, t.Doc)
	case *syntax.NamedType:
		*docs = append(*docs, t.Doc)
	case *syntax.ListType:
		*docs = append(*docs, t.Doc)
		collectTypeDocs(t.Elem, docs)
	case *syntax.RecordType:
		*docs = append(*docs, t.Doc)
		for _, f := range t.Fields {
			*docs = append(*docs, f.Doc)
			collectTypeDocs(f.Type, docs)
		}
	}
}

func TestParseCRLFInput(t *testing.T) {
	src := "type user record {\r\n    name string;\r\n};\r\n"
	f := mustParse(t, src)
	if len(f.Decls) != 1 {
		t.Fatalf("%d declarations, want 1", len(f.Decls))
	}
	d := f.Decls[0].(*syntax.TypeDecl)
	if got, want := d.Span(), (syntax.Span{0, 40}); got != want {
		t.Errorf("decl span = %v, want %v", got, want)
	}
	// The first field name "name" begins at offset 24: line 2, column 5.
	rec := d.Type.(*syntax.RecordType)
	pos := f.Position(rec.Fields[0].Name.Span().Start)
	if pos.Offset != 24 || pos.Line != 2 || pos.Column != 5 {
		t.Errorf("field name position = %+v, want offset 24 line 2 column 5", pos)
	}
	// EOF after the trailing CRLF is line 4, column 1.
	eof := f.Position(f.Size)
	if eof.Line != 4 || eof.Column != 1 {
		t.Errorf("EOF position = %d:%d, want 4:1", eof.Line, eof.Column)
	}
}

func TestParseErrorPositions(t *testing.T) {
	tests := []struct {
		name       string
		src        string
		wantOffset int
		wantLine   int
		wantCol    int
		wantMsg    string
	}{
		{"missing name", "type", 4, 1, 5, "expected identifier, found end of file"},
		{"missing name eof", "type ", 5, 1, 6, "expected identifier, found end of file"},
		{"reserved name list", "type list uint8;", 5, 1, 6, "expected identifier, found 'list'"},
		{"reserved name record", "type record {};", 5, 1, 6, "expected identifier, found 'record'"},
		{"reserved name primitive", "type x uint8; type int8;", 19, 1, 20, "expected identifier, found 'int8'"},
		{"reserved exception name", "exception procedure;", 10, 1, 11, "expected identifier, found 'procedure'"},
		{"reserved proc name", "procedure type {};", 10, 1, 11, "expected identifier, found 'type'"},
		{"reserved param name", "procedure p { list uint8; };", 14, 1, 15, "expected identifier, found 'list'"},
		{"reserved field name", "type t record { record uint8; };", 16, 1, 17, "expected identifier, found 'record'"},
		{"missing semi", "type x uint8", 12, 1, 13, "expected ';', found end of file"},
		{"missing type", "type x;", 6, 1, 7, "expected type, found ';'"},
		{"brace where type", "type x { y uint8; };", 7, 1, 8, "expected type, found '{'"},
		{"eof in list", "type x list", 11, 1, 12, "expected type, found end of file"},
		{"eof in record", "type x record {", 15, 1, 16, "expected '}' or field, found end of file"},
		{"eof in params", "procedure p { x uint8;", 22, 1, 23, "expected '}' or parameter, found end of file"},
		{"eof after proc", "procedure p {}", 14, 1, 15, "expected type, found end of file"},
		{"eof after exception", "exception e", 11, 1, 12, "expected type, found end of file"},
		{"no decl", ";", 0, 1, 1, "expected declaration, found ';'"},
		{"closing brace at top", "}", 0, 1, 1, "expected declaration, found '}'"},
		{"keyword at top", "list uint8;", 0, 1, 1, "expected declaration, found 'list'"},
		{"list without elem", "type x list;", 11, 1, 12, "expected type, found ';'"},
		{"record without brace", "type x record;", 13, 1, 14, "expected '{', found ';'"},
		{"missing param semi", "procedure p { x string } uint8;", 23, 1, 24, "expected ';', found '}'"},
		{"field without semi", "type t record { f uint8 } ;", 24, 1, 25, "expected ';', found '}'"},
		{"exception then stray", "exception e; type;", 17, 1, 18, "expected identifier, found ';'"},
		{"second decl bad", "type a uint8; }", 14, 1, 15, "expected declaration, found '}'"},
		{"eof in comment", "type x /* never", 7, 1, 8, "comment not terminated"},
		{"bom", "\xEF\xBB\xBFtype x;", 0, 1, 1, "invalid byte-order mark"},
		{"invalid utf8", "type x \x80;", 7, 1, 8, "invalid UTF-8 encoding"},
		{"invalid char", "type x !", 7, 1, 8, "invalid character '!'"},
		{"bad char in params", "procedure p { x string; !", 24, 1, 25, "invalid character '!'"},
		{"invalid utf8 in record body", "type t record { \x80 };", 16, 1, 17, "invalid UTF-8 encoding"},
		{"unterminated comment in params", "procedure p { x string; /* unterminated", 24, 1, 25, "comment not terminated"},
		{"bom in record body", "type t record { \xEF\xBB\xBF };", 16, 1, 17, "invalid byte-order mark"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			e := parseErr(t, tt.src)
			if e.Pos.Offset != tt.wantOffset {
				t.Errorf("offset = %d, want %d", e.Pos.Offset, tt.wantOffset)
			}
			if e.Pos.Line != tt.wantLine || e.Pos.Column != tt.wantCol {
				t.Errorf("position = %d:%d, want %d:%d", e.Pos.Line, e.Pos.Column, tt.wantLine, tt.wantCol)
			}
			if e.Msg != tt.wantMsg {
				t.Errorf("msg = %q, want %q", e.Msg, tt.wantMsg)
			}
			if e.Span.Start != tt.wantOffset {
				t.Errorf("span start = %d, want %d", e.Span.Start, tt.wantOffset)
			}
		})
	}
}

func TestParseErrorLineColumnAfterNewlines(t *testing.T) {
	// The same error after newlines must carry the right physical position.
	src := "type a uint8;\n\ntype b list;"
	e := parseErr(t, src)
	if e.Pos.Offset != 26 {
		t.Errorf("offset = %d, want 26", e.Pos.Offset)
	}
	if e.Pos.Line != 3 || e.Pos.Column != 12 {
		t.Errorf("position = %d:%d, want 3:12", e.Pos.Line, e.Pos.Column)
	}
}

func TestParseErrorAtEOFOffset(t *testing.T) {
	// Diagnostics at EOF sit at offset len(src) under the same line/column
	// rules as any other position.
	src := "type x"
	e := parseErr(t, src)
	if e.Pos.Offset != len(src) {
		t.Errorf("offset = %d, want %d", e.Pos.Offset, len(src))
	}
	f := syntax.NewFile("", []byte(src))
	if got := f.Position(len(src)); got != e.Pos {
		t.Errorf("error position %+v != File.Position(len) %+v", e.Pos, got)
	}
}

func TestParseErrorFormatting(t *testing.T) {
	_, err := syntax.Parse("iface.intercall", []byte("type list uint8;"))
	e, ok := err.(*syntax.Error)
	if !ok {
		t.Fatalf("error type %T, want *syntax.Error", err)
	}
	if got, want := e.Error(), "iface.intercall:1:6: expected identifier, found 'list'"; got != want {
		t.Errorf("Error() = %q, want %q", got, want)
	}
}

func TestParseNilInput(t *testing.T) {
	f, err := syntax.Parse("", nil)
	if err != nil {
		t.Fatal(err)
	}
	if f.Size != 0 || len(f.Decls) != 0 {
		t.Errorf("nil input parsed to %d bytes, %d declarations", f.Size, len(f.Decls))
	}
}

func TestParseReturnsNilFileOnError(t *testing.T) {
	f, err := syntax.Parse("", []byte("type"))
	if err == nil {
		t.Fatal("parse succeeded")
	}
	if f != nil {
		t.Errorf("file = %v, want nil on error", f)
	}
}

func TestParseDeepNesting(t *testing.T) {
	// Recursive descent must handle deeply nested list types without
	// failure; the grammar has no depth limit.
	depth := 5000
	src := "type t " + strings.Repeat("list ", depth) + "uint8;"
	f := mustParse(t, src)
	d := f.Decls[0].(*syntax.TypeDecl)
	got := 0
	for t := d.Type; ; {
		list, ok := t.(*syntax.ListType)
		if !ok {
			break
		}
		got++
		t = list.Elem
	}
	if got != depth {
		t.Errorf("nesting depth = %d, want %d", got, depth)
	}
}

func TestParseReservedWordsInValidPositions(t *testing.T) {
	// Reserved words are fine where the grammar calls for keywords or
	// primitives; only identifier positions reject them.
	for _, src := range []string{
		"type t list record { f string; };",
		"type t record { f list list uint8; };",
		"exception e record { f list uint8; };",
		"procedure p { x list uint8; } record {};",
		"type t uint8; exception e t; procedure p { a t; } t;",
	} {
		mustParse(t, src)
	}
}

func TestParseCommentsAtEOF(t *testing.T) {
	// A file ending in a complete comment is valid and the comment is kept.
	f := mustParse(t, "type x uint8;/* tail */")
	if len(f.Comments) != 1 {
		t.Fatalf("%d comments, want 1", len(f.Comments))
	}
	if got, want := f.Comments[0].Span, (syntax.Span{13, 23}); got != want {
		t.Errorf("comment span = %v, want %v", got, want)
	}
}
