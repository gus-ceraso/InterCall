package syntax_test

import (
	"bytes"
	"fmt"
	"math/rand"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/cerasos/intercall/internal/syntax"
)

// mustFormat parses, validates, attaches documentation, and formats src.
func mustFormat(t *testing.T, src string) []byte {
	t.Helper()
	f := mustParse(t, src)
	if err := syntax.Validate(f); err != nil {
		t.Fatalf("Validate(%q) failed: %v", src, err)
	}
	syntax.AttachDocs(f)
	return syntax.Format(f)
}

// TestFormatGolden formats every testdata/format/*.intercall fixture —
// parse, validate, attach documentation, and format — and compares the
// canonical bytes against the fixture's .golden file, which is derived
// directly from the SPEC.md canonical formatter algorithm.
func TestFormatGolden(t *testing.T) {
	files, err := filepath.Glob(filepath.Join("testdata", "format", "*.intercall"))
	if err != nil {
		t.Fatal(err)
	}
	if len(files) == 0 {
		t.Fatal("no format fixtures")
	}
	for _, path := range files {
		t.Run(filepath.Base(path), func(t *testing.T) {
			src, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			f, err := syntax.Parse(path, src)
			if err != nil {
				t.Fatalf("Parse failed on a valid fixture: %v", err)
			}
			if err := syntax.Validate(f); err != nil {
				t.Fatalf("Validate failed on a valid fixture: %v", err)
			}
			syntax.AttachDocs(f)
			got := syntax.Format(f)
			want, err := os.ReadFile(path + ".golden")
			if err != nil {
				t.Fatal(err)
			}
			if string(got) != string(want) {
				t.Errorf("format mismatch for %s:\n--- got ---\n%s--- want ---\n%s", path, got, want)
			}
		})
	}
}

// TestFormatGoldenCanonical verifies that every golden file is itself
// canonical: parsing, validating, attaching, and formatting it reproduces
// it byte for byte, ending in one LF.
func TestFormatGoldenCanonical(t *testing.T) {
	files, err := filepath.Glob(filepath.Join("testdata", "format", "*.intercall.golden"))
	if err != nil {
		t.Fatal(err)
	}
	if len(files) == 0 {
		t.Fatal("no format goldens")
	}
	for _, path := range files {
		t.Run(filepath.Base(path), func(t *testing.T) {
			src, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			f, err := syntax.Parse(path, src)
			if err != nil {
				t.Fatalf("Parse failed on a canonical fixture: %v", err)
			}
			if err := syntax.Validate(f); err != nil {
				t.Fatalf("Validate failed on a canonical fixture: %v", err)
			}
			syntax.AttachDocs(f)
			got := syntax.Format(f)
			if string(got) != string(src) {
				t.Errorf("golden is not canonical for %s:\n--- got ---\n%s--- want ---\n%s", path, got, src)
			}
			if len(got) > 0 && got[len(got)-1] != '\n' {
				t.Errorf("golden %s does not end in LF", path)
			}
		})
	}
}

// TestFormatEmpty verifies the empty-body rule: an empty interface, a
// file of only whitespace, and a file of only unattached comments all
// format to zero bytes.
func TestFormatEmpty(t *testing.T) {
	for _, src := range []string{"", "   \t\r\n\f\v", "/* only a comment */", "/* a */\n/* b */\n", "/* trailing */  \n"} {
		if got := mustFormat(t, src); len(got) != 0 {
			t.Errorf("Format(%q) = %q, want zero bytes", src, got)
		}
	}
}

// TestFormatDeclarationOrder verifies that declarations remain in AST
// (source) order with one blank line between them and a single final LF.
func TestFormatDeclarationOrder(t *testing.T) {
	src := "type b uint8;\ntype a int16;\nprocedure z {};\nexception q;\ntype c record {};"
	want := "type b uint8;\n\ntype a int16;\n\nprocedure z {};\n\nexception q;\n\ntype c record {};\n"
	if got := string(mustFormat(t, src)); got != want {
		t.Errorf("format mismatch:\n--- got ---\n%s--- want ---\n%s", got, want)
	}
}

// TestFormatDocumentedTypes covers the documented-type line breaks for
// every prefix kind: type name, field, parameter, exception name,
// procedure '}', and list. A documented type follows its prefix after LF,
// with the document and the type at the type's indentation.
func TestFormatDocumentedTypes(t *testing.T) {
	src := `type a uint8;
type t /* underlying */ a;
exception e /* payload */ record {};
procedure p {
    x /* ptype */ int8;
    y list /* elem */ bytes;
    z /* ztype */ list record {};
}
/* result */ a;`
	want := `type a uint8;

type t
/* underlying */
a;

exception e
/* payload */
record {};

procedure p {
    x
    /* ptype */
    int8;
    y list
    /* elem */
    bytes;
    z
    /* ztype */
    list record {};
}
/* result */
a;
`
	if got := string(mustFormat(t, src)); got != want {
		t.Errorf("format mismatch:\n--- got ---\n%s--- want ---\n%s", got, want)
	}
}

// TestFormatDocBlocks covers the two documentation emission forms: a
// one-line document as "/* D */", and a multi-line document as a block
// with every nonempty line indented four spaces beyond the slot and every
// empty line bare.
func TestFormatDocBlocks(t *testing.T) {
	src := "/* one */ type t uint8;\n/* a\n\n  b */ type u int16;"
	want := "/* one */\ntype t uint8;\n\n/*\n    a\n\n     b\n*/\ntype u int16;\n"
	if got := string(mustFormat(t, src)); got != want {
		t.Errorf("format mismatch:\n--- got ---\n%s--- want ---\n%s", got, want)
	}
}

// TestFormatCRLFAndBareCR verifies that CRLF and bare-CR input produce
// the same canonical LF output, including documentation bodies, and that
// a comment after a completed node on the same physical line stays
// trailing under every line-ending style.
func TestFormatCRLFAndBareCR(t *testing.T) {
	tests := []struct {
		name string
		src  string
		want string
	}{
		{
			name: "crlf",
			src:  "/* doc */ type t uint8;\r\n/* more */ type u int16;\r\n",
			want: "/* doc */\ntype t uint8;\n\n/* more */\ntype u int16;\n",
		},
		{
			name: "bare-cr",
			src:  "/* doc */ type t uint8;\r/* more */ type u int16;\r",
			want: "/* doc */\ntype t uint8;\n\n/* more */\ntype u int16;\n",
		},
		{
			name: "crlf-doc-body",
			src:  "type t /* a\r\n   b */ uint8;\r\n",
			want: "type t\n/*\n    a\n      b\n*/\nuint8;\n",
		},
		{
			name: "bare-cr-doc-body",
			src:  "type t /* a\r   b */ uint8;\r",
			want: "type t\n/*\n    a\n      b\n*/\nuint8;\n",
		},
		{
			name: "crlf-trailing",
			src:  "type t uint8; /* c */\r\ntype u int16;\r\n",
			want: "type t uint8;\n\ntype u int16;\n",
		},
		{
			name: "bare-cr-own-line",
			src:  "type t uint8;\r/* c */\rtype u int16;\r",
			want: "type t uint8;\n\n/* c */\ntype u int16;\n",
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := string(mustFormat(t, tc.src)); got != tc.want {
				t.Errorf("format mismatch for %q:\n--- got ---\n%s--- want ---\n%s", tc.src, got, tc.want)
			}
		})
	}
}

// TestFormatUnicode verifies that Unicode documentation text survives the
// canonical round trip byte for byte.
func TestFormatUnicode(t *testing.T) {
	src := "/* héllo 世界 — doc. */ type t string;\n/* émoji 😀 line\n   第二行 */ type u bytes;"
	want := "/* héllo 世界 — doc. */\ntype t string;\n\n/*\n    émoji 😀 line\n      第二行\n*/\ntype u bytes;\n"
	if got := string(mustFormat(t, src)); got != want {
		t.Errorf("format mismatch:\n--- got ---\n%s--- want ---\n%s", got, want)
	}
}

// TestFormatExistingFixturesIdempotent verifies the idempotent
// parse-validate-attach-format pipeline over every existing valid
// fixture: formatting the canonical output again reproduces it exactly.
func TestFormatExistingFixturesIdempotent(t *testing.T) {
	files, err := filepath.Glob(filepath.Join("testdata", "valid", "*.intercall"))
	if err != nil {
		t.Fatal(err)
	}
	for _, path := range files {
		t.Run(filepath.Base(path), func(t *testing.T) {
			src, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			f, err := syntax.Parse(path, src)
			if err != nil {
				t.Fatalf("Parse failed on a valid fixture: %v", err)
			}
			if err := syntax.Validate(f); err != nil {
				t.Fatalf("Validate failed on a valid fixture: %v", err)
			}
			syntax.AttachDocs(f)
			got := syntax.Format(f)
			checkCanonical(t, path, got, f)
		})
	}
}

// checkCanonical asserts the full canonical properties of got, produced
// from orig: a nonempty output ends in LF, reparsing reproduces the exact
// bytes, and every documentation slot round-trips.
func checkCanonical(t *testing.T, name string, got []byte, orig *syntax.File) {
	t.Helper()
	if len(got) == 0 {
		if len(orig.Decls) != 0 {
			t.Fatalf("%s: empty output for %d declarations", name, len(orig.Decls))
		}
		return
	}
	if got[len(got)-1] != '\n' {
		t.Fatalf("%s: nonempty output does not end in LF", name)
	}
	f2, err := syntax.Parse("canonical", got)
	if err != nil {
		t.Fatalf("%s: canonical output does not parse: %v", name, err)
	}
	if err := syntax.Validate(f2); err != nil {
		t.Fatalf("%s: canonical output does not validate: %v", name, err)
	}
	syntax.AttachDocs(f2)
	if got2 := syntax.Format(f2); !bytes.Equal(got2, got) {
		t.Fatalf("%s: canonical output is not idempotent", name)
	}
	old, new := collectDocs(orig), collectDocs(f2)
	if !reflect.DeepEqual(new, old) {
		t.Fatalf("%s: documentation changed across canonical round trip:\nold: %q\nnew: %q", name, old, new)
	}
}

// collectDocs returns every documentation slot in source order.
func collectDocs(f *syntax.File) []string {
	var docs []string
	var visitType func(t syntax.TypeExpr)
	var visitField func(f *syntax.Field)
	var visitParam func(p *syntax.Param)
	visitField = func(f *syntax.Field) {
		docs = append(docs, f.Doc)
		visitType(f.Type)
	}
	visitParam = func(p *syntax.Param) {
		docs = append(docs, p.Doc)
		visitType(p.Type)
	}
	visitType = func(t syntax.TypeExpr) {
		switch t := t.(type) {
		case *syntax.PrimType:
			docs = append(docs, t.Doc)
		case *syntax.NamedType:
			docs = append(docs, t.Doc)
		case *syntax.ListType:
			docs = append(docs, t.Doc)
			visitType(t.Elem)
		case *syntax.RecordType:
			docs = append(docs, t.Doc)
			for _, f := range t.Fields {
				visitField(f)
			}
		}
	}
	for _, d := range f.Decls {
		switch d := d.(type) {
		case *syntax.TypeDecl:
			docs = append(docs, d.Doc)
			visitType(d.Type)
		case *syntax.ExceptionDecl:
			docs = append(docs, d.Doc)
			if d.Type != nil {
				visitType(d.Type)
			}
		case *syntax.ProcDecl:
			docs = append(docs, d.Doc)
			for _, p := range d.Params {
				visitParam(p)
			}
			if d.Result != nil {
				visitType(d.Result)
			}
		}
	}
	return docs
}

// TestFormatRoundTripProperty drives a deterministic generator over valid
// interfaces with random trivia, comments, and comment bodies, and checks
// the canonical properties: byte-exact output, final LF, idempotence, and
// exact documentation round trip on every slot.
func TestFormatRoundTripProperty(t *testing.T) {
	for seed := int64(1); seed <= 200; seed++ {
		src := genSource(seed)
		f, err := syntax.Parse("prop", []byte(src))
		if err != nil {
			t.Fatalf("seed %d: generated source does not parse: %v\n%s", seed, err, src)
		}
		if err := syntax.Validate(f); err != nil {
			t.Fatalf("seed %d: generated source does not validate: %v\n%s", seed, err, src)
		}
		syntax.AttachDocs(f)
		checkCanonical(t, fmt.Sprintf("seed %d", seed), syntax.Format(f), f)
	}
}

// primitives are the primitive type names.
var primitives = []string{"int8", "int16", "int32", "int64", "uint8", "uint16", "uint32", "uint64", "float32", "float64", "string", "bytes"}

// words are ASCII documentation fragments without '*' or '/'.
var words = []string{"a", "b", "doc", "line", "text", "with spaces", "  padded", "tabbed\t", "one two three"}

// gen is a deterministic generator of valid interface sources with random
// whitespace, comments, and nesting.
type gen struct {
	rnd   *rand.Rand
	b     strings.Builder
	types []string        // declared type names in source order
	keys  map[uint64]bool // procedure and exception keys in use
	np    int             // procedure and exception name counter
	nt    int             // type name counter
}

// genSource generates one valid interface source for seed.
func genSource(seed int64) string {
	g := &gen{rnd: rand.New(rand.NewSource(seed)), keys: make(map[uint64]bool)}
	for i, n := 0, 1+g.rnd.Intn(6); i < n; i++ {
		switch g.rnd.Intn(3) {
		case 0:
			g.typeDecl()
		case 1:
			g.procDecl()
		case 2:
			g.exceptionDecl()
		}
		g.trivia()
	}
	return g.b.String()
}

// trivia emits random whitespace and zero to two comments.
func (g *gen) trivia() {
	switch g.rnd.Intn(5) {
	case 0:
		g.b.WriteByte(' ')
	case 1:
		g.b.WriteString(" \t")
	case 2:
		g.b.WriteByte('\n')
	case 3:
		g.b.WriteString("\n\n")
	case 4:
		g.b.WriteString("\r\n")
	}
	for i, n := 0, g.rnd.Intn(3); i < n; i++ {
		g.comment()
		switch g.rnd.Intn(3) {
		case 0:
			g.b.WriteByte('\n')
		case 1:
			g.b.WriteByte(' ')
		case 2:
			g.b.WriteString("\n\n")
		}
	}
}

// comment emits one block comment with a random body. Bodies never
// contain '*' or '/', so they cannot close early.
func (g *gen) comment() {
	g.b.WriteString("/*")
	switch g.rnd.Intn(4) {
	case 0:
		g.b.WriteString(" " + g.words(1) + " ")
	case 1: // empty body
	case 2:
		g.b.WriteString("\n" + g.ws() + g.words(2) + "\n" + g.ws() + g.words(1) + "\n")
	case 3:
		g.b.WriteString(" " + g.uni() + " ")
	}
	g.b.WriteString("*/")
}

// words emits n random ASCII words joined by single spaces.
func (g *gen) words(n int) string {
	parts := make([]string, n)
	for i := range parts {
		parts[i] = words[g.rnd.Intn(len(words))]
	}
	return strings.Join(parts, " ")
}

// ws emits a random run of spaces and tabs.
func (g *gen) ws() string {
	return strings.Repeat([]string{" ", "  ", "\t", " \t", "    "}[g.rnd.Intn(5)], g.rnd.Intn(3))
}

// uni emits a Unicode documentation fragment.
func (g *gen) uni() string {
	return []string{"héllo", "世界", "😀", "日本語 text", "héllo 世界 😀"}[g.rnd.Intn(5)]
}

// typeSpec emits one type-specifier, referencing only earlier types.
func (g *gen) typeSpec(depth int) {
	if depth >= 2 || g.rnd.Intn(2) == 0 {
		if len(g.types) > 0 && g.rnd.Intn(2) == 0 {
			g.b.WriteString(g.types[g.rnd.Intn(len(g.types))])
		} else {
			g.b.WriteString(primitives[g.rnd.Intn(len(primitives))])
		}
		return
	}
	if g.rnd.Intn(2) == 0 {
		g.b.WriteString("list")
		g.trivia()
		g.typeSpec(depth + 1)
		return
	}
	g.b.WriteString("record")
	g.trivia()
	g.b.WriteByte('{')
	g.trivia()
	for i, n := 0, g.rnd.Intn(3); i < n; i++ {
		fmt.Fprintf(&g.b, "f%d", i)
		g.trivia()
		g.typeSpec(depth + 1)
		g.trivia()
		g.b.WriteString(";")
		g.trivia()
	}
	g.b.WriteByte('}')
}

// typeDecl emits one type declaration and records its name.
func (g *gen) typeDecl() {
	g.b.WriteString("type")
	g.trivia()
	name := fmt.Sprintf("t%d", g.nt)
	g.nt++
	g.b.WriteString(name)
	g.trivia()
	g.typeSpec(0)
	g.trivia()
	g.b.WriteString(";")
	g.types = append(g.types, name)
}

// procDecl emits one procedure declaration with a unique key.
func (g *gen) procDecl() {
	g.b.WriteString("procedure")
	g.trivia()
	g.b.WriteString(g.freshName("p", "procedure"))
	g.trivia()
	g.b.WriteByte('{')
	g.trivia()
	for i, n := 0, g.rnd.Intn(3); i < n; i++ {
		fmt.Fprintf(&g.b, "a%d", i)
		g.trivia()
		g.typeSpec(0)
		g.trivia()
		g.b.WriteString(";")
		g.trivia()
	}
	g.b.WriteByte('}')
	if g.rnd.Intn(2) == 0 {
		g.trivia()
		g.typeSpec(0)
	}
	g.trivia()
	g.b.WriteString(";")
}

// exceptionDecl emits one exception declaration with a unique key.
func (g *gen) exceptionDecl() {
	g.b.WriteString("exception")
	g.trivia()
	g.b.WriteString(g.freshName("e", "exception"))
	if g.rnd.Intn(2) == 0 {
		g.trivia()
		g.typeSpec(0)
	}
	g.trivia()
	g.b.WriteString(";")
}

// freshName returns an unused procedure or exception name for kind.
func (g *gen) freshName(prefix, kind string) string {
	for {
		name := fmt.Sprintf("%s%d", prefix, g.np)
		g.np++
		if !g.keys[syntax.Key(kind, name)] {
			g.keys[syntax.Key(kind, name)] = true
			return name
		}
	}
}
