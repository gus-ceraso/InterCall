package syntax

import (
	"sort"
	"strings"
)

// AttachDocs attaches documentation comments to every eligible anchor and
// normalizes the resulting documentation strings, following the "Semantic
// documentation" rules in SPEC.md.
//
// Eligible anchors are the first token of every declaration, procedure
// parameter, record field, and type-specifier occurrence, in source order.
// A documentation group is the maximal run of block comments in the trivia
// immediately before an anchor, with no blank line within the group or
// between the group and the anchor; a blank line contains only spaces or
// tabs between line terminators. A comment after a completed node on the
// same line is trailing and does not attach to a later node. A comment
// between a parameter, field, exception, or type-declaration name and its
// type anchors that type, and a comment after "list" anchors its element.
// The rules apply recursively, and each comment attaches at most once;
// comments that attach to no anchor are discarded by Format.
//
// Each attached comment body is normalized separately: CRLF and bare CR
// become LF; trailing spaces and tabs are removed from each line; leading
// and trailing blank lines are removed; the longest spaces-and-tabs prefix
// shared by all nonblank lines is removed from every nonblank line; and
// the lines are joined with LF. Empty bodies are discarded, and the
// remaining bodies of one group are joined with two LFs. AttachDocs
// recomputes every documentation slot from the file's comment list, so
// calling it again on the same file is a no-op in effect.
func AttachDocs(f *File) {
	a := &attacher{file: f}
	a.collect()
	a.attach()
}

// attacher holds the per-file state of one attachment pass.
type attacher struct {
	file    *File
	tokens  []int    // end offsets of every non-comment token, ascending
	ends    []int    // end offsets of every node, ascending
	anchors []anchor // documentation anchors in source order
	lines   []int    // offsets of every line start, terminators \n, \r\n, \r
}

// anchor is one documentation slot with the byte offset of its anchor
// token's first byte.
type anchor struct {
	start int
	set   func(string)
}

// collect resets every documentation slot and gathers the token ends, node
// ends, and anchors of the file. The anchor walk is a pre-order traversal
// in source order driven by an explicit frame stack, so call-stack use
// does not grow with type nesting; node ends are collected in traversal
// order and sorted afterwards.
func (a *attacher) collect() {
	scan := NewScanner(a.file)
	for {
		tok, err := scan.Next()
		if err != nil || tok.Kind == TokEOF {
			break // a file returned by Parse always rescans cleanly
		}
		if tok.Kind == TokComment {
			continue
		}
		a.tokens = append(a.tokens, tok.Span.End)
	}

	for _, d := range a.file.Decls {
		switch d := d.(type) {
		case *TypeDecl:
			d.Doc = ""
			a.ends = append(a.ends, d.Span().End)
			a.anchors = append(a.anchors, anchor{d.TypeSpan.Start, func(s string) { d.Doc = s }})
			a.walkType(d.Type)
		case *ExceptionDecl:
			d.Doc = ""
			a.ends = append(a.ends, d.Span().End)
			a.anchors = append(a.anchors, anchor{d.ExceptionSpan.Start, func(s string) { d.Doc = s }})
			if d.Type != nil {
				a.walkType(d.Type)
			}
		case *ProcDecl:
			d.Doc = ""
			a.ends = append(a.ends, d.Span().End)
			a.anchors = append(a.anchors, anchor{d.ProcedureSpan.Start, func(s string) { d.Doc = s }})
			for _, p := range d.Params {
				a.paramAnchor(p)
				a.walkType(p.Type)
			}
			if d.Result != nil {
				a.walkType(d.Result)
			}
		}
	}

	sort.Ints(a.ends)
	a.lines = lineStarts(a.file.src)
}

// walkType visits every type occurrence under root in pre-order (source
// order), resetting each documentation slot and recording its anchor and
// node end. An explicit frame stack of open records replaces recursion,
// and each list chain is walked as one unit by walkListChain, so both
// call-stack use and time stay independent of type nesting.
func (a *attacher) walkType(root TypeExpr) {
	type frame struct {
		rec  *RecordType
		next int // next field index
	}
	var stack []frame
	t := root
	for {
		if lst, ok := t.(*ListType); ok {
			t = a.walkListChain(lst)
			continue
		}
		a.typeAnchor(t)
		if rec, ok := t.(*RecordType); ok {
			stack = append(stack, frame{rec: rec})
		}
		// Descend into the next pending record field, or finish when no
		// record frame has fields left.
		t = nil
		for len(stack) > 0 {
			top := &stack[len(stack)-1]
			if top.next >= len(top.rec.Fields) {
				stack = stack[:len(stack)-1]
				continue
			}
			f := top.rec.Fields[top.next]
			top.next++
			a.fieldAnchor(f)
			t = f.Type
			break
		}
		if t == nil {
			return
		}
	}
}

// walkListChain records the anchor and documentation reset of every list
// node in a list chain in source order and returns the terminal element.
// Every list node's span ends where its element ends, so the whole chain
// shares the terminal element's end; recording that end once per node
// here keeps the walk linear, and the chain walk itself is a loop, so
// call-stack use does not grow with chain length.
func (a *attacher) walkListChain(lst *ListType) TypeExpr {
	count := 0
	node := lst
	for {
		cur := node // fresh per iteration for the anchor closure
		cur.Doc = ""
		a.anchors = append(a.anchors, anchor{cur.ListSpan.Start, func(s string) { cur.Doc = s }})
		count++
		if e, ok := cur.Elem.(*ListType); ok {
			node = e
			continue
		}
		end := cur.Elem.Span().End
		for i := 0; i < count; i++ {
			a.ends = append(a.ends, end)
		}
		return cur.Elem
	}
}

// typeAnchor resets one type occurrence's documentation slot and records
// its anchor and node end. List chains are anchored by walkListChain,
// which shares one end computation across the whole chain; the ListType
// case below stays correct on its own through the iterative
// (*ListType).Span.
func (a *attacher) typeAnchor(t TypeExpr) {
	switch t := t.(type) {
	case *PrimType:
		t.Doc = ""
		span := t.Span()
		a.ends = append(a.ends, span.End)
		a.anchors = append(a.anchors, anchor{span.Start, func(s string) { t.Doc = s }})
	case *NamedType:
		t.Doc = ""
		span := t.Span()
		a.ends = append(a.ends, span.End)
		a.anchors = append(a.anchors, anchor{span.Start, func(s string) { t.Doc = s }})
	case *ListType:
		t.Doc = ""
		a.ends = append(a.ends, t.Span().End)
		a.anchors = append(a.anchors, anchor{t.ListSpan.Start, func(s string) { t.Doc = s }})
	case *RecordType:
		t.Doc = ""
		a.ends = append(a.ends, t.Span().End)
		a.anchors = append(a.anchors, anchor{t.RecordSpan.Start, func(s string) { t.Doc = s }})
	}
}

// fieldAnchor resets one record field's documentation slot and records
// its anchor and node end.
func (a *attacher) fieldAnchor(f *Field) {
	f.Doc = ""
	a.ends = append(a.ends, f.Span().End)
	a.anchors = append(a.anchors, anchor{f.Name.span.Start, func(s string) { f.Doc = s }})
}

// paramAnchor resets one procedure parameter's documentation slot and
// records its anchor and node end.
func (a *attacher) paramAnchor(p *Param) {
	p.Doc = ""
	a.ends = append(a.ends, p.Span().End)
	a.anchors = append(a.anchors, anchor{p.Name.span.Start, func(s string) { p.Doc = s }})
}

// attach walks the anchors in source order and fills each slot from the
// documentation group in the trivia immediately before the anchor.
func (a *attacher) attach() {
	comments := a.file.Comments
	ends := make([]int, len(comments))
	for i, c := range comments {
		ends[i] = c.Span.End
	}
	for _, an := range a.anchors {
		// The previous token bounds the trivia; the anchor's own token is
		// the first token with an end beyond the anchor, so the token at
		// the search position minus one is the previous one.
		i := sort.Search(len(a.tokens), func(i int) bool { return a.tokens[i] > an.start })
		prevEnd := 0
		if i > 0 {
			prevEnd = a.tokens[i-1]
		}
		// The comments in the trivia are exactly those ending after the
		// previous token and at or before the anchor.
		lo := sort.Search(len(ends), func(i int) bool { return ends[i] > prevEnd })
		hi := sort.Search(len(ends), func(i int) bool { return ends[i] > an.start })
		if lo == hi {
			continue
		}
		group := comments[lo:hi]

		// The last segment is the maximal run of comments ending at the
		// anchor with no blank line between consecutive comments.
		seg := len(group) - 1
		for seg > 0 && !hasBlankLine(a.file.src, group[seg-1].Span.End, group[seg].Span.Start) {
			seg--
		}
		// A blank line between the group and the anchor detaches it.
		if hasBlankLine(a.file.src, group[len(group)-1].Span.End, an.start) {
			continue
		}
		// Trailing comments form a prefix of the segment: skip them.
		k := seg
		for k < len(group) && a.isTrailing(group[k]) {
			k++
		}
		if k == len(group) {
			continue
		}
		an.set(groupDoc(group[k:]))
	}
}

// isTrailing reports whether the comment sits after a completed node on
// the same line, which makes it trailing: it does not attach to a later
// node. The completed node closest to the comment decides, because every
// node completed before the comment on its line is also completed before
// that closest node.
func (a *attacher) isTrailing(c *Comment) bool {
	i := sort.Search(len(a.ends), func(i int) bool { return a.ends[i] > c.Span.Start })
	if i == 0 {
		return false
	}
	return a.lineOf(a.ends[i-1]-1) == a.lineOf(c.Span.Start)
}

// lineOf returns the zero-based physical line of a byte offset, where
// lines are delimited by '\n', '\r\n', or a bare '\r'.
func (a *attacher) lineOf(offset int) int {
	return sort.Search(len(a.lines), func(i int) bool { return a.lines[i] > offset }) - 1
}

// lineStarts returns the offset of every line start in src: zero, then one
// position after every line terminator ('\n', '\r\n' as one terminator,
// or a bare '\r').
func lineStarts(src []byte) []int {
	starts := []int{0}
	for i := 0; i < len(src); i++ {
		switch src[i] {
		case '\n':
			starts = append(starts, i+1)
		case '\r':
			if i+1 < len(src) && src[i+1] == '\n' {
				i++
			}
			starts = append(starts, i+1)
		}
	}
	return starts
}

// hasBlankLine reports whether src[a:b] contains a complete line whose
// content is only spaces or tabs. Only lines delimited by line terminators
// on both sides count; the partial first and last lines of the range are
// tails of the surrounding lines and never count.
func hasBlankLine(src []byte, a, b int) bool {
	prevEnd := -1 // end of the previous terminator inside [a, b)
	i := a
	for i < b {
		switch src[i] {
		case '\n':
			if prevEnd >= 0 && isBlankContent(src[prevEnd:i]) {
				return true
			}
			prevEnd = i + 1
			i++
		case '\r':
			next := i + 1
			if next < b && src[next] == '\n' {
				next++
			}
			if prevEnd >= 0 && isBlankContent(src[prevEnd:i]) {
				return true
			}
			prevEnd = next
			i = next
		default:
			i++
		}
	}
	return false
}

// isBlankContent reports whether every byte is a space or a tab.
func isBlankContent(b []byte) bool {
	for _, c := range b {
		if c != ' ' && c != '\t' {
			return false
		}
	}
	return true
}

// groupDoc normalizes every comment body in the group separately, discards
// empty bodies, and joins the remaining bodies with two LFs.
func groupDoc(group []*Comment) string {
	bodies := make([]string, 0, len(group))
	for _, c := range group {
		if d := normalizeDoc(c.Text); d != "" {
			bodies = append(bodies, d)
		}
	}
	return strings.Join(bodies, "\n\n")
}

// normalizeDoc normalizes one comment body: CRLF and bare CR become LF and
// trailing spaces and tabs are removed from each line; leading and trailing
// blank lines are removed; the longest spaces-and-tabs prefix shared by all
// nonblank lines is removed from every nonblank line; and the lines are
// joined with LF. An empty result is an empty documentation slot.
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
