package syntax_test

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/cerasos/intercall/internal/syntax"
)

// TestGoldenValid parses every testdata/valid/*.intercall file and compares
// the canonical AST dump against its .golden file. The dump is a
// deterministic rendering of the exact source structure: declaration and
// comment order, names, token kinds, and byte spans.
func TestGoldenValid(t *testing.T) {
	files, err := filepath.Glob(filepath.Join("testdata", "valid", "*.intercall"))
	if err != nil {
		t.Fatal(err)
	}
	if len(files) == 0 {
		t.Fatal("no valid fixtures")
	}
	for _, path := range files {
		t.Run(filepath.Base(path), func(t *testing.T) {
			src, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			f, err := syntax.Parse(path, src)
			if err != nil {
				t.Fatalf("Parse failed on a valid fixture: %v", err)
			}
			got := dumpFile(f)
			want, err := os.ReadFile(path + ".golden")
			if err != nil {
				t.Fatal(err)
			}
			if got != string(want) {
				t.Errorf("dump mismatch for %s:\n--- got ---\n%s--- want ---\n%s", path, got, want)
			}
		})
	}
}

// TestGoldenInvalid parses every testdata/invalid/*.intercall file and
// compares the exact diagnostic — error offset and the rendered
// "path:line:column: message" — against its .golden file.
func TestGoldenInvalid(t *testing.T) {
	files, err := filepath.Glob(filepath.Join("testdata", "invalid", "*.intercall"))
	if err != nil {
		t.Fatal(err)
	}
	if len(files) == 0 {
		t.Fatal("no invalid fixtures")
	}
	for _, path := range files {
		t.Run(filepath.Base(path), func(t *testing.T) {
			src, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			_, err = syntax.Parse(path, src)
			if err == nil {
				t.Fatalf("Parse succeeded on an invalid fixture %s", path)
			}
			e, ok := err.(*syntax.Error)
			if !ok {
				t.Fatalf("error type %T, want *syntax.Error", err)
			}
			got := fmt.Sprintf("offset %d\n%s\n", e.Pos.Offset, e.Error())
			want, err := os.ReadFile(path + ".golden")
			if err != nil {
				t.Fatal(err)
			}
			if got != string(want) {
				t.Errorf("diagnostic mismatch for %s:\n--- got ---\n%s--- want ---\n%s", path, got, want)
			}
		})
	}
}

// dumpFile renders the complete source structure of f.
func dumpFile(f *syntax.File) string {
	var b strings.Builder
	fmt.Fprintf(&b, "file %s (%d bytes)\n", f.Name, f.Size)
	for _, c := range f.Comments {
		fmt.Fprintf(&b, "comment [%d,%d) %q\n", c.Span.Start, c.Span.End, c.Text)
	}
	for i, d := range f.Decls {
		dumpDecl(&b, i, d, 0)
	}
	return b.String()
}

func indent(depth int) string { return strings.Repeat("  ", depth) }

func dumpDecl(b *strings.Builder, i int, d syntax.Decl, depth int) {
	pad := indent(depth)
	switch d := d.(type) {
	case *syntax.TypeDecl:
		fmt.Fprintf(b, "%sdecl %d type-decl [%d,%d)\n", pad, i, d.Span().Start, d.Span().End)
		fmt.Fprintf(b, "%s  kw type [%d,%d)\n", pad, d.TypeSpan.Start, d.TypeSpan.End)
		dumpIdent(b, d.Name, depth+1)
		dumpType(b, d.Type, depth+1)
		dumpSemi(b, d.Semi, depth+1)
	case *syntax.ExceptionDecl:
		fmt.Fprintf(b, "%sdecl %d exception-decl [%d,%d)\n", pad, i, d.Span().Start, d.Span().End)
		fmt.Fprintf(b, "%s  kw exception [%d,%d)\n", pad, d.ExceptionSpan.Start, d.ExceptionSpan.End)
		dumpIdent(b, d.Name, depth+1)
		if d.Type != nil {
			dumpType(b, d.Type, depth+1)
		}
		dumpSemi(b, d.Semi, depth+1)
	case *syntax.ProcDecl:
		fmt.Fprintf(b, "%sdecl %d procedure-decl [%d,%d)\n", pad, i, d.Span().Start, d.Span().End)
		fmt.Fprintf(b, "%s  kw procedure [%d,%d)\n", pad, d.ProcedureSpan.Start, d.ProcedureSpan.End)
		dumpIdent(b, d.Name, depth+1)
		fmt.Fprintf(b, "%s  lbrace [%d,%d)\n", pad, d.LBrace.Start, d.LBrace.End)
		for j, p := range d.Params {
			dumpParam(b, j, p, depth+1)
		}
		fmt.Fprintf(b, "%s  rbrace [%d,%d)\n", pad, d.RBrace.Start, d.RBrace.End)
		if d.Result != nil {
			dumpType(b, d.Result, depth+1)
		}
		dumpSemi(b, d.Semi, depth+1)
	}
}

func dumpParam(b *strings.Builder, i int, p *syntax.Param, depth int) {
	pad := indent(depth)
	fmt.Fprintf(b, "%sparam %d [%d,%d)\n", pad, i, p.Span().Start, p.Span().End)
	dumpIdent(b, p.Name, depth+1)
	dumpType(b, p.Type, depth+1)
	dumpSemi(b, p.Semi, depth+1)
}

func dumpField(b *strings.Builder, i int, fld *syntax.Field, depth int) {
	pad := indent(depth)
	fmt.Fprintf(b, "%sfield %d [%d,%d)\n", pad, i, fld.Span().Start, fld.Span().End)
	dumpIdent(b, fld.Name, depth+1)
	dumpType(b, fld.Type, depth+1)
	dumpSemi(b, fld.Semi, depth+1)
}

func dumpIdent(b *strings.Builder, id *syntax.Ident, depth int) {
	fmt.Fprintf(b, "%sname %q [%d,%d)\n", indent(depth), id.Name, id.Span().Start, id.Span().End)
}

func dumpSemi(b *strings.Builder, semi syntax.Span, depth int) {
	fmt.Fprintf(b, "%ssemi [%d,%d)\n", indent(depth), semi.Start, semi.End)
}

func dumpType(b *strings.Builder, t syntax.TypeExpr, depth int) {
	pad := indent(depth)
	switch t := t.(type) {
	case *syntax.PrimType:
		span := t.Span()
		fmt.Fprintf(b, "%sprim %s [%d,%d)\n", pad, t.Kind, span.Start, span.End)
	case *syntax.NamedType:
		span := t.Span()
		fmt.Fprintf(b, "%snamed [%d,%d)\n", pad, span.Start, span.End)
		dumpIdent(b, t.Name, depth+1)
	case *syntax.ListType:
		span := t.Span()
		fmt.Fprintf(b, "%slist [%d,%d)\n", pad, span.Start, span.End)
		fmt.Fprintf(b, "%s  kw list [%d,%d)\n", pad, t.ListSpan.Start, t.ListSpan.End)
		dumpType(b, t.Elem, depth+1)
	case *syntax.RecordType:
		span := t.Span()
		fmt.Fprintf(b, "%srecord [%d,%d)\n", pad, span.Start, span.End)
		fmt.Fprintf(b, "%s  kw record [%d,%d)\n", pad, t.RecordSpan.Start, t.RecordSpan.End)
		fmt.Fprintf(b, "%s  lbrace [%d,%d)\n", pad, t.LBrace.Start, t.LBrace.End)
		for j, fld := range t.Fields {
			dumpField(b, j, fld, depth+2)
		}
		fmt.Fprintf(b, "%s  rbrace [%d,%d)\n", pad, t.RBrace.Start, t.RBrace.End)
	}
}
