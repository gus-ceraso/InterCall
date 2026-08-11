package tool

import (
	"math/rand"
	"strings"
	"testing"

	"github.com/cerasos/intercall/internal/syntax"
)

// specInitialisms is the exact fixed initialism list from SPEC.md "Names
// and native overrides"; the table in initialisms.go must match it
// verbatim.
const specInitialisms = "ACL API ASCII CPU CSS DNS EOF GUID HTML HTTP HTTPS ID IP JSON QPS RAM RPC SLA SMTP SQL SSH TCP TLS TTL UDP UI UID URI URL UTF8 UUID VM XML XMPP XSRF XSS"

// parseFixture parses and validates one interface source text.
func parseFixture(t *testing.T, src string) *syntax.File {
	t.Helper()
	f, err := syntax.Parse("fixture.intercall", []byte(src))
	if err != nil {
		t.Fatalf("syntax.Parse: %v", err)
	}
	if err := syntax.Validate(f); err != nil {
		t.Fatalf("syntax.Validate: %v", err)
	}
	return f
}

// project runs ProjectNames over one fixture with the given overrides and
// fails the test on error.
func project(t *testing.T, src string, overrides ...Override) *Names {
	t.Helper()
	n, err := ProjectNames(parseFixture(t, src), overrides)
	if err != nil {
		t.Fatalf("ProjectNames: %v", err)
	}
	return n
}

// projectErr runs ProjectNames over one fixture and fails the test unless
// it errors.
func projectErr(t *testing.T, src string, overrides ...Override) error {
	t.Helper()
	_, err := ProjectNames(parseFixture(t, src), overrides)
	if err == nil {
		t.Fatal("ProjectNames succeeded, want an error")
	}
	return err
}

// projectFile runs ProjectNames over an already parsed file. Tests that
// need to look up AST nodes must parse once and use this helper so the
// node pointers match the returned table keys.
func projectFile(t *testing.T, f *syntax.File, overrides ...Override) *Names {
	t.Helper()
	n, err := ProjectNames(f, overrides)
	if err != nil {
		t.Fatalf("ProjectNames: %v", err)
	}
	return n
}

func mustSelector(t *testing.T, text string) Selector {
	t.Helper()
	sel, err := ParseSelector(text)
	if err != nil {
		t.Fatalf("ParseSelector(%q): %v", text, err)
	}
	return sel
}

func mustOverride(t *testing.T, text string) Override {
	t.Helper()
	o, err := ParseOverride(text)
	if err != nil {
		t.Fatalf("ParseOverride(%q): %v", text, err)
	}
	return o
}

func TestNaming(t *testing.T) {
	t.Run("InitialismTable", func(t *testing.T) {
		if got := strings.Join(initialisms[:], " "); got != specInitialisms {
			t.Fatalf("initialism table differs from SPEC:\ngot  %q\nwant %q", got, specInitialisms)
		}
		if len(initialisms) != 36 {
			t.Fatalf("initialism count = %d, want 36", len(initialisms))
		}
		// Every initialism round-trips through both projections.
		for _, init := range initialisms {
			lower := strings.ToLower(init)
			if got, err := WireToGo(lower, PascalCase); err != nil || got != init {
				t.Fatalf("WireToGo(%q, Pascal) = %q, %v; want %q", lower, got, err, init)
			}
			if got, err := WireToGo(lower, CamelCase); err != nil || got != lower {
				t.Fatalf("WireToGo(%q, camel) = %q, %v; want %q", lower, got, err, lower)
			}
			if got, err := GoToWire(init, PascalCase); err != nil || got != lower {
				t.Fatalf("GoToWire(%q, Pascal) = %q, %v; want %q", init, got, err, lower)
			}
		}
		// Longest-initialism behavior: a longer initialism wins over any
		// shorter prefix of the same run.
		checks := map[string]string{
			"HTTPS": "HTTPS", "HTTP": "HTTP", "HTTPX": "HTTP",
			"UID": "UID", "URI": "URI", "URL": "URL", "UI": "UI",
			"UTF8": "UTF8", "XSS": "XSS", "XSRF": "XSRF", "": "",
		}
		for run, want := range checks {
			if got := longestInitialism(run); got != want {
				t.Errorf("longestInitialism(%q) = %q, want %q", run, got, want)
			}
		}
		// The exact SPEC example: HTTPSClient splits before the final
		// uppercase letter of the run, keeping HTTPS as one initialism.
		if words := splitGoWords("HTTPSClient"); strings.Join(words, "|") != "HTTPS|Client" {
			t.Fatalf("splitGoWords(HTTPSClient) = %v, want [HTTPS Client]", words)
		}
	})

	t.Run("WireToGoExamples", func(t *testing.T) {
		// Every example from SPEC.md "Names and native overrides", plus
		// additional canonical shapes.
		examples := []struct {
			wire, pascal, camel string
		}{
			{"user_id", "UserID", "userID"},
			{"http_url", "HTTPURL", "httpURL"},
			{"https_client", "HTTPSClient", "httpsClient"},
			{"utf8_value", "UTF8Value", "utf8Value"},
			{"api2_client", "Api2Client", "api2Client"},
			{"sha_value", "ShaValue", "shaValue"},
			{"a", "A", "a"},
			{"id", "ID", "id"},
			{"ssh_key", "SSHKey", "sshKey"},
			{"ui_id", "UIID", "uiID"},
			{"url_path", "URLPath", "urlPath"},
			{"xsrf_token", "XSRFToken", "xsrfToken"},
			{"xml_parser", "XMLParser", "xmlParser"},
			{"qps_limit", "QPSLimit", "qpsLimit"},
			{"a1_b2", "A1B2", "a1B2"},
			{"user_i_d", "UserID", "userID"},
		}
		for _, ex := range examples {
			if got, err := WireToGo(ex.wire, PascalCase); err != nil || got != ex.pascal {
				t.Errorf("WireToGo(%q, Pascal) = %q, %v; want %q", ex.wire, got, err, ex.pascal)
			}
			if got, err := WireToGo(ex.wire, CamelCase); err != nil || got != ex.camel {
				t.Errorf("WireToGo(%q, camel) = %q, %v; want %q", ex.wire, got, err, ex.camel)
			}
		}
	})

	t.Run("WireToGoRejectsNoncanonical", func(t *testing.T) {
		// Any valid but noncanonical wire name requires a --go-name
		// override and cannot be projected by default; invalid names are
		// rejected outright.
		for _, wire := range []string{
			"UserID", "userID", "user__id", "_user", "user_", "_", "A",
			"123abc", "a b", "a-b", "a_", "üser", "",
		} {
			if _, err := WireToGo(wire, PascalCase); err == nil {
				t.Errorf("WireToGo(%q, Pascal) succeeded, want error", wire)
			}
			if _, err := WireToGo(wire, CamelCase); err == nil {
				t.Errorf("WireToGo(%q, camel) succeeded, want error", wire)
			}
		}
	})

	t.Run("GoToWireExamples", func(t *testing.T) {
		// The SPEC accept list and additional accepted forms: the exact
		// checked inverse recovers the canonical wire name.
		examples := []struct {
			goName string
			c      Case
			wire   string
		}{
			{"HTTPServer", PascalCase, "http_server"},
			{"HTTPURL", PascalCase, "http_url"},
			{"HTTPSClient", PascalCase, "https_client"},
			{"UserID", PascalCase, "user_id"},
			{"Version42ID", PascalCase, "version42_id"},
			{"UTF8Value", PascalCase, "utf8_value"},
			{"ShaValue", PascalCase, "sha_value"},
			{"SSHKey", PascalCase, "ssh_key"},
			{"HTMLPage", PascalCase, "html_page"},
			{"XSRFToken", PascalCase, "xsrf_token"},
			{"UUID", PascalCase, "uuid"},
			{"A", PascalCase, "a"},
			{"X1", PascalCase, "x1"},
			{"A42", PascalCase, "a42"},
			{"F2B", PascalCase, "f2_b"},
			{"ABc", PascalCase, "a_bc"},
			{"A1B2", PascalCase, "a1_b2"},
			{"A2Bc", PascalCase, "a2_bc"},
			{"userID", CamelCase, "user_id"},
			{"httpURL", CamelCase, "http_url"},
			{"httpsClient", CamelCase, "https_client"},
			{"utf8Value", CamelCase, "utf8_value"},
			{"api2Client", CamelCase, "api2_client"},
			{"shaValue", CamelCase, "sha_value"},
			{"a1B2", CamelCase, "a1_b2"},
			{"v42ID", CamelCase, "v42_id"},
			{"user", CamelCase, "user"},
			{"a", CamelCase, "a"},
			{"x1", CamelCase, "x1"},
		}
		for _, ex := range examples {
			if got, err := GoToWire(ex.goName, ex.c); err != nil || got != ex.wire {
				t.Errorf("GoToWire(%q, %v) = %q, %v; want %q", ex.goName, ex.c, got, err, ex.wire)
			}
		}
	})

	t.Run("GoToWireRejects", func(t *testing.T) {
		// The SPEC reject list plus every rejection class: non-ASCII,
		// underscores, non-initialism uppercase runs, case that does not
		// survive the round trip, keywords, and invalid identifiers.
		rejects := []struct {
			goName string
			c      Case
		}{
			{"SHAValue", PascalCase},
			{"API2Client", PascalCase},
			{"User_ID", PascalCase},
			{"AB", PascalCase},
			{"Http", PascalCase},
			{"Api", PascalCase},
			{"ID2", PascalCase},
			{"IDD", PascalCase},
			{"HTTPSX", PascalCase},
			{"A1BC2", PascalCase},
			{"ID42", PascalCase},
			{"utf8", PascalCase},
			{"userid", PascalCase},
			{"userID", PascalCase}, // Pascal projection must match exactly
			{"UserID", CamelCase},  // camel projection must match exactly
			{"http_url", PascalCase},
			{"sha_Value", PascalCase},
			{"_foo", PascalCase},
			{"foo_", PascalCase},
			{"A__B", PascalCase},
			{"123abc", PascalCase},
			{"a-b", PascalCase},
			{"", PascalCase},
			{"日本語", PascalCase},
			{"if", PascalCase}, // Go keyword
		}
		for _, ex := range rejects {
			if got, err := GoToWire(ex.goName, ex.c); err == nil {
				t.Errorf("GoToWire(%q, %v) = %q, want an error", ex.goName, ex.c, got)
			}
		}
	})

	t.Run("ExceptionNamesNeverStripped", func(t *testing.T) {
		// The default exception wire name converts the complete Go
		// identifier and never strips Err, Error, or another affix.
		examples := map[string]string{
			"ErrNotFound":  "err_not_found",
			"Err":          "err",
			"ErrorInvalid": "error_invalid",
			"ErrorOut":     "error_out",
		}
		for goName, want := range examples {
			if got, err := GoToWire(goName, PascalCase); err != nil || got != want {
				t.Errorf("GoToWire(%q, Pascal) = %q, %v; want %q", goName, got, err, want)
			}
		}
	})

	t.Run("CanonicalWireNames", func(t *testing.T) {
		canonical := map[string]bool{
			"a": true, "a1": true, "a_b": true, "a1_b2": true,
			"user_id": true, "id2": true, "utf8": true, "a_b_c": true,
			"UserID": false, "userID": false, "user__id": false,
			"_user": false, "user_": false, "_": false, "A": false,
			"123abc": false, "a b": false, "a-b": false, "": false,
			"üser": false,
		}
		for name, want := range canonical {
			if got := IsCanonicalWireName(name); got != want {
				t.Errorf("IsCanonicalWireName(%q) = %v, want %v", name, got, want)
			}
		}
		valid := map[string]bool{
			"a": true, "UserID": true, "userID": true, "_user": true,
			"user_": true, "_": true, "A": true,
			"123abc": false, "a b": false, "a-b": false, "": false,
			"üser": false,
		}
		for name, want := range valid {
			if got := IsValidWireName(name); got != want {
				t.Errorf("IsValidWireName(%q) = %v, want %v", name, got, want)
			}
		}
	})

	t.Run("GoIdentifierValidity", func(t *testing.T) {
		valid := []string{"User", "user", "User2", "_user", "main", "URL", "x_y"}
		for _, name := range valid {
			if !IsValidGoIdentifier(name) {
				t.Errorf("IsValidGoIdentifier(%q) = false, want true", name)
			}
		}
		invalid := []string{
			"", "_", "1abc", "a-b", "a b", "üser",
			"break", "case", "chan", "const", "continue", "default",
			"defer", "else", "fallthrough", "for", "func", "go", "goto",
			"if", "import", "interface", "map", "package", "range",
			"return", "select", "struct", "switch", "type", "var",
		}
		for _, name := range invalid {
			if IsValidGoIdentifier(name) {
				t.Errorf("IsValidGoIdentifier(%q) = true, want false", name)
			}
		}
		exported := map[string]bool{
			"User": true, "URL": true, "User2": true,
			"user": false, "_User": false, "_": false, "type": false,
		}
		for name, want := range exported {
			if got := IsExportedGoIdentifier(name); got != want {
				t.Errorf("IsExportedGoIdentifier(%q) = %v, want %v", name, got, want)
			}
		}
	})

	t.Run("PackageNames", func(t *testing.T) {
		valid := []string{"binding", "user_pkg", "UserPkg", "user2", "_x", "x"}
		for _, name := range valid {
			if err := ValidGoPackageName(name); err != nil {
				t.Errorf("ValidGoPackageName(%q) = %v, want nil", name, err)
			}
		}
		invalid := []string{"main", "_", "type", "func", "1x", "a-b", "a b", ""}
		for _, name := range invalid {
			if err := ValidGoPackageName(name); err == nil {
				t.Errorf("ValidGoPackageName(%q) succeeded, want error", name)
			}
		}
	})

	t.Run("RoundTripProperty", func(t *testing.T) {
		rng := rand.New(rand.NewSource(0x5EED))
		letters := "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ"
		alnum := letters + "0123456789"
		for i := 0; i < 10000; i++ {
			n := 1 + rng.Intn(14)
			var b strings.Builder
			b.WriteByte(letters[rng.Intn(len(letters))])
			for j := 1; j < n; j++ {
				b.WriteByte(alnum[rng.Intn(len(alnum))])
			}
			goName := b.String()
			for _, c := range []Case{PascalCase, CamelCase} {
				wire, err := GoToWire(goName, c)
				if err != nil {
					continue // rejected identifiers are table-tested
				}
				if !IsCanonicalWireName(wire) {
					t.Fatalf("GoToWire(%q, %v) = %q is not canonical", goName, c, wire)
				}
				back, err := WireToGo(wire, c)
				if err != nil {
					t.Fatalf("GoToWire(%q, %v) = %q but WireToGo fails: %v", goName, c, wire, err)
				}
				if back != goName {
					t.Fatalf("checked inverse broken: %q -> %q -> %q (case %v)", goName, wire, back, c)
				}
			}
		}
		// Every canonical wire name projects deterministically with the
		// required visibility shape.
		for i := 0; i < 10000; i++ {
			wire := randomCanonicalWire(rng)
			for _, c := range []Case{PascalCase, CamelCase} {
				g1, err := WireToGo(wire, c)
				if err != nil {
					t.Fatalf("WireToGo(%q, %v) failed: %v", wire, c, err)
				}
				g2, err := WireToGo(wire, c)
				if err != nil || g1 != g2 {
					t.Fatalf("WireToGo(%q, %v) is not deterministic", wire, c)
				}
				if c == PascalCase && !IsExportedGoIdentifier(g1) {
					t.Fatalf("WireToGo(%q, Pascal) = %q is not exported", wire, g1)
				}
				if c == CamelCase && (g1 == "" || !isLower(g1[0])) {
					t.Fatalf("WireToGo(%q, camel) = %q does not start lowercase", wire, g1)
				}
			}
		}
	})

	t.Run("ManglePrivate", func(t *testing.T) {
		// Deterministic: the same parts always produce the same name.
		if a, b := ManglePrivate("encode", "user_id"), ManglePrivate("encode", "user_id"); a != b {
			t.Fatalf("ManglePrivate is not deterministic: %q vs %q", a, b)
		}
		// Always a valid, unexported, non-keyword Go identifier with the
		// fixed prefix, never the blank identifier or "_intercallSemantic".
		for _, parts := range [][]string{
			{}, {""}, {"user_id"}, {"encode", "user_id"},
			{"github.com/foo/bar"}, {"a b"}, {"日本語"}, {"123"},
		} {
			name := ManglePrivate(parts...)
			if !strings.HasPrefix(name, manglePrefix) {
				t.Errorf("ManglePrivate(%q) = %q does not start with %q", parts, name, manglePrefix)
			}
			if !IsValidGoIdentifier(name) {
				t.Errorf("ManglePrivate(%q) = %q is not a valid Go identifier", parts, name)
			}
			if IsExportedGoIdentifier(name) {
				t.Errorf("ManglePrivate(%q) = %q is exported", parts, name)
			}
			if IsGoKeyword(name) || name == "_" || name == "_intercallSemantic" {
				t.Errorf("ManglePrivate(%q) = %q is reserved", parts, name)
			}
		}
		// Distinct parts stay distinct, including inputs that sanitize to
		// the same body.
		distinct := [][][]string{
			{{"a", "b"}, {"ab"}},
			{{"a-b"}, {"a_b"}},
			{{"a b"}, {"ab"}},
			{{"encode", "user"}, {"encode", "users"}},
		}
		for _, pair := range distinct {
			if a, b := ManglePrivate(pair[0]...), ManglePrivate(pair[1]...); a == b {
				t.Errorf("ManglePrivate(%q) == ManglePrivate(%q) == %q", pair[0], pair[1], a)
			}
		}
		// The sanitized parts survive inside the mangled name.
		if name := ManglePrivate("user id"); !strings.Contains(name, "user_id") {
			t.Errorf("ManglePrivate(\"user id\") = %q does not contain user_id", name)
		}
	})

	t.Run("DeclarationCollisions", func(t *testing.T) {
		// Two canonical wire names projecting to the same Pascal name
		// collide in the package scope.
		err := projectErr(t, `
type user_id record { x string; };
type user_i_d record { y string; };
`)
		if !strings.Contains(err.Error(), "Go declaration name collision") ||
			!strings.Contains(err.Error(), "UserID") {
			t.Fatalf("declaration collision error = %v", err)
		}
		// A still-colliding override is an error rather than a renumber.
		err = projectErr(t, `
type user record { x string; };
type user_id record { y string; };
`, mustOverride(t, "type:user=UserID"))
		if !strings.Contains(err.Error(), "Go declaration name collision") {
			t.Fatalf("override-induced collision error = %v", err)
		}
		// Distinct projections do not collide.
		f := parseFixture(t, `
type user record { x string; };
type user_id record { y string; };
`)
		n := projectFile(t, f)
		if got := n.Decl[findType(t, f, "user")]; got != "User" {
			t.Fatalf("type user projected as %q, want User", got)
		}
		if got := n.Decl[findType(t, f, "user_id")]; got != "UserID" {
			t.Fatalf("type user_id projected as %q, want UserID", got)
		}
	})

	t.Run("FieldCollisions", func(t *testing.T) {
		// Two wire fields of one record projecting to the same Pascal
		// name collide in the record's field scope.
		err := projectErr(t, `
type t record {
    user_id uint32;
    user_i_d uint32;
};
`)
		if !strings.Contains(err.Error(), "Go field name collision") ||
			!strings.Contains(err.Error(), "UserID") {
			t.Fatalf("field collision error = %v", err)
		}
		// A still-colliding field override is an error.
		err = projectErr(t, `
type t record {
    identifier uint32;
    user_id uint32;
};
`, mustOverride(t, "type:t/field:user_id=Identifier"))
		if !strings.Contains(err.Error(), "Go field name collision") {
			t.Fatalf("override-induced field collision error = %v", err)
		}
		// The same wire field name in different records is legal: each
		// record has its own field scope.
		project(t, `
type t record {
    a record { x string; };
    b record { x string; };
};
`)
	})

	t.Run("ParameterCollisions", func(t *testing.T) {
		// Two parameters of one procedure projecting to the same camel
		// name collide in the procedure's parameter scope.
		err := projectErr(t, `
procedure p {
    user_id uint32;
    user_i_d uint32;
};
`)
		if !strings.Contains(err.Error(), "Go parameter name collision") ||
			!strings.Contains(err.Error(), "userID") {
			t.Fatalf("parameter collision error = %v", err)
		}
		// The same parameter name in different procedures is legal, and a
		// parameter never collides with a package declaration or field.
		project(t, `
type t record { user_id uint32; };
procedure p { user_id uint32; };
procedure q { user_id uint32; };
`)
	})

	t.Run("NoncanonicalRequiresOverride", func(t *testing.T) {
		// A valid but noncanonical wire name cannot be projected by
		// default, at any of the three identifier kinds.
		err := projectErr(t, `
type UserID record { x string; };
`)
		if !strings.Contains(err.Error(), "requires a --go-name override") {
			t.Fatalf("noncanonical type error = %v", err)
		}
		err = projectErr(t, `
type t record { userID uint32; };
`)
		if !strings.Contains(err.Error(), "requires a --go-name override") {
			t.Fatalf("noncanonical field error = %v", err)
		}
		err = projectErr(t, `
procedure p { userID uint32; };
`)
		if !strings.Contains(err.Error(), "requires a --go-name override") {
			t.Fatalf("noncanonical parameter error = %v", err)
		}
		// With an override the same names project cleanly.
		f := parseFixture(t, `
type UserID record { x string; };
type t record { userID uint32; };
procedure p { userID uint32; };
`)
		n := projectFile(t, f,
			mustOverride(t, "type:UserID=User"),
			mustOverride(t, "type:t/field:userID=UserID"),
			mustOverride(t, "procedure:p/param:userID=uid"))
		if len(n.Decl) != 3 || len(n.Field) != 2 || len(n.Param) != 1 {
			t.Fatalf("override projection sizes = %d/%d/%d, want 3/2/1", len(n.Decl), len(n.Field), len(n.Param))
		}
	})

	t.Run("KeywordDefaults", func(t *testing.T) {
		// A camel projection that lands on a Go keyword is an error and
		// needs an override; Pascal projections can never be keywords.
		err := projectErr(t, `
procedure p { func string; };
`)
		if !strings.Contains(err.Error(), "Go keyword") {
			t.Fatalf("keyword default error = %v", err)
		}
		f := parseFixture(t, `
procedure p { func string; };
`)
		n := projectFile(t, f, mustOverride(t, "procedure:p/param:func=f"))
		if got := n.Param[findParam(t, findProc(t, f, "p"), "func")]; got != "f" {
			t.Fatalf("parameter func projected as %q, want f", got)
		}
	})
}

// randomCanonicalWire generates one canonical wire name with the given
// deterministic generator.
func randomCanonicalWire(rng *rand.Rand) string {
	words := 1 + rng.Intn(4)
	parts := make([]string, 0, words)
	for w := 0; w < words; w++ {
		n := 1 + rng.Intn(8)
		var b strings.Builder
		b.WriteByte(byte('a' + rng.Intn(26)))
		for j := 1; j < n; j++ {
			if rng.Intn(2) == 0 {
				b.WriteByte(byte('a' + rng.Intn(26)))
			} else {
				b.WriteByte(byte('0' + rng.Intn(10)))
			}
		}
		parts = append(parts, b.String())
	}
	return strings.Join(parts, "_")
}

func findType(t *testing.T, f *syntax.File, name string) *syntax.TypeDecl {
	t.Helper()
	for _, d := range f.Decls {
		if td, ok := d.(*syntax.TypeDecl); ok && td.Name.Name == name {
			return td
		}
	}
	t.Fatalf("type %q not found in fixture", name)
	return nil
}

func findException(t *testing.T, f *syntax.File, name string) *syntax.ExceptionDecl {
	t.Helper()
	for _, d := range f.Decls {
		if ed, ok := d.(*syntax.ExceptionDecl); ok && ed.Name.Name == name {
			return ed
		}
	}
	t.Fatalf("exception %q not found in fixture", name)
	return nil
}

func findProc(t *testing.T, f *syntax.File, name string) *syntax.ProcDecl {
	t.Helper()
	for _, d := range f.Decls {
		if pd, ok := d.(*syntax.ProcDecl); ok && pd.Name.Name == name {
			return pd
		}
	}
	t.Fatalf("procedure %q not found in fixture", name)
	return nil
}

func findField(t *testing.T, rec *syntax.RecordType, name string) *syntax.Field {
	t.Helper()
	for _, f := range rec.Fields {
		if f.Name.Name == name {
			return f
		}
	}
	t.Fatalf("field %q not found in record", name)
	return nil
}

func findParam(t *testing.T, proc *syntax.ProcDecl, name string) *syntax.Param {
	t.Helper()
	for _, p := range proc.Params {
		if p.Name.Name == name {
			return p
		}
	}
	t.Fatalf("parameter %q not found in procedure %q", name, proc.Name.Name)
	return nil
}
