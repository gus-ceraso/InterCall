package syntax

import "strings"

// Format renders the byte-exact canonical body of one validated interface
// file, following the "Semantic documentation" rules in SPEC.md.
//
// The caller must pass a file produced by Parse and accepted by Validate;
// Format reads the AST and its documentation slots without validating or
// attaching comments itself. Format preserves source declaration order and
// renders a canonical byte result for every valid AST:
//
//   - declarations remain in AST order, with one blank line between them;
//   - indentation is four spaces per enclosing procedure or record;
//   - adjacent same-line keywords and names use one ASCII space, and '{'
//     follows its keyword or name by one ASCII space; there is no other
//     horizontal whitespace outside indentation or documentation;
//   - a node's document appears immediately before the complete node;
//   - a type without documentation follows its prefix (type name, field,
//     parameter, exception name, procedure '}', or list) after one space;
//   - a documented type follows its prefix after LF, its document at the
//     type's indentation, and the type at that indentation;
//   - nonempty records and parameter blocks put one field or parameter on
//     each line, and record {} and an empty parameter block {} stay
//     inline;
//   - semicolons immediately follow their value or closing brace;
//   - output never wraps for line width;
//   - an empty body is zero bytes, and a nonempty body ends in one LF.
//
// Unattached comments are not part of the AST and never appear in the
// output.
func Format(f *File) []byte {
	if len(f.Decls) == 0 {
		return nil
	}
	var b strings.Builder
	for i, d := range f.Decls {
		if i > 0 {
			b.WriteByte('\n')
		}
		writeDecl(&b, d, "")
	}
	return []byte(b.String())
}

// fmtKind identifies one pending formatter action.
type fmtKind uint8

const (
	fmtType  fmtKind = iota // one type occurrence after its prefix
	fmtField                // one record field head, then its type and tail
	fmtParam                // one parameter head, then its type and tail
	fmtSemi                 // ";\n" after a completed field, parameter, or type
	fmtTail                 // indent + "}" closing an open record or parameter block
)

// fmtItem is one pending formatter action.
type fmtItem struct {
	kind   fmtKind
	t      TypeExpr
	f      *Field
	p      *Param
	indent string
}

// runFormat drains the item stack, writing the complete output of one
// declaration. The canonical output is a strict pre-order stream, so an
// explicit item stack replaces the recursive grammar printer: a nested
// type, record field, or parameter is pushed as work and popped in the
// same order the recursive printer would have emitted it, keeping
// call-stack use independent of type nesting.
func runFormat(b *strings.Builder, stack []fmtItem) {
	for len(stack) > 0 {
		it := stack[len(stack)-1]
		stack = stack[:len(stack)-1]
		switch it.kind {
		case fmtType:
			writeType(b, &stack, it.t, it.indent)
		case fmtField:
			writeDoc(b, it.indent, it.f.Doc)
			b.WriteString(it.indent)
			b.WriteString(it.f.Name.Name)
			stack = append(stack, fmtItem{kind: fmtSemi}, fmtItem{kind: fmtType, t: it.f.Type, indent: it.indent})
		case fmtParam:
			writeDoc(b, it.indent, it.p.Doc)
			b.WriteString(it.indent)
			b.WriteString(it.p.Name.Name)
			stack = append(stack, fmtItem{kind: fmtSemi}, fmtItem{kind: fmtType, t: it.p.Type, indent: it.indent})
		case fmtSemi:
			b.WriteString(";\n")
		case fmtTail:
			b.WriteString(it.indent)
			b.WriteByte('}')
		}
	}
}

// writeDecl writes one complete declaration at indent, including its
// documentation, and ends with a line feed.
func writeDecl(b *strings.Builder, d Decl, indent string) {
	switch d := d.(type) {
	case *TypeDecl:
		writeDoc(b, indent, d.Doc)
		b.WriteString(indent)
		b.WriteString("type ")
		b.WriteString(d.Name.Name)
		runFormat(b, []fmtItem{{kind: fmtSemi}, {kind: fmtType, t: d.Type, indent: indent}})
	case *ExceptionDecl:
		writeDoc(b, indent, d.Doc)
		b.WriteString(indent)
		b.WriteString("exception ")
		b.WriteString(d.Name.Name)
		stack := make([]fmtItem, 0, 2)
		stack = append(stack, fmtItem{kind: fmtSemi})
		if d.Type != nil {
			stack = append(stack, fmtItem{kind: fmtType, t: d.Type, indent: indent})
		}
		runFormat(b, stack)
	case *ProcDecl:
		writeDoc(b, indent, d.Doc)
		b.WriteString(indent)
		b.WriteString("procedure ")
		b.WriteString(d.Name.Name)
		b.WriteString(" {")
		if len(d.Params) > 0 {
			b.WriteByte('\n')
			stack := make([]fmtItem, 0, 3+2*len(d.Params))
			stack = append(stack, fmtItem{kind: fmtSemi})
			if d.Result != nil {
				stack = append(stack, fmtItem{kind: fmtType, t: d.Result, indent: indent})
			}
			stack = append(stack, fmtItem{kind: fmtTail, indent: indent})
			for i := len(d.Params) - 1; i >= 0; i-- {
				stack = append(stack, fmtItem{kind: fmtParam, p: d.Params[i], indent: indent + "    "})
			}
			runFormat(b, stack)
			return
		}
		b.WriteByte('}')
		if d.Result != nil {
			runFormat(b, []fmtItem{{kind: fmtSemi}, {kind: fmtType, t: d.Result, indent: indent}})
			return
		}
		b.WriteString(";\n")
	}
}

// writeType writes a type occurrence after its prefix: one space for an
// undocumented type, or LF, the type's documentation at its indentation,
// and the type at that indentation. Nested occurrences are pushed onto
// the item stack instead of recursing.
func writeType(b *strings.Builder, stack *[]fmtItem, t TypeExpr, indent string) {
	if doc := typeDoc(t); doc != "" {
		b.WriteByte('\n')
		writeDoc(b, indent, doc)
		b.WriteString(indent)
		writeTypeBody(b, stack, t, indent)
		return
	}
	b.WriteByte(' ')
	writeTypeBody(b, stack, t, indent)
}

// writeTypeBody writes the type occurrence itself at indent, including the
// nested documentation of list elements and record fields, but not the
// type's own documentation, which writeType emits after the prefix. A
// list element is pushed as one item; a record pushes its closing tail
// and its fields in reverse so the first field is emitted next.
func writeTypeBody(b *strings.Builder, stack *[]fmtItem, t TypeExpr, indent string) {
	switch t := t.(type) {
	case *PrimType:
		b.WriteString(t.Kind.String())
	case *NamedType:
		b.WriteString(t.Name.Name)
	case *ListType:
		b.WriteString("list")
		*stack = append(*stack, fmtItem{kind: fmtType, t: t.Elem, indent: indent})
	case *RecordType:
		b.WriteString("record {")
		if len(t.Fields) > 0 {
			b.WriteByte('\n')
			*stack = append(*stack, fmtItem{kind: fmtTail, indent: indent})
			for i := len(t.Fields) - 1; i >= 0; i-- {
				*stack = append(*stack, fmtItem{kind: fmtField, f: t.Fields[i], indent: indent + "    "})
			}
		} else {
			b.WriteByte('}')
		}
	}
}

// typeDoc returns the documentation slot of one type occurrence.
func typeDoc(t TypeExpr) string {
	switch t := t.(type) {
	case *PrimType:
		return t.Doc
	case *NamedType:
		return t.Doc
	case *ListType:
		return t.Doc
	case *RecordType:
		return t.Doc
	}
	return ""
}

// writeDoc writes doc(indent, D) for a nonempty documentation string D:
// "indent + "/* D */\n" when D has no line feed; otherwise a block of
// "indent + "/*\n"", each nonempty line as "indent + "    " + line + "\n"",
// each empty line as "\n", and finally "indent + "*/\n"". An empty D writes
// nothing.
func writeDoc(b *strings.Builder, indent, doc string) {
	if doc == "" {
		return
	}
	if !strings.Contains(doc, "\n") {
		b.WriteString(indent)
		b.WriteString("/* ")
		b.WriteString(doc)
		b.WriteString(" */\n")
		return
	}
	b.WriteString(indent)
	b.WriteString("/*\n")
	for _, line := range strings.Split(doc, "\n") {
		if line == "" {
			b.WriteByte('\n')
			continue
		}
		b.WriteString(indent)
		b.WriteString("    ")
		b.WriteString(line)
		b.WriteByte('\n')
	}
	b.WriteString(indent)
	b.WriteString("*/\n")
}
