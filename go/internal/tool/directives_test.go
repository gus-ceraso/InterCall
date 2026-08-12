package tool

import (
	"os"
	"strings"
	"testing"
)

// goFixture parses one handwritten Go fixture from testdata.
func goFixture(t *testing.T, name string) *Document {
	t.Helper()
	src, err := os.ReadFile("testdata/" + name)
	if err != nil {
		t.Fatalf("reading fixture: %v", err)
	}
	doc, err := ParseGoSource("testdata/"+name, src)
	if err != nil {
		t.Fatalf("ParseGoSource(%s): %v", name, err)
	}
	return doc
}

// goFixtureErr parses one handwritten Go fixture from testdata and fails
// the test unless ParseGoSource reports the expected error.
func goFixtureErr(t *testing.T, name, want string) {
	t.Helper()
	src, err := os.ReadFile("testdata/" + name)
	if err != nil {
		t.Fatalf("reading fixture: %v", err)
	}
	_, err = ParseGoSource("testdata/"+name, src)
	if err == nil {
		t.Fatalf("ParseGoSource(%s) succeeded, want error %q", name, want)
	}
	ge, ok := err.(*Error)
	if !ok {
		t.Fatalf("ParseGoSource(%s) error type = %T, want *Error", name, err)
	}
	if got := ge.Error(); got != "testdata/"+name+":"+want {
		t.Errorf("ParseGoSource(%s) error = %q, want %q", name, got, "testdata/"+name+":"+want)
	}
}

// goSrc wraps one declaration source into a complete Go file.
func goSrc(body string) string {
	return "package p\n\n" + body
}

// goErr parses one inline Go source and returns the reported *Error.
func goErr(t *testing.T, src string) *Error {
	t.Helper()
	_, err := ParseGoSource("x.go", []byte(src))
	if err == nil {
		t.Fatal("ParseGoSource succeeded, want an error")
	}
	ge, ok := err.(*Error)
	if !ok {
		t.Fatalf("error type = %T, want *Error", err)
	}
	return ge
}

// goErrMsg parses one inline Go source and returns the error message.
func goErrMsg(t *testing.T, src string) string {
	t.Helper()
	return goErr(t, src).Msg
}

// goDecl parses one inline Go source and returns the named declaration.
func goDecl(t *testing.T, src, name string) *GoDecl {
	t.Helper()
	doc, err := ParseGoSource("x.go", []byte(src))
	if err != nil {
		t.Fatalf("ParseGoSource: %v", err)
	}
	for _, d := range doc.Decls {
		if d.Name == name {
			return d
		}
	}
	t.Fatalf("no declaration named %q", name)
	return nil
}

// dirs returns the directives of one declaration's doc.
func dirs(t *testing.T, d *GoDecl) []Directive {
	t.Helper()
	if d.Doc == nil {
		t.Fatalf("%s has no doc comment", d.Name)
	}
	return d.Doc.Directives
}

// oneDir returns the single directive of one declaration's doc.
func oneDir(t *testing.T, d *GoDecl) Directive {
	t.Helper()
	got := dirs(t, d)
	if len(got) != 1 {
		t.Fatalf("%s has %d directives, want 1", d.Name, len(got))
	}
	return got[0]
}

func TestDirectives(t *testing.T) {
	t.Run("Valid", func(t *testing.T) {
		doc := goFixture(t, "directives_valid.go")
		if doc.Generated || doc.IntercallGenerated {
			t.Errorf("handwritten fixture classified generated: %v %v", doc.Generated, doc.IntercallGenerated)
		}

		find := func(name string) *GoDecl {
			for _, d := range doc.Decls {
				if d.Name == name {
					return d
				}
			}
			t.Fatalf("no declaration %q", name)
			return nil
		}

		// A type with prose and a wire-name directive.
		d := find("UserID")
		if d.Kind != GoType || d.Name != "UserID" || d.Type.Alias || d.Type.Generic || d.Type.Struct {
			t.Errorf("UserID decl = %+v", d)
		}
		if d.Doc.Retained != "A no-parameter wire type." {
			t.Errorf("UserID retained doc = %q", d.Doc.Retained)
		}
		if dir := oneDir(t, d); dir.Kind != TypeDir || dir.Wire != "user_id" {
			t.Errorf("UserID directive = %+v", dir)
		}

		// A directive-only doc: empty retained documentation.
		d = find("Name")
		if d.Doc.Retained != "" {
			t.Errorf("Name retained doc = %q, want empty", d.Doc.Retained)
		}
		if dir := oneDir(t, d); dir.Kind != TypeDir || dir.Wire != "name" {
			t.Errorf("Name directive = %+v", dir)
		}

		// A struct type with field documentation.
		d = find("Point")
		if !d.Type.Struct {
			t.Error("Point is not a struct type")
		}
		if len(d.Fields) != 2 {
			t.Fatalf("Point has %d fields, want 2", len(d.Fields))
		}
		if f := d.Fields[0]; f.Name != "X" || f.Doc != "The horizontal coordinate." {
			t.Errorf("Point.X = %+v", f)
		}
		if f := d.Fields[1]; f.Name != "Y" || f.Doc != "The vertical coordinate." {
			t.Errorf("Point.Y = %+v", f)
		}

		// A sentinel with an explicit wire name.
		d = find("SentinelError")
		if d.Kind != GoVar || len(d.Names) != 1 {
			t.Errorf("SentinelError decl = %+v", d)
		}
		if dir := oneDir(t, d); dir.Kind != ExceptionDir || dir.Wire != "sentinel_error" {
			t.Errorf("SentinelError directive = %+v", dir)
		}

		// A sentinel with the optional wire name omitted.
		d = find("PlainSentinel")
		if dir := oneDir(t, d); dir.Kind != ExceptionDir || dir.Wire != "" {
			t.Errorf("PlainSentinel directive = %+v", dir)
		}

		// A payload exception struct.
		d = find("ExPayload")
		if !d.Type.Struct || d.Type.Alias || d.Type.Generic {
			t.Errorf("ExPayload type info = %+v", d.Type)
		}
		if dir := oneDir(t, d); dir.Kind != ExceptionDir || dir.Wire != "ex_payload" {
			t.Errorf("ExPayload directive = %+v", dir)
		}

		// A procedure with all parameter and return directives.
		d = find("FindUser")
		if d.Kind != GoFunc {
			t.Errorf("FindUser kind = %v", d.Kind)
		}
		if d.Doc.Retained != "FindUser finds a user by name." {
			t.Errorf("FindUser retained doc = %q", d.Doc.Retained)
		}
		got := dirs(t, d)
		want := []Directive{
			{Kind: ProcedureDir, Wire: "find_user"},
			{Kind: ParamDir, GoName: "userID", Wire: "user_id"},
			{Kind: ParamDocDir, GoName: "userID", Text: "the user to find"},
			{Kind: ReturnDocDir, Text: "the matching user"},
		}
		if len(got) != len(want) {
			t.Fatalf("FindUser has %d directives, want %d", len(got), len(want))
		}
		for i := range want {
			if got[i].Kind != want[i].Kind || got[i].GoName != want[i].GoName ||
				got[i].Wire != want[i].Wire || got[i].Text != want[i].Text {
				t.Errorf("FindUser directive %d = %+v, want %+v", i, got[i], want[i])
			}
		}

		// A procedure with no wire name.
		d = find("Ping")
		if dir := oneDir(t, d); dir.Kind != ProcedureDir || dir.Wire != "ping" {
			t.Errorf("Ping directive = %+v", dir)
		}

		// A block-comment doc: the directive line is recognized and the
		// remaining prose is dedented by the normalization.
		d = find("LookupUser")
		if d.Doc.Retained != "LookupUser finds a user by id." {
			t.Errorf("LookupUser retained doc = %q", d.Doc.Retained)
		}
		if dir := oneDir(t, d); dir.Kind != ProcedureDir || dir.Wire != "lookup_user" {
			t.Errorf("LookupUser directive = %+v", dir)
		}

		// Physical positions of directives.
		if dir := oneDir(t, find("UserID")); dir.Pos.Line != 8 || dir.Pos.Column != 4 {
			t.Errorf("UserID directive at %v, want 8:4", dir.Pos)
		}
		if dir := dirs(t, find("FindUser"))[0]; dir.Pos.Line != 37 || dir.Pos.Column != 4 {
			t.Errorf("FindUser procedure directive at %v, want 37:4", dir.Pos)
		}
	})

	t.Run("Bare", func(t *testing.T) {
		goFixtureErr(t, "directives_bare.go", "6:4: bare @intercall directive")
	})

	t.Run("Unknown", func(t *testing.T) {
		goFixtureErr(t, "directives_unknown.go", "6:4: unknown @intercall directive '@intercall frobnicate'")
	})

	t.Run("Malformed", func(t *testing.T) {
		goFixtureErr(t, "directives_malformed.go", "6:4: malformed @intercall procedure directive: expected at most one wire name")
	})

	t.Run("Misplaced", func(t *testing.T) {
		goFixtureErr(t, "directives_misplaced.go", "6:4: misplaced @intercall type directive: it applies only to a type declaration")
	})

	t.Run("Duplicate", func(t *testing.T) {
		goFixtureErr(t, "directives_duplicate.go", "7:4: duplicate @intercall procedure directive")
	})

	t.Run("Contradictory", func(t *testing.T) {
		goFixtureErr(t, "directives_contradictory.go", "4:4: contradictory @intercall exception directive: it applies only to a named struct type")
	})

	t.Run("Sentinel", func(t *testing.T) {
		goFixtureErr(t, "directives_sentinel.go", "4:4: contradictory @intercall exception directive: a sentinel declaration must contain exactly one variable")
	})

	t.Run("Context", func(t *testing.T) {
		goFixtureErr(t, "directives_context.go", "6:4: contradictory @intercall param directive: the context parameter cannot be named")
	})

	t.Run("Unresolved", func(t *testing.T) {
		goFixtureErr(t, "directives_unresolved.go", "6:4: unresolved @intercall param directive: no parameter named 'nope'")
	})

	t.Run("Return", func(t *testing.T) {
		goFixtureErr(t, "directives_return.go", "6:4: contradictory @return directive: the function has no data result")
	})

	t.Run("MalformedVariants", func(t *testing.T) {
		for _, tc := range []struct {
			name, body, want string
		}{
			{"ExtraOperand", "// @intercall exception a b\nvar X error\n",
				"malformed @intercall exception directive: expected at most one wire name"},
			{"BadWire", "// @intercall procedure 9x\nfunc F() error\n",
				"malformed @intercall procedure directive: invalid wire name '9x'"},
			{"ReservedWire", "// @intercall procedure type\nfunc F() error\n",
				"malformed @intercall procedure directive: invalid wire name 'type'"},
			{"ParamMissing", "// @intercall param\nfunc F(a int) error\n",
				"malformed @intercall param directive: expected a Go name and a wire name"},
			{"ParamMissingWire", "// @intercall param a\nfunc F(a int) error\n",
				"malformed @intercall param directive: expected a wire name after the Go name"},
			{"ParamExtra", "// @intercall param a w x\nfunc F(a int) error\n",
				"malformed @intercall param directive: expected a Go name and a wire name"},
			{"ParamBadGoName", "// @intercall param a-b w\nfunc F(a int) error\n",
				"malformed @intercall param directive: invalid Go name 'a-b'"},
			{"ParamBadWire", "// @intercall param a 9x\nfunc F(a int) error\n",
				"malformed @intercall param directive: invalid wire name '9x'"},
			{"ParamDocMissing", "// @param\nfunc F(a int) error\n",
				"malformed @param directive: expected a Go name and documentation text"},
			{"ParamDocNoText", "// @param a\nfunc F(a int) error\n",
				"malformed @param directive: expected documentation text"},
			{"ParamDocBadGoName", "// @param a.b text\nfunc F(a int) error\n",
				"malformed @param directive: invalid Go name 'a.b'"},
			{"ReturnNoText", "// @return\nfunc F() (int, error)\n",
				"malformed @return directive: expected documentation text"},
		} {
			t.Run(tc.name, func(t *testing.T) {
				if got := goErrMsg(t, goSrc(tc.body)); got != tc.want {
					t.Errorf("error = %q, want %q", got, tc.want)
				}
			})
		}
	})

	t.Run("MisplacedVariants", func(t *testing.T) {
		for _, tc := range []struct {
			name, body, want string
		}{
			{"ProcedureOnVar", "// @intercall procedure\nvar X int\n",
				"misplaced @intercall procedure directive: it applies only to a function declaration"},
			{"ProcedureOnConst", "// @intercall procedure\nconst X = 1\n",
				"misplaced @intercall procedure directive: it applies only to a function declaration"},
			{"ProcedureOnImport", "// @intercall procedure\nimport \"fmt\"\n",
				"misplaced @intercall procedure directive: it applies only to a function declaration"},
			{"ExceptionOnFunc", "// @intercall exception\nfunc F() error\n",
				"misplaced @intercall exception directive: it applies only to a variable or type declaration"},
			{"ExceptionOnConst", "// @intercall exception\nconst X = 1\n",
				"misplaced @intercall exception directive: it applies only to a variable or type declaration"},
			{"TypeOnFunc", "// @intercall type\nfunc F() error\n",
				"misplaced @intercall type directive: it applies only to a type declaration"},
			{"TypeOnVar", "// @intercall type\nvar X int\n",
				"misplaced @intercall type directive: it applies only to a type declaration"},
			{"ParamOnType", "// @intercall param a w\nvar X int\n",
				"misplaced @intercall param directive: it applies only to a function declaration"},
			{"ParamDocOnVar", "// @param a text\nvar X int\n",
				"misplaced @param directive: it applies only to a function declaration"},
			{"ReturnOnType", "// @return text\ntype A int\n",
				"misplaced @return directive: it applies only to a function declaration"},
			{"ParamOnMethod", "// @intercall param a w\nfunc (r *R) M(a int) error\n",
				"misplaced @intercall param directive: it applies only to a function declaration"},
		} {
			t.Run(tc.name, func(t *testing.T) {
				if got := goErrMsg(t, goSrc(tc.body)); got != tc.want {
					t.Errorf("error = %q, want %q", got, tc.want)
				}
			})
		}
	})

	t.Run("ContradictoryVariants", func(t *testing.T) {
		for _, tc := range []struct {
			name, body, want string
		}{
			{"ExceptionOnUnexportedVar", "// @intercall exception\nvar x error\n",
				"contradictory @intercall exception directive: it applies only to an exported package variable"},
			{"ExceptionOnUnexportedType", "// @intercall exception\ntype t struct{}\n",
				"contradictory @intercall exception directive: it applies only to an exported named struct type"},
			{"ExceptionOnAliasType", "// @intercall exception\ntype A = B\n",
				"contradictory @intercall exception directive: a type alias is not a named struct type"},
			{"ExceptionOnGenericType", "// @intercall exception\ntype A[T any] struct{}\n",
				"contradictory @intercall exception directive: a generic type is not a named struct type"},
			{"TypeOnAlias", "// @intercall type\ntype A = B\n",
				"contradictory @intercall type directive: a type alias is not an ordinary defined type"},
			{"TypeOnGeneric", "// @intercall type\ntype A[T any] struct{}\n",
				"contradictory @intercall type directive: a generic type is not an ordinary defined type"},
			{"ProcedureOnMethod", "// @intercall procedure\nfunc (r *R) M() error\n",
				"contradictory @intercall procedure directive: a method cannot be an eligible function"},
			{"ParamDocContext", "// @param ctx text\nfunc F(ctx context.Context, a int) error\n",
				"contradictory @param directive: the context parameter cannot be documented"},
			{"ReturnOnlyError", "// @return text\nfunc F() error\n",
				"contradictory @return directive: the function has no data result"},
			{"ReturnNone", "// @return text\nfunc F()\n",
				"contradictory @return directive: the function has no data result"},
		} {
			t.Run(tc.name, func(t *testing.T) {
				if tc.want == "" {
					t.Fatal("missing expected error")
				}
				if got := goErrMsg(t, goSrc(tc.body)); got != tc.want {
					t.Errorf("error = %q, want %q", got, tc.want)
				}
			})
		}
	})

	t.Run("DuplicateVariants", func(t *testing.T) {
		for _, tc := range []struct {
			name, body, want string
		}{
			{"TwoTypes", "// @intercall type a\n// @intercall type b\ntype A int\n",
				"duplicate @intercall type directive"},
			{"TwoExceptions", "// @intercall exception a\n// @intercall exception b\nvar E error\n",
				"duplicate @intercall exception directive"},
			{"TwoParamDirs", "// @intercall procedure f\n// @intercall param a w1\n// @intercall param a w2\nfunc F(ctx context.Context, a int) error\n",
				"duplicate @intercall param directive for parameter 'a'"},
			{"TwoParamDocs", "// @intercall procedure f\n// @param a one\n// @param a two\nfunc F(ctx context.Context, a int) error\n",
				"duplicate @param directive for parameter 'a'"},
			{"TwoReturns", "// @intercall procedure f\n// @return one\n// @return two\nfunc F() (int, error)\n",
				"duplicate @return directive"},
		} {
			t.Run(tc.name, func(t *testing.T) {
				if got := goErrMsg(t, goSrc(tc.body)); got != tc.want {
					t.Errorf("error = %q, want %q", got, tc.want)
				}
			})
		}
	})

	t.Run("ParamAndParamDocCoexist", func(t *testing.T) {
		// A wire-name directive and a documentation directive for the
		// same parameter fill different slots and are not duplicates.
		d := goDecl(t, goSrc("// @intercall procedure f\n// @intercall param a wire_a\n// @param a docs for a\nfunc F(ctx context.Context, a int) error\n"), "F")
		got := dirs(t, d)
		if len(got) != 3 {
			t.Fatalf("F has %d directives, want 3", len(got))
		}
		if got[1].Kind != ParamDir || got[1].GoName != "a" || got[1].Wire != "wire_a" {
			t.Errorf("param directive = %+v", got[1])
		}
		if got[2].Kind != ParamDocDir || got[2].GoName != "a" || got[2].Text != "docs for a" {
			t.Errorf("param doc directive = %+v", got[2])
		}
	})

	t.Run("GroupedAndUnnamedParams", func(t *testing.T) {
		// Grouped parameters resolve individually, and an all-unnamed
		// parameter list leaves nothing to resolve.
		d := goDecl(t, goSrc("// @intercall procedure f\n// @intercall param b wire_b\nfunc F(ctx context.Context, a, b int) error\n"), "F")
		got := dirs(t, d)
		if len(got) != 2 || got[1].GoName != "b" || got[1].Wire != "wire_b" {
			t.Errorf("directives = %+v", got)
		}
		if msg := goErrMsg(t, goSrc("// @intercall procedure f\n// @intercall param a wire_a\nfunc F(int, string) error\n")); !strings.Contains(msg, "no parameter named 'a'") {
			t.Errorf("error = %q, want unresolved parameter", msg)
		}
	})

	t.Run("FirstParamIsContext", func(t *testing.T) {
		// The context exclusion is positional: the first parameter is
		// the context parameter whether or not it is named ctx.
		d := goDecl(t, goSrc("// @intercall procedure f\n// @intercall param a wire_a\nfunc F(ctx context.Context, a int) error\n"), "F")
		got := dirs(t, d)
		if len(got) != 2 {
			t.Fatalf("F has %d directives, want 2", len(got))
		}
		if got[1].Kind != ParamDir || got[1].GoName != "a" {
			t.Errorf("param directive = %+v", got[1])
		}
		if msg := goErrMsg(t, goSrc("// @intercall procedure f\n// @intercall param a wire_a\nfunc F(a, b int) error\n")); !strings.Contains(msg, "context parameter") {
			t.Errorf("error = %q, want context exclusion", msg)
		}
	})

	t.Run("GroupDocFallback", func(t *testing.T) {
		// A declaration group's doc comment applies to every spec
		// without its own doc comment, following go/doc.
		src := "package p\n\n// @intercall exception\nvar (X error)\n"
		doc, err := ParseGoSource("x.go", []byte(src))
		if err != nil {
			t.Fatalf("ParseGoSource: %v", err)
		}
		if len(doc.Decls) != 1 {
			t.Fatalf("%d declarations, want 1", len(doc.Decls))
		}
		if dir := oneDir(t, doc.Decls[0]); dir.Kind != ExceptionDir {
			t.Errorf("directive = %+v", dir)
		}
	})

	t.Run("GroupDocMultiVar", func(t *testing.T) {
		// A parenthesized multi-variable spec with a group directive
		// violates the sentinel single-variable rule.
		src := "package p\n\n// @intercall exception\nvar (a, b error)\n"
		_, err := ParseGoSource("x.go", []byte(src))
		if err == nil {
			t.Fatal("ParseGoSource succeeded, want sentinel error")
		}
		if got := err.Error(); got != "x.go:3:4: contradictory @intercall exception directive: a sentinel declaration must contain exactly one variable" {
			t.Errorf("error = %q", got)
		}
	})

	t.Run("UnexportedTypeWithTypeDir", func(t *testing.T) {
		// @intercall type applies to every reachable ordinary defined
		// type; export status is not a type-directive constraint.
		d := goDecl(t, goSrc("// @intercall type wire_x\ntype x int\n"), "x")
		if dir := oneDir(t, d); dir.Kind != TypeDir || dir.Wire != "wire_x" {
			t.Errorf("directive = %+v", dir)
		}
	})

	t.Run("EarliestErrorWins", func(t *testing.T) {
		// Errors across declarations are sorted by physical position;
		// the earliest is returned.
		src := "package p\n\n// @intercall frobnicate\nvar A int\n\n// @intercall procedure a b\nfunc F() error\n"
		if got := goErrMsg(t, src); got != "unknown @intercall directive '@intercall frobnicate'" {
			t.Errorf("error = %q, want the first unknown directive", got)
		}
	})

	t.Run("ParseErrorPhysical", func(t *testing.T) {
		// A Go syntax error reports the physical position of the
		// offending token.
		src := "package p\n\nfunc F( {\n"
		ge := goErr(t, src)
		if ge.Msg == "" {
			t.Error("empty parse error message")
		}
		if ge.Pos.Line != 3 || ge.Pos.Column != 9 {
			t.Errorf("parse error at %v, want 3:9", ge.Pos)
		}
	})
}
