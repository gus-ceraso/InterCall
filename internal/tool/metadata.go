package tool

import (
	"bytes"
	"encoding/base64"
	"fmt"
	"go/ast"
	"go/token"
	"strconv"
	"unicode/utf8"

	"github.com/cerasos/intercall/internal/syntax"
)

// This file implements the consumer side of SPEC.md "Safe import and
// re-export metadata": decoding and validating the one
// `_intercallSemantic` constant of an intercall-generated file, finding
// the semantic declaration of a generated type, projecting the
// generated Go type back to its documentation-free wire structure, and
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
// generated type and its exact wire name — and reverse maps the exact
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
// interface, and match its canonical reformatting byte for byte. The
// returned Semantic resolves type declarations by exact wire name and
// projects generated Go types through the file's machine lines.
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
	s.buildMachineTable()
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

// buildMachineTable records the machine line of every type declaration
// of the generated file: the Go type name and its exact wire name. The
// table is the projection lookup; every type the projection resolves is
// separately validated when the mapper reaches it, so a malformed line
// on an unreached type stays inert.
func (s *Semantic) buildMachineTable() {
	for _, d := range s.GoFile.Decls {
		gd, ok := d.(*ast.GenDecl)
		if !ok || gd.Tok != token.TYPE {
			continue
		}
		for _, sp := range gd.Specs {
			ts := sp.(*ast.TypeSpec)
			group := ts.Doc
			if group == nil {
				group = gd.Doc
			}
			if group == nil {
				continue
			}
			for _, ln := range commentLines(group, s.Doc) {
				dir, err := parseDirectiveLine(ln, s.Doc)
				if err != nil || dir == nil {
					continue
				}
				if dir.Kind == TypeDir && dir.Wire != "" {
					s.table[ts.Name.Name] = dir.Wire
					if s.reverse[dir.Wire] == nil {
						s.reverse[dir.Wire] = ts
					}
					break
				}
			}
		}
	}
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
