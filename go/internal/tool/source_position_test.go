package tool

import (
	"bytes"
	"go/ast"
	"go/importer"
	"go/parser"
	"go/token"
	"go/types"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"golang.org/x/tools/go/packages"
)

// This file tests the source identity and position contracts of SPEC.md
// "Diagnostics": physical positions resolved through the load FileSet
// (never through an unrelated parse FileSet), syntax/documents paired
// by filename or file identity with an invariant break, canonical
// logical paths for ordinary and external compiler-generated sources,
// and the file:line / file:line:column position grammar.
//
// The pipeline tests use temporary modules whose packages load after
// every standard-library dependency, so their positions carry high
// token.FileSet bases; a conversion through a per-file parse FileSet
// would clamp every offset to the end of its document. Fixtures are
// written only under t.TempDir.

// sourcePosFixture is the module of the cross-FileSet position tests: a
// dependency package, a multi-file explicit package whose first file
// carries a //line directive, and the output package.
var sourcePosFixture = map[string]string{
	"go.mod": "module example.com/pos\n\ngo 1.26.5\n",
	"dep/dep.go": `// Package dep provides reachable named types.
package dep

// Thing is a dependency named type.
// @intercall type thing
type Thing struct {
	V string
}

// Broken carries a field doc whose retained text terminates a comment.
// @intercall type broken
type Broken struct {
	// doc with */ terminator
	X string
}
`,
	"vals/one.go": `//line synthetics.go:1000
package vals

import (
	"context"

	"example.com/pos/dep"
)

// User is a named type.
// @intercall type user
type User struct {
	ID string
}

// UseUser maps a user.
// @intercall procedure use_user
func UseUser(ctx context.Context, u User) error { return nil }

// UseThing maps a dependency type.
// @intercall procedure use_thing
func UseThing(ctx context.Context, t dep.Thing) error { return nil }
`,
	"vals/two.go": `package vals

import (
	"context"

	"example.com/pos/dep"
)

//line mapped.go:500

// @intercall procedure bad_map
func BadMap(ctx context.Context, m map[string]int) error { return nil }

// @intercall procedure bad_doc
func BadDoc(ctx context.Context, b dep.Broken) error { return nil }
`,
	"out/out.go": `// Package out is the output package.
package out
`,
}

// wantPosition computes the one-based physical line and byte column of
// the first occurrence of substr in src, using only the raw bytes.
func wantPosition(t *testing.T, src []byte, substr string) Position {
	t.Helper()
	idx := bytes.Index(src, []byte(substr))
	if idx < 0 {
		t.Fatalf("substring %q not found in source", substr)
	}
	line := 1 + bytes.Count(src[:idx], []byte("\n"))
	start := bytes.LastIndexByte(src[:idx], '\n') + 1
	return Position{Line: line, Column: idx - start + 1}
}

// wantErrorPosition fails unless err is an *Error at the exact physical
// line and byte column of the first occurrence of substr in src.
func wantErrorPosition(t *testing.T, err error, filename string, src []byte, substr string, contains ...string) {
	t.Helper()
	te, ok := err.(*Error)
	if !ok {
		t.Fatalf("error = %T %v, want a *tool.Error", err, err)
	}
	want := wantPosition(t, src, substr)
	if te.Filename != filename {
		t.Errorf("Filename = %q, want %q", te.Filename, filename)
	}
	if te.Pos.Line != want.Line || te.Pos.Column != want.Column {
		t.Errorf("position = %d:%d, want %d:%d", te.Pos.Line, te.Pos.Column, want.Line, want.Column)
	}
	for _, s := range contains {
		if !strings.Contains(te.Msg, s) {
			t.Errorf("message = %q, want it to contain %q", te.Msg, s)
		}
	}
}

// TestSplitFilePositionForms covers the position grammar of SPEC.md
// "Diagnostics": both file:line and file:line:column forms, numeric
// suffixes scanned from the right so colons in filenames are preserved,
// and a missing column defaulting to 1.
func TestSplitFilePositionForms(t *testing.T) {
	for _, tc := range []struct {
		pos       string
		file      string
		line, col int
		ok        bool
	}{
		{"file.go:10:20", "file.go", 10, 20, true},
		{"file.go:10", "file.go", 10, 1, true},
		{"/abs/path/file.go:3:9", "/abs/path/file.go", 3, 9, true},
		{"/abs/path/file.go:3", "/abs/path/file.go", 3, 1, true},
		{"we:ird/a:b.go:7:3", "we:ird/a:b.go", 7, 3, true},
		{"we:ird/a:b.go:7", "we:ird/a:b.go", 7, 1, true},
		{"a:b:12:34", "a:b", 12, 34, true},
		{"a:b:12", "a:b", 12, 1, true},
		{"file.go:10:20:30", "file.go:10", 20, 30, true},
		{"12:34", "12", 34, 1, true},
		{"file.go", "", 0, 0, false},
		{"file.go:x", "", 0, 0, false},
		{"file.go:10:", "", 0, 0, false},
		{":5:6", "", 0, 0, false},
		{"", "", 0, 0, false},
	} {
		file, line, col, ok := splitFilePos(tc.pos)
		if ok != tc.ok || file != tc.file || line != tc.line || col != tc.col {
			t.Errorf("splitFilePos(%q) = (%q, %d, %d, %v), want (%q, %d, %d, %v)",
				tc.pos, file, line, col, ok, tc.file, tc.line, tc.col, tc.ok)
		}
	}
}

// TestPackageErrorLogicalPath covers the canonical logical path of
// ordinary package diagnostics: the slash-normalized package-relative
// path under the canonical import path, including colon-bearing file
// names, the line-only form, and the no-position fallback.
func TestPackageErrorLogicalPath(t *testing.T) {
	p := &ExplicitPackage{Path: "example.com/proc", Dir: "/work/proc"}

	t.Run("SubdirectoryFile", func(t *testing.T) {
		e := packageError(p, packages.Error{Pos: "/work/proc/sub/a.go:7:3", Msg: "boom"})
		want := &Error{Filename: "example.com/proc/sub/a.go", Pos: Position{Line: 7, Column: 3}, Msg: "boom"}
		if !reflect.DeepEqual(e, want) {
			t.Errorf("packageError = %+v, want %+v", e, want)
		}
	})

	t.Run("ColonInFilename", func(t *testing.T) {
		e := packageError(p, packages.Error{Pos: "/work/proc/we:ird/a:b.go:2:1", Msg: "boom"})
		if e.Filename != "example.com/proc/we:ird/a:b.go" || e.Pos.Line != 2 || e.Pos.Column != 1 {
			t.Errorf("packageError = %+v", e)
		}
	})

	t.Run("LineOnlyDefaultsColumnOne", func(t *testing.T) {
		e := packageError(p, packages.Error{Pos: "/work/proc/a.go:9", Msg: "boom"})
		if e.Filename != "example.com/proc/a.go" || e.Pos.Line != 9 || e.Pos.Column != 1 {
			t.Errorf("packageError = %+v", e)
		}
	})

	t.Run("RelativePositionFile", func(t *testing.T) {
		// A position file that is not absolute resolves against the
		// package directory before the logical-path rendering.
		e := packageError(p, packages.Error{Pos: "sub/a.go:4:5", Msg: "boom"})
		if e.Filename != "example.com/proc/sub/a.go" || e.Pos.Line != 4 || e.Pos.Column != 5 {
			t.Errorf("packageError = %+v", e)
		}
	})

	t.Run("NoPosition", func(t *testing.T) {
		e := packageError(p, packages.Error{Msg: "boom"})
		if e.Filename != "example.com/proc" || e.Pos != (Position{Line: 1, Column: 1}) {
			t.Errorf("packageError = %+v", e)
		}
	})

	t.Run("UnparsablePosition", func(t *testing.T) {
		// A position whose final suffix is not numeric is not a source
		// span; the diagnostic falls back to the package at 1:1.
		e := packageError(p, packages.Error{Pos: "/work/proc/a.go:notanumber", Msg: "boom"})
		if e.Filename != "example.com/proc" || e.Pos != (Position{Line: 1, Column: 1}) {
			t.Errorf("packageError = %+v", e)
		}
	})
}

// TestPackageErrorExternalGeneratedPath covers the canonical logical
// path of external compiler-generated sources — the import path plus
// ".intercall-generated/<base-name>" — and the package-load invariant
// that duplicate external base names fail rather than conflate.
func TestPackageErrorExternalGeneratedPath(t *testing.T) {
	p := &ExplicitPackage{Path: "example.com/proc", Dir: "/work/proc"}

	t.Run("CgoGeneratedFile", func(t *testing.T) {
		e := packageError(p, packages.Error{Pos: "/tmp/go-build123/b001/_cgo_gotypes.go:4:2", Msg: "boom"})
		want := &Error{
			Filename: "example.com/proc/.intercall-generated/_cgo_gotypes.go",
			Pos:      Position{Line: 4, Column: 2},
			Msg:      "boom",
		}
		if !reflect.DeepEqual(e, want) {
			t.Errorf("packageError = %+v, want %+v", e, want)
		}
	})

	t.Run("ExternalBasenameOnly", func(t *testing.T) {
		// The external path is rendered by base name: the physical build
		// directory is never exposed.
		e := packageError(p, packages.Error{Pos: "/tmp/go-build456/b007/generated.go:1:1", Msg: "boom"})
		if e.Filename != "example.com/proc/.intercall-generated/generated.go" {
			t.Errorf("Filename = %q", e.Filename)
		}
	})

	t.Run("DuplicateExternalBases", func(t *testing.T) {
		err := checkExternalBases("example.com/proc", "/work/proc", []string{
			"/work/proc/a.go",
			"/tmp/build/b001/_cgo_gotypes.go",
			"/tmp/build/b002/_cgo_gotypes.go",
		})
		if err == nil {
			t.Fatal("checkExternalBases succeeded, want the invariant error")
		}
		te, ok := err.(*Error)
		if !ok {
			t.Fatalf("error = %T, want an *Error", err)
		}
		if te.Filename != "example.com/proc" || te.Pos != (Position{Line: 1, Column: 1}) {
			t.Errorf("invariant error position = %+v", te)
		}
		for _, s := range []string{"package-load invariant error", "_cgo_gotypes.go", "conflate"} {
			if !strings.Contains(te.Msg, s) {
				t.Errorf("message = %q, want it to contain %q", te.Msg, s)
			}
		}
	})

	t.Run("DistinctExternalBases", func(t *testing.T) {
		if err := checkExternalBases("example.com/proc", "/work/proc", []string{
			"/work/proc/a.go",
			"/tmp/build/b001/_cgo_gotypes.go",
			"/tmp/build/b001/_cgo_import.go",
		}); err != nil {
			t.Errorf("checkExternalBases = %v, want nil", err)
		}
	})

	t.Run("SameBaseUnderPackageDirectory", func(t *testing.T) {
		// Files under the package directory are distinct by their
		// relative path, so an equal base name is not a conflation.
		if err := checkExternalBases("example.com/proc", "/work/proc", []string{
			"/work/proc/a.go",
			"/work/proc/sub/a.go",
		}); err != nil {
			t.Errorf("checkExternalBases = %v, want nil", err)
		}
	})
}

// pairFixture is a two-file synthetic package whose load FileSet also
// contains an unrelated padding file, so every package position carries
// a base far above any per-file parse FileSet. The a.go file holds a
// provider that both reaches the b.go named type and carries a bad map
// parameter, whose mapping error must land in a.go at the exact
// physical position.
var pairFixture = map[string]string{
	"a.go": `package synth

import "context"

// @intercall procedure use
func Use(ctx context.Context, t BType, m map[string]int) error { return nil }
`,
	"b.go": `package synth

// BType is a named type of the second file.
// @intercall type btype
type BType struct {
	X string
}
`,
}

// synthPairPackage builds the two-file synthetic package of
// pairFixture. The load FileSet contains an unrelated padding file
// first, and the returned ExplicitPackage deliberately lists its
// compiled files in an order different from the syntax order, so
// positional pairing would attach every file's document to the
// adjacent file.
func synthPairPackage(t *testing.T, docs map[string]*Document) *ExplicitPackage {
	t.Helper()
	names := []string{"a.go", "b.go"}
	fset := token.NewFileSet()
	// An unrelated file inflates the bases of the package files, so
	// positions can never coincide with a per-file parse FileSet's
	// offsets.
	if _, err := parser.ParseFile(fset, "padding.go", []byte("// "+strings.Repeat("x", 10000)+"\npackage padding\n"), parser.ParseComments); err != nil {
		t.Fatalf("parsing padding: %v", err)
	}
	var afs []*ast.File
	for _, n := range names {
		af, err := parser.ParseFile(fset, n, []byte(pairFixture[n]), parser.ParseComments|parser.SkipObjectResolution)
		if err != nil {
			t.Fatalf("parsing %s: %v", n, err)
		}
		afs = append(afs, af)
	}
	info := &types.Info{
		Types: make(map[ast.Expr]types.TypeAndValue),
		Defs:  make(map[*ast.Ident]types.Object),
		Uses:  make(map[*ast.Ident]types.Object),
	}
	tpkg, err := (&types.Config{Importer: importer.Default()}).Check("example.com/synth", fset, afs, info)
	if err != nil {
		t.Fatalf("type-checking fixture: %v", err)
	}
	if docs == nil {
		docs = make(map[string]*Document, len(names))
		for _, n := range names {
			doc, err := ParseGoSource(logicalFilePath("example.com/synth", "", n), []byte(pairFixture[n]))
			if err != nil {
				t.Fatalf("parsing document %s: %v", n, err)
			}
			docs[n] = doc
		}
	}
	return &ExplicitPackage{
		Path:  "example.com/synth",
		Name:  tpkg.Name(),
		Dir:   "",
		files: []string{"b.go", "a.go"}, // deliberate mismatch with Syntax order
		pkg: &packages.Package{
			ID:              "example.com/synth",
			PkgPath:         "example.com/synth",
			Name:            tpkg.Name(),
			Fset:            fset,
			Syntax:          afs,
			Types:           tpkg,
			TypesInfo:       info,
			CompiledGoFiles: names,
		},
		docs: docs,
	}
}

// synthProvider builds the provider record of the tagged function of
// one synthetic package, looking up its function declaration in the
// syntax and its declaration record in the package documents.
func synthProvider(t *testing.T, exp *ExplicitPackage, name string) *Provider {
	t.Helper()
	var fn *ast.FuncDecl
	for _, af := range exp.pkg.Syntax {
		for _, d := range af.Decls {
			if fd, ok := d.(*ast.FuncDecl); ok && fd.Name.Name == name {
				fn = fd
			}
		}
	}
	if fn == nil {
		t.Fatalf("no function %q in fixture", name)
	}
	var decl *GoDecl
	for _, doc := range exp.docs {
		for _, d := range doc.Decls {
			if d.Kind == GoFunc && d.Name == name {
				decl = d
			}
		}
	}
	if decl == nil {
		t.Fatalf("no declaration record for %q", name)
	}
	return &Provider{Pkg: exp, Name: name, Func: fn, Doc: decl}
}

// TestPackageSyntaxDocumentPairingByFilename covers the pairing of
// package syntax with its parsed documents by filename or file
// identity: a mapping diagnostic in one file of a multi-file package
// lands in that exact file at the exact physical position even when
// the compiled-file order differs from the syntax order, and a syntax
// file without a parsed document fails the invariant instead of
// borrowing its neighbor's document.
func TestPackageSyntaxDocumentPairingByFilename(t *testing.T) {
	t.Run("PairingByFilenameNotPosition", func(t *testing.T) {
		exp := synthPairPackage(t, nil)
		providers := []*Provider{synthProvider(t, exp, "Use")}
		_, err := MapValues(providers, "")
		src := []byte(pairFixture["a.go"])
		wantErrorPosition(t, err, "example.com/synth/a.go", src, "map[string]int", "map types are not wire values")
	})

	t.Run("MissingDocumentFailsInvariant", func(t *testing.T) {
		// The document of b.go is absent: the mapping reaches BType in
		// b.go and must fail the invariant rather than pair the syntax
		// with an adjacent file's document.
		docs := make(map[string]*Document)
		doc, err := ParseGoSource("a.go", []byte(pairFixture["a.go"]))
		if err != nil {
			t.Fatalf("parsing document a.go: %v", err)
		}
		docs["a.go"] = doc
		exp := synthPairPackage(t, docs)
		providers := []*Provider{synthProvider(t, exp, "Use")}
		_, err = MapValues(providers, "")
		if err == nil {
			t.Fatal("mapping succeeded, want the document-pairing invariant error")
		}
		for _, s := range []string{"internal error: no parsed document for file b.go", "example.com/synth"} {
			if !strings.Contains(err.Error(), s) {
				t.Errorf("error = %q, want it to contain %q", err.Error(), s)
			}
		}
	})
}

// TestMappingPhysicalPositionsAcrossFileSets covers mapping diagnostics
// of a real discovery load: the error points at the exact physical line
// and byte column of the offending node in the exact file, despite high
// load FileSet bases, a preceding //line directive, and an adjacent
// file loaded earlier.
func TestMappingPhysicalPositionsAcrossFileSets(t *testing.T) {
	dir := writeFixture(t, sourcePosFixture)
	res, err := discover(t, dir, []string{"./vals"}, []string{"example.com/pos/vals.BadMap"}, nil, "out")
	if err != nil {
		t.Fatalf("discover: %v", err)
	}
	_, err = MapValues(res.Providers, "example.com/pos/out")
	src, rerr := os.ReadFile(filepath.Join(dir, "vals", "two.go"))
	if rerr != nil {
		t.Fatalf("reading fixture: %v", rerr)
	}
	wantErrorPosition(t, err, "example.com/pos/vals/two.go", src, "map[string]int", "map types are not wire values")
}

// TestNamedTypeRecordedPositionAcrossFileSets covers the recorded
// GoDecl.Pos / NamedType.Pos of reachable named types of a real load:
// the declaration position is the exact physical line and byte column
// of the type name in the declaring file, even when a //line directive
// precedes it.
func TestNamedTypeRecordedPositionAcrossFileSets(t *testing.T) {
	dir := writeFixture(t, sourcePosFixture)
	res, err := discover(t, dir, []string{"./vals"}, []string{
		"example.com/pos/vals.UseUser",
		"example.com/pos/vals.UseThing",
	}, nil, "out")
	if err != nil {
		t.Fatalf("discover: %v", err)
	}
	tm, err := MapValues(res.Providers, "example.com/pos/out")
	if err != nil {
		t.Fatalf("MapValues: %v", err)
	}
	oneSrc, rerr := os.ReadFile(filepath.Join(dir, "vals", "one.go"))
	if rerr != nil {
		t.Fatalf("reading fixture: %v", rerr)
	}
	depSrc, rerr := os.ReadFile(filepath.Join(dir, "dep", "dep.go"))
	if rerr != nil {
		t.Fatalf("reading fixture: %v", rerr)
	}
	got := make(map[string]*NamedType)
	for _, rec := range tm.Types {
		got[rec.WireName] = rec
	}
	check := func(wire, filename string, src []byte, substr string) {
		t.Helper()
		rec := got[wire]
		if rec == nil {
			t.Fatalf("no recorded type %q", wire)
		}
		if rec.Filename != filename {
			t.Errorf("type %q: Filename = %q, want %q", wire, rec.Filename, filename)
		}
		want := wantPosition(t, src, substr)
		if rec.Pos.Line != want.Line || rec.Pos.Column != want.Column {
			t.Errorf("type %q: position = %d:%d, want %d:%d", wire, rec.Pos.Line, rec.Pos.Column, want.Line, want.Column)
		}
	}
	check("user", "example.com/pos/vals/one.go", oneSrc, "User struct")
	check("thing", "example.com/pos/dep/dep.go", depSrc, "Thing struct")
}

// TestSemanticMetadataPositionAcrossFileSets covers machine-metadata
// diagnostics of a generated file: the malformed _intercallSemantic
// constant of an intercall-generated import binding is reported at the
// exact physical position of the constant, whether the binding is a
// dependency package or an explicit package of the load.
func TestSemanticMetadataPositionAcrossFileSets(t *testing.T) {
	dir := writeFixture(t, genFixtureModule)
	impSrc := []byte("// Code generated by intercall-go; DO NOT EDIT.\n" +
		"\n" +
		"package imp\n" +
		"\n" +
		"// @intercall type user_id\n" +
		"type UserID struct {\n" +
		"\tName string `intercall:\"name\"`\n" +
		"}\n" +
		"\n" +
		"// @intercall type code\n" +
		"type Codes []uint8\n" +
		"\n" +
		"const _intercallSemantic = \"!!!not base64url!!!\"\n")
	if err := writeFileAt(t, dir, "imp/imp.go", impSrc); err != nil {
		t.Fatalf("writing generated fixture: %v", err)
	}
	run := func(t *testing.T, patterns ...string) {
		t.Helper()
		res, err := discover(t, dir, patterns, []string{"example.com/gen/prov.FindUser"}, nil, "out")
		if err != nil {
			t.Fatalf("discover: %v", err)
		}
		_, err = MapValues(res.Providers, "example.com/gen/out")
		wantErrorPosition(t, err, "example.com/gen/imp/imp.go", impSrc, "_intercallSemantic", "not valid base64url")
	}
	t.Run("DependencyPackage", func(t *testing.T) {
		run(t, "./prov")
	})
	t.Run("ExplicitPackage", func(t *testing.T) {
		run(t, "./prov", "./imp")
	})
}

// TestMappedDocumentationPositionAcrossFileSets covers retained
// documentation diagnostics of a declaration reached only through
// mapping: the field doc of a dependency package's named type is
// processed on demand, and its terminator rejection points at the exact
// physical byte of the offending text in the dependency file.
func TestMappedDocumentationPositionAcrossFileSets(t *testing.T) {
	dir := writeFixture(t, sourcePosFixture)
	res, err := discover(t, dir, []string{"./vals"}, []string{"example.com/pos/vals.BadDoc"}, nil, "out")
	if err != nil {
		t.Fatalf("discover: %v", err)
	}
	_, err = MapValues(res.Providers, "example.com/pos/out")
	src, rerr := os.ReadFile(filepath.Join(dir, "dep", "dep.go"))
	if rerr != nil {
		t.Fatalf("reading fixture: %v", rerr)
	}
	wantErrorPosition(t, err, "example.com/pos/dep/dep.go", src, "*/", "retained documentation contains '*/'")
}
