package syntax_test

import (
	"fmt"
	"strings"
	"testing"

	"github.com/cerasos/intercall/go/internal/syntax"
)

// attachDocs parses, validates, and attaches documentation for src.
func attachDocs(t *testing.T, src string) *syntax.File {
	t.Helper()
	f := mustParse(t, src)
	if err := syntax.Validate(f); err != nil {
		t.Fatalf("Validate(%q) failed: %v", src, err)
	}
	syntax.AttachDocs(f)
	return f
}

// dumpDocs renders every documentation slot in source order.
func dumpDocs(f *syntax.File) string {
	var b strings.Builder
	for i, d := range f.Decls {
		dumpDeclDocs(&b, i, d, 0)
	}
	return b.String()
}

func dumpDeclDocs(b *strings.Builder, i int, d syntax.Decl, depth int) {
	pad := indent(depth)
	switch d := d.(type) {
	case *syntax.TypeDecl:
		fmt.Fprintf(b, "%sdecl %d doc %q\n", pad, i, d.Doc)
		dumpTypeDocs(b, d.Type, depth+1)
	case *syntax.ExceptionDecl:
		fmt.Fprintf(b, "%sdecl %d doc %q\n", pad, i, d.Doc)
		if d.Type != nil {
			dumpTypeDocs(b, d.Type, depth+1)
		}
	case *syntax.ProcDecl:
		fmt.Fprintf(b, "%sdecl %d doc %q\n", pad, i, d.Doc)
		for j, p := range d.Params {
			dumpParamDocs(b, j, p, depth+1)
		}
		if d.Result != nil {
			dumpTypeDocs(b, d.Result, depth+1)
		}
	}
}

func dumpParamDocs(b *strings.Builder, i int, p *syntax.Param, depth int) {
	pad := indent(depth)
	fmt.Fprintf(b, "%sparam %d doc %q\n", pad, i, p.Doc)
	dumpTypeDocs(b, p.Type, depth+1)
}

func dumpFieldDocs(b *strings.Builder, i int, f *syntax.Field, depth int) {
	pad := indent(depth)
	fmt.Fprintf(b, "%sfield %d doc %q\n", pad, i, f.Doc)
	dumpTypeDocs(b, f.Type, depth+1)
}

func dumpTypeDocs(b *strings.Builder, t syntax.TypeExpr, depth int) {
	pad := indent(depth)
	switch t := t.(type) {
	case *syntax.PrimType:
		fmt.Fprintf(b, "%stype doc %q\n", pad, t.Doc)
	case *syntax.NamedType:
		fmt.Fprintf(b, "%stype doc %q\n", pad, t.Doc)
	case *syntax.ListType:
		fmt.Fprintf(b, "%slist doc %q\n", pad, t.Doc)
		dumpTypeDocs(b, t.Elem, depth+1)
	case *syntax.RecordType:
		fmt.Fprintf(b, "%srecord doc %q\n", pad, t.Doc)
		for j, f := range t.Fields {
			dumpFieldDocs(b, j, f, depth+1)
		}
	}
}

// TestAttachDocsAnchors covers every eligible anchor: the first token of a
// declaration, parameter, field, and type occurrence, including the
// underlying type of a declaration, an exception payload, a procedure
// return, a parameter or field type, a list element, and an inline record.
func TestAttachDocsAnchors(t *testing.T) {
	src := `/* decl */ type t /* underlying */ uint8;
/* decl */ exception e /* payload */ record { /* field */ f /* ftype */ int16; };
/* decl */ procedure p {
    /* param */ x /* ptype */ list /* elem */ int8;
}
/* result */ record { /* rfield */ g /* rtype */ string; };`
	f := attachDocs(t, src)
	want := `decl 0 doc "decl"
  type doc "underlying"
decl 1 doc "decl"
  record doc "payload"
    field 0 doc "field"
      type doc "ftype"
decl 2 doc "decl"
  param 0 doc "param"
    list doc "ptype"
      type doc "elem"
  record doc "result"
    field 0 doc "rfield"
      type doc "rtype"
`
	if got := dumpDocs(f); got != want {
		t.Errorf("docs mismatch:\n--- got ---\n%s--- want ---\n%s", got, want)
	}
}

// TestAttachDocsNamedAndListCovers comments between a parameter, field,
// exception, or type-declaration name and its type, and comments after
// "list", which anchor the element.
func TestAttachDocsNamedAndList(t *testing.T) {
	src := `type t /* named */ uint8;
exception e /* payload */ record {};
procedure p {
    x /* xtype */ int8;
    y list /* yelem */ bytes;
    z /* zlist */ list /* zdelem */ string;
}
/* result */ a;`
	// The referenced types must exist for validation.
	src = `type a uint8;
` + src
	f := attachDocs(t, src)
	want := `decl 0 doc ""
  type doc ""
decl 1 doc ""
  type doc "named"
decl 2 doc ""
  record doc "payload"
decl 3 doc ""
  param 0 doc ""
    type doc "xtype"
  param 1 doc ""
    list doc ""
      type doc "yelem"
  param 2 doc ""
    list doc "zlist"
      type doc "zdelem"
  type doc "result"
`
	if got := dumpDocs(f); got != want {
		t.Errorf("docs mismatch:\n--- got ---\n%s--- want ---\n%s", got, want)
	}
}

// TestAttachDocsTrailing covers the trailing rule: a comment after a
// completed node on the same physical line attaches to nothing, except
// that a candidate type prefix (a type, exception, parameter, or field
// name, "list", or a procedure '}') directly before the comment makes it
// eligible for the type the prefix introduces even when the prefix
// follows an earlier same-line node. The same comment on its own line
// attaches to the next anchor.
func TestAttachDocsTrailing(t *testing.T) {
	tests := []struct {
		name string
		src  string
		want string
	}{
		{
			name: "after-semicolon",
			src:  "type t uint8; /* c */\ntype u int16;",
			want: "decl 0 doc \"\"\n  type doc \"\"\ndecl 1 doc \"\"\n  type doc \"\"\n",
		},
		{
			name: "own-line",
			src:  "type t uint8;\n/* c */\ntype u int16;",
			want: "decl 0 doc \"\"\n  type doc \"\"\ndecl 1 doc \"c\"\n  type doc \"\"\n",
		},
		{
			name: "trailing-prefix",
			src:  "type t uint8; /* c1 */\n/* c2 */\ntype u int16;",
			want: "decl 0 doc \"\"\n  type doc \"\"\ndecl 1 doc \"c2\"\n  type doc \"\"\n",
		},
		{
			name: "after-type",
			src:  "type v /* c */ int32; /* after type */",
			want: "decl 0 doc \"\"\n  type doc \"c\"\n",
		},
		{
			name: "after-param",
			src:  "procedure p {\n    x int8; /* c */\n    y int16;\n};",
			want: "decl 0 doc \"\"\n  param 0 doc \"\"\n    type doc \"\"\n  param 1 doc \"\"\n    type doc \"\"\n",
		},
		{
			name: "same-line-as-param-end",
			src:  "procedure p { y int8; x /* c */ int8; };",
			want: "decl 0 doc \"\"\n  param 0 doc \"\"\n    type doc \"\"\n  param 1 doc \"\"\n    type doc \"c\"\n",
		},
		{
			name: "same-line-after-semicolon",
			src:  "procedure p { x int8; /* c */ y int16; };",
			want: "decl 0 doc \"\"\n  param 0 doc \"\"\n    type doc \"\"\n  param 1 doc \"\"\n    type doc \"\"\n",
		},
		{
			name: "after-record",
			src:  "type t record { f uint8; } /* c */;",
			want: "decl 0 doc \"\"\n  record doc \"\"\n    field 0 doc \"\"\n      type doc \"\"\n",
		},
		{
			name: "empty-block-result",
			src:  "procedure p { } /* c */ int8;",
			want: "decl 0 doc \"\"\n  type doc \"c\"\n",
		},
		{
			name: "nonempty-block-result",
			src:  "procedure p { x int8; } /* c */ int8;",
			want: "decl 0 doc \"\"\n  param 0 doc \"\"\n    type doc \"\"\n  type doc \"c\"\n",
		},
		{
			name: "type-name-after-early-decl",
			src:  "type a uint8; type b /* c */ a;",
			want: "decl 0 doc \"\"\n  type doc \"\"\ndecl 1 doc \"\"\n  type doc \"c\"\n",
		},
		{
			name: "exception-name-after-early-decl",
			src:  "exception e record {}; exception f /* c */ record {};",
			want: "decl 0 doc \"\"\n  record doc \"\"\ndecl 1 doc \"\"\n  record doc \"c\"\n",
		},
		{
			name: "field-name-after-early-field",
			src:  "type t record { f uint8; g /* c */ int16; };",
			want: "decl 0 doc \"\"\n  record doc \"\"\n    field 0 doc \"\"\n      type doc \"\"\n    field 1 doc \"\"\n      type doc \"c\"\n",
		},
		{
			name: "list-after-early-decl",
			src:  "type t list bytes; type u list /* c */ bytes;",
			want: "decl 0 doc \"\"\n  list doc \"\"\n    type doc \"\"\ndecl 1 doc \"\"\n  list doc \"\"\n    type doc \"c\"\n",
		},
		{
			name: "trailing-after-semicolon-same-line",
			src:  "type a uint8; /* c */ type b int16;",
			want: "decl 0 doc \"\"\n  type doc \"\"\ndecl 1 doc \"\"\n  type doc \"\"\n",
		},
		{
			name: "exception-omitted-payload",
			src:  "exception e /* c */;\ntype t uint8;",
			want: "decl 0 doc \"\"\ndecl 1 doc \"\"\n  type doc \"\"\n",
		},
		{
			name: "before-lbrace",
			src:  "procedure p /* c */ { x int8; };",
			want: "decl 0 doc \"\"\n  param 0 doc \"\"\n    type doc \"\"\n",
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			f := attachDocs(t, tc.src)
			if got := dumpDocs(f); got != tc.want {
				t.Errorf("docs mismatch for %q:\n--- got ---\n%s--- want ---\n%s", tc.src, got, tc.want)
			}
		})
	}
}

// TestAttachDocsSharedLineTypeAnchors covers the required regression
// shape: a comment after a prior same-line node still attaches when it
// follows the later declaration, parameter, field, exception, list, or
// return prefix, and the resulting type document is preserved by the
// canonical format round trip.
func TestAttachDocsSharedLineTypeAnchors(t *testing.T) {
	src := `type a uint8; type b /* doc */ a;
procedure p { x int8; y /* doc */ int16; };
type t record { f uint8; g /* doc */ int16; };
type u list bytes; type v list /* doc */ bytes;
exception e record {}; exception f /* doc */ record {};
procedure q { x int8; } /* doc */ int8;`
	f := attachDocs(t, src)
	want := `decl 0 doc ""
  type doc ""
decl 1 doc ""
  type doc "doc"
decl 2 doc ""
  param 0 doc ""
    type doc ""
  param 1 doc ""
    type doc "doc"
decl 3 doc ""
  record doc ""
    field 0 doc ""
      type doc ""
    field 1 doc ""
      type doc "doc"
decl 4 doc ""
  list doc ""
    type doc ""
decl 5 doc ""
  list doc ""
    type doc "doc"
decl 6 doc ""
  record doc ""
decl 7 doc ""
  record doc "doc"
decl 8 doc ""
  param 0 doc ""
    type doc ""
  type doc "doc"
`
	if got := dumpDocs(f); got != want {
		t.Errorf("docs mismatch:\n--- got ---\n%s--- want ---\n%s", got, want)
	}
	// Every attached document must appear exactly once in the canonical
	// output, and the output must round-trip byte for byte with the same
	// documentation on every slot.
	if got := string(syntax.Format(f)); strings.Count(got, "doc") != 6 {
		t.Errorf("canonical output contains %d doc bodies, want 6:\n%s", strings.Count(got, "doc"), got)
	}
	checkCanonical(t, "shared-line-type-anchors", syntax.Format(f), f)
}

// TestAttachDocsBlankLines covers documentation-group formation: the
// maximal run of comments immediately before an anchor, with no blank line
// within the group or between the group and the anchor, joined with two
// LFs after discarding empty bodies. Blank lines follow the physical-line
// model: only LF-terminated lines count, a CRLF is one terminator, and a
// bare CR is an ordinary byte, so a line containing one is not blank.
func TestAttachDocsBlankLines(t *testing.T) {
	tests := []struct {
		name string
		src  string
		want string
	}{
		{
			name: "group",
			src:  "/* a */\n/* b */\ntype t uint8;",
			want: "decl 0 doc \"a\\n\\nb\"\n  type doc \"\"\n",
		},
		{
			name: "same-line-group",
			src:  "/* a */ /* b */ type t uint8;",
			want: "decl 0 doc \"a\\n\\nb\"\n  type doc \"\"\n",
		},
		{
			name: "blank-line-within",
			src:  "/* a */\n\n/* b */\ntype t uint8;",
			want: "decl 0 doc \"b\"\n  type doc \"\"\n",
		},
		{
			name: "blank-line-before-anchor",
			src:  "/* a */\n\ntype t uint8;",
			want: "decl 0 doc \"\"\n  type doc \"\"\n",
		},
		{
			name: "empty-bodies",
			src:  "/* a */\n/**/\n/* b */\ntype t uint8;",
			want: "decl 0 doc \"a\\n\\nb\"\n  type doc \"\"\n",
		},
		{
			name: "all-empty",
			src:  "/**/\ntype t uint8;",
			want: "decl 0 doc \"\"\n  type doc \"\"\n",
		},
		{
			name: "crlf-blank-line",
			src:  "/* a */\r\n\r\n/* b */\r\ntype t uint8;\r\n",
			want: "decl 0 doc \"b\"\n  type doc \"\"\n",
		},
		{
			name: "bare-cr-is-content",
			src:  "type t /* a */\r\r/* b */ uint8;",
			want: "decl 0 doc \"\"\n  type doc \"a\\n\\nb\"\n",
		},
		{
			name: "bare-cr-line-not-blank",
			src:  "/* a */\n  \r  \n/* b */\ntype t uint8;",
			want: "decl 0 doc \"a\\n\\nb\"\n  type doc \"\"\n",
		},
		{
			name: "mixed-crlf-lf-blank",
			src:  "/* a */\n\r\n/* b */\ntype t uint8;",
			want: "decl 0 doc \"b\"\n  type doc \"\"\n",
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			f := attachDocs(t, tc.src)
			if got := dumpDocs(f); got != tc.want {
				t.Errorf("docs mismatch for %q:\n--- got ---\n%s--- want ---\n%s", tc.src, got, tc.want)
			}
		})
	}
}

// TestAttachDocsNormalization covers the documentation normalization
// function: CRLF and bare CR to LF, trailing spaces and tabs removed,
// leading and trailing blank lines removed, the longest spaces-and-tabs
// prefix shared by all nonblank lines removed, and lines joined with LF.
func TestAttachDocsNormalization(t *testing.T) {
	tests := []struct {
		name string
		src  string
		want string
	}{
		{
			name: "trailing-spaces",
			src:  "type t /* a   */ uint8;",
			want: "type doc \"a\"\n",
		},
		{
			name: "shared-prefix",
			src:  "type t /*    first\n      second\t */ uint8;",
			want: "type doc \"first\\n  second\"\n",
		},
		{
			name: "no-shared-prefix",
			src:  "type t /* \tfirst\n   second */ uint8;",
			want: "type doc \"\\tfirst\\n  second\"\n",
		},
		{
			name: "blank-body-lines",
			src:  "type t /*\n\n a \n\n */ uint8;",
			want: "type doc \"a\"\n",
		},
		{
			name: "middle-blank-line",
			src:  "type t /*  a\n\n   b */ uint8;",
			want: "type doc \"a\\n\\n b\"\n",
		},
		{
			name: "crlf",
			src:  "type t /* a\r\n   b */ uint8;",
			want: "type doc \"a\\n  b\"\n",
		},
		{
			name: "bare-cr",
			src:  "type t /* a\r   b */ uint8;",
			want: "type doc \"a\\n  b\"\n",
		},
		{
			name: "tab-indent",
			src:  "type t /*\ta */ uint8;",
			want: "type doc \"a\"\n",
		},
		{
			name: "mixed-tab-prefix",
			src:  "type t /* \ta\n  b */ uint8;",
			want: "type doc \"\\ta\\n b\"\n",
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			f := attachDocs(t, tc.src)
			if got := dumpDocs(f); got != "decl 0 doc \"\"\n  "+tc.want {
				t.Errorf("docs mismatch for %q:\n--- got ---\n%s--- want ---\n%s", tc.src, got, "decl 0 doc \"\"\n  "+tc.want)
			}
		})
	}
}

// TestAttachDocsUnicode covers Unicode comment bodies, which are
// documentation text like any other.
func TestAttachDocsUnicode(t *testing.T) {
	f := attachDocs(t, "/* héllo 世界 😀 */ type t string;")
	if got := dumpDocs(f); got != "decl 0 doc \"héllo 世界 😀\"\n  type doc \"\"\n" {
		t.Errorf("docs mismatch:\n%s", got)
	}
}

// TestAttachDocsAtMostOnce verifies that each comment attaches to exactly
// one slot: every attached body appears exactly once in the canonical
// output.
func TestAttachDocsAtMostOnce(t *testing.T) {
	src := "/* alpha */ type t /* beta */ list /* gamma */ uint8;"
	f := attachDocs(t, src)
	got := string(syntax.Format(f))
	for _, text := range []string{"alpha", "beta", "gamma"} {
		if n := strings.Count(got, text); n != 1 {
			t.Errorf("doc %q appears %d times in canonical output, want 1:\n%s", text, n, got)
		}
	}
}

// TestAttachDocsIdempotent verifies that a second attachment pass
// recomputes the same documentation.
func TestAttachDocsIdempotent(t *testing.T) {
	src := "/* a */ type t uint8; /* trailing */\n/* b */ type u /* c */ int16;"
	f := attachDocs(t, src)
	first := dumpDocs(f)
	syntax.AttachDocs(f)
	if got := dumpDocs(f); got != first {
		t.Errorf("second attachment changed docs:\n%s", got)
	}
}

// TestAttachDocsRecursive verifies the recursive rules on nested anchors:
// a documented list element inside a record field inside a list, and a
// comment between the list and the record anchor the record, not the list.
func TestAttachDocsRecursive(t *testing.T) {
	src := `type t list /* rec */ record {
    f /* field */ uint8;
};`
	f := attachDocs(t, src)
	want := `decl 0 doc ""
  list doc ""
    record doc "rec"
      field 0 doc ""
        type doc "field"
`
	if got := dumpDocs(f); got != want {
		t.Errorf("docs mismatch:\n--- got ---\n%s--- want ---\n%s", got, want)
	}
}
