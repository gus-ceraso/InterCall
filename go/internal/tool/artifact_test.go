package tool

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/cerasos/intercall/go/internal/syntax"
)

// This file unit-tests the exact stamps, ownership lines, host
// filename equivalence helpers, and package-name resolution of SPEC.md
// "One-file ownership and safe replacement". The host-filesystem
// behavior of the writer itself is tested in artifact_filesystem_test.go,
// always in temporary directories.

// testBindingGo is a complete generated binding body after the two
// ownership lines: its package clause and body. The bytes are already
// gofmt-canonical, so the writer's format.Source pass leaves them
// unchanged.
const testBindingGo = `package gen

const maxInt = int(^uint(0) >> 1)
`

// testBindingGoOther is the same binding body with a different package
// clause.
const testBindingGoOther = `package other

const maxInt = int(^uint(0) >> 1)
`

// testInterfaceSrc1 and testInterfaceSrc2 are two distinct interface
// sources whose canonical bodies drive the stamp and the artifact
// bytes.
const (
	testInterfaceSrc1 = "exception done;\nprocedure echo {\n    value string;\n} string;\n"
	testInterfaceSrc2 = "exception done;\nprocedure echo {\n    value string;\n} string;\nprocedure ping {};\n"
)

// canonicalBody parses, validates, and formats one interface source to
// its byte-exact canonical body.
func canonicalBody(t *testing.T, src string) []byte {
	t.Helper()
	f, err := syntax.Parse("body.intercall", []byte(src))
	if err != nil {
		t.Fatalf("parsing body: %v", err)
	}
	if err := syntax.Validate(f); err != nil {
		t.Fatalf("validating body: %v", err)
	}
	return syntax.Format(f)
}

// composedBinding composes the exact owned binding file of one mode,
// stamp, and generated body: the marker line, the binding line, one
// blank line, and the body.
func composedBinding(mode ArtifactMode, stamp string, goFile []byte) []byte {
	var b bytes.Buffer
	b.WriteString(intercallGeneratedMarker)
	b.WriteByte('\n')
	b.WriteString(bindingOwnershipPrefix)
	b.WriteString(mode.String())
	b.WriteString(" sha256:")
	b.WriteString(stamp)
	b.WriteByte('\n')
	b.WriteByte('\n')
	b.Write(goFile)
	return b.Bytes()
}

// ownedInterfaceBytes composes the exact owned interface file of one
// canonical body: the marker with the body's stamp, one blank line,
// and the body.
func ownedInterfaceBytes(body []byte) []byte {
	return []byte(interfaceMarkerPrefix + ArtifactStamp(body) + interfaceMarkerSuffix + "\n\n" + string(body))
}

// writeOnce runs one complete artifact write and fails the test on
// error. The write uses the import checker, whose synthetic runtime
// SPI model and standard-library resolution accept any checker-relevant
// content these tests stage; a test that needs the production checker
// of one direction supplies its own CheckGo.
func writeOnce(t *testing.T, cfg WriteConfig) {
	t.Helper()
	if cfg.CheckGo == nil {
		cfg.CheckGo = NewImportGoChecker()
	}
	if err := WriteArtifacts(cfg); err != nil {
		t.Fatalf("WriteArtifacts: %v", err)
	}
}

// writeFails runs one complete artifact write and requires an error
// containing every substring.
func writeFails(t *testing.T, cfg WriteConfig, contains ...string) {
	t.Helper()
	if cfg.CheckGo == nil {
		cfg.CheckGo = NewImportGoChecker()
	}
	wantErr(t, WriteArtifacts(cfg), contains...)
}

// noTemps fails the test when dir contains a leftover staging file.
func noTemps(t *testing.T, dir string) {
	t.Helper()
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("reading %s: %v", dir, err)
	}
	for _, e := range entries {
		if strings.Contains(e.Name(), ".tmp-") {
			t.Errorf("%s contains leftover staging file %q", dir, e.Name())
		}
	}
}

func TestArtifactStamp(t *testing.T) {
	body := []byte("exception done;\n")
	sum := sha256.Sum256(body)
	if got, want := ArtifactStamp(body), hex.EncodeToString(sum[:]); got != want {
		t.Errorf("ArtifactStamp = %q, want %q", got, want)
	}
	if got := ArtifactStamp(body); len(got) != artifactStampLen {
		t.Errorf("ArtifactStamp length = %d, want %d", len(got), artifactStampLen)
	}
	if got := ArtifactStamp(body); got != strings.ToLower(got) {
		t.Errorf("ArtifactStamp = %q, want lowercase", got)
	}
	if ArtifactStamp(nil) != ArtifactStamp([]byte{}) {
		t.Error("ArtifactStamp(nil) differs from ArtifactStamp([]byte{})")
	}
	if ArtifactStamp([]byte("a")) == ArtifactStamp([]byte("b")) {
		t.Error("distinct bodies must have distinct stamps")
	}
}

func TestValidStamp(t *testing.T) {
	stamp := ArtifactStamp([]byte("body"))
	if !validStamp(stamp) {
		t.Fatalf("validStamp(%q) = false, want true", stamp)
	}
	for _, bad := range []string{
		"",
		strings.Repeat("0", artifactStampLen-1),
		strings.Repeat("0", artifactStampLen+1),
		strings.ToUpper(stamp),
		strings.Repeat("g", artifactStampLen), // not hex
		strings.Repeat("0", artifactStampLen-1) + "G",
		strings.Repeat("0", artifactStampLen) + "x",
	} {
		if validStamp(bad) {
			t.Errorf("validStamp(%q) = true, want false", bad)
		}
	}
}

func TestParseBindingOwnership(t *testing.T) {
	stamp := ArtifactStamp([]byte("body"))
	for _, mode := range []ArtifactMode{ImportMode, ExportMode} {
		src := composedBinding(mode, stamp, []byte(testBindingGo))
		bo, ok := parseBindingOwnership(src)
		if !ok {
			t.Fatalf("parseBindingOwnership(%s) = not owned", mode)
		}
		if bo.mode != mode {
			t.Errorf("mode = %v, want %v", bo.mode, mode)
		}
		if bo.stamp != stamp {
			t.Errorf("stamp = %q, want %q", bo.stamp, stamp)
		}
	}

	valid := composedBinding(ExportMode, stamp, []byte(testBindingGo))
	cases := []struct {
		name string
		src  []byte
	}{
		{"WrongFirstLine", []byte("// hand written\n" + string(valid[strings.IndexByte(string(valid), '\n')+1:]))},
		{"MissingSecondLine", []byte(intercallGeneratedMarker + "\n")},
		{"OnlyMarker", []byte(intercallGeneratedMarker)},
		{"Empty", nil},
		{"BadKind", []byte(intercallGeneratedMarker + "\n// intercall-go binding: bridge sha256:" + stamp + "\n")},
		{"MissingKind", []byte(intercallGeneratedMarker + "\n// intercall-go binding: sha256:" + stamp + "\n")},
		{"UppercaseStamp", []byte(intercallGeneratedMarker + "\n// intercall-go binding: export sha256:" + strings.ToUpper(stamp) + "\n")},
		{"ShortStamp", []byte(intercallGeneratedMarker + "\n// intercall-go binding: export sha256:" + stamp[:10] + "\n")},
		{"NonHexStamp", []byte(intercallGeneratedMarker + "\n// intercall-go binding: export sha256:" + strings.Repeat("z", artifactStampLen) + "\n")},
		{"EmptyStamp", []byte(intercallGeneratedMarker + "\n// intercall-go binding: export sha256:\n")},
		{"TrailingGarbage", []byte(intercallGeneratedMarker + "\n// intercall-go binding: export sha256:" + stamp + " extra\n")},
		{"TrailingSpace", []byte(intercallGeneratedMarker + "\n// intercall-go binding: export sha256:" + stamp + " \n")},
		{"WrongPrefix", []byte(intercallGeneratedMarker + "\n// intercall binding: export sha256:" + stamp + "\n")},
		{"EmptySecondLine", []byte(intercallGeneratedMarker + "\n\npackage gen\n")},
	}
	for _, tc := range cases {
		if _, ok := parseBindingOwnership(tc.src); ok {
			t.Errorf("parseBindingOwnership(%s) = owned, want not owned", tc.name)
		}
	}

	// A CRLF-owned file is recognized: a trailing '\r' is a line
	// terminator, not part of the line.
	crlf := bytes.ReplaceAll(valid, []byte("\n"), []byte("\r\n"))
	if bo, ok := parseBindingOwnership(crlf); !ok || bo.stamp != stamp {
		t.Errorf("parseBindingOwnership(CRLF) = %+v, %v; want owned with stamp %q", bo, ok, stamp)
	}

	// The second line may end the file without a trailing newline.
	noFinalNL := bytes.TrimSuffix(valid, []byte("\n"))
	if bo, ok := parseBindingOwnership(noFinalNL); !ok || bo.stamp != stamp {
		t.Errorf("parseBindingOwnership(no final newline) = %+v, %v; want owned", bo, ok)
	}
}

func TestParseInterfaceOwnership(t *testing.T) {
	body := []byte("exception done;\nprocedure echo(value string) string;\n")
	valid := ownedInterfaceBytes(body)
	io, ok := parseInterfaceOwnership(valid)
	if !ok {
		t.Fatal("parseInterfaceOwnership(valid) = not owned")
	}
	if io.stamp != ArtifactStamp(body) {
		t.Errorf("stamp = %q, want %q", io.stamp, ArtifactStamp(body))
	}
	if !bytes.Equal(io.body, body) {
		t.Errorf("body = %q, want %q", io.body, body)
	}

	cases := []struct {
		name string
		src  []byte
	}{
		{"Empty", nil},
		{"MarkerOnly", []byte(interfaceMarkerPrefix + ArtifactStamp(body) + interfaceMarkerSuffix)},
		{"MarkerAndBodyNoBlank", []byte(interfaceMarkerPrefix + ArtifactStamp(body) + interfaceMarkerSuffix + "\n" + string(body))},
		{"MarkerTwoBlankLines", []byte(interfaceMarkerPrefix + ArtifactStamp(body) + interfaceMarkerSuffix + "\n\n\n" + string(body))},
		{"WrongPrefix", []byte("/* generated; sha256:" + ArtifactStamp(body) + "; */\n\n" + string(body))},
		{"WrongSuffix", []byte("/* Code generated by intercall-go; artifact sha256:" + ArtifactStamp(body) + " */\n\n" + string(body))},
		{"UppercaseStamp", []byte(interfaceMarkerPrefix + strings.ToUpper(ArtifactStamp(body)) + interfaceMarkerSuffix + "\n\n" + string(body))},
		{"BadStamp", []byte(interfaceMarkerPrefix + strings.Repeat("0", artifactStampLen) + interfaceMarkerSuffix + "\n\n" + string(body))},
		{"TamperedBody", []byte(interfaceMarkerPrefix + ArtifactStamp(body) + interfaceMarkerSuffix + "\n\n" + string(body) + " ")},
		{"EditedBody", []byte(interfaceMarkerPrefix + ArtifactStamp(body) + interfaceMarkerSuffix + "\n\ntype edited;\n")},
		{"CRLF", []byte(bytes.ReplaceAll(valid, []byte("\n"), []byte("\r\n")))},
		{"TrailingSpace", []byte(interfaceMarkerPrefix + ArtifactStamp(body) + interfaceMarkerSuffix + " \n\n" + string(body))},
	}
	for _, tc := range cases {
		if _, ok := parseInterfaceOwnership(tc.src); ok {
			t.Errorf("parseInterfaceOwnership(%s) = owned, want not owned", tc.name)
		}
	}

	// An empty canonical body is owned: marker, blank line, zero bytes.
	empty := ownedInterfaceBytes(nil)
	io, ok = parseInterfaceOwnership(empty)
	if !ok {
		t.Fatal("parseInterfaceOwnership(empty body) = not owned")
	}
	if io.stamp != ArtifactStamp(nil) || len(io.body) != 0 {
		t.Errorf("empty body parse = stamp %q, body %q", io.stamp, io.body)
	}
}

func TestArtifactNameEquivalence(t *testing.T) {
	// On a host that distinguishes case (fold == nil) only the exact
	// names match.
	if !isBindingTargetName(nil, "binding_gen.go") {
		t.Error("isBindingTargetName(nil, binding_gen.go) = false, want true")
	}
	if isBindingTargetName(nil, "BINDING_GEN.GO") {
		t.Error("isBindingTargetName(nil, BINDING_GEN.GO) = true, want false on a case-sensitive host")
	}
	if !isGoFileName(nil, "x.go") || !isGoFileName(nil, "binding_gen.go") {
		t.Error("isGoFileName(nil, *.go) = false, want true")
	}
	if isGoFileName(nil, "X.GO") || isGoFileName(nil, "notes.txt") || isGoFileName(nil, "api.intercall") {
		t.Error("isGoFileName(nil, non-.go name) = true, want false on a case-sensitive host")
	}

	// Simulate a case-folding host with strings.ToLower: case variants
	// of the target name and of the ".go" suffix are equivalent.
	fold := strings.ToLower
	if !isBindingTargetName(fold, "BINDING_GEN.GO") {
		t.Error("isBindingTargetName(fold, BINDING_GEN.GO) = false, want true")
	}
	if !isGoFileName(fold, "X.GO") || !isGoFileName(fold, "API.GO") {
		t.Error("isGoFileName(fold, X.GO / API.GO) = false, want true")
	}
	if isGoFileName(fold, "api.intercall") {
		t.Error("isGoFileName(fold, api.intercall) = true, want false")
	}
}

func TestOutputPackageBase(t *testing.T) {
	if got := outputPackageBase("gen"); got != "gen" {
		t.Errorf("outputPackageBase(gen) = %q", got)
	}
	if got := outputPackageBase("./gen"); got != "gen" {
		t.Errorf("outputPackageBase(./gen) = %q", got)
	}
	if got := outputPackageBase("a/b/gen"); got != "gen" {
		t.Errorf("outputPackageBase(a/b/gen) = %q", got)
	}
	if got := outputPackageBase(filepath.Join(t.TempDir(), "newpkg")); got != "newpkg" {
		t.Errorf("outputPackageBase(new dir) = %q", got)
	}
	// An existing directory resolves through the host filesystem, so a
	// symlink contributes its target's name.
	real := filepath.Join(t.TempDir(), "realpkg")
	if err := os.MkdirAll(real, 0o777); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(t.TempDir(), "link")
	if err := os.Symlink(real, link); err != nil {
		t.Fatal(err)
	}
	if got := outputPackageBase(link); got != "realpkg" {
		t.Errorf("outputPackageBase(symlink) = %q, want realpkg", got)
	}
	// "." resolves through the working directory.
	t.Chdir(t.TempDir())
	wd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if got, want := outputPackageBase("."), filepath.Base(wd); got != want {
		t.Errorf("outputPackageBase(.) = %q, want %q", got, want)
	}
}

func TestResolvePackageName(t *testing.T) {
	// A fresh output directory uses its base name.
	out := filepath.Join(t.TempDir(), "freshpkg")
	if got, err := ResolvePackageName(ExportMode, out, ""); err != nil || got != "freshpkg" {
		t.Errorf("ResolvePackageName(fresh) = %q, %v; want freshpkg", got, err)
	}
	// An explicit name wins for a fresh directory.
	if got, err := ResolvePackageName(ExportMode, out, "explicit"); err != nil || got != "explicit" {
		t.Errorf("ResolvePackageName(fresh, explicit) = %q, %v", got, err)
	}

	// An existing owned binding's package clause wins without an
	// explicit name.
	owned := filepath.Join(t.TempDir(), "ownedpkg")
	writeOnce(t, WriteConfig{
		Mode:          ExportMode,
		OutDir:        owned,
		Package:       "gen",
		InterfacePath: filepath.Join(t.TempDir(), "api.intercall"),
		GoFile:        []byte(testBindingGo),
		InterfaceBody: canonicalBody(t, testInterfaceSrc1),
	})
	if got, err := ResolvePackageName(ExportMode, owned, ""); err != nil || got != "gen" {
		t.Errorf("ResolvePackageName(owned) = %q, %v; want gen", got, err)
	}
	// An explicit name must equal the owned binding's clause.
	if _, err := ResolvePackageName(ExportMode, owned, "other"); err == nil || !strings.Contains(err.Error(), "must equal") {
		t.Errorf("ResolvePackageName(owned, other) error = %v, want a mismatch error", err)
	}
	if got, err := ResolvePackageName(ExportMode, owned, "gen"); err != nil || got != "gen" {
		t.Errorf("ResolvePackageName(owned, gen) = %q, %v", got, err)
	}

	// An existing binding of the other mode is a collision.
	imported := filepath.Join(t.TempDir(), "imported")
	writeOnce(t, WriteConfig{
		Mode:    ImportMode,
		OutDir:  imported,
		Package: "gen",
		GoFile:  []byte(testBindingGo),
	})
	if _, err := ResolvePackageName(ExportMode, imported, ""); err == nil || !strings.Contains(err.Error(), "import binding") {
		t.Errorf("ResolvePackageName(other mode) error = %v, want a mode collision", err)
	}

	// An unowned existing binding is a collision.
	unowned := filepath.Join(t.TempDir(), "unowned")
	if err := os.MkdirAll(unowned, 0o777); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(unowned, bindingFile), []byte("package gen\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := ResolvePackageName(ExportMode, unowned, ""); err == nil || !strings.Contains(err.Error(), "not replaceable") {
		t.Errorf("ResolvePackageName(unowned) error = %v, want not replaceable", err)
	}

	// Invalid explicit names are rejected before any filesystem read.
	for _, bad := range []string{"_", "main", "type", "1bad", "not-valid"} {
		if _, err := ResolvePackageName(ExportMode, out, bad); err == nil {
			t.Errorf("ResolvePackageName(%q) succeeded, want an invalid package name error", bad)
		}
	}

	// An invalid base name for a new output is an error; the tool never
	// sanitizes it.
	badBase := filepath.Join(t.TempDir(), "123bad")
	if _, err := ResolvePackageName(ExportMode, badBase, ""); err == nil || !strings.Contains(err.Error(), "base name") {
		t.Errorf("ResolvePackageName(bad base) error = %v, want a base-name error", err)
	}

	// Empty operands are errors.
	if _, err := ResolvePackageName(ArtifactMode(9), out, ""); err == nil {
		t.Error("ResolvePackageName(bad mode) succeeded")
	}
	if _, err := ResolvePackageName(ExportMode, "", ""); err == nil {
		t.Error("ResolvePackageName(no out dir) succeeded")
	}
}
