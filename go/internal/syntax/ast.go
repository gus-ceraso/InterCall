package syntax

// Decl is one interface declaration in source order.
//
// Every declaration has a documentation slot (Doc) that is empty after
// parsing; comment attachment and normalization are a later phase.
type Decl interface {
	declNode()
	Span() Span
}

// TypeExpr is one type-specifier occurrence.
//
// Every type occurrence — a declaration's underlying type, an exception
// payload, a procedure return, a parameter or field type, a list element, a
// primitive or named reference, and an inline record — has a documentation
// slot (Doc) that is empty after parsing.
type TypeExpr interface {
	typeNode()
	Span() Span
}

// Ident is one identifier token.
type Ident struct {
	Name string
	span Span
}

// Span returns the identifier's exact source span.
func (id *Ident) Span() Span { return id.span }

// TypeDecl is a "type NAME type-specifier ;" declaration.
type TypeDecl struct {
	Doc      string // documentation slot; empty after parsing
	TypeSpan Span   // span of the "type" keyword
	Name     *Ident
	Type     TypeExpr
	Semi     Span
}

func (d *TypeDecl) declNode()  {}
func (d *TypeDecl) Span() Span { return Span{d.TypeSpan.Start, d.Semi.End} }

// ExceptionDecl is an "exception NAME type-specifier? ;" declaration.
type ExceptionDecl struct {
	Doc           string // documentation slot; empty after parsing
	ExceptionSpan Span   // span of the "exception" keyword
	Name          *Ident
	Type          TypeExpr // nil when the payload is omitted
	Semi          Span
}

func (d *ExceptionDecl) declNode()  {}
func (d *ExceptionDecl) Span() Span { return Span{d.ExceptionSpan.Start, d.Semi.End} }

// ProcDecl is a "procedure NAME { parameter* } type-specifier? ;"
// declaration.
type ProcDecl struct {
	Doc           string // documentation slot; empty after parsing
	ProcedureSpan Span   // span of the "procedure" keyword
	Name          *Ident
	LBrace        Span
	Params        []*Param
	RBrace        Span
	Result        TypeExpr // nil when the return value is omitted
	Semi          Span
}

func (d *ProcDecl) declNode()  {}
func (d *ProcDecl) Span() Span { return Span{d.ProcedureSpan.Start, d.Semi.End} }

// Param is one procedure parameter: "IDENT type-specifier ;".
type Param struct {
	Doc  string // documentation slot; empty after parsing
	Name *Ident
	Type TypeExpr
	Semi Span
}

// Span returns the parameter's span from its name through its semicolon.
func (p *Param) Span() Span { return Span{p.Name.span.Start, p.Semi.End} }

// PrimType is one primitive type occurrence.
type PrimType struct {
	Doc  string // documentation slot; empty after parsing
	Kind TokenKind
	span Span
}

func (t *PrimType) typeNode() {}

// Span returns the primitive name's exact source span.
func (t *PrimType) Span() Span { return t.span }

// NamedType is one named-type reference: an identifier in type position.
type NamedType struct {
	Doc  string // documentation slot; empty after parsing
	Name *Ident
}

func (t *NamedType) typeNode() {}

// Span returns the reference's exact source span.
func (t *NamedType) Span() Span { return t.Name.span }

// ListType is a "list type-specifier" occurrence.
type ListType struct {
	Doc      string // documentation slot; empty after parsing
	ListSpan Span   // span of the "list" keyword
	Elem     TypeExpr
}

func (t *ListType) typeNode() {}

// Span returns the complete list type's span, from "list" through its
// element. The element chain is walked iteratively, so call-stack use
// stays independent of type nesting.
func (t *ListType) Span() Span {
	start := t.ListSpan.Start
	for {
		if e, ok := t.Elem.(*ListType); ok {
			t = e
			continue
		}
		return Span{start, t.Elem.Span().End}
	}
}

// RecordType is a "record { field* }" occurrence.
type RecordType struct {
	Doc        string // documentation slot; empty after parsing
	RecordSpan Span   // span of the "record" keyword
	LBrace     Span
	Fields     []*Field
	RBrace     Span
}

func (t *RecordType) typeNode() {}

// Span returns the record's span from "record" through its closing brace.
func (t *RecordType) Span() Span { return Span{t.RecordSpan.Start, t.RBrace.End} }

// Field is one record field: "IDENT type-specifier ;".
type Field struct {
	Doc  string // documentation slot; empty after parsing
	Name *Ident
	Type TypeExpr
	Semi Span
}

// Span returns the field's span from its name through its semicolon.
func (f *Field) Span() Span { return Span{f.Name.span.Start, f.Semi.End} }
