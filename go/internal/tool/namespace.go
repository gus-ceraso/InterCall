package tool

import (
	"fmt"

	"github.com/cerasos/intercall/go/internal/syntax"
)

// This file implements the complete generated-name reservation of
// SPEC.md "Names and native overrides" for the import model: every
// identifier an import binding emits — the package scope, the
// exception struct member scope, and the request-encoder and caller
// parameter/local scopes — is reserved before any emission. A
// user-projected name that collides with a reserved generated name is
// an error rather than a silent escape, and the reserved private
// names are deterministic.
//
// User declaration names are always exported, so the package scope is
// collision-free by construction except for the exported ImportBinding
// accessor; the check nevertheless validates the complete scope so the
// reservation model is exact. User parameter names appear in the
// request encoder and the caller, whose bodies reference the mangled
// private locals, the per-procedure encoder and decoder, the
// importBinding singleton, the intercall runtime package, and the
// predeclared identifiers the closures and type declarations use; a
// parameter projection equal to any of these breaks the generated
// code by capture or shadowing, so every one is reserved.

// primitiveKinds is the README primitive-table order shared by the
// primitive pair emitters and the generated-name reservation.
var primitiveKinds = []syntax.TokenKind{
	syntax.TokInt8, syntax.TokInt16, syntax.TokInt32, syntax.TokInt64,
	syntax.TokUint8, syntax.TokUint16, syntax.TokUint32, syntax.TokUint64,
	syntax.TokFloat32, syntax.TokFloat64, syntax.TokString, syntax.TokBytes,
}

// errorMethodName is the fixed name of the generated Error method of
// every payload exception struct. A Go compiler rejects a struct whose
// field and method share a name, so an exception payload field may
// never project to it.
const errorMethodName = "Error"

// importPackageScope returns the complete set of package-level
// identifiers one import binding generates for the model, each mapped
// to the role description used in diagnostics. The set contains the
// ImportBinding accessor, the importBinding singleton variable, the
// fixed _intercallSemantic constant, every mangled codec support and
// response support name, every codec pair name, every exception
// payload decoder name, and every request encoder and response
// decoder name.
func (m *Model) importPackageScope() map[string]string {
	scope := map[string]string{
		importAccessorName:   "the generated ImportBinding accessor",
		importBindingName:    "the generated importBinding singleton variable",
		semanticConstantName: "the generated _intercallSemantic constant",
		errTrailingName:      "a generated response-decoder failure value",
		errUnknownName:       "a generated response-decoder failure value",
		maxIntName:           "a generated codec support value",
		errShortName:         "a generated codec failure value",
		errLongName:          "a generated codec failure value",
		errNaNName:           "a generated codec failure value",
		errUTF8Name:          "a generated codec failure value",
	}
	for _, k := range primitiveKinds {
		scope[codecName("enc", k.String())] = "a generated codec pair"
		scope[codecName("dec", k.String())] = "a generated codec pair"
	}
	for _, t := range m.Types {
		scope[codecName("enc", namedParts(t.Decl.Name.Name)...)] = "a generated codec pair"
		scope[codecName("dec", namedParts(t.Decl.Name.Name)...)] = "a generated codec pair"
	}
	e := newCodecEmitter(nil, m)
	e.register()
	for _, key := range e.order {
		scope[codecName("enc", key)] = "a generated codec pair"
		scope[codecName("dec", key)] = "a generated codec pair"
	}
	for _, x := range m.Exceptions {
		if x.Decl.Type == nil || IsFixedRuntimeException(x.Decl.Name.Name) {
			continue
		}
		if _, ok := x.Decl.Type.(*syntax.RecordType); ok {
			scope[codecName("decexc", x.Decl.Name.Name)] = "a generated exception payload decoder"
		}
	}
	for _, p := range m.Procs {
		scope[codecName("encreq", p.Decl.Name.Name)] = "a generated request encoder"
		scope[codecName("decresp", p.Decl.Name.Name)] = "a generated response decoder"
	}
	return scope
}

// importParamScope returns the complete set of identifiers one
// procedure's request encoder and caller reference in their bodies and
// nested closure signatures, each mapped to the role description used
// in diagnostics. The procedure's parameter projections all live in
// both function scopes, so every name the two bodies could capture or
// shadow is reserved: the mangled private parameters and locals, the
// per-procedure encoder and decoder, the importBinding singleton and
// the intercall runtime package the caller references, the
// predeclared identifiers of the closure signatures and result locals,
// the encoder of every parameter's own wire type, and every named or
// predeclared identifier of the result type text.
func (m *Model) importParamScope(p *ProcRec) map[string]string {
	scope := map[string]string{
		bufName:                                "the request encoder's buffer parameter",
		encErrName:                             "the request encoder's error local",
		ctxName:                                "the caller's context parameter",
		connName:                               "the caller's connection local",
		callErrName:                            "the caller's error local",
		outName:                                "the caller's result local",
		excName:                                "the caller's exception local",
		zeroName:                               "the caller's zero-value local",
		importBindingName:                      "the importBinding singleton variable referenced by the caller",
		"intercall":                            "the intercall runtime package referenced by the caller",
		"error":                                "the predeclared error type referenced by the request encoder, the caller, and the response-decoder closures",
		"byte":                                 "the predeclared byte type referenced by the response-decoder closures",
		"uint64":                               "the predeclared uint64 type referenced by the response-decoder closures",
		"nil":                                  "the predeclared nil identifier referenced by the request encoder and the caller",
		codecName("encreq", p.Decl.Name.Name):  "the request encoder referenced by the caller",
		codecName("decresp", p.Decl.Name.Name): "the response decoder referenced by the caller",
	}
	for _, pr := range p.Params {
		scope[encCall(pr.Type.Type, m)] = "the encoder of a parameter's own wire type, referenced by the request encoder"
	}
	if p.Result != nil {
		typeIdentifiers(p.Result.Type, m.names, m.types, scope)
	}
	return scope
}

// typeIdentifiers records every Go identifier the emitted type text of
// one occurrence references: predeclared primitives (with byte for the
// bytes form) and named declaration projections. Anonymous struct
// field names are declared inside the struct type's own scope and are
// never referenced by the caller body.
func typeIdentifiers(t syntax.TypeExpr, names *Names, types map[string]*syntax.TypeDecl, out map[string]string) {
	switch t := t.(type) {
	case *syntax.PrimType:
		if t.Kind == syntax.TokBytes {
			out["byte"] = "the predeclared byte type referenced by the caller's result local"
		} else {
			out[t.Kind.String()] = fmt.Sprintf("the predeclared %s type referenced by the caller's result local", t.Kind)
		}
	case *syntax.NamedType:
		name := names.Decl[types[t.Name.Name]]
		out[name] = fmt.Sprintf("the generated type %s referenced by the caller's result local", name)
	case *syntax.ListType:
		typeIdentifiers(t.Elem, names, types, out)
	case *syntax.RecordType:
		for _, f := range t.Fields {
			typeIdentifiers(f.Type, names, types, out)
		}
	}
}

// reserveImportScopes validates every user-projected name of one
// import model against the reserved package, exception-member, and
// parameter/local scopes, reporting the first collision in declaration
// order. The check runs on the generation records before any emission,
// so a namespace failure never reaches the artifact layer.
func reserveImportScopes(m *Model) error {
	pkg := m.importPackageScope()
	checkDecl := func(d syntax.Decl, kind, wire string) error {
		name, ok := m.names.Decl[d]
		if !ok {
			return nil // fixed runtime exceptions generate no symbol
		}
		if why, reserved := pkg[name]; reserved {
			return fmt.Errorf("Go declaration name collision: %s %q projects to %q, which is reserved for %s", kind, wire, name, why)
		}
		return nil
	}
	for _, t := range m.Types {
		if err := checkDecl(t.Decl, "type", t.Decl.Name.Name); err != nil {
			return err
		}
	}
	for _, x := range m.Exceptions {
		if IsFixedRuntimeException(x.Decl.Name.Name) {
			continue
		}
		if err := checkDecl(x.Decl, "exception", x.Decl.Name.Name); err != nil {
			return err
		}
		if x.Decl.Type == nil {
			continue
		}
		rt, ok := x.Decl.Type.(*syntax.RecordType)
		if !ok {
			continue // a wrapper struct whose fixed Payload field is generated
		}
		for _, f := range rt.Fields {
			if name := m.names.Field[f]; name == errorMethodName {
				return fmt.Errorf("Go field name collision: field %q of exception %q projects to %q, which is reserved for the generated Error method", f.Name.Name, x.Decl.Name.Name, name)
			}
		}
	}
	for _, p := range m.Procs {
		if err := checkDecl(p.Decl, "procedure", p.Decl.Name.Name); err != nil {
			return err
		}
		scope := m.importParamScope(p)
		for _, pr := range p.Params {
			if why, reserved := scope[pr.GoName]; reserved {
				return fmt.Errorf("Go parameter name collision: parameter %q of procedure %q projects to %q, which is reserved for %s", pr.Decl.Name.Name, p.Decl.Name.Name, pr.GoName, why)
			}
		}
	}
	return nil
}
