package main

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

// This file tests the diagnostic rendering and ordering of SPEC.md
// "Diagnostics": path:line:column: message with one-based physical
// line and byte-column semantics, EOF at offset len(input), invalid
// UTF-8 at its first invalid byte, normalized logical paths, and
// sorted multi-diagnostics. Every test operates only in temporary
// directories.

// TestCommandDiagnostics black-box tests the command diagnostics.
func TestCommandDiagnostics(t *testing.T) {
	t.Run("SortedMultiErrors", func(t *testing.T) {
		// The grammar phase collects every violation and sorts it by
		// logical path, line, column, and message.
		root := t.TempDir()
		status, _, stderr := runCLI(t, "import", "--out", root, "--package", "123bad",
			"--go-name", "nope", "--go-name", "type:echo=bad-name", "a.intercall", "b.intercall")
		if status != 1 {
			t.Errorf("status = %d, want 1", status)
		}
		want := []string{
			`--go-name:1:1: invalid --go-name override "nope": expected SELECTOR=GoIdentifier`,
			`--go-name:1:1: invalid --go-name override "type:echo=bad-name": "bad-name" is not a usable Go identifier`,
			`--package:1:1: invalid Go package name "123bad": must match [A-Za-z_][A-Za-z0-9_]*, and must not be "_", "main", or a Go keyword`,
			"import:1:1: import requires exactly one interface file, got 2 operands",
		}
		if got := strings.Split(strings.TrimSuffix(stderr, "\n"), "\n"); !equalStrings(got, want) {
			t.Errorf("sorted diagnostics:\n got %q\nwant %q", got, want)
		}
	})

	t.Run("ExportGrammarMultiErrors", func(t *testing.T) {
		root := t.TempDir()
		status, _, stderr := runCLI(t, "export", "--out", root, "--go-name", "type:a=B")
		if status != 1 {
			t.Errorf("status = %d, want 1", status)
		}
		want := []string{
			"--go-name:1:1: option --go-name is only valid with import",
			"export:1:1: export requires --interface FILE",
			"export:1:1: export requires at least one package pattern",
		}
		if got := strings.Split(strings.TrimSuffix(stderr, "\n"), "\n"); !equalStrings(got, want) {
			t.Errorf("sorted diagnostics:\n got %q\nwant %q", got, want)
		}
	})

	t.Run("PhysicalByteColumn", func(t *testing.T) {
		// Invalid UTF-8 points at its first invalid byte. The column is
		// the physical byte column: the two-byte "é" before the bad
		// byte counts as two bytes, so the diagnostic sits at column 7,
		// not at the rune column 6.
		root := t.TempDir()
		file := importFixture(t, root, "utf8.intercall", "/* é \xff */\n")
		status, _, stderr := runCLI(t, "import", "--out", filepath.Join(root, "gen"), file)
		if status != 1 {
			t.Errorf("status = %d, want 1", status)
		}
		want := "utf8.intercall:1:7: invalid UTF-8 encoding"
		if !strings.Contains(stderr, want) {
			t.Errorf("stderr = %q, want it to contain %q", stderr, want)
		}
		if _, err := os.Stat(filepath.Join(root, "gen")); !os.IsNotExist(err) {
			t.Error("validation failure created the output directory")
		}
	})

	t.Run("EofPosition", func(t *testing.T) {
		// An EOF diagnostic uses offset len(input) under the same
		// physical line and byte-column rules.
		root := t.TempDir()
		src := "procedure broken {"
		file := importFixture(t, root, "broken.intercall", src)
		status, _, stderr := runCLI(t, "import", "--out", filepath.Join(root, "gen"), file)
		if status != 1 {
			t.Errorf("status = %d, want 1", status)
		}
		want := "broken.intercall:1:" + strconv.Itoa(len(src)+1) + ":"
		if !strings.Contains(stderr, want) {
			t.Errorf("stderr = %q, want it to contain %q", stderr, want)
		}
	})

	t.Run("LogicalPathNormalization", func(t *testing.T) {
		// Diagnostics use the cleaned logical operand path, never the
		// resolved or absolute path.
		root := t.TempDir()
		importFixture(t, root, "broken.intercall", "procedure broken {")
		t.Chdir(root)
		operand := "a" + string(os.PathSeparator) + ".." + string(os.PathSeparator) + "broken.intercall"
		status, _, stderr := runCLI(t, "import", "--out", filepath.Join(root, "gen"), operand)
		if status != 1 {
			t.Errorf("status = %d, want 1", status)
		}
		if !strings.Contains(stderr, "broken.intercall:1:19:") {
			t.Errorf("stderr = %q, want the normalized logical path", stderr)
		}
		if strings.Contains(stderr, "a"+string(os.PathSeparator)+"..") {
			t.Errorf("stderr = %q, want the cleaned operand path", stderr)
		}
	})

	t.Run("ExportLogicalPathNormalization", func(t *testing.T) {
		// The export interface operand is normalized the same way: an
		// unowned interface named through a dotted path is reported at
		// the cleaned logical path.
		root := exportFixture(t)
		cliEnv(t, root)
		intf := filepath.Join(root, "interfaces", "api.intercall")
		if err := os.WriteFile(intf, []byte("exception seed;\n"), 0o644); err != nil {
			t.Fatal(err)
		}
		out := filepath.Join(root, "out")
		operand := "sub" + string(os.PathSeparator) + ".." + string(os.PathSeparator) + filepath.Join("interfaces", "api.intercall")
		status, _, stderr := runCLI(t, "export", "--out", out, "--interface", operand, "./prov")
		if status != 1 {
			t.Errorf("status = %d, want 1", status)
		}
		if !strings.Contains(stderr, "interfaces/api.intercall:1:1:") {
			t.Errorf("stderr = %q, want the normalized logical interface path", stderr)
		}
		if strings.Contains(stderr, "sub"+string(os.PathSeparator)+"..") || strings.Contains(stderr, root) {
			t.Errorf("stderr = %q, want the cleaned logical path only", stderr)
		}
	})

	t.Run("WrongCommandOptions", func(t *testing.T) {
		root := t.TempDir()
		status, _, stderr := runCLI(t, "import", "--out", root, "--interface", "api.intercall", "f.intercall")
		if status != 1 || !strings.Contains(stderr, "--interface:1:1: option --interface is only valid with export") {
			t.Errorf("import with --interface: status %d, stderr %q", status, stderr)
		}
		status, _, stderr = runCLI(t, "import", "--out", root, "--include", "a.B", "f.intercall")
		if status != 1 || !strings.Contains(stderr, "--include:1:1: option --include is only valid with export") {
			t.Errorf("import with --include: status %d, stderr %q", status, stderr)
		}
		status, _, stderr = runCLI(t, "export", "--out", root, "--interface", "api.intercall", "--go-name", "type:a=B", "./prov")
		if status != 1 || !strings.Contains(stderr, "--go-name:1:1: option --go-name is only valid with import") {
			t.Errorf("export with --go-name: status %d, stderr %q", status, stderr)
		}
	})

	t.Run("RepeatedNonRepeatableOption", func(t *testing.T) {
		root := t.TempDir()
		status, _, stderr := runCLI(t, "import", "--out", root, "--out", root, "f.intercall")
		if status != 1 || !strings.Contains(stderr, "--out:1:1: duplicate option --out") {
			t.Errorf("duplicate --out: status %d, stderr %q", status, stderr)
		}
	})

	t.Run("MissingOptionValues", func(t *testing.T) {
		status, _, stderr := runCLI(t, "import", "--out")
		if status != 1 || !strings.Contains(stderr, "--out:1:1: option --out requires a value") {
			t.Errorf("missing value: status %d, stderr %q", status, stderr)
		}
		status, _, stderr = runCLI(t, "import", "--out=", "f.intercall")
		if status != 1 || !strings.Contains(stderr, "--out=:1:1: option --out requires a non-empty value") {
			t.Errorf("empty value: status %d, stderr %q", status, stderr)
		}
	})

	t.Run("UnknownOption", func(t *testing.T) {
		root := t.TempDir()
		status, _, stderr := runCLI(t, "import", "--out", root, "--frobnicate", "f.intercall")
		if status != 1 || !strings.Contains(stderr, `--frobnicate:1:1: unknown option "--frobnicate"`) {
			t.Errorf("unknown option: status %d, stderr %q", status, stderr)
		}
	})

	t.Run("OperandsStopOptions", func(t *testing.T) {
		// After the first operand every token is an operand, so a flag
		// after the interface file is an operand-count error, and the
		// terminator "--" starts the operands, making a following
		// option-like token an operand.
		root := t.TempDir()
		status, _, stderr := runCLI(t, "import", "--out", root, "f.intercall", "--package", "gen")
		if status != 1 || !strings.Contains(stderr, "import requires exactly one interface file, got 3 operands") {
			t.Errorf("post-operand flags: status %d, stderr %q", status, stderr)
		}
		status, _, stderr = runCLI(t, "import", "--out", filepath.Join(root, "out"), "--", "--weird")
		if status != 1 || !strings.Contains(stderr, "reading --weird:") {
			t.Errorf("terminator: status %d, stderr %q", status, stderr)
		}
	})
}

// equalStrings reports whether two string slices are equal.
func equalStrings(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
