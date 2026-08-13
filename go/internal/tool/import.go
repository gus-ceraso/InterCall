package tool

import (
	"encoding/base64"
	"fmt"
	"go/format"
	"strings"

	"github.com/cerasos/intercall/go/internal/syntax"
)

// This file implements SPEC.md "Go Import Model" and the import side of
// "Safe import and re-export metadata" and "Generated Binding SPI and
// Runtime": the import generation records with --go-name overrides, the
// fixed runtime exception reservation, the one canonical chunked
// base64url `_intercallSemantic` constant, the generated named types and
// anonymous inline record declarations, the application and fixed
// exception symbols, the per-procedure request encoders and response
// decoders, the generated callers, and the immutable import binding
// singleton.
//
// The entry point GenerateImport parses and validates the exact input
// interface bytes, attaches documentation, renders the canonical
// interface body, builds the generation records, and emits the complete
// binding Go file after the ownership lines (the artifact layer composes
// and writes those lines). The binding handle is the simplified
// architecture's descriptor replacement: there is no descriptor schema,
// client object, registry, or runtime digest.

// semanticChunkSize is the exact maximum byte length of one quoted
// base64url chunk of the _intercallSemantic constant. The base64url value
// is split left to right into chunks of at most this size, and every
// nonfinal chunk has exactly this size (SPEC.md "Safe import and
// re-export metadata").
const semanticChunkSize = 4096

// Import binding support names. The request encoders and callers take
// user-projected parameter names, so every helper local, the buffer
// parameter, and the context parameter use IC-08's deterministic private
// mangling: a wire parameter may project to any unexported Go name, and
// a fixed plain helper name could collide with it. Package-level helper
// names below never collide with user identifiers, because every
// user-generated package-level symbol is exported.
var (
	errTrailingName = ManglePrivate("error", "trailing")
	errUnknownName  = ManglePrivate("error", "unknown", "key")
	ctxName         = ManglePrivate("ctx")
	connName        = ManglePrivate("conn")
	encErrName      = ManglePrivate("enc", "err")
	callErrName     = ManglePrivate("call", "err")
	outName         = ManglePrivate("out")
	excName         = ManglePrivate("exc")
	zeroName        = ManglePrivate("zero")
	bufName         = ManglePrivate("buf")
)

// MapImport builds the generation records of one parsed and validated
// import interface with the given --go-name overrides.
//
// The file must come from syntax.Parse and syntax.Validate; MapImport
// does not re-run protocol validation. It applies the import-specific
// fixed runtime exception reservation of SPEC.md "Go Import Model" and
// "Fixed Go Runtime Exceptions": the three fixed names are reserved
// across the global declaration namespace, so a fixed name used by a
// type or procedure declaration, or by an exception with a payload, is
// an error. A fixed no-payload exception declaration is accepted and
// maps to a root-runtime sentinel; an interface may omit all three.
// MapImport reports the first reservation, naming, or fact error, which
// is deterministic for a given file.
func MapImport(f *syntax.File, overrides []Override) (*Model, error) {
	// The strict Go projection depth preflight runs before any
	// recursive override resolution, naming, model, codec, or emission
	// work and reports the first over-limit interface occurrence at its
	// exact source position (SPEC.md "Strict Go projection depth").
	if err := checkSyntaxProjectionDepth(f); err != nil {
		return nil, err
	}
	if err := checkImportFixed(f); err != nil {
		return nil, err
	}
	return buildModel(f, overrides)
}

// checkImportFixed rejects every use of a fixed runtime exception name
// outside its required shape: a no-payload exception declaration. The
// fixed names are reserved across the global InterCall declaration
// namespace, so a type or procedure declaration with a fixed name, or an
// exception declaration with a fixed name and a payload, is an error at
// the declaration's exact position.
func checkImportFixed(f *syntax.File) error {
	for _, d := range f.Decls {
		switch d := d.(type) {
		case *syntax.TypeDecl:
			if IsFixedRuntimeException(d.Name.Name) {
				return fixedNameError(f, d.Name, "type", "type declarations have no generated runtime sentinel")
			}
		case *syntax.ExceptionDecl:
			if IsFixedRuntimeException(d.Name.Name) && d.Type != nil {
				return fixedNameError(f, d.Name, "exception", "the fixed runtime exceptions are no-payload exceptions and cannot carry a payload")
			}
		case *syntax.ProcDecl:
			if IsFixedRuntimeException(d.Name.Name) {
				return fixedNameError(f, d.Name, "procedure", "procedure declarations have no generated runtime sentinel")
			}
		}
	}
	return nil
}

// fixedNameError builds one import reservation diagnostic at the exact
// source position of the offending declaration name.
func fixedNameError(f *syntax.File, id *syntax.Ident, kind, why string) *Error {
	pos := f.Position(id.Span().Start)
	return &Error{
		Filename: f.Name,
		Pos:      Position{Offset: pos.Offset, Line: pos.Line, Column: pos.Column},
		Msg: fmt.Sprintf("%s %q uses the fixed runtime exception name %q, which is reserved: %s",
			kind, id.Name, id.Name, why),
	}
}

// GenerateImport parses, validates, and renders one complete import
// binding from the exact interface input bytes, applying the given
// --go-name overrides.
//
// The input is parsed and validated exactly; documentation is attached
// and the canonical interface body is rendered with the IC-06 semantic
// formatter, so formatting and unattached comments are absent from the
// body while every attached declaration, parameter, field, and nested
// type document remains. The returned goFile is the complete generated
// binding after the ownership lines: its package clause and body,
// formatted and valid Go. The returned interfaceBody is the byte-exact
// canonical interface body whose SHA-256 digest is the artifact stamp.
// The two values are exactly what the artifact layer composes into a
// stamped binding file.
func GenerateImport(filename string, src []byte, overrides []Override, pkg string) (goFile []byte, interfaceBody []byte, err error) {
	f, err := syntax.Parse(filename, src)
	if err != nil {
		return nil, nil, err
	}
	syntax.AttachDocs(f)
	if err := syntax.Validate(f); err != nil {
		return nil, nil, err
	}
	body := syntax.Format(f)
	m, err := MapImport(f, overrides)
	if err != nil {
		return nil, nil, err
	}
	goFile, err = generateImportFile(pkg, body, m)
	if err != nil {
		return nil, nil, err
	}
	return goFile, body, nil
}

// generateImportFile renders one complete generated import binding file:
// the package clause, the fixed import set (context only when the
// interface has procedures), the one _intercallSemantic constant, the
// codec support and response support, the named type declarations with
// their exact machine lines, the exception symbols, the exception
// payload decoders, every codec pair, the per-procedure request
// encoders, response decoders, and callers, and the immutable import
// binding singleton. The output is formatted with go/format.
func generateImportFile(pkg string, body []byte, m *Model) ([]byte, error) {
	var src source
	src.linef("package %s", pkg)
	src.blank()
	src.linef("import (")
	src.open()
	src.linef(`"errors"`)
	if len(m.Procs) > 0 {
		src.linef(`"context"`)
	}
	src.linef(`"github.com/cerasos/intercall/go"`)
	src.linef(`"math"`)
	src.linef(`"unicode/utf8"`)
	src.close()
	src.linef(")")
	src.blank()
	emitSemanticConstant(&src, body)
	emitImportSupport(&src)

	e := newCodecEmitter(&src, m)
	e.register()
	e.emitTypeDecls()
	emitImportExceptions(&src, m)
	emitImportExceptionDecoders(&src, m)
	e.emitPrimitivePairs()
	for _, t := range m.Types {
		e.emitNamedPair(t)
	}
	for _, key := range e.order {
		e.emitAnonPair(key)
	}
	for _, p := range m.Procs {
		emitImportProc(&src, m, p)
	}
	emitImportSingleton(&src)
	return format.Source(src.bytes())
}

// emitSemanticConstant emits the one _intercallSemantic constant whose
// value is the unpadded RFC 4648 base64url encoding of the canonical
// interface body, split left to right into quoted ASCII chunks of at
// most semanticChunkSize bytes with every nonfinal chunk at exactly that
// size. A single chunk needs no '+', and the empty value is `""`.
func emitSemanticConstant(src *source, body []byte) {
	enc := base64.RawURLEncoding.EncodeToString(body)
	if enc == "" {
		src.linef("const %s = \"\"", semanticConstantName)
		return
	}
	if len(enc) <= semanticChunkSize {
		src.linef("const %s = %q", semanticConstantName, enc)
		return
	}
	src.linef("const %s = %q +", semanticConstantName, enc[:semanticChunkSize])
	for off := semanticChunkSize; off < len(enc); off += semanticChunkSize {
		end := off + semanticChunkSize
		if end > len(enc) {
			end = len(enc)
		}
		if end == len(enc) {
			src.linef("\t%q", enc[off:end])
		} else {
			src.linef("\t%q +", enc[off:end])
		}
	}
}

// emitImportSupport emits the shared response support after the codec
// support: the two response decoder failure values. The trailing error
// rejects a payload that is not exhausted exactly, and the unknown error
// rejects an undeclared exception key; both decoder failures terminate
// the connection through the runtime's matched-response handling.
func emitImportSupport(src *source) {
	emitCodecSupport(src)
	src.linef("var (")
	src.open()
	src.linef(`%s = errors.New("intercall: response payload has trailing bytes")`, errTrailingName)
	src.linef(`%s = errors.New("intercall: undeclared exception key")`, errUnknownName)
	src.close()
	src.linef(")")
	src.blank()
}

// emitImportExceptions emits the Go exception symbols of every non-fixed
// exception declaration in syntax order: a no-payload application
// exception becomes one exported sentinel whose Error string is exactly
// the wire name, an inline-record payload becomes an exported named
// error struct with the record fields directly (including a distinct
// zero-field type for record {}), and every other payload becomes an
// exported named error struct with one field
//
//	Payload <mapped-payload-type>
//
// Payload exceptions are returned as unwrapped, nonnil pointers, and
// their Error method returns the exact wire name regardless of payload.
// Fixed runtime exceptions have no generated symbol.
func emitImportExceptions(src *source, m *Model) {
	for _, x := range m.Exceptions {
		if IsFixedRuntimeException(x.Decl.Name.Name) {
			continue // mapped to a root-runtime sentinel, never a symbol
		}
		if x.Decl.Type == nil {
			src.linef("var %s error = errors.New(%q)", x.GoName, x.Decl.Name.Name)
			src.blank()
			continue
		}
		if rt, ok := x.Decl.Type.(*syntax.RecordType); ok {
			if len(rt.Fields) == 0 {
				src.linef("type %s struct{}", x.GoName)
			} else {
				src.linef("type %s struct {", x.GoName)
				src.open()
				for _, f := range rt.Fields {
					src.linef("%s %s `intercall:%q`", m.names.Field[f], goTypeOf(f.Type, m.names, m.types), f.Name.Name)
				}
				src.close()
				src.linef("}")
			}
		} else {
			src.linef("type %s struct {", x.GoName)
			src.open()
			src.linef("Payload %s", goTypeOf(x.Decl.Type, m.names, m.types))
			src.close()
			src.linef("}")
		}
		src.linef("func (e *%s) Error() string { return %q }", x.GoName, x.Decl.Name.Name)
		src.blank()
	}
}

// emitImportExceptionDecoders emits one private decode delegate per
// inline-record payload exception. The shared codec pair of the payload
// record decodes into the anonymous record type, and the delegate
// converts it to the named error struct type; converting a decoded
// anonymous record to the exported struct type in the response decoder
// itself is impossible because a conversion result is not addressable.
// Non-record payloads decode directly to their mapped Go type and need
// no delegate.
func emitImportExceptionDecoders(src *source, m *Model) {
	for _, x := range m.Exceptions {
		if x.Decl.Type == nil || IsFixedRuntimeException(x.Decl.Name.Name) {
			continue
		}
		rt, ok := x.Decl.Type.(*syntax.RecordType)
		if !ok {
			continue
		}
		name := codecName("decexc", x.Decl.Name.Name)
		target := codecName("dec", typeKeyOf(rt))
		src.linef("func %s(src []byte) (%s, []byte, error) {", name, x.GoName)
		src.open()
		src.linef("v, src, err := %s(src)", target)
		src.linef("if err != nil {")
		src.open()
		src.linef("return %s{}, nil, err", x.GoName)
		src.close()
		src.linef("}")
		src.linef("return %s(v), src, nil", x.GoName)
		src.close()
		src.linef("}")
		src.blank()
	}
}

// emitImportProc emits one procedure's request encoder, response
// decoder, and caller in that order.
func emitImportProc(src *source, m *Model, p *ProcRec) {
	emitRequestEncoder(src, m, p)
	emitResponseDecoder(src, m, p)
	emitCaller(src, m, p)
}

// emitRequestEncoder emits the request encoder of one procedure:
//
//	func name(buf []byte, p1 T1, ..., pn Tn) ([]byte, error)
//
// appending the parameters in declaration order to an owned payload.
// Zero-width parameters occupy no wire bytes and are skipped. The buffer
// parameter and the error local are privately mangled because the
// parameter names are user-projected.
func emitRequestEncoder(src *source, m *Model, p *ProcRec) {
	name := codecName("encreq", p.Decl.Name.Name)
	params := []string{bufName + " []byte"}
	for _, pr := range p.Params {
		params = append(params, fmt.Sprintf("%s %s", pr.GoName, goTypeOf(pr.Type.Type, m.names, m.types)))
	}
	src.linef("func %s(%s) ([]byte, error) {", name, strings.Join(params, ", "))
	src.open()
	emitted := false
	for _, pr := range p.Params {
		if zeroWidthOf(pr.Type.Type, m.types) {
			continue
		}
		if !emitted {
			src.linef("var %s error", encErrName)
			emitted = true
		}
		src.linef("%s, %s = %s(%s, %s)", bufName, encErrName, encCall(pr.Type.Type, m), bufName, pr.GoName)
		src.linef("if %s != nil {", encErrName)
		src.open()
		src.linef("return nil, %s", encErrName)
		src.close()
		src.linef("}")
	}
	src.linef("return %s, nil", bufName)
	src.close()
	src.linef("}")
	src.blank()
}

// emitResponseDecoder emits the response decoder of one procedure:
//
//	func name(key uint64, payload []byte, out *T, exc *error) error
//
// (without out when the procedure has no return value). The switch
// accepts exception key zero as the procedure's return value, then every
// declared exception key in declaration order; every accepted arm
// consumes the payload exactly, storing the decoded result in out or the
// exception value in exc. The fixed runtime exceptions map to the shared
// root-runtime sentinels, a no-payload exception requires an empty
// payload, and a payload exception decodes its mapped payload. A
// nonempty payload for a no-payload exception or return, trailing bytes
// after a decoded value, and every undeclared key are decoder errors
// that terminate the connection through the runtime's matched-response
// handling.
func emitResponseDecoder(src *source, m *Model, p *ProcRec) {
	name := codecName("decresp", p.Decl.Name.Name)
	out := ""
	if p.Result != nil {
		out = fmt.Sprintf(", out *%s", goTypeOf(p.Result.Type, m.names, m.types))
	}
	src.linef("func %s(key uint64, payload []byte%s, exc *error) error {", name, out)
	src.open()
	src.linef("switch key {")
	src.open()
	if p.Result != nil {
		src.linef("case 0:")
		src.open()
		src.linef("v, rest, err := %s(payload)", decCall(p.Result.Type, m))
		src.linef("if err != nil {")
		src.open()
		src.linef("return err")
		src.close()
		src.linef("}")
		src.linef("if len(rest) != 0 {")
		src.open()
		src.linef("return %s", errTrailingName)
		src.close()
		src.linef("}")
		src.linef("*out = v")
		src.linef("return nil")
		src.close()
	} else {
		src.linef("case 0:")
		src.open()
		src.linef("if len(payload) != 0 {")
		src.open()
		src.linef("return %s", errTrailingName)
		src.close()
		src.linef("}")
		src.linef("return nil")
		src.close()
	}
	for _, x := range m.Exceptions {
		src.linef("case 0x%x:", x.Key)
		src.open()
		if x.Decl.Type == nil {
			src.linef("if len(payload) != 0 {")
			src.open()
			src.linef("return %s", errTrailingName)
			src.close()
			src.linef("}")
			if IsFixedRuntimeException(x.Decl.Name.Name) {
				src.linef("*exc = intercall.%s", fixedRuntimeGoName(x.Decl.Name.Name))
			} else {
				src.linef("*exc = %s", x.GoName)
			}
			src.linef("return nil")
			src.close()
			continue
		}
		if _, ok := x.Decl.Type.(*syntax.RecordType); ok {
			src.linef("v, rest, err := %s(payload)", codecName("decexc", x.Decl.Name.Name))
		} else {
			src.linef("v, rest, err := %s(payload)", decCall(x.Decl.Type, m))
		}
		src.linef("if err != nil {")
		src.open()
		src.linef("return err")
		src.close()
		src.linef("}")
		src.linef("if len(rest) != 0 {")
		src.open()
		src.linef("return %s", errTrailingName)
		src.close()
		src.linef("}")
		if _, ok := x.Decl.Type.(*syntax.RecordType); ok {
			src.linef("*exc = &v")
		} else {
			src.linef("*exc = &%s{Payload: v}", x.GoName)
		}
		src.linef("return nil")
		src.close()
	}
	src.linef("default:")
	src.open()
	src.linef("return %s", errUnknownName)
	src.close()
	src.close()
	src.linef("}")
	src.close()
	src.linef("}")
	src.blank()
}

// fixedRuntimeGoName maps one fixed runtime exception wire name to its
// exported root-runtime sentinel symbol.
func fixedRuntimeGoName(wire string) string {
	switch wire {
	case "procedure_not_found":
		return "ErrProcedureNotFound"
	case "invalid_arguments":
		return "ErrInvalidArguments"
	case "internal_exception":
		return "ErrInternalException"
	}
	panic("tool: unknown fixed runtime exception name " + wire)
}

// emitCaller emits one procedure's exported caller:
//
//	func P(ctx context.Context, p1 T1, ..., pn Tn) error
//	func P(ctx context.Context, p1 T1, ..., pn Tn) (T, error)
//
// The caller obtains the connection from the root runtime context with
// intercall.ConnectionFromContext and returns its exact error without
// constructing either closure's wire result when no connection is bound.
// It then calls the runtime with the package's immutable import binding,
// the procedure key, one request-encoder closure over the parameters,
// and one response-decoder closure over a result and exception storage
// local; a Call failure returns its exact error, and otherwise the
// caller returns the stored result and stored exception. The context
// parameter and every local are privately mangled because the parameter
// names are user-projected.
func emitCaller(src *source, m *Model, p *ProcRec) {
	params := []string{ctxName + " context.Context"}
	for _, pr := range p.Params {
		params = append(params, fmt.Sprintf("%s %s", pr.GoName, goTypeOf(pr.Type.Type, m.names, m.types)))
	}
	sig := strings.Join(params, ", ")
	if p.Result != nil {
		src.linef("func %s(%s) (%s, error) {", p.GoName, sig, goTypeOf(p.Result.Type, m.names, m.types))
	} else {
		src.linef("func %s(%s) error {", p.GoName, sig)
	}
	src.open()
	src.linef("%s, %s := intercall.ConnectionFromContext(%s)", connName, callErrName, ctxName)
	src.linef("if %s != nil {", callErrName)
	src.open()
	if p.Result != nil {
		src.linef("var %s %s", zeroName, goTypeOf(p.Result.Type, m.names, m.types))
		src.linef("return %s, %s", zeroName, callErrName)
	} else {
		src.linef("return %s", callErrName)
	}
	src.close()
	src.linef("}")
	if p.Result != nil {
		src.linef("var %s %s", outName, goTypeOf(p.Result.Type, m.names, m.types))
	}
	src.linef("var %s error", excName)
	encName := codecName("encreq", p.Decl.Name.Name)
	decName := codecName("decresp", p.Decl.Name.Name)
	args := ""
	if len(p.Params) > 0 {
		names := make([]string, 0, len(p.Params))
		for _, pr := range p.Params {
			names = append(names, pr.GoName)
		}
		args = ", " + strings.Join(names, ", ")
	}
	outArg := ""
	if p.Result != nil {
		outArg = fmt.Sprintf(", &%s", outName)
	}
	src.linef("%s = %s.Call(%s, importBinding, 0x%x, func() ([]byte, error) {", callErrName, connName, ctxName, p.Key)
	src.open()
	src.linef("return %s(nil%s)", encName, args)
	src.close()
	src.linef("}, func(key uint64, payload []byte) error {")
	src.open()
	src.linef("return %s(key, payload%s, &%s)", decName, outArg, excName)
	src.close()
	src.linef("})")
	src.linef("if %s != nil {", callErrName)
	src.open()
	if p.Result != nil {
		src.linef("var %s %s", zeroName, goTypeOf(p.Result.Type, m.names, m.types))
		src.linef("return %s, %s", zeroName, callErrName)
	} else {
		src.linef("return %s", callErrName)
	}
	src.close()
	src.linef("}")
	if p.Result != nil {
		src.linef("return %s, %s", outName, excName)
	} else {
		src.linef("return %s", excName)
	}
	src.close()
	src.linef("}")
	src.blank()
}

// emitImportSingleton emits the immutable import binding singleton: the
// package constructs its handle exactly once into an unexported package
// variable, and ImportBinding returns it. Copying the returned handle
// copies the pointer and retains identity.
func emitImportSingleton(src *source) {
	src.linef("var importBinding = intercall.NewImportBinding()")
	src.blank()
	src.linef("// ImportBinding returns the package's immutable import binding.")
	src.linef("func ImportBinding() intercall.ImportBinding {")
	src.open()
	src.linef("return importBinding")
	src.close()
	src.linef("}")
	src.blank()
}

// encCall returns the generated encoder function name of one type
// occurrence: the primitive, named, or shared anonymous pair the codec
// emitter emits.
func encCall(t syntax.TypeExpr, m *Model) string {
	switch t := t.(type) {
	case *syntax.PrimType:
		return codecName("enc", primitiveParts(t.Kind)...)
	case *syntax.NamedType:
		return codecName("enc", namedParts(t.Name.Name)...)
	case *syntax.ListType, *syntax.RecordType:
		return codecName("enc", typeKeyOf(t))
	}
	panic("tool: unknown type occurrence")
}

// decCall returns the generated decoder function name of one type
// occurrence, mirroring encCall.
func decCall(t syntax.TypeExpr, m *Model) string {
	switch t := t.(type) {
	case *syntax.PrimType:
		return codecName("dec", primitiveParts(t.Kind)...)
	case *syntax.NamedType:
		return codecName("dec", namedParts(t.Name.Name)...)
	case *syntax.ListType, *syntax.RecordType:
		return codecName("dec", typeKeyOf(t))
	}
	panic("tool: unknown type occurrence")
}
