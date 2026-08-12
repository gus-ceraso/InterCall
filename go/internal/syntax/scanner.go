package syntax

import (
	"fmt"
	"strconv"
	"unicode/utf8"
)

// Scanner is a lexical scanner for one interface file.
//
// Next returns tokens in source order, skipping whitespace but returning
// comments as TokComment tokens. The scanner validates UTF-8 and the BOM
// rule everywhere: a byte-order mark outside a comment, an invalid UTF-8
// sequence, or a character that starts no token is an error whose position
// is the exact offending byte offset. After the first error, every
// subsequent Next call returns the same error and a zero token.
//
// A Scanner is not safe for concurrent use.
type Scanner struct {
	file *File
	src  []byte
	off  int
	err  error
}

// NewScanner constructs a scanner over the file's exact source bytes.
func NewScanner(file *File) *Scanner {
	return &Scanner{file: file, src: file.src}
}

// Next returns the next token.
//
// The scanner always advances or reports an error, so arbitrary input
// cannot stall it. An unterminated comment is reported at the comment's
// opening offset; an invalid byte is reported at its own offset, which is
// the first invalid byte of the input from the scan position onward.
func (s *Scanner) Next() (Token, error) {
	if s.err != nil {
		return Token{}, s.err
	}
	for s.off < len(s.src) && isSpace(s.src[s.off]) {
		s.off++
	}
	if s.off >= len(s.src) {
		return Token{Kind: TokEOF, Span: Span{s.off, s.off}}, nil
	}
	start := s.off
	switch c := s.src[s.off]; {
	case c == '{':
		s.off++
		return Token{Kind: TokLBrace, Span: Span{start, s.off}}, nil
	case c == '}':
		s.off++
		return Token{Kind: TokRBrace, Span: Span{start, s.off}}, nil
	case c == ';':
		s.off++
		return Token{Kind: TokSemicolon, Span: Span{start, s.off}}, nil
	case c == '/':
		if s.off+1 < len(s.src) && s.src[s.off+1] == '*' {
			return s.scanComment()
		}
		return s.invalid()
	case isIdentStart(c):
		return s.scanIdent(), nil
	default:
		return s.invalid()
	}
}

// scanIdent scans one identifier or reserved word.
//
// Identifiers are ASCII: they start with an ASCII letter or '_' and
// continue with ASCII letters, digits, or '_'. The exact lowercase spellings
// of the reserved words are keyword tokens; every other spelling, including
// differently cased spellings such as "Procedure", is an identifier.
func (s *Scanner) scanIdent() Token {
	start := s.off
	for s.off < len(s.src) && isIdentPart(s.src[s.off]) {
		s.off++
	}
	lit := string(s.src[start:s.off])
	kind := keywords[lit] // TokInvalid (zero) when lit is not a keyword
	if kind == TokInvalid {
		kind = TokIdent
	}
	return Token{Kind: kind, Span: Span{start, s.off}, Lit: lit}
}

// scanComment scans one non-nesting block comment.
//
// The comment ends at the first "*/"; an inner "/*" is ordinary text.
// COMMENT_TEXT is any sequence of Unicode scalar values not containing
// "*/", so the body must be valid UTF-8. Invalid UTF-8 inside a comment is
// reported at its first bad byte, before the unterminated-comment error for
// a comment that never closes.
func (s *Scanner) scanComment() (Token, error) {
	start := s.off
	s.off += 2 // consume "/*"
	body := s.off
	for s.off < len(s.src) {
		if s.src[s.off] == '*' && s.off+1 < len(s.src) && s.src[s.off+1] == '/' {
			text := s.src[body:s.off]
			if i := firstInvalidUTF8(text); i >= 0 {
				return s.fail(body+i, body+i+1, "invalid UTF-8 encoding")
			}
			s.off += 2
			return Token{Kind: TokComment, Span: Span{start, s.off}, Lit: string(text)}, nil
		}
		s.off++
	}
	if i := firstInvalidUTF8(s.src[body:]); i >= 0 {
		return s.fail(body+i, body+i+1, "invalid UTF-8 encoding")
	}
	return s.fail(start, len(s.src), "comment not terminated")
}

// invalid reports a byte that starts no token.
//
// A valid rune is reported as an invalid character at its start offset, and
// the scanner advances past it. A byte that does not begin a valid UTF-8
// sequence is reported as invalid UTF-8 at that exact byte; the BOM is
// reported as an invalid byte-order mark wherever it appears outside a
// comment. The scanner always advances or records a sticky error, so
// arbitrary bytes cannot make it loop.
func (s *Scanner) invalid() (Token, error) {
	off := s.off
	r, size := utf8.DecodeRune(s.src[off:])
	if r == utf8.RuneError && size == 1 {
		return s.fail(off, off+1, "invalid UTF-8 encoding")
	}
	if r == '\uFEFF' {
		return s.fail(off, off+3, "invalid byte-order mark")
	}
	s.off += size
	return s.fail(off, off+size, "invalid character %s", strconv.QuoteRune(r))
}

// fail records the first scanner error and returns it.
func (s *Scanner) fail(offset, end int, format string, args ...any) (Token, error) {
	s.err = &Error{
		Filename: s.file.Name,
		Pos:      s.file.Position(offset),
		Span:     Span{offset, end},
		Msg:      fmt.Sprintf(format, args...),
	}
	return Token{}, s.err
}

// isSpace reports whether b is one of the six InterCall whitespace bytes:
// space, tab, carriage return, line feed, form feed, and vertical tab.
func isSpace(b byte) bool {
	switch b {
	case ' ', '\t', '\r', '\n', '\f', '\v':
		return true
	}
	return false
}

// isIdentStart reports whether b may begin an identifier.
func isIdentStart(b byte) bool {
	return b == '_' || ('a' <= b && b <= 'z') || ('A' <= b && b <= 'Z')
}

// isIdentPart reports whether b may continue an identifier.
func isIdentPart(b byte) bool {
	return isIdentStart(b) || ('0' <= b && b <= '9')
}

// firstInvalidUTF8 returns the offset of the first byte that does not begin
// a valid UTF-8 sequence in b, or -1 when b is valid UTF-8.
func firstInvalidUTF8(b []byte) int {
	for i := 0; i < len(b); {
		r, size := utf8.DecodeRune(b[i:])
		if r == utf8.RuneError && size == 1 {
			return i
		}
		i += size
	}
	return -1
}
