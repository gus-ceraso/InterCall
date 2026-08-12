package syntax

import "fmt"

// Validate checks the protocol semantics of a parsed file.
//
// Validation implements the README.md "Grammar" scope and resolution rules
// and the "Procedure and Exception Keys" rules:
//
//   - Type, exception, and procedure declarations share one global name
//     scope; a duplicate name is an error at the later declaration.
//   - Each procedure parameter list and each record occurrence has its own
//     local name scope; names must be unique within their scope, and a
//     local name may equal a global name.
//   - In a type position, an identifier resolves case-sensitively to an
//     earlier global type declaration. Self, forward, and unknown
//     references are errors, as are references to exception or procedure
//     names. Local names never affect type resolution.
//   - Every declaration is validated, including declarations that no other
//     declaration references.
//   - Procedure and exception keys come from Key. Key zero is invalid, and
//     a collision between any two procedure or exception declarations in
//     the same interface is invalid, including a collision between a
//     procedure key and an exception key.
//
// Reserved words never reach validation: the scanner tokenizes their exact
// lowercase spellings as keyword tokens, so identifier positions reject
// them during parsing with exact positions.
//
// On the first violation, Validate returns a non-nil error that always has
// concrete type *Error, with the exact span of the offending identifier and
// the declaration-order precedence: duplicate global name, then the
// declaration's type specifier, then the declaration key. Validate never
// panics on a file returned by Parse; the file must not be nil.
func Validate(f *File) (err error) {
	v := &validator{
		file:   f,
		global: make(map[string]Position),
		types:  make(map[string]bool),
		keys:   make(map[uint64]keyInfo),
	}
	defer func() {
		if r := recover(); r != nil {
			if _, ok := r.(bailout); ok {
				err = v.err
				return
			}
			panic(r)
		}
	}()
	for _, d := range f.Decls {
		v.decl(d)
	}
	return nil
}

// keyInfo records the first declaration that claimed a key, for collision
// diagnostics.
type keyInfo struct {
	kind string
	name string
}

// validator walks declarations in source order, enforcing scopes, earlier
// type resolution, and key rules. It stops at the first violation.
type validator struct {
	file   *File
	global map[string]Position // declaration name -> first declaration position
	types  map[string]bool     // earlier type declaration names
	keys   map[uint64]keyInfo  // declared procedure and exception keys
	err    error
}

// errf records the first validation error and stops validation.
func (v *validator) errf(span Span, format string, args ...any) {
	v.err = &Error{
		Filename: v.file.Name,
		Pos:      v.file.Position(span.Start),
		Span:     span,
		Msg:      fmt.Sprintf(format, args...),
	}
	panic(bailout{})
}

// decl validates one declaration: global name, its type specifier, and its
// key, in that order.
func (v *validator) decl(d Decl) {
	switch d := d.(type) {
	case *TypeDecl:
		v.declName(d.Name, "type")
		v.typeSpec(d.Type, "type "+d.Name.Name)
		v.types[d.Name.Name] = true
	case *ExceptionDecl:
		v.declName(d.Name, "exception")
		if d.Type != nil {
			v.typeSpec(d.Type, "exception "+d.Name.Name)
		}
		v.key(d.Name, "exception")
	case *ProcDecl:
		v.declName(d.Name, "procedure")
		where := "procedure " + d.Name.Name
		seen := make(map[string]bool)
		for _, p := range d.Params {
			if seen[p.Name.Name] {
				v.errf(p.Name.span, "duplicate parameter %q in %s", p.Name.Name, where)
			}
			seen[p.Name.Name] = true
			v.typeSpec(p.Type, fmt.Sprintf("parameter %q of %s", p.Name.Name, where))
		}
		if d.Result != nil {
			v.typeSpec(d.Result, "return type of "+where)
		}
		v.key(d.Name, "procedure")
	}
}

// declName checks the global name scope. The diagnostic span is the
// duplicate identifier itself; the message names the first declaration's
// line.
func (v *validator) declName(id *Ident, kind string) {
	if first, ok := v.global[id.Name]; ok {
		v.errf(id.span, "duplicate %s name %q (first declared at line %d)", kind, id.Name, first.Line)
	}
	v.global[id.Name] = v.file.Position(id.span.Start)
}

// vsKind identifies one entry in the validator's explicit work stack.
type vsKind uint8

const (
	vsType   vsKind = iota // visit one type occurrence
	vsRecord               // continue one record's field loop
)

// vsStep is one entry in the validator's explicit work stack.
type vsStep struct {
	kind  vsKind
	t     TypeExpr        // vsType
	where string          // enclosing declaration or field context
	rec   *RecordType     // vsRecord
	seen  map[string]bool // vsRecord: field names seen so far
	next  int             // vsRecord: next field index
}

// typeSpec validates one type occurrence against earlier type declarations,
// descending into list elements and record fields with their own scopes.
// The walk uses an explicit work stack that mirrors the recursive descent
// exactly — a record's field duplicates are checked in field order, and
// each field's type is fully visited before the next field — so call-stack
// use stays independent of type nesting. where describes the enclosing
// declaration or field for diagnostics.
func (v *validator) typeSpec(t TypeExpr, where string) {
	stack := []vsStep{{kind: vsType, t: t, where: where}}
	for len(stack) > 0 {
		s := &stack[len(stack)-1]
		switch s.kind {
		case vsType:
			stack = stack[:len(stack)-1]
			switch t := s.t.(type) {
			case *NamedType:
				if !v.types[t.Name.Name] {
					v.errf(t.Name.span, "unresolved type reference %q in %s", t.Name.Name, s.where)
				}
			case *ListType:
				stack = append(stack, vsStep{kind: vsType, t: t.Elem, where: s.where})
			case *RecordType:
				stack = append(stack, vsStep{kind: vsRecord, rec: t, seen: make(map[string]bool), where: s.where})
			}
		case vsRecord:
			if s.next >= len(s.rec.Fields) {
				stack = stack[:len(stack)-1]
				continue
			}
			f := s.rec.Fields[s.next]
			s.next++
			if s.seen[f.Name.Name] {
				v.errf(f.Name.span, "duplicate field %q in %s", f.Name.Name, s.where)
			}
			s.seen[f.Name.Name] = true
			stack = append(stack, vsStep{kind: vsType, t: f.Type, where: fmt.Sprintf("field %q of %s", f.Name.Name, s.where)})
		}
	}
}

// key rejects key zero and collisions across procedure and exception
// declarations in the same interface.
func (v *validator) key(id *Ident, kind string) {
	k := Key(kind, id.Name)
	if k == 0 {
		v.errf(id.span, "key of %s %q is 0, which is invalid", kind, id.Name)
	}
	if first, ok := v.keys[k]; ok {
		v.errf(id.span, "key collision: %s %q collides with %s %q", kind, id.Name, first.kind, first.name)
	}
	v.keys[k] = keyInfo{kind: kind, name: id.Name}
}
