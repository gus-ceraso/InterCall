package tool

import (
	"bytes"
	"go/ast"
	"go/importer"
	"go/parser"
	"go/token"
	"go/types"
	"os"
	"testing"

	"github.com/cerasos/intercall/internal/syntax"
)

// fixtureGenPath is the checked-in generated fixture, relative to the
// package directory that go test runs in.
const fixtureGenPath = "fixture/codec_gen.go"

// TestGeneratedCodecFixtureCompiles regenerates the codec fixture from
// the embedded interface, verifies the checked-in fixture file is
// byte-identical, and type-checks the regenerated source with the
// standard library. Validation never rewrites the checked-in fixture.
//
// The fixture package also compiles as a real dependency of this test
// binary, so a generated source that does not compile fails here before
// any vector can run.
func TestGeneratedCodecFixtureCompiles(t *testing.T) {
	gen, err := generateCodecFixture()
	if err != nil {
		t.Fatalf("generateCodecFixture: %v", err)
	}
	checked, err := os.ReadFile(fixtureGenPath)
	if err != nil {
		t.Fatalf("reading %s: %v", fixtureGenPath, err)
	}
	if !bytes.Equal(gen, checked) {
		t.Fatalf("%s is stale: generated fixture (%d bytes) differs from the checked-in file (%d bytes); regenerate it from the emitter", fixtureGenPath, len(gen), len(checked))
	}
	typeCheckFixture(t, gen)
}

// typeCheckFixture parses and type-checks one generated fixture source
// against the standard library, verifying it is valid Go independent of
// the compiled fixture package.
func typeCheckFixture(t *testing.T, src []byte) {
	t.Helper()
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, fixtureGenPath, src, parser.AllErrors)
	if err != nil {
		t.Fatalf("generated fixture does not parse: %v", err)
	}
	conf := types.Config{Importer: importer.Default()}
	if _, err := conf.Check("fixture", fset, []*ast.File{f}, nil); err != nil {
		t.Fatalf("generated fixture does not type-check: %v", err)
	}
}

// TestCodecGenerationDeterminism verifies that the emitter is
// deterministic: the same interface generates byte-identical output on
// repeated runs, and a second representative interface is stable too.
// The fixture comparison in TestGeneratedCodecFixtureCompiles already
// pins the emitted bytes; this test additionally runs the emitter twice
// with fresh models and over a different interface shape.
func TestCodecGenerationDeterminism(t *testing.T) {
	first, err := generateCodecFixture()
	if err != nil {
		t.Fatalf("generateCodecFixture: %v", err)
	}
	second, err := generateCodecFixture()
	if err != nil {
		t.Fatalf("generateCodecFixture (second run): %v", err)
	}
	if !bytes.Equal(first, second) {
		t.Fatal("generating the fixture twice produced different bytes")
	}

	src := []byte(`
type token record {
    issuer string;
    expires_at uint64;
};
exception unauthorized;
procedure authenticate {
    token token;
} record {
    ok uint8;
    detail string;
};
`)
	a, err := generateCodecString(src)
	if err != nil {
		t.Fatalf("generateCodecString: %v", err)
	}
	b, err := generateCodecString(src)
	if err != nil {
		t.Fatalf("generateCodecString (second run): %v", err)
	}
	if !bytes.Equal(a, b) {
		t.Fatal("generating the same non-fixture interface twice produced different bytes")
	}
	if bytes.Equal(a, first) {
		t.Fatal("a different interface produced the same fixture bytes")
	}
}

// generateCodecString runs the complete generation pipeline over one
// interface source text.
func generateCodecString(src []byte) ([]byte, error) {
	f, err := syntax.Parse("codec.intercall", src)
	if err != nil {
		return nil, err
	}
	if err := syntax.Validate(f); err != nil {
		return nil, err
	}
	m, err := BuildModel(f)
	if err != nil {
		return nil, err
	}
	return generateCodecFile("codec", m)
}

// TestCodecModel verifies the generation records and codec facts of the
// fixture interface: syntax order, projected Go names, keys, zero-width
// facts, and the emitted Go type expressions.
func TestCodecModel(t *testing.T) {
	f, err := syntax.Parse("fixture.intercall", codecFixtureSource)
	if err != nil {
		t.Fatalf("syntax.Parse: %v", err)
	}
	if err := syntax.Validate(f); err != nil {
		t.Fatalf("syntax.Validate: %v", err)
	}
	m, err := BuildModel(f)
	if err != nil {
		t.Fatalf("BuildModel: %v", err)
	}
	if len(m.Types) != 9 {
		t.Fatalf("len(m.Types) = %d, want 9", len(m.Types))
	}
	if len(m.Exceptions) != 3 {
		t.Fatalf("len(m.Exceptions) = %d, want 3", len(m.Exceptions))
	}
	if len(m.Procs) != 18 {
		t.Fatalf("len(m.Procs) = %d, want 18", len(m.Procs))
	}
	// Records stay in syntax order with the projected Pascal names.
	wantOrder := []string{"UserID", "Name", "Point", "Pixel", "Empty", "Names", "Matrix", "Blob", "CustomerID"}
	for i, want := range wantOrder {
		if got := m.Types[i].GoName; got != want {
			t.Errorf("m.Types[%d].GoName = %q, want %q", i, got, want)
		}
	}
	// Zero-width facts: empty is zero-width; point, names, and matrix are not.
	zero := map[string]bool{}
	for _, tr := range m.Types {
		zero[tr.GoName] = tr.ZeroWidth
	}
	if !zero["Empty"] {
		t.Error("Empty must be zero-width")
	}
	for _, name := range []string{"Point", "Names", "Matrix", "Blob", "UserID"} {
		if zero[name] {
			t.Errorf("%s must not be zero-width", name)
		}
	}
	// Keys come from the exact declaration names.
	keyOf := map[string]uint64{}
	for _, p := range m.Procs {
		keyOf[p.Decl.Name.Name] = p.Key
	}
	if got, want := keyOf["add"], syntax.ProcedureKey("add"); got != want {
		t.Errorf("procedure add key = %#x, want %#x", got, want)
	}
	if got, want := keyOf["echo"], syntax.ProcedureKey("echo"); got != want {
		t.Errorf("procedure echo key = %#x, want %#x", got, want)
	}
	// The README key vector pins the FNV-0 derivation.
	f2 := parseFixture(t, "type user record {\n    name string;\n};\nprocedure get_user {\n    name string;\n} user;\n")
	m2, err := BuildModel(f2)
	if err != nil {
		t.Fatalf("BuildModel(get_user): %v", err)
	}
	if got := m2.Procs[0].Key; got != 0x4c63cc5048869eb7 {
		t.Errorf("procedure get_user key = %#x, want the README vector %#x", got, uint64(0x4c63cc5048869eb7))
	}
	// Codec facts: the paint origin is an anonymous record with Go type
	// struct { X int32 ... } and zero-width fields stay zero-width.
	var origin *TypeFact
	for _, p := range m.Procs {
		if p.Decl.Name.Name == "paint" {
			origin = p.Params[0].Type
		}
	}
	if origin == nil {
		t.Fatal("paint procedure not found")
	}
	if origin.ZeroWidth {
		t.Error("paint origin must not be zero-width")
	}
	if !bytes.Contains([]byte(origin.GoType), []byte(`X int32`)) {
		t.Errorf("paint origin GoType %q lacks the X field", origin.GoType)
	}
	// The result of blanks is list empty: not zero-width, but its element
	// is. The model reports the list fact; the emitter derives the
	// element fact from the same walk.
	for _, p := range m.Procs {
		if p.Decl.Name.Name == "blanks" {
			if p.Result.ZeroWidth {
				t.Error("list empty must not be zero-width (it carries its count)")
			}
			lt, ok := p.Result.Type.(*syntax.ListType)
			if !ok {
				t.Fatalf("blanks result is %T, want *syntax.ListType", p.Result.Type)
			}
			if !zeroWidthOf(lt.Elem, m.types) {
				t.Error("list empty element must be zero-width")
			}
		}
	}
}

// TestCodecModelErrors verifies that the model build reports naming
// failures deterministically.
func TestCodecModelErrors(t *testing.T) {
	f := parseFixture(t, "type bad_Name string;")
	if _, err := BuildModel(f); err == nil {
		t.Fatal("BuildModel succeeded for a noncanonical wire name, want an error")
	}
	f = parseFixture(t, "type a string;\ntype A string;")
	if _, err := BuildModel(f); err == nil {
		t.Fatal("BuildModel succeeded for colliding projected Go names, want an error")
	}
}

// TestEmptyInterfaceGeneration verifies the generator handles an empty
// interface: no declarations, but still deterministic valid Go.
func TestEmptyInterfaceGeneration(t *testing.T) {
	out, err := generateCodecString(nil)
	if err != nil {
		t.Fatalf("generateCodecString(empty): %v", err)
	}
	if !bytes.Contains(out, []byte("package codec")) {
		t.Fatalf("empty interface output lacks the package clause:\n%s", out)
	}
	typeCheckFixture(t, out)
	out2, err := generateCodecString(nil)
	if err != nil {
		t.Fatalf("generateCodecString(empty) second run: %v", err)
	}
	if !bytes.Equal(out, out2) {
		t.Fatal("empty interface generation is not deterministic")
	}
}
