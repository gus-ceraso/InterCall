package syntax

import "fmt"

// Error is one syntax diagnostic with its exact source position.
//
// Pos is the one-based physical line and byte column of the diagnostic,
// derived from the exact input bytes; Span is the full offending byte range.
// Invalid UTF-8 points at its first invalid byte, EOF diagnostics sit at
// offset len(src), and parser diagnostics point at the start of the token
// that broke the grammar.
type Error struct {
	Filename string
	Pos      Position
	Span     Span
	Msg      string
}

// Error renders the diagnostic as "path:line:column: message", or
// "line:column: message" when no filename is set.
func (e *Error) Error() string {
	if e.Filename != "" {
		return fmt.Sprintf("%s:%d:%d: %s", e.Filename, e.Pos.Line, e.Pos.Column, e.Msg)
	}
	return fmt.Sprintf("%d:%d: %s", e.Pos.Line, e.Pos.Column, e.Msg)
}

// Parse parses one complete interface source file.
//
// Parse accepts exactly the grammar, lexical rules, and UTF-8 rules in
// README.md, including an empty file, a file containing only whitespace and
// comments, every nesting form of the type grammar, and every keyword
// boundary. The returned File owns the declarations in source order and
// every comment with its exact span and raw body. Documentation slots are
// left empty; attachment and normalization are a later phase.
//
// On the first error, Parse returns a nil File and a non-nil error that
// always has concrete type *Error. Parse never panics on input bytes; a
// malformed input always yields an error whose position is an exact offset
// of that input.
func Parse(filename string, src []byte) (file *File, err error) {
	f := NewFile(filename, src)
	p := &parser{file: f, scan: NewScanner(f)}
	defer func() {
		if r := recover(); r != nil {
			if _, ok := r.(bailout); ok {
				file, err = nil, p.err
				return
			}
			panic(r)
		}
	}()
	p.next()
	for p.tok.Kind != TokEOF {
		p.parseDecl()
	}
	if p.err != nil {
		p.bail()
	}
	return f, nil
}

// bailout unwinds the parser after an error has been recorded. It is never
// re-raised: Parse recovers it and returns the recorded error.
type bailout struct{}

// parser is a recursive-descent parser over one file's tokens.
type parser struct {
	file *File
	scan *Scanner
	tok  Token
	err  error
}

// next advances to the next token, recording every comment in the file's
// comment list. After a scanner error it records the error, sets the
// current token to EOF, and stops advancing.
func (p *parser) next() {
	for {
		tok, err := p.scan.Next()
		if err != nil {
			p.err = err
			p.tok = Token{Kind: TokEOF, Span: Span{p.file.Size, p.file.Size}}
			return
		}
		if tok.Kind == TokComment {
			p.file.Comments = append(p.file.Comments, &Comment{Span: tok.Span, Text: tok.Lit})
			continue
		}
		p.tok = tok
		return
	}
}

// bail stops parsing with the error already recorded in p.err.
func (p *parser) bail() {
	if p.err == nil {
		panic("syntax: internal error: bail without a recorded error")
	}
	panic(bailout{})
}

// errorf records the first parser error and stops parsing. The diagnostic
// points at the start of the token that broke the grammar.
func (p *parser) errorf(tok Token, format string, args ...any) {
	p.err = &Error{
		Filename: p.file.Name,
		Pos:      p.file.Position(tok.Span.Start),
		Span:     tok.Span,
		Msg:      fmt.Sprintf(format, args...),
	}
	panic(bailout{})
}

// expect consumes one token of the given kind or reports "expected X,
// found Y" at the offending token.
func (p *parser) expect(kind TokenKind) Token {
	if p.err != nil {
		p.bail()
	}
	if p.tok.Kind != kind {
		p.errorf(p.tok, "expected %s, found %s", kind.literal(), p.tok)
	}
	t := p.tok
	p.next()
	return t
}

// expectIdent consumes one identifier or reports "expected identifier,
// found Y" at the offending token, which is how reserved words in
// identifier positions are rejected with their exact positions.
func (p *parser) expectIdent() *Ident {
	if p.err != nil {
		p.bail()
	}
	if p.tok.Kind != TokIdent {
		p.errorf(p.tok, "expected identifier, found %s", p.tok)
	}
	id := p.tok
	p.next()
	return &Ident{Name: id.Lit, span: id.Span}
}

// parseDecl parses one declaration.
func (p *parser) parseDecl() {
	if p.err != nil {
		p.bail()
	}
	switch p.tok.Kind {
	case TokType:
		p.parseTypeDecl()
	case TokException:
		p.parseExceptionDecl()
	case TokProcedure:
		p.parseProcDecl()
	default:
		p.errorf(p.tok, "expected declaration, found %s", p.tok)
	}
}

// parseTypeDecl parses "type IDENT type-specifier ;".
func (p *parser) parseTypeDecl() {
	kw := p.expect(TokType)
	name := p.expectIdent()
	typ := p.parseTypeSpec()
	semi := p.expect(TokSemicolon)
	p.file.Decls = append(p.file.Decls, &TypeDecl{
		TypeSpan: kw.Span,
		Name:     name,
		Type:     typ,
		Semi:     semi.Span,
	})
}

// parseExceptionDecl parses "exception IDENT type-specifier? ;".
func (p *parser) parseExceptionDecl() {
	kw := p.expect(TokException)
	name := p.expectIdent()
	var typ TypeExpr
	if p.tok.Kind != TokSemicolon {
		typ = p.parseTypeSpec()
	}
	semi := p.expect(TokSemicolon)
	p.file.Decls = append(p.file.Decls, &ExceptionDecl{
		ExceptionSpan: kw.Span,
		Name:          name,
		Type:          typ,
		Semi:          semi.Span,
	})
}

// parseProcDecl parses "procedure IDENT { parameter* } type-specifier? ;".
func (p *parser) parseProcDecl() {
	kw := p.expect(TokProcedure)
	name := p.expectIdent()
	lbrace := p.expect(TokLBrace)
	var params []*Param
	for p.tok.Kind != TokRBrace {
		if p.err != nil {
			p.bail()
		}
		if p.tok.Kind == TokEOF {
			p.errorf(p.tok, "expected '}' or parameter, found %s", p.tok)
		}
		params = append(params, p.parseParam())
	}
	rbrace := p.expect(TokRBrace)
	var result TypeExpr
	if p.tok.Kind != TokSemicolon {
		result = p.parseTypeSpec()
	}
	semi := p.expect(TokSemicolon)
	p.file.Decls = append(p.file.Decls, &ProcDecl{
		ProcedureSpan: kw.Span,
		Name:          name,
		LBrace:        lbrace.Span,
		Params:        params,
		RBrace:        rbrace.Span,
		Result:        result,
		Semi:          semi.Span,
	})
}

// parseParam parses "IDENT type-specifier ;".
func (p *parser) parseParam() *Param {
	if p.err != nil {
		p.bail()
	}
	name := p.expectIdent()
	typ := p.parseTypeSpec()
	semi := p.expect(TokSemicolon)
	return &Param{Name: name, Type: typ, Semi: semi.Span}
}

// parseField parses "IDENT type-specifier ;".
func (p *parser) parseField() *Field {
	if p.err != nil {
		p.bail()
	}
	name := p.expectIdent()
	typ := p.parseTypeSpec()
	semi := p.expect(TokSemicolon)
	return &Field{Name: name, Type: typ, Semi: semi.Span}
}

// parseTypeSpec parses one type-specifier.
func (p *parser) parseTypeSpec() TypeExpr {
	if p.err != nil {
		p.bail()
	}
	switch p.tok.Kind {
	case TokInt8, TokInt16, TokInt32, TokInt64,
		TokUint8, TokUint16, TokUint32, TokUint64,
		TokFloat32, TokFloat64, TokString, TokBytes:
		t := p.tok
		p.next()
		return &PrimType{Kind: t.Kind, span: t.Span}
	case TokIdent:
		id := p.expectIdent()
		return &NamedType{Name: id}
	case TokList:
		return p.parseListType()
	case TokRecord:
		return p.parseRecordType()
	default:
		p.errorf(p.tok, "expected type, found %s", p.tok)
		return nil // unreachable
	}
}

// parseListType parses "list type-specifier".
func (p *parser) parseListType() *ListType {
	if p.err != nil {
		p.bail()
	}
	kw := p.expect(TokList)
	elem := p.parseTypeSpec()
	return &ListType{ListSpan: kw.Span, Elem: elem}
}

// parseRecordType parses "record { field* }".
func (p *parser) parseRecordType() *RecordType {
	if p.err != nil {
		p.bail()
	}
	kw := p.expect(TokRecord)
	lbrace := p.expect(TokLBrace)
	var fields []*Field
	for p.tok.Kind != TokRBrace {
		if p.err != nil {
			p.bail()
		}
		if p.tok.Kind == TokEOF {
			p.errorf(p.tok, "expected '}' or field, found %s", p.tok)
		}
		fields = append(fields, p.parseField())
	}
	rbrace := p.expect(TokRBrace)
	return &RecordType{RecordSpan: kw.Span, LBrace: lbrace.Span, Fields: fields, RBrace: rbrace.Span}
}
