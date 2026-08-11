package tool

import (
	"fmt"
	"strings"

	"github.com/cerasos/intercall/internal/syntax"
)

// SelectorKind identifies the declaration kind a selector names.
type SelectorKind int

const (
	// TypeSelector names a type declaration.
	TypeSelector SelectorKind = iota
	// ExceptionSelector names an exception declaration.
	ExceptionSelector
	// ProcedureSelector names a procedure declaration.
	ProcedureSelector
)

// String renders the kind for selectors and diagnostics.
func (k SelectorKind) String() string {
	switch k {
	case TypeSelector:
		return "type"
	case ExceptionSelector:
		return "exception"
	case ProcedureSelector:
		return "procedure"
	}
	return "unknown"
}

// StepKind identifies one field-path step.
type StepKind int

const (
	// ElementStep is a "/element" step that enters a list element.
	ElementStep StepKind = iota
	// FieldStep is a "/field:<name>" step that selects a record field or,
	// when not final, enters that field's type.
	FieldStep
)

// Step is one field-path step of a selector.
type Step struct {
	Kind  StepKind
	Field string // exact wire field name; non-empty only for FieldStep
}

// Selector is one parsed --go-name selector over exact wire names,
// following the exact grammar in SPEC.md "Names and native overrides":
//
//	type:<name>
//	exception:<name>
//	procedure:<name>
//	procedure:<name>/param:<name>
//	type:<name><field-path>
//	exception:<name><field-path>
//	procedure:<name>/param:<name><field-path>
//	procedure:<name>/return<field-path>
//
// A field path is zero or more /element or /field:<name> steps followed
// by /field:<name>. Steps is nil for declaration and parameter roots; a
// return selector always carries a field path because the unnamed return
// value has no identifier of its own.
type Selector struct {
	Kind   SelectorKind
	Name   string // exact wire declaration name
	Param  string // exact wire parameter name; non-empty only for a parameter value
	Return bool   // true only for the return value (Steps is then non-empty)
	Steps  []Step
}

// String renders the selector in its canonical flag form.
func (s Selector) String() string {
	var b strings.Builder
	b.WriteString(s.Kind.String())
	b.WriteByte(':')
	b.WriteString(s.Name)
	if s.Param != "" {
		b.WriteString("/param:")
		b.WriteString(s.Param)
	}
	if s.Return {
		b.WriteString("/return")
	}
	for _, st := range s.Steps {
		if st.Kind == ElementStep {
			b.WriteString("/element")
		} else {
			b.WriteString("/field:")
			b.WriteString(st.Field)
		}
	}
	return b.String()
}

// ParseSelector parses one --go-name selector. Names are exact wire
// identifiers, matched case-sensitively; a field path must end with
// /field:<name>.
func ParseSelector(text string) (Selector, error) {
	var kind SelectorKind
	var rest string
	switch {
	case strings.HasPrefix(text, "type:"):
		kind, rest = TypeSelector, text[len("type:"):]
	case strings.HasPrefix(text, "exception:"):
		kind, rest = ExceptionSelector, text[len("exception:"):]
	case strings.HasPrefix(text, "procedure:"):
		kind, rest = ProcedureSelector, text[len("procedure:"):]
	default:
		return Selector{}, fmt.Errorf("invalid selector %q: must start with type:, exception:, or procedure:", text)
	}
	name, rest := scanWireName(rest)
	if name == "" {
		return Selector{}, fmt.Errorf("invalid selector %q: missing declaration name", text)
	}
	sel := Selector{Kind: kind, Name: name}
	if kind != ProcedureSelector {
		if rest == "" {
			return sel, nil // declaration root
		}
		steps, err := parseFieldPath(text, rest)
		if err != nil {
			return Selector{}, err
		}
		sel.Steps = steps
		return sel, nil
	}
	switch {
	case rest == "":
		return sel, nil // procedure declaration root
	case strings.HasPrefix(rest, "/param:"):
		pname, r := scanWireName(rest[len("/param:"):])
		if pname == "" {
			return Selector{}, fmt.Errorf("invalid selector %q: missing parameter name", text)
		}
		sel.Param = pname
		if r != "" {
			steps, err := parseFieldPath(text, r)
			if err != nil {
				return Selector{}, err
			}
			sel.Steps = steps
		}
		return sel, nil
	case strings.HasPrefix(rest, "/return"):
		steps, err := parseFieldPath(text, rest[len("/return"):])
		if err != nil {
			return Selector{}, err
		}
		sel.Return = true
		sel.Steps = steps
		return sel, nil
	}
	return Selector{}, fmt.Errorf("invalid selector %q: expected /param:<name> or /return after the procedure name", text)
}

// parseFieldPath parses a non-empty field-path remainder: zero or more
// /element or /field:<name> steps, ending in /field:<name>.
func parseFieldPath(text, rest string) ([]Step, error) {
	if rest == "" {
		return nil, fmt.Errorf("invalid selector %q: missing field path (a field path must end with /field:<name>)", text)
	}
	var steps []Step
	for rest != "" {
		if rest[0] != '/' {
			return nil, fmt.Errorf("invalid selector %q: unexpected %q", text, rest)
		}
		rest = rest[1:]
		if rest == "" {
			return nil, fmt.Errorf("invalid selector %q: empty step after '/'", text)
		}
		switch {
		case strings.HasPrefix(rest, "element") && (len(rest) == len("element") || rest[len("element")] == '/'):
			steps = append(steps, Step{Kind: ElementStep})
			rest = rest[len("element"):]
		case strings.HasPrefix(rest, "field:"):
			name, r := scanWireName(rest[len("field:"):])
			if name == "" {
				return nil, fmt.Errorf("invalid selector %q: missing field name after /field:", text)
			}
			steps = append(steps, Step{Kind: FieldStep, Field: name})
			rest = r
		default:
			return nil, fmt.Errorf("invalid selector %q: expected /element or /field:<name>", text)
		}
	}
	if steps[len(steps)-1].Kind != FieldStep {
		return nil, fmt.Errorf("invalid selector %q: field path must end with /field:<name>", text)
	}
	return steps, nil
}

// scanWireName consumes the longest wire-identifier prefix of s and
// returns it with the remainder.
func scanWireName(s string) (name, rest string) {
	if s == "" || !(isASCIILetter(s[0]) || s[0] == '_') {
		return "", s
	}
	i := 1
	for i < len(s) && isWireChar(s[i]) {
		i++
	}
	return s[:i], s[i:]
}

// Target is the exact generated identifier one selector renames. A target
// is exactly one of a declaration root, a parameter, or a field of an
// inline record.
type Target struct {
	Selector Selector
	Decl     syntax.Decl        // the root declaration
	Param    *syntax.Param      // non-nil only for a parameter target
	Field    *syntax.Field      // non-nil only for a field-path target
	Record   *syntax.RecordType // the field's enclosing record; nil otherwise
}

// ResolveSelector resolves sel against one parsed and validated file.
//
// The file must come from syntax.Parse and syntax.Validate, so
// declaration names are unique, parameter and field names are unique
// within their scopes, and every name is a valid wire identifier.
// Resolution is exact wire-name matching against the source AST: a
// selector resolves to at most one target, and every failure reports why
// it does not resolve exactly once. A named reference is never traversed;
// a field path through one is an error, and the referenced declaration
// needs its own root selector. Fixed runtime exceptions have no generated
// package symbol and cannot be overridden.
func ResolveSelector(f *syntax.File, sel Selector) (*Target, error) {
	switch sel.Kind {
	case TypeSelector:
		return resolveType(f, sel)
	case ExceptionSelector:
		return resolveException(f, sel)
	case ProcedureSelector:
		return resolveProcedure(f, sel)
	}
	return nil, fmt.Errorf("selector %s: unknown selector kind", sel)
}

func resolveType(f *syntax.File, sel Selector) (*Target, error) {
	decl := findDecl(f, sel.Name)
	switch d := decl.(type) {
	case nil:
		return nil, fmt.Errorf("selector %s: no type declaration named %q", sel, sel.Name)
	case *syntax.TypeDecl:
		if len(sel.Steps) > 0 {
			field, rec, err := walkFieldPath(sel, fmt.Sprintf("the underlying type of type %q", sel.Name), d.Type, sel.Steps)
			if err != nil {
				return nil, err
			}
			return &Target{Selector: sel, Decl: d, Field: field, Record: rec}, nil
		}
		return &Target{Selector: sel, Decl: d}, nil
	default:
		return nil, fmt.Errorf("selector %s: %q is a %s, not a type", sel, sel.Name, declKind(decl))
	}
}

func resolveException(f *syntax.File, sel Selector) (*Target, error) {
	decl := findDecl(f, sel.Name)
	switch d := decl.(type) {
	case nil:
		return nil, fmt.Errorf("selector %s: no exception declaration named %q", sel, sel.Name)
	case *syntax.ExceptionDecl:
		if IsFixedRuntimeException(d.Name.Name) {
			return nil, fmt.Errorf("selector %s: fixed runtime exception %q has no generated package symbol and cannot be overridden", sel, sel.Name)
		}
		if len(sel.Steps) > 0 {
			if d.Type == nil {
				return nil, fmt.Errorf("selector %s: exception %q has no payload", sel, sel.Name)
			}
			field, rec, err := walkFieldPath(sel, fmt.Sprintf("the payload of exception %q", sel.Name), d.Type, sel.Steps)
			if err != nil {
				return nil, err
			}
			return &Target{Selector: sel, Decl: d, Field: field, Record: rec}, nil
		}
		return &Target{Selector: sel, Decl: d}, nil
	default:
		return nil, fmt.Errorf("selector %s: %q is a %s, not an exception", sel, sel.Name, declKind(decl))
	}
}

func resolveProcedure(f *syntax.File, sel Selector) (*Target, error) {
	decl := findDecl(f, sel.Name)
	d, ok := decl.(*syntax.ProcDecl)
	if decl == nil {
		return nil, fmt.Errorf("selector %s: no procedure declaration named %q", sel, sel.Name)
	}
	if !ok {
		return nil, fmt.Errorf("selector %s: %q is a %s, not a procedure", sel, sel.Name, declKind(decl))
	}
	if sel.Param != "" {
		for _, p := range d.Params {
			if p.Name.Name == sel.Param {
				if len(sel.Steps) > 0 {
					field, rec, err := walkFieldPath(sel, fmt.Sprintf("parameter %q of procedure %q", sel.Param, sel.Name), p.Type, sel.Steps)
					if err != nil {
						return nil, err
					}
					return &Target{Selector: sel, Decl: d, Param: p, Field: field, Record: rec}, nil
				}
				return &Target{Selector: sel, Decl: d, Param: p}, nil
			}
		}
		return nil, fmt.Errorf("selector %s: procedure %q has no parameter named %q", sel, sel.Name, sel.Param)
	}
	if sel.Return {
		if d.Result == nil {
			return nil, fmt.Errorf("selector %s: procedure %q has no return value", sel, sel.Name)
		}
		field, rec, err := walkFieldPath(sel, fmt.Sprintf("the return value of procedure %q", sel.Name), d.Result, sel.Steps)
		if err != nil {
			return nil, err
		}
		return &Target{Selector: sel, Decl: d, Field: field, Record: rec}, nil
	}
	if len(sel.Steps) > 0 {
		return nil, fmt.Errorf("selector %s: a procedure root takes no field path", sel)
	}
	return &Target{Selector: sel, Decl: d}, nil
}

// walkFieldPath applies a non-empty field path over one type occurrence.
// where describes the path root for diagnostics. The final step must be a
// field step, and every intermediate step must enter a traversable type:
// /element requires a list and /field:<name> requires an inline record
// containing that exact field. A named reference is never traversed.
func walkFieldPath(sel Selector, where string, t syntax.TypeExpr, steps []Step) (*syntax.Field, *syntax.RecordType, error) {
	cur := t
	for i, st := range steps {
		if named, ok := cur.(*syntax.NamedType); ok {
			return nil, nil, fmt.Errorf("selector %s: %s: named reference %q is not traversed; use its own type root", sel, where, named.Name.Name)
		}
		final := i == len(steps)-1
		switch st.Kind {
		case ElementStep:
			lt, ok := cur.(*syntax.ListType)
			if !ok {
				return nil, nil, fmt.Errorf("selector %s: /element requires a list, but %s is %s", sel, where, typeKind(cur))
			}
			cur = lt.Elem
		case FieldStep:
			rt, ok := cur.(*syntax.RecordType)
			if !ok {
				return nil, nil, fmt.Errorf("selector %s: /field:%s requires an inline record, but %s is %s", sel, st.Field, where, typeKind(cur))
			}
			var f *syntax.Field
			for _, field := range rt.Fields {
				if field.Name.Name == st.Field {
					f = field
					break
				}
			}
			if f == nil {
				return nil, nil, fmt.Errorf("selector %s: no field %q in %s", sel, st.Field, where)
			}
			if final {
				return f, rt, nil
			}
			cur = f.Type
		}
	}
	return nil, nil, fmt.Errorf("selector %s: internal error: field path has no final field step", sel)
}

// typeKind renders one type occurrence for diagnostics.
func typeKind(t syntax.TypeExpr) string {
	switch t := t.(type) {
	case *syntax.PrimType:
		return "primitive " + t.Kind.String()
	case *syntax.NamedType:
		return "named reference " + t.Name.Name
	case *syntax.ListType:
		return "a list"
	case *syntax.RecordType:
		return "a record"
	}
	return "an unknown type"
}

// findDecl returns the first declaration named name in source order, or
// nil when no declaration has that name.
func findDecl(f *syntax.File, name string) syntax.Decl {
	for _, d := range f.Decls {
		if declName(d) == name {
			return d
		}
	}
	return nil
}

func declName(d syntax.Decl) string {
	switch d := d.(type) {
	case *syntax.TypeDecl:
		return d.Name.Name
	case *syntax.ExceptionDecl:
		return d.Name.Name
	case *syntax.ProcDecl:
		return d.Name.Name
	}
	return ""
}

func declKind(d syntax.Decl) string {
	switch d.(type) {
	case *syntax.TypeDecl:
		return "type"
	case *syntax.ExceptionDecl:
		return "exception"
	case *syntax.ProcDecl:
		return "procedure"
	}
	return "declaration"
}
