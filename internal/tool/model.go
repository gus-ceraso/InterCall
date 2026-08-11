package tool

import (
	"fmt"
	"strings"

	"github.com/cerasos/intercall/internal/syntax"
)

// Model is the small command-specific generation record set for one
// validated interface, following SPEC.md "Interface Processing".
//
// Import and export build these records directly from validated syntax and
// (in later phases) Go objects; there is no second general-purpose AST,
// target-neutral generation framework, descriptor schema, or plugin IR.
// The syntax AST remains the source of wire order, wire names,
// documentation, and source diagnostics. The records add only the
// projected Go name and the codec facts needed to emit binding codecs:
// the emitted Go type expression of every type occurrence and whether the
// occurrence has a zero-byte wire representation.
//
// Wire names are the exact syntax declaration names. Later phases that
// apply source directives, field tags, or --go-name overrides project the
// same names through IC-08's naming layer before building their own
// records; this model carries the default projection with no overrides.
type Model struct {
	Types      []*TypeRec      // type declarations in syntax order
	Exceptions []*ExceptionRec // exception declarations in syntax order
	Procs      []*ProcRec      // procedure declarations in syntax order

	names *Names                      // complete projected Go name table
	types map[string]*syntax.TypeDecl // wire name -> type declaration
}

// TypeRec is the generation record of one type declaration.
type TypeRec struct {
	Decl      *syntax.TypeDecl
	GoName    string // projected PascalCase declaration name
	ZeroWidth bool   // codec fact: the underlying type occupies zero wire bytes
}

// ExceptionRec is the generation record of one exception declaration.
type ExceptionRec struct {
	Decl    *syntax.ExceptionDecl
	GoName  string    // projected PascalCase declaration name
	Key     uint64    // 64-bit FNV-0 exception key
	Payload *TypeFact // nil when the payload is omitted
}

// ProcRec is the generation record of one procedure declaration.
type ProcRec struct {
	Decl   *syntax.ProcDecl
	GoName string // projected PascalCase declaration name
	Key    uint64 // 64-bit FNV-0 procedure key
	Params []*ParamRec
	Result *TypeFact // nil when the return value is omitted
}

// ParamRec is the generation record of one procedure parameter.
type ParamRec struct {
	Decl   *syntax.Param
	GoName string // projected camelCase parameter name
	Type   *TypeFact
}

// TypeFact is the codec fact set of one type occurrence: its wire
// structure, its emitted Go type expression, and its zero-width fact.
//
// GoType uses the inverse wire-to-Go mapping of SPEC.md "Go Import Model":
// primitives keep their names, bytes maps to []byte, list uint8 maps to
// []uint8, a list maps to a slice of its element type, an inline record
// maps to an anonymous struct whose fields carry exact intercall tags, and
// a named reference maps to the referenced declaration's Go name.
type TypeFact struct {
	Type      syntax.TypeExpr
	GoType    string
	ZeroWidth bool
}

// BuildModel builds the generation records of one parsed and validated
// file.
//
// The file must come from syntax.Parse and syntax.Validate; BuildModel
// does not re-run protocol validation. Go names come from the default
// projection of IC-08's ProjectNames with no overrides, and codec facts
// are computed from the validated wire structure. BuildModel reports the
// first naming or fact error, which is deterministic for a given file.
func BuildModel(f *syntax.File) (*Model, error) {
	names, err := ProjectNames(f, nil)
	if err != nil {
		return nil, err
	}
	types := make(map[string]*syntax.TypeDecl)
	for _, d := range f.Decls {
		if td, ok := d.(*syntax.TypeDecl); ok {
			types[td.Name.Name] = td
		}
	}
	m := &Model{names: names, types: types}
	for _, d := range f.Decls {
		switch d := d.(type) {
		case *syntax.TypeDecl:
			m.Types = append(m.Types, &TypeRec{
				Decl:      d,
				GoName:    names.Decl[d],
				ZeroWidth: zeroWidthOf(d.Type, types),
			})
		case *syntax.ExceptionDecl:
			rec := &ExceptionRec{
				Decl:   d,
				GoName: names.Decl[d],
				Key:    syntax.ExceptionKey(d.Name.Name),
			}
			if d.Type != nil {
				rec.Payload = typeFact(d.Type, names, types)
			}
			m.Exceptions = append(m.Exceptions, rec)
		case *syntax.ProcDecl:
			rec := &ProcRec{
				Decl:   d,
				GoName: names.Decl[d],
				Key:    syntax.ProcedureKey(d.Name.Name),
			}
			for _, p := range d.Params {
				rec.Params = append(rec.Params, &ParamRec{
					Decl:   p,
					GoName: names.Param[p],
					Type:   typeFact(p.Type, names, types),
				})
			}
			if d.Result != nil {
				rec.Result = typeFact(d.Result, names, types)
			}
			m.Procs = append(m.Procs, rec)
		}
	}
	return m, nil
}

// typeFact computes the codec facts of one type occurrence.
func typeFact(t syntax.TypeExpr, names *Names, types map[string]*syntax.TypeDecl) *TypeFact {
	return &TypeFact{
		Type:      t,
		GoType:    goTypeOf(t, names, types),
		ZeroWidth: zeroWidthOf(t, types),
	}
}

// goTypeOf renders the emitted Go type expression of one type occurrence,
// following the exact inverse mapping of SPEC.md "Go Import Model": the
// predeclared spelling []byte stands for bytes and []uint8 stands for
// list uint8, so the two remain distinct source forms.
func goTypeOf(t syntax.TypeExpr, names *Names, types map[string]*syntax.TypeDecl) string {
	switch t := t.(type) {
	case *syntax.PrimType:
		if t.Kind == syntax.TokBytes {
			return "[]byte"
		}
		return t.Kind.String()
	case *syntax.NamedType:
		return names.Decl[types[t.Name.Name]]
	case *syntax.ListType:
		return "[]" + goTypeOf(t.Elem, names, types)
	case *syntax.RecordType:
		if len(t.Fields) == 0 {
			return "struct{}"
		}
		var b strings.Builder
		b.WriteString("struct {\n")
		for _, f := range t.Fields {
			fmt.Fprintf(&b, "\t%s %s `intercall:%q`\n", names.Field[f], goTypeOf(f.Type, names, types), f.Name.Name)
		}
		b.WriteString("}")
		return b.String()
	}
	panic("tool: unknown type occurrence")
}

// zeroWidthOf reports whether one type occurrence has a zero-byte wire
// representation: a record whose fields all have zero-byte
// representations, or a named reference to such a record. Lists and
// primitives always carry bytes.
func zeroWidthOf(t syntax.TypeExpr, types map[string]*syntax.TypeDecl) bool {
	switch t := t.(type) {
	case *syntax.PrimType:
		return false
	case *syntax.NamedType:
		return zeroWidthOf(types[t.Name.Name].Type, types)
	case *syntax.ListType:
		return false
	case *syntax.RecordType:
		for _, f := range t.Fields {
			if !zeroWidthOf(f.Type, types) {
				return false
			}
		}
		return true
	}
	panic("tool: unknown type occurrence")
}

// typeKeyOf renders the canonical wire text of one type occurrence, used
// to share one codec pair among structurally identical anonymous types.
// The text uses exact wire names, so records with the same structure but
// different field names stay distinct.
func typeKeyOf(t syntax.TypeExpr) string {
	switch t := t.(type) {
	case *syntax.PrimType:
		return t.Kind.String()
	case *syntax.NamedType:
		return t.Name.Name
	case *syntax.ListType:
		return "list " + typeKeyOf(t.Elem)
	case *syntax.RecordType:
		var b strings.Builder
		b.WriteString("record{")
		for i, f := range t.Fields {
			if i > 0 {
				b.WriteByte(';')
			}
			b.WriteString(f.Name.Name)
			b.WriteByte(' ')
			b.WriteString(typeKeyOf(f.Type))
		}
		b.WriteByte('}')
		return b.String()
	}
	panic("tool: unknown type occurrence")
}

// isAnonymousType reports whether one type occurrence is an inline list or
// record, the only forms that get their own shared codec pair. Primitives
// and named references delegate to fixed or declared pairs.
func isAnonymousType(t syntax.TypeExpr) bool {
	switch t.(type) {
	case *syntax.ListType, *syntax.RecordType:
		return true
	}
	return false
}
