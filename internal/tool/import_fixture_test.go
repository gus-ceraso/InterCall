package tool

import (
	"bytes"
	"go/ast"
	"go/importer"
	"go/parser"
	"go/token"
	"go/types"
	"os"
	"path/filepath"
	"strings"
	"testing"

	_ "embed"

	"github.com/cerasos/intercall/internal/syntax"
)

// importFixtureSource is the exact interface of the compiled import
// fixture. The fixture generation entry, determinism test, and fixture
// compile test all share these bytes, so the checked-in fixture file can
// never drift from the generator.
//
//go:embed testdata/import/import.intercall
var importFixtureSource []byte

// importFixturePackage is the package name of the compiled import
// fixture.
const importFixturePackage = "importfixture"

// importFixtureGenPath is the checked-in generated fixture, relative to
// the package directory that go test runs in.
const importFixtureGenPath = "importfixture/binding_gen.go"

// generateImportFixture parses and validates the embedded fixture
// interface and renders the complete generated binding Go file with no
// overrides, together with its byte-exact canonical interface body. The
// result is the byte-exact content behind the ownership lines of
// internal/tool/importfixture/binding_gen.go.
func generateImportFixture() (goFile []byte, body []byte, err error) {
	return GenerateImport("import.intercall", importFixtureSource, nil, importFixturePackage)
}

// composeImportBinding assembles one complete stamped import binding
// file exactly as the artifact layer does: the standard generated-file
// marker, the import ownership line with the artifact stamp of the
// canonical body, one blank line, and the generated binding bytes.
func composeImportBinding(body, goFile []byte) []byte {
	var b bytes.Buffer
	b.WriteString(intercallGeneratedMarker)
	b.WriteByte('\n')
	b.WriteString(bindingOwnershipPrefix)
	b.WriteString(ImportMode.String())
	b.WriteString(" sha256:")
	b.WriteString(ArtifactStamp(body))
	b.WriteByte('\n')
	b.WriteByte('\n')
	b.Write(goFile)
	return b.Bytes()
}

// TestImportGeneratedFixtureCompiles regenerates the import fixture
// through the complete artifact pipeline into a temporary directory,
// verifies the checked-in fixture file is byte-identical to the staged
// artifact, and type-checks the regenerated source against the standard
// library and the module root runtime package. Validation never rewrites
// the checked-in fixture.
//
// The fixture package also compiles as a real dependency of this test
// binary, so a generated source that does not compile fails here before
// any runtime scenario can run.
func TestImportGeneratedFixtureCompiles(t *testing.T) {
	goFile, body, err := generateImportFixture()
	if err != nil {
		t.Fatalf("generateImportFixture: %v", err)
	}
	dir := t.TempDir()
	if err := WriteArtifacts(WriteConfig{
		Mode:          ImportMode,
		OutDir:        dir,
		Package:       importFixturePackage,
		GoFile:        goFile,
		InterfaceBody: body,
	}); err != nil {
		t.Fatalf("WriteArtifacts: %v", err)
	}
	written, err := os.ReadFile(filepath.Join(dir, bindingFile))
	if err != nil {
		t.Fatalf("reading the staged binding: %v", err)
	}
	checked, err := os.ReadFile(importFixtureGenPath)
	if err != nil {
		t.Fatalf("reading %s: %v", importFixtureGenPath, err)
	}
	if !bytes.Equal(written, checked) {
		t.Fatalf("%s is stale: the staged artifact (%d bytes) differs from the checked-in file (%d bytes); regenerate it from the emitter", importFixtureGenPath, len(written), len(checked))
	}
	typeCheckImportBinding(t, written)
}

// moduleImporter resolves the module root runtime package from source
// and every other import through the standard library, so a regenerated
// binding can be type-checked in memory without a module build. Every
// resolved package is cached, because go/types treats two separately
// imported package instances as distinct packages: the fixture's
// context.Context must be the very same types.Package instance the root
// runtime package used.
type moduleImporter struct {
	fset   *token.FileSet
	parsed map[string]*types.Package
	dirs   map[string]string // import path -> source directory, defaulting to moduleSourceDirs
}

// moduleSourceDirs maps the module packages the in-memory binding type
// checks load from source to their directories relative to the package
// directory that go test runs in: the module root runtime package and
// the export provider fixture package.
var moduleSourceDirs = map[string]string{
	"github.com/cerasos/intercall":                                  filepath.Join("..", ".."),
	"github.com/cerasos/intercall/internal/tool/exportfixture/prov": filepath.Join("exportfixture", "prov"),
}

// Import implements types.Importer.
func (mi *moduleImporter) Import(path string) (*types.Package, error) {
	if mi.dirs == nil {
		mi.dirs = moduleSourceDirs
	}
	if pkg := mi.parsed[path]; pkg != nil {
		return pkg, nil
	}
	dir, ok := mi.dirs[path]
	if !ok {
		pkg, err := importer.Default().Import(path)
		if err != nil {
			return nil, err
		}
		mi.parsed[path] = pkg
		return pkg, nil
	}
	files, err := filepath.Glob(filepath.Join(dir, "*.go"))
	if err != nil {
		return nil, err
	}
	var afs []*ast.File
	for _, name := range files {
		if strings.HasSuffix(name, "_test.go") {
			continue
		}
		src, err := os.ReadFile(name)
		if err != nil {
			return nil, err
		}
		af, err := parser.ParseFile(mi.fset, name, src, parser.AllErrors)
		if err != nil {
			return nil, err
		}
		afs = append(afs, af)
	}
	conf := types.Config{Importer: mi}
	pkg, err := conf.Check(path, mi.fset, afs, nil)
	if err != nil {
		return nil, err
	}
	mi.parsed[path] = pkg
	return pkg, nil
}

// typeCheckImportBinding parses and type-checks one complete import
// binding source against the standard library and the module root
// runtime package, verifying it is valid Go independent of the compiled
// fixture package.
func typeCheckImportBinding(t *testing.T, src []byte) {
	t.Helper()
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, importFixtureGenPath, src, parser.AllErrors)
	if err != nil {
		t.Fatalf("generated import binding does not parse: %v", err)
	}
	mi := &moduleImporter{fset: fset, parsed: make(map[string]*types.Package)}
	conf := types.Config{Importer: mi}
	if _, err := conf.Check(importFixturePackage, fset, []*ast.File{f}, nil); err != nil {
		t.Fatalf("generated import binding does not type-check: %v", err)
	}
}

// canonicalBodyOf renders the canonical interface body of one interface
// source: parse, attach documentation, validate, and format, exactly the
// pipeline GenerateImport uses for the semantic constant.
func canonicalBodyOf(t *testing.T, name string, src []byte) []byte {
	t.Helper()
	f, err := syntax.Parse(name, src)
	if err != nil {
		t.Fatalf("syntax.Parse: %v", err)
	}
	syntax.AttachDocs(f)
	if err := syntax.Validate(f); err != nil {
		t.Fatalf("syntax.Validate: %v", err)
	}
	return syntax.Format(f)
}
