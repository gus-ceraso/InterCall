package tool

import (
	"fmt"
	"go/format"
	"go/types"
	"sort"
	"strings"

	"github.com/cerasos/intercall/go/internal/syntax"
)

// This file implements SPEC.md "Go Export Model" and the export side of
// "Generated Binding SPI and Runtime": the export emitter and entry
// point that render one complete export binding from the generation
// records of IC-14 — the immutable export binding singleton, the static
// procedure key switch with the procedure_not_found and
// invalid_arguments fixed exceptions, the per-procedure request
// decoders, the response encoders through the export codec emitter, the
// direct application-exception matcher, and deterministic imports.
//
// The generated binding imports the provider, application exception,
// and reachable named-type packages with deterministic aliases and
// operates on the providers' own Go types: request decoders produce
// exactly the provider parameter types, response encoders consume the
// provider result and exception payload types, and the matcher compares
// a provider's nonnil error against every application exception with
// direct equality and direct assertion only. There is no reflection,
// registration, or callback layer: the static switch, the matcher, and
// the codecs are all emitted code, and the runtime's one recovery
// around the complete dispatch maps every escaped panic — a provider
// panic, a matching panic, or an encoding panic — to the fixed
// internal_exception.

// The fixed runtime exception keys the generated dispatch selects
// (SPEC.md "Fixed Go Runtime Exceptions"): an unknown static key
// receives procedure_not_found, a known procedure whose arguments are
// malformed or leave trailing bytes receives invalid_arguments without
// invoking the provider, and every fallback — zero or multiple
// matches, a wrapped error, a typed-nil payload pointer, an encoding
// failure, or a panic — receives internal_exception.
const (
	exportProcedureNotFoundKey uint64 = 0x970e76fcc5e2dacb
	exportInvalidArgumentsKey  uint64 = 0x3f5fc972f8477b07
	exportInternalExceptionKey uint64 = 0x1aaec22e85996f50
)

// The generated dispatch function parameters. The dispatch operates on
// no user-projected names, but the parameters are mangled for
// consistency with the import-side convention and to keep the function
// body free of any plain identifier that a future emission could
// collide with.
var (
	dispatchName        = ManglePrivate("dispatch")
	matcherName         = ManglePrivate("match", "exc")
	dispatchCtxName     = ManglePrivate("ctx")
	dispatchKeyName     = ManglePrivate("key")
	dispatchPayloadName = ManglePrivate("payload")
	exportBindingName   = ManglePrivate("binding")
)

// exportEmitter is the working state of one export binding emission.
type exportEmitter struct {
	model *ExportModel

	wireTypes map[string]*syntax.TypeDecl // exact wire name -> type declaration
	aliases   map[string]string           // import path -> deterministic alias
	names     map[string]string           // import path -> package name
	gtName    map[string]string           // exact wire name -> qualified Go name

	pairs *exportCodecEmitter

	// Occurrence facts of the model, resolved in the deterministic walk
	// that registers the anonymous codec pairs.
	params   map[*ExportParam]*exportOccurrence
	results  map[*ExportProc]*exportOccurrence
	payloads map[*ExportException]*exportOccurrence

	src source
}

// exportOccurrence is the codec fact set of one type occurrence of the
// export model: its Go type text and the codec pair names for its
// top-level type.
type exportOccurrence struct {
	gt      string
	encName string
	decName string
}

// GenerateExport renders one complete export binding from an export
// model, formatted and valid Go after the ownership lines.
//
// The model must come from MapExport; GenerateExport does not re-run
// discovery, mapping, or protocol validation. The returned goFile is
// the complete generated binding after the ownership lines, and the
// returned interfaceBody is the byte-exact canonical interface body
// whose SHA-256 digest is the artifact stamp — the two values are
// exactly what the artifact layer composes into a stamped export
// binding and its owned interface file.
func GenerateExport(m *ExportModel, pkg string) (goFile []byte, interfaceBody []byte, err error) {
	if m == nil {
		return nil, nil, fmt.Errorf("tool: internal error: no export model")
	}
	e := &exportEmitter{model: m}
	if err := e.prepare(); err != nil {
		return nil, nil, err
	}
	goFile, err = e.emit(pkg)
	if err != nil {
		return nil, nil, err
	}
	return goFile, m.CanonicalBody(), nil
}

// prepare resolves the deterministic import aliases, the qualified Go
// names of the reachable named types, the codec pair registry, and the
// occurrence facts of every procedure parameter, procedure result, and
// exception payload, in the same deterministic walk that registers the
// anonymous codec pairs.
func (e *exportEmitter) prepare() error {
	e.wireTypes = make(map[string]*syntax.TypeDecl, len(e.model.Types))
	for _, rec := range e.model.Types {
		e.wireTypes[rec.WireName] = &syntax.TypeDecl{Name: &syntax.Ident{Name: rec.WireName}, Type: rec.Type}
	}
	if err := e.buildImports(); err != nil {
		return err
	}
	e.gtName = make(map[string]string, len(e.model.Types))
	for _, rec := range e.model.Types {
		e.gtName[rec.WireName] = e.qual(rec.PkgPath) + "." + rec.GoName
	}
	e.pairs = newExportCodecEmitter(&e.src, func(wire string) string { return e.gtName[wire] }, e.wireTypes)
	for _, rec := range e.model.Types {
		e.pairs.registerNamed(rec)
	}
	e.params = make(map[*ExportParam]*exportOccurrence)
	e.results = make(map[*ExportProc]*exportOccurrence)
	e.payloads = make(map[*ExportException]*exportOccurrence)
	for _, p := range e.model.Procs {
		for i, pr := range p.Params {
			e.params[pr] = e.occurrence(pr.Param.Value.Type, e.paramType(p, i), "")
		}
		if p.Result != nil {
			e.results[p] = e.occurrence(p.Result.Type, e.resultType(p), "")
		}
	}
	for _, x := range e.model.Exceptions {
		if x.Payload == nil {
			continue
		}
		tn, err := e.exceptionType(x)
		if err != nil {
			return err
		}
		e.payloads[x] = e.occurrence(x.Payload.Type, tn.Type(), e.qual(x.Pkg.Path)+"."+x.GoName)
	}
	return nil
}

// buildImports resolves the deterministic import alias table: the
// imported packages are the provider, application exception, and
// reachable named-type packages; aliases are the package names, with a
// deterministic private mangling for a name already taken by another
// import or by one of the fixed imports — context, errors, the root
// runtime package, math, and unicode/utf8.
func (e *exportEmitter) buildImports() error {
	names := make(map[string]string)
	add := func(path, name string) {
		if path == "" || name == "" {
			return
		}
		names[path] = name
	}
	for _, p := range e.model.Procs {
		add(p.Provider.Pkg.Path, p.Provider.Pkg.Name)
	}
	for _, x := range e.model.Exceptions {
		if !x.Fixed {
			add(x.Pkg.Path, x.Pkg.Name)
		}
	}
	for _, rec := range e.model.Types {
		add(rec.PkgPath, rec.PkgName)
	}
	paths := make([]string, 0, len(names))
	for path := range names {
		paths = append(paths, path)
	}
	sort.Strings(paths)
	e.aliases = make(map[string]string, len(paths))
	e.names = names
	taken := map[string]bool{
		"context":   true,
		"errors":    true,
		"intercall": true,
		"math":      true,
		"utf8":      true,
	}
	for _, path := range paths {
		alias := names[path]
		if taken[alias] {
			alias = ManglePrivate("import", path)
		}
		if taken[alias] {
			return fmt.Errorf("tool: internal error: import alias collision for package %q", path)
		}
		taken[alias] = true
		e.aliases[path] = alias
	}
	return nil
}

// qual returns the deterministic import alias of one package path.
func (e *exportEmitter) qual(path string) string {
	if alias := e.aliases[path]; alias != "" {
		return alias
	}
	panic("tool: internal error: no import alias for package " + path)
}

// paramType returns the go/types type of one wire parameter, correlated
// through the provider's source signature exactly as the mapper walked
// it: parameter fields after the context parameter, one wire parameter
// per name in source order.
func (e *exportEmitter) paramType(p *ExportProc, index int) types.Type {
	fn := p.Provider.Func
	k := 0
	for _, field := range fn.Type.Params.List[1:] {
		for range field.Names {
			if k == index {
				return p.Provider.Pkg.pkg.TypesInfo.TypeOf(field.Type)
			}
			k++
		}
	}
	panic("tool: internal error: no source field for parameter " + p.Provider.Name)
}

// resultType returns the go/types type of one procedure's data result.
func (e *exportEmitter) resultType(p *ExportProc) types.Type {
	fn := p.Provider.Func
	return p.Provider.Pkg.pkg.TypesInfo.TypeOf(fn.Type.Results.List[0].Type)
}

// exceptionType resolves the go/types object of one payload exception
// struct in its declaring package.
func (e *exportEmitter) exceptionType(x *ExportException) (*types.TypeName, error) {
	obj := x.Pkg.pkg.Types.Scope().Lookup(x.GoName)
	tn, ok := obj.(*types.TypeName)
	if !ok {
		return nil, fmt.Errorf("tool: internal error: no type object for exception %q", x.WireName)
	}
	return tn, nil
}

// occurrence builds the Go projection of one type occurrence, registers
// its codec pair, and returns its generation facts. gt overrides the
// projection's own type text for the exception payload pairs, whose
// value type is the named exception struct.
func (e *exportEmitter) occurrence(wire syntax.TypeExpr, t types.Type, gt string) *exportOccurrence {
	node := e.pairs.buildNode(wire, t)
	if gt == "" {
		gt = node.gt
	}
	occ := &exportOccurrence{gt: gt}
	switch wire.(type) {
	case *syntax.PrimType, *syntax.NamedType:
		occ.encName = node.encName()
		occ.decName = node.decName()
	case *syntax.ListType, *syntax.RecordType:
		pair := e.pairs.registerAnon(wire, gt, node)
		occ.encName = pair.encName()
		occ.decName = pair.decName()
	}
	return occ
}

// emit renders the complete generated export binding file: the package
// clause, the fixed and provider imports, the codec support, the codec
// pairs, the per-procedure request decoders, the direct
// application-exception matcher, the static procedure key switch, and
// the immutable export binding singleton. The output is formatted with
// go/format.
func (e *exportEmitter) emit(pkg string) ([]byte, error) {
	e.src.linef("package %s", pkg)
	e.src.blank()
	e.emitImports()
	emitCodecSupport(&e.src)
	e.pairs.emit()
	for _, p := range e.model.Procs {
		e.emitRequestDecoder(p)
	}
	if e.hasApplicationExceptions() {
		e.emitMatcher()
	}
	e.emitDispatch()
	e.emitSingleton()
	return format.Source(e.src.bytes())
}

// hasApplicationExceptions reports whether the interface has at least
// one application exception for the matcher to test.
func (e *exportEmitter) hasApplicationExceptions() bool {
	for _, x := range e.model.Exceptions {
		if !x.Fixed {
			return true
		}
	}
	return false
}

// emitImports emits the import block: the fixed standard library and
// runtime imports, then every provider package in canonical import path
// order with its deterministic alias. go/format sorts the block, so the
// final order is deterministic.
func (e *exportEmitter) emitImports() {
	e.src.linef("import (")
	e.src.open()
	e.src.linef(`"context"`)
	e.src.linef(`"errors"`)
	e.src.linef(`"github.com/cerasos/intercall/go"`)
	e.src.linef(`"math"`)
	paths := make([]string, 0, len(e.aliases))
	for path := range e.aliases {
		paths = append(paths, path)
	}
	sort.Strings(paths)
	for _, path := range paths {
		if e.aliases[path] == e.names[path] {
			e.src.linef("%q", path)
		} else {
			e.src.linef("%s %q", e.aliases[path], path)
		}
	}
	e.src.linef(`"unicode/utf8"`)
	e.src.close()
	e.src.linef(")")
	e.src.blank()
}

// emitRequestDecoder emits one procedure's request decoder:
//
//	func name(src []byte) (P1, ..., Pn, []byte, error)
//
// decoding the wire parameters in declaration order into the provider
// parameter types. A decoder failure returns the decoded prefix with a
// nil remainder; the dispatch turns every decoder error and every
// nonempty remainder into invalid_arguments without invoking the
// provider. Zero-width parameters decode to their zero values without
// consuming bytes.
func (e *exportEmitter) emitRequestDecoder(p *ExportProc) {
	name := codecName("dereq", p.WireName)
	gts := make([]string, 0, len(p.Params)+2)
	for _, pr := range p.Params {
		gts = append(gts, e.params[pr].gt)
	}
	gts = append(gts, "[]byte", "error")
	e.src.linef("func %s(src []byte) (%s) {", name, strings.Join(gts, ", "))
	e.src.open()
	for i := range p.Params {
		e.src.linef("var v%d %s", i, gts[i])
	}
	if len(p.Params) > 0 {
		e.src.linef("var err error")
	}
	vals := func() string {
		parts := make([]string, len(p.Params))
		for i := range p.Params {
			parts[i] = fmt.Sprintf("v%d", i)
		}
		return strings.Join(parts, ", ")
	}
	errVals := func() string {
		if len(p.Params) == 0 {
			return "nil"
		}
		return vals() + ", nil"
	}
	for i, pr := range p.Params {
		e.src.linef("v%d, src, err = %s(src)", i, e.params[pr].decName)
		e.src.linef("if err != nil {")
		e.src.open()
		e.src.linef("return %s, err", errVals())
		e.src.close()
		e.src.linef("}")
	}
	if len(p.Params) > 0 {
		e.src.linef("return %s, src, nil", vals())
	} else {
		e.src.linef("return src, nil")
	}
	e.src.close()
	e.src.linef("}")
	e.src.blank()
}

// emitMatcher emits the shared direct application-exception matcher:
//
//	func name(err error) (uint64, []byte)
//
// comparing the provider's nonnil error against every application
// exception with direct err == error(provider.Sentinel) comparisons and
// direct err.(*provider.T) assertions — never errors.Is or errors.As.
// Exactly one match sends that exception, a sentinel with an empty
// payload and a payload exception with its encoded record; the
// assertion accepts only nonnil payload pointers, so a typed-nil
// pointer matches nothing. Zero matches, multiple matches, a wrapped
// error, and an encoding failure of a matched payload send the fixed
// no-payload internal_exception. A panic during matching — as with an
// uncomparable sentinel value — escapes to the runtime's one recovery
// around the complete dispatch, which also selects internal_exception.
func (e *exportEmitter) emitMatcher() {
	e.src.linef("func %s(err error) (uint64, []byte) {", matcherName)
	e.src.open()
	e.src.linef("var match int")
	e.src.linef("var excKey uint64")
	e.src.linef("var excPayload []byte")
	e.src.linef("var encErr error")
	for _, x := range e.model.Exceptions {
		if x.Fixed {
			continue // runtime conditions select the fixed exceptions
		}
		gt := e.qual(x.Pkg.Path) + "." + x.GoName
		switch x.Match {
		case MatchEquality:
			e.src.linef("if err == error(%s) {", gt)
			e.src.open()
			e.src.linef("match++")
			e.src.linef("excKey = 0x%x", x.Key)
			e.src.close()
			e.src.linef("}")
		case MatchAssertion:
			e.src.linef("if e, ok := err.(*%s); ok && e != nil {", gt)
			e.src.open()
			e.src.linef("match++")
			e.src.linef("excKey = 0x%x", x.Key)
			e.src.linef("excPayload, encErr = %s(nil, *e)", e.payloads[x].encName)
			e.src.close()
			e.src.linef("}")
		}
	}
	e.src.linef("if match != 1 || encErr != nil {")
	e.src.open()
	e.src.linef("return 0x%x, nil", exportInternalExceptionKey)
	e.src.close()
	e.src.linef("}")
	e.src.linef("return excKey, excPayload")
	e.src.close()
	e.src.linef("}")
	e.src.blank()
}

// emitDispatch emits the static procedure key switch: an unknown key
// receives procedure_not_found after its payload has been buffered, a
// known procedure whose arguments are malformed or leave trailing bytes
// receives invalid_arguments without invoking the provider, a provider
// error goes through the direct matcher, and a success value is encoded
// through its codec pair. A failure to encode the success value sends
// the no-payload internal_exception; a panic anywhere in the case body
// escapes to the runtime's recovery around the complete dispatch.
func (e *exportEmitter) emitDispatch() {
	hasApp := e.hasApplicationExceptions()
	e.src.linef("func %s(%s context.Context, %s uint64, %s []byte) (uint64, []byte) {", dispatchName, dispatchCtxName, dispatchKeyName, dispatchPayloadName)
	e.src.open()
	e.src.linef("switch %s {", dispatchKeyName)
	e.src.open()
	for _, p := range e.model.Procs {
		e.src.linef("case 0x%x:", p.Key)
		e.src.open()
		e.emitProcCase(p, hasApp)
		e.src.close()
	}
	e.src.linef("default:")
	e.src.open()
	e.src.linef("return 0x%x, nil", exportProcedureNotFoundKey)
	e.src.close()
	e.src.close()
	e.src.linef("}")
	e.src.close()
	e.src.linef("}")
	e.src.blank()
}

// emitProcCase emits one procedure arm of the static switch.
func (e *exportEmitter) emitProcCase(p *ExportProc, hasApp bool) {
	reqdec := codecName("dereq", p.WireName)
	if len(p.Params) > 0 {
		names := make([]string, len(p.Params))
		for i := range p.Params {
			names[i] = fmt.Sprintf("v%d", i)
		}
		e.src.linef("%s, rest, err := %s(%s)", strings.Join(names, ", "), reqdec, dispatchPayloadName)
	} else {
		e.src.linef("rest, err := %s(%s)", reqdec, dispatchPayloadName)
	}
	e.src.linef("if err != nil || len(rest) != 0 {")
	e.src.open()
	e.src.linef("return 0x%x, nil", exportInvalidArgumentsKey)
	e.src.close()
	e.src.linef("}")
	gt := e.qual(p.Provider.Pkg.Path) + "." + p.Provider.Name
	args := ""
	if len(p.Params) > 0 {
		names := make([]string, len(p.Params))
		for i := range p.Params {
			names[i] = fmt.Sprintf("v%d", i)
		}
		args = ", " + strings.Join(names, ", ")
	}
	if p.Result != nil {
		e.src.linef("out, err := %s(%s%s)", gt, dispatchCtxName, args)
	} else {
		e.src.linef("err = %s(%s%s)", gt, dispatchCtxName, args)
	}
	e.src.linef("if err != nil {")
	e.src.open()
	if hasApp {
		e.src.linef("return %s(err)", matcherName)
	} else {
		e.src.linef("return 0x%x, nil", exportInternalExceptionKey)
	}
	e.src.close()
	e.src.linef("}")
	if p.Result != nil {
		e.src.linef("enc, err := %s(nil, out)", e.results[p].encName)
		e.src.linef("if err != nil {")
		e.src.open()
		e.src.linef("return 0x%x, nil", exportInternalExceptionKey)
		e.src.close()
		e.src.linef("}")
		e.src.linef("return 0, enc")
	} else {
		e.src.linef("return 0, nil")
	}
}

// emitSingleton emits the immutable export binding singleton: the
// package constructs its handle exactly once into an unexported package
// variable, and ExportBinding returns it. The dispatch function is
// static and never nil, so the constructor error is unreachable.
func (e *exportEmitter) emitSingleton() {
	e.src.linef("var %s = func() intercall.ExportBinding {", exportBindingName)
	e.src.open()
	e.src.linef("b, err := intercall.NewExportBinding(%s)", dispatchName)
	e.src.linef("if err != nil {")
	e.src.open()
	e.src.linef("panic(err)")
	e.src.close()
	e.src.linef("}")
	e.src.linef("return b")
	e.src.close()
	e.src.linef("}()")
	e.src.blank()
	e.src.linef("// ExportBinding returns the package's immutable export binding.")
	e.src.linef("func ExportBinding() intercall.ExportBinding {")
	e.src.open()
	e.src.linef("return %s", exportBindingName)
	e.src.close()
	e.src.linef("}")
	e.src.blank()
}
