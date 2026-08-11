package syntax_test

import (
	"strings"
	"testing"

	"github.com/cerasos/intercall/internal/syntax"
)

// mustValidate parses and validates src, failing the test on any error.
func mustValidate(t *testing.T, src string) *syntax.File {
	t.Helper()
	f := mustParse(t, src)
	if err := syntax.Validate(f); err != nil {
		t.Fatalf("Validate(%q) failed: %v", src, err)
	}
	return f
}

// validateErr parses and validates src, returning the *syntax.Error,
// failing the test when validation succeeds.
func validateErr(t *testing.T, src string) *syntax.Error {
	t.Helper()
	f := mustParse(t, src)
	err := syntax.Validate(f)
	if err == nil {
		t.Fatalf("Validate(%q) succeeded", src)
	}
	e, ok := err.(*syntax.Error)
	if !ok {
		t.Fatalf("Validate(%q) error type %T, want *syntax.Error", src, err)
	}
	return e
}

func TestValidateEmptyFile(t *testing.T) {
	for _, src := range []string{"", "   \n", "/* only */"} {
		f := mustParse(t, src)
		if err := syntax.Validate(f); err != nil {
			t.Errorf("Validate(%q) failed: %v", src, err)
		}
	}
}

func TestValidateEarlierOnlyReferences(t *testing.T) {
	// Valid: references name only earlier type declarations, including
	// transitively and inside lists and records.
	mustValidate(t, "type a uint8; type b a; type c b; type d list b; type e record { f c; g list d; };")
	mustValidate(t, "type u uint8; type v u; procedure p { x v; } list u; exception e record { f v; };")
	// The very same spellings in the other order are invalid: a type may
	// not reference itself or a later declaration.
	validateErr(t, "type t t;")                             // self
	validateErr(t, "type a b; type b uint8;")               // forward
	validateErr(t, "type a b; type b c; type c uint8;")     // transitive forward
	validateErr(t, "type a missing;")                       // unknown
	validateErr(t, "type a record { f b; }; type b uint8;") // forward inside record
	validateErr(t, "type a list b; type b uint8;")          // forward through list
	// Exception and procedure names are not types.
	validateErr(t, "exception e uint8; type a e;")
	validateErr(t, "procedure p {}; type a p;")
	validateErr(t, "type e uint8; exception e;") // global duplicate, not a type alias
}

func TestValidateReferenceResolvesEarlierOnly(t *testing.T) {
	// The diagnostic for a forward reference points at the reference's
	// exact span, not the later declaration.
	src := "type a b; type b uint8;"
	f := mustParse(t, src)
	e := validateErr(t, src)
	if got, want := e.Msg, "unresolved type reference \"b\" in type a"; got != want {
		t.Errorf("msg = %q, want %q", got, want)
	}
	if got := f.Text(e.Span); got != "b" {
		t.Errorf("span text = %q, want %q", got, "b")
	}
	if e.Pos.Offset != 7 {
		t.Errorf("offset = %d, want 7", e.Pos.Offset)
	}
	// A self-reference is rejected before the name could ever be
	// registered: "type t t;" reports the reference.
	e = validateErr(t, "type t t;")
	if got, want := e.Msg, "unresolved type reference \"t\" in type t"; got != want {
		t.Errorf("msg = %q, want %q", got, want)
	}
}

func TestValidateGlobalDuplicates(t *testing.T) {
	// Every ordered pair of declaration kinds shares the global scope.
	tests := []struct {
		name string
		src  string
	}{
		{"type/type", "type t uint8; type t string;"},
		{"type/exception", "type t uint8; exception t;"},
		{"type/procedure", "type t uint8; procedure t {};"},
		{"exception/type", "exception t; type t uint8;"},
		{"exception/exception", "exception t; exception t uint8;"},
		{"exception/procedure", "exception t; procedure t {};"},
		{"procedure/type", "procedure t {}; type t uint8;"},
		{"procedure/exception", "procedure t {}; exception t;"},
		{"procedure/procedure", "procedure t {}; procedure t {};"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			e := validateErr(t, tt.src)
			if !strings.Contains(e.Msg, "duplicate") {
				t.Errorf("msg = %q, want a duplicate diagnostic", e.Msg)
			}
			if !strings.Contains(e.Msg, "first declared at line 1") {
				t.Errorf("msg = %q, want the first declaration's line", e.Msg)
			}
		})
	}
	// The duplicate diagnostic span is the second identifier.
	e := validateErr(t, "type t uint8; type t string;")
	if got := e.Msg; got != "duplicate type name \"t\" (first declared at line 1)" {
		t.Errorf("msg = %q", got)
	}
	if e.Pos.Offset != 19 {
		t.Errorf("offset = %d, want 19", e.Pos.Offset)
	}
}

func TestValidateLocalScopes(t *testing.T) {
	// Valid: local names may repeat across procedures and records, may
	// equal global names, and nested records each have their own scope.
	mustValidate(t, "type x uint8; procedure p { x uint8; } x; procedure q { x string; };")
	mustValidate(t, "type user record { name string; }; procedure get_user { user uint8; };")
	mustValidate(t, "type t record { f uint8; r record { f uint8; }; };")
	mustValidate(t, "type t record { r record { f uint8; }; g record { f uint8; }; };")
	mustValidate(t, "type t record { r record { s record { f uint8; }; f string; }; };")
	mustValidate(t, "procedure p { a uint8; }; procedure q { a uint8; };")
	mustValidate(t, "exception e record { f record { f uint8; }; };")

	// Invalid: duplicates within one scope.
	e := validateErr(t, "procedure p { x uint8; x string; };")
	if got, want := e.Msg, "duplicate parameter \"x\" in procedure p"; got != want {
		t.Errorf("msg = %q, want %q", got, want)
	}
	e = validateErr(t, "type t record { f uint8; f string; };")
	if got, want := e.Msg, "duplicate field \"f\" in type t"; got != want {
		t.Errorf("msg = %q, want %q", got, want)
	}
	e = validateErr(t, "type t record { r record { f uint8; f string; }; };")
	if got, want := e.Msg, "duplicate field \"f\" in field \"r\" of type t"; got != want {
		t.Errorf("msg = %q, want %q", got, want)
	}
	e = validateErr(t, "procedure p { x uint8; x string; };")
	if e.Pos.Offset != 23 {
		t.Errorf("param duplicate offset = %d, want 23", e.Pos.Offset)
	}
	// The local duplicate's span is the second identifier.
	f := mustParse(t, "type t record { f uint8; f string; };")
	e = validateErr(t, "type t record { f uint8; f string; };")
	if got := f.Text(e.Span); got != "f" {
		t.Errorf("field duplicate span text = %q, want %q", got, "f")
	}
}

func TestValidateReservedWords(t *testing.T) {
	// Reserved words are rejected in every identifier position at parse
	// time, so validation only ever sees non-reserved names. Each case
	// fails with the reserved word's exact position.
	tests := []struct {
		src  string
		want string
	}{
		{"type list uint8;", "expected identifier, found 'list'"},
		{"exception procedure;", "expected identifier, found 'procedure'"},
		{"procedure type {};", "expected identifier, found 'type'"},
		{"procedure p { list uint8; };", "expected identifier, found 'list'"},
		{"type t record { record uint8; };", "expected identifier, found 'record'"},
		{"type int8 uint8;", "expected identifier, found 'int8'"},
		{"procedure p { x uint8; } exception;", "expected type, found 'exception'"},
	}
	for _, tt := range tests {
		e := parseErr(t, tt.src)
		if e.Msg != tt.want {
			t.Errorf("src %q: msg = %q, want %q", tt.src, e.Msg, tt.want)
		}
	}
}

func TestValidateUnreachableDeclarations(t *testing.T) {
	// Every declaration is validated even when nothing references it.
	validateErr(t, "type a uint8; type b missing;")
	validateErr(t, "type a uint8; type b uint8; type b string;")
	validateErr(t, "type a uint8; procedure p {}; type b c;")
	// A valid unreferenced declaration is fine.
	mustValidate(t, "type a uint8; type b list a; procedure p {};")
}

func TestValidateKeysZero(t *testing.T) {
	// Machine-found names whose FNV-0 keys are exactly zero, one per kind.
	tests := []struct {
		kind, name string
	}{
		{"procedure", "p3vuYLtJYAr"},
		{"exception", "vi_rZiaP6_h"},
	}
	for _, tt := range tests {
		if got := syntax.Key(tt.kind, tt.name); got != 0 {
			t.Fatalf("Key(%q, %q) = %#016x, want 0", tt.kind, tt.name, got)
		}
		var src string
		if tt.kind == "procedure" {
			src = "procedure " + tt.name + " {};"
		} else {
			src = "exception " + tt.name + ";"
		}
		e := validateErr(t, src)
		want := "key of " + tt.kind + " \"" + tt.name + "\" is 0, which is invalid"
		if e.Msg != want {
			t.Errorf("msg = %q, want %q", e.Msg, want)
		}
	}
}

func TestValidateKeyCollisions(t *testing.T) {
	// Machine-found colliding names, within each kind and across kinds.
	tests := []struct {
		name       string
		firstKind  string
		first      string
		secondKind string
		second     string
		want       string // full expected message for the second declaration
	}{
		{
			name:      "procedure/procedure",
			firstKind: "procedure", first: "gaft2bn2kl5il",
			secondKind: "procedure", second: "an1bk5lqs3ekj",
			want: "key collision: procedure \"an1bk5lqs3ekj\" collides with procedure \"gaft2bn2kl5il\"",
		},
		{
			name:      "exception/exception",
			firstKind: "exception", first: "can20qcvtehol",
			secondKind: "exception", second: "ejmcku0cl4u50",
			want: "key collision: exception \"ejmcku0cl4u50\" collides with exception \"can20qcvtehol\"",
		},
		{
			name:      "procedure/exception",
			firstKind: "procedure", first: "oopqz20a4nrzy",
			secondKind: "exception", second: "k53gkqh4kufnc",
			want: "key collision: exception \"k53gkqh4kufnc\" collides with procedure \"oopqz20a4nrzy\"",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if syntax.Key(tt.firstKind, tt.first) != syntax.Key(tt.secondKind, tt.second) {
				t.Fatalf("fixture names %q/%q do not collide", tt.first, tt.second)
			}
			var src string
			if tt.firstKind == "procedure" {
				src = "procedure " + tt.first + " {};"
			} else {
				src = "exception " + tt.first + ";"
			}
			if tt.secondKind == "procedure" {
				src += " procedure " + tt.second + " {};"
			} else {
				src += " exception " + tt.second + ";"
			}
			e := validateErr(t, src)
			if e.Msg != tt.want {
				t.Errorf("msg = %q, want %q", e.Msg, tt.want)
			}
			if !strings.Contains(e.Msg, "collides") {
				t.Errorf("msg = %q, want a collision diagnostic", e.Msg)
			}
		})
	}
}

func TestValidateKeyOrder(t *testing.T) {
	// The first colliding declaration keeps its key; the second reports.
	// Reversing the declaration order swaps the diagnostic.
	e := validateErr(t, "procedure gaft2bn2kl5il {}; procedure an1bk5lqs3ekj {};")
	if !strings.Contains(e.Msg, "an1bk5lqs3ekj") || !strings.Contains(e.Msg, "gaft2bn2kl5il") {
		t.Errorf("msg = %q", e.Msg)
	}
	// An earlier zero key is reported before a later collision would be.
	e = validateErr(t, "procedure p3vuYLtJYAr {}; procedure gaft2bn2kl5il {}; procedure an1bk5lqs3ekj {};")
	if !strings.Contains(e.Msg, "is 0") {
		t.Errorf("msg = %q, want the zero-key diagnostic first", e.Msg)
	}
}

func TestValidateDiagnosticFormat(t *testing.T) {
	// The rendering includes the filename, line, and column of the
	// offending reference.
	f := mustParse(t, "type a b;\n\ntype b uint8;")
	f.Name = "iface.intercall"
	err := syntax.Validate(f)
	e, ok := err.(*syntax.Error)
	if !ok {
		t.Fatalf("error type %T, want *syntax.Error", err)
	}
	if got, want := e.Error(), "iface.intercall:1:8: unresolved type reference \"b\" in type a"; got != want {
		t.Errorf("Error() = %q, want %q", got, want)
	}
}

func TestValidateDeclOrderWithinDeclaration(t *testing.T) {
	// Within one declaration the order is: duplicate global name, then the
	// type specifier, then the key.
	e := validateErr(t, "type t uint8; type t record { f missing; };")
	if got, want := e.Msg, "duplicate type name \"t\" (first declared at line 1)"; got != want {
		t.Errorf("msg = %q, want the duplicate diagnostic %q", got, want)
	}
	e = validateErr(t, "procedure p { x missing; };")
	if got, want := e.Msg, "unresolved type reference \"missing\" in parameter \"x\" of procedure p"; got != want {
		t.Errorf("msg = %q, want %q", got, want)
	}
	e = validateErr(t, "exception e missing;")
	if got, want := e.Msg, "unresolved type reference \"missing\" in exception e"; got != want {
		t.Errorf("msg = %q, want %q", got, want)
	}
}

func TestValidateProcedureWhereContexts(t *testing.T) {
	// The where context distinguishes the parameter, result, and nested
	// positions that contain the unresolved reference.
	tests := []struct {
		src  string
		want string
	}{
		{"procedure p { x missing; };", "unresolved type reference \"missing\" in parameter \"x\" of procedure p"},
		{"procedure p { x list missing; };", "unresolved type reference \"missing\" in parameter \"x\" of procedure p"},
		{"procedure p {} missing;", "unresolved type reference \"missing\" in return type of procedure p"},
		{"procedure p { x record { f missing; }; };", "unresolved type reference \"missing\" in field \"f\" of parameter \"x\" of procedure p"},
		{"type t record { f missing; };", "unresolved type reference \"missing\" in field \"f\" of type t"},
		{"exception e record { f missing; };", "unresolved type reference \"missing\" in field \"f\" of exception e"},
	}
	for _, tt := range tests {
		e := validateErr(t, tt.src)
		if e.Msg != tt.want {
			t.Errorf("src %q: msg = %q, want %q", tt.src, e.Msg, tt.want)
		}
	}
}
