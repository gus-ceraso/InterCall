package tool

import (
	"bytes"
	"encoding/base64"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"strconv"
	"strings"
	"testing"
)

// This file tests the import generator: the fixed runtime exception
// reservation, --go-name overrides, the generated named types and
// exception symbols, the exact machine lines and intercall tags, the one
// canonical chunked base64url semantic constant (including the empty and
// >4096-byte cases), the immutable binding singleton, and deterministic
// byte output. The checked-in fixture is the byte-exact generation
// golden; these tests additionally check generated shapes over synthetic
// interfaces.

// generateImportString runs the complete import generation pipeline over
// one interface source text.
func generateImportString(src string, pkg string, overrides ...Override) ([]byte, []byte, error) {
	return GenerateImport("import.intercall", []byte(src), overrides, pkg)
}

// TestImportGeneration checks the generated shapes of the fixture and of
// synthetic interfaces: exact machine lines, exact intercall tags, the
// three exception symbol forms, the fixed sentinel mapping, the binding
// singleton, the semantic constant, and the fixed-name reservation.
func TestImportGeneration(t *testing.T) {
	t.Run("fixture shapes", func(t *testing.T) {
		goFile, body, err := generateImportFixture()
		if err != nil {
			t.Fatalf("generateImportFixture: %v", err)
		}
		gen := string(goFile)
		// Exact machine lines on the generated named types.
		for _, want := range []string{
			"// @intercall type user_id", "// @intercall type point",
			"// @intercall type empty", "// @intercall type names", "// @intercall type blob",
		} {
			if !strings.Contains(gen, want) {
				t.Errorf("generated binding lacks %q", want)
			}
		}
		// Exact intercall tags on generated fields, including nested
		// anonymous record fields. The struct fields are gofmt-aligned,
		// so the field name and its tag are asserted separately.
		for _, want := range []string{
			"X float64", "Y float64",
			"Code", "Message",
			`intercall:"x"`, `intercall:"y"`, `intercall:"code"`, `intercall:"message"`,
		} {
			if !strings.Contains(gen, want) {
				t.Errorf("generated binding lacks %q", want)
			}
		}
		// Exception symbols: the no-payload sentinel, the inline-record
		// error struct, and the other-payload wrapper struct.
		for _, want := range []string{
			`var Denied error = errors.New("denied")`,
			"type Failed struct {",
			`func (e *Failed) Error() string { return "failed" }`,
			"type Overloaded struct {",
			"\tPayload Names",
			`func (e *Overloaded) Error() string { return "overloaded" }`,
		} {
			if !strings.Contains(gen, want) {
				t.Errorf("generated binding lacks %q", want)
			}
		}
		// Fixed runtime exception mapping to the shared root sentinels
		// and the exact procedure key literal of echo.
		for _, want := range []string{
			"*exc = intercall.ErrProcedureNotFound",
			"*exc = intercall.ErrInvalidArguments",
			"*exc = intercall.ErrInternalException",
			"0x159eb91a98f8f42",
		} {
			if !strings.Contains(gen, want) {
				t.Errorf("generated binding lacks %q", want)
			}
		}
		// The immutable binding singleton.
		for _, want := range []string{
			"var importBinding = intercall.NewImportBindingWithInterfaceID(",
			"func ImportBinding() intercall.ImportBinding {",
		} {
			if !strings.Contains(gen, want) {
				t.Errorf("generated binding lacks %q", want)
			}
		}
		// The one semantic constant and the caller entry points; the
		// generated file carries no ownership marker itself.
		if !strings.Contains(gen, "const _intercallSemantic = ") {
			t.Error("generated binding lacks the _intercallSemantic constant")
		}
		if strings.Count(gen, "const _intercallSemantic = ") != 1 {
			t.Error("generated binding has more than one _intercallSemantic constant")
		}
		for _, want := range []string{"func Echo(", "func Add(", "func Paint(", "func Ping("} {
			if !strings.Contains(gen, want) {
				t.Errorf("generated binding lacks %q", want)
			}
		}
		if strings.Contains(gen, intercallGeneratedMarker) {
			t.Error("the generated binding Go file must not carry the ownership marker itself")
		}
		// The canonical body is exactly the fixture's canonical body.
		wantBody := canonicalBodyOf(t, "import.intercall", importFixtureSource)
		if !bytes.Equal(body, wantBody) {
			t.Error("the canonical interface body does not match the fixture source")
		}
	})

	t.Run("empty interface", func(t *testing.T) {
		goFile, body, err := generateImportString("", "imp")
		if err != nil {
			t.Fatalf("GenerateImport(empty): %v", err)
		}
		if len(body) != 0 {
			t.Errorf("the canonical body of an empty interface is %d bytes, want 0", len(body))
		}
		gen := string(goFile)
		if !strings.Contains(gen, `const _intercallSemantic = ""`) {
			t.Errorf("empty interface lacks the empty semantic constant:\n%s", gen)
		}
		if !strings.Contains(gen, "var importBinding = intercall.NewImportBindingWithInterfaceID(") {
			t.Error("empty interface lacks the binding singleton")
		}
		if strings.Contains(gen, `"context"`) {
			t.Error("an interface without procedures must not import context")
		}
		typeCheckImportBinding(t, goFile)
	})

	t.Run("fixed name reservation", func(t *testing.T) {
		cases := []struct {
			src  string
			want string
		}{
			{"type internal_exception uint64;", "fixed runtime exception name"},
			{"type procedure_not_found string;", "fixed runtime exception name"},
			{"type invalid_arguments bytes;", "fixed runtime exception name"},
			{"procedure internal_exception {};", "fixed runtime exception name"},
			{"procedure procedure_not_found { value string; };", "fixed runtime exception name"},
			{"exception invalid_arguments string;", "fixed runtime exception name"},
			{"exception procedure_not_found record { x int32; };", "fixed runtime exception name"},
		}
		for _, tc := range cases {
			_, _, err := generateImportString(tc.src, "imp")
			if err == nil {
				t.Errorf("GenerateImport(%q) succeeded, want a reservation error", tc.src)
				continue
			}
			if !strings.Contains(err.Error(), tc.want) {
				t.Errorf("GenerateImport(%q) error %q lacks %q", tc.src, err, tc.want)
			}
		}
		// The exact no-payload fixed exception declarations are valid and
		// map to root sentinels without generated symbols.
		goFile, _, err := generateImportString(`
exception internal_exception;
exception invalid_arguments;
exception procedure_not_found;
procedure ping {};
`, "imp")
		if err != nil {
			t.Fatalf("GenerateImport(fixed no-payload): %v", err)
		}
		gen := string(goFile)
		for _, want := range []string{
			"*exc = intercall.ErrInternalException",
			"*exc = intercall.ErrInvalidArguments",
			"*exc = intercall.ErrProcedureNotFound",
			"func Ping(",
		} {
			if !strings.Contains(gen, want) {
				t.Errorf("fixed-exception interface lacks %q", want)
			}
		}
		if strings.Contains(gen, "var InternalException") {
			t.Error("a fixed runtime exception must not generate a package symbol")
		}
		typeCheckImportBinding(t, goFile)
		// Overriding a fixed exception is an error.
		_, _, err = GenerateImport("import.intercall", []byte("exception internal_exception;"),
			[]Override{{Selector: Selector{Kind: ExceptionSelector, Name: "internal_exception"}, Name: "X"}}, "imp")
		if err == nil || !strings.Contains(err.Error(), "cannot be overridden") {
			t.Errorf("overriding a fixed exception: err = %v, want an override rejection", err)
		}
	})

	t.Run("exception payload forms", func(t *testing.T) {
		// Every payload form must generate a compilable binding: the
		// inline record with fields, the distinct zero-field type for
		// record {}, and the Payload wrapper for every other payload.
		goFile, _, err := generateImportString(`
type point record {
    x int32;
    y int32;
};
procedure ping {};
exception a record {};
exception b record {
    x int32;
    y string;
};
exception c list string;
exception d uint64;
exception e point;
`, "imp")
		if err != nil {
			t.Fatalf("GenerateImport(payload forms): %v", err)
		}
		gen := string(goFile)
		for _, want := range []string{
			"type A struct{}",
			"type B struct {",
			"type C struct {",
			"\tPayload []string",
			"type D struct {",
			"\tPayload uint64",
			"type E struct {",
			"\tPayload Point",
			`func (e *A) Error() string { return "a" }`,
		} {
			if !strings.Contains(gen, want) {
				t.Errorf("payload-forms binding lacks %q", want)
			}
		}
		typeCheckImportBinding(t, goFile)
	})

	t.Run("overrides", func(t *testing.T) {
		over, err := ParseOverrides([]string{
			"type:user_id=CustomerKey",
			"type:point/field:x=XCoord",
			"exception:denied=AccessDenied",
			"procedure:echo=Say",
			"procedure:echo/param:value=text",
		})
		if err != nil {
			t.Fatalf("ParseOverrides: %v", err)
		}
		goFile, body, err := GenerateImport("import.intercall", importFixtureSource, over, "imp")
		if err != nil {
			t.Fatalf("GenerateImport(overrides): %v", err)
		}
		gen := string(goFile)
		for _, want := range []string{
			"type CustomerKey uint64",
			"XCoord float64 `intercall:\"x\"`",
			"var AccessDenied error",
			"func Say(",
			"text string",
		} {
			if !strings.Contains(gen, want) {
				t.Errorf("overridden binding lacks %q", want)
			}
		}
		for _, gone := range []string{
			"type UserID uint64",
			"var Denied error",
			"func Echo(",
		} {
			if strings.Contains(gen, gone) {
				t.Errorf("overridden binding still contains %q", gone)
			}
		}
		// Overrides never change the interface bytes or the semantic
		// metadata: the canonical body is identical to the unoverridden
		// body, and the semantic constant is identical.
		_, plainBody, err := generateImportFixture()
		if err != nil {
			t.Fatalf("generateImportFixture: %v", err)
		}
		if !bytes.Equal(body, plainBody) {
			t.Error("--go-name overrides changed the canonical interface body")
		}
		plain, _, err := generateImportFixture()
		if err != nil {
			t.Fatalf("generateImportFixture: %v", err)
		}
		if got, want := semanticValue(t, goFile), semanticValue(t, plain); got != want {
			t.Error("--go-name overrides changed the _intercallSemantic constant")
		}
		typeCheckImportBinding(t, goFile)
	})

	t.Run("hostile parameter names", func(t *testing.T) {
		// Every plain helper name a naive emitter might use is a legal
		// wire parameter name; the generated callers must mangle their
		// own locals and compile.
		src := `
procedure p {
    err string;
    ctx uint8;
    conn uint64;
    out bytes;
    exc string;
    zero int32;
    buf uint64;
    key uint16;
    payload string;
    rest int8;
    value string;
} string;
`
		goFile, _, err := generateImportString(src, "imp")
		if err != nil {
			t.Fatalf("GenerateImport(hostile params): %v", err)
		}
		typeCheckImportBinding(t, goFile)
	})

	t.Run("semantic chunking", func(t *testing.T) {
		// The empty value is `""` with no '+'.
		goFile, body, err := generateImportString("", "imp")
		if err != nil {
			t.Fatalf("GenerateImport(empty): %v", err)
		}
		chunks := semanticChunks(t, goFile)
		if len(chunks) != 1 || chunks[0] != "" {
			t.Errorf("empty interface chunks = %q, want one empty chunk", chunks)
		}
		if len(body) != 0 {
			t.Errorf("empty interface body = %d bytes, want 0", len(body))
		}

		// A canonical body above one chunk boundary: chunks of at most
		// 4096 bytes with every nonfinal chunk at exactly 4096, decoding
		// to the canonical body.
		var b strings.Builder
		for i := 0; i < 40; i++ {
			fmt.Fprintf(&b, "/* Procedure %d. */\n", i)
			fmt.Fprintf(&b, "/* A documented procedure whose documentation and structure push the canonical interface body well beyond a single 4096-byte base64url chunk, exercising the deterministic chunked emission of the semantic constant. */\n")
			fmt.Fprintf(&b, "procedure p%d {\n    /* First parameter. */\n    a string;\n} string;\n", i)
		}
		goFile, body, err = generateImportString(b.String(), "imp")
		if err != nil {
			t.Fatalf("GenerateImport(large): %v", err)
		}
		chunks = semanticChunks(t, goFile)
		if len(chunks) < 2 {
			t.Fatalf("large interface produced %d chunks, want at least 2", len(chunks))
		}
		for i, c := range chunks {
			if len(c) > semanticChunkSize {
				t.Errorf("chunk %d is %d bytes, over the %d-byte limit", i, len(c), semanticChunkSize)
			}
			if i < len(chunks)-1 && len(c) != semanticChunkSize {
				t.Errorf("nonfinal chunk %d is %d bytes, want exactly %d", i, len(c), semanticChunkSize)
			}
		}
		got := strings.Join(chunks, "")
		raw, err := base64.RawURLEncoding.DecodeString(got)
		if err != nil {
			t.Fatalf("decoding the chunked constant: %v", err)
		}
		if base64.RawURLEncoding.EncodeToString(raw) != got {
			t.Error("the chunked constant is not canonical base64url")
		}
		if !bytes.Equal(raw, body) {
			t.Error("the chunked constant does not decode to the canonical body")
		}
	})

	t.Run("fixture constant chunking", func(t *testing.T) {
		// The compiled fixture's canonical body is above one chunk, so
		// the checked-in binding exercises the chunked emission.
		goFile, body, err := generateImportFixture()
		if err != nil {
			t.Fatalf("generateImportFixture: %v", err)
		}
		if len(body) <= semanticChunkSize {
			t.Fatalf("the fixture body is %d bytes; the chunking test needs more than %d", len(body), semanticChunkSize)
		}
		chunks := semanticChunks(t, goFile)
		if len(chunks) < 2 {
			t.Fatalf("the fixture constant has %d chunks, want at least 2", len(chunks))
		}
		for i, c := range chunks {
			if i < len(chunks)-1 && len(c) != semanticChunkSize {
				t.Errorf("nonfinal fixture chunk %d is %d bytes, want exactly %d", i, len(c), semanticChunkSize)
			}
		}
	})
}

// semanticValue extracts the decoded value of the one
// _intercallSemantic constant of a generated binding.
func semanticValue(t *testing.T, src []byte) string {
	t.Helper()
	return strings.Join(semanticChunks(t, src), "")
}

// semanticChunks extracts the quoted chunks of the one
// _intercallSemantic constant of a generated binding, in order. The
// constant must be declared alone with exactly one value, a
// concatenation of string literals.
func semanticChunks(t *testing.T, src []byte) []string {
	t.Helper()
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, "binding_gen.go", src, parser.AllErrors)
	if err != nil {
		t.Fatalf("parsing the generated binding: %v", err)
	}
	var vs *ast.ValueSpec
	for _, d := range f.Decls {
		gd, ok := d.(*ast.GenDecl)
		if !ok || gd.Tok != token.CONST {
			continue
		}
		for _, s := range gd.Specs {
			spec := s.(*ast.ValueSpec)
			for _, n := range spec.Names {
				if n.Name == semanticConstantName {
					if vs != nil {
						t.Fatalf("more than one %s constant", semanticConstantName)
					}
					vs = spec
				}
			}
		}
	}
	if vs == nil {
		t.Fatalf("no %s constant", semanticConstantName)
	}
	if len(vs.Values) != 1 {
		t.Fatalf("the %s constant has %d values, want 1", semanticConstantName, len(vs.Values))
	}
	var chunks []string
	var walk func(e ast.Expr)
	walk = func(e ast.Expr) {
		switch e := e.(type) {
		case *ast.BasicLit:
			if e.Kind != token.STRING {
				t.Fatalf("the %s value is not a string literal", semanticConstantName)
			}
			s, err := strconv.Unquote(e.Value)
			if err != nil {
				t.Fatalf("unquoting the %s value: %v", semanticConstantName, err)
			}
			chunks = append(chunks, s)
		case *ast.BinaryExpr:
			if e.Op != token.ADD {
				t.Fatalf("the %s value is not a concatenation", semanticConstantName)
			}
			walk(e.X)
			walk(e.Y)
		default:
			t.Fatalf("the %s value has an unexpected node %T", semanticConstantName, e)
		}
	}
	walk(vs.Values[0])
	return chunks
}

// TestImportGenerationDeterminism verifies that the import emitter is
// deterministic: the same interface generates byte-identical output on
// repeated runs, with and without overrides, and a different interface
// generates different bytes. The fixture comparison in
// TestImportGeneratedFixtureCompiles already pins the emitted bytes.
func TestImportGenerationDeterminism(t *testing.T) {
	first, firstBody, err := generateImportFixture()
	if err != nil {
		t.Fatalf("generateImportFixture: %v", err)
	}
	second, secondBody, err := generateImportFixture()
	if err != nil {
		t.Fatalf("generateImportFixture (second run): %v", err)
	}
	if !bytes.Equal(first, second) || !bytes.Equal(firstBody, secondBody) {
		t.Fatal("generating the fixture twice produced different bytes")
	}

	src := `
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
`
	over, err := ParseOverrides([]string{"type:token=AuthToken", "procedure:authenticate/param:token=cred"})
	if err != nil {
		t.Fatalf("ParseOverrides: %v", err)
	}
	a, aBody, err := GenerateImport("import.intercall", []byte(src), over, "imp")
	if err != nil {
		t.Fatalf("GenerateImport: %v", err)
	}
	b, bBody, err := GenerateImport("import.intercall", []byte(src), over, "imp")
	if err != nil {
		t.Fatalf("GenerateImport (second run): %v", err)
	}
	if !bytes.Equal(a, b) || !bytes.Equal(aBody, bBody) {
		t.Fatal("generating the same non-fixture interface twice produced different bytes")
	}
	if bytes.Equal(a, first) {
		t.Fatal("a different interface produced the same binding bytes")
	}
}
