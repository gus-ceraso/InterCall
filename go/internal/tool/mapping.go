package tool

import (
	"fmt"
	"go/ast"
	"go/token"
	"go/types"
	"os"
	"sort"
	"strconv"
	"strings"

	"github.com/cerasos/intercall/go/internal/syntax"
	"golang.org/x/tools/go/packages"
)

// This file implements the exact source-form-sensitive Go value mapping
// of SPEC.md "Procedure signatures and wire values", the reachable
// named-type graph with importability, recursion rejection, and the
// stable topological export order of SPEC.md "Deterministic export
// order", and the directive and field-tag wire-name overrides of SPEC.md
// "Names and native overrides". The trusted generated-type semantic
// recovery of SPEC.md "Safe import and re-export metadata" lives in
// metadata.go.
//
// The mapping correlates go/ast source spellings with go/types
// structure at every node: the predeclared spellings `byte` and `uint8`
// at each slice node select `bytes` and `list uint8`; aliases are
// flattened through their declaration RHS, so `type B = []byte` and
// `type U = []uint8` remain distinct; anonymous structs stay inline
// records in every position; and every reachable ordinary defined type
// becomes a named type with exactly one @intercall type directive,
// mapped once and preserved in the type graph. Parenthesized types are
// transparent: go/parser keeps their parentheses as ParenExpr nodes,
// and the mapping unwraps them, so `([]byte)` is the `[]byte`
// occurrence `bytes`, `type B = ([]byte)` flattens to `bytes`, and the
// byte/uint8 spelling rule applies to the direct element identifier of
// a slice node.

// TypeMap is the complete wire-value mapping of one discovery pass.
//
// Providers holds the mapped wire values of the selected providers in
// provider order, and Types the reachable ordinary named types in the
// stable topological emission order of SPEC.md "Deterministic export
// order". The wire structures are syntax.AST type occurrences whose
// record fields carry their exact wire names and documentation slots;
// the same trees feed the canonical interface assembly and the codec
// facts of later phases.
type TypeMap struct {
	Providers []*MappedProvider
	Types     []*NamedType // reachable named types in stable topological order

	byWire map[string]*NamedType // exact wire name -> type record
}

// MappedProvider is the mapped wire values of one selected provider:
// every wire parameter in source order and the optional data result.
type MappedProvider struct {
	Provider *Provider
	Params   []*MappedParam
	Result   *MappedValue // nil when the provider has no data result
}

// MappedParam is one wire parameter with its Go name.
type MappedParam struct {
	GoName string
	Value  *MappedValue
}

// MappedValue is the wire mapping of one Go value occurrence: a
// procedure parameter or the data result.
//
// Type is the exact wire structure. ZeroWidth is the codec fact that
// the structure occupies zero wire bytes: an inline record whose fields
// all occupy zero bytes or a named reference to such a record; lists
// and primitives always carry bytes, and nil slices encode as empty
// lists or bytes values.
type MappedValue struct {
	Type      syntax.TypeExpr
	ZeroWidth bool
}

// NamedType is one reachable ordinary defined type.
//
// GoName is the source Go name, WireName the exact wire name from the
// @intercall type directive or the default projection, and Decl the
// source type spec. Doc is the documentation slot: the retained Go
// documentation for handwritten types, or the semantic type
// documentation for generated types. Type is the underlying wire
// structure with every nested documentation slot, ZeroWidth its codec
// fact, and Generated reports whether the type was recovered from the
// trusted machine metadata of an intercall-generated file. TypeName
// retains the type's go/types object so the export emitter can resolve
// its Go type and the Go field names of its underlying structure.
type NamedType struct {
	GoName    string
	WireName  string
	PkgPath   string
	PkgName   string
	Filename  string
	Pos       Position
	Decl      *ast.TypeSpec
	TypeName  *types.TypeName // the type's go/types object
	Doc       string          // documentation slot
	Type      syntax.TypeExpr // underlying wire structure, with nested docs
	ZeroWidth bool            // codec fact
	Generated bool            // recovered from intercall-generated metadata
}

// MapValues maps the wire values of the selected providers of one
// discovery pass and builds the reachable named-type graph.
//
// outPath is the import path of the export output package: the
// generated binding in it must be able to import every reachable
// named-type package. The empty string skips the importability checks.
//
// The mapping walks the provider signatures in provider order, so the
// first error is deterministic for one discovery result. Every reachable
// ordinary defined type must be exported and carry exactly one
// @intercall type directive, and its package must be importable from
// the output package; recursive type graphs and wire-name collisions
// are rejected. On success, Types is in the stable topological order:
// among the remaining types whose named dependencies have already been
// emitted, the lexicographically smallest exact wire name is chosen.
func MapValues(providers []*Provider, outPath string) (*TypeMap, error) {
	m := newMapper(providers, outPath)
	tm, err := m.mapProviders(providers)
	if err != nil {
		return nil, err
	}
	if err := m.finalize(tm); err != nil {
		return nil, err
	}
	return tm, nil
}

// mapProviders maps the wire values of the selected providers in
// provider order.
func (m *mapper) mapProviders(providers []*Provider) (*TypeMap, error) {
	tm := &TypeMap{}
	for _, p := range providers {
		mp, err := m.mapProvider(p)
		if err != nil {
			return nil, err
		}
		tm.Providers = append(tm.Providers, mp)
	}
	return tm, nil
}

// typeKey identifies one Go type declaration by package path and name.
type typeKey struct {
	pkg  string
	name string
}

// mapper is the working state of one value-mapping pass.
type mapper struct {
	outPath string

	exp     map[*packages.Package]*ExplicitPackage
	pkgs    map[string]*packages.Package
	pkgMaps map[*packages.Package]*pkgMap

	types  map[typeKey]*NamedType // every reachable type record
	byWire map[string]*NamedType  // exact wire name -> record
	sems   map[*ast.File]*semResult

	// Application exception facts of the export model: the tagged
	// exception struct types of every explicit package and the exact
	// wire name of every collected application exception. Role and
	// global-collision checks consult these during value mapping.
	excStructs map[typeKey]bool            // tagged payload-exception structs
	excWire    map[string]*ExportException // application exception wire names
}

// semResult caches one file's semantic recovery, including failures.
type semResult struct {
	sem *Semantic
	err error
}

// newMapper builds the mapper of one mapping pass, indexing the
// transitive import closure of every provider package.
func newMapper(providers []*Provider, outPath string) *mapper {
	m := &mapper{
		outPath:    outPath,
		exp:        make(map[*packages.Package]*ExplicitPackage, len(providers)),
		pkgs:       make(map[string]*packages.Package),
		pkgMaps:    make(map[*packages.Package]*pkgMap),
		types:      make(map[typeKey]*NamedType),
		byWire:     make(map[string]*NamedType),
		sems:       make(map[*ast.File]*semResult),
		excStructs: make(map[typeKey]bool),
		excWire:    make(map[string]*ExportException),
	}
	for _, p := range providers {
		m.exp[p.Pkg.pkg] = p.Pkg
		indexPackage(p.Pkg.pkg, m.pkgs)
	}
	return m
}

// indexPackage records pkg and its transitive import closure in idx.
func indexPackage(pkg *packages.Package, idx map[string]*packages.Package) {
	if pkg == nil || idx[pkg.PkgPath] != nil {
		return
	}
	idx[pkg.PkgPath] = pkg
	for _, dep := range pkg.Imports {
		indexPackage(dep, idx)
	}
}

// pkgMap is the per-package working state of the mapper: the parsed
// document of every file and the type-spec table keyed by type name
// object.
//
// An explicit package's documents were parsed and validated by
// IC-12's parseDocuments; for every other package the mapper builds a
// position-only wrapper around the package's own token.File and the
// file bytes on disk, so directives and documentation are checked only
// on declarations the mapping reaches.
type pkgMap struct {
	pkg    *packages.Package
	exp    *ExplicitPackage // nil for packages that are not explicit
	docs   map[*ast.File]*Document
	docErr map[*ast.File]error
	specs  map[*types.TypeName]*ast.TypeSpec
	gds    map[*ast.TypeSpec]*GoDecl
	gdErr  map[*ast.TypeSpec]error
}

// newPkgMap builds the per-package state of one package.
func newPkgMap(pkg *packages.Package, exp *ExplicitPackage) *pkgMap {
	pm := &pkgMap{
		pkg:    pkg,
		exp:    exp,
		docs:   make(map[*ast.File]*Document),
		docErr: make(map[*ast.File]error),
		specs:  make(map[*types.TypeName]*ast.TypeSpec),
		gds:    make(map[*ast.TypeSpec]*GoDecl),
		gdErr:  make(map[*ast.TypeSpec]error),
	}
	// The syntax files of a load all live in the package's own load
	// FileSet. Documents are paired with their syntax by filename — the
	// token.File name is the exact compiled-file path — and the
	// document's offset conversion is retargeted to that same token.File,
	// so physical positions resolve through the load FileSet. A syntax
	// file without a parsed document is an invariant break and fails
	// rather than trusting positional slice correspondence.
	if err := checkExternalBases(pkg.PkgPath, pkg.Dir, compiledNames(pkg)); err != nil {
		// The invariant failure applies to every file of the package:
		// no document is built and every reachable declaration fails
		// with the invariant error instead of a conflated logical path.
		for _, af := range pkg.Syntax {
			pm.docErr[af] = err
		}
		return pm
	}
	for _, af := range pkg.Syntax {
		tf := pkg.Fset.File(af.Pos())
		if tf == nil {
			pm.docErr[af] = fmt.Errorf("internal error: no token.File for a syntax file of %s", pkg.PkgPath)
			continue
		}
		if exp != nil {
			doc := exp.docs[tf.Name()]
			if doc == nil {
				pm.docErr[af] = fmt.Errorf("internal error: no parsed document for file %s of %s", tf.Name(), pkg.PkgPath)
				continue
			}
			// The parsed document's own offsets come from the load
			// FileSet's file, never from the document's parse-time
			// FileSet.
			doc.tok = tf
			pm.docs[af] = doc
			continue
		}
		name := tf.Name()
		src, err := os.ReadFile(name)
		if err != nil {
			pm.docErr[af] = fmt.Errorf("reading %s: %v", name, err)
			continue
		}
		pm.docs[af] = &Document{
			Name:               logicalFilePath(pkg.PkgPath, pkg.Dir, name),
			Size:               len(src),
			Generated:          ast.IsGenerated(af),
			IntercallGenerated: firstLine(src) == intercallGeneratedMarker,
			src:                src,
			lines:              lineStarts(src),
			tok:                tf,
		}
	}
	for _, af := range pkg.Syntax {
		for _, d := range af.Decls {
			gd, ok := d.(*ast.GenDecl)
			if !ok || gd.Tok != token.TYPE {
				continue
			}
			for _, s := range gd.Specs {
				ts := s.(*ast.TypeSpec)
				if obj, ok := pkg.TypesInfo.Defs[ts.Name]; ok {
					if tn, ok := obj.(*types.TypeName); ok {
						pm.specs[tn] = ts
					}
				}
			}
		}
	}
	return pm
}

// compiledNames returns the compiled-file names of one package's
// syntax, in syntax order.
func compiledNames(pkg *packages.Package) []string {
	names := make([]string, 0, len(pkg.Syntax))
	for _, af := range pkg.Syntax {
		if tf := pkg.Fset.File(af.Pos()); tf != nil {
			names = append(names, tf.Name())
		}
	}
	return names
}

// fileContaining returns the package file containing pos, or nil.
func (pm *pkgMap) fileContaining(pos token.Pos) *ast.File {
	for _, af := range pm.pkg.Syntax {
		if pos >= af.Pos() && pos < af.End() {
			return af
		}
	}
	return nil
}

// pkgMapOf returns the working state of one package, building it on
// first use.
func (m *mapper) pkgMapOf(pkg *packages.Package) *pkgMap {
	if pm := m.pkgMaps[pkg]; pm != nil {
		return pm
	}
	pm := newPkgMap(pkg, m.exp[pkg])
	m.pkgMaps[pkg] = pm
	return pm
}

// pkgOf returns the load record of one package path, or nil when the
// package is not in the provider import closure.
func (m *mapper) pkgOf(path string) *packages.Package { return m.pkgs[path] }

// docOf returns the parsed document of one package file.
func (m *mapper) docOf(pkg *packages.Package, af *ast.File) (*Document, error) {
	pm := m.pkgMapOf(pkg)
	if d := pm.docs[af]; d != nil {
		return d, nil
	}
	if err := pm.docErr[af]; err != nil {
		return nil, err
	}
	return nil, fmt.Errorf("internal error: no document for a file of %s", pkg.PkgPath)
}

// goDeclOf returns the declaration record of one type spec: the parsed
// record of an explicit package, or a record built on demand from the
// source for any other package, so the directive and documentation
// checks apply exactly to the reachable declaration.
func (m *mapper) goDeclOf(pkg *packages.Package, af *ast.File, spec *ast.TypeSpec) (*GoDecl, error) {
	pm := m.pkgMapOf(pkg)
	if gd := pm.gds[spec]; gd != nil {
		return gd, nil
	}
	if err := pm.gdErr[spec]; err != nil {
		return nil, err
	}
	doc, err := m.docOf(pkg, af)
	if err != nil {
		return nil, err
	}
	if pm.exp != nil {
		for _, d := range doc.Decls {
			if d.Kind == GoType && d.Name == spec.Name.Name {
				pm.gds[spec] = d
				return d, nil
			}
		}
		return nil, fmt.Errorf("internal error: no declaration record for type %q in %s", spec.Name.Name, doc.Name)
	}
	var gen *ast.GenDecl
	for _, d := range af.Decls {
		gd, ok := d.(*ast.GenDecl)
		if !ok || gd.Tok != token.TYPE {
			continue
		}
		for _, s := range gd.Specs {
			if s == spec {
				gen = gd
			}
		}
	}
	if gen == nil {
		return nil, fmt.Errorf("internal error: no type declaration for %q in %s", spec.Name.Name, doc.Name)
	}
	var errs []*Error
	index, firstDocless := 0, -1
	for i, s := range gen.Specs {
		if ownDoc(s) == nil && firstDocless == -1 {
			firstDocless = i
		}
		if s == spec {
			index = i
		}
	}
	inert := doc.Generated && !doc.IntercallGenerated
	gd := buildSpec(gen, spec, index, firstDocless, len(gen.Specs), inert, doc, doc.offset, &errs)
	if err := firstError(errs); err != nil {
		pm.gdErr[spec] = err
		return nil, err
	}
	checkGroupDoc(gen, firstDocless, inert, doc, &errs)
	if err := firstError(errs); err != nil {
		pm.gdErr[spec] = err
		return nil, err
	}
	pm.gds[spec] = gd
	return gd, nil
}

// errAt builds one diagnostic at a physical position of a package file.
func (m *mapper) errAt(pkg *packages.Package, pos token.Pos, format string, args ...any) *Error {
	if pos.IsValid() {
		pm := m.pkgMapOf(pkg)
		if af := pm.fileContaining(pos); af != nil {
			if doc, err := m.docOf(pkg, af); err == nil {
				return &Error{Filename: doc.Name, Pos: doc.Position(doc.offset(pos)), Msg: fmt.Sprintf(format, args...)}
			}
		}
	}
	f := pkg.Fset.PositionFor(pos, false)
	return &Error{Filename: logicalFilePath(pkg.PkgPath, pkg.Dir, f.Filename), Pos: Position{Offset: f.Offset, Line: f.Line, Column: f.Column}, Msg: fmt.Sprintf(format, args...)}
}

// semanticOf recovers the machine metadata of one intercall-generated
// file, caching the result per file.
func (m *mapper) semanticOf(af *ast.File, doc *Document) (*Semantic, error) {
	if r := m.sems[af]; r != nil {
		return r.sem, r.err
	}
	sem, err := RecoverSemantic(af, doc)
	m.sems[af] = &semResult{sem: sem, err: err}
	return sem, err
}

// mapProvider maps the wire values of one selected provider: every wire
// parameter and the optional data result, in source order.
func (m *mapper) mapProvider(p *Provider) (*MappedProvider, error) {
	mp := &MappedProvider{Provider: p}
	pkg := p.Pkg.pkg
	fn := p.Func
	qname := p.Pkg.Path + "." + p.Name
	if fn.Type.Params == nil {
		return nil, m.errAt(pkg, fn.Pos(), "internal error: procedure %q has no parameters", qname)
	}
	// The context parameter is not a wire value and must be declared
	// alone: a multi-name first field would silently drop the extra
	// names, which are wire parameters of interface type.
	if len(fn.Type.Params.List[0].Names) != 1 {
		return nil, m.errAt(pkg, fn.Type.Params.List[0].Pos(), "procedure %q: the context parameter must be declared alone", qname)
	}
	for _, field := range fn.Type.Params.List[1:] {
		if len(field.Names) == 0 {
			return nil, m.errAt(pkg, field.Pos(), "internal error: parameter of procedure %q has no name", qname)
		}
		val, err := m.mapValue(pkg, field.Type, fmt.Sprintf("parameter %q of procedure %q", field.Names[0].Name, qname))
		if err != nil {
			return nil, err
		}
		for _, name := range field.Names {
			mp.Params = append(mp.Params, &MappedParam{GoName: name.Name, Value: val})
		}
	}
	if p.DataResult {
		val, err := m.mapValue(pkg, fn.Type.Results.List[0].Type, fmt.Sprintf("the return value of procedure %q", qname))
		if err != nil {
			return nil, err
		}
		mp.Result = val
	}
	return mp, nil
}

// mapValue maps one Go type occurrence to its wire structure.
func (m *mapper) mapValue(pkg *packages.Package, e ast.Expr, where string) (*MappedValue, error) {
	t := pkg.TypesInfo.TypeOf(e)
	if t == nil {
		return nil, m.errAt(pkg, e.Pos(), "internal error: no type information for %s", where)
	}
	wt, err := m.mapExpr(pkg, e, t, where)
	if err != nil {
		return nil, err
	}
	return &MappedValue{Type: wt}, nil
}

// mapExpr maps one Go type occurrence, dispatching on its source form
// and correlating it with the go/types structure.
func (m *mapper) mapExpr(pkg *packages.Package, e ast.Expr, t types.Type, where string) (syntax.TypeExpr, error) {
	switch e := e.(type) {
	case *ast.ArrayType:
		return m.mapArray(pkg, e, where)
	case *ast.StructType:
		return m.mapStruct(pkg, e, where)
	case *ast.Ident:
		return m.mapIdent(pkg, e, where)
	case *ast.SelectorExpr:
		return m.mapByType(pkg, e, t, where)
	case *ast.ParenExpr:
		// Parentheses are transparent to Go type identity: the inner
		// node keeps the parenthesized occurrence's go/types type, so
		// `([]byte)` is the `[]byte` occurrence and `type B = ([]byte)`
		// flattens to `bytes` like any other alias RHS.
		return m.mapExpr(pkg, e.X, t, where)
	case *ast.IndexExpr, *ast.IndexListExpr:
		return nil, m.errAt(pkg, e.Pos(), "%s: generic types and instantiations are rejected", where)
	case *ast.StarExpr:
		return nil, m.errAt(pkg, e.Pos(), "%s: pointer types are not wire values", where)
	case *ast.MapType:
		return nil, m.errAt(pkg, e.Pos(), "%s: map types are not wire values", where)
	case *ast.ChanType:
		return nil, m.errAt(pkg, e.Pos(), "%s: channel types are not wire values", where)
	case *ast.FuncType:
		return nil, m.errAt(pkg, e.Pos(), "%s: function types are not wire values", where)
	case *ast.InterfaceType:
		return nil, m.errAt(pkg, e.Pos(), "%s: interface types are not wire values (the predeclared error interface is legal only as the mandatory final function result)", where)
	case *ast.Ellipsis:
		return nil, m.errAt(pkg, e.Pos(), "%s: variadic parameters are not wire values", where)
	}
	return nil, m.errAt(pkg, e.Pos(), "%s: unsupported Go type form %T is not a wire value", where, e)
}

// mapArray maps one slice or array occurrence. A slice whose element is
// the predeclared spelling `byte` maps to `bytes`, and one whose element
// is the predeclared spelling `uint8` maps to `list uint8`; every other
// slice maps to `list` of its mapped element, so the two spellings stay
// distinct at every source and alias node. Arrays are rejected.
func (m *mapper) mapArray(pkg *packages.Package, e *ast.ArrayType, where string) (syntax.TypeExpr, error) {
	if e.Len != nil {
		return nil, m.errAt(pkg, e.Pos(), "%s: array types are not wire values", where)
	}
	et := pkg.TypesInfo.TypeOf(e.Elt)
	if et == nil {
		return nil, m.errAt(pkg, e.Pos(), "%s: internal error: no type information for the element type", where)
	}
	if id, ok := e.Elt.(*ast.Ident); ok {
		if b, ok := types.Unalias(et).(*types.Basic); ok && b.Kind() == types.Uint8 {
			switch id.Name {
			case "byte":
				return &syntax.PrimType{Kind: syntax.TokBytes}, nil
			case "uint8":
				return &syntax.ListType{Elem: &syntax.PrimType{Kind: syntax.TokUint8}}, nil
			}
		}
	}
	elem, err := m.mapExpr(pkg, e.Elt, et, where)
	if err != nil {
		return nil, err
	}
	return &syntax.ListType{Elem: elem}, nil
}

// mapStruct maps one anonymous struct occurrence to an inline record.
// Fields map in source order; every field is required, named,
// nonembedded, and exported, the only wire tag is
// `intercall:"wire_name"`, and the resulting wire field names must be
// collision-free in the record scope. struct{} maps to record {}.
func (m *mapper) mapStruct(pkg *packages.Package, e *ast.StructType, where string) (syntax.TypeExpr, error) {
	pm := m.pkgMapOf(pkg)
	doc, err := m.docOf(pkg, pm.fileContaining(e.Pos()))
	if err != nil {
		return nil, err
	}
	rec := &syntax.RecordType{}
	seen := make(map[string]bool)
	for _, f := range e.Fields.List {
		if len(f.Names) == 0 {
			return nil, m.errAt(pkg, f.Pos(), "%s: embedded fields are not wire fields (every field is required, named, nonembedded, and exported)", where)
		}
		fdoc := ""
		if f.Doc != nil && !(doc.Generated && !doc.IntercallGenerated) {
			var errs []*Error
			gd := processDoc(f.Doc, doc, &errs, docTarget{field: true})
			if err := firstError(errs); err != nil {
				return nil, err
			}
			fdoc = gd.Retained
		}
		wire := ""
		if f.Tag != nil {
			tag, err := tagValue(f.Tag.Value)
			if err != nil {
				return nil, m.errAt(pkg, f.Tag.Pos(), "%s: malformed intercall tag: %v", where, err)
			}
			w, _, err := intercallTag(tag)
			if err != nil {
				return nil, m.errAt(pkg, f.Tag.Pos(), "%s: %v", where, err)
			}
			wire = w
		}
		ft := pkg.TypesInfo.TypeOf(f.Type)
		if ft == nil {
			return nil, m.errAt(pkg, f.Type.Pos(), "%s: internal error: no type information for a field type", where)
		}
		for _, name := range f.Names {
			if !IsExportedGoIdentifier(name.Name) {
				return nil, m.errAt(pkg, name.Pos(), "field %q of %s must be exported", name.Name, where)
			}
			fw := wire
			if fw == "" {
				fw, err = GoToWire(name.Name, PascalCase)
				if err != nil {
					return nil, m.errAt(pkg, name.Pos(), "field %q of %s: %v; an intercall tag can override the wire name", name.Name, where, err)
				}
				if reservedWireWords[fw] {
					return nil, m.errAt(pkg, name.Pos(), "field %q of %s projects to reserved wire name %q; rename the field or add an intercall tag", name.Name, where, fw)
				}
			}
			if seen[fw] {
				return nil, m.errAt(pkg, name.Pos(), "duplicate wire field name %q in %s", fw, where)
			}
			seen[fw] = true
			ft2, err := m.mapExpr(pkg, f.Type, ft, fmt.Sprintf("field %q of %s", name.Name, where))
			if err != nil {
				return nil, err
			}
			rec.Fields = append(rec.Fields, &syntax.Field{Name: &syntax.Ident{Name: fw}, Type: ft2, Doc: fdoc})
		}
	}
	return rec, nil
}

// mapIdent maps one identifier-named type occurrence: a primitive, an
// alias, or a defined type, resolved through the identifier's type
// object.
func (m *mapper) mapIdent(pkg *packages.Package, e *ast.Ident, where string) (syntax.TypeExpr, error) {
	if tn, ok := pkg.TypesInfo.ObjectOf(e).(*types.TypeName); ok {
		switch tt := tn.Type().(type) {
		case *types.Basic:
			return m.mapBasic(pkg, e, tt, where)
		case *types.Alias:
			return m.mapAlias(pkg, e, tn, where)
		case *types.Named:
			return m.mapNamedType(pkg, e, tn, where)
		case *types.TypeParam, *types.Union:
			return nil, m.errAt(pkg, e.Pos(), "%s: generic types are rejected", where)
		}
	}
	return nil, m.errAt(pkg, e.Pos(), "%s: internal error: identifier %q does not name a type", where, e.Name)
}

// mapByType maps one type occurrence by its go/types form, used for
// qualified names and the type-driven fallbacks.
func (m *mapper) mapByType(pkg *packages.Package, e ast.Expr, t types.Type, where string) (syntax.TypeExpr, error) {
	switch t := t.(type) {
	case *types.Basic:
		return m.mapBasic(pkg, e, t, where)
	case *types.Alias:
		return m.mapAlias(pkg, e, t.Obj(), where)
	case *types.Named:
		return m.mapNamedType(pkg, e, t.Obj(), where)
	case *types.Pointer:
		return nil, m.errAt(pkg, e.Pos(), "%s: pointer types are not wire values", where)
	case *types.Map:
		return nil, m.errAt(pkg, e.Pos(), "%s: map types are not wire values", where)
	case *types.Chan:
		return nil, m.errAt(pkg, e.Pos(), "%s: channel types are not wire values", where)
	case *types.Signature:
		return nil, m.errAt(pkg, e.Pos(), "%s: function types are not wire values", where)
	case *types.Interface:
		return nil, m.errAt(pkg, e.Pos(), "%s: interface types are not wire values (the predeclared error interface is legal only as the mandatory final function result)", where)
	case *types.Array:
		return nil, m.errAt(pkg, e.Pos(), "%s: array types are not wire values", where)
	case *types.Slice:
		return nil, m.errAt(pkg, e.Pos(), "%s: slice types are not wire values in this position", where)
	case *types.TypeParam, *types.Union:
		return nil, m.errAt(pkg, e.Pos(), "%s: generic types are rejected", where)
	}
	return nil, m.errAt(pkg, e.Pos(), "%s: unsupported type %s is not a wire value", where, t.String())
}

// mapBasic maps one basic type to its exact-width primitive. The
// machine-word integers, bool, complex numbers, and unsafe.Pointer are
// not wire values.
func (m *mapper) mapBasic(pkg *packages.Package, e ast.Expr, b *types.Basic, where string) (syntax.TypeExpr, error) {
	switch b.Kind() {
	case types.Int8:
		return &syntax.PrimType{Kind: syntax.TokInt8}, nil
	case types.Int16:
		return &syntax.PrimType{Kind: syntax.TokInt16}, nil
	case types.Int32:
		return &syntax.PrimType{Kind: syntax.TokInt32}, nil
	case types.Int64:
		return &syntax.PrimType{Kind: syntax.TokInt64}, nil
	case types.Uint8:
		return &syntax.PrimType{Kind: syntax.TokUint8}, nil
	case types.Uint16:
		return &syntax.PrimType{Kind: syntax.TokUint16}, nil
	case types.Uint32:
		return &syntax.PrimType{Kind: syntax.TokUint32}, nil
	case types.Uint64:
		return &syntax.PrimType{Kind: syntax.TokUint64}, nil
	case types.Float32:
		return &syntax.PrimType{Kind: syntax.TokFloat32}, nil
	case types.Float64:
		return &syntax.PrimType{Kind: syntax.TokFloat64}, nil
	case types.String:
		return &syntax.PrimType{Kind: syntax.TokString}, nil
	case types.Int, types.Uint, types.Uintptr:
		return nil, m.errAt(pkg, e.Pos(), "%s: %s is a machine-word integer; only exact-width integers are wire values", where, b.Name())
	case types.Bool:
		return nil, m.errAt(pkg, e.Pos(), "%s: bool is not a wire value", where)
	case types.Complex64, types.Complex128:
		return nil, m.errAt(pkg, e.Pos(), "%s: complex numbers are not wire values", where)
	case types.UnsafePointer:
		return nil, m.errAt(pkg, e.Pos(), "%s: unsafe.Pointer is not a wire value", where)
	}
	return nil, m.errAt(pkg, e.Pos(), "%s: primitive %s is not a wire value", where, b.Name())
}

// mapAlias flattens one type alias to its resolved target without a
// declaration, following the exact source and alias RHS syntax: the RHS
// of the final alias in the chain is mapped in its declaring package,
// so `type B = []byte` and `type U = []uint8` remain distinct at every
// slice node. An alias of a defined type resolves to that type's named
// reference.
func (m *mapper) mapAlias(pkg *packages.Package, e ast.Expr, tn *types.TypeName, where string) (syntax.TypeExpr, error) {
	if tn.Pkg() == nil {
		// A predeclared alias: byte and uint8 are basic types, and any
		// is an alias of interface{}.
		switch u := types.Unalias(tn.Type()).(type) {
		case *types.Basic:
			return m.mapBasic(pkg, e, u, where)
		case *types.Interface:
			return nil, m.errAt(pkg, e.Pos(), "%s: interface types are not wire values (the predeclared error interface is legal only as the mandatory final function result)", where)
		}
		return nil, m.errAt(pkg, e.Pos(), "%s: predeclared type %q is not a wire value", where, tn.Name())
	}
	apkg := m.pkgOf(tn.Pkg().Path())
	if apkg == nil {
		return nil, m.errAt(pkg, e.Pos(), "%s: internal error: package %q of alias %q is not in the load graph", where, tn.Pkg().Path(), tn.Name())
	}
	pm := m.pkgMapOf(apkg)
	spec := pm.specs[tn]
	if spec == nil {
		return nil, m.errAt(pkg, e.Pos(), "%s: internal error: no declaration for alias %q", where, tn.Name())
	}
	rt := apkg.TypesInfo.TypeOf(spec.Type)
	if rt == nil {
		return nil, m.errAt(pkg, e.Pos(), "%s: internal error: no type information for the RHS of alias %q", where, tn.Name())
	}
	return m.mapExpr(apkg, spec.Type, rt, where)
}

// mapNamedType maps one reachable ordinary defined type. The type must
// be exported, nongeneric, and carry exactly one @intercall type
// directive; its exact wire name comes from the directive or the
// default projection and must be unique, nonreserved, and not reserved
// for a fixed runtime exception. A tagged application exception struct
// is never an ordinary wire type and cannot occur as a procedure value
// or wire-type reference. Its underlying structure is mapped once —
// aliases never reach this function — or recovered from the trusted
// machine metadata of an intercall-generated file. Every later reference
// reuses the recorded type, so recursive graphs terminate here and are
// rejected by the emission-order check.
func (m *mapper) mapNamedType(pkg *packages.Package, e ast.Expr, tn *types.TypeName, where string) (syntax.TypeExpr, error) {
	if tn.Pkg() == nil {
		// The predeclared named types error and comparable are
		// interfaces, legal only as the mandatory final result.
		if _, ok := tn.Type().Underlying().(*types.Interface); ok {
			return nil, m.errAt(pkg, e.Pos(), "%s: interface types are not wire values (the predeclared error interface is legal only as the mandatory final function result)", where)
		}
		return nil, m.errAt(pkg, e.Pos(), "%s: predeclared type %q is not a wire value", where, tn.Name())
	}
	key := typeKey{pkg: tn.Pkg().Path(), name: tn.Name()}
	if rec := m.types[key]; rec != nil {
		return &syntax.NamedType{Name: &syntax.Ident{Name: rec.WireName}}, nil
	}
	apkg := m.pkgOf(tn.Pkg().Path())
	if apkg == nil {
		return nil, m.errAt(pkg, e.Pos(), "%s: internal error: package %q of type %q is not in the load graph", where, tn.Pkg().Path(), tn.Name())
	}
	pm := m.pkgMapOf(apkg)
	spec := pm.specs[tn]
	if spec == nil {
		return nil, m.errAt(pkg, e.Pos(), "%s: internal error: no declaration for type %q", where, tn.Name())
	}
	if m.excStructs[key] {
		return nil, m.errAt(apkg, spec.Name.Pos(), "type %q is a tagged application exception struct and cannot occur as a procedure value or wire-type reference", tn.Name())
	}
	af := pm.fileContaining(spec.Pos())
	doc, err := m.docOf(apkg, af)
	if err != nil {
		return nil, err
	}
	if !isExported(tn.Name()) {
		return nil, m.errAt(apkg, spec.Name.Pos(), "reachable type %q must be exported", tn.Name())
	}
	if spec.TypeParams != nil {
		return nil, m.errAt(apkg, spec.Name.Pos(), "type %q is generic, and generic types are rejected", tn.Name())
	}
	gd, err := m.goDeclOf(apkg, af, spec)
	if err != nil {
		return nil, err
	}

	rec := &NamedType{
		GoName:   tn.Name(),
		PkgPath:  tn.Pkg().Path(),
		PkgName:  apkg.Name,
		Filename: doc.Name,
		Pos:      doc.Position(doc.offset(spec.Name.Pos())),
		Decl:     spec,
		TypeName: tn,
	}
	m.types[key] = rec // in progress: recursive references reuse the wire name

	dir := typeDirectiveOf(gd)
	if dir == nil {
		return nil, m.errAt(apkg, spec.Name.Pos(), "reachable type %q must have exactly one @intercall type directive", tn.Name())
	}
	wire := dir.Wire
	if wire == "" && doc.IntercallGenerated {
		return nil, m.errAt(apkg, spec.Name.Pos(), "generated type %q: malformed machine line: expected the exact wire name", tn.Name())
	}
	if wire == "" {
		wire, err = GoToWire(tn.Name(), PascalCase)
		if err != nil {
			return nil, m.errAt(apkg, spec.Name.Pos(), "type %q: %v; an @intercall type directive can override the wire name", tn.Name(), err)
		}
		if reservedWireWords[wire] {
			return nil, m.errAt(apkg, spec.Name.Pos(), "type %q projects to reserved wire name %q; rename the type or add an @intercall type directive", tn.Name(), wire)
		}
	}
	rec.WireName = wire
	if IsFixedRuntimeException(wire) {
		return nil, m.errAt(apkg, spec.Name.Pos(), "type %q maps to wire name %q, which is reserved for a fixed runtime exception", tn.Name(), wire)
	}
	if prev := m.excWire[wire]; prev != nil {
		return nil, m.errAt(apkg, spec.Name.Pos(), "wire name collision: exception %q and type %q both map to wire name %q", prev.GoName, tn.Name(), wire)
	}
	if prev := m.byWire[wire]; prev != nil {
		return nil, m.errAt(apkg, spec.Name.Pos(), "wire name collision: types %q and %q both map to wire name %q", prev.GoName, tn.Name(), wire)
	}
	m.byWire[wire] = rec

	if doc.IntercallGenerated {
		sem, err := m.semanticOf(af, doc)
		if err != nil {
			return nil, err
		}
		tdecl, err := sem.TypeDecl(wire)
		if err != nil {
			return nil, m.errAt(apkg, spec.Name.Pos(), "generated type %q: %v", tn.Name(), err)
		}
		projected, err := sem.ProjectType(spec.Type, tn.Name())
		if err != nil {
			return nil, m.errAt(apkg, spec.Name.Pos(), "generated type %q: %v", tn.Name(), err)
		}
		if !sameType(tdecl.Type, projected) {
			return nil, m.errAt(apkg, spec.Name.Pos(), "generated type %q projects to a wire structure that conflicts with its semantic declaration %q", tn.Name(), wire)
		}
		rec.Generated = true
		rec.Type = tdecl.Type
		rec.Doc = tdecl.Doc
		if err := m.mapSemanticRefs(apkg, rec, sem); err != nil {
			return nil, err
		}
	} else {
		underlying, err := m.mapExpr(apkg, spec.Type, apkg.TypesInfo.TypeOf(spec.Type), fmt.Sprintf("type %q", tn.Name()))
		if err != nil {
			return nil, err
		}
		rec.Type = underlying
		rec.Doc = gd.Doc.Retained
	}
	return &syntax.NamedType{Name: &syntax.Ident{Name: wire}}, nil
}

// mapSemanticRefs maps every named reference of one recovered generated
// type's semantic underlying: each reference names another generated
// type of the same file, which the mapper reaches through its machine
// line and recovers in turn, so the whole generated type graph enters
// the reachable set.
func (m *mapper) mapSemanticRefs(pkg *packages.Package, rec *NamedType, sem *Semantic) error {
	return walkTypeRefs(rec.Type, func(wire string) error {
		spec := sem.specOf(wire)
		if spec == nil {
			return fmt.Errorf("internal error: semantic reference %q of generated type %q has no generated declaration", wire, rec.GoName)
		}
		obj, ok := pkg.TypesInfo.Defs[spec.Name]
		if !ok {
			return fmt.Errorf("internal error: no type object for generated type %q", spec.Name.Name)
		}
		tn, ok := obj.(*types.TypeName)
		if !ok {
			return fmt.Errorf("internal error: %q is not a type name", spec.Name.Name)
		}
		_, err := m.mapNamedType(pkg, spec.Name, tn, fmt.Sprintf("type %q", tn.Name()))
		return err
	})
}

// typeDirectiveOf returns the @intercall type directive of one type
// declaration, or nil.
func typeDirectiveOf(gd *GoDecl) *Directive {
	return directiveOf(gd, TypeDir)
}

// directiveOf returns the first directive of one kind in a declaration's
// doc comment, or nil. Duplicate directives are already diagnostics of
// the source-directive grammar, so at most one can survive to this
// check.
func directiveOf(gd *GoDecl, kind DirectiveKind) *Directive {
	if gd == nil || gd.Doc == nil {
		return nil
	}
	for _, d := range gd.Doc.Directives {
		if d.Kind == kind {
			return &d
		}
	}
	return nil
}

// walkTypeRefs visits every named reference of one wire structure in
// source order.
func walkTypeRefs(t syntax.TypeExpr, fn func(wire string) error) error {
	switch t := t.(type) {
	case *syntax.NamedType:
		return fn(t.Name.Name)
	case *syntax.ListType:
		return walkTypeRefs(t.Elem, fn)
	case *syntax.RecordType:
		for _, f := range t.Fields {
			if err := walkTypeRefs(f.Type, fn); err != nil {
				return err
			}
		}
	}
	return nil
}

// finalize validates the complete type graph and computes the codec
// facts: the importability of every reachable type's package, the
// stable topological emission order, and the zero-width fact of every
// type and mapped value.
func (m *mapper) finalize(tm *TypeMap) error {
	keys := make([]typeKey, 0, len(m.types))
	for k := range m.types {
		keys = append(keys, k)
	}
	sort.Slice(keys, func(i, j int) bool {
		if keys[i].pkg != keys[j].pkg {
			return keys[i].pkg < keys[j].pkg
		}
		return keys[i].name < keys[j].name
	})
	for _, k := range keys {
		if err := m.checkImportable(m.types[k]); err != nil {
			return err
		}
	}
	order, err := m.emissionOrder()
	if err != nil {
		return err
	}
	tm.Types = order
	tm.byWire = m.byWire
	for _, rec := range order {
		rec.ZeroWidth = tm.zeroWidth(rec.Type)
	}
	for _, mp := range tm.Providers {
		for _, p := range mp.Params {
			p.Value.ZeroWidth = tm.zeroWidth(p.Value.Type)
		}
		if mp.Result != nil {
			mp.Result.ZeroWidth = tm.zeroWidth(mp.Result.Type)
		}
	}
	return nil
}

// checkImportable validates that the generated binding in the output
// package can import the package of one reachable type: the package
// must not be the output package itself or a main package, must have Go
// files, must be visible under the internal rule, and must not import
// the output package.
func (m *mapper) checkImportable(rec *NamedType) error {
	if m.outPath == "" {
		return nil // no output package configured: importability is not checked
	}
	bad := func(format string, args ...any) error {
		return &Error{Filename: rec.Filename, Pos: rec.Pos, Msg: fmt.Sprintf(format, args...)}
	}
	apkg := m.pkgOf(rec.PkgPath)
	if apkg == nil {
		return fmt.Errorf("internal error: no load record for package %q of reachable type %q", rec.PkgPath, rec.GoName)
	}
	switch {
	case rec.PkgPath == m.outPath:
		return bad("reachable type %q is declared in the output package %q: the generated binding would import its own package", rec.GoName, m.outPath)
	case apkg.Name == "main":
		return bad("reachable type %q: package %q is a main package and is not importable", rec.GoName, rec.PkgPath)
	case len(apkg.CompiledGoFiles) == 0:
		return bad("reachable type %q: package %q has no Go files and is not importable", rec.GoName, rec.PkgPath)
	case !internalVisible(m.outPath, rec.PkgPath):
		return bad("reachable type %q: package %q is internal and not visible from the output package %q", rec.GoName, rec.PkgPath, m.outPath)
	case importsPath(m.outPath, apkg):
		return bad("reachable type %q: package %q imports the output package %q, which would form an import cycle", rec.GoName, rec.PkgPath, m.outPath)
	}
	return nil
}

// emissionOrder computes the stable topological emission order of
// SPEC.md "Deterministic export order": among the remaining types whose
// named dependencies have already been emitted, the lexicographically
// smallest exact wire name is chosen. If remaining nodes have no ready
// node, the type graph is recursive and rejected.
func (m *mapper) emissionOrder() ([]*NamedType, error) {
	indeg := make(map[*NamedType]int, len(m.types))
	dependents := make(map[*NamedType][]*NamedType, len(m.types))
	for _, rec := range m.types {
		seen := make(map[*NamedType]bool)
		err := walkTypeRefs(rec.Type, func(wire string) error {
			dep := m.byWire[wire]
			if dep == nil {
				return fmt.Errorf("internal error: type %q references unrecorded wire name %q", rec.WireName, wire)
			}
			if !seen[dep] {
				seen[dep] = true
				indeg[rec]++
				dependents[dep] = append(dependents[dep], rec)
			}
			return nil
		})
		if err != nil {
			return nil, err
		}
	}
	var ready []*NamedType
	for _, rec := range m.types {
		if indeg[rec] == 0 {
			ready = append(ready, rec)
		}
	}
	sort.Slice(ready, func(i, j int) bool { return ready[i].WireName < ready[j].WireName })
	var order []*NamedType
	for len(ready) > 0 {
		rec := ready[0]
		ready = ready[1:]
		order = append(order, rec)
		for _, dep := range dependents[rec] {
			indeg[dep]--
			if indeg[dep] == 0 {
				ready = insertSorted(ready, dep)
			}
		}
	}
	if len(order) != len(m.types) {
		var smallest *NamedType
		for _, rec := range m.types {
			if indeg[rec] > 0 && (smallest == nil || rec.WireName < smallest.WireName) {
				smallest = rec
			}
		}
		return nil, &Error{
			Filename: smallest.Filename,
			Pos:      smallest.Pos,
			Msg:      fmt.Sprintf("recursive type graph: type %q cannot be emitted because its named dependencies form a cycle (recursive types are rejected, including recursion through slices and anonymous records)", smallest.WireName),
		}
	}
	return order, nil
}

// insertSorted inserts rec into the wire-name-sorted slice ready.
func insertSorted(ready []*NamedType, rec *NamedType) []*NamedType {
	i := sort.Search(len(ready), func(i int) bool { return ready[i].WireName >= rec.WireName })
	ready = append(ready, nil)
	copy(ready[i+1:], ready[i:])
	ready[i] = rec
	return ready
}

// zeroWidth reports whether one wire structure occupies zero wire bytes:
// a record whose fields all occupy zero bytes, or a named reference to
// such a record. Lists and primitives always carry bytes. The facts of
// every named type must already be computed, which the topological
// order guarantees.
func (tm *TypeMap) zeroWidth(t syntax.TypeExpr) bool {
	switch t := t.(type) {
	case *syntax.PrimType:
		return false
	case *syntax.NamedType:
		return tm.byWire[t.Name.Name].ZeroWidth
	case *syntax.ListType:
		return false
	case *syntax.RecordType:
		for _, f := range t.Fields {
			if !tm.zeroWidth(f.Type) {
				return false
			}
		}
		return true
	}
	panic("tool: unknown type occurrence")
}

// tagValue unquotes one Go struct tag literal: the go/ast Value of a
// BasicLit includes the raw-string or interpreted-string delimiters, and
// the wire-tag grammar operates on the tag string itself.
func tagValue(lit string) (string, error) {
	return strconv.Unquote(lit)
}

// intercallTag parses the intercall key of one Go struct tag. ok is
// false when the tag has no intercall key; a non-nil error reports an
// intercall key that violates the wire-tag grammar of SPEC.md
// "Procedure signatures and wire values": the value is exactly one
// valid, nonreserved InterCall identifier; empty values, "-", comma
// options, duplicate intercall keys, and malformed values are errors.
// Other tag keys are ignored.
func intercallTag(raw string) (wire string, ok bool, err error) {
	rest := raw
	seen := 0
	for rest != "" {
		i := 0
		for i < len(rest) && rest[i] != ' ' {
			i++
		}
		elem := rest[:i]
		rest = rest[i:]
		for len(rest) > 0 && rest[0] == ' ' {
			rest = rest[1:]
		}
		if elem == "" {
			continue
		}
		colon := strings.IndexByte(elem, ':')
		if colon <= 0 || colon+1 >= len(elem) {
			if strings.Contains(elem, "intercall") {
				return "", true, fmt.Errorf("malformed intercall tag %q", elem)
			}
			continue
		}
		key, val := elem[:colon], elem[colon+1:]
		if key != "intercall" {
			continue
		}
		if val[0] != '"' {
			return "", true, fmt.Errorf("malformed intercall tag: expected a quoted wire name")
		}
		end := 1
		for end < len(val) && val[end] != '"' {
			end++
		}
		if end >= len(val) {
			return "", true, fmt.Errorf("malformed intercall tag: unterminated value")
		}
		if end != len(val)-1 {
			return "", true, fmt.Errorf("malformed intercall tag: unexpected text after the quoted value")
		}
		seen++
		if seen > 1 {
			return "", true, fmt.Errorf("duplicate intercall tag keys")
		}
		value := val[1:end]
		switch {
		case value == "":
			return "", true, fmt.Errorf("empty intercall tag value")
		case value == "-":
			return "", true, fmt.Errorf("intercall tag value %q: '-' is not a valid wire name", value)
		case strings.Contains(value, ","):
			return "", true, fmt.Errorf("intercall tag value %q: comma options are not supported", value)
		case !IsValidWireName(value) || reservedWireWords[value]:
			return "", true, fmt.Errorf("intercall tag value %q is not a valid nonreserved InterCall identifier", value)
		}
		wire = value
	}
	return wire, seen > 0, nil
}
