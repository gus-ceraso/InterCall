package tool

import (
	"bytes"
	"go/ast"
	"go/parser"
	"go/token"
	"go/types"
	"os"
	"path/filepath"
	"testing"

	"github.com/cerasos/intercall/go/internal/syntax"
)

// This file tests the compiled export fixture as generated code
// through the complete artifact pipeline: the provider fixture package
// is discovered in the module, mapped to the export model, emitted,
// staged through the artifact writer into a temporary directory, and
// compared byte for byte against the checked-in binding and the golden
// owned interface file. Validation never rewrites the checked-in
// fixture. The fixture package also compiles as a real dependency of
// this test binary, so a generated source that does not compile fails
// here before any runtime scenario can run.

// exportFixturePackage is the package name of the compiled export
// fixture.
const exportFixturePackage = "exportfixture"

// exportFixtureGenPath is the checked-in generated fixture, relative to
// the package directory that go test runs in.
const exportFixtureGenPath = "exportfixture/binding_gen.go"

// exportFixtureInterfaceGolden is the checked-in golden owned interface
// file of the export fixture: the exact ownership marker, one blank
// line, and the byte-exact canonical interface body.
const exportFixtureInterfaceGolden = "testdata/export/export.intercall"

// exportFixtureProviderPattern is the discovery operand of the provider
// fixture package, relative to the module root.
const exportFixtureProviderPattern = "./internal/tool/exportfixture/prov"

// exportFixtureOutputDir is the discovery output directory operand: the
// checked-in export fixture package the generated binding belongs to.
const exportFixtureOutputDir = "./internal/tool/exportfixture"

// exportFixtureOutPath is the import path of the export fixture
// package, used for the export model's importability checks.
const exportFixtureOutPath = "github.com/cerasos/intercall/go/internal/tool/exportfixture"

// generateExportFixture runs the complete export generation pipeline
// over the provider fixture: discovery in the active module, the export
// model with the importability checks, and the emitter. The result is
// the byte-exact content behind the ownership lines of
// internal/tool/exportfixture/binding_gen.go and the byte-exact
// canonical interface body of the golden owned interface file.
func generateExportFixture(t *testing.T) (goFile []byte, body []byte) {
	t.Helper()
	root, err := filepath.Abs(filepath.Join("..", ".."))
	if err != nil {
		t.Fatalf("resolving the module root: %v", err)
	}
	res, err := discover(t, root, []string{exportFixtureProviderPattern}, nil, nil, exportFixtureOutputDir)
	if err != nil {
		t.Fatalf("discover: %v", err)
	}
	model, err := MapExport(res, exportFixtureOutPath)
	if err != nil {
		t.Fatalf("MapExport: %v", err)
	}
	goFile, body, err = GenerateExport(model, exportFixturePackage)
	if err != nil {
		t.Fatalf("GenerateExport: %v", err)
	}
	return goFile, body
}

// generateExportModel builds the export model of the provider fixture
// without emitting, for tests that need the model itself.
func generateExportModel(t *testing.T) *ExportModel {
	t.Helper()
	root, err := filepath.Abs(filepath.Join("..", ".."))
	if err != nil {
		t.Fatalf("resolving the module root: %v", err)
	}
	res, err := discover(t, root, []string{exportFixtureProviderPattern}, nil, nil, exportFixtureOutputDir)
	if err != nil {
		t.Fatalf("discover: %v", err)
	}
	model, err := MapExport(res, exportFixtureOutPath)
	if err != nil {
		t.Fatalf("MapExport: %v", err)
	}
	return model
}

// composeExportInterface assembles one complete owned export interface
// file exactly as the artifact layer does: the interface ownership
// marker with the artifact stamp of the canonical body, one blank line,
// and the canonical interface body bytes.
func composeExportInterface(body []byte) []byte {
	var b bytes.Buffer
	b.WriteString(interfaceMarkerPrefix)
	b.WriteString(ArtifactStamp(body))
	b.WriteString(interfaceMarkerSuffix)
	b.WriteByte('\n')
	b.WriteByte('\n')
	b.Write(body)
	return b.Bytes()
}

// TestExportGeneratedFixtureCompiles regenerates the export fixture
// through the complete artifact pipeline into a temporary directory,
// verifies the checked-in binding and the golden owned interface file
// are byte-identical to the staged artifacts, and type-checks the
// regenerated source against the standard library, the module root
// runtime package, and the provider fixture package. Validation never
// rewrites the checked-in fixture.
func TestExportGeneratedFixtureCompiles(t *testing.T) {
	goFile, body := generateExportFixture(t)
	dir := t.TempDir()
	if err := WriteArtifacts(WriteConfig{
		Mode:          ExportMode,
		OutDir:        dir,
		Package:       exportFixturePackage,
		InterfacePath: filepath.Join(dir, "export.intercall"),
		GoFile:        goFile,
		InterfaceBody: body,
	}); err != nil {
		t.Fatalf("WriteArtifacts: %v", err)
	}
	written, err := os.ReadFile(filepath.Join(dir, bindingFile))
	if err != nil {
		t.Fatalf("reading the staged binding: %v", err)
	}
	checked, err := os.ReadFile(exportFixtureGenPath)
	if err != nil {
		t.Fatalf("reading %s: %v", exportFixtureGenPath, err)
	}
	if !bytes.Equal(written, checked) {
		t.Fatalf("%s is stale: the staged artifact (%d bytes) differs from the checked-in file (%d bytes); regenerate it from the emitter", exportFixtureGenPath, len(written), len(checked))
	}
	writtenIntf, err := os.ReadFile(filepath.Join(dir, "export.intercall"))
	if err != nil {
		t.Fatalf("reading the staged interface: %v", err)
	}
	golden, err := os.ReadFile(exportFixtureInterfaceGolden)
	if err != nil {
		t.Fatalf("reading %s: %v", exportFixtureInterfaceGolden, err)
	}
	if !bytes.Equal(writtenIntf, golden) {
		t.Fatalf("%s is stale: the staged interface (%d bytes) differs from the golden file (%d bytes); regenerate it from the emitter", exportFixtureInterfaceGolden, len(writtenIntf), len(golden))
	}
	// The golden interface is the exact ownership form followed by one
	// blank line and the canonical interface body.
	wantIntf := composeExportInterface(body)
	if !bytes.Equal(golden, wantIntf) {
		t.Fatal("the golden owned interface file does not match the composed ownership form")
	}
	// The golden body is canonical: it parses, attaches its
	// documentation, validates, and reformats to the identical bytes.
	f, err := syntax.Parse("export.intercall", body)
	if err != nil {
		t.Fatalf("parsing the canonical interface body: %v", err)
	}
	syntax.AttachDocs(f)
	if err := syntax.Validate(f); err != nil {
		t.Fatalf("validating the canonical interface body: %v", err)
	}
	if formatted := syntax.Format(f); !bytes.Equal(formatted, body) {
		t.Fatal("the canonical interface body does not survive the parse-validate-format round trip")
	}
	typeCheckExportBinding(t, written)
}

// typeCheckExportBinding parses and type-checks one complete export
// binding source against the standard library, the module root runtime
// package, and the provider fixture package, verifying it is valid Go
// independent of the compiled fixture package.
func typeCheckExportBinding(t *testing.T, src []byte) {
	t.Helper()
	typeCheckBinding(t, exportFixtureGenPath, exportFixturePackage, src, nil)
}

// typeCheckSyntheticExportBinding parses and type-checks one generated
// export binding against synthetic provider packages: each package is
// written to its own temporary directory and loaded from source
// alongside the module packages, so synthetic-model bindings are
// verified as generated code without a module build.
func typeCheckSyntheticExportBinding(t *testing.T, dirs map[string]string, src []byte) {
	t.Helper()
	extra := make(map[string]string, len(dirs))
	for path, content := range dirs {
		dir := t.TempDir()
		if err := os.WriteFile(filepath.Join(dir, "synth.go"), []byte(content), 0o644); err != nil {
			t.Fatalf("writing the synthetic provider %s: %v", path, err)
		}
		extra[path] = dir
	}
	typeCheckBinding(t, "binding_gen.go", "exp", src, extra)
}

// typeCheckBinding parses and type-checks one generated binding source
// against the standard library, the module source packages, and any
// extra source packages, verifying it is valid Go independent of the
// compiled fixture package.
func typeCheckBinding(t *testing.T, logical, pkg string, src []byte, extra map[string]string) {
	t.Helper()
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, logical, src, parser.AllErrors)
	if err != nil {
		t.Fatalf("generated binding does not parse: %v", err)
	}
	dirs := make(map[string]string, len(moduleSourceDirs)+len(extra))
	for path, dir := range moduleSourceDirs {
		dirs[path] = dir
	}
	for path, dir := range extra {
		dirs[path] = dir
	}
	mi := &moduleImporter{fset: fset, parsed: make(map[string]*types.Package), dirs: dirs}
	conf := types.Config{Importer: mi}
	if _, err := conf.Check(pkg, fset, []*ast.File{f}, nil); err != nil {
		t.Fatalf("generated binding does not type-check: %v", err)
	}
}
