package tool

import (
	"os"
	"reflect"
	"strings"
	"testing"
)

func TestGoDocumentation(t *testing.T) {
	t.Run("UnicodeRetained", func(t *testing.T) {
		// Unicode scalar values survive extraction and normalization
		// byte for byte, and positions are byte columns.
		doc := goFixture(t, "doc_unicode.go")
		var proc *GoDecl
		for _, decl := range doc.Decls {
			if decl.Name == "UnicodeProc" {
				proc = decl
			}
		}
		if proc == nil {
			t.Fatal("no UnicodeProc declaration")
		}
		want := "Résumé: ünïcödé — 日本語のドキュメント — 🎉.\nSee also: café au lait."
		if proc.Doc.Retained != want {
			t.Errorf("retained doc = %q, want %q", proc.Doc.Retained, want)
		}
		if dir := oneDir(t, proc); dir.Kind != ProcedureDir || dir.Wire != "unicode_proc" {
			t.Errorf("directive = %+v", dir)
		}
		if dir := oneDir(t, proc); dir.Pos.Line != 7 || dir.Pos.Column != 4 {
			t.Errorf("directive at %v, want 7:4", dir.Pos)
		}
		// Byte columns: the '日' rune of the Unicode doc line is a
		// three-byte character; the position of the line's end marker
		// must count bytes, not runes.
		src, err := os.ReadFile("testdata/doc_unicode.go")
		if err != nil {
			t.Fatal(err)
		}
		idx := strings.Index(string(src), "🎉")
		if idx < 0 {
			t.Fatal("emoji not found in fixture")
		}
		p := doc.Position(idx)
		lineStart := strings.LastIndex(string(src[:idx]), "\n") + 1
		if p.Column != idx-lineStart+1 || p.Line != 6 {
			t.Errorf("Position of emoji = %v (line %d), want line 6, column %d", p, p.Line, idx-lineStart+1)
		}
	})

	t.Run("TagsAndProseRetained", func(t *testing.T) {
		// Non-InterCall tags and prose stay in the retained doc; only
		// the directive lines are removed.
		src := "package p\n\n// A procedure with prose.\n// Deprecated: use another one.\n// @intercall procedure f\n// @param a its parameter\n// Trailing prose.\nfunc F(ctx context.Context, a int) error\n"
		d := goDecl(t, src, "F")
		want := "A procedure with prose.\nDeprecated: use another one.\nTrailing prose."
		if d.Doc.Retained != want {
			t.Errorf("retained doc = %q, want %q", d.Doc.Retained, want)
		}
	})

	t.Run("Normalization", func(t *testing.T) {
		for _, tc := range []struct {
			name, decl, src, want string
		}{
			{"Dedent", "X", "package p\n\n//\tfirst line\n//\tsecond line\nvar X int\n",
				"first line\nsecond line"},
			{"InteriorBlank", "X", "package p\n\n// a\n//\n//\n// b\nvar X int\n",
				"a\n\nb"},
			{"LeadingTrailingBlank", "X", "package p\n\n//\n// a\n//\nvar X int\n",
				"a"},
			{"TrailingSpaceTrimmed", "X", "package p\n\n// a   \nvar X int\n",
				"a"},
			{"DirectiveMiddle", "F", "package p\n\n// one\n// @intercall procedure f\n// three\nfunc F() error\n",
				"one\nthree"},
			{"DirectiveOnly", "F", "package p\n\n// @intercall procedure f\nfunc F() error\n",
				""},
		} {
			t.Run(tc.name, func(t *testing.T) {
				d := goDecl(t, tc.src, tc.decl)
				if d.Doc.Retained != tc.want {
					t.Errorf("retained doc = %q, want %q", d.Doc.Retained, tc.want)
				}
			})
		}
	})

	t.Run("GoDirectivesDropped", func(t *testing.T) {
		// Lines that go/ast.CommentGroup.Text drops as comment
		// directives are neither directives nor retained prose; the
		// check is exact: "// line:1" with a space is ordinary prose.
		src := "package p\n\n//go:generate echo hi\n//lin:3\n// line:1 stays as prose\n// @intercall procedure f\n// real prose\nfunc F() error\n"
		d := goDecl(t, src, "F")
		if d.Doc.Retained != "line:1 stays as prose\nreal prose" {
			t.Errorf("retained doc = %q, want %q", d.Doc.Retained, "line:1 stays as prose\nreal prose")
		}
		if got := dirs(t, d); len(got) != 1 || got[0].Kind != ProcedureDir {
			t.Errorf("directives = %+v", got)
		}
	})

	t.Run("TerminatorRejection", func(t *testing.T) {
		goFixtureErr(t, "doc_terminator.go", "5:4: retained documentation contains '*/'")
		for _, tc := range []struct {
			name, src string
			line, col int
		}{
			{"ParamText", "package p\n\n// @intercall procedure f\n// @param a text with */ inside\nfunc F(ctx context.Context, a int) error\n", 4, 23},
			{"ReturnText", "package p\n\n// @intercall procedure f\n// @return a */ result\nfunc F() (int, error)\n", 4, 14},
			{"FieldDoc", "package p\n\n// @intercall type t\ntype T struct {\n\t// X */ doc\n\tX int\n}\n", 5, 7},
		} {
			t.Run(tc.name, func(t *testing.T) {
				ge := goErr(t, tc.src)
				if ge.Msg != "retained documentation contains '*/'" {
					t.Errorf("error = %q", ge.Msg)
				}
				if ge.Pos.Line != tc.line || ge.Pos.Column != tc.col {
					t.Errorf("error at %v, want %d:%d", ge.Pos, tc.line, tc.col)
				}
			})
		}
	})

	t.Run("BlockCommentDirective", func(t *testing.T) {
		// A single-line block comment is a logical line; a directive
		// inside it is recognized.
		src := "package p\n\n/* @intercall procedure f */\nfunc F() error\n"
		d := goDecl(t, src, "F")
		if dir := oneDir(t, d); dir.Kind != ProcedureDir || dir.Pos.Line != 3 || dir.Pos.Column != 4 {
			t.Errorf("directive = %+v", dir)
		}
	})

	t.Run("StarDecoratedBlockIsProse", func(t *testing.T) {
		// go/ast.CommentGroup.Text does not strip '*' decoration, so a
		// decorated directive line is ordinary retained prose.
		src := "package p\n\n/*\n * @intercall procedure f\n */\nfunc F() error\n"
		d := goDecl(t, src, "F")
		if got := dirs(t, d); len(got) != 0 {
			t.Errorf("directives = %+v, want none", got)
		}
		if d.Doc.Retained != "* @intercall procedure f" {
			t.Errorf("retained doc = %q", d.Doc.Retained)
		}
	})

	t.Run("MixedCommentGroup", func(t *testing.T) {
		// A doc group may mix line and block comments; the logical
		// lines are joined in source order.
		src := "package p\n\n// first\n/* second\nthird */\n// @intercall procedure f\nfunc F() error\n"
		d := goDecl(t, src, "F")
		if d.Doc.Retained != "first\n second\nthird" {
			t.Errorf("retained doc = %q", d.Doc.Retained)
		}
	})

	t.Run("FieldDocs", func(t *testing.T) {
		// Field docs are retained documentation without a directive
		// grammar; embedded fields have no name.
		src := "package p\n\n// @intercall type t\n// Embedded is retained.\ntype T struct {\n\t// A field with a doc.\n\tA int\n\t// Doc before the embedded field.\n\tN\n}\n"
		d := goDecl(t, src, "T")
		if len(d.Fields) != 2 {
			t.Fatalf("T has %d fields, want 2", len(d.Fields))
		}
		if f := d.Fields[0]; f.Name != "A" || f.Doc != "A field with a doc." {
			t.Errorf("field 0 = %+v", f)
		}
		if f := d.Fields[1]; f.Name != "" || f.Doc != "Doc before the embedded field." {
			t.Errorf("field 1 = %+v", f)
		}
	})

	t.Run("FileDocNotChecked", func(t *testing.T) {
		// The package clause doc is not a package-level declaration;
		// InterCall-looking lines there are ordinary prose.
		src := "// @intercall frobnicate\n// package doc\npackage p\n\nvar X int\n"
		doc, err := ParseGoSource("x.go", []byte(src))
		if err != nil {
			t.Fatalf("ParseGoSource: %v", err)
		}
		if len(doc.Decls) != 1 || doc.Decls[0].Doc != nil {
			t.Errorf("decls = %+v", doc.Decls)
		}
	})

	t.Run("LineImmunity", func(t *testing.T) {
		// A //line directive before the doc comment rewrites the
		// scanner positions, but the diagnostic stays at the physical
		// position of the malformed directive.
		goFixtureErr(t, "doc_line.go", "6:4: malformed @intercall procedure directive: expected at most one wire name")

		// A //line line inside a doc comment is dropped like any other
		// comment directive; the directive lines around it are both
		// recognized, and the duplicate reports the physical position
		// of the second one.
		src := "package p\n\n// @intercall procedure foo\n//line other.go:1\n// @intercall procedure bar\nfunc F(ctx context.Context) error\n"
		ge := goErr(t, src)
		if ge.Msg != "duplicate @intercall procedure directive" {
			t.Errorf("error = %q", ge.Msg)
		}
		if ge.Pos.Line != 5 || ge.Pos.Column != 4 {
			t.Errorf("duplicate at %v, want 5:4", ge.Pos)
		}
	})

	t.Run("LocalDeclarationsNotChecked", func(t *testing.T) {
		// Directive checking covers package-level declarations only; a
		// local variable's doc with directive-looking lines is prose.
		src := "package p\n\nfunc F() error {\n\t// @intercall frobnicate\n\tvar x int\n\t_ = x\n\treturn nil\n}\n"
		if _, err := ParseGoSource("x.go", []byte(src)); err != nil {
			t.Fatalf("ParseGoSource: %v", err)
		}
	})
}

func TestGeneratedMarker(t *testing.T) {
	t.Run("Handwritten", func(t *testing.T) {
		doc := goFixture(t, "directives_valid.go")
		if doc.Generated || doc.IntercallGenerated {
			t.Errorf("handwritten fixture classified generated: %v %v", doc.Generated, doc.IntercallGenerated)
		}
	})

	t.Run("GenericMarker", func(t *testing.T) {
		// Go's standard generated-file marker is recognized for any
		// generator.
		src := "// Code generated by protoc; DO NOT EDIT.\n\npackage p\n\nvar X int\n"
		doc, err := ParseGoSource("x.go", []byte(src))
		if err != nil {
			t.Fatalf("ParseGoSource: %v", err)
		}
		if !doc.Generated || doc.IntercallGenerated {
			t.Errorf("flags = %v %v, want generated only", doc.Generated, doc.IntercallGenerated)
		}
	})

	t.Run("IntercallMarker", func(t *testing.T) {
		src := "// Code generated by intercall-go; DO NOT EDIT.\n\npackage p\n\nvar X int\n"
		doc, err := ParseGoSource("x.go", []byte(src))
		if err != nil {
			t.Fatalf("ParseGoSource: %v", err)
		}
		if !doc.Generated || !doc.IntercallGenerated {
			t.Errorf("flags = %v %v, want both", doc.Generated, doc.IntercallGenerated)
		}
	})

	t.Run("MarkerNotFirstLine", func(t *testing.T) {
		// The trust marker must be the file's first line; a marker
		// after the package clause is not the standard marker either.
		src := "package p\n\n// Code generated by intercall-go; DO NOT EDIT.\n\nvar X int\n"
		doc, err := ParseGoSource("x.go", []byte(src))
		if err != nil {
			t.Fatalf("ParseGoSource: %v", err)
		}
		if doc.Generated || doc.IntercallGenerated {
			t.Errorf("flags = %v %v, want none", doc.Generated, doc.IntercallGenerated)
		}
	})

	t.Run("CaseSensitive", func(t *testing.T) {
		// The standard marker is case-sensitive.
		src := "// code generated by tool; do not edit.\n\npackage p\n"
		doc, err := ParseGoSource("x.go", []byte(src))
		if err != nil {
			t.Fatalf("ParseGoSource: %v", err)
		}
		if doc.Generated || doc.IntercallGenerated {
			t.Errorf("flags = %v %v, want none", doc.Generated, doc.IntercallGenerated)
		}
	})

	t.Run("MarkerInBlockComment", func(t *testing.T) {
		// go/ast.IsGenerated checks every comment before the package
		// clause, including block comment lines.
		src := "/* Header.\n// Code generated by tool; DO NOT EDIT.\n*/\npackage p\n"
		doc, err := ParseGoSource("x.go", []byte(src))
		if err != nil {
			t.Fatalf("ParseGoSource: %v", err)
		}
		if !doc.Generated || doc.IntercallGenerated {
			t.Errorf("flags = %v %v, want generated only", doc.Generated, doc.IntercallGenerated)
		}
	})

	t.Run("UnmarkedSemanticConstantIsOrdinary", func(t *testing.T) {
		// In an unmarked handwritten file a _intercallSemantic
		// constant is ordinary Go, never generated metadata.
		src := "package p\n\nconst _intercallSemantic = \"aGVsbG8\"\n"
		doc, err := ParseGoSource("x.go", []byte(src))
		if err != nil {
			t.Fatalf("ParseGoSource: %v", err)
		}
		if doc.IntercallGenerated || doc.Generated {
			t.Errorf("flags = %v %v, want none", doc.Generated, doc.IntercallGenerated)
		}
		if len(doc.Decls) != 1 {
			t.Fatalf("%d declarations, want 1", len(doc.Decls))
		}
		d := doc.Decls[0]
		if d.Kind != GoConst || d.Name != "_intercallSemantic" || len(d.Names) != 1 {
			t.Errorf("decl = %+v", d)
		}
	})

	t.Run("TrustedFixture", func(t *testing.T) {
		// The checked-in generated codec fixture is the trust boundary:
		// its machine lines parse as @intercall type directives on the
		// generated type declarations, with empty retained docs.
		src, err := os.ReadFile("fixture/codec_gen.go")
		if err != nil {
			t.Fatal(err)
		}
		doc, err := ParseGoSource("fixture/codec_gen.go", src)
		if err != nil {
			t.Fatalf("ParseGoSource: %v", err)
		}
		if !doc.Generated || !doc.IntercallGenerated {
			t.Errorf("flags = %v %v, want both", doc.Generated, doc.IntercallGenerated)
		}
		wantWires := []string{"user_id", "name", "point", "pixel", "empty", "names", "matrix", "blob", "customer_id"}
		var got []string
		for _, d := range doc.Decls {
			if d.Kind != GoType || d.Doc == nil {
				continue
			}
			if d.Doc.Retained != "" {
				t.Errorf("%s retained doc = %q, want empty", d.Name, d.Doc.Retained)
			}
			dir := oneDir(t, d)
			if dir.Kind != TypeDir || dir.Wire == "" {
				t.Errorf("%s machine line = %+v", d.Name, dir)
			}
			got = append(got, dir.Wire)
		}
		if strings.Join(got, ",") != strings.Join(wantWires, ",") {
			t.Fatalf("machine wires = %v, want %v", got, wantWires)
		}
		// Physical position of the first machine line.
		var userID *GoDecl
		for _, d := range doc.Decls {
			if d.Name == "UserID" {
				userID = d
			}
		}
		if userID == nil {
			t.Fatal("no UserID declaration")
		}
		if dir := oneDir(t, userID); dir.Pos.Line != 20 || dir.Pos.Column != 4 {
			t.Errorf("user_id machine line at %v, want 20:4", dir.Pos)
		}
	})

	t.Run("TrustBoundaryHandwrittenDirectives", func(t *testing.T) {
		// Handwritten @intercall lines in an unmarked file are source
		// directives; they never form generated metadata. A malformed
		// machine-looking line is a directive error, not prose.
		src := "package p\n\n// @intercall type\nvar _ int\n"
		ge := goErr(t, src)
		if !strings.Contains(ge.Msg, "misplaced") {
			t.Errorf("error = %q, want misplaced directive", ge.Msg)
		}
	})
}

// TestGeneratedDirectiveBoundary pins the two generated-file boundaries
// of SPEC.md "Package discovery and selection": ordinary third-party
// files recognized by Go's standard generated-file marker are inert for
// source directive selection — they supply no directives and no
// documentation, and source-like prose never aborts parsing — while the
// exact InterCall generated-file marker remains the metadata trust
// boundary, so a marked file stays full metadata input.
func TestGeneratedDirectiveBoundary(t *testing.T) {
	t.Run("ThirdPartyGeneratedIsInert", func(t *testing.T) {
		// A protoc-style generated file may contain InterCall-looking
		// lines, a "*/" terminator, or any prose; none of it is parsed
		// as directives or documentation, and none of it errors.
		src := "// Code generated by protoc-gen-x; DO NOT EDIT.\n\npackage p\n\n// @intercall frobnicate\nvar X int\n\n// @intercall procedure a b\nfunc F() error { return nil }\n\n// Source-like prose.\n// @intercall exception gen_only\ntype T struct {\n\t// Field prose with */ terminator.\n\tA int\n}\n"
		doc, err := ParseGoSource("x.go", []byte(src))
		if err != nil {
			t.Fatalf("ParseGoSource: %v", err)
		}
		if !doc.Generated || doc.IntercallGenerated {
			t.Errorf("flags = %v %v, want generated only", doc.Generated, doc.IntercallGenerated)
		}
		if len(doc.Decls) != 3 {
			t.Fatalf("%d declarations, want 3", len(doc.Decls))
		}
		for _, d := range doc.Decls {
			if d.Doc != nil {
				t.Errorf("%s has a doc comment, want none", d.Name)
			}
		}
		if len(doc.Decls[2].Fields) != 1 || doc.Decls[2].Fields[0].Doc != "" {
			t.Errorf("inert field docs = %+v", doc.Decls[2].Fields)
		}
	})

	t.Run("ExactIntercallMarkerStaysMetadata", func(t *testing.T) {
		// The exact InterCall marker keeps the file a metadata input:
		// its @intercall lines are directives, and a malformed one is
		// still an error rather than inert prose.
		src := "// Code generated by intercall-go; DO NOT EDIT.\n\npackage p\n\n// @intercall frobnicate\nvar X int\n"
		ge := goErr(t, src)
		if !strings.Contains(ge.Msg, "unknown @intercall directive '@intercall frobnicate'") {
			t.Errorf("error = %q, want the unknown-directive error", ge.Msg)
		}
		// A valid machine line in a marked file is recognized as a
		// directive with its wire name.
		marked := "// Code generated by intercall-go; DO NOT EDIT.\n\npackage p\n\n// @intercall type user_id\ntype UserID uint64\n"
		doc, err := ParseGoSource("x.go", []byte(marked))
		if err != nil {
			t.Fatalf("ParseGoSource: %v", err)
		}
		if !doc.IntercallGenerated {
			t.Fatal("marker not recognized")
		}
		if dir := oneDir(t, doc.Decls[0]); dir.Kind != TypeDir || dir.Wire != "user_id" {
			t.Errorf("machine line = %+v", dir)
		}
	})

	t.Run("GeneratedFileSuppliesNoExceptions", func(t *testing.T) {
		// End to end: a package whose third-party generated file
		// carries a valid-looking exception directive and a malformed
		// directive exports cleanly; the generated file supplies no
		// exception and its prose is ignored, while the handwritten
		// sentinel belongs to the interface.
		dir := writeFixture(t, map[string]string{
			"go.mod": "module example.com/boundary\n\ngo 1.26.5\n",
			"prov/prov.go": `// Package prov has a handwritten sentinel.
package prov

import "errors"

// @intercall exception handwritten
var ErrHandwritten = errors.New("handwritten")
`,
			"prov/gen.go": `// Code generated by protoc-gen-y; DO NOT EDIT.

// Package prov shares the package with a generated file.
package prov

import "errors"

// @intercall exception gen_only
var ErrGenOnly = errors.New("gen_only")

// @intercall frobnicate
var X int
`,
			"out/out.go": `// Package out is the output target.
package out

func Helper() {}
`,
		})
		res, err := discover(t, dir, []string{"./prov"}, nil, nil, "out")
		if err != nil {
			t.Fatalf("discover: %v", err)
		}
		model, err := MapExport(res, "example.com/boundary/out")
		if err != nil {
			t.Fatalf("MapExport: %v", err)
		}
		var wires []string
		for _, w := range excWires(model) {
			if w != "procedure_not_found" && w != "invalid_arguments" && w != "internal_exception" {
				wires = append(wires, w)
			}
		}
		if !reflect.DeepEqual(wires, []string{"handwritten"}) {
			t.Errorf("exceptions = %v, want [handwritten]", wires)
		}
	})
}
