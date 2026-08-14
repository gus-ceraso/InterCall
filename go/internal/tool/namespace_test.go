package tool

import (
	"bytes"
	"fmt"
	"strings"
	"testing"
)

// This file tests the complete generated-name reservation of RM-12:
// the import model's package, exception-member, and parameter/local
// namespaces (mandatory accessor, exception Error method, private
// generated helpers and locals) and the export model's provider
// package aliases (a package named ExportBinding and packages whose
// names collide with generated helpers are deterministically aliased).
// Every user-triggerable namespace failure is diagnosed before any
// emission; valid names and aliases remain deterministic.

// importReservedParams is the exact set of generated private parameter
// and local names a user parameter projection may not take: the
// request encoder's buffer parameter and error local and the caller's
// context parameter, connection, error, result, exception, and
// zero-value locals.
func importReservedParams() []string {
	return []string{
		bufName, encErrName, ctxName, connName, callErrName,
		outName, excName, zeroName,
	}
}

// TestGeneratedNamespaceValidation checks the complete reservation
// model of both directions: every user-triggerable namespace failure
// is an error before emission, and every valid name stays valid.
func TestGeneratedNamespaceValidation(t *testing.T) {
	t.Run("import accessor reservation", func(t *testing.T) {
		// A declaration whose projected Go name is ImportBinding
		// collides with the mandatory accessor of the import binding,
		// in every declaration kind and through overrides.
		for _, src := range []string{
			"procedure import_binding {};",
			"type import_binding uint32;",
			"exception import_binding;",
		} {
			_, _, err := generateImportString(src, "imp")
			if err == nil {
				t.Errorf("GenerateImport(%q) succeeded, want an accessor reservation error", src)
				continue
			}
			if !strings.Contains(err.Error(), "Go declaration name collision") ||
				!strings.Contains(err.Error(), "ImportBinding") ||
				!strings.Contains(err.Error(), "reserved for the generated ImportBinding accessor") {
				t.Errorf("GenerateImport(%q) error %q lacks the accessor reservation", src, err)
			}
		}
		_, _, err := generateImportString("type x uint32;", "imp",
			Override{Selector: Selector{Kind: TypeSelector, Name: "x"}, Name: "ImportBinding"})
		if err == nil || !strings.Contains(err.Error(), "Go declaration name collision") {
			t.Errorf("override to ImportBinding: err = %v, want a declaration collision", err)
		}
		// The accessor itself is the only exported generated package
		// symbol; a declaration with any other exported name is valid.
		goFile, _, err := generateImportString("procedure ping {};", "imp")
		if err != nil {
			t.Fatalf("GenerateImport(ping): %v", err)
		}
		typeCheckImportBinding(t, goFile)
	})

	t.Run("import parameter reservation", func(t *testing.T) {
		// A --go-name parameter equal to any generated private
		// parameter or local of the request encoder or the caller is
		// an error, because the projected parameter would capture or
		// shadow the generated name inside the same function scope.
		for _, name := range importReservedParams() {
			_, _, err := generateImportString("procedure p { a uint32; };", "imp",
				Override{Selector: Selector{Kind: ProcedureSelector, Name: "p", Param: "a"}, Name: name})
			if err == nil {
				t.Errorf("parameter override to %q succeeded, want a parameter reservation error", name)
				continue
			}
			if !strings.Contains(err.Error(), "Go parameter name collision") ||
				!strings.Contains(err.Error(), name) {
				t.Errorf("parameter override to %q: error %q lacks the reservation", name, err)
			}
		}
		// The per-procedure request encoder and response decoder names
		// and the encoder of a parameter's own wire type are referenced
		// by the caller and the encoder bodies.
		for _, name := range []string{
			codecName("encreq", "p"),
			codecName("decresp", "p"),
			codecName("enc", "string"),
		} {
			_, _, err := generateImportString("procedure p { a string; b uint32; };", "imp",
				Override{Selector: Selector{Kind: ProcedureSelector, Name: "p", Param: "b"}, Name: name})
			if err == nil || !strings.Contains(err.Error(), "Go parameter name collision") {
				t.Errorf("parameter override to %q: err = %v, want a parameter reservation error", name, err)
			}
		}
		// Default projections that land on the importBinding singleton
		// variable or the intercall runtime package referenced by the
		// caller are errors without any override.
		for _, src := range []string{
			"procedure p { import_binding uint32; };",
			"procedure p { intercall uint32; };",
		} {
			_, _, err := generateImportString(src, "imp")
			if err == nil || !strings.Contains(err.Error(), "Go parameter name collision") {
				t.Errorf("GenerateImport(%q): err = %v, want a parameter reservation error", src, err)
			}
		}
		// A parameter equal to a predeclared identifier the caller
		// bodies reference (error, byte, uint64, nil) breaks the
		// generated code by capture.
		for _, name := range []string{"error", "byte", "uint64", "nil"} {
			_, _, err := generateImportString("procedure p { a uint32; };", "imp",
				Override{Selector: Selector{Kind: ProcedureSelector, Name: "p", Param: "a"}, Name: name})
			if err == nil || !strings.Contains(err.Error(), "Go parameter name collision") {
				t.Errorf("parameter override to %q: err = %v, want a parameter reservation error", name, err)
			}
		}
		// A parameter equal to a named type the caller's result local
		// declares is an error; a parameter equal to nothing the bodies
		// reference stays valid.
		_, _, err := generateImportString("type user_id uint64; procedure p { a uint32; } user_id;", "imp",
			Override{Selector: Selector{Kind: ProcedureSelector, Name: "p", Param: "a"}, Name: "UserID"})
		if err == nil || !strings.Contains(err.Error(), "Go parameter name collision") {
			t.Errorf("parameter override to a result type name: err = %v, want a parameter reservation error", err)
		}
		goFile, _, err := generateImportString("type user_id uint64; procedure p { a uint32; } user_id;", "imp",
			Override{Selector: Selector{Kind: ProcedureSelector, Name: "p", Param: "a"}, Name: "uid"})
		if err != nil {
			t.Fatalf("GenerateImport(valid param override): %v", err)
		}
		typeCheckImportBinding(t, goFile)
	})

	t.Run("exception member reservation", func(t *testing.T) {
		// A record-payload exception field projecting to Error collides
		// with the generated Error method, which the Go compiler
		// rejects outright; the rejection covers default projections
		// and overrides.
		_, _, err := generateImportString("exception e record { error string; };", "imp")
		if err == nil {
			t.Fatal("GenerateImport(exception field error) succeeded, want a member reservation error")
		}
		if !strings.Contains(err.Error(), "Go field name collision") ||
			!strings.Contains(err.Error(), "reserved for the generated Error method") {
			t.Fatalf("exception field error = %v, want the Error method reservation", err)
		}
		_, _, err = generateImportString("exception e record { x string; };", "imp",
			Override{Selector: Selector{Kind: ExceptionSelector, Name: "e", Steps: []Step{{Kind: FieldStep, Field: "x"}}}, Name: "Error"})
		if err == nil || !strings.Contains(err.Error(), "Go field name collision") {
			t.Errorf("exception field override to Error: err = %v, want a member reservation error", err)
		}
		// A nested record field inside a payload is a member of the
		// anonymous struct type, which has no Error method; a named
		// type field may be Error; and a non-record payload has no user
		// fields at all.
		goFile, _, err := generateImportString(`
exception e record { a record { error string; }; };
type t record { error string; };
exception c string;
procedure ping {};
`, "imp")
		if err != nil {
			t.Fatalf("GenerateImport(valid member names): %v", err)
		}
		typeCheckImportBinding(t, goFile)
	})

	t.Run("export package reservation", func(t *testing.T) {
		// A provider package named ExportBinding is aliased
		// deterministically: the generated binding still declares its
		// own ExportBinding accessor and calls the provider through the
		// mangled alias.
		src := `package ExportBinding

import "context"

// @intercall procedure ping
func Ping(ctx context.Context) error { return nil }
`
		model := exportOne(t, "example.com/synth", src)
		goFile, _, err := GenerateExport(model, "exp")
		if err != nil {
			t.Fatalf("GenerateExport(provider named ExportBinding): %v", err)
		}
		gen := string(goFile)
		mangled := ManglePrivate("import", "example.com/synth")
		if !strings.Contains(gen, mangled+` "example.com/synth"`) {
			t.Errorf("provider package named ExportBinding is not aliased as %s:\n%s", mangled, gen)
		}
		if strings.Contains(gen, `ExportBinding "example.com/synth"`) {
			t.Errorf("provider package named ExportBinding kept the plain name:\n%s", gen)
		}
		if !strings.Contains(gen, "func ExportBinding() intercall.ExportBinding {") ||
			!strings.Contains(gen, mangled+".Ping(") {
			t.Errorf("the ExportBinding accessor or the mangled provider call is missing:\n%s", gen)
		}
		typeCheckSyntheticExportBinding(t, map[string]string{"example.com/synth": src}, goFile)
	})

	t.Run("valid names remain valid", func(t *testing.T) {
		// Names that never collide with a generated identifier stay
		// valid in both directions: plain helper lookalikes that the
		// mangling protects, non-colliding overrides, and provider
		// package names outside the reserved scope.
		goFile, _, err := generateImportString(`
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
`, "imp")
		if err != nil {
			t.Fatalf("GenerateImport(hostile params): %v", err)
		}
		typeCheckImportBinding(t, goFile)

		src := `package binding

import "context"

// @intercall procedure ping
func Ping(ctx context.Context) error { return nil }
`
		model := exportOne(t, "example.com/synth", src)
		goFile, _, err = GenerateExport(model, "exp")
		if err != nil {
			t.Fatalf("GenerateExport(provider named binding): %v", err)
		}
		if !strings.Contains(string(goFile), `"example.com/synth"`) ||
			strings.Contains(string(goFile), ManglePrivate("import", "example.com/synth")+` "example.com/synth"`) {
			t.Error("a provider package named binding must keep its plain alias")
		}
	})
}

// TestGeneratedImportAliases checks the import model's deterministic
// private-name reservation: a --go-name parameter equal to any
// generated private parameter or local is rejected, and valid names
// and aliases generate byte-identical output on repeated runs.
func TestGeneratedImportAliases(t *testing.T) {
	t.Run("every generated private parameter and local is reserved", func(t *testing.T) {
		for _, name := range importReservedParams() {
			_, _, err := generateImportString("procedure p { a uint32; };", "imp",
				Override{Selector: Selector{Kind: ProcedureSelector, Name: "p", Param: "a"}, Name: name})
			if err == nil || !strings.Contains(err.Error(), "Go parameter name collision") {
				t.Errorf("parameter override to %q: err = %v, want a parameter reservation error", name, err)
			}
		}
	})

	t.Run("valid overrides remain deterministic", func(t *testing.T) {
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
			t.Fatal("the same interface with the same overrides generated different bytes")
		}
		typeCheckImportBinding(t, a)
	})
}

// TestGeneratedExportAliases checks the export model's deterministic
// provider package aliasing: a provider package named ExportBinding
// and provider package names that collide with generated helpers,
// plain locals, or referenced predeclared identifiers are privately
// mangled, valid package names keep their plain aliases, and the
// output is byte-identical on repeated generation.
func TestGeneratedExportAliases(t *testing.T) {
	t.Run("helper name collisions", func(t *testing.T) {
		// Every generated helper class: the mangled dispatch, matcher,
		// and binding names, the codec support values, a primitive and
		// a per-procedure codec name, and the ExportBinding accessor.
		names := []string{
			exportBindingName, dispatchName, matcherName, maxIntName,
			errShortName, codecName("enc", "string"), codecName("dereq", "ping"),
		}
		for _, name := range names {
			t.Run(name, func(t *testing.T) {
				src := "package " + name + `

import "context"

// @intercall procedure ping
func Ping(ctx context.Context) error { return nil }
`
				model := exportOne(t, "example.com/synth", src)
				goFile, _, err := GenerateExport(model, "exp")
				if err != nil {
					t.Fatalf("GenerateExport(provider named %q): %v", name, err)
				}
				gen := string(goFile)
				mangled := ManglePrivate("import", "example.com/synth")
				if !strings.Contains(gen, mangled+` "example.com/synth"`) {
					t.Errorf("provider package named %q is not aliased as %s:\n%s", name, mangled, gen)
				}
				if !strings.Contains(gen, mangled+".Ping(") {
					t.Errorf("the provider must be called through its mangled alias %s", mangled)
				}
				typeCheckSyntheticExportBinding(t, map[string]string{"example.com/synth": src}, goFile)
			})
		}
	})

	t.Run("plain local and predeclared name collisions", func(t *testing.T) {
		// A provider package named like a plain local of a generated
		// function body (err, match, out, buf) or like a predeclared
		// identifier a body references (byte, error, len) would be
		// captured or shadowed by the generated code; the alias is
		// mangled instead.
		for _, name := range []string{"err", "match", "out", "buf", "byte", "error", "len", "uint16", "float32", "uint"} {
			src := "package " + name + `

import "context"

// @intercall procedure ping
func Ping(ctx context.Context) error { return nil }
`
			model := exportOne(t, "example.com/synth", src)
			goFile, _, err := GenerateExport(model, "exp")
			if err != nil {
				t.Fatalf("GenerateExport(provider named %q): %v", name, err)
			}
			gen := string(goFile)
			mangled := ManglePrivate("import", "example.com/synth")
			if !strings.Contains(gen, mangled+` "example.com/synth"`) {
				t.Errorf("provider package named %q is not aliased as %s:\n%s", name, mangled, gen)
			}
			typeCheckSyntheticExportBinding(t, map[string]string{"example.com/synth": src}, goFile)
		}
	})

	t.Run("exception package collisions", func(t *testing.T) {
		// An application exception package whose name collides with a
		// generated helper is aliased deterministically, and the
		// matcher compares through the mangled alias.
		src := `package ` + exportBindingName + `

import "errors"

// ErrDenied is a no-payload application exception.
// @intercall exception denied
var ErrDenied error = errors.New("denied")
`
		model := exportOne(t, "example.com/synth", src)
		goFile, _, err := GenerateExport(model, "exp")
		if err != nil {
			t.Fatalf("GenerateExport(exception package named %q): %v", exportBindingName, err)
		}
		gen := string(goFile)
		mangled := ManglePrivate("import", "example.com/synth")
		if !strings.Contains(gen, mangled+` "example.com/synth"`) {
			t.Errorf("exception package is not aliased as %s:\n%s", mangled, gen)
		}
		if !strings.Contains(gen, "err == error("+mangled+".ErrDenied)") {
			t.Errorf("the matcher does not compare through the mangled alias:\n%s", gen)
		}
		typeCheckSyntheticExportBinding(t, map[string]string{"example.com/synth": src}, goFile)
	})

	t.Run("deterministic aliases", func(t *testing.T) {
		// The same colliding package generates the same alias and the
		// same bytes on repeated runs; two different colliding package
		// names generate different mangled aliases.
		src := `package ExportBinding

import "context"

// @intercall procedure ping
func Ping(ctx context.Context) error { return nil }
`
		model := exportOne(t, "example.com/synth", src)
		a, aBody, err := GenerateExport(model, "exp")
		if err != nil {
			t.Fatalf("GenerateExport: %v", err)
		}
		b, bBody, err := GenerateExport(model, "exp")
		if err != nil {
			t.Fatalf("GenerateExport (second run): %v", err)
		}
		if !bytes.Equal(a, b) || !bytes.Equal(aBody, bBody) {
			t.Fatal("the same colliding model generated different bytes")
		}
		other := exportOne(t, "example.com/other", src)
		c, _, err := GenerateExport(other, "exp")
		if err != nil {
			t.Fatalf("GenerateExport(other path): %v", err)
		}
		if bytes.Equal(a, c) {
			t.Fatal("two different colliding packages produced the same binding bytes")
		}
		if got := ManglePrivate("import", "example.com/synth"); got == ManglePrivate("import", "example.com/other") {
			t.Fatal("the mangling is not path-distinct")
		}
	})

	t.Run("complete reserved scope", func(t *testing.T) {
		// Every reserved identifier of the generated export binding is
		// enumerated: a provider package named after any of them is
		// privately mangled and the binding type-checks; a provider
		// package outside the reserved scope keeps its plain alias.
		// The []uint16 parameter is a true list, so the codec bodies
		// exercise the i0 and count0 locals, and the two wire
		// parameters exercise v0 and v1.
		reserved := []string{
			"ExportBinding", "context", "errors", "intercall", "math", "utf8",
			"buf", "v", "src", "err", "bits", "n64", "n", "b", "dst", "rest",
			"e", "ok", "match", "excKey", "excPayload", "encErr", "out", "enc",
			"int8", "int16", "int32", "int64", "uint8", "uint16", "uint32",
			"uint64", "uint", "float32", "float64", "byte", "error", "string", "int",
			"len", "make", "copy", "append", "panic", "nil", "v0", "i0", "count0",
		}
		for _, name := range reserved {
			t.Run(name, func(t *testing.T) {
				src := "package " + name + `

import "context"

// @intercall procedure ping
func Ping(ctx context.Context, samples []uint16, extra uint32) error { return nil }
`
				model := exportOne(t, "example.com/synth", src)
				goFile, _, err := GenerateExport(model, "exp")
				if err != nil {
					t.Fatalf("GenerateExport(provider named %q): %v", name, err)
				}
				gen := string(goFile)
				mangled := ManglePrivate("import", "example.com/synth")
				if !strings.Contains(gen, mangled+` "example.com/synth"`) {
					t.Errorf("reserved package name %q is not mangled:\n%s", name, gen)
				}
				typeCheckSyntheticExportBinding(t, map[string]string{"example.com/synth": src}, goFile)
			})
		}
		for _, name := range []string{"prov", "binding", "value", "key", "payload"} {
			t.Run(name, func(t *testing.T) {
				src := "package " + name + `

import "context"

// @intercall procedure ping
func Ping(ctx context.Context, samples []uint16, extra uint32) error { return nil }
`
				model := exportOne(t, "example.com/synth", src)
				goFile, _, err := GenerateExport(model, "exp")
				if err != nil {
					t.Fatalf("GenerateExport(provider named %q): %v", name, err)
				}
				gen := string(goFile)
				if strings.Contains(gen, ManglePrivate("import", "example.com/synth")+` "example.com/synth"`) {
					t.Errorf("non-reserved package name %q must keep its plain alias:\n%s", name, gen)
				}
				typeCheckSyntheticExportBinding(t, map[string]string{"example.com/synth": src}, goFile)
			})
		}
	})

	t.Run("anonymous pair fixpoint", func(t *testing.T) {
		// A provider package named exactly like the anonymous codec
		// pair of its own inline record occurrence is aliased
		// deterministically through the fixpoint: the pair name embeds
		// the resolved alias, so the collision is only visible after
		// the first registration and the alias is re-resolved.
		pairName := codecName("enc", "record{x int32}", "struct {\n\tX int32\n}")
		src := fmt.Sprintf(`package %s

import "context"

// @intercall procedure p
func P(ctx context.Context, v struct{ X int32 }) error { return nil }
`, pairName)
		model := exportOne(t, "example.com/synth", src)
		goFile, _, err := GenerateExport(model, "exp")
		if err != nil {
			t.Fatalf("GenerateExport(anon-pair-named provider): %v", err)
		}
		gen := string(goFile)
		mangled := ManglePrivate("import", "example.com/synth")
		if !strings.Contains(gen, mangled+` "example.com/synth"`) {
			t.Errorf("the anon-pair-named provider is not aliased as %s:\n%s", mangled, gen)
		}
		if strings.Contains(gen, pairName+` "example.com/synth"`) {
			t.Errorf("the anon-pair-named provider kept its plain import alias:\n%s", gen)
		}
		if !strings.Contains(gen, mangled+".P(") {
			t.Errorf("the provider must be called through its mangled alias %s", mangled)
		}
		typeCheckSyntheticExportBinding(t, map[string]string{"example.com/synth": src}, goFile)
	})
}
