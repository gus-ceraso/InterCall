package main

import (
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"github.com/cerasos/intercall/go/internal/syntax"
	"github.com/cerasos/intercall/go/internal/tool"
)

// This file implements the intercall-go command: the exact import and
// export grammar of SPEC.md "Commands" — operand counts, required and
// distinct targets, and the repeatable --include, --exclude, and
// --go-name flags — the package defaults, the ordered validation and
// mutation phases of SPEC.md "One-file ownership and safe replacement"
// (source validation and generated-content validation finish before
// output-directory creation; ownership checks then finish before any
// target-file creation or replacement), and the diagnostic rendering
// of SPEC.md "Diagnostics" (path:line:column: message, physical byte
// positions, normalized logical paths, sorted multi-diagnostics, and
// never a staging path).
//
// The phase order of both commands is: option and operand grammar,
// package-name resolution (a read-only preflight that never creates
// anything), source validation and generation in memory, and finally
// the artifact write, which performs its own content validation,
// output-directory creation, ownership checks, and rename replacement.
// Any error exits with status 1 after printing the phase's
// diagnostics; a successful invocation prints nothing.

// pos1 is the line 1, column 1 position of every diagnostic without a
// source span (SPEC.md "Diagnostics": "Errors without a source span
// use line 1, column 1 of the relevant operand").
var pos1 = tool.Position{Line: 1, Column: 1}

// run executes one invocation and returns the process exit status:
// 0 on success, 1 on any failure. A bare invocation prints the usage
// to stderr; --help or -h prints it to stdout.
func run(args []string, stdout, stderr io.Writer) int {
	if len(args) == 0 {
		fmt.Fprint(stderr, usageText)
		return 1
	}
	for _, a := range args {
		if a == "--help" || a == "-h" {
			fmt.Fprint(stdout, usageText)
			return 0
		}
	}

	cmd := args[0]
	var opts *options
	var operands []string
	var diags []*tool.Error
	switch cmd {
	case "export", "import":
		opts, operands, diags = parseOptions(cmd, args[1:])
		diags = append(diags, checkGrammar(cmd, opts, operands)...)
	default:
		diags = append(diags, &tool.Error{
			Filename: cmd,
			Pos:      pos1,
			Msg:      fmt.Sprintf("unknown command %q; expected export or import", cmd),
		})
	}
	if len(diags) > 0 {
		tool.SortDiagnostics(diags)
		for _, d := range diags {
			fmt.Fprintln(stderr, d.Error())
		}
		return 1
	}

	var err error
	switch cmd {
	case "import":
		err = runImport(opts, operands[0])
	case "export":
		err = runExport(opts, operands)
	}
	if err != nil {
		fmt.Fprintln(stderr, err.Error())
		return 1
	}
	return 0
}

// options is one parsed option set.
type options struct {
	outDir        string
	interfaceFile string
	packageName   string
	includes      []string
	excludes      []string
	goNames       []string
	seen          map[string]bool // non-repeatable option names already given
}

// optionSpec describes one option of the grammar.
type optionSpec struct {
	command string // the only command that may use the option; "" = both
	repeat  bool   // the option is repeatable
	apply   func(o *options, value string)
}

// optionSpecs is the exact option table of SPEC.md "Commands". The
// repeatable options are --include, --exclude, and --go-name; every
// other option may appear at most once. The --out and --interface
// values are cleaned to their logical paths, so diagnostics use the
// normalized operand (SPEC.md "Diagnostics").
var optionSpecs = map[string]optionSpec{
	"out":       {repeat: false, apply: func(o *options, v string) { o.outDir = filepath.Clean(v) }},
	"interface": {command: "export", repeat: false, apply: func(o *options, v string) { o.interfaceFile = filepath.Clean(v) }},
	"package":   {repeat: false, apply: func(o *options, v string) { o.packageName = v }},
	"include":   {command: "export", repeat: true, apply: func(o *options, v string) { o.includes = append(o.includes, v) }},
	"exclude":   {command: "export", repeat: true, apply: func(o *options, v string) { o.excludes = append(o.excludes, v) }},
	"go-name":   {command: "import", repeat: true, apply: func(o *options, v string) { o.goNames = append(o.goNames, v) }},
}

// parseOptions parses the tokens after the command word into options
// and operands, collecting every grammar diagnostic. Options precede
// the operands: after the first operand — or after the terminator "--"
// — every token is an operand. A token is an option when it starts
// with "-" and is not the single "-" token.
func parseOptions(cmd string, args []string) (*options, []string, []*tool.Error) {
	opts := &options{seen: make(map[string]bool)}
	var operands []string
	var diags []*tool.Error
	flagEnd := false
	for i := 0; i < len(args); i++ {
		a := args[i]
		if flagEnd || a == "-" || !strings.HasPrefix(a, "-") {
			operands = append(operands, a)
			flagEnd = true
			continue
		}
		if a == "--" {
			flagEnd = true
			continue
		}
		name, value, hasValue := strings.Cut(a, "=")
		name = strings.TrimPrefix(name, "--")
		spec, ok := optionSpecs[name]
		if !ok {
			diags = append(diags, &tool.Error{
				Filename: a,
				Pos:      pos1,
				Msg:      fmt.Sprintf("unknown option %q", a),
			})
			continue
		}
		if spec.command != "" && spec.command != cmd {
			// Consume the separate value of a known option even when it
			// is not valid for this command, so the value never leaks
			// into the operands.
			if !hasValue && i+1 < len(args) {
				i++
			}
			diags = append(diags, &tool.Error{
				Filename: a,
				Pos:      pos1,
				Msg:      fmt.Sprintf("option --%s is only valid with %s", name, spec.command),
			})
			continue
		}
		if !spec.repeat {
			if opts.seen[name] {
				// Consume the separate value before reporting the
				// duplicate, so it never leaks into the operands.
				if !hasValue && i+1 < len(args) {
					i++
				}
				diags = append(diags, &tool.Error{
					Filename: a,
					Pos:      pos1,
					Msg:      fmt.Sprintf("duplicate option --%s", name),
				})
				continue
			}
			opts.seen[name] = true
		}
		if !hasValue {
			if i+1 >= len(args) {
				diags = append(diags, &tool.Error{
					Filename: a,
					Pos:      pos1,
					Msg:      fmt.Sprintf("option --%s requires a value", name),
				})
				continue
			}
			i++
			value = args[i]
		}
		if value == "" {
			diags = append(diags, &tool.Error{
				Filename: a,
				Pos:      pos1,
				Msg:      fmt.Sprintf("option --%s requires a non-empty value", name),
			})
			continue
		}
		spec.apply(opts, value)
	}
	return opts, operands, diags
}

// checkGrammar validates the command-specific grammar of the parsed
// options and operands and returns every violation as a diagnostic:
// the required and distinct targets of export, the exact operand count
// of import, the package-name rule, and the --go-name flag grammar.
// Filter selectors are validated by the discovery phase with the tool's
// own grammar; a malformed filter never reaches the filesystem.
func checkGrammar(cmd string, opts *options, operands []string) []*tool.Error {
	var diags []*tool.Error
	bad := func(filename, msg string) {
		diags = append(diags, &tool.Error{Filename: filename, Pos: pos1, Msg: msg})
	}
	if !opts.seen["out"] {
		bad(cmd, fmt.Sprintf("%s requires --out DIR", cmd))
	}
	if opts.packageName != "" {
		if err := tool.ValidGoPackageName(opts.packageName); err != nil {
			bad("--package", err.Error())
		}
	}
	switch cmd {
	case "export":
		if !opts.seen["interface"] {
			bad(cmd, "export requires --interface FILE")
		}
		if len(operands) == 0 {
			bad(cmd, "export requires at least one package pattern")
		}
		if opts.outDir != "" && opts.interfaceFile != "" {
			// Export requires distinct binding and interface targets.
			// The writer enforces the same rule under the host
			// filesystem's filename equivalence; this lexical check
			// reports the obvious case in the grammar phase.
			binding := filepath.Join(filepath.Clean(opts.outDir), "binding_gen.go")
			if filepath.Clean(opts.interfaceFile) == binding {
				bad(opts.interfaceFile, "the interface target is the generated Go target binding_gen.go of the output directory; the binding and interface targets must be distinct")
			}
		}
	case "import":
		if len(operands) != 1 {
			bad(cmd, fmt.Sprintf("import requires exactly one interface file, got %d operands", len(operands)))
		}
		for _, text := range opts.goNames {
			if _, err := tool.ParseOverride(text); err != nil {
				bad("--go-name", err.Error())
			}
		}
	}
	return diags
}

// runImport executes the import phases: package-name resolution, the
// exact-byte read of the one interface file, in-memory source
// validation and generation, and the artifact write. The file operand
// is cleaned to its logical path, so every diagnostic uses the
// normalized operand.
func runImport(opts *options, file string) error {
	file = filepath.Clean(file)
	pkg, err := tool.ResolvePackageName(tool.ImportMode, opts.outDir, opts.packageName)
	if err != nil {
		return err
	}
	overrides, err := tool.ParseOverrides(opts.goNames)
	if err != nil {
		return err
	}
	src, err := os.ReadFile(file)
	if err != nil {
		return &tool.Error{
			Filename: file,
			Pos:      pos1,
			Msg:      fmt.Sprintf("reading %s: %s", file, cause(err)),
		}
	}
	goFile, body, err := tool.GenerateImport(file, src, overrides, pkg)
	if err != nil {
		return operandError(err, file)
	}
	return tool.WriteArtifacts(tool.WriteConfig{
		Mode:          tool.ImportMode,
		OutDir:        opts.outDir,
		Package:       pkg,
		GoFile:        goFile,
		InterfaceBody: body,
	})
}

// runExport executes the export phases: package-name resolution,
// discovery and selection in the active module or workspace, the
// export model with its importability checks, in-memory generation,
// and the artifact write of the binding and its owned interface.
func runExport(opts *options, patterns []string) error {
	pkg, err := tool.ResolvePackageName(tool.ExportMode, opts.outDir, opts.packageName)
	if err != nil {
		return err
	}
	res, err := tool.Discover(tool.DiscoverConfig{
		Patterns: patterns,
		Includes: opts.includes,
		Excludes: opts.excludes,
		OutDir:   opts.outDir,
	})
	if err != nil {
		return err
	}
	model, err := tool.MapExport(res, res.OutPath)
	if err != nil {
		return err
	}
	goFile, body, err := tool.GenerateExport(model, pkg)
	if err != nil {
		return err
	}
	return tool.WriteArtifacts(tool.WriteConfig{
		Mode:          tool.ExportMode,
		OutDir:        opts.outDir,
		Package:       pkg,
		InterfacePath: opts.interfaceFile,
		GoFile:        goFile,
		InterfaceBody: body,
	})
}

// operandError renders one phase error without a source span at line
// 1, column 1 of the relevant operand (SPEC.md "Diagnostics"). A
// tool or syntax diagnostic already carries its exact physical
// position and passes through unchanged.
func operandError(err error, filename string) error {
	var te *tool.Error
	if errors.As(err, &te) {
		return err
	}
	var se *syntax.Error
	if errors.As(err, &se) {
		return err
	}
	return &tool.Error{Filename: filename, Pos: pos1, Msg: err.Error()}
}

// cause strips the operand paths from one filesystem error so a
// diagnostic carries only the underlying cause: a staging failure
// never reports a staging path, and no diagnostic embeds a resolved
// physical directory (SPEC.md "Diagnostics").
func cause(err error) string {
	var pe *fs.PathError
	if errors.As(err, &pe) && pe.Err != nil {
		return pe.Err.Error()
	}
	var le *os.LinkError
	if errors.As(err, &le) && le.Err != nil {
		return le.Err.Error()
	}
	return err.Error()
}
