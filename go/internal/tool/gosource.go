package tool

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/scanner"
	"go/token"
	"sort"
	"strings"
)

// Position is a byte offset with its one-based physical line and byte
// column, following the same conventions as internal/syntax.Position.
//
// Lines follow go/token semantics: a line starts at offset zero and after
// every '\n', and the column is the byte distance from the line start plus
// one. A '\r' is an ordinary byte; CRLF therefore counts as one line break
// at the '\n'. Positions are always physical: //line directives never
// rewrite them.
type Position struct {
	Offset int
	Line   int
	Column int
}

// String renders the position as "line:column".
func (p Position) String() string {
	return fmt.Sprintf("%d:%d", p.Line, p.Column)
}

// Error is one source-document diagnostic with its exact physical
// position, rendered as "path:line:column: message" like
// internal/syntax.Error.
type Error struct {
	Filename string
	Pos      Position
	Msg      string
}

// Error renders the diagnostic as "path:line:column: message", or
// "line:column: message" when no filename is set.
func (e *Error) Error() string {
	if e.Filename != "" {
		return fmt.Sprintf("%s:%d:%d: %s", e.Filename, e.Pos.Line, e.Pos.Column, e.Msg)
	}
	return fmt.Sprintf("%d:%d: %s", e.Pos.Line, e.Pos.Column, e.Msg)
}

// GoDeclKind identifies the declaration kind of one package-level Go
// declaration.
type GoDeclKind int

const (
	// GoFunc is a package-level function without a receiver.
	GoFunc GoDeclKind = iota
	// GoMethod is a function declaration with a receiver.
	GoMethod
	// GoVar is a package-level variable declaration.
	GoVar
	// GoType is a package-level type declaration.
	GoType
	// GoConst is a package-level constant declaration.
	GoConst
	// GoImport is one import specification.
	GoImport
)

// String renders the declaration kind for diagnostics.
func (k GoDeclKind) String() string {
	switch k {
	case GoFunc:
		return "function"
	case GoMethod:
		return "method"
	case GoVar:
		return "variable"
	case GoType:
		return "type"
	case GoConst:
		return "constant"
	case GoImport:
		return "import"
	}
	panic("tool: unknown declaration kind")
}

// DirectiveKind identifies one InterCall source-directive form from
// SPEC.md "Source directives and Go documentation".
type DirectiveKind int

const (
	// ProcedureDir is "@intercall procedure [wire_name]".
	ProcedureDir DirectiveKind = iota
	// ExceptionDir is "@intercall exception [wire_name]".
	ExceptionDir
	// TypeDir is "@intercall type [wire_name]".
	TypeDir
	// ParamDir is "@intercall param GoName wire_name".
	ParamDir
	// ParamDocDir is "@param GoName text".
	ParamDocDir
	// ReturnDocDir is "@return text".
	ReturnDocDir
)

// String renders the directive's leading token for diagnostics.
func (k DirectiveKind) String() string {
	switch k {
	case ProcedureDir:
		return "@intercall procedure"
	case ExceptionDir:
		return "@intercall exception"
	case TypeDir:
		return "@intercall type"
	case ParamDir:
		return "@intercall param"
	case ParamDocDir:
		return "@param"
	case ReturnDocDir:
		return "@return"
	}
	panic("tool: unknown directive kind")
}

// Directive is one parsed InterCall directive of a declaration's doc
// comment, with the physical position of its leading '@'.
//
// GoName is set for ParamDir and ParamDocDir, Wire for ProcedureDir,
// ExceptionDir, TypeDir, and ParamDir, and Text for ParamDocDir and
// ReturnDocDir. Directives appear in source order.
type Directive struct {
	Kind   DirectiveKind
	Pos    Position
	GoName string
	Wire   string
	Text   string

	textOffset int // physical offset of Text's first byte, for diagnostics
}

// GoDoc is the parsed doc comment of one declaration.
//
// Retained is the normalized documentation string after the directive
// lines are removed: CRLF and bare CR become LF, trailing spaces and tabs
// are removed from each line, leading and trailing blank lines are
// removed, the longest spaces-and-tabs prefix shared by all nonblank
// lines is removed, and the lines are joined with LF. An empty Retained
// is an empty documentation slot. Directives lists the recognized
// directives in source order.
type GoDoc struct {
	Retained   string
	Directives []Directive
}

// GoTypeInfo records the syntactic facts of one type declaration that
// decide which directives can apply to it.
type GoTypeInfo struct {
	Alias   bool // type A = B is an alias, not a defined type
	Generic bool // type A[T any] is a generic declaration
	Struct  bool // the underlying type is an anonymous struct
}

// GoField is one struct field of a type declaration with its preceding
// documentation.
//
// A field's doc becomes field documentation per SPEC.md "Source
// directives and Go documentation". No InterCall directive applies to a
// field, so Doc is the plain normalized comment text. Name is empty for
// an embedded field, whose type expression is not a wire field.
type GoField struct {
	Name string
	Pos  Position
	Doc  string
}

// GoDecl is one package-level Go declaration with its parsed doc comment.
//
// Var and const declarations can declare several names in one
// specification; Names holds all of them in source order and Name is the
// first. Every other kind declares exactly one name, and Names is nil.
// Doc is nil when the declaration has no doc comment. Fields and Type are
// set only for GoType declarations whose underlying type is a struct.
type GoDecl struct {
	Kind   GoDeclKind
	Name   string
	Names  []string
	Pos    Position
	Doc    *GoDoc
	Type   GoTypeInfo
	Fields []*GoField
}

// Document is one parsed Go source file: its generated-file marker
// classification and the package-level declarations in source order with
// their InterCall directives and retained documentation.
//
// ParseGoSource applies SPEC.md "Source directives and Go documentation":
// InterCall directives occupy complete logical lines of a declaration's
// doc comment, and every package-level declaration is checked for
// malformed, unknown, bare, misplaced, contradictory, duplicate, and
// unresolved directives. Declarations without doc comments cannot carry
// directives and are not checked.
type Document struct {
	Name string
	Size int

	// Generated reports whether the file is recognized by Go's standard
	// generated-file marker ("// Code generated ... DO NOT EDIT." before
	// the package clause), following go/ast.IsGenerated.
	Generated bool

	// IntercallGenerated reports whether the file's first line is the
	// exact marker "// Code generated by intercall-go; DO NOT EDIT.",
	// the trust boundary for generated machine metadata per SPEC.md
	// "Safe import and re-export metadata".
	IntercallGenerated bool

	Decls []*GoDecl

	src   []byte
	lines []int       // offsets of every line start; lines[0] is always 0
	tok   *token.File // the file's token.File, for physical offsets
}

// intercallGeneratedMarker is the exact generated-file marker that forms
// the machine-metadata trust boundary.
const intercallGeneratedMarker = "// Code generated by intercall-go; DO NOT EDIT."

// ParseGoSource parses one Go source file and applies the InterCall
// directive grammar to every package-level declaration.
//
// The name is used only for diagnostics. A Go syntax error returns a nil
// Document and a *Error pointing at the physical position of the first
// offending token. Directive and documentation errors are collected
// across all declarations and the earliest diagnostic by physical
// position (ties broken by message) is returned. Positions are physical
// byte offsets into src: //line directives never rewrite them.
func ParseGoSource(name string, src []byte) (*Document, error) {
	fset := token.NewFileSet()
	af, err := parser.ParseFile(fset, name, src, parser.ParseComments|parser.SkipObjectResolution)
	if err != nil {
		return nil, goParseError(name, src, err)
	}
	tf := fset.File(af.Package)
	doc := &Document{
		Name:               name,
		Size:               len(src),
		Generated:          ast.IsGenerated(af),
		IntercallGenerated: firstLine(src) == intercallGeneratedMarker,
		src:                src,
		lines:              lineStarts(src),
		tok:                tf,
	}
	off := func(p token.Pos) int { return tf.Offset(p) }

	var errs []*Error
	doc.Decls = buildDecls(af, doc, off, &errs)
	if len(errs) > 0 {
		sort.Slice(errs, func(i, j int) bool {
			a, b := errs[i].Pos, errs[j].Pos
			if a.Line != b.Line {
				return a.Line < b.Line
			}
			if a.Column != b.Column {
				return a.Column < b.Column
			}
			return errs[i].Msg < errs[j].Msg
		})
		return nil, errs[0]
	}
	return doc, nil
}

// goParseError converts a go/parser error list into a *Error at the
// physical position of the first offending token. The scanner's offset
// is physical even when //line directives adjust its line and column,
// so the position is recomputed from the raw bytes.
func goParseError(name string, src []byte, err error) *Error {
	list, ok := err.(scanner.ErrorList)
	if !ok || list.Len() == 0 {
		return &Error{Filename: name, Pos: Position{Offset: len(src), Line: 1, Column: 1}, Msg: err.Error()}
	}
	pe := list[0]
	pos := positionAt(src, pe.Pos.Offset)
	return &Error{Filename: name, Pos: pos, Msg: pe.Msg}
}

// positionAt computes the one-based physical line and byte column of a
// byte offset, where lines start at zero and after every '\n'.
func positionAt(src []byte, offset int) Position {
	line := 1
	start := 0
	for i := 0; i < offset && i < len(src); i++ {
		if src[i] == '\n' {
			line++
			start = i + 1
		}
	}
	return Position{Offset: offset, Line: line, Column: offset - start + 1}
}

// firstLine returns the file's first physical line without its line
// terminator: the bytes up to the first '\n', with one trailing '\r'
// removed so CRLF files compare cleanly.
func firstLine(src []byte) string {
	i := 0
	for i < len(src) && src[i] != '\n' {
		i++
	}
	line := src[:i]
	if len(line) > 0 && line[len(line)-1] == '\r' {
		line = line[:len(line)-1]
	}
	return string(line)
}

// lineStarts returns the offset of every line start in src: zero, then
// one position after every '\n'. A '\r' is an ordinary byte, matching
// go/token physical line semantics.
func lineStarts(src []byte) []int {
	starts := []int{0}
	for i, b := range src {
		if b == '\n' {
			starts = append(starts, i+1)
		}
	}
	return starts
}

// Position maps a byte offset to its one-based physical line and byte
// column. The offset must satisfy 0 <= offset <= Size; any other offset
// panics.
func (d *Document) Position(offset int) Position {
	if offset < 0 || offset > d.Size {
		panic(fmt.Sprintf("tool: Position(%d) out of range [0, %d]", offset, d.Size))
	}
	line := sort.Search(len(d.lines), func(i int) bool { return d.lines[i] > offset }) - 1
	return Position{
		Offset: offset,
		Line:   line + 1,
		Column: offset - d.lines[line] + 1,
	}
}

// offset returns the physical byte offset of a token position of the
// file's own token.File.
func (d *Document) offset(pos token.Pos) int {
	return d.tok.Offset(pos)
}

// buildDecls walks the AST in source order and produces one GoDecl per
// specification. A spec without its own doc comment inherits its
// declaration group's doc comment, following go/doc's rule. Each
// declaration's doc is then processed for directives and retained
// documentation, and every diagnostic is appended to errs.
func buildDecls(af *ast.File, d *Document, off func(token.Pos) int, errs *[]*Error) []*GoDecl {
	var decls []*GoDecl
	for _, decl := range af.Decls {
		switch decl := decl.(type) {
		case *ast.FuncDecl:
			info := collectFuncInfo(decl)
			gd := &GoDecl{
				Kind: GoFunc,
				Name: decl.Name.Name,
				Pos:  d.Position(off(decl.Name.Pos())),
			}
			if decl.Recv != nil {
				gd.Kind = GoMethod
			}
			if decl.Doc != nil {
				gd.Doc = processDoc(decl.Doc, d, errs, docTarget{kind: gd.Kind, params: info.params, results: info.results})
			}
			decls = append(decls, gd)
		case *ast.GenDecl:
			for _, spec := range decl.Specs {
				decls = append(decls, buildSpec(decl, spec, d, off, errs))
			}
		}
	}
	return decls
}

// funcInfo is the syntactic signature facts needed to resolve
// parameter and return directives.
type funcInfo struct {
	params  []string // every parameter name in order; unnamed parameters are ""
	results int      // number of result type positions
}

// collectFuncInfo collects the parameter names and result arity of one
// function declaration.
func collectFuncInfo(decl *ast.FuncDecl) funcInfo {
	var info funcInfo
	if decl.Type.Params != nil {
		for _, f := range decl.Type.Params.List {
			if len(f.Names) == 0 {
				info.params = append(info.params, "")
				continue
			}
			for _, n := range f.Names {
				info.params = append(info.params, n.Name)
			}
		}
	}
	if decl.Type.Results != nil {
		info.results = len(decl.Type.Results.List)
	}
	return info
}

// buildSpec produces the GoDecl of one GenDecl specification.
func buildSpec(decl *ast.GenDecl, spec ast.Spec, d *Document, off func(token.Pos) int, errs *[]*Error) *GoDecl {
	var doc *ast.CommentGroup
	switch spec := spec.(type) {
	case *ast.ValueSpec:
		if spec.Doc != nil {
			doc = spec.Doc
		} else {
			doc = decl.Doc
		}
	case *ast.TypeSpec:
		if spec.Doc != nil {
			doc = spec.Doc
		} else {
			doc = decl.Doc
		}
	case *ast.ImportSpec:
		if spec.Doc != nil {
			doc = spec.Doc
		} else {
			doc = decl.Doc
		}
	}

	gd := &GoDecl{Kind: specKind(decl.Tok)}
	var names []string
	switch spec := spec.(type) {
	case *ast.ValueSpec:
		for _, n := range spec.Names {
			names = append(names, n.Name)
		}
		if len(names) > 0 {
			gd.Pos = d.Position(off(spec.Names[0].Pos()))
		}
	case *ast.TypeSpec:
		names = append(names, spec.Name.Name)
		gd.Pos = d.Position(off(spec.Name.Pos()))
		gd.Type = GoTypeInfo{
			Alias:   spec.Assign.IsValid(),
			Generic: spec.TypeParams != nil,
			Struct:  isStructType(spec.Type),
		}
		if st, ok := spec.Type.(*ast.StructType); ok {
			gd.Fields = buildFields(st, d, off, errs)
		}
	case *ast.ImportSpec:
		name := ""
		if spec.Name != nil {
			name = spec.Name.Name
		} else {
			name = strings.Trim(spec.Path.Value, `"`)
		}
		gd.Name = name
		gd.Pos = d.Position(off(spec.Pos()))
	}
	if len(names) > 0 {
		gd.Name = names[0]
		gd.Names = names
	}

	if doc != nil {
		gd.Doc = processDoc(doc, d, errs, docTarget{kind: gd.Kind, name: gd.Name, names: names, typeInfo: gd.Type})
	}
	return gd
}

// specKind maps a declaration token to the GoDecl kind of its specs.
func specKind(tok token.Token) GoDeclKind {
	switch tok {
	case token.VAR:
		return GoVar
	case token.TYPE:
		return GoType
	case token.CONST:
		return GoConst
	}
	return GoImport
}

// isStructType reports whether the type expression is an anonymous
// struct.
func isStructType(t ast.Expr) bool {
	_, ok := t.(*ast.StructType)
	return ok
}

// buildFields produces the field records of one struct type, processing
// each field's doc comment for retained documentation. Field docs carry
// no directives; a retained '*/' is still an error.
func buildFields(st *ast.StructType, d *Document, off func(token.Pos) int, errs *[]*Error) []*GoField {
	var fields []*GoField
	for _, f := range st.Fields.List {
		gf := &GoField{Pos: d.Position(off(f.Pos()))}
		if len(f.Names) > 0 {
			gf.Name = f.Names[0].Name
			gf.Pos = d.Position(off(f.Names[0].Pos()))
		}
		if f.Doc != nil {
			gd := processDoc(f.Doc, d, errs, docTarget{field: true})
			gf.Doc = gd.Retained
		}
		fields = append(fields, gf)
	}
	return fields
}
