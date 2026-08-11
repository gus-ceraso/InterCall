package tool

import (
	"fmt"
	"strings"

	"github.com/cerasos/intercall/internal/syntax"
)

// fixedExceptionNames are the three fixed no-payload runtime exception
// wire names in exact byte order (SPEC.md "Fixed Go Runtime
// Exceptions"): internal_exception sorts before invalid_arguments
// before procedure_not_found. They have no generated package symbol and
// cannot be overridden; import shape rejection for a fixed name used by
// another declaration kind or with a payload is a later semantic phase.
var fixedExceptionNames = []string{
	"internal_exception",
	"invalid_arguments",
	"procedure_not_found",
}

// fixedRuntimeExceptions is the name set of the fixed runtime
// exceptions, built from the byte-ordered list above.
var fixedRuntimeExceptions = func() map[string]bool {
	m := make(map[string]bool, len(fixedExceptionNames))
	for _, name := range fixedExceptionNames {
		m[name] = true
	}
	return m
}()

// IsFixedRuntimeException reports whether name is one of the three fixed
// runtime exception wire names.
func IsFixedRuntimeException(name string) bool { return fixedRuntimeExceptions[name] }

// Override is one parsed --go-name SELECTOR=GoIdentifier flag. It changes
// only one generated Go identifier; it never changes wire names, keys,
// values, source metadata, or interface bytes.
type Override struct {
	Selector Selector
	Name     string // the Go identifier
	Text     string // the exact flag text, for diagnostics
}

// String renders the override in its canonical flag form.
func (o Override) String() string { return o.Selector.String() + "=" + o.Name }

// ParseOverride parses one --go-name flag of the form
// SELECTOR=GoIdentifier. The selector must follow the exact selector
// grammar, and the identifier must be a usable non-keyword Go identifier
// that is not the blank identifier. Visibility is kind-dependent and is
// checked when the override resolves against an interface.
func ParseOverride(text string) (Override, error) {
	eq := strings.IndexByte(text, '=')
	if eq < 0 {
		return Override{}, fmt.Errorf("invalid --go-name override %q: expected SELECTOR=GoIdentifier", text)
	}
	sel, err := ParseSelector(text[:eq])
	if err != nil {
		return Override{}, err
	}
	name := text[eq+1:]
	if !IsValidGoIdentifier(name) {
		if IsGoKeyword(name) {
			return Override{}, fmt.Errorf("invalid --go-name override %q: %q is a Go keyword", text, name)
		}
		return Override{}, fmt.Errorf("invalid --go-name override %q: %q is not a usable Go identifier", text, name)
	}
	return Override{Selector: sel, Name: name, Text: text}, nil
}

// ParseOverrides parses a list of --go-name flags in order, stopping at
// the first invalid flag.
func ParseOverrides(texts []string) ([]Override, error) {
	overrides := make([]Override, 0, len(texts))
	for _, text := range texts {
		o, err := ParseOverride(text)
		if err != nil {
			return nil, err
		}
		overrides = append(overrides, o)
	}
	return overrides, nil
}

// Names is the complete projected Go identifier table for one interface.
//
// Decl holds the generated package symbol name of every declaration that
// has one: ordinary types, non-fixed exceptions, and procedures. Field
// holds every field of every generated inline record (record payloads,
// nested records, and list element records), and Param holds every
// procedure parameter. Fixed runtime exceptions generate no symbols and
// have no entries. Wire names are never stored here: the syntax AST
// remains the source of wire order and wire names.
type Names struct {
	Decl  map[syntax.Decl]string
	Field map[*syntax.Field]string
	Param map[*syntax.Param]string
}

// ProjectNames projects every generated Go identifier of one parsed and
// validated file, applying the given import overrides.
//
// Every node with a generated identifier gets its default projection
// (PascalCase for declarations and fields, CamelCase for parameters)
// unless an override names that exact node. Overrides must be unique and
// must resolve exactly once; every resulting name must be a usable
// non-keyword Go identifier, declaration and field names must be
// exported, and parameter names may be exported or unexported. A
// noncanonical wire name without an override is an error, and any
// collision in an actual scope — package declarations, record fields, or
// procedure parameters — is an error rather than a silent escape or
// renumbering.
//
// The file must have been parsed by syntax.Parse and validated by
// syntax.Validate; ProjectNames does not re-run protocol validation.
// Fixed runtime exceptions generate no names and cannot be overridden.
func ProjectNames(f *syntax.File, overrides []Override) (*Names, error) {
	// Duplicate selectors are errors: every selector must resolve exactly
	// once, and two flags naming the same identifier would be ambiguous.
	seen := make(map[string]string) // canonical selector text -> flag text
	for _, o := range overrides {
		if prev, ok := seen[o.Selector.String()]; ok {
			return nil, fmt.Errorf("duplicate --go-name override for %s (given as %q and %q)", o.Selector, prev, o.Text)
		}
		seen[o.Selector.String()] = o.Text
	}

	// Resolve every override exactly once and validate its Go identifier
	// against the target kind.
	byDecl := make(map[syntax.Decl]string)
	byParam := make(map[*syntax.Param]string)
	byField := make(map[*syntax.Field]string)
	for _, o := range overrides {
		t, err := ResolveSelector(f, o.Selector)
		if err != nil {
			return nil, err
		}
		if err := validateOverrideName(t, o.Name); err != nil {
			return nil, err
		}
		switch {
		case t.Field != nil:
			byField[t.Field] = o.Name
		case t.Param != nil:
			byParam[t.Param] = o.Name
		default:
			byDecl[t.Decl] = o.Name
		}
	}

	n := &Names{
		Decl:  make(map[syntax.Decl]string),
		Field: make(map[*syntax.Field]string),
		Param: make(map[*syntax.Param]string),
	}
	for _, d := range f.Decls {
		switch d := d.(type) {
		case *syntax.TypeDecl:
			name, err := nodeName(byDecl[d], false, fmt.Sprintf("type %q", d.Name.Name), d.Name.Name, PascalCase)
			if err != nil {
				return nil, err
			}
			n.Decl[d] = name
			if err := walkType(n, byField, d.Type, fmt.Sprintf("the underlying type of type %q", d.Name.Name)); err != nil {
				return nil, err
			}
		case *syntax.ExceptionDecl:
			if IsFixedRuntimeException(d.Name.Name) {
				continue // no generated package symbol and no generated fields
			}
			name, err := nodeName(byDecl[d], false, fmt.Sprintf("exception %q", d.Name.Name), d.Name.Name, PascalCase)
			if err != nil {
				return nil, err
			}
			n.Decl[d] = name
			if d.Type != nil {
				if err := walkType(n, byField, d.Type, fmt.Sprintf("the payload of exception %q", d.Name.Name)); err != nil {
					return nil, err
				}
			}
		case *syntax.ProcDecl:
			name, err := nodeName(byDecl[d], false, fmt.Sprintf("procedure %q", d.Name.Name), d.Name.Name, PascalCase)
			if err != nil {
				return nil, err
			}
			n.Decl[d] = name
			seenParam := make(map[string]*syntax.Param)
			for _, p := range d.Params {
				where := fmt.Sprintf("parameter %q of procedure %q", p.Name.Name, d.Name.Name)
				pname, err := nodeName(byParam[p], true, where, p.Name.Name, CamelCase)
				if err != nil {
					return nil, err
				}
				n.Param[p] = pname
				if prev, ok := seenParam[pname]; ok {
					return nil, fmt.Errorf("Go parameter name collision: parameters %q and %q of procedure %q both project to %q", prev.Name.Name, p.Name.Name, d.Name.Name, pname)
				}
				seenParam[pname] = p
				if err := walkType(n, byField, p.Type, where); err != nil {
					return nil, err
				}
			}
			if d.Result != nil {
				if err := walkType(n, byField, d.Result, fmt.Sprintf("the return value of procedure %q", d.Name.Name)); err != nil {
					return nil, err
				}
			}
		}
	}

	// Package scope: every generated declaration name must be unique.
	// Iterating the declarations in source order keeps the diagnostic
	// deterministic.
	byName := make(map[string]syntax.Decl, len(n.Decl))
	for _, d := range f.Decls {
		name, ok := n.Decl[d]
		if !ok {
			continue // fixed runtime exceptions have no symbol
		}
		if prev, ok := byName[name]; ok {
			return nil, fmt.Errorf("Go declaration name collision: %s %q and %s %q both project to %q", declKind(prev), declName(prev), declKind(d), declName(d), name)
		}
		byName[name] = d
	}
	return n, nil
}

// validateOverrideName checks the kind-dependent override rules: every
// name must be a usable non-keyword Go identifier, declaration and field
// names must be exported, and parameter names may be exported or
// unexported. A field target stays a field even when it sits under a
// parameter or return value.
func validateOverrideName(t *Target, name string) error {
	if !IsValidGoIdentifier(name) {
		if IsGoKeyword(name) {
			return fmt.Errorf("--go-name override %s: %q is a Go keyword", t.Selector, name)
		}
		return fmt.Errorf("--go-name override %s: %q is not a usable Go identifier", t.Selector, name)
	}
	if t.Field == nil && t.Param == nil && !IsExportedGoIdentifier(name) {
		return fmt.Errorf("--go-name override %s: declaration overrides must be exported Go identifiers, not %q", t.Selector, name)
	}
	if t.Field != nil && !IsExportedGoIdentifier(name) {
		return fmt.Errorf("--go-name override %s: field overrides must be exported Go identifiers, not %q", t.Selector, name)
	}
	return nil
}

// nodeName computes the Go identifier for one node: the override when
// present, otherwise the default projection. The default projection of a
// noncanonical wire name is an error, since such a name requires an
// override. Every result must be a usable non-keyword Go identifier, and
// declaration and field names (isParam == false) must be exported.
func nodeName(override string, isParam bool, what, wire string, c Case) (string, error) {
	name := override
	if name == "" {
		var err error
		name, err = WireToGo(wire, c)
		if err != nil {
			return "", fmt.Errorf("%s: %v", what, err)
		}
	}
	if !IsValidGoIdentifier(name) {
		if IsGoKeyword(name) {
			return "", fmt.Errorf("%s: projected Go name %q is a Go keyword", what, name)
		}
		return "", fmt.Errorf("%s: projected Go name %q is not a usable Go identifier", what, name)
	}
	if !isParam && !IsExportedGoIdentifier(name) {
		return "", fmt.Errorf("%s: projected Go name %q is not exported; declaration and field names must be exported Go identifiers", what, name)
	}
	return name, nil
}

// walkType assigns Go names to every field of every inline record under t
// and checks each record's field scope. where describes the enclosing
// value for diagnostics; nested occurrences extend it with the field
// path.
func walkType(n *Names, byField map[*syntax.Field]string, t syntax.TypeExpr, where string) error {
	switch t := t.(type) {
	case *syntax.ListType:
		return walkType(n, byField, t.Elem, where)
	case *syntax.RecordType:
		seen := make(map[string]*syntax.Field)
		for _, f := range t.Fields {
			name, err := nodeName(byField[f], false, fmt.Sprintf("field %q of %s", f.Name.Name, where), f.Name.Name, PascalCase)
			if err != nil {
				return err
			}
			n.Field[f] = name
			if prev, ok := seen[name]; ok {
				return fmt.Errorf("Go field name collision: fields %q and %q of %s both project to %q", prev.Name.Name, f.Name.Name, where, name)
			}
			seen[name] = f
			if err := walkType(n, byField, f.Type, fmt.Sprintf("field %q of %s", f.Name.Name, where)); err != nil {
				return err
			}
		}
	}
	return nil
}
