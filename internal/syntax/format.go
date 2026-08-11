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

// writeDecl writes one complete declaration at indent, including its
// documentation, and ends with a line feed.
func writeDecl(b *strings.Builder, d Decl, indent string) {
	switch d := d.(type) {
	case *TypeDecl:
		writeDoc(b, indent, d.Doc)
		b.WriteString(indent)
		b.WriteString("type ")
		b.WriteString(d.Name.Name)
		writeType(b, d.Type, indent)
		b.WriteString(";\n")
	case *ExceptionDecl:
		writeDoc(b, indent, d.Doc)
		b.WriteString(indent)
		b.WriteString("exception ")
		b.WriteString(d.Name.Name)
		if d.Type != nil {
			writeType(b, d.Type, indent)
		}
		b.WriteString(";\n")
	case *ProcDecl:
		writeDoc(b, indent, d.Doc)
		b.WriteString(indent)
		b.WriteString("procedure ")
		b.WriteString(d.Name.Name)
		b.WriteString(" {")
		if len(d.Params) > 0 {
			b.WriteByte('\n')
			for _, p := range d.Params {
				writeParam(b, p, indent+"    ")
			}
			b.WriteString(indent)
			b.WriteByte('}')
		} else {
			b.WriteByte('}')
		}
		if d.Result != nil {
			writeType(b, d.Result, indent)
		}
		b.WriteString(";\n")
	}
}

// writeParam writes one procedure parameter at indent, including its
// documentation.
func writeParam(b *strings.Builder, p *Param, indent string) {
	writeDoc(b, indent, p.Doc)
	b.WriteString(indent)
	b.WriteString(p.Name.Name)
	writeType(b, p.Type, indent)
	b.WriteString(";\n")
}

// writeField writes one record field at indent, including its
// documentation.
func writeField(b *strings.Builder, f *Field, indent string) {
	writeDoc(b, indent, f.Doc)
	b.WriteString(indent)
	b.WriteString(f.Name.Name)
	writeType(b, f.Type, indent)
	b.WriteString(";\n")
}

// writeType writes a type occurrence after its prefix: one space for an
// undocumented type, or LF, the type's documentation at its indentation,
// and the type at that indentation.
func writeType(b *strings.Builder, t TypeExpr, indent string) {
	if doc := typeDoc(t); doc != "" {
		b.WriteByte('\n')
		writeDoc(b, indent, doc)
		b.WriteString(indent)
		writeTypeBody(b, t, indent)
		return
	}
	b.WriteByte(' ')
	writeTypeBody(b, t, indent)
}

// writeTypeBody writes the type occurrence itself at indent, including the
// nested documentation of list elements and record fields, but not the
// type's own documentation, which writeType emits after the prefix.
func writeTypeBody(b *strings.Builder, t TypeExpr, indent string) {
	switch t := t.(type) {
	case *PrimType:
		b.WriteString(t.Kind.String())
	case *NamedType:
		b.WriteString(t.Name.Name)
	case *ListType:
		b.WriteString("list")
		writeType(b, t.Elem, indent)
	case *RecordType:
		b.WriteString("record {")
		if len(t.Fields) > 0 {
			b.WriteByte('\n')
			for _, f := range t.Fields {
				writeField(b, f, indent+"    ")
			}
			b.WriteString(indent)
			b.WriteByte('}')
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
