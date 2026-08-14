package syntax

import (
	"fmt"
	"sort"
)

// Span is a half-open byte range [Start, End) into one source file.
//
// Start and End are exact offsets in the input bytes. A span is valid when
// 0 <= Start <= End <= len(src). End is one past the last byte of the
// covered text, so the empty span [k, k) covers nothing and EOF is
// [len(src), len(src)).
type Span struct {
	Start, End int
}

// String renders the span as "[Start,End)".
func (s Span) String() string {
	return fmt.Sprintf("[%d,%d)", s.Start, s.End)
}

// Position is a byte offset with its one-based physical line and byte column.
//
// Lines and columns follow go/token semantics: a line starts at offset zero
// and after every '\n', and the column is the byte distance from the line
// start plus one. A '\r' is an ordinary byte; CRLF therefore counts as one
// line break at the '\n'. EOF is the position of offset len(src).
type Position struct {
	Offset int
	Line   int
	Column int
}

// String renders the position as "line:column".
func (p Position) String() string {
	return fmt.Sprintf("%d:%d", p.Line, p.Column)
}

// Comment is one block comment in source order.
//
// Span covers the complete comment including the "/*" and "*/" delimiters.
// Text is the exact raw body between the delimiters, without normalization.
type Comment struct {
	Span Span
	Text string
}

// File is one parsed interface source file.
//
// A File is source-only: it owns the AST declarations and the complete
// comment list, and it can map byte offsets to physical positions and
// extract exact source text for diagnostics. Decls are in source order, as
// are Comments.
type File struct {
	Name     string
	Size     int
	Decls    []Decl
	Comments []*Comment

	src   []byte
	lines []int // offsets of every line start; lines[0] is always 0
}

// NewFile constructs a File for src without parsing it.
//
// The name is used only for diagnostics. NewFile never copies src; callers
// must not mutate it after construction.
func NewFile(name string, src []byte) *File {
	lines := []int{0}
	for i, b := range src {
		if b == '\n' {
			lines = append(lines, i+1)
		}
	}
	return &File{
		Name:  name,
		Size:  len(src),
		src:   src,
		lines: lines,
	}
}

// Position maps a byte offset to its one-based physical line and byte
// column.
//
// The offset must satisfy 0 <= offset <= Size; any other offset panics.
// Position(Size) is the EOF position.
func (f *File) Position(offset int) Position {
	if offset < 0 || offset > f.Size {
		panic(fmt.Sprintf("syntax: Position(%d) out of range [0, %d]", offset, f.Size))
	}
	// lines[i] is the first line start strictly after offset; the offset
	// belongs to the preceding line.
	line := sort.Search(len(f.lines), func(i int) bool { return f.lines[i] > offset }) - 1
	return Position{
		Offset: offset,
		Line:   line + 1,
		Column: offset - f.lines[line] + 1,
	}
}

// Text returns the exact source bytes covered by a span.
//
// The span must satisfy 0 <= Start <= End <= Size; any other span panics.
func (f *File) Text(span Span) string {
	if span.Start < 0 || span.End < span.Start || span.End > f.Size {
		panic(fmt.Sprintf("syntax: Text(%s) out of range for size %d", span, f.Size))
	}
	return string(f.src[span.Start:span.End])
}
