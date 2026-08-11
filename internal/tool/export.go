package tool

import (
	"fmt"
	"go/ast"
	"go/types"
	"sort"

	"github.com/cerasos/intercall/internal/syntax"
)

// This file implements SPEC.md "Application exceptions", "Fixed Go
// Runtime Exceptions", and "Deterministic export order": the
// collection and validation of every tagged application exception of
// the explicit packages (no-payload sentinel variables and payload
// struct types whose pointer implements error), the reservation and
// insertion of the three fixed runtime exceptions, the application of
// documentation and wire names to exceptions and procedures, the small
// export generation records, and the canonical interface AST assembled
// in the stable emission order.
//
// The generated dispatch matches a provider's nonnil error against
// every application exception with direct equality and direct
// assertion only: a no-payload sentinel compares with
// `err == error(provider.Sentinel)` and a payload exception asserts
// `err.(*provider.T)`. InterCall dispatch has no errors.Is or
// errors.As semantics and never unwraps an error. Zero or multiple
// matches, a wrapped error, a typed-nil payload pointer, and a panic
// during matching send the fixed no-payload exception
// internal_exception; a data result is ignored when the provider
// returns a nonnil error, and a failure to encode a success value or a
// matched exception payload also sends internal_exception.
//
// The canonical interface AST follows the byte-exact emission order:
// reachable ordinary named types in the stable topological order of
// SPEC.md "Deterministic export order" (among the remaining types whose
// named dependencies have already been emitted, the lexicographically
// smallest exact wire name), then every exception — application and
// fixed — by exact wire-name byte order, then every procedure by the
// same order. The interface formatter of internal/syntax renders the
// canonical body from this AST.

// ExceptionForm identifies the two no-payload and payload exception
// forms of SPEC.md "Application exceptions".
type ExceptionForm int

const (
	// SentinelForm is a no-payload exception: an exported package
	// variable assignable to error for an application exception, and
	// no generated package symbol for a fixed runtime exception.
	SentinelForm ExceptionForm = iota
	// PayloadForm is an exported named struct type whose pointer
	// implements error; its fields form the inline payload record
	// under the ordinary record rules.
	PayloadForm
)

// ExceptionMatch is the exact direct match the generated dispatch
// performs for one exception. The dispatch never uses errors.Is or
// errors.As semantics.
type ExceptionMatch int

const (
	// MatchRuntime marks a fixed runtime exception: the generated
	// dispatch never matches it, because runtime conditions select it.
	MatchRuntime ExceptionMatch = iota
	// MatchEquality is the direct err == error(provider.Sentinel)
	// comparison of a no-payload application exception.
	MatchEquality
	// MatchAssertion is the direct err.(*provider.T) assertion of a
	// payload application exception.
	MatchAssertion
)

// InternalExceptionName is the exact wire name of the fixed no-payload
// exception that every dispatch fallback condition selects (SPEC.md
// "Application exceptions" and "Fixed Go Runtime Exceptions"): zero
// matches, multiple matches, a wrapped error that no direct comparison
// or assertion matches, a typed-nil payload pointer, and a panic during
// matching. A data result is ignored when the provider returns a
// nonnil error, and a failure to encode a success value or a matched
// exception payload also sends this exception.
const InternalExceptionName = "internal_exception"

// ExportException is the generation record of one application or fixed
// runtime exception.
//
// WireName is the exact wire name, Key the 64-bit FNV-0 exception key,
// and Doc the exception documentation slot. Form and Match record the
// direct matching facts. Payload is the mapped inline payload record
// and its codec facts, or nil for a no-payload exception.
//
// An application exception (Fixed == false) additionally records its
// source: GoName is the sentinel variable or payload struct name, Pkg
// the declaring explicit package, and Filename and Pos its declaration
// position. A fixed runtime exception (Fixed == true) has no generated
// package symbol and no source position.
type ExportException struct {
	WireName string
	Key      uint64
	Doc      string
	Form     ExceptionForm
	Match    ExceptionMatch
	Payload  *MappedValue // nil when the payload is omitted

	GoName   string // application exceptions only
	Pkg      *ExplicitPackage
	Filename string
	Pos      Position

	Fixed bool // one of the three fixed runtime exceptions
}

// ExportParam is one wire parameter of an export procedure with its
// wire name and documentation applied.
type ExportParam struct {
	Param    *MappedParam
	WireName string
	Doc      string
}

// ExportProc is the generation record of one selected procedure after
// wire names, keys, and documentation are applied.
//
// Provider is the selected provider, WireName the exact procedure wire
// name from the @intercall procedure directive or the default
// projection, Key the 64-bit FNV-0 procedure key, and Doc the procedure
// documentation. Params lists the wire parameters in source order with
// their exact wire names and @param documentation. Result is the mapped
// data result with its @return documentation applied to the type
// occurrence, or nil when the procedure has no return value.
type ExportProc struct {
	Provider *Provider
	WireName string
	Key      uint64
	Doc      string
	Filename string
	Pos      Position
	Params   []*ExportParam
	Result   *MappedValue // nil when the procedure has no return value
}

// ExportModel is the complete export generation record set of one
// discovery pass, following SPEC.md "Deterministic export order".
//
// The embedded TypeMap carries the mapped providers and the reachable
// ordinary named types in stable topological order. Exceptions lists
// every application exception and the three fixed runtime exceptions in
// exact wire-name byte order, and Procs every selected procedure in the
// same order. AST is the canonical interface AST assembled from the
// three lists; CanonicalBody renders its byte-exact canonical body with
// the IC-06 semantic formatter.
type ExportModel struct {
	*TypeMap
	Exceptions []*ExportException
	Procs      []*ExportProc
	AST        *syntax.File
}

// CanonicalBody renders the byte-exact canonical interface body of the
// model: the canonical formatter output of the assembled AST.
func (m *ExportModel) CanonicalBody() []byte {
	return syntax.Format(m.AST)
}

// errorInterface is the predeclared error interface: the only legal
// interface in a provider signature, the assignability target of
// no-payload sentinel exceptions, and the implementation target of
// payload exception structs.
var errorInterface = types.Universe.Lookup("error").Type().Underlying().(*types.Interface)

// MapExport builds the complete export model of one discovery pass:
// the mapped provider values, the reachable named-type graph, every
// collected application exception, the three fixed runtime exceptions,
// and the canonical interface AST.
//
// outPath is the import path of the export output package: the
// generated binding in it must be able to import every provider,
// application exception package, and reachable named-type package. The
// empty string skips the importability checks.
//
// The pass runs in a deterministic order: the tagged exception structs
// are pre-scanned so value mapping can reject exception/type role
// conflicts, then the providers are mapped, then every tagged
// application exception of every explicit package is collected and
// validated in package-path and source order, then the procedures get
// their wire names and documentation, and finally the shared type-graph
// checks, exception importability, key checks, sorting, and interface
// assembly run. The fixed runtime exception names are reserved for all
// three declaration kinds, and their exact no-payload declarations are
// inserted into every interface.
func MapExport(res *DiscoverResult, outPath string) (*ExportModel, error) {
	m := newMapper(res.Providers, outPath)
	for _, p := range res.Packages {
		m.addExplicit(p)
	}
	m.scanExceptions(res.Packages)
	tm, err := m.mapProviders(res.Providers)
	if err != nil {
		return nil, err
	}
	excs, err := m.collectExceptions(res.Packages)
	if err != nil {
		return nil, err
	}
	procs, err := m.exportProcs(tm.Providers)
	if err != nil {
		return nil, err
	}
	if err := m.finalize(tm); err != nil {
		return nil, err
	}
	for _, e := range excs {
		if err := m.checkExceptionImportable(e); err != nil {
			return nil, err
		}
		if e.Payload != nil {
			e.Payload.ZeroWidth = tm.zeroWidth(e.Payload.Type)
		}
	}

	model := &ExportModel{TypeMap: tm}
	model.Exceptions = append(excs, fixedExceptions()...)
	sort.Slice(model.Exceptions, func(i, j int) bool { return model.Exceptions[i].WireName < model.Exceptions[j].WireName })
	model.Procs = procs
	sort.Slice(model.Procs, func(i, j int) bool { return model.Procs[i].WireName < model.Procs[j].WireName })
	if err := checkInterfaceKeys(model); err != nil {
		return nil, err
	}
	model.AST = assembleInterface(model)
	// The assembled file must be a valid interface. Every check above —
	// global names, reserved words, key zero and collisions, local
	// parameter and field scopes, and earlier type resolution — already
	// ran over the same declarations with source positions, so this
	// final validation is a safety net that should never fire.
	if err := syntax.Validate(model.AST); err != nil {
		return nil, fmt.Errorf("internal error: the assembled interface is invalid: %v", err)
	}
	return model, nil
}

// addExplicit registers one explicit package in the mapper's package
// tables. MapExport reaches application exceptions of every explicit
// package, including packages that contribute no providers and are not
// part of the provider import closure.
func (m *mapper) addExplicit(p *ExplicitPackage) {
	if m.exp[p.pkg] == nil {
		m.exp[p.pkg] = p
	}
	indexPackage(p.pkg, m.pkgs)
}

// walkExceptionDecls visits every tagged application exception
// declaration of the explicit packages: each package in canonical path
// order, each compiled file in package order, and each declaration in
// source order. Generated files supply no application exceptions and
// are skipped (SPEC.md "Package discovery and selection").
func walkExceptionDecls(pkgs []*ExplicitPackage, fn func(p *ExplicitPackage, doc *Document, gd *GoDecl) error) error {
	for _, p := range pkgs {
		for _, file := range p.files {
			doc := p.docs[file]
			if doc == nil || doc.Generated {
				continue
			}
			for _, gd := range doc.Decls {
				if (gd.Kind == GoVar || gd.Kind == GoType) && hasDirective(gd.Doc, ExceptionDir) {
					if err := fn(p, doc, gd); err != nil {
						return err
					}
				}
			}
		}
	}
	return nil
}

// scanExceptions records the tagged payload-exception struct types of
// every explicit package before provider mapping, so a role-conflict
// reference to an exception struct is rejected as a procedure value or
// wire-type reference.
func (m *mapper) scanExceptions(pkgs []*ExplicitPackage) {
	_ = walkExceptionDecls(pkgs, func(p *ExplicitPackage, doc *Document, gd *GoDecl) error {
		if gd.Kind == GoType {
			m.excStructs[typeKey{pkg: p.Path, name: gd.Name}] = true
		}
		return nil
	})
}

// collectExceptions collects and validates every tagged application
// exception of the explicit packages in package-path and source order.
//
// A no-payload application exception is one exported package variable
// assignable to error; a payload application exception is one exported
// named struct type whose pointer implements error, and its fields form
// the inline payload record under the ordinary record rules. The
// exception struct cannot also be an ordinary named wire type. Wire
// names come from the @intercall exception directive or the default
// projection and must be nonreserved, distinct from fixed runtime
// exception names, and collision-free against types and earlier
// exceptions. Payload mapping reaches the tagged ordinary types of its
// fields, which become ordinary named wire types.
func (m *mapper) collectExceptions(pkgs []*ExplicitPackage) ([]*ExportException, error) {
	var out []*ExportException
	err := walkExceptionDecls(pkgs, func(p *ExplicitPackage, doc *Document, gd *GoDecl) error {
		bad := func(format string, args ...any) error {
			return &Error{Filename: doc.Name, Pos: gd.Pos, Msg: fmt.Sprintf(format, args...)}
		}
		rec := &ExportException{
			GoName:   gd.Name,
			Pkg:      p,
			Filename: doc.Name,
			Pos:      gd.Pos,
			Doc:      gd.Doc.Retained,
		}
		obj := p.pkg.Types.Scope().Lookup(gd.Name)
		switch gd.Kind {
		case GoVar:
			v, ok := obj.(*types.Var)
			if !ok {
				return fmt.Errorf("internal error: no variable object for sentinel %q of %s", gd.Name, p.Path)
			}
			if !types.AssignableTo(v.Type(), errorInterface) {
				return bad("contradictory @intercall exception directive: sentinel %q of type %s is not assignable to error", gd.Name, v.Type().String())
			}
			rec.Form = SentinelForm
			rec.Match = MatchEquality
		case GoType:
			tn, ok := obj.(*types.TypeName)
			if !ok {
				return fmt.Errorf("internal error: no type object for exception struct %q of %s", gd.Name, p.Path)
			}
			if hasDirective(gd.Doc, TypeDir) {
				return bad("contradictory @intercall exception directive: type %q also carries an @intercall type directive and cannot be both an ordinary named wire type and an exception struct", gd.Name)
			}
			named, ok := tn.Type().(*types.Named)
			if !ok {
				return fmt.Errorf("internal error: exception struct %q of %s is not a named type", gd.Name, p.Path)
			}
			if !types.Implements(types.NewPointer(named), errorInterface) {
				return bad("contradictory @intercall exception directive: *%s must implement error (a method Error() string with a value or pointer receiver)", gd.Name)
			}
			rec.Form = PayloadForm
			rec.Match = MatchAssertion
		default:
			return fmt.Errorf("internal error: unexpected declaration kind %s for an @intercall exception directive", gd.Kind)
		}

		wire, err := exceptionWireName(doc, gd)
		if err != nil {
			return err
		}
		if prev := m.byWire[wire]; prev != nil {
			return bad("wire name collision: type %q and exception %q both map to wire name %q", prev.GoName, gd.Name, wire)
		}
		if prev := m.excWire[wire]; prev != nil {
			return bad("wire name collision: exceptions %q and %q both map to wire name %q", prev.GoName, gd.Name, wire)
		}
		rec.WireName = wire
		rec.Key = syntax.ExceptionKey(wire)
		// The exception is registered before its payload is mapped, so a
		// payload-reached type whose wire name collides with this
		// exception's wire name is rejected during type mapping.
		m.excWire[wire] = rec
		out = append(out, rec)

		if gd.Kind == GoType {
			spec := m.pkgMapOf(p.pkg).specs[obj.(*types.TypeName)]
			if spec == nil {
				return fmt.Errorf("internal error: no type declaration for exception struct %q of %s", gd.Name, p.Path)
			}
			st, ok := spec.Type.(*ast.StructType)
			if !ok {
				return fmt.Errorf("internal error: exception struct %q of %s is not a struct type", gd.Name, p.Path)
			}
			payload, err := m.mapStruct(p.pkg, st, fmt.Sprintf("exception %q", gd.Name))
			if err != nil {
				return err
			}
			rec.Payload = &MappedValue{Type: payload}
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return out, nil
}

// exceptionWireName computes the exact wire name of one tagged
// application exception: the @intercall exception directive operand, or
// the default Pascal projection of the complete Go identifier. The
// result must not be a reserved wire word or one of the fixed runtime
// exception names.
func exceptionWireName(doc *Document, gd *GoDecl) (string, error) {
	bad := func(format string, args ...any) error {
		return &Error{Filename: doc.Name, Pos: gd.Pos, Msg: fmt.Sprintf(format, args...)}
	}
	wire := ""
	if dir := directiveOf(gd, ExceptionDir); dir != nil {
		wire = dir.Wire
	}
	if wire == "" {
		var err error
		wire, err = GoToWire(gd.Name, PascalCase)
		if err != nil {
			return "", bad("exception %q: %v; an @intercall exception directive can override the wire name", gd.Name, err)
		}
		if reservedWireWords[wire] {
			return "", bad("exception %q projects to reserved wire name %q; rename the exception or add an @intercall exception directive", gd.Name, wire)
		}
	}
	if IsFixedRuntimeException(wire) {
		return "", bad("global name collision: application exception %q maps to wire name %q, which is reserved for a fixed runtime exception", gd.Name, wire)
	}
	return wire, nil
}

// exportProcs applies the wire names and documentation of every
// selected procedure: the procedure wire name from the @intercall
// procedure directive or the default Pascal projection, the parameter
// wire names from @intercall param directives or the default camel
// projection, the @param parameter documentation, and the @return
// return-type documentation. Wire names must be nonreserved, distinct
// from the fixed runtime exception names, and collision-free against
// types, exceptions, other procedures, and the procedure's own
// parameter scope.
func (m *mapper) exportProcs(providers []*MappedProvider) ([]*ExportProc, error) {
	var out []*ExportProc
	procWire := make(map[string]*ExportProc)
	for _, mp := range providers {
		p := mp.Provider
		filename := p.Pkg.Path + "/" + fileOf(p.Pkg, p.Doc)
		bad := func(format string, args ...any) error {
			return &Error{Filename: filename, Pos: p.Doc.Pos, Msg: fmt.Sprintf("procedure %q: %s", p.Pkg.Path+"."+p.Name, fmt.Sprintf(format, args...))}
		}
		wire := ""
		if dir := directiveOf(p.Doc, ProcedureDir); dir != nil {
			wire = dir.Wire
		}
		if wire == "" {
			var err error
			wire, err = GoToWire(p.Name, PascalCase)
			if err != nil {
				return nil, bad("%v; an @intercall procedure directive can override the wire name", err)
			}
			if reservedWireWords[wire] {
				return nil, bad("it projects to reserved wire name %q; rename the procedure or add an @intercall procedure directive", wire)
			}
		}
		if IsFixedRuntimeException(wire) {
			return nil, bad("it maps to wire name %q, which is reserved for a fixed runtime exception", wire)
		}
		if prev := m.byWire[wire]; prev != nil {
			return nil, bad("wire name collision: type %q and procedure %q both map to wire name %q", prev.GoName, p.Name, wire)
		}
		if prev := m.excWire[wire]; prev != nil {
			return nil, bad("wire name collision: exception %q and procedure %q both map to wire name %q", prev.GoName, p.Name, wire)
		}
		if prev := procWire[wire]; prev != nil {
			return nil, bad("wire name collision: procedures %q and %q both map to wire name %q", prev.Provider.Name, p.Name, wire)
		}
		rec := &ExportProc{
			Provider: p,
			WireName: wire,
			Key:      syntax.ProcedureKey(wire),
			Doc:      p.Doc.Doc.Retained,
			Filename: filename,
			Pos:      p.Doc.Pos,
		}
		procWire[wire] = rec

		seen := make(map[string]string)
		for i, goName := range p.Params {
			pwire := ""
			if dir := paramDirectiveOf(p.Doc.Doc, goName); dir != nil {
				pwire = dir.Wire
			}
			if pwire == "" {
				var err error
				pwire, err = GoToWire(goName, CamelCase)
				if err != nil {
					return nil, bad("parameter %q: %v; an @intercall param directive can override the wire name", goName, err)
				}
				if reservedWireWords[pwire] {
					return nil, bad("parameter %q projects to reserved wire name %q; rename the parameter or add an @intercall param directive", goName, pwire)
				}
			}
			if prev, ok := seen[pwire]; ok {
				return nil, bad("wire name collision: parameters %q and %q both map to wire name %q", prev, goName, pwire)
			}
			seen[pwire] = goName
			pdoc := ""
			if dir := paramDocOf(p.Doc.Doc, goName); dir != nil {
				pdoc = dir.Text
			}
			rec.Params = append(rec.Params, &ExportParam{
				Param:    mp.Params[i],
				WireName: pwire,
				Doc:      pdoc,
			})
		}
		if mp.Result != nil {
			if dir := returnDocOf(p.Doc.Doc); dir != nil {
				// The @return text documents the return type
				// occurrence, the result node's documentation slot.
				setTypeDoc(mp.Result.Type, dir.Text)
			}
			rec.Result = mp.Result
		}
		out = append(out, rec)
	}
	return out, nil
}

// setTypeDoc sets the documentation slot of one type occurrence. The
// occurrence is a fresh tree owned by its mapped value, so the slot is
// never shared with another declaration.
func setTypeDoc(t syntax.TypeExpr, doc string) {
	switch t := t.(type) {
	case *syntax.PrimType:
		t.Doc = doc
	case *syntax.NamedType:
		t.Doc = doc
	case *syntax.ListType:
		t.Doc = doc
	case *syntax.RecordType:
		t.Doc = doc
	}
}

// paramDirectiveOf returns the @intercall param directive naming one
// wire parameter, or nil.
func paramDirectiveOf(doc *GoDoc, goName string) *Directive {
	if doc == nil {
		return nil
	}
	for _, d := range doc.Directives {
		if d.Kind == ParamDir && d.GoName == goName {
			return &d
		}
	}
	return nil
}

// paramDocOf returns the @param documentation directive naming one wire
// parameter, or nil.
func paramDocOf(doc *GoDoc, goName string) *Directive {
	if doc == nil {
		return nil
	}
	for _, d := range doc.Directives {
		if d.Kind == ParamDocDir && d.GoName == goName {
			return &d
		}
	}
	return nil
}

// returnDocOf returns the @return documentation directive, or nil.
func returnDocOf(doc *GoDoc) *Directive {
	if doc == nil {
		return nil
	}
	for _, d := range doc.Directives {
		if d.Kind == ReturnDocDir {
			return &d
		}
	}
	return nil
}

// checkExceptionImportable validates that the generated binding in the
// output package can import the package of one application exception:
// the package must not be the output package itself or a main package,
// must have Go files, must be visible under the internal rule, and must
// not import the output package.
func (m *mapper) checkExceptionImportable(e *ExportException) error {
	if m.outPath == "" {
		return nil // no output package configured: importability is not checked
	}
	bad := func(format string, args ...any) error {
		return &Error{Filename: e.Filename, Pos: e.Pos, Msg: fmt.Sprintf(format, args...)}
	}
	apkg := m.pkgOf(e.Pkg.Path)
	if apkg == nil {
		return fmt.Errorf("internal error: no load record for package %q of exception %q", e.Pkg.Path, e.WireName)
	}
	switch {
	case e.Pkg.Path == m.outPath:
		return bad("exception %q is declared in the output package %q: the generated binding would import its own package", e.WireName, m.outPath)
	case apkg.Name == "main":
		return bad("exception %q: package %q is a main package and is not importable", e.WireName, e.Pkg.Path)
	case len(apkg.CompiledGoFiles) == 0:
		return bad("exception %q: package %q has no Go files and is not importable", e.WireName, e.Pkg.Path)
	case !internalVisible(m.outPath, e.Pkg.Path):
		return bad("exception %q: package %q is internal and not visible from the output package %q", e.WireName, e.Pkg.Path, m.outPath)
	case importsPath(m.outPath, apkg):
		return bad("exception %q: package %q imports the output package %q, which would form an import cycle", e.WireName, e.Pkg.Path, m.outPath)
	}
	return nil
}

// fixedExceptions builds the exact generation records of the three
// fixed runtime exceptions in exact wire-name byte order (SPEC.md
// "Fixed Go Runtime Exceptions"). Export inserts all three into every
// interface; their names are reserved across the global InterCall
// declaration namespace, and runtime conditions, not provider matching,
// select them.
func fixedExceptions() []*ExportException {
	out := make([]*ExportException, 0, len(fixedExceptionNames))
	for _, name := range fixedExceptionNames {
		out = append(out, &ExportException{
			WireName: name,
			Key:      syntax.ExceptionKey(name),
			Form:     SentinelForm,
			Match:    MatchRuntime,
			Fixed:    true,
		})
	}
	return out
}

// checkInterfaceKeys rejects key zero and collisions across every
// procedure and exception declaration of the interface, in exact
// emission order: exceptions in wire-name byte order followed by
// procedures in the same order. The message names the later
// declaration and reports at its source position; a collision with a
// fixed runtime exception reports at the application declaration's
// position because the fixed exceptions have no source.
func checkInterfaceKeys(model *ExportModel) error {
	type keyInfo struct {
		kind     string
		name     string
		filename string
		pos      Position
	}
	seen := make(map[uint64]keyInfo)
	report := func(kind, name, filename string, pos Position, k uint64) error {
		if k == 0 {
			return &Error{Filename: filename, Pos: pos, Msg: fmt.Sprintf("key of %s %q is 0, which is invalid", kind, name)}
		}
		if prev, ok := seen[k]; ok {
			msg := fmt.Sprintf("key collision: %s %q collides with %s %q", kind, name, prev.kind, prev.name)
			if filename == "" {
				filename, pos = prev.filename, prev.pos
			}
			return &Error{Filename: filename, Pos: pos, Msg: msg}
		}
		seen[k] = keyInfo{kind: kind, name: name, filename: filename, pos: pos}
		return nil
	}
	for _, e := range model.Exceptions {
		if err := report("exception", e.WireName, e.Filename, e.Pos, e.Key); err != nil {
			return err
		}
	}
	for _, p := range model.Procs {
		if err := report("procedure", p.WireName, p.Filename, p.Pos, p.Key); err != nil {
			return err
		}
	}
	return nil
}

// assembleInterface builds the canonical interface AST of one export
// model in the exact emission order of SPEC.md "Deterministic export
// order": reachable ordinary named types in stable topological order,
// then all exceptions by exact wire-name byte order, then all
// procedures by the same order. The type occurrences are the mapped
// wire structures, which already carry every nested documentation
// slot; the declaration, parameter, and result slots carry the applied
// Go documentation.
func assembleInterface(model *ExportModel) *syntax.File {
	// NewFile (rather than a bare struct literal) gives the file its line
	// table, which the validator consults when recording every global
	// declaration position; the assembled declarations carry no source
	// spans.
	f := syntax.NewFile("", nil)
	for _, rec := range model.Types {
		f.Decls = append(f.Decls, &syntax.TypeDecl{
			Doc:  rec.Doc,
			Name: &syntax.Ident{Name: rec.WireName},
			Type: rec.Type,
		})
	}
	for _, e := range model.Exceptions {
		var payload syntax.TypeExpr
		if e.Payload != nil {
			payload = e.Payload.Type
		}
		f.Decls = append(f.Decls, &syntax.ExceptionDecl{
			Doc:  e.Doc,
			Name: &syntax.Ident{Name: e.WireName},
			Type: payload,
		})
	}
	for _, p := range model.Procs {
		pd := &syntax.ProcDecl{
			Doc:  p.Doc,
			Name: &syntax.Ident{Name: p.WireName},
		}
		for _, prm := range p.Params {
			pd.Params = append(pd.Params, &syntax.Param{
				Doc:  prm.Doc,
				Name: &syntax.Ident{Name: prm.WireName},
				Type: prm.Param.Value.Type,
			})
		}
		if p.Result != nil {
			pd.Result = p.Result.Type
		}
		f.Decls = append(f.Decls, pd)
	}
	return f
}
