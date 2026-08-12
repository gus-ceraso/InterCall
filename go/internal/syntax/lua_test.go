package syntax_test

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/cerasos/intercall/go/internal/syntax"
)

// TestLuaDifferential compares the Go validity decision — Parse plus
// Validate — against the optional Lua oracle intercall-validate.lua for
// the complete fixture corpus and a set of inline semantic cases. The Lua
// validator is non-normative: only the binary valid/invalid decision is
// compared, and any difference must be investigated against README.md,
// never resolved by following Lua.
//
// When lua or LPeg is unavailable the test skips; PLAN.md's evidence
// command guards this case in the shell first.
func TestLuaDifferential(t *testing.T) {
	_, thisFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("cannot locate test source")
	}
	script := filepath.Join(filepath.Dir(thisFile), "..", "..", "..", "intercall-validate.lua")
	if _, err := os.Stat(script); err != nil {
		t.Fatalf("Lua oracle script not found at %s", script)
	}
	if _, err := exec.LookPath("lua"); err != nil {
		t.Skip("lua not available")
	}
	if out, err := exec.Command("lua", "-e", "require('lpeg')").CombinedOutput(); err != nil {
		t.Skipf("LPeg unavailable: %v (%s)", err, out)
	}

	var cases []struct {
		name string
		src  []byte
	}
	// The complete fixture corpus.
	for _, dir := range []string{"valid", "invalid"} {
		paths, err := filepath.Glob(filepath.Join("testdata", dir, "*.intercall"))
		if err != nil {
			t.Fatal(err)
		}
		for _, path := range paths {
			src, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			cases = append(cases, struct {
				name string
				src  []byte
			}{path, src})
		}
	}
	// Inline semantic and boundary cases beyond the fixtures.
	inline := []struct{ name, src string }{
		{"empty", ""},
		{"trivia only", " \t\r\n"},
		{"comment only", "/* c */"},
		{"README example", "exception failed;\nprocedure echo {\n    value uint16;\n} uint16;\n"},
		{"self reference", "type t t;"},
		{"forward reference", "type a b; type b uint8;"},
		{"transitive forward", "type a b; type b c; type c uint8;"},
		{"unknown reference", "type a missing;"},
		{"reference to exception", "exception e uint8; type a e;"},
		{"reference to procedure", "procedure p {}; type a p;"},
		{"valid chain", "type a uint8; type b a; type c list b; type d record { f c; };"},
		{"duplicate type", "type a uint8; type a string;"},
		{"duplicate cross kind", "exception e; type e uint8;"},
		{"duplicate param", "procedure p { x uint8; x string; };"},
		{"duplicate field", "type t record { f uint8; f string; };"},
		{"duplicate nested field", "type t record { r record { f uint8; f string; }; };"},
		{"local equals global", "type x uint8; procedure p { x uint8; } x;"},
		{"nested scopes", "type t record { x uint8; r record { x uint8; }; };"},
		{"unreachable invalid", "type a uint8; type b c;"},
		{"zero procedure key", "procedure p3vuYLtJYAr {};"},
		{"zero exception key", "exception vi_rZiaP6_h;"},
		{"procedure collision", "procedure gaft2bn2kl5il {}; procedure an1bk5lqs3ekj {};"},
		{"exception collision", "exception can20qcvtehol; exception ejmcku0cl4u50;"},
		{"cross collision", "procedure oopqz20a4nrzy {}; exception k53gkqh4kufnc;"},
		{"duplicate exception", "exception e; exception e uint8;"},
		{"duplicate procedure", "procedure p {}; procedure p {} uint8;"},
		{"reserved name", "type list uint8;"},
		{"local reserved", "procedure p { list uint8; };"},
		{"record empty", "type t record {};"},
		{"procedure no return", "procedure ping {};"},
		{"exception no payload", "exception unavailable;"},
	}
	for _, c := range inline {
		cases = append(cases, struct {
			name string
			src  []byte
		}{c.name, []byte(c.src)})
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			goValid := goIsValid(c.src)
			luaValid := luaIsValid(t, script, c.src)
			if goValid != luaValid {
				t.Errorf("decision mismatch: Go %v, Lua %v for input:\n%s", goValid, luaValid, c.src)
			}
		})
	}
}

// goIsValid reports whether the Go pipeline (Parse then Validate) accepts
// the input.
func goIsValid(src []byte) bool {
	f, err := syntax.Parse("differential", src)
	if err != nil {
		return false
	}
	return syntax.Validate(f) == nil
}

// luaIsValid runs the oracle script with the input on stdin and reports
// whether it exits 0 (valid). Exit code 2 signals an environment problem
// (missing LPeg or unreadable input) and fails the test rather than
// producing a bogus decision.
func luaIsValid(t *testing.T, script string, src []byte) bool {
	t.Helper()
	cmd := exec.Command("lua", script, "-")
	cmd.Stdin = bytes.NewReader(src)
	out, err := cmd.CombinedOutput()
	if err != nil {
		if ee, ok := err.(*exec.ExitError); ok {
			if ee.ExitCode() == 2 {
				t.Fatalf("lua oracle environment error: %s", out)
			}
			if ee.ExitCode() == 1 {
				return false // invalid per the oracle
			}
		}
		t.Fatalf("lua oracle failed to run: %v (%s)", err, out)
	}
	return true
}
