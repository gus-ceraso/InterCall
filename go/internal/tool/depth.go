package tool

import (
	"errors"
	"fmt"
	"go/ast"
	"go/token"
	"go/types"

	"github.com/cerasos/intercall/go/internal/syntax"
	"golang.org/x/tools/go/packages"
)

// This file implements the strict Go projection depth preflight of
// SPEC.md "Strict Go projection depth": the exact 4,096-occurrence
// native representability ceiling. Import checks the validated
// interface occurrence before any recursive naming, model, codec, or
// emission work; export checks the physical Go occurrences of the
// reachable provider, exception, and named-type graph before any
// recursive mapping, and the trusted semantic metadata of a generated
// file before any recursive projection or comparison. The preflight is
// iterative with an explicit work stack, so its own call-stack use is
// independent of type nesting; it is cycle-safe, and recursive type
// graphs keep their existing recursive-type diagnostics, which still
// own cycles.

// maxProjectionDepth is the exact maximum resolved type depth of the
// strict Go projection: 4,096 occurrences. A root — a type
// declaration's underlying type, an exception payload, a procedure
// parameter, or a procedure return — has depth 1, and each
// list-element, record-field, named-reference-to-underlying,
// defined-type-to-underlying, or alias-expansion edge adds 1. This is
// a native representability boundary, not a protocol grammar rule and
// not a configurable resource policy.
const maxProjectionDepth = 4096

// projectionDepthMessage renders the stable physical diagnostic of the
// first source occurrence exceeding maxProjectionDepth.
func projectionDepthMessage(where string) string {
	return fmt.Sprintf("%s exceeds the strict Go projection depth limit of %d occurrences", where, maxProjectionDepth)
}

// errRecursiveGraph aborts the export preflight when the reachable
// type graph is recursive: the existing recursive-type diagnostics own
// cycles, so the preflight stops without a depth diagnostic and the
// mapping reports the recursive graph.
var errRecursiveGraph = errors.New("tool: recursive type graph")

// checkSyntaxProjectionDepth verifies that every type occurrence of
// one validated interface file fits the strict Go projection ceiling,
// reporting the first over-limit occurrence at its exact source
// position.
//
// The roots are the type declarations' underlying types, the exception
// payloads, the procedure parameters, and the procedure returns, each
// at depth 1; a list-element, record-field, or
// named-reference-to-underlying edge adds 1. The walk uses an explicit
// work stack and follows named references through the file's type
// declarations, which validation guarantees are earlier declarations.
// The walk is cycle-safe for unvalidated input: a named reference to a
// type already on the current path aborts the walk, and a type whose
// underlying was already walked at a deeper root depth is not walked
// again, so shared references stay linear.
func checkSyntaxProjectionDepth(f *syntax.File) error {
	st := &syntaxDepthState{
		f:         f,
		types:     make(map[string]*syntax.TypeDecl),
		maxWalked: make(map[string]int),
		gray:      make(map[string]bool),
	}
	for _, d := range f.Decls {
		if td, ok := d.(*syntax.TypeDecl); ok {
			st.types[td.Name.Name] = td
		}
	}
	for _, d := range f.Decls {
		switch d := d.(type) {
		case *syntax.TypeDecl:
			if err := st.walk(d.Type, 1, "type "+d.Name.Name); err != nil {
				return err
			}
		case *syntax.ExceptionDecl:
			if d.Type != nil {
				if err := st.walk(d.Type, 1, "exception "+d.Name.Name); err != nil {
					return err
				}
			}
		case *syntax.ProcDecl:
			for _, p := range d.Params {
				if err := st.walk(p.Type, 1, fmt.Sprintf("parameter %q of procedure %s", p.Name.Name, d.Name.Name)); err != nil {
					return err
				}
			}
			if d.Result != nil {
				if err := st.walk(d.Result, 1, "return type of procedure "+d.Name.Name); err != nil {
					return err
				}
			}
		}
	}
	return nil
}

// syntaxDepthState is the working state of one interface-file depth
// walk: the declaration table for named references, the deepest root
// depth at which each declaration's underlying was already walked, and
// the declaration names on the current path.
type syntaxDepthState struct {
	f         *syntax.File
	types     map[string]*syntax.TypeDecl // wire name -> type declaration
	maxWalked map[string]int              // wire name -> deepest underlying root depth walked
	gray      map[string]bool             // wire names on the current path
}

// syntaxFrame is one occurrence of the syntax depth walk.
type syntaxFrame struct {
	t     syntax.TypeExpr
	depth int
	where string
}

// syntaxExit marks the end of one named reference's underlying walk.
type syntaxExit struct{ wire string }

// walk visits every occurrence of one root type in source order with
// an explicit work stack. The root occurrence has depth 1; each
// list-element, record-field, or named-reference-to-underlying edge
// adds 1, and the first occurrence exceeding maxProjectionDepth is
// reported at its exact source position.
func (st *syntaxDepthState) walk(root syntax.TypeExpr, depth int, where string) error {
	var stack []any
	stack = append(stack, syntaxFrame{t: root, depth: depth, where: where})
	for len(stack) > 0 {
		n := stack[len(stack)-1]
		stack = stack[:len(stack)-1]
		switch e := n.(type) {
		case syntaxExit:
			delete(st.gray, e.wire)
		case syntaxFrame:
			if e.depth > maxProjectionDepth {
				return st.errf(e.t.Span(), projectionDepthMessage(e.where))
			}
			switch t := e.t.(type) {
			case *syntax.NamedType:
				td := st.types[t.Name.Name]
				if td == nil {
					continue // unresolved: validation owns this diagnostic
				}
				next := e.depth + 1
				if st.gray[t.Name.Name] {
					return nil // recursive graph: the recursive-type diagnostic owns the error
				}
				if st.maxWalked[t.Name.Name] >= next {
					continue // a deeper walk of the same underlying already fit
				}
				st.maxWalked[t.Name.Name] = next
				st.gray[t.Name.Name] = true
				stack = append(stack, syntaxExit{wire: t.Name.Name})
				stack = append(stack, syntaxFrame{t: td.Type, depth: next, where: "type " + td.Name.Name})
			case *syntax.ListType:
				stack = append(stack, syntaxFrame{t: t.Elem, depth: e.depth + 1, where: e.where})
			case *syntax.RecordType:
				for i := len(t.Fields) - 1; i >= 0; i-- {
					f := t.Fields[i]
					stack = append(stack, syntaxFrame{
						t:     f.Type,
						depth: e.depth + 1,
						where: fmt.Sprintf("field %q of %s", f.Name.Name, e.where),
					})
				}
			}
		}
	}
	return nil
}

// errf builds the physical depth diagnostic of one over-limit
// occurrence.
func (st *syntaxDepthState) errf(span syntax.Span, msg string) error {
	pos := st.f.Position(span.Start)
	return &Error{
		Filename: st.f.Name,
		Pos:      Position{Offset: pos.Offset, Line: pos.Line, Column: pos.Column},
		Msg:      msg,
	}
}

// depthMsg returns the message of one checkSyntaxProjectionDepth
// diagnostic, which is always an *Error.
func depthMsg(err error) string {
	if de, ok := err.(*Error); ok {
		return de.Msg
	}
	return err.Error()
}

// preflightGoDepth verifies that every physical Go type occurrence the
// export mapping would recurse through fits the strict Go projection
// ceiling, before any recursive mapping, metadata projection, override,
// model, codec, or emission work.
//
// The roots are the provider wire values — each wire parameter and the
// optional data result in provider order — and the tagged
// payload-exception types in collection order, each at depth 1. A
// slice-element, struct-field, named-reference-to-underlying,
// defined-type-to-underlying, or alias-expansion edge adds 1. The walk
// follows exactly the references the mapper would recurse into:
// predeclared types, tagged exception types, unexported types, and
// generic types are leaves because the mapper reports them without
// descending. The walk uses an explicit work stack, re-walks a named
// type's underlying only when it is reached at a deeper root depth, and
// aborts on a recursive graph, which keeps its existing recursive-type
// diagnostic.
func (m *mapper) preflightGoDepth(providers []*Provider, pkgs []*ExplicitPackage) error {
	s := &goDepthState{
		m:         m,
		maxWalked: make(map[typeKey]int),
		gray:      make(map[typeKey]bool),
	}
	for _, p := range providers {
		qname := p.Pkg.Path + "." + p.Name
		fn := p.Func
		if fn.Type.Params == nil {
			continue // the mapper reports this internal error itself
		}
		for _, field := range fn.Type.Params.List[1:] {
			if len(field.Names) == 0 {
				continue // the mapper reports this internal error itself
			}
			if err := s.walk(p.Pkg.pkg, field.Type, 1, fmt.Sprintf("parameter %q of procedure %q", field.Names[0].Name, qname)); err != nil {
				if err == errRecursiveGraph {
					return nil // the recursive-type diagnostics own cycles
				}
				return err
			}
		}
		if p.DataResult {
			if err := s.walk(p.Pkg.pkg, fn.Type.Results.List[0].Type, 1, fmt.Sprintf("the return value of procedure %q", qname)); err != nil {
				if err == errRecursiveGraph {
					return nil // the recursive-type diagnostics own cycles
				}
				return err
			}
		}
	}
	err := walkExceptionDecls(pkgs, func(p *ExplicitPackage, doc *Document, gd *GoDecl) error {
		if gd.Kind != GoType {
			return nil // sentinels carry no payload
		}
		obj := p.pkg.Types.Scope().Lookup(gd.Name)
		tn, ok := obj.(*types.TypeName)
		if !ok {
			return nil // the collector reports this internal error itself
		}
		spec := m.pkgMapOf(p.pkg).specs[tn]
		if spec == nil {
			return nil
		}
		return s.walk(p.pkg, spec.Type, 1, fmt.Sprintf("exception %q", gd.Name))
	})
	if err == errRecursiveGraph {
		return nil // the existing recursive-type diagnostics own cycles
	}
	if err != nil {
		return err
	}
	if s.over != nil {
		return s.m.errAt(s.over.pkg, s.over.pos, "%s", projectionDepthMessage(s.over.where))
	}
	return nil
}

// goDepthState is the working state of one physical Go depth walk: the
// deepest root depth at which each named type or alias was already
// walked, the type keys on the current path, and the first over-limit
// occurrence recorded before any recursive graph abort.
type goDepthState struct {
	m         *mapper
	stack     []any // gdFrame or gdExit
	maxWalked map[typeKey]int
	gray      map[typeKey]bool
	over      *gdOver // first occurrence exceeding maxProjectionDepth
}

// gdFrame is one occurrence of the physical Go depth walk. pkg is the
// package whose type information resolves the occurrence's references.
type gdFrame struct {
	pkg   *packages.Package
	e     ast.Expr
	depth int
	where string
}

// gdExit marks the end of one named type's underlying walk.
type gdExit struct{ key typeKey }

// gdOver records the first over-limit occurrence of one walk, reported
// only when the walk completes without finding a recursive graph.
type gdOver struct {
	pkg   *packages.Package
	pos   token.Pos
	where string
}

// walk visits every occurrence of one root type expression in source
// order with an explicit work stack. The root occurrence has depth 1;
// each slice-element, struct-field, named-reference-to-underlying,
// defined-type-to-underlying, or alias-expansion edge adds 1. The
// first occurrence exceeding maxProjectionDepth is recorded but the
// walk continues, so a recursive graph anywhere in the reachable set
// still aborts the walk and keeps its existing recursive-type
// diagnostic; only a walk that completes without a cycle reports the
// recorded over-limit occurrence at its exact physical position.
func (s *goDepthState) walk(pkg *packages.Package, root ast.Expr, depth int, where string) error {
	s.stack = append(s.stack, gdFrame{pkg: pkg, e: root, depth: depth, where: where})
	for len(s.stack) > 0 {
		n := s.stack[len(s.stack)-1]
		s.stack = s.stack[:len(s.stack)-1]
		switch e := n.(type) {
		case gdExit:
			delete(s.gray, e.key)
		case gdFrame:
			if e.depth > maxProjectionDepth && s.over == nil {
				s.over = &gdOver{pkg: e.pkg, pos: e.e.Pos(), where: e.where}
			}
			switch t := e.e.(type) {
			case *ast.ParenExpr:
				// Parentheses are transparent to Go type identity and
				// add no depth, exactly as in the mapping.
				s.stack = append(s.stack, gdFrame{pkg: e.pkg, e: t.X, depth: e.depth, where: e.where})
			case *ast.Ident:
				if tn, ok := e.pkg.TypesInfo.ObjectOf(t).(*types.TypeName); ok {
					if err := s.reference(e.pkg, tn, e.depth, e.where); err != nil {
						return err
					}
				}
			case *ast.SelectorExpr:
				switch tt := e.pkg.TypesInfo.TypeOf(t).(type) {
				case *types.Named:
					if err := s.reference(e.pkg, tt.Obj(), e.depth, e.where); err != nil {
						return err
					}
				case *types.Alias:
					if err := s.reference(e.pkg, tt.Obj(), e.depth, e.where); err != nil {
						return err
					}
				}
			case *ast.ArrayType:
				if t.Len == nil {
					s.stack = append(s.stack, gdFrame{pkg: e.pkg, e: t.Elt, depth: e.depth + 1, where: e.where})
				}
				// Arrays are rejected by the mapping without recursion.
			case *ast.StructType:
				for i := len(t.Fields.List) - 1; i >= 0; i-- {
					f := t.Fields.List[i]
					if len(f.Names) == 0 {
						continue // embedded fields are rejected without recursion
					}
					s.stack = append(s.stack, gdFrame{
						pkg:   e.pkg,
						e:     f.Type,
						depth: e.depth + 1,
						where: fmt.Sprintf("field %q of %s", f.Names[0].Name, e.where),
					})
				}
			}
			// Every other form is not a wire value and is rejected by
			// the mapping before any recursion.
		}
	}
	return nil
}

// reference follows one named-type or alias reference at depth d: the
// reference to an ordinary defined type reaches its declaration's
// underlying type at depth d+1, and the reference to an alias reaches
// the alias's RHS at depth d+1. References the mapping rejects before
// descending — predeclared types, tagged exception types, unexported
// types, and generic types — are leaves. A recursive graph aborts the
// preflight; a type whose underlying was already walked at a deeper
// root depth is not walked again.
func (s *goDepthState) reference(pkg *packages.Package, tn *types.TypeName, depth int, where string) error {
	if tn.Pkg() == nil {
		return nil // a predeclared type: byte, uint8, any, error, ...
	}
	key := typeKey{pkg: tn.Pkg().Path(), name: tn.Name()}
	if s.m.excTypes[key] {
		return nil // an exception type: rejected as a wire-type reference
	}
	if !isExported(tn.Name()) {
		return nil // reachable types must be exported: the mapper reports it
	}
	apkg := s.m.pkgOf(tn.Pkg().Path())
	if apkg == nil {
		return nil // outside the load graph: the mapper reports an internal error
	}
	pm := s.m.pkgMapOf(apkg)
	spec := pm.specs[tn]
	if spec == nil {
		return nil // the mapper reports the missing declaration
	}
	if spec.TypeParams != nil {
		return nil // generic types are rejected before any recursion
	}
	next := depth + 1
	if s.gray[key] {
		return errRecursiveGraph
	}
	if s.maxWalked[key] >= next {
		return nil
	}
	s.maxWalked[key] = next
	if _, isAlias := tn.Type().(*types.Alias); isAlias {
		// An alias flattens to its RHS in the declaring package; the
		// surrounding value context stays, exactly as in the mapping.
		s.stack = append(s.stack, gdFrame{pkg: apkg, e: spec.Type, depth: next, where: where})
		return nil
	}
	s.gray[key] = true
	s.stack = append(s.stack, gdExit{key: key})
	s.stack = append(s.stack, gdFrame{pkg: apkg, e: spec.Type, depth: next, where: fmt.Sprintf("type %q", tn.Name())})
	return nil
}
