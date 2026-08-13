package tool

import (
	"bytes"
	"encoding/base64"
	"fmt"
	"go/ast"
	"go/token"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/cerasos/intercall/go/internal/syntax"
)

// This file implements the consumer side of SPEC.md "Safe import and
// re-export metadata": decoding and validating the one
// `_intercallSemantic` constant of an intercall-generated file,
// validating the complete machine table of the file, finding the
// semantic declaration of a generated type, projecting the generated
// Go type back to its documentation-free wire structure, and
// recovering the declaration and its complete nested documentation
// tree.
//
// The exact generated-file marker is the trust boundary: recovery runs
// only on intercall-generated files, and an unmarked handwritten
// `_intercallSemantic` constant is ordinary Go. In a marked file the
// consumer requires exactly one constant, canonical base64url, valid
// UTF-8, a successfully validated decoded interface, and byte-for-byte
// equality between the decoded bytes and their canonical reformatting.
// Decoded documentation is assigned directly to AST slots and is never
// rescanned as a Go directive.

// semanticConstantName is the exact name of the import binding's one
// unexported machine-metadata constant.
const semanticConstantName = "_intercallSemantic"

// Semantic is the decoded, parsed, validated, and canonically stable
// machine metadata of one intercall-generated file.
//
// AST is the parsed canonical interface body of the constant; its type
// declarations carry every semantic documentation slot. table records
// the file's machine lines — the generated Go type name of every
// validated row and its exact wire name — and reverse maps the exact
// wire name back to its generated type spec.
type Semantic struct {
	GoFile  *ast.File
	Doc     *Document
	AST     *syntax.File
	types   map[string]*syntax.TypeDecl // wire name -> semantic declaration
	table   map[string]string           // generated Go type name -> exact wire name
	reverse map[string]*ast.TypeSpec    // exact wire name -> generated Go type spec
}

// RecoverSemantic decodes and validates the machine metadata of one
// intercall-generated file. The caller has already established the
// trust boundary: doc.IntercallGenerated is true.
//
// The constant must be declared alone as one unexported constant whose
// value is a concatenation of string literals, decode as canonical
// unpadded base64url into valid UTF-8, parse and validate as an
// interface, and match its canonical reformatting byte for byte. On
// first use of a table-backed type the complete machine table of the
// file is validated before any row is consumed: every top-level type
// spec carrying exactly one valid `@intercall type <wire-name>`
// machine line is a row, the rows must be in bijection with the
// decoded semantic type declarations, and every row must project back
// to its semantic declaration's documentation-free wire structure
// (SPEC.md "Safe import and re-export metadata"). The returned
// Semantic resolves type declarations by exact wire name and projects
// generated Go types through the file's machine lines.
func RecoverSemantic(goFile *ast.File, doc *Document) (*Semantic, error) {
	vs, err := findSemanticConstant(goFile, doc)
	if err != nil {
		return nil, err
	}
	if vs == nil {
		return nil, mErr(doc, goFile.Package, "generated file %s has no %s constant; import-binding machine metadata is missing", doc.Name, semanticConstantName)
	}
	if len(vs.Values) != 1 {
		return nil, mErr(doc, vs.Pos(), "the %s constant must have exactly one value", semanticConstantName)
	}
	enc, err := evalStringChain(vs.Values[0], doc)
	if err != nil {
		return nil, err
	}
	raw, err := decodeSemantic(enc, doc, vs)
	if err != nil {
		return nil, err
	}
	f, err := syntax.Parse(doc.Name+" (_intercallSemantic)", raw)
	if err != nil {
		return nil, mErr(doc, vs.Pos(), "decoded %s: %v", semanticConstantName, err)
	}
	// The canonical body's comments carry the documentation slots; the
	// canonical reformatting is the parse-attach-validate-format round
	// trip of the decoded bytes.
	syntax.AttachDocs(f)
	if err := syntax.Validate(f); err != nil {
		return nil, mErr(doc, vs.Pos(), "decoded %s: %v", semanticConstantName, err)
	}
	if formatted := syntax.Format(f); !bytes.Equal(formatted, raw) {
		return nil, mErr(doc, vs.Pos(), "decoded %s is not canonical: the decoded bytes differ from their canonical reformatting", semanticConstantName)
	}
	s := &Semantic{
		GoFile:  goFile,
		Doc:     doc,
		AST:     f,
		types:   make(map[string]*syntax.TypeDecl),
		table:   make(map[string]string),
		reverse: make(map[string]*ast.TypeSpec),
	}
	for _, d := range f.Decls {
		if td, ok := d.(*syntax.TypeDecl); ok {
			s.types[td.Name.Name] = td
		}
	}
	if err := s.validateMachineTable(goFile, doc, vs); err != nil {
		return nil, err
	}
	return s, nil
}

// findSemanticConstant locates the one _intercallSemantic constant of a
// generated file. A spec that declares the name alongside other names,
// or a second declaration of the name, is an error.
func findSemanticConstant(goFile *ast.File, doc *Document) (*ast.ValueSpec, error) {
	var found *ast.ValueSpec
	for _, d := range goFile.Decls {
		gd, ok := d.(*ast.GenDecl)
		if !ok || gd.Tok != token.CONST {
			continue
		}
		for _, s := range gd.Specs {
			vs := s.(*ast.ValueSpec)
			for _, n := range vs.Names {
				if n.Name != semanticConstantName {
					continue
				}
				if len(vs.Names) != 1 {
					return nil, mErr(doc, vs.Pos(), "the %s constant must be declared alone as one unexported constant", semanticConstantName)
				}
				if found != nil {
					return nil, mErr(doc, vs.Pos(), "duplicate %s constant", semanticConstantName)
				}
				found = vs
			}
		}
	}
	return found, nil
}

// evalStringChain evaluates one constant string expression that must be
// a concatenation of string literals, the exact emission form of the
// semantic payload.
func evalStringChain(e ast.Expr, doc *Document) (string, error) {
	switch e := e.(type) {
	case *ast.BasicLit:
		if e.Kind != token.STRING {
			return "", mErr(doc, e.Pos(), "the %s value must be a concatenation of string literals", semanticConstantName)
		}
		s, err := strconv.Unquote(e.Value)
		if err != nil {
			return "", mErr(doc, e.Pos(), "malformed string literal in the %s value: %v", semanticConstantName, err)
		}
		return s, nil
	case *ast.BinaryExpr:
		if e.Op != token.ADD {
			return "", mErr(doc, e.Pos(), "the %s value must be a concatenation of string literals", semanticConstantName)
		}
		a, err := evalStringChain(e.X, doc)
		if err != nil {
			return "", err
		}
		b, err := evalStringChain(e.Y, doc)
		if err != nil {
			return "", err
		}
		return a + b, nil
	}
	return "", mErr(doc, e.Pos(), "the %s value must be a concatenation of string literals", semanticConstantName)
}

// decodeSemantic applies the encoding checks of the semantic payload:
// canonical unpadded base64url and valid UTF-8.
func decodeSemantic(enc string, doc *Document, vs *ast.ValueSpec) ([]byte, error) {
	raw, err := base64.RawURLEncoding.DecodeString(enc)
	if err != nil {
		return nil, mErr(doc, vs.Pos(), "the %s value is not valid base64url: %v", semanticConstantName, err)
	}
	if base64.RawURLEncoding.EncodeToString(raw) != enc {
		return nil, mErr(doc, vs.Pos(), "the %s value is not canonical base64url", semanticConstantName)
	}
	if !utf8.Valid(raw) {
		return nil, mErr(doc, vs.Pos(), "the decoded %s value is not valid UTF-8", semanticConstantName)
	}
	return raw, nil
}

// mErr builds one metadata diagnostic at a physical position of the
// generated file.
func mErr(doc *Document, pos token.Pos, format string, args ...any) *Error {
	return &Error{Filename: doc.Name, Pos: doc.Position(doc.offset(pos)), Msg: fmt.Sprintf(format, args...)}
}

// machineRow is one validated table row of a marked generated file: a
// top-level type spec carrying exactly one valid type machine line.
// line is the exact physical position of the machine line's '@'.
type machineRow struct {
	spec *ast.TypeSpec
	wire string
	line token.Pos
}

// validateMachineTable validates the complete machine table of one
// marked generated file on first use of any table-backed type: a row
// is a top-level type spec carrying exactly one valid
// `@intercall type <wire-name>` machine line, and the rows must be in
// bijection with the decoded semantic type declarations, while
// generated helper and exception types without a machine line are
// permitted. Every malformed, unknown, misplaced, duplicate, missing,
// extra, or structurally conflicting row — including an otherwise
// unreached row — is an error at its exact physical position. The
// table is built from the validated rows and is the projection lookup
// of the Semantic.
//
// The scan mirrors the source-directive grammar's doc attachment and
// directive rules exactly, so a row's diagnostics never depend on
// whether the mapper reached it. Prose, blank lines, and non-type
// directives of a marked file's docs are not machine metadata and
// stay inert here; the grammar applies to them when the declaration is
// reached. The scan is purely structural, terminates on adversarial
// tables, and never mutates the canonical metadata.
func (s *Semantic) validateMachineTable(goFile *ast.File, doc *Document, vs *ast.ValueSpec) error {
	var rows []machineRow
	// First pass: scan every top-level declaration's effective doc for
	// machine lines in source order; the first malformed, unknown,
	// misplaced, or duplicate machine line aborts the scan. The doc
	// attachment follows buildSpec and checkGroupDoc: a spec's own doc
	// wins, the group doc belongs to the first spec without its own
	// doc, and a multi-spec group doc that no spec inherits is still
	// subject to the grammar.
	for _, d := range goFile.Decls {
		switch d := d.(type) {
		case *ast.FuncDecl:
			if d.Doc != nil {
				if err := s.scanMachineDoc(d.Doc, doc, nil, false, &rows); err != nil {
					return err
				}
			}
		case *ast.GenDecl:
			firstDocless := -1
			for i, sp := range d.Specs {
				if ownDoc(sp) == nil {
					firstDocless = i
					break
				}
			}
			if len(d.Specs) > 1 && firstDocless == -1 && d.Doc != nil {
				// A group doc no spec inherits: a machine line in it
				// applies to no single declared object. For a type group
				// the doc is still a type doc, so the line is
				// contradictory rather than misplaced.
				var groupSpec *ast.TypeSpec
				if ts, ok := d.Specs[0].(*ast.TypeSpec); ok {
					groupSpec = ts
				}
				if err := s.scanMachineDoc(d.Doc, doc, groupSpec, true, &rows); err != nil {
					return err
				}
			}
			for i, sp := range d.Specs {
				eff := ownDoc(sp)
				grouped := false
				if eff == nil && i == firstDocless {
					eff = d.Doc
					grouped = true
				}
				if eff == nil {
					continue
				}
				ts, isType := sp.(*ast.TypeSpec)
				if !isType {
					ts = nil
				}
				if err := s.scanMachineDoc(eff, doc, ts, grouped && len(d.Specs) > 1, &rows); err != nil {
					return err
				}
			}
		}
	}
	// Second pass: record the rows as the projection lookup and check
	// them in source order: duplicate wire names, rows without a
	// semantic declaration, row shape, and the projection against the
	// declaration's documentation-free wire structure.
	for _, r := range rows {
		s.table[r.spec.Name.Name] = r.wire
		if s.reverse[r.wire] == nil {
			s.reverse[r.wire] = r.spec
		}
	}
	seen := make(map[string]*ast.TypeSpec) // wire name -> first row
	for _, r := range rows {
		if prev := seen[r.wire]; prev != nil {
			return mErr(doc, r.line, "generated types %q and %q both carry machine line wire name %q", prev.Name.Name, r.spec.Name.Name, r.wire)
		}
		seen[r.wire] = r.spec
		if s.types[r.wire] == nil {
			return mErr(doc, r.line, "generated type %q: machine line names %q, but the semantic metadata has no type declaration named %q", r.spec.Name.Name, r.wire, r.wire)
		}
		ti := typeInfoOf(r.spec)
		if ti.Alias {
			return mErr(doc, r.line, "contradictory @intercall type directive: a type alias is not an ordinary defined type")
		}
		if ti.Generic {
			return mErr(doc, r.line, "contradictory @intercall type directive: a generic type is not an ordinary defined type")
		}
		projected, err := s.ProjectType(r.spec.Type, r.spec.Name.Name)
		if err != nil {
			return mErr(doc, r.spec.Name.Pos(), "generated type %q: %v", r.spec.Name.Name, err)
		}
		if !sameType(s.types[r.wire].Type, projected) {
			return mErr(doc, r.spec.Name.Pos(), "generated type %q projects to a wire structure that conflicts with its semantic declaration %q", r.spec.Name.Name, r.wire)
		}
	}
	// Third pass: every decoded semantic type declaration must have a
	// row; the constant is the only physical anchor of a missing row.
	for _, d := range s.AST.Decls {
		if td, ok := d.(*syntax.TypeDecl); ok && s.reverse[td.Name.Name] == nil {
			return mErr(doc, vs.Pos(), "semantic type declaration %q has no machine line in the generated file", td.Name.Name)
		}
	}
	return nil
}

// scanMachineDoc parses every machine line of one effective doc
// comment. spec is the type spec the doc belongs to — including the
// first spec of a multi-spec type group whose doc no spec inherits —
// or nil for a non-type declaration; multiGroup reports a declaration
// group with more than one specification, whose machine line would
// apply to more than one declared object. Every valid type machine
// line is appended to rows, and the first malformed, unknown,
// misplaced, or duplicate machine line fails at its exact physical
// position. Prose, blank lines, and non-type directives are not
// machine metadata and stay inert.
func (s *Semantic) scanMachineDoc(group *ast.CommentGroup, doc *Document, spec *ast.TypeSpec, multiGroup bool, rows *[]machineRow) error {
	if group == nil {
		return nil
	}
	var line token.Pos
	for _, ln := range commentLines(group, doc) {
		dir, err := parseDirectiveLine(ln, doc)
		if err != nil {
			return err // malformed or unknown machine line
		}
		if dir == nil || dir.Kind != TypeDir {
			continue // prose or a non-type directive, not a machine line
		}
		switch {
		case spec == nil:
			return machineLineError(doc, ln, "misplaced @intercall type directive: it applies only to a type declaration")
		case multiGroup:
			return machineLineError(doc, ln, "contradictory @intercall type directive: a declaration group must contain exactly one specification")
		case dir.Wire == "":
			return machineLineError(doc, ln, "malformed machine line: expected the exact wire name")
		}
		if line.IsValid() {
			return machineLineError(doc, ln, "duplicate @intercall type directive")
		}
		at := ln.offset + len(ln.text) - len(strings.TrimLeft(ln.text, " \t"))
		line = doc.tok.Pos(at)
		*rows = append(*rows, machineRow{spec: spec, wire: dir.Wire, line: line})
	}
	return nil
}

// machineLineError builds one machine-line diagnostic at the exact
// physical position of the line's first non-space byte, the '@'.
func machineLineError(doc *Document, ln docLine, format string, args ...any) *Error {
	at := ln.offset + len(ln.text) - len(strings.TrimLeft(ln.text, " \t"))
	return &Error{Filename: doc.Name, Pos: doc.Position(at), Msg: fmt.Sprintf(format, args...)}
}

// TypeDecl returns the semantic type declaration of one exact wire
// name. Post-validation the names are unique, so a missing declaration
// is the only failure.
func (s *Semantic) TypeDecl(wire string) (*syntax.TypeDecl, error) {
	if td := s.types[wire]; td != nil {
		return td, nil
	}
	return nil, fmt.Errorf("generated metadata has no type declaration named %q", wire)
}

// specOf returns the generated Go type spec whose machine line names
// wire, or nil.
func (s *Semantic) specOf(wire string) *ast.TypeSpec {
	return s.reverse[wire]
}

// ProjectType projects the generated Go type expression of one type
// declaration back to its documentation-free wire structure, using the
// file's machine lines for named references: primitives keep their
// names, []byte maps to bytes, []uint8 maps to list uint8, a slice maps
// to a list of its projected element, and a struct maps to a record
// whose fields take their exact wire names from their intercall tags.
// Every other form is structurally conflicting generated metadata.
func (s *Semantic) ProjectType(t ast.Expr, typeName string) (syntax.TypeExpr, error) {
	return s.projectType(t, fmt.Sprintf("type %q", typeName))
}

func (s *Semantic) projectType(t ast.Expr, where string) (syntax.TypeExpr, error) {
	switch t := t.(type) {
	case *ast.Ident:
		if k, ok := primKind(t.Name); ok {
			return &syntax.PrimType{Kind: k}, nil
		}
		if wire, ok := s.table[t.Name]; ok {
			return &syntax.NamedType{Name: &syntax.Ident{Name: wire}}, nil
		}
		return nil, fmt.Errorf("%s: reference to %q has no @intercall type machine line in the generated file", where, t.Name)
	case *ast.ArrayType:
		if t.Len != nil {
			return nil, fmt.Errorf("%s: array types are not part of the generated wire mapping", where)
		}
		if id, ok := t.Elt.(*ast.Ident); ok {
			switch id.Name {
			case "byte":
				return &syntax.PrimType{Kind: syntax.TokBytes}, nil
			case "uint8":
				return &syntax.ListType{Elem: &syntax.PrimType{Kind: syntax.TokUint8}}, nil
			}
		}
		elem, err := s.projectType(t.Elt, where)
		if err != nil {
			return nil, err
		}
		return &syntax.ListType{Elem: elem}, nil
	case *ast.StructType:
		rec := &syntax.RecordType{}
		for _, f := range t.Fields.List {
			if len(f.Names) != 1 {
				return nil, fmt.Errorf("%s: embedded and multi-name fields are not part of the generated wire mapping", where)
			}
			name := f.Names[0].Name
			if !IsExportedGoIdentifier(name) {
				return nil, fmt.Errorf("%s: field %q is not an exported generated field", where, name)
			}
			if f.Tag == nil {
				return nil, fmt.Errorf("%s: field %q has no intercall tag", where, name)
			}
			tag, err := tagValue(f.Tag.Value)
			if err != nil {
				return nil, fmt.Errorf("%s: field %q: malformed intercall tag: %v", where, name, err)
			}
			wire, ok, err := intercallTag(tag)
			if err != nil {
				return nil, fmt.Errorf("%s: field %q: %v", where, name, err)
			}
			if !ok {
				return nil, fmt.Errorf("%s: field %q has no intercall tag", where, name)
			}
			ft, err := s.projectType(f.Type, fmt.Sprintf("field %q of %s", name, where))
			if err != nil {
				return nil, err
			}
			rec.Fields = append(rec.Fields, &syntax.Field{Name: &syntax.Ident{Name: wire}, Type: ft})
		}
		return rec, nil
	case *ast.SelectorExpr:
		return nil, fmt.Errorf("%s: qualified type %s.%s is not part of the generated wire mapping", where, t.X, t.Sel.Name)
	case *ast.IndexExpr, *ast.IndexListExpr:
		return nil, fmt.Errorf("%s: generic instantiations are not part of the generated wire mapping", where)
	case *ast.StarExpr, *ast.MapType, *ast.ChanType, *ast.FuncType, *ast.InterfaceType:
		return nil, fmt.Errorf("%s: unsupported Go type form in generated metadata", where)
	}
	return nil, fmt.Errorf("%s: unsupported Go type form %T in generated metadata", where, t)
}

// primKind maps a predeclared primitive name to its wire token kind.
func primKind(name string) (syntax.TokenKind, bool) {
	switch name {
	case "int8":
		return syntax.TokInt8, true
	case "int16":
		return syntax.TokInt16, true
	case "int32":
		return syntax.TokInt32, true
	case "int64":
		return syntax.TokInt64, true
	case "uint8":
		return syntax.TokUint8, true
	case "uint16":
		return syntax.TokUint16, true
	case "uint32":
		return syntax.TokUint32, true
	case "uint64":
		return syntax.TokUint64, true
	case "float32":
		return syntax.TokFloat32, true
	case "float64":
		return syntax.TokFloat64, true
	case "string":
		return syntax.TokString, true
	}
	return 0, false
}

// sameType compares two wire structures structurally, ignoring every
// documentation slot.
func sameType(a, b syntax.TypeExpr) bool {
	switch a := a.(type) {
	case *syntax.PrimType:
		bb, ok := b.(*syntax.PrimType)
		return ok && a.Kind == bb.Kind
	case *syntax.NamedType:
		bb, ok := b.(*syntax.NamedType)
		return ok && a.Name.Name == bb.Name.Name
	case *syntax.ListType:
		bb, ok := b.(*syntax.ListType)
		return ok && sameType(a.Elem, bb.Elem)
	case *syntax.RecordType:
		bb, ok := b.(*syntax.RecordType)
		if !ok || len(a.Fields) != len(bb.Fields) {
			return false
		}
		for i := range a.Fields {
			if a.Fields[i].Name.Name != bb.Fields[i].Name.Name || !sameType(a.Fields[i].Type, bb.Fields[i].Type) {
				return false
			}
		}
		return true
	}
	return false
}
