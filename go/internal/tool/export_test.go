package tool

import (
	"bytes"
	"reflect"
	"testing"

	"github.com/cerasos/intercall/go/internal/syntax"
)

// This file tests the export model and interface assembly: application
// exception collection and validation (SPEC.md "Application
// exceptions"), the fixed runtime exceptions (SPEC.md "Fixed Go Runtime
// Exceptions"), and the stable emission order and canonical interface
// AST (SPEC.md "Deterministic export order").
//
// The negative cases are small in-memory packages type-checked against
// the standard library; the cross-package integration cases run over
// temporary modules loaded through the real discovery pipeline.

// The exact keys of the three fixed runtime exceptions.
const (
	fixedProcedureNotFound uint64 = 0x970e76fcc5e2dacb
	fixedInvalidArguments  uint64 = 0x3f5fc972f8477b07
	fixedInternalException uint64 = 0x1aaec22e85996f50
)

// exportOne runs the complete export model pass over one synthetic
// package. The empty output path skips the importability checks.
func exportOne(t *testing.T, name, src string) *ExportModel {
	t.Helper()
	exp, providers := synthPkg(t, name, map[string]string{"synth.go": src})
	model, err := MapExport(&DiscoverResult{Packages: []*ExplicitPackage{exp}, Providers: providers}, "")
	if err != nil {
		t.Fatalf("MapExport: %v", err)
	}
	return model
}

// exportErr fails unless MapExport over one synthetic package returns
// an error containing every substring.
func exportErr(t *testing.T, name, src string, contains ...string) {
	t.Helper()
	exp, providers := synthPkg(t, name, map[string]string{"synth.go": src})
	_, err := MapExport(&DiscoverResult{Packages: []*ExplicitPackage{exp}, Providers: providers}, "")
	wantErr(t, err, contains...)
}

// excByName returns the exception record of one exact wire name.
func excByName(t *testing.T, model *ExportModel, wire string) *ExportException {
	t.Helper()
	for _, e := range model.Exceptions {
		if e.WireName == wire {
			return e
		}
	}
	t.Fatalf("no exception %q in the model", wire)
	return nil
}

// procByName returns the procedure record of one exact wire name.
func procByName(t *testing.T, model *ExportModel, wire string) *ExportProc {
	t.Helper()
	for _, p := range model.Procs {
		if p.WireName == wire {
			return p
		}
	}
	t.Fatalf("no procedure %q in the model", wire)
	return nil
}

// excWires renders the wire names of a model's exceptions.
func excWires(model *ExportModel) []string {
	var wires []string
	for _, e := range model.Exceptions {
		wires = append(wires, e.WireName)
	}
	return wires
}

// procWires renders the wire names of a model's procedures.
func procWires(model *ExportModel) []string {
	var wires []string
	for _, p := range model.Procs {
		wires = append(wires, p.WireName)
	}
	return wires
}

// bodyRoundTrip fails unless the model's canonical body parses,
// attaches its documentation, validates, and reformats to the identical
// bytes, and its declarations follow the exact type/exception/procedure
// emission order.
func bodyRoundTrip(t *testing.T, model *ExportModel) {
	t.Helper()
	body := model.CanonicalBody()
	f, err := syntax.Parse("canonical", body)
	if err != nil {
		t.Fatalf("parsing the canonical body: %v", err)
	}
	syntax.AttachDocs(f)
	if err := syntax.Validate(f); err != nil {
		t.Fatalf("validating the canonical body: %v", err)
	}
	if got := syntax.Format(f); !bytes.Equal(got, body) {
		t.Errorf("canonical body does not survive the parse-validate-format round trip:\n%s", body)
	}
	wantKinds := []string{}
	for range model.Types {
		wantKinds = append(wantKinds, "type")
	}
	for range model.Exceptions {
		wantKinds = append(wantKinds, "exception")
	}
	for range model.Procs {
		wantKinds = append(wantKinds, "procedure")
	}
	var gotKinds []string
	for _, d := range f.Decls {
		switch d.(type) {
		case *syntax.TypeDecl:
			gotKinds = append(gotKinds, "type")
		case *syntax.ExceptionDecl:
			gotKinds = append(gotKinds, "exception")
		case *syntax.ProcDecl:
			gotKinds = append(gotKinds, "procedure")
		}
	}
	if !reflect.DeepEqual(gotKinds, wantKinds) {
		t.Errorf("declaration kinds = %v, want %v", gotKinds, wantKinds)
	}
}

// TestApplicationExceptions covers the sentinel and payload exception
// forms, the *T error implementation, exception/type role conflicts,
// collection from every explicit package regardless of procedure
// filters, the direct equality/assertion and fallback matching facts,
// the exact fixed exception shapes and keys, global collisions, and the
// documentation slots.
func TestApplicationExceptions(t *testing.T) {
	t.Run("SentinelForms", func(t *testing.T) {
		model := exportOne(t, "example.com/synth", `package synth

import (
	"context"
	"errors"
)

// ErrDenied is the denied sentinel.
// @intercall exception
var ErrDenied = errors.New("denied")

// @intercall exception refused
var ErrRefused = errors.New("refused")

// @intercall exception typed
var ErrTyped error = errors.New("typed")

// @intercall procedure use
func Use(ctx context.Context) error { return nil }
`)
		// The default projection converts the complete identifier and
		// never strips the Err affix.
		e := excByName(t, model, "err_denied")
		if e.Form != SentinelForm || e.Match != MatchEquality || e.Payload != nil {
			t.Errorf("err_denied record = form %v match %v payload %v, want sentinel equality without payload", e.Form, e.Match, e.Payload)
		}
		if e.GoName != "ErrDenied" || e.Doc != "ErrDenied is the denied sentinel." || e.Fixed {
			t.Errorf("err_denied record = %+v", e)
		}
		if e.Key != syntax.ExceptionKey("err_denied") {
			t.Errorf("err_denied key = %#x, want %#x", e.Key, syntax.ExceptionKey("err_denied"))
		}
		// The directive wire name replaces the projection, and an
		// explicitly typed error variable is assignable to error.
		if e := excByName(t, model, "refused"); e.GoName != "ErrRefused" {
			t.Errorf("refused GoName = %q", e.GoName)
		}
		if e := excByName(t, model, "typed"); e.Form != SentinelForm || e.Match != MatchEquality || e.GoName != "ErrTyped" {
			t.Errorf("typed record = %+v", e)
		}
		if got := excWires(model); len(got) != 6 {
			t.Errorf("exceptions = %v, want three application plus three fixed", got)
		}
	})

	t.Run("PayloadForms", func(t *testing.T) {
		model := exportOne(t, "example.com/synth", `package synth

import "context"

// Failed carries the failure details.
// @intercall exception failed
type Failed struct {
	// The failure code.
	Code    int32
	Message string
}

// Error implements error for Failed.
func (f *Failed) Error() string { return "failed" }

// @intercall exception empty
type EmptyErr struct{}

// Error implements error for EmptyErr.
func (e *EmptyErr) Error() string { return "empty" }

// @intercall exception value_recv
type ValueErr struct {
	Code int32
}

// Error implements error for ValueErr with a value receiver, which the
// pointer type inherits.
func (e ValueErr) Error() string { return "value_recv" }

// @intercall procedure use
func Use(ctx context.Context) error { return nil }
`)
		e := excByName(t, model, "failed")
		if e.Form != PayloadForm || e.Match != MatchAssertion {
			t.Errorf("failed record = form %v match %v, want payload assertion", e.Form, e.Match)
		}
		if e.Doc != "Failed carries the failure details." || e.GoName != "Failed" {
			t.Errorf("failed record = %+v", e)
		}
		if e.Payload == nil || typeKeyOf(e.Payload.Type) != "record{code int32;message string}" {
			t.Fatalf("failed payload = %+v", e.Payload)
		}
		rec := e.Payload.Type.(*syntax.RecordType)
		if rec.Fields[0].Doc != "The failure code." {
			t.Errorf("failed field doc = %q, want %q", rec.Fields[0].Doc, "The failure code.")
		}
		if e.Payload.ZeroWidth {
			t.Errorf("failed payload: ZeroWidth = true, want false")
		}
		// struct{} maps to record {} with the zero-width fact.
		if e := excByName(t, model, "empty"); e.Payload == nil || typeKeyOf(e.Payload.Type) != "record{}" || !e.Payload.ZeroWidth {
			t.Errorf("empty record = %+v", e)
		}
		// A value-receiver Error method still makes *T implement error.
		if e := excByName(t, model, "value_recv"); e.Form != PayloadForm || e.Match != MatchAssertion {
			t.Errorf("value_recv record = %+v", e)
		}
	})

	t.Run("ErrorImplementation", func(t *testing.T) {
		exportErr(t, "example.com/synth", `package synth

// @intercall exception no_method
type NoMethod struct {
	Code int32
}
`, "*NoMethod must implement error")
		exportErr(t, "example.com/synth", `package synth

// @intercall exception wrong_sig
type WrongSig struct {
	Code int32
}

// Error has the wrong result type.
func (w *WrongSig) Error() int { return 1 }
`, "*WrongSig must implement error")
	})

	t.Run("SentinelAssignability", func(t *testing.T) {
		exportErr(t, "example.com/synth", `package synth

// @intercall exception not_err
var ErrNotError string
`, "not assignable to error")
	})

	t.Run("RoleConflicts", func(t *testing.T) {
		t.Run("DoubleDirective", func(t *testing.T) {
			// A type carrying both the type and the exception
			// directive cannot be an ordinary named wire type and an
			// exception struct at once.
			exportErr(t, "example.com/synth", `package synth

// @intercall type
// @intercall exception both
type Both struct {
	X string
}

// Error implements error for Both.
func (b *Both) Error() string { return "both" }
`, "cannot be both an ordinary named wire type and an exception struct")
		})
		t.Run("AsProcedureValue", func(t *testing.T) {
			exportErr(t, "example.com/synth", `package synth

import "context"

// @intercall exception exc
type Exc struct {
	X string
}

// Error implements error for Exc.
func (e *Exc) Error() string { return "exc" }

// @intercall procedure use
func Use(ctx context.Context, e Exc) error { return nil }
`, `type "Exc" is a tagged application exception struct and cannot occur as a procedure value or wire-type reference`)
		})
		t.Run("AsTypeField", func(t *testing.T) {
			// A tagged ordinary type whose record references an
			// exception struct is a role conflict.
			exportErr(t, "example.com/synth", `package synth

import "context"

// @intercall exception exc
type Exc struct {
	X string
}

// Error implements error for Exc.
func (e *Exc) Error() string { return "exc" }

// @intercall type holder
type Holder struct {
	E Exc
}

// @intercall procedure use
func Use(ctx context.Context, h Holder) error { return nil }
`, `type "Exc" is a tagged application exception struct and cannot occur as a procedure value or wire-type reference`)
		})
		t.Run("PayloadFieldRejectsEmbedding", func(t *testing.T) {
			exportErr(t, "example.com/synth", `package synth

// @intercall exception bad
type Bad struct {
	Inner
	X string
}

// Error implements error for Bad.
func (b *Bad) Error() string { return "bad" }

type Inner struct {
	Y string
}
`, "embedded fields are not wire fields")
		})
	})

	t.Run("FixedNameReservation", func(t *testing.T) {
		// The fixed runtime exception names are reserved across the
		// global InterCall declaration namespace: an exception, type,
		// or procedure may not use one, whether by directive or by
		// default projection.
		exportErr(t, "example.com/synth", `package synth

// @intercall exception procedure_not_found
var ErrPNF error
`, `reserved for a fixed runtime exception`)
		exportErr(t, "example.com/synth", `package synth

// @intercall exception
var InternalException error
`, `reserved for a fixed runtime exception`)
		exportErr(t, "example.com/synth", `package synth

import "context"

// @intercall type
type ProcedureNotFound struct {
	X string
}

// @intercall procedure use
func Use(ctx context.Context, p ProcedureNotFound) error { return nil }
`, `reserved for a fixed runtime exception`)
		exportErr(t, "example.com/synth", `package synth

import "context"

// @intercall procedure invalid_arguments
func InvalidArguments(ctx context.Context) error { return nil }
`, `reserved for a fixed runtime exception`)
	})

	t.Run("FixedShapesAndKeys", func(t *testing.T) {
		// The exact keys from SPEC.md "Fixed Go Runtime Exceptions".
		if got := syntax.ExceptionKey("procedure_not_found"); got != fixedProcedureNotFound {
			t.Errorf("procedure_not_found key = %#x, want %#x", got, fixedProcedureNotFound)
		}
		if got := syntax.ExceptionKey("invalid_arguments"); got != fixedInvalidArguments {
			t.Errorf("invalid_arguments key = %#x, want %#x", got, fixedInvalidArguments)
		}
		if got := syntax.ExceptionKey("internal_exception"); got != fixedInternalException {
			t.Errorf("internal_exception key = %#x, want %#x", got, fixedInternalException)
		}
		model := exportOne(t, "example.com/synth", `package synth

import "context"

// @intercall procedure use
func Use(ctx context.Context) error { return nil }
`)
		for _, name := range fixedExceptionNames {
			e := excByName(t, model, name)
			if !e.Fixed || e.Form != SentinelForm || e.Match != MatchRuntime || e.Payload != nil || e.Doc != "" || e.GoName != "" || e.Pkg != nil {
				t.Errorf("fixed %s record = %+v, want a no-payload runtime record without a Go symbol", name, e)
			}
			if e.Key != syntax.ExceptionKey(name) {
				t.Errorf("fixed %s key = %#x, want %#x", name, e.Key, syntax.ExceptionKey(name))
			}
		}
	})

	t.Run("FallbackFacts", func(t *testing.T) {
		// The dispatch fallback facts of SPEC.md "Application
		// exceptions": zero matches, multiple matches, a wrapped error,
		// a typed-nil payload pointer, and a panic during matching all
		// select the fixed no-payload internal_exception; a data result
		// is ignored for a nonnil error, and an encoding failure of a
		// success value or matched payload also sends it.
		if InternalExceptionName != "internal_exception" {
			t.Errorf("InternalExceptionName = %q, want %q", InternalExceptionName, "internal_exception")
		}
		model := exportOne(t, "example.com/synth", `package synth

import (
	"context"
	"errors"
)

// @intercall exception denied
var ErrDenied = errors.New("denied")

// @intercall exception failed
type Failed struct {
	Code int32
}

// Error implements error for Failed.
func (f *Failed) Error() string { return "failed" }

// @intercall procedure use
func Use(ctx context.Context) error { return nil }
`)
		// Every application exception matches by direct equality or
		// direct assertion only, and the fallback target is the fixed
		// internal_exception with no payload and no match form.
		for _, e := range model.Exceptions {
			if e.Fixed && e.Match != MatchRuntime {
				t.Errorf("fixed %s match = %v, want MatchRuntime", e.WireName, e.Match)
			}
			if !e.Fixed && e.Match != MatchEquality && e.Match != MatchAssertion {
				t.Errorf("application %s match = %v, want direct equality or assertion", e.WireName, e.Match)
			}
		}
		if e := excByName(t, model, InternalExceptionName); !e.Fixed || e.Payload != nil {
			t.Errorf("fallback record = %+v, want the fixed no-payload internal_exception", e)
		}
	})

	t.Run("PayloadReachesNamedTypes", func(t *testing.T) {
		// Tagged ordinary types reached through an exception payload
		// become ordinary named wire types in the reachable graph.
		model := exportOne(t, "example.com/synth", `package synth

import "context"

// @intercall type detail
type Detail struct {
	Code int32
}

// @intercall exception failed
type Failed struct {
	Info Detail
	Tags []string
}

// Error implements error for Failed.
func (f *Failed) Error() string { return "failed" }

// @intercall procedure use
func Use(ctx context.Context) error { return nil }
`)
		var wires []string
		for _, rec := range model.Types {
			wires = append(wires, rec.WireName)
		}
		if !reflect.DeepEqual(wires, []string{"detail"}) {
			t.Errorf("types = %v, want [detail]", wires)
		}
		e := excByName(t, model, "failed")
		if e.Payload == nil || typeKeyOf(e.Payload.Type) != "record{info detail;tags list string}" {
			t.Errorf("failed payload = %+v", e.Payload)
		}
	})

	t.Run("PayloadRejectsExceptionReference", func(t *testing.T) {
		// A payload field referencing another exception struct is a
		// role conflict: exception structs are not wire types.
		exportErr(t, "example.com/synth", `package synth

// @intercall exception outer
type Outer struct {
	Inner Inner
}

// Error implements error for Outer.
func (o *Outer) Error() string { return "outer" }

// @intercall exception inner
type Inner struct {
	X string
}

// Error implements error for Inner.
func (i *Inner) Error() string { return "inner" }
`, `type "Inner" is a tagged application exception struct and cannot occur as a procedure value or wire-type reference`)
	})
}

// exportFixture is the integration module of the exception tests: a
// provider package with sentinel and payload exceptions and a
// generated-looking file, an exceptions-only package, and the output
// package.
var exportFixture = map[string]string{
	"go.mod": "module example.com/expmod\n\ngo 1.26.5\n",
	"prov/prov.go": `// Package prov holds providers with application exceptions.
package prov

import (
	"context"
	"errors"
)

// ErrDenied is the denied sentinel.
// @intercall exception denied
var ErrDenied = errors.New("denied")

// Failed carries the failure details.
// @intercall exception failed
type Failed struct {
	// The failure code.
	Code    int32
	Message string
}

// Error implements error for Failed.
func (f *Failed) Error() string { return "failed" }

// @intercall procedure find
func Find(ctx context.Context, query string) (int32, error) { return 0, nil }

// @intercall procedure remove
func Remove(ctx context.Context, id uint64) error { return nil }
`,
	"prov/gen.go": `// Code generated by intercall-go; DO NOT EDIT.

// Package prov holds a generated-looking declaration.
package prov

import "errors"

// @intercall exception gen_only
var ErrGenOnly = errors.New("gen_only")
`,
	"exconly/exconly.go": `// Package exconly contributes only exceptions.
package exconly

import "errors"

// @intercall exception overloaded
var ErrOverloaded = errors.New("overloaded")

// Quota is a payload exception.
// @intercall exception quota
type Quota struct {
	Limit uint32
}

// Error implements error for Quota.
func (q *Quota) Error() string { return "quota" }
`,
	"out/out.go": `// Package out is the output target.
package out

// Helper is an ordinary function.
func Helper() {}
`,
}

// TestApplicationExceptionsIntegration runs the exception collection
// over a real load: exceptions belong to the interface from every
// explicit package regardless of the procedure filters, generated files
// supply no exceptions, and payload records carry their field
// documentation.
func TestApplicationExceptionsIntegration(t *testing.T) {
	dir := writeFixture(t, exportFixture)

	t.Run("AllExplicitPackages", func(t *testing.T) {
		// The include filter selects only Find; the exceptions of the
		// exceptions-only package still belong to the interface.
		res, err := discover(t, dir, []string{"./prov", "./exconly"}, []string{"example.com/expmod/prov.Find"}, nil, "out")
		if err != nil {
			t.Fatalf("discover: %v", err)
		}
		model, err := MapExport(res, "example.com/expmod/out")
		if err != nil {
			t.Fatalf("MapExport: %v", err)
		}
		if got := procWires(model); !reflect.DeepEqual(got, []string{"find"}) {
			t.Errorf("procs = %v, want [find]", got)
		}
		want := []string{"denied", "failed", "internal_exception", "invalid_arguments", "overloaded", "procedure_not_found", "quota"}
		if got := excWires(model); !reflect.DeepEqual(got, want) {
			t.Errorf("exceptions = %v, want %v", got, want)
		}
		// The generated-looking file's exception is not collected.
		for _, w := range excWires(model) {
			if w == "gen_only" {
				t.Errorf("generated-file exception gen_only was collected")
			}
		}
		e := excByName(t, model, "failed")
		if e.Payload == nil || typeKeyOf(e.Payload.Type) != "record{code int32;message string}" {
			t.Errorf("failed payload = %+v", e.Payload)
		}
		rec := e.Payload.Type.(*syntax.RecordType)
		if rec.Fields[0].Doc != "The failure code." {
			t.Errorf("failed field doc = %q", rec.Fields[0].Doc)
		}
		bodyRoundTrip(t, model)
	})
}

// TestExportInterfaceModel covers the small export generation records,
// the canonical interface AST and its byte-exact canonical body, the
// key checks, the wire-name collisions, and the importability of
// application exception packages.
func TestExportInterfaceModel(t *testing.T) {
	t.Run("AssemblyAndCanonicalBody", func(t *testing.T) {
		model := exportOne(t, "example.com/synth", `package synth

import (
	"context"
	"errors"
)

// User is a user record.
// @intercall type user
type User struct {
	ID string
}

// ErrDenied is the denied sentinel.
// @intercall exception denied
var ErrDenied = errors.New("denied")

// @intercall exception failed
type Failed struct {
	// The failure code.
	Code int32
}

// Error implements error for Failed.
func (f *Failed) Error() string { return "failed" }

// Lookup finds a user.
// @intercall procedure lookup
// @param query The query text.
// @return A user id.
func Lookup(ctx context.Context, query string) (User, error) { return User{}, nil }

// @intercall procedure ping
func Ping(ctx context.Context) error { return nil }
`)

		// The assembled AST follows the exact emission order: reachable
		// types, exceptions by wire-name byte order, procedures by the
		// same order.
		var kinds []string
		var names []string
		for _, d := range model.AST.Decls {
			switch d := d.(type) {
			case *syntax.TypeDecl:
				kinds = append(kinds, "type")
				names = append(names, d.Name.Name)
			case *syntax.ExceptionDecl:
				kinds = append(kinds, "exception")
				names = append(names, d.Name.Name)
			case *syntax.ProcDecl:
				kinds = append(kinds, "procedure")
				names = append(names, d.Name.Name)
			}
		}
		wantKinds := []string{"type", "exception", "exception", "exception", "exception", "exception", "procedure", "procedure"}
		wantNames := []string{"user", "denied", "failed", "internal_exception", "invalid_arguments", "procedure_not_found", "lookup", "ping"}
		if !reflect.DeepEqual(kinds, wantKinds) || !reflect.DeepEqual(names, wantNames) {
			t.Errorf("AST = %v %v, want %v %v", kinds, names, wantKinds, wantNames)
		}

		// The records carry keys, wire names, and documentation.
		if p := procByName(t, model, "lookup"); p.Key != syntax.ProcedureKey("lookup") || p.Doc != "Lookup finds a user." {
			t.Errorf("lookup record = %+v", p)
		} else {
			if len(p.Params) != 1 || p.Params[0].WireName != "query" || p.Params[0].Doc != "The query text." {
				t.Errorf("lookup params = %+v", p.Params)
			}
			if p.Result == nil || typeKeyOf(p.Result.Type) != "user" {
				t.Errorf("lookup result = %+v", p.Result)
			}
		}
		if p := procByName(t, model, "ping"); len(p.Params) != 0 || p.Result != nil {
			t.Errorf("ping record = %+v", p)
		}

		// The canonical body is byte-exact: every supported
		// documentation slot and the fixed exception declarations.
		want := "/* User is a user record. */\n" +
			"type user record {\n    id string;\n};\n\n" +
			"/* ErrDenied is the denied sentinel. */\n" +
			"exception denied;\n\n" +
			"exception failed record {\n    /* The failure code. */\n    code int32;\n};\n\n" +
			"exception internal_exception;\n\n" +
			"exception invalid_arguments;\n\n" +
			"exception procedure_not_found;\n\n" +
			"/* Lookup finds a user. */\n" +
			"procedure lookup {\n    /* The query text. */\n    query string;\n}\n" +
			"/* A user id. */\n" +
			"user;\n\n" +
			"procedure ping {};\n"
		if got := string(model.CanonicalBody()); got != want {
			t.Errorf("canonical body = %q, want %q", got, want)
		}
		bodyRoundTrip(t, model)
	})

	t.Run("EmptyInterface", func(t *testing.T) {
		// Export inserts the three fixed runtime exceptions into every
		// interface, even an empty one.
		model, err := MapExport(&DiscoverResult{}, "")
		if err != nil {
			t.Fatalf("MapExport: %v", err)
		}
		if got := excWires(model); !reflect.DeepEqual(got, fixedExceptionNames) {
			t.Errorf("exceptions = %v, want %v", got, fixedExceptionNames)
		}
		want := "exception internal_exception;\n\nexception invalid_arguments;\n\nexception procedure_not_found;\n"
		if got := string(model.CanonicalBody()); got != want {
			t.Errorf("canonical body = %q, want %q", got, want)
		}
		bodyRoundTrip(t, model)
	})

	t.Run("KeyChecks", func(t *testing.T) {
		pos := func(line int) Position { return Position{Line: line, Column: 1} }
		t.Run("Collision", func(t *testing.T) {
			model := &ExportModel{Exceptions: []*ExportException{
				{WireName: "a", Key: 1, Filename: "f1", Pos: pos(1)},
				{WireName: "b", Key: 1, Filename: "f2", Pos: pos(2)},
			}}
			wantErr(t, checkInterfaceKeys(model), `key collision: exception "b" collides with exception "a"`)
		})
		t.Run("CrossKind", func(t *testing.T) {
			model := &ExportModel{
				Exceptions: []*ExportException{{WireName: "a", Key: 1, Filename: "f1", Pos: pos(1)}},
				Procs:      []*ExportProc{{WireName: "p", Key: 1, Filename: "f2", Pos: pos(2)}},
			}
			wantErr(t, checkInterfaceKeys(model), `key collision: procedure "p" collides with exception "a"`)
		})
		t.Run("KeyZero", func(t *testing.T) {
			model := &ExportModel{Procs: []*ExportProc{{WireName: "z", Key: 0, Filename: "f", Pos: pos(1)}}}
			wantErr(t, checkInterfaceKeys(model), `key of procedure "z" is 0, which is invalid`)
		})
		t.Run("FixedCollisionReportsAtApplication", func(t *testing.T) {
			// A later fixed declaration has no source position; the
			// collision reports at the application declaration.
			model := &ExportModel{Exceptions: []*ExportException{
				{WireName: "a", Key: 1, Filename: "f1", Pos: pos(1)},
				{WireName: "internal_exception", Key: 1, Fixed: true},
			}}
			err := checkInterfaceKeys(model)
			wantErr(t, err, `key collision: exception "internal_exception" collides with exception "a"`)
			if err.Error()[:2] != "f1" {
				t.Errorf("error = %q, want the application declaration's position", err)
			}
		})
	})

	t.Run("WireNameCollisions", func(t *testing.T) {
		t.Run("ProcedureVersusProcedure", func(t *testing.T) {
			exportErr(t, "example.com/synth", `package synth

import "context"

// @intercall procedure same
func F(ctx context.Context) error { return nil }

// @intercall procedure same
func G(ctx context.Context) error { return nil }
`, `wire name collision: procedures "F" and "G" both map to wire name "same"`)
		})
		t.Run("ProcedureVersusType", func(t *testing.T) {
			exportErr(t, "example.com/synth", `package synth

import "context"

// @intercall type user
type User struct {
	ID string
}

// @intercall procedure user
func Find(ctx context.Context, u User) error { return nil }
`, `wire name collision: type "User" and procedure "Find" both map to wire name "user"`)
		})
		t.Run("ProcedureVersusException", func(t *testing.T) {
			exportErr(t, "example.com/synth", `package synth

import (
	"context"
	"errors"
)

// @intercall exception denied
var ErrDenied = errors.New("denied")

// @intercall procedure denied
func F(ctx context.Context) error { return nil }
`, `wire name collision: exception "ErrDenied" and procedure "F" both map to wire name "denied"`)
		})
		t.Run("ExceptionVersusType", func(t *testing.T) {
			exportErr(t, "example.com/synth", `package synth

import "context"

// @intercall type detail
type Detail struct {
	X string
}

// @intercall exception detail
var ErrDetail error

// @intercall procedure use
func Use(ctx context.Context, d Detail) error { return nil }
`, `wire name collision: type "Detail" and exception "ErrDetail" both map to wire name "detail"`)
		})
		t.Run("ExceptionVersusException", func(t *testing.T) {
			exportErr(t, "example.com/synth", `package synth

// @intercall exception same
var ErrOne error

// @intercall exception same
var ErrTwo error
`, `wire name collision: exceptions "ErrOne" and "ErrTwo" both map to wire name "same"`)
		})
		t.Run("ParameterVersusParameter", func(t *testing.T) {
			exportErr(t, "example.com/synth", `package synth

import "context"

// @intercall procedure collide
// @intercall param a same
// @intercall param b same
func F(ctx context.Context, a int8, b int8) error { return nil }
`, `wire name collision: parameters "a" and "b" both map to wire name "same"`)
		})
		t.Run("PayloadReachedTypeVersusException", func(t *testing.T) {
			// A payload field reaches a named type whose wire name
			// collides with the exception's own wire name.
			exportErr(t, "example.com/synth", `package synth

// @intercall exception failed
type Failed struct {
	Info Detail
}

// Error implements error for Failed.
func (f *Failed) Error() string { return "failed" }

// @intercall type failed
type Detail struct {
	X string
}
`, `wire name collision: exception "Failed" and type "Detail" both map to wire name "failed"`)
		})
	})
}

// exportImportFixture is the integration module of the exception
// importability checks: an internal exception package, an output
// package outside and one inside the internal root, an exception
// declared in the output package itself, and an exception package that
// imports the output package.
var exportImportFixture = map[string]string{
	"go.mod": "module example.com/expimp\n\ngo 1.26.5\n",
	"sub/internal/hidden/hidden.go": `// Package hidden is internal to example.com/expimp/sub.
package hidden

import "errors"

// @intercall exception hidden_err
var ErrHidden = errors.New("hidden_err")
`,
	"sub/internal/out/out.go": `// Package out is the output target inside the internal root.
package out

// Helper is an ordinary function.
func Helper() {}
`,
	"out/out.go": `// Package out is the output target outside the internal root.
package out

// Helper is an ordinary function.
func Helper() {}
`,
	"self/self.go": `// Package self is the output package with an exception.
package self

import "errors"

// @intercall exception self_err
var ErrSelf = errors.New("self_err")
`,
	"cyc/cyc.go": `// Package cyc imports the output package.
package cyc

import (
	"errors"

	"example.com/expimp/out"
)

var _ = out.Helper

// @intercall exception cyc_err
var ErrCyc = errors.New("cyc_err")
`,
}

// TestExceptionImportability runs the application-exception package
// importability checks over a real load: the generated binding must be
// able to import every application exception package.
func TestExceptionImportability(t *testing.T) {
	dir := writeFixture(t, exportImportFixture)

	t.Run("InternalInvisible", func(t *testing.T) {
		res, err := discover(t, dir, []string{"./sub/internal/hidden"}, nil, nil, "out")
		if err != nil {
			t.Fatalf("discover: %v", err)
		}
		_, err = MapExport(res, "example.com/expimp/out")
		wantErr(t, err, "exception", "hidden_err", "internal and not visible", "example.com/expimp/out")
	})

	t.Run("InternalVisible", func(t *testing.T) {
		res, err := discover(t, dir, []string{"./sub/internal/hidden"}, nil, nil, "sub/internal/out")
		if err != nil {
			t.Fatalf("discover: %v", err)
		}
		model, err := MapExport(res, "example.com/expimp/sub/internal/out")
		if err != nil {
			t.Fatalf("MapExport: %v", err)
		}
		excByName(t, model, "hidden_err")
	})

	t.Run("OutputPackageSelf", func(t *testing.T) {
		res, err := discover(t, dir, []string{"./self"}, nil, nil, "self")
		if err != nil {
			t.Fatalf("discover: %v", err)
		}
		_, err = MapExport(res, "example.com/expimp/self")
		wantErr(t, err, "exception", "self_err", "would import its own package")
	})

	t.Run("ImportCycle", func(t *testing.T) {
		res, err := discover(t, dir, []string{"./cyc"}, nil, nil, "out")
		if err != nil {
			t.Fatalf("discover: %v", err)
		}
		_, err = MapExport(res, "example.com/expimp/out")
		wantErr(t, err, "exception", "cyc_err", "import cycle")
	})
}

// exportOrderFixture is the integration module of the emission-order
// tests: a dependent type chain with a cross-package base, exceptions
// declared in non-sorted order, and procedures declared in non-sorted
// order.
var exportOrderFixture = map[string]string{
	"go.mod": "module example.com/expord\n\ngo 1.26.5\n",
	"ord.go": `// Package ord holds a dependent type chain and mixed declarations.
package ord

import (
	"context"
	"errors"

	"example.com/expord/dep"
)

// @intercall type alpha
type Alpha struct {
	M Middle
}

// @intercall type middle
type Middle struct {
	Z Zeta
}

// @intercall type zeta
type Zeta struct {
	N int8
}

// @intercall type delta
type Delta struct {
	B dep.Base
}

// @intercall exception z_exc
var ErrZExc = errors.New("z_exc")

// @intercall exception a_exc
type AExc struct {
	Code int32
}

// Error implements error for AExc.
func (a *AExc) Error() string { return "a_exc" }

// @intercall procedure zproc
func ZProc(ctx context.Context) error { return nil }

// @intercall procedure aproc
func AProc(ctx context.Context, x int8) error { return nil }

// @intercall procedure use_all
func UseAll(ctx context.Context, a Alpha, d Delta, m Middle, z Zeta) error { return nil }
`,
	"dep/dep.go": `// Package dep holds the cross-package base type.
package dep

// @intercall type base
type Base struct {
	V string
}
`,
	"out/out.go": `// Package out is the output target.
package out

// Helper is an ordinary function.
func Helper() {}
`,
}

// TestExportOrder covers the stable emission order of SPEC.md
// "Deterministic export order": reachable ordinary named types in the
// stable topological order, then all exceptions by exact wire-name byte
// order, then all procedures by the same order — independent of Go
// declaration order, package loading order, and repeated passes.
func TestExportOrder(t *testing.T) {
	dir := writeFixture(t, exportOrderFixture)

	load := func(t *testing.T) *ExportModel {
		t.Helper()
		res, err := discover(t, dir, []string{".", "./dep"}, nil, nil, "out")
		if err != nil {
			t.Fatalf("discover: %v", err)
		}
		model, err := MapExport(res, "example.com/expord/out")
		if err != nil {
			t.Fatalf("MapExport: %v", err)
		}
		return model
	}

	first := load(t)
	wantTypes := []string{"base", "delta", "zeta", "middle", "alpha"}
	var gotTypes []string
	for _, rec := range first.Types {
		gotTypes = append(gotTypes, rec.WireName)
	}
	if !reflect.DeepEqual(gotTypes, wantTypes) {
		t.Errorf("types = %v, want %v", gotTypes, wantTypes)
	}
	wantExc := []string{"a_exc", "internal_exception", "invalid_arguments", "procedure_not_found", "z_exc"}
	if got := excWires(first); !reflect.DeepEqual(got, wantExc) {
		t.Errorf("exceptions = %v, want %v", got, wantExc)
	}
	wantProc := []string{"aproc", "use_all", "zproc"}
	if got := procWires(first); !reflect.DeepEqual(got, wantProc) {
		t.Errorf("procs = %v, want %v", got, wantProc)
	}
	// The payload exception carries its record through the sort.
	if e := excByName(t, first, "a_exc"); e.Payload == nil || typeKeyOf(e.Payload.Type) != "record{code int32}" {
		t.Errorf("a_exc payload = %+v", e.Payload)
	}
	bodyRoundTrip(t, first)

	// A second complete pass produces the identical orders and the
	// identical canonical body.
	second := load(t)
	if !reflect.DeepEqual(excWires(second), wantExc) || !reflect.DeepEqual(procWires(second), wantProc) {
		t.Errorf("second pass order differs: %v %v", excWires(second), procWires(second))
	}
	if !bytes.Equal(second.CanonicalBody(), first.CanonicalBody()) {
		t.Error("second pass canonical body differs from the first")
	}
}
