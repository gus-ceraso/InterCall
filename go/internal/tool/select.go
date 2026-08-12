package tool

import (
	"fmt"
	"go/ast"
	"go/types"
	"sort"
	"strings"
)

// This file implements procedure eligibility, the --include and
// --exclude filters, and the exact provider signatures of SPEC.md
// "Package discovery and selection" and "Procedure signatures and wire
// values".

// Provider is one selected procedure: an eligible function of an
// explicit package that survived the include and exclude filters and
// passed the exact signature checks.
//
// Name is the Go symbol, Func the package-level function declaration,
// and Doc the parsed document record of its declaration, whose
// directives carry the optional @intercall procedure wire name and
// whose Retained text is the procedure documentation. Params lists the
// wire parameter names in source order (the context parameter is not a
// wire value), and DataResult reports whether the signature has a data
// result before the mandatory final error.
type Provider struct {
	Pkg        *ExplicitPackage
	Name       string
	Func       *ast.FuncDecl
	Doc        *GoDecl
	Params     []string
	DataResult bool
}

// selector is one parsed --include or --exclude value of the exact form
// full/import/path.Symbol.
type selector struct {
	path   string
	symbol string
	text   string // the exact flag text, for diagnostics
}

// candidate is one eligible function of an explicit package.
type candidate struct {
	pkg  *ExplicitPackage
	decl *GoDecl
	fn   *ast.FuncDecl
}

// hasDirective reports whether a declaration's doc carries a directive
// of the given kind.
func hasDirective(doc *GoDoc, kind DirectiveKind) bool {
	if doc == nil {
		return false
	}
	for _, d := range doc.Directives {
		if d.Kind == kind {
			return true
		}
	}
	return false
}

// selectProviders applies the include and exclude filters to the
// eligible functions of the explicit packages and validates the exact
// signature of every selected procedure. Errors are reported in
// deterministic order: directive contradictions and filter grammar and
// resolution errors first, then signature diagnostics in package and
// symbol order.
func selectProviders(pkgs []*ExplicitPackage, inc, exc []*selector) ([]*Provider, error) {
	var candidates []*candidate
	var diags []*Error
	for _, p := range pkgs {
		cs, err := collectCandidates(p)
		if err != nil {
			return nil, err
		}
		for _, c := range cs {
			if !isExported(c.decl.Name) {
				diags = append(diags, &Error{
					Filename: fileOf(p, c.decl),
					Pos:      c.decl.Pos,
					Msg:      "contradictory @intercall procedure directive: it applies only to an exported function",
				})
				continue
			}
			candidates = append(candidates, c)
		}
	}
	if err := firstError(diags); err != nil {
		return nil, err
	}
	sort.Slice(candidates, func(i, j int) bool {
		if candidates[i].pkg.Path != candidates[j].pkg.Path {
			return candidates[i].pkg.Path < candidates[j].pkg.Path
		}
		return candidates[i].decl.Name < candidates[j].decl.Name
	})

	byKey := make(map[string]*candidate, len(candidates))
	for _, c := range candidates {
		byKey[c.pkg.Path+"."+c.decl.Name] = c
	}

	resolvedInc := make(map[string]*candidate, len(inc))
	for _, s := range inc {
		c, err := resolveSelector(s, pkgs, byKey)
		if err != nil {
			return nil, err
		}
		resolvedInc[s.text] = c
	}
	resolvedExc := make(map[string]*candidate, len(exc))
	for _, s := range exc {
		c, err := resolveSelector(s, pkgs, byKey)
		if err != nil {
			return nil, err
		}
		resolvedExc[s.text] = c
	}

	// With no --include every eligible function is selected; otherwise
	// the include set restricts the selection. Excludes remove from it,
	// so exclusion wins; naming a symbol in both sets is valid and
	// excludes it.
	var selected []*candidate
	for _, c := range candidates {
		if len(resolvedInc) > 0 && resolvedInc[c.pkg.Path+"."+c.decl.Name] == nil {
			continue
		}
		if resolvedExc[c.pkg.Path+"."+c.decl.Name] != nil {
			continue
		}
		selected = append(selected, c)
	}

	providers := make([]*Provider, 0, len(selected))
	for _, c := range selected {
		p, err := buildProvider(c)
		if err != nil {
			return nil, err
		}
		providers = append(providers, p)
	}
	return providers, nil
}

// fileOf returns the logical path of the file holding one declaration.
func fileOf(p *ExplicitPackage, decl *GoDecl) string {
	for _, doc := range p.docs {
		for _, d := range doc.Decls {
			if d == decl {
				return doc.Name
			}
		}
	}
	return ""
}

// collectCandidates gathers the eligible functions of one explicit
// package: exported package-level functions whose doc has an
// @intercall procedure directive in a nongenerated file. Tagged
// functions in generated files are not eligible (SPEC.md "Package
// discovery and selection") and are skipped, not reported.
func collectCandidates(p *ExplicitPackage) ([]*candidate, error) {
	funcs := make(map[string]*ast.FuncDecl)
	for _, af := range p.pkg.Syntax {
		for _, decl := range af.Decls {
			if fd, ok := decl.(*ast.FuncDecl); ok && fd.Recv == nil {
				funcs[fd.Name.Name] = fd
			}
		}
	}
	var cs []*candidate
	for _, file := range p.files {
		doc := p.docs[file]
		if doc.Generated {
			continue
		}
		for _, decl := range doc.Decls {
			if decl.Kind != GoFunc || !hasDirective(decl.Doc, ProcedureDir) {
				continue
			}
			fd := funcs[decl.Name]
			if fd == nil {
				return nil, fmt.Errorf("internal error: no function declaration for %q in %s", decl.Name, p.Path)
			}
			cs = append(cs, &candidate{pkg: p, decl: decl, fn: fd})
		}
	}
	return cs, nil
}

// parseSelectors parses and checks the grammar of one filter list. The
// exact form is full/import/path.Symbol: the symbol is the text after
// the last dot, and the remaining text is a full import path. A
// duplicate selector text in the same list is an error; naming a symbol
// in both the include and exclude lists is valid.
func parseSelectors(texts []string) ([]*selector, error) {
	seen := make(map[string]bool, len(texts))
	out := make([]*selector, 0, len(texts))
	for _, text := range texts {
		bad := func(format string, args ...any) ([]*selector, error) {
			return nil, &Error{Filename: text, Pos: Position{Line: 1, Column: 1}, Msg: fmt.Sprintf(format, args...)}
		}
		if seen[text] {
			return nil, &Error{Filename: text, Pos: Position{Line: 1, Column: 1}, Msg: fmt.Sprintf("duplicate selector %q", text)}
		}
		seen[text] = true
		dot := strings.LastIndexByte(text, '.')
		if dot <= 0 || dot == len(text)-1 {
			return bad("malformed selector %q: expected full/import/path.Symbol", text)
		}
		s := &selector{path: text[:dot], symbol: text[dot+1:], text: text}
		if !validFilterPath(s.path) {
			return bad("malformed selector %q: %q is not a full import path", text, s.path)
		}
		if !IsValidGoIdentifier(s.symbol) {
			return bad("malformed selector %q: %q is not a Go identifier", text, s.symbol)
		}
		out = append(out, s)
	}
	return out, nil
}

// validFilterPath checks the import-path shape of a selector: slash
// elements that are nonempty and neither "." nor "..", no leading or
// trailing slash, no relative-pattern or query prefixes.
func validFilterPath(path string) bool {
	if path == "" || strings.HasPrefix(path, "/") || strings.HasPrefix(path, ".") {
		return false
	}
	if strings.Contains(path, "...") {
		return false
	}
	for _, elem := range strings.Split(path, "/") {
		if elem == "" || elem == "." || elem == ".." {
			return false
		}
	}
	return true
}

// resolveSelector resolves one filter to an eligible function of an
// explicit package. The filter's package path must be an explicit
// package and its symbol an eligible function there; malformed,
// unknown, duplicate, unexported, untagged, and method selectors are
// errors. A generic eligible function resolves, and selecting it is an
// error; an exclude that names one is valid and removes it from a set
// it could never enter.
func resolveSelector(s *selector, pkgs []*ExplicitPackage, byKey map[string]*candidate) (*candidate, error) {
	bad := func(format string, args ...any) (*candidate, error) {
		return nil, &Error{Filename: s.text, Pos: Position{Line: 1, Column: 1}, Msg: fmt.Sprintf(format, args...)}
	}
	var pkg *ExplicitPackage
	for _, p := range pkgs {
		if p.Path == s.path {
			pkg = p
			break
		}
	}
	if pkg == nil {
		return bad("unknown selector %q: no explicit package with import path %q", s.text, s.path)
	}

	key := s.path + "." + s.symbol
	if c := byKey[key]; c != nil {
		return c, nil
	}

	// The symbol is not an eligible function. Distinguish the error
	// categories: a generated-file function (generated files supply no
	// selectable procedures), a method, an unexported function, an
	// untagged function, or an unknown symbol.
	genFuncs := make(map[string]bool) // functions declared in generated files
	for _, doc := range pkg.docs {
		if !doc.Generated {
			continue
		}
		for _, decl := range doc.Decls {
			if decl.Kind == GoFunc {
				genFuncs[decl.Name] = true
			}
		}
	}
	if genFuncs[s.symbol] {
		return bad("untagged selector %q: %q is declared in a generated file, and generated files do not supply selectable procedures", s.text, s.symbol)
	}
	for _, af := range pkg.pkg.Syntax {
		for _, decl := range af.Decls {
			fd, ok := decl.(*ast.FuncDecl)
			if !ok || fd.Name.Name != s.symbol {
				continue
			}
			if fd.Recv != nil {
				return bad("method selector %q: %q in %q is a method, not a package-level function", s.text, s.symbol, s.path)
			}
			if !isExported(s.symbol) {
				return bad("unexported selector %q: %q in %q is not exported", s.text, s.symbol, s.path)
			}
			return bad("untagged selector %q: %q in %q has no @intercall procedure directive", s.text, s.symbol, s.path)
		}
	}
	return bad("unknown selector %q: no function %q in %q", s.text, s.symbol, s.path)
}

// buildProvider validates the exact signature of one selected procedure
// and builds its generation record.
//
// A selected provider is an exported, nongeneric, nonvariadic
// package-level function without a receiver whose signature is exactly
// one of
//
//	func(context.Context, P1, ..., Pn) error
//	func(context.Context, P1, ..., Pn) (T, error)
//
// The first parameter has the exact type identity context.Context
// (aliases resolving to it are accepted, defined lookalikes are not),
// the final result is the predeclared error interface, there is at most
// one data result, and every wire parameter has a nonblank Go name.
// Methods, generics, variadics, and every other result form are
// rejected.
func buildProvider(c *candidate) (*Provider, error) {
	fn := c.fn
	bad := func(format string, args ...any) (*Provider, error) {
		msg := fmt.Sprintf("procedure %q: %s", c.pkg.Path+"."+c.decl.Name, fmt.Sprintf(format, args...))
		return nil, &Error{Filename: fileOf(c.pkg, c.decl), Pos: c.decl.Pos, Msg: msg}
	}

	if fn.Recv != nil {
		return bad("it is a method, and methods are rejected")
	}
	if fn.Type.TypeParams != nil {
		return bad("it is generic, and generics are rejected")
	}

	params := fn.Type.Params
	if params == nil || len(params.List) == 0 {
		return bad("it must take context.Context as its first parameter")
	}
	if _, ok := params.List[len(params.List)-1].Type.(*ast.Ellipsis); ok {
		return bad("it is variadic, and variadics are rejected")
	}

	info := c.pkg.pkg.TypesInfo
	if info == nil {
		return bad("it could not be type-checked")
	}
	first := info.TypeOf(params.List[0].Type)
	if !isContextType(first) {
		return bad("its first parameter must have the exact type identity context.Context")
	}

	var names []string
	for _, field := range params.List[1:] {
		if len(field.Names) == 0 {
			return bad("its parameter %d must have a name", len(names)+2)
		}
		for _, name := range field.Names {
			if name.Name == "_" {
				return bad("its parameter %d must not be the blank identifier", len(names)+2)
			}
			names = append(names, name.Name)
		}
	}

	dataResult := false
	switch results := fn.Type.Results; {
	case results == nil || len(results.List) == 0:
		return bad("it must return error as its final result")
	case len(results.List) == 1:
		if !isErrorType(info.TypeOf(results.List[0].Type)) {
			return bad("its final result must be the predeclared error interface")
		}
	case len(results.List) == 2:
		if !isErrorType(info.TypeOf(results.List[1].Type)) {
			return bad("its final result must be the predeclared error interface")
		}
		dataResult = true
	default:
		return bad("it has %d results; at most one data result is allowed", len(results.List))
	}

	return &Provider{
		Pkg:        c.pkg,
		Name:       c.decl.Name,
		Func:       fn,
		Doc:        c.decl,
		Params:     names,
		DataResult: dataResult,
	}, nil
}

// isContextType reports whether t has the exact type identity of
// context.Context: a named type declared as Context by the context
// package. An alias resolving to context.Context is the same type
// object and is accepted; a defined lookalike is a different object and
// is rejected.
func isContextType(t types.Type) bool {
	named, ok := types.Unalias(t).(*types.Named)
	if !ok {
		return false
	}
	obj := named.Obj()
	return obj != nil && obj.Pkg() != nil && obj.Pkg().Path() == "context" && obj.Name() == "Context"
}

// isErrorType reports whether t is the predeclared error interface: the
// identical type object of types.Universe. An alias resolving to error
// is the same object and is accepted; a defined lookalike or an
// anonymous interface with the same method set is a different type and
// is rejected.
func isErrorType(t types.Type) bool {
	return types.Unalias(t) == types.Universe.Lookup("error").Type()
}
