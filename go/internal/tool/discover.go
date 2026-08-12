package tool

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strconv"
	"strings"

	"golang.org/x/tools/go/packages"
)

// This file implements SPEC.md "Package discovery and selection": export
// operands are standard Go package patterns interpreted in the active
// module or workspace and active Go build configuration, explicit
// packages are deduplicated by canonical import path, patterns that
// match no package are errors, and every explicit package must
// type-check and be an importable non-main package. Only discovery code
// uses golang.org/x/tools/go/packages, as AGENTS.md allows.

// discoverMode is the go/packages load mode of one discovery load: the
// complete syntax, types, and import graph of every matched package and
// its dependencies. The import graph is required for the output-package
// import-cycle check, and the syntax and type information feed procedure
// selection and the later value-mapping phases. Test files and test
// variants are never requested, so they are excluded; the active build
// configuration (build tags, GOOS, GOARCH, module and workspace mode)
// comes from cfg.Dir and cfg.Env.
const discoverMode = packages.NeedName |
	packages.NeedFiles |
	packages.NeedCompiledGoFiles |
	packages.NeedImports |
	packages.NeedDeps |
	packages.NeedTypes |
	packages.NeedSyntax |
	packages.NeedTypesInfo

// outputMode is the light load mode of the output-directory resolution:
// only the identity and directory of the package the output directory
// resolves to.
const outputMode = packages.NeedName | packages.NeedFiles

// DiscoverConfig configures one package discovery and procedure
// selection pass.
//
// Dir is the working directory whose module or workspace is active; the
// empty string uses the process working directory. Env is the complete
// environment of the go command; the nil value uses os.Environ with the
// Go toolchain's bin directory prepended to PATH when PATH cannot find a
// go executable. Patterns are the export operands; Includes and Excludes
// are the --include and --exclude filter values in flag order. OutDir is
// the export output directory: it must resolve to an importable package
// in the active module or workspace, or be a fresh directory — one that
// does not yet exist or exists without Go files — whose to-be-created
// path can belong to the active module or workspace; the artifact write
// then creates it if necessary.
type DiscoverConfig struct {
	Dir      string
	Env      []string
	Patterns []string
	Includes []string
	Excludes []string
	OutDir   string
}

// DiscoverResult is the outcome of one discovery pass: the explicit
// packages in canonical-path order, the selected providers in
// package-path then symbol order, and the canonical import path of
// the resolved export output package.
//
// OutPath is the import path of the package the export output
// directory resolves to: the import path of the resolved package, or
// the import path a fresh output directory would have under Go's
// nearest-go.mod resolution rule. It is always non-empty on success:
// export requires an output directory, and the directory must resolve
// to an importable package in the active module or workspace, or be a
// fresh directory the active module or workspace can contain. The
// command passes it to MapExport so the model's importability checks
// run against the actual output package.
type DiscoverResult struct {
	Packages  []*ExplicitPackage
	Providers []*Provider
	OutPath   string
}

// ExplicitPackage is one package directly matched by an export operand.
//
// Path is the canonical import path, Name the package name, and Dir the
// absolute package directory. The unexported fields retain the complete
// load record and the parsed source documents of the compiled files,
// which later phases use for value mapping and documentation. The
// package is deduplicated by canonical import path, so two operands that
// match the same package yield one ExplicitPackage.
type ExplicitPackage struct {
	Path string
	Name string
	Dir  string

	files []string             // CompiledGoFiles, absolute, in package order
	pkg   *packages.Package    // the load record: Types, TypesInfo, Syntax, Fset
	docs  map[string]*Document // absolute file path -> parsed document
}

// Discover loads the explicit packages of the export operands, checks
// them, selects the providers, and validates the output directory, in a
// deterministic order: operand and filter grammar first, then load and
// package diagnostics, then documents, filter resolution, procedure
// signatures, and finally output-package importability.
//
// A diagnostic is an *Error. Pattern and filter diagnostics use the
// operand text as their path and line 1, column 1; package diagnostics
// use the canonical import path plus the package-relative file path;
// load-level failures are returned as ordinary errors.
func Discover(cfg DiscoverConfig) (*DiscoverResult, error) {
	if err := validateOperands(cfg.Patterns); err != nil {
		return nil, err
	}
	inc, err := parseSelectors(cfg.Includes)
	if err != nil {
		return nil, err
	}
	exc, err := parseSelectors(cfg.Excludes)
	if err != nil {
		return nil, err
	}

	dir := cfg.Dir
	if dir == "" {
		var err error
		dir, err = os.Getwd()
		if err != nil {
			return nil, fmt.Errorf("determining working directory: %v", err)
		}
	}
	env := buildEnv(cfg.Env)

	pkgs, err := loadPackages(dir, env, cfg.Patterns...)
	if err != nil {
		return nil, fmt.Errorf("loading export packages: %v", err)
	}
	if len(pkgs) == 0 {
		// The go command reports unmatched patterns as error packages,
		// but a wildcard that matches nothing in an empty module yields
		// no packages at all; either way a pattern that matches no
		// package is an error.
		return nil, &Error{
			Pos: Position{Line: 1, Column: 1},
			Msg: "no package matched any export pattern; every pattern must match at least one package",
		}
	}
	explicit, err := collectExplicit(pkgs)
	if err != nil {
		return nil, err
	}

	// External compiler-generated files of one package must have
	// distinct base names, or their canonical logical paths would be
	// silently conflated (SPEC.md "Diagnostics"). This package-load
	// invariant is checked before any other package diagnostic.
	for _, p := range explicit {
		if err := checkExternalBases(p.Path, p.Dir, p.files); err != nil {
			return nil, err
		}
	}

	// Every explicit package must type-check. Package diagnostics from
	// go list and from type checking are reported in deterministic
	// order before any later phase.
	var diags []*Error
	for _, p := range explicit {
		for _, e := range p.pkg.Errors {
			diags = append(diags, packageError(p, e))
		}
	}
	if err := firstError(diags); err != nil {
		return nil, err
	}

	// Every explicit package must be importable and non-main.
	for _, p := range explicit {
		if p.Name == "main" {
			return nil, &Error{
				Filename: p.Path,
				Pos:      Position{Line: 1, Column: 1},
				Msg:      fmt.Sprintf("explicit package %q is main and is not importable", p.Path),
			}
		}
		if len(p.files) == 0 {
			return nil, &Error{
				Filename: p.Path,
				Pos:      Position{Line: 1, Column: 1},
				Msg:      fmt.Sprintf("explicit package %q has no Go files and is not importable", p.Path),
			}
		}
	}

	// Parse the source documents of every compiled file. Directive and
	// documentation diagnostics are reported per package in source
	// order, earliest first.
	for _, p := range explicit {
		if err := p.parseDocuments(); err != nil {
			return nil, err
		}
	}

	selected, err := selectProviders(explicit, inc, exc)
	if err != nil {
		return nil, err
	}

	outPath, err := checkOutputPackage(cfg, dir, env, selected)
	if err != nil {
		return nil, err
	}

	return &DiscoverResult{Packages: explicit, Providers: selected, OutPath: outPath}, nil
}

// validateOperands checks the export operands: at least one pattern is
// required, and file operands and go/packages query patterns are not
// supported (SPEC.md "Package discovery and selection": file operands
// and an implicit module-wide scan are not supported).
func validateOperands(patterns []string) error {
	if len(patterns) == 0 {
		return &Error{
			Pos: Position{Line: 1, Column: 1},
			Msg: "export requires at least one package pattern",
		}
	}
	for _, pattern := range patterns {
		if pattern == "" {
			return &Error{
				Filename: pattern,
				Pos:      Position{Line: 1, Column: 1},
				Msg:      "empty package pattern",
			}
		}
		if strings.HasSuffix(pattern, ".go") {
			return &Error{
				Filename: pattern,
				Pos:      Position{Line: 1, Column: 1},
				Msg:      fmt.Sprintf("pattern %q: file operands are not supported", pattern),
			}
		}
		if strings.HasPrefix(pattern, "file=") {
			return &Error{
				Filename: pattern,
				Pos:      Position{Line: 1, Column: 1},
				Msg:      fmt.Sprintf("pattern %q: file operands are not supported", pattern),
			}
		}
		if strings.HasPrefix(pattern, "pattern=") {
			return &Error{
				Filename: pattern,
				Pos:      Position{Line: 1, Column: 1},
				Msg:      fmt.Sprintf("pattern %q: query patterns are not supported", pattern),
			}
		}
	}
	return nil
}

// buildEnv returns the complete environment of the go command. When env
// is nil, os.Environ is used, and the Go toolchain's bin directory is
// prepended to PATH when PATH cannot find a go executable, so the
// discovery driver always finds the toolchain's go command.
func buildEnv(env []string) []string {
	if env == nil {
		env = os.Environ()
	}
	if findInPath(env, "go") != "" {
		return env
	}
	bin := filepath.Join(runtime.GOROOT(), "bin")
	if info, err := os.Stat(filepath.Join(bin, "go")); err != nil || info.IsDir() {
		return env
	}
	return withPathPrefix(env, bin)
}

// findInPath reports the first existing regular file named name in the
// PATH of env, or "" when PATH does not contain one. Only the last PATH
// value of env is used, following os/exec.
func findInPath(env []string, name string) string {
	path := ""
	for _, kv := range env {
		if k, v, ok := strings.Cut(kv, "="); ok && k == "PATH" {
			path = v
		}
	}
	for _, dir := range filepath.SplitList(path) {
		if dir == "" {
			continue
		}
		cand := filepath.Join(dir, name)
		if info, err := os.Stat(cand); err == nil && !info.IsDir() {
			return cand
		}
	}
	return ""
}

// withPathPrefix returns env with dir prepended to its PATH value.
func withPathPrefix(env []string, dir string) []string {
	out := make([]string, 0, len(env)+1)
	found := false
	for _, kv := range env {
		if k, v, ok := strings.Cut(kv, "="); ok && k == "PATH" {
			out = append(out, "PATH="+dir+string(os.PathListSeparator)+v)
			found = true
			continue
		}
		out = append(out, kv)
	}
	if !found {
		out = append(out, "PATH="+dir)
	}
	return out
}

// loadPackages runs one go/packages load of the export operands in the
// active module or workspace of dir.
func loadPackages(dir string, env []string, patterns ...string) ([]*packages.Package, error) {
	cfg := &packages.Config{
		Mode: discoverMode,
		Dir:  dir,
		Env:  env,
	}
	return packages.Load(cfg, patterns...)
}

// collectExplicit deduplicates the loaded packages by canonical import
// path and returns them in canonical-path order. The packages matched by
// the operands are exactly the roots of the load; dependencies appear
// only inside their Imports graph and are not explicit.
func collectExplicit(pkgs []*packages.Package) ([]*ExplicitPackage, error) {
	seen := make(map[string]bool, len(pkgs))
	var explicit []*ExplicitPackage
	for _, pkg := range pkgs {
		path := pkg.PkgPath
		if path == "" {
			path = pkg.ID
		}
		if path == "" {
			return nil, fmt.Errorf("loaded package has no import path")
		}
		if seen[path] {
			continue
		}
		seen[path] = true
		explicit = append(explicit, &ExplicitPackage{
			Path:  path,
			Name:  pkg.Name,
			Dir:   pkg.Dir,
			files: pkg.CompiledGoFiles,
			pkg:   pkg,
		})
	}
	sort.Slice(explicit, func(i, j int) bool { return explicit[i].Path < explicit[j].Path })
	return explicit, nil
}

// packageError converts one go/packages diagnostic into an *Error. A
// positioned diagnostic uses the canonical logical path of its file; a
// diagnostic without a position uses the package path (the raw operand
// text for an unmatched pattern) at line 1, column 1.
func packageError(p *ExplicitPackage, e packages.Error) *Error {
	pos := Position{Line: 1, Column: 1}
	filename := p.Path
	if e.Pos != "" {
		if file, line, col, ok := splitFilePos(e.Pos); ok {
			filename = logicalFilePath(p.Path, p.Dir, file)
			pos = Position{Line: line, Column: col}
		}
	}
	return &Error{Filename: filename, Pos: pos, Msg: e.Msg}
}

// splitFilePos parses a go/token position string of the form
// "file:line" or "file:line:column", scanning numeric suffixes from
// the right so colons in filenames are preserved, and defaults a
// missing column to 1 (SPEC.md "Diagnostics").
func splitFilePos(pos string) (file string, line, col int, ok bool) {
	i := strings.LastIndexByte(pos, ':')
	if i < 0 {
		return "", 0, 0, false
	}
	col, err := strconv.Atoi(pos[i+1:])
	if err != nil {
		return "", 0, 0, false
	}
	if j := strings.LastIndexByte(pos[:i], ':'); j >= 0 {
		if ln, err := strconv.Atoi(pos[j+1 : i]); err == nil {
			// The "file:line:column" form: the suffix before the last
			// colon is the line and the rest is the file.
			file = pos[:j]
			if file == "" {
				return "", 0, 0, false
			}
			return file, ln, col, true
		}
	}
	// The "file:line" form: the last suffix is the line and the column
	// defaults to 1.
	file = pos[:i]
	if file == "" {
		return "", 0, 0, false
	}
	return file, col, 1, true
}

// logicalFilePath renders the canonical logical path of one compiled
// file of a package (SPEC.md "Diagnostics"): a physical source under
// the package directory uses the slash-normalized package-relative
// path under the package's canonical import path, and an external
// compiler-generated source uses
// "<import-path>/.intercall-generated/<base-name>". A relative file
// name is resolved against the package directory first.
func logicalFilePath(pkgPath, pkgDir, file string) string {
	if !filepath.IsAbs(file) {
		file = filepath.Join(pkgDir, file)
	}
	if underPackageDir(pkgDir, file) {
		rel, err := filepath.Rel(filepath.Clean(pkgDir), file)
		if err == nil {
			return pkgPath + "/" + filepath.ToSlash(rel)
		}
	}
	return pkgPath + "/.intercall-generated/" + filepath.Base(file)
}

// underPackageDir reports whether a physical file lies under the
// package directory.
func underPackageDir(pkgDir, file string) bool {
	rel, err := filepath.Rel(filepath.Clean(pkgDir), file)
	return err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))
}

// checkExternalBases reports the package-load invariant error of two
// external compiler-generated files of one package that share a base
// name: their canonical logical paths would be silently conflated
// (SPEC.md "Diagnostics").
func checkExternalBases(pkgPath, pkgDir string, files []string) error {
	seen := make(map[string]string)
	for _, file := range files {
		if !filepath.IsAbs(file) {
			file = filepath.Join(pkgDir, file)
		}
		if underPackageDir(pkgDir, file) {
			continue // a physical source under the package directory
		}
		base := filepath.Base(file)
		if prev, ok := seen[base]; ok {
			return &Error{
				Filename: pkgPath,
				Pos:      Position{Line: 1, Column: 1},
				Msg:      fmt.Sprintf("package-load invariant error: external generated files %q and %q of package %q share the base name %q, which would conflate their logical paths", prev, file, pkgPath, base),
			}
		}
		seen[base] = file
	}
	return nil
}

// parseDocuments parses every compiled file of one explicit package with
// ParseGoSource and stores the document under its absolute path.
// Directive and documentation diagnostics are reported in source order,
// earliest first.
func (p *ExplicitPackage) parseDocuments() error {
	docs := make(map[string]*Document, len(p.files))
	var diags []*Error
	for _, file := range p.files {
		src, err := os.ReadFile(file)
		if err != nil {
			return fmt.Errorf("reading %s: %v", file, err)
		}
		doc, err := ParseGoSource(logicalFilePath(p.Path, p.Dir, file), src)
		if err != nil {
			if ge, ok := err.(*Error); ok {
				diags = append(diags, ge)
				continue
			}
			return err
		}
		docs[file] = doc
	}
	if err := firstError(diags); err != nil {
		return err
	}
	p.docs = docs
	return nil
}

// checkOutputPackage validates the export output directory and returns
// its canonical import path: the import path of the resolved package,
// or the import path a fresh output directory would have. The
// directory must resolve to an importable package in the active module
// or workspace, or be a fresh directory — one that does not yet exist
// or exists without Go files — that can belong to the active module or
// workspace (SPEC.md "One-file ownership and safe replacement": the
// artifact write creates --out if necessary). The resolution runs the
// go command from the discovery directory, so a directory outside the
// active module or workspace does not resolve and is an error; a fresh
// directory is accepted only when its to-be-created path falls under
// the active module or one of the workspace modules, and the
// importability checks then run against the import path the directory
// would have.
func checkOutputPackage(cfg DiscoverConfig, dir string, env []string, providers []*Provider) (string, error) {
	if cfg.OutDir == "" {
		return "", &Error{
			Filename: cfg.OutDir,
			Pos:      Position{Line: 1, Column: 1},
			Msg:      "export requires an output directory",
		}
	}
	abs := cfg.OutDir
	if !filepath.IsAbs(abs) {
		abs = filepath.Join(dir, abs)
	}
	abs, err := filepath.Abs(abs)
	if err != nil {
		return "", fmt.Errorf("resolving output directory %q: %v", cfg.OutDir, err)
	}

	pkgs, err := packages.Load(&packages.Config{
		Mode: outputMode,
		Dir:  dir,
		Env:  env,
	}, abs)
	if err != nil {
		return "", fmt.Errorf("resolving output directory %q: %v", cfg.OutDir, err)
	}
	if len(pkgs) == 0 {
		return "", &Error{
			Filename: cfg.OutDir,
			Pos:      Position{Line: 1, Column: 1},
			Msg:      fmt.Sprintf("output directory %q does not resolve to a package in the active module or workspace", cfg.OutDir),
		}
	}
	out := pkgs[0]
	if len(out.Errors) > 0 {
		// A fresh directory does not resolve to a package yet: the
		// artifact write creates --out if necessary, so discovery
		// accepts it when the to-be-created path can belong to the
		// active module or workspace. Every other unresolved directory
		// keeps the package-resolution diagnostic.
		if outPath, ok := freshOutputPath(dir, env, abs); ok {
			if err := checkOutputProviders(cfg, outPath, providers); err != nil {
				return "", err
			}
			return outPath, nil
		}
		return "", &Error{
			Filename: cfg.OutDir,
			Pos:      Position{Line: 1, Column: 1},
			Msg:      fmt.Sprintf("output directory %q: %s", cfg.OutDir, out.Errors[0].Msg),
		}
	}
	if out.Name == "main" {
		return "", &Error{
			Filename: cfg.OutDir,
			Pos:      Position{Line: 1, Column: 1},
			Msg:      fmt.Sprintf("output directory %q resolves to the main package and is not importable", cfg.OutDir),
		}
	}
	if len(out.GoFiles) == 0 {
		return "", &Error{
			Filename: cfg.OutDir,
			Pos:      Position{Line: 1, Column: 1},
			Msg:      fmt.Sprintf("output directory %q has no Go files and is not importable", cfg.OutDir),
		}
	}

	if err := checkOutputProviders(cfg, out.PkgPath, providers); err != nil {
		return "", err
	}
	if out.PkgPath == "" {
		return "", fmt.Errorf("internal error: the resolved output package %q has no import path", cfg.OutDir)
	}
	return out.PkgPath, nil
}

// checkOutputProviders verifies that a binding in the output package
// outPath can import every selected provider: no selected procedure may
// live in the output package, no provider package may import the output
// package (which would form an import cycle), and every provider
// package must be internal-visible from the output package.
func checkOutputProviders(cfg DiscoverConfig, outPath string, providers []*Provider) error {
	for _, p := range providers {
		qualified := p.Pkg.Path + "." + p.Name
		if p.Pkg.Path == outPath {
			return &Error{
				Filename: cfg.OutDir,
				Pos:      Position{Line: 1, Column: 1},
				Msg:      fmt.Sprintf("selected procedure %q is in the output package: the generated binding would import its own package", qualified),
			}
		}
		if importsPath(outPath, p.Pkg.pkg) {
			return &Error{
				Filename: cfg.OutDir,
				Pos:      Position{Line: 1, Column: 1},
				Msg:      fmt.Sprintf("selected procedure %q: provider package %q imports the output package %q, which would form an import cycle", qualified, p.Pkg.Path, outPath),
			}
		}
		if !internalVisible(outPath, p.Pkg.Path) {
			return &Error{
				Filename: cfg.OutDir,
				Pos:      Position{Line: 1, Column: 1},
				Msg:      fmt.Sprintf("selected procedure %q: provider package %q is internal and not visible from the output package %q", qualified, p.Pkg.Path, outPath),
			}
		}
	}
	return nil
}

// freshOutputPath returns the import path a fresh output directory
// would have in the active module or workspace. ok is false unless the
// directory is fresh — it does not exist, or exists as a directory
// containing no Go file — and its to-be-created path can belong to the
// active module or workspace under Go's nearest-go.mod resolution rule;
// the caller then keeps the package-resolution diagnostic.
func freshOutputPath(dir string, env []string, outDir string) (path string, ok bool) {
	fresh, err := freshOutDir(outDir)
	if err != nil || !fresh {
		return "", false
	}
	roots, err := activeModuleRoots(dir, env)
	if err != nil {
		return "", false
	}
	goMod := nearestGoMod(outDir)
	if goMod == "" {
		return "", false
	}
	for root, prefix := range roots {
		if filepath.Join(root, "go.mod") != goMod {
			continue
		}
		rel, err := filepath.Rel(root, outDir)
		if err != nil {
			return "", false
		}
		if rel == "." {
			return prefix, true
		}
		return prefix + "/" + filepath.ToSlash(rel), true
	}
	return "", false
}

// freshOutDir reports whether outDir is a fresh export output
// directory: the path does not exist, or it exists as a directory whose
// entries contain no Go filename. A path that exists and is not a
// directory is not fresh, and a directory whose state cannot be
// inspected is an error.
func freshOutDir(outDir string) (fresh bool, err error) {
	info, err := os.Stat(outDir)
	if err != nil {
		if os.IsNotExist(err) {
			return true, nil
		}
		return false, err
	}
	if !info.IsDir() {
		return false, nil
	}
	entries, err := os.ReadDir(outDir)
	if err != nil {
		return false, err
	}
	for _, e := range entries {
		if strings.HasSuffix(e.Name(), ".go") {
			return false, nil
		}
	}
	return true, nil
}

// activeModuleRoots returns the module root directories of the active
// module or workspace mapped to their import paths, as the go command
// reports them from dir: the main module in module mode, and every
// workspace module in workspace mode. Only the roots of the active
// module or workspace can contain a fresh output directory.
func activeModuleRoots(dir string, env []string) (map[string]string, error) {
	cmd := exec.Command(goTool(env), "list", "-m", "-json")
	cmd.Dir = dir
	cmd.Env = env
	out, err := cmd.Output()
	if err != nil {
		return nil, err
	}
	roots := make(map[string]string)
	for dec := json.NewDecoder(bytes.NewReader(out)); dec.More(); {
		var mod struct {
			Path string
			Dir  string
			Main bool
		}
		if err := dec.Decode(&mod); err != nil {
			return nil, err
		}
		if !mod.Main || mod.Dir == "" || mod.Path == "" {
			continue
		}
		root, err := filepath.Abs(mod.Dir)
		if err != nil {
			return nil, err
		}
		roots[root] = mod.Path
	}
	return roots, nil
}

// goTool returns the go executable of env's PATH, or "go" when the
// PATH has none; buildEnv guarantees the caller's environment resolves
// the toolchain's go command, so the subprocess runs the same toolchain
// as the package loads.
func goTool(env []string) string {
	if p := findInPath(env, "go"); p != "" {
		return p
	}
	return "go"
}

// nearestGoMod returns the path of the nearest go.mod walking up from
// dir — the go.mod that governs a directory at dir under Go's module
// resolution — or "" when no go.mod exists above it. dir itself need
// not exist; the walk reaches the deepest existing ancestor.
func nearestGoMod(dir string) string {
	d := filepath.Clean(dir)
	for {
		cand := filepath.Join(d, "go.mod")
		if info, err := os.Stat(cand); err == nil && !info.IsDir() {
			return cand
		}
		parent := filepath.Dir(d)
		if parent == d {
			return ""
		}
		d = parent
	}
}

// importsPath reports whether the transitive import closure of pkg
// contains path.
func importsPath(path string, pkg *packages.Package) bool {
	seen := make(map[string]bool)
	var visit func(*packages.Package) bool
	visit = func(p *packages.Package) bool {
		for _, dep := range p.Imports {
			if dep.PkgPath == path {
				return true
			}
			if seen[dep.PkgPath] {
				continue
			}
			seen[dep.PkgPath] = true
			if dep.Imports != nil && visit(dep) {
				return true
			}
		}
		return false
	}
	return visit(pkg)
}

// internalVisible reports whether the package at importerPath may import
// the package at pkgPath under Go's internal rule: an import of a path
// containing the element "internal" is allowed only from within the tree
// rooted at the parent of the internal directory. The innermost internal
// element is the most restrictive and therefore the only one that
// matters.
func internalVisible(importerPath, pkgPath string) bool {
	i := strings.LastIndex(pkgPath, "/internal/")
	if i < 0 && strings.HasSuffix(pkgPath, "/internal") {
		i = len(pkgPath) - len("/internal")
	}
	if i < 0 {
		return true
	}
	root := pkgPath[:i]
	return importerPath == root || strings.HasPrefix(importerPath, root+"/")
}
