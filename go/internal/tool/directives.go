package tool

import (
	"fmt"
	"go/ast"
	"strings"
	"unicode"
	"unicode/utf8"
)

// This file implements SPEC.md "Source directives and Go documentation":
// InterCall directives occupy complete logical lines of a declaration's
// doc comment after comment markers and surrounding whitespace are
// removed. The logical lines are exactly those of
// go/ast.CommentGroup.Text(): comment markers are removed, the first
// space of a line comment is dropped, Go comment directives ("//line ",
// "//go:name", ...) are dropped, trailing whitespace is stripped from
// each line, leading blank lines are removed, and runs of interior blank
// lines collapse to one. Each logical line keeps the physical byte
// offset of its first byte so diagnostics never depend on //line
// adjustments.
//
// Directive lines are then removed and the retained lines are normalized
// with the same function as interface documentation (SPEC.md "Semantic
// documentation"): trailing spaces and tabs are removed, leading and
// trailing blank lines are removed, the longest spaces-and-tabs prefix
// shared by all nonblank lines is removed, and the lines are joined with
// LF. A retained string containing "*/" is an error because InterCall
// has no escape for its block comment terminator.

// docTarget describes the declaration a doc comment belongs to, which
// decides the placement, contradiction, and resolution checks.
type docTarget struct {
	field    bool // a struct field doc: no directives apply
	kind     GoDeclKind
	name     string     // the declaration's first name
	names    []string   // every variable name of a var or const spec
	typeInfo GoTypeInfo // the syntactic facts of a type declaration
	params   []string   // every parameter name of a function; unnamed parameters are ""
	results  int        // number of result type positions of a function

	// groupSpecs is the number of specifications of the declaration
	// group whose doc comment this target carries; zero means the doc
	// comment belongs to the declaration itself, and one means a
	// single-spec group. A group-level declaration directive on a group
	// with more than one specification applies to more than one
	// declared object and is contradictory (SPEC.md "Source directives
	// and Go documentation").
	groupSpecs int
}

// docLine is one logical line of a doc comment with the physical byte
// offset of its first byte in the source file.
type docLine struct {
	text   string
	offset int
}

// processDoc parses one declaration's doc comment: it extracts the
// logical lines, applies the directive grammar with the placement,
// contradiction, duplicate, and resolution checks for the target, and
// normalizes the retained documentation. Every diagnostic is appended to
// errs. The returned GoDoc is complete even when errors were collected.
//
// A struct field doc has no directive grammar: every line is retained as
// prose, and only the "*/" terminator rejection applies.
func processDoc(group *ast.CommentGroup, d *Document, errs *[]*Error, target docTarget) *GoDoc {
	lines := commentLines(group, d)
	gd := &GoDoc{}
	removed := make([]bool, len(lines))
	if !target.field {
		for i, ln := range lines {
			dir, err := parseDirectiveLine(ln, d)
			if err != nil {
				*errs = append(*errs, err)
				continue
			}
			if dir == nil {
				continue // prose or blank line
			}
			removed[i] = true
			gd.Directives = append(gd.Directives, *dir)
		}
		checkDirectives(gd, d, target, errs)
	}

	// Retained documentation: the surviving lines, then the "*/"
	// terminator rejection, then normalization.
	for i, ln := range lines {
		if removed[i] {
			continue
		}
		if idx := strings.Index(ln.text, "*/"); idx >= 0 {
			*errs = append(*errs, &Error{
				Filename: d.Name,
				Pos:      d.Position(ln.offset + idx),
				Msg:      "retained documentation contains '*/'",
			})
		}
		gd.Retained = joinLines(gd.Retained, ln.text)
	}
	gd.Retained = normalizeDoc(gd.Retained)
	return gd
}

// commentLines converts one comment group into logical lines with
// physical offsets, replicating go/ast.CommentGroup.Text() exactly: the
// per-comment marker and directive handling, the per-line trailing
// whitespace strip, the leading blank removal, and the collapse of
// interior blank runs. The trailing newline Text() adds is not a line.
func commentLines(group *ast.CommentGroup, d *Document) []docLine {
	var lines []docLine
	for _, c := range group.List {
		raw := c.Text
		base := d.offset(c.Pos())
		switch {
		case strings.HasPrefix(raw, "//"):
			content := raw[2:]
			off := base + 2
			if content == "" {
				lines = append(lines, docLine{"", off})
				continue
			}
			if content[0] == ' ' {
				content = content[1:]
				off++
			} else if isCommentDirective(content) {
				continue // dropped like go/ast.CommentGroup.Text
			}
			lines = append(lines, docLine{strings.TrimRight(content, " \t\r"), off})
		default:
			// Block comment: the body between "/*" and "*/".
			body := raw[2 : len(raw)-2]
			start := 0
			off := base + 2
			for i := 0; i <= len(body); i++ {
				if i == len(body) || body[i] == '\n' {
					lines = append(lines, docLine{strings.TrimRight(body[start:i], " \t\r"), off})
					off += i - start + 1
					start = i + 1
				}
			}
		}
	}
	// Remove leading blank lines and collapse interior blank runs,
	// exactly like go/ast.CommentGroup.Text. A single trailing blank
	// line is kept here; normalization removes it later.
	n := 0
	for _, ln := range lines {
		if ln.text != "" || n > 0 && lines[n-1].text != "" {
			lines[n] = ln
			n++
		}
	}
	return lines[:n]
}

// isCommentDirective replicates go/ast's comment-directive test that
// makes go/ast.CommentGroup.Text drop a line comment: "line ", "extern ",
// and "export " prefixes, or the "[a-z0-9]+:[a-z0-9]" form such as
// "go:noinline".
func isCommentDirective(c string) bool {
	if strings.HasPrefix(c, "line ") || strings.HasPrefix(c, "extern ") || strings.HasPrefix(c, "export ") {
		return true
	}
	colon := strings.Index(c, ":")
	if colon <= 0 || colon+1 >= len(c) {
		return false
	}
	for i := 0; i <= colon+1; i++ {
		if i == colon {
			continue
		}
		b := c[i]
		if !('a' <= b && b <= 'z' || '0' <= b && b <= '9') {
			return false
		}
	}
	return true
}

// parseDirectiveLine recognizes and parses one logical line as an
// InterCall directive. A nil directive with no error means the line is
// blank or prose and is retained. Grammar errors (bare, unknown, and
// malformed directives) are reported at the directive's leading '@'.
func parseDirectiveLine(ln docLine, d *Document) (*Directive, *Error) {
	trimmed := strings.TrimLeft(ln.text, " \t")
	if trimmed == "" {
		return nil, nil
	}
	at := ln.offset + len(ln.text) - len(trimmed)
	fail := func(format string, args ...any) (*Directive, *Error) {
		return nil, &Error{Filename: d.Name, Pos: d.Position(at), Msg: fmt.Sprintf(format, args...)}
	}

	switch {
	case trimmed == "@intercall":
		return fail("bare @intercall directive")
	case strings.HasPrefix(trimmed, "@intercall ") || strings.HasPrefix(trimmed, "@intercall\t"):
		return parseIntercallDirective(trimmed, at, d, fail)
	case trimmed == "@param":
		return fail("malformed @param directive: expected a Go name and documentation text")
	case strings.HasPrefix(trimmed, "@param ") || strings.HasPrefix(trimmed, "@param\t"):
		return parseParamDocDirective(trimmed, at, d, fail)
	case trimmed == "@return":
		return fail("malformed @return directive: expected documentation text")
	case strings.HasPrefix(trimmed, "@return ") || strings.HasPrefix(trimmed, "@return\t"):
		return parseReturnDocDirective(trimmed, at, d, fail)
	}
	return nil, nil
}

// parseIntercallDirective parses the operands of one "@intercall ..."
// line. The directive keyword is the first operand after "@intercall";
// the remaining operands follow the grammar of SPEC.md "Source
// directives and Go documentation".
func parseIntercallDirective(trimmed string, at int, d *Document, fail func(string, ...any) (*Directive, *Error)) (*Directive, *Error) {
	rest := trimmed[len("@intercall"):]
	fields := strings.Fields(rest)
	if len(fields) == 0 {
		return fail("bare @intercall directive")
	}
	word := fields[0]
	pos := d.Position(at)
	switch word {
	case "procedure", "exception", "type":
		kind := ProcedureDir
		switch word {
		case "exception":
			kind = ExceptionDir
		case "type":
			kind = TypeDir
		}
		switch {
		case len(fields) == 1:
			return &Directive{Kind: kind, Pos: pos}, nil
		case len(fields) == 2:
			if !validDirectiveWire(fields[1]) {
				return fail("malformed %s directive: invalid wire name '%s'", kind, fields[1])
			}
			return &Directive{Kind: kind, Pos: pos, Wire: fields[1]}, nil
		default:
			return fail("malformed %s directive: expected at most one wire name", kind)
		}
	case "param":
		switch {
		case len(fields) == 1:
			return fail("malformed @intercall param directive: expected a Go name and a wire name")
		case len(fields) == 2:
			return fail("malformed @intercall param directive: expected a wire name after the Go name")
		case len(fields) > 3:
			return fail("malformed @intercall param directive: expected a Go name and a wire name")
		}
		if !IsValidGoIdentifier(fields[1]) {
			return fail("malformed @intercall param directive: invalid Go name '%s'", fields[1])
		}
		if !validDirectiveWire(fields[2]) {
			return fail("malformed @intercall param directive: invalid wire name '%s'", fields[2])
		}
		return &Directive{Kind: ParamDir, Pos: pos, GoName: fields[1], Wire: fields[2]}, nil
	default:
		return fail("unknown @intercall directive '@intercall %s'", word)
	}
}

// parseParamDocDirective parses "@param GoName text".
func parseParamDocDirective(trimmed string, at int, d *Document, fail func(string, ...any) (*Directive, *Error)) (*Directive, *Error) {
	rest := trimmed[len("@param"):]
	i := skipWS(rest, 0)
	word, end := scanWord(rest, i)
	if word == "" {
		return fail("malformed @param directive: expected a Go name and documentation text")
	}
	if !IsValidGoIdentifier(word) {
		return fail("malformed @param directive: invalid Go name '%s'", word)
	}
	j := skipWS(rest, end)
	text := rest[j:]
	if text == "" {
		return fail("malformed @param directive: expected documentation text")
	}
	return &Directive{
		Kind:       ParamDocDir,
		Pos:        d.Position(at),
		GoName:     word,
		Text:       text,
		textOffset: at + len(trimmed) - len(rest) + j,
	}, nil
}

// parseReturnDocDirective parses "@return text".
func parseReturnDocDirective(trimmed string, at int, d *Document, fail func(string, ...any) (*Directive, *Error)) (*Directive, *Error) {
	rest := trimmed[len("@return"):]
	i := skipWS(rest, 0)
	text := rest[i:]
	if text == "" {
		return fail("malformed @return directive: expected documentation text")
	}
	return &Directive{
		Kind:       ReturnDocDir,
		Pos:        d.Position(at),
		Text:       text,
		textOffset: at + len(trimmed) - len(rest) + i,
	}, nil
}

// skipWS returns the index of the first byte after a run of spaces and
// tabs starting at i.
func skipWS(s string, i int) int {
	for i < len(s) && (s[i] == ' ' || s[i] == '\t') {
		i++
	}
	return i
}

// scanWord scans the whitespace-delimited word starting at index i of s,
// returning the word and the index of the first byte after it.
func scanWord(s string, i int) (string, int) {
	j := i
	for j < len(s) && s[j] != ' ' && s[j] != '\t' {
		j++
	}
	return s[i:j], j
}

// checkDirectives applies the placement, contradiction, duplicate, and
// resolution checks to one declaration's parsed directives, in line
// order, and checks the captured documentation texts for the "*/"
// terminator.
func checkDirectives(gd *GoDoc, d *Document, target docTarget, errs *[]*Error) {
	for _, dir := range gd.Directives {
		// A group-level declaration directive on a group with multiple
		// specifications is contradictory before any placement or
		// resolution check: it applies to exactly one declared object,
		// and the group declares more than one.
		if isDeclarationDirective(dir.Kind) && target.groupSpecs > 1 {
			*errs = append(*errs, &Error{
				Filename: d.Name,
				Pos:      dir.Pos,
				Msg:      fmt.Sprintf("contradictory %s directive: a declaration group must contain exactly one specification", dir.Kind),
			})
			continue
		}
		checkPlacement(dir, d, target, errs)
	}
	for _, dir := range gd.Directives {
		if isDeclarationDirective(dir.Kind) && target.groupSpecs > 1 {
			continue // already reported
		}
		checkResolution(dir, d, target, errs)
	}
	seen := make(map[Directive]bool)
	for _, dir := range gd.Directives {
		key := dir
		key.Pos = Position{}
		key.Text = ""
		key.textOffset = 0
		// A repeat of the same directive kind is a duplicate; for
		// parameter directives the named parameter is the target.
		switch dir.Kind {
		case ParamDir, ParamDocDir:
			key.Wire = ""
		default:
			key.GoName = ""
			key.Wire = ""
		}
		if seen[key] {
			*errs = append(*errs, &Error{Filename: d.Name, Pos: dir.Pos, Msg: duplicateMessage(dir)})
		}
		seen[key] = true
	}
	checkTexts(gd, d, errs)
}

// isDeclarationDirective reports whether a directive kind is a
// declaration directive that applies to exactly one declared object:
// @intercall procedure, @intercall exception, or @intercall type.
func isDeclarationDirective(kind DirectiveKind) bool {
	switch kind {
	case ProcedureDir, ExceptionDir, TypeDir:
		return true
	}
	return false
}

// checkPlacement validates the declaration kind each directive applies
// to.
func checkPlacement(dir Directive, d *Document, target docTarget, errs *[]*Error) {
	bad := func(msg string) {
		*errs = append(*errs, &Error{Filename: d.Name, Pos: dir.Pos, Msg: msg})
	}
	switch dir.Kind {
	case ProcedureDir:
		switch target.kind {
		case GoFunc:
		case GoMethod:
			bad("contradictory @intercall procedure directive: a method cannot be an eligible function")
		default:
			bad("misplaced @intercall procedure directive: it applies only to a function declaration")
		}
	case ExceptionDir:
		switch target.kind {
		case GoVar, GoType:
		default:
			bad("misplaced @intercall exception directive: it applies only to a variable or type declaration")
		}
	case TypeDir:
		if target.kind != GoType {
			bad("misplaced @intercall type directive: it applies only to a type declaration")
		}
	case ParamDir, ParamDocDir, ReturnDocDir:
		if target.kind != GoFunc {
			bad(fmt.Sprintf("misplaced %s directive: it applies only to a function declaration", dir.Kind))
		}
	}
}

// checkResolution validates the declaration each directive applies to:
// exception sentinel and struct shape, ordinary defined types, parameter
// resolution, and the return data result. The checks are purely
// syntactic; type identity, eligibility, and reachability checks live in
// later phases.
func checkResolution(dir Directive, d *Document, target docTarget, errs *[]*Error) {
	bad := func(msg string) {
		*errs = append(*errs, &Error{Filename: d.Name, Pos: dir.Pos, Msg: msg})
	}
	switch dir.Kind {
	case ExceptionDir:
		switch target.kind {
		case GoVar:
			switch {
			case len(target.names) != 1:
				bad("contradictory @intercall exception directive: a sentinel declaration must contain exactly one variable")
			case !isExported(target.names[0]):
				bad("contradictory @intercall exception directive: it applies only to an exported package variable")
			}
		case GoType:
			switch {
			case !isExported(target.name):
				bad("contradictory @intercall exception directive: it applies only to an exported named defined type")
			case target.typeInfo.Alias:
				bad("contradictory @intercall exception directive: an alias is not a named defined type")
			case target.typeInfo.Generic:
				bad("contradictory @intercall exception directive: a generic type is not a named defined type")
			}
		}
	case TypeDir:
		if target.kind == GoType {
			switch {
			case target.typeInfo.Alias:
				bad("contradictory @intercall type directive: a type alias is not an ordinary defined type")
			case target.typeInfo.Generic:
				bad("contradictory @intercall type directive: a generic type is not an ordinary defined type")
			}
		}
	case ParamDir, ParamDocDir:
		if target.kind != GoFunc {
			return // placement already reported
		}
		what := "named"
		if dir.Kind == ParamDocDir {
			what = "documented"
		}
		for i, name := range target.params {
			if name == dir.GoName {
				if i == 0 {
					bad(fmt.Sprintf("contradictory %s directive: the context parameter cannot be %s", dir.Kind, what))
				}
				return
			}
		}
		bad(fmt.Sprintf("unresolved %s directive: no parameter named '%s'", dir.Kind, dir.GoName))
	case ReturnDocDir:
		if target.kind != GoFunc {
			return // placement already reported
		}
		if target.results < 2 {
			bad("contradictory @return directive: the function has no data result")
		}
	}
}

// checkTexts rejects "*/" in the captured @param and @return texts,
// which are documented slots that would be emitted into InterCall block
// comments.
func checkTexts(gd *GoDoc, d *Document, errs *[]*Error) {
	for _, dir := range gd.Directives {
		if dir.Kind != ParamDocDir && dir.Kind != ReturnDocDir {
			continue
		}
		if idx := strings.Index(dir.Text, "*/"); idx >= 0 {
			*errs = append(*errs, &Error{
				Filename: d.Name,
				Pos:      d.Position(dir.textOffset + idx),
				Msg:      "retained documentation contains '*/'",
			})
		}
	}
}

// duplicateMessage reports a directive that repeats an earlier one of
// the same kind and target.
func duplicateMessage(dir Directive) string {
	switch dir.Kind {
	case ParamDir:
		return fmt.Sprintf("duplicate @intercall param directive for parameter '%s'", dir.GoName)
	case ParamDocDir:
		return fmt.Sprintf("duplicate @param directive for parameter '%s'", dir.GoName)
	}
	return fmt.Sprintf("duplicate %s directive", dir.Kind)
}

// joinLines appends one logical line to the retained documentation
// string with an LF separator.
func joinLines(doc, line string) string {
	if doc == "" {
		return line
	}
	return doc + "\n" + line
}

// normalizeDoc normalizes one documentation string like the interface
// documentation: CRLF and bare CR become LF, trailing spaces and tabs
// are removed from each line, leading and trailing blank lines are
// removed, the longest spaces-and-tabs prefix shared by all nonblank
// lines is removed, and the lines are joined with LF.
func normalizeDoc(text string) string {
	text = strings.ReplaceAll(text, "\r\n", "\n")
	text = strings.ReplaceAll(text, "\r", "\n")
	lines := strings.Split(text, "\n")
	for i, ln := range lines {
		lines[i] = strings.TrimRight(ln, " \t")
	}
	for len(lines) > 0 && lines[0] == "" {
		lines = lines[1:]
	}
	for len(lines) > 0 && lines[len(lines)-1] == "" {
		lines = lines[:len(lines)-1]
	}
	if len(lines) == 0 {
		return ""
	}
	prefix := leadingWS(lines[0])
	for _, ln := range lines[1:] {
		if ln == "" {
			continue
		}
		p := leadingWS(ln)
		n := 0
		for n < len(prefix) && n < len(p) && prefix[n] == p[n] {
			n++
		}
		prefix = prefix[:n]
		if prefix == "" {
			break
		}
	}
	if prefix != "" {
		for i, ln := range lines {
			if ln != "" {
				lines[i] = ln[len(prefix):]
			}
		}
	}
	return strings.Join(lines, "\n")
}

// leadingWS returns the longest prefix of s that consists only of spaces
// and tabs.
func leadingWS(s string) string {
	i := 0
	for i < len(s) && (s[i] == ' ' || s[i] == '\t') {
		i++
	}
	return s[:i]
}

// reservedWireWords are the exact lowercase spellings unavailable in
// every identifier position of the InterCall language: the keywords and
// primitive type names of README.md "Grammar".
var reservedWireWords = map[string]bool{
	"type": true, "exception": true, "procedure": true, "list": true, "record": true,
	"int8": true, "int16": true, "int32": true, "int64": true,
	"uint8": true, "uint16": true, "uint32": true, "uint64": true,
	"float32": true, "float64": true, "string": true, "bytes": true,
}

// validDirectiveWire reports whether a directive wire name is a valid,
// nonreserved InterCall identifier: the C-like ASCII form of
// IsValidWireName without the reserved words.
func validDirectiveWire(name string) bool {
	return IsValidWireName(name) && !reservedWireWords[name]
}

// isExported reports whether a Go name is exported under Go's
// Unicode-aware lexical rules: its first character is a Unicode
// uppercase letter.
func isExported(name string) bool {
	r, _ := utf8.DecodeRuneInString(name)
	return unicode.IsUpper(r)
}
