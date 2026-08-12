package syntax

import (
	"strings"
	"testing"
)

// scanAll scans every token of src, returning the tokens and the first
// error, if any.
func scanAll(t *testing.T, src string) ([]Token, error) {
	t.Helper()
	s := NewScanner(NewFile("f", []byte(src)))
	var toks []Token
	for {
		tok, err := s.Next()
		if err != nil {
			return toks, err
		}
		toks = append(toks, tok)
		if tok.Kind == TokEOF {
			return toks, nil
		}
	}
}

func TestScanBasicTokens(t *testing.T) {
	src := "type user record { name string; };"
	toks, err := scanAll(t, src)
	if err != nil {
		t.Fatal(err)
	}
	want := []Token{
		{Kind: TokType, Span: Span{0, 4}, Lit: "type"},
		{Kind: TokIdent, Span: Span{5, 9}, Lit: "user"},
		{Kind: TokRecord, Span: Span{10, 16}, Lit: "record"},
		{Kind: TokLBrace, Span: Span{17, 18}},
		{Kind: TokIdent, Span: Span{19, 23}, Lit: "name"},
		{Kind: TokString, Span: Span{24, 30}, Lit: "string"},
		{Kind: TokSemicolon, Span: Span{30, 31}},
		{Kind: TokRBrace, Span: Span{32, 33}},
		{Kind: TokSemicolon, Span: Span{33, 34}},
		{Kind: TokEOF, Span: Span{34, 34}},
	}
	if len(toks) != len(want) {
		t.Fatalf("got %d tokens, want %d: %v", len(toks), len(want), toks)
	}
	for i := range want {
		if toks[i] != want[i] {
			t.Errorf("token %d = %+v, want %+v", i, toks[i], want[i])
		}
	}
}

func TestScanEmptyInput(t *testing.T) {
	for _, src := range []string{"", "   ", "\t\r\n\f\v", " \t \r\n \f\v "} {
		toks, err := scanAll(t, src)
		if err != nil {
			t.Fatalf("src %q: %v", src, err)
		}
		if len(toks) != 1 || toks[0].Kind != TokEOF {
			t.Errorf("src %q: tokens = %v, want single EOF", src, toks)
		}
		if toks[0].Span != (Span{len(src), len(src)}) {
			t.Errorf("src %q: EOF span = %v, want [%d,%d)", src, toks[0].Span, len(src), len(src))
		}
	}
}

func TestScanKeywords(t *testing.T) {
	src := "type exception procedure list record int8 int16 int32 int64 uint8 uint16 uint32 uint64 float32 float64 string bytes"
	toks, err := scanAll(t, src)
	if err != nil {
		t.Fatal(err)
	}
	wantKinds := []TokenKind{
		TokType, TokException, TokProcedure, TokList, TokRecord,
		TokInt8, TokInt16, TokInt32, TokInt64,
		TokUint8, TokUint16, TokUint32, TokUint64,
		TokFloat32, TokFloat64, TokString, TokBytes,
		TokEOF,
	}
	if len(toks) != len(wantKinds) {
		t.Fatalf("got %d tokens, want %d", len(toks), len(wantKinds))
	}
	for i, kind := range wantKinds {
		if toks[i].Kind != kind {
			t.Errorf("token %d = %v, want %v", i, toks[i].Kind, kind)
		}
	}
}

func TestScanCaseSensitiveKeywords(t *testing.T) {
	// Only the exact lowercase spellings are reserved.
	for _, src := range []string{"Type", "TYPE", "Procedure", "LIST", "Record", "Int8", "Bytes", "type_", "_type"} {
		toks, err := scanAll(t, src)
		if err != nil {
			t.Fatalf("src %q: %v", src, err)
		}
		if toks[0].Kind != TokIdent {
			t.Errorf("src %q: first token = %v, want TokIdent", src, toks[0].Kind)
		}
		if toks[0].Lit != src {
			t.Errorf("src %q: literal = %q, want %q", src, toks[0].Lit, src)
		}
	}
}

func TestScanKeywordBoundaries(t *testing.T) {
	tests := []struct {
		src   string
		kinds []TokenKind
		lits  []string
	}{
		{"listlist uint8", []TokenKind{TokIdent, TokUint8, TokEOF}, []string{"listlist", "uint8", ""}},
		{"list list", []TokenKind{TokList, TokList, TokEOF}, []string{"list", "list", ""}},
		{"int8x", []TokenKind{TokIdent, TokEOF}, []string{"int8x", ""}},
		{"x_1a2", []TokenKind{TokIdent, TokEOF}, []string{"x_1a2", ""}},
		{"record_record", []TokenKind{TokIdent, TokEOF}, []string{"record_record", ""}},
		{"procedure;", []TokenKind{TokProcedure, TokSemicolon, TokEOF}, []string{"procedure", "", ""}},
	}
	for _, tt := range tests {
		toks, err := scanAll(t, tt.src)
		if err != nil {
			t.Fatalf("src %q: %v", tt.src, err)
		}
		if len(toks) != len(tt.kinds) {
			t.Fatalf("src %q: got %d tokens, want %d: %v", tt.src, len(toks), len(tt.kinds), toks)
		}
		for i := range tt.kinds {
			if toks[i].Kind != tt.kinds[i] {
				t.Errorf("src %q token %d: kind %v, want %v", tt.src, i, toks[i].Kind, tt.kinds[i])
			}
			if toks[i].Lit != tt.lits[i] {
				t.Errorf("src %q token %d: literal %q, want %q", tt.src, i, toks[i].Lit, tt.lits[i])
			}
		}
	}
}

func TestScanWhitespaceForms(t *testing.T) {
	// All six whitespace bytes separate tokens and may repeat freely.
	src := "type\u0009x\u000cuint8\vy;\r\n\u000bexception\u0020e;"
	toks, err := scanAll(t, src)
	if err != nil {
		t.Fatal(err)
	}
	var kinds []TokenKind
	for _, tok := range toks {
		kinds = append(kinds, tok.Kind)
	}
	want := []TokenKind{TokType, TokIdent, TokUint8, TokIdent, TokSemicolon, TokException, TokIdent, TokSemicolon, TokEOF}
	if len(kinds) != len(want) {
		t.Fatalf("kinds = %v, want %v", kinds, want)
	}
	for i := range want {
		if kinds[i] != want[i] {
			t.Errorf("token %d = %v, want %v", i, kinds[i], want[i])
		}
	}
}

func TestScanComments(t *testing.T) {
	tests := []struct {
		src  string
		lits []string
	}{
		{"/*a*/", []string{"a"}},
		{"/* a user */", []string{" a user "}},
		{"/* a /* b */", []string{" a /* b "}},
		{"/*a*//*b*/", []string{"a", "b"}},
		{"/* */ /* */", []string{" ", " "}},
		{"/*\nmulti\nline\n*/", []string{"\nmulti\nline\n"}},
		{"/* héllo 世界 */", []string{" héllo 世界 "}},
		{"/* \uFEFF */", []string{" \uFEFF "}},
		{"type/*c*/x", []string{"c"}},
		{"/*\u0000*/", []string{"\u0000"}},
	}
	for _, tt := range tests {
		toks, err := scanAll(t, tt.src)
		if err != nil {
			t.Fatalf("src %q: %v", tt.src, err)
		}
		var lits []string
		for _, tok := range toks {
			if tok.Kind == TokComment {
				lits = append(lits, tok.Lit)
			}
		}
		if strings.Join(lits, "|") != strings.Join(tt.lits, "|") {
			t.Errorf("src %q: comment bodies = %q, want %q", tt.src, lits, tt.lits)
		}
		// Every comment token must span exactly "/*" + body + "*/".
		for _, tok := range toks {
			if tok.Kind == TokComment {
				span := tok.Span
				if span.End-span.Start != len("/*"+tok.Lit+"*/") {
					t.Errorf("src %q: comment span %v does not cover %q", tt.src, span, "/*"+tok.Lit+"*/")
				}
			}
		}
	}
}

func TestScanCommentSeparatesTokens(t *testing.T) {
	// Whitespace is not required where a comment already separates tokens.
	toks, err := scanAll(t, "list/*c*/uint8")
	if err != nil {
		t.Fatal(err)
	}
	want := []TokenKind{TokList, TokComment, TokUint8, TokEOF}
	if len(toks) != len(want) {
		t.Fatalf("tokens = %v, want kinds %v", toks, want)
	}
	for i := range want {
		if toks[i].Kind != want[i] {
			t.Errorf("token %d = %v, want %v", i, toks[i].Kind, want[i])
		}
	}
}

func TestScanCommentErrors(t *testing.T) {
	tests := []struct {
		name     string
		src      string
		wantOff  int
		wantMsg  string
		wantSpan Span
	}{
		{"empty", "/*", 0, "comment not terminated", Span{0, 2}},
		{"text", "/* never closed", 0, "comment not terminated", Span{0, 15}},
		{"star at eof", "/* *", 0, "comment not terminated", Span{0, 4}},
		{"close without open", "a/", 1, "invalid character '/'", Span{1, 2}},
		{"bad utf8 in closed comment", "/* \x80 */", 3, "invalid UTF-8 encoding", Span{3, 4}},
		{"bad utf8 wins over unterminated", "/* \x80", 3, "invalid UTF-8 encoding", Span{3, 4}},
		{"bad utf8 before terminator", "/*a\xC3", 3, "invalid UTF-8 encoding", Span{3, 4}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := scanAll(t, tt.src)
			if err == nil {
				t.Fatal("scan succeeded, want error")
			}
			e, ok := err.(*Error)
			if !ok {
				t.Fatalf("error type %T, want *Error", err)
			}
			if e.Pos.Offset != tt.wantOff {
				t.Errorf("offset = %d, want %d", e.Pos.Offset, tt.wantOff)
			}
			if e.Msg != tt.wantMsg {
				t.Errorf("msg = %q, want %q", e.Msg, tt.wantMsg)
			}
			if e.Span != tt.wantSpan {
				t.Errorf("span = %v, want %v", e.Span, tt.wantSpan)
			}
		})
	}
}

func TestScanInvalidUTF8AtFirstBadByte(t *testing.T) {
	tests := []struct {
		name    string
		src     string
		wantOff int
	}{
		{"lone continuation", "\x80", 0},
		{"after ascii", "ab\x80", 2},
		{"overlong c0", "\xC0\xAF", 0},
		{"overlong c1", "\xC1\xBF", 0},
		{"truncated 2-byte", "a\xC3", 1},
		{"bad continuation", "\xE2\x28\xA1", 0},
		{"surrogate ed", "\xED\xA0\x80", 0},
		{"too big f4", "\xF4\x90\x80\x80", 0},
		{"truncated 3-byte", "a\xE2\x82", 1},
		{"truncated bom", "a\xEF\xBB", 1},
		{"invalid after token", "type x; \xF0\x28\x8C\x28", 8},
		{"invalid in param name position", "procedure p { \x80 string; };", 14},
		{"invalid between decls", "type x uint8;\n\x80", 14},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := scanAll(t, tt.src)
			if err == nil {
				t.Fatal("scan succeeded, want error")
			}
			e, ok := err.(*Error)
			if !ok {
				t.Fatalf("error type %T, want *Error", err)
			}
			if e.Msg != "invalid UTF-8 encoding" {
				t.Errorf("msg = %q, want %q", e.Msg, "invalid UTF-8 encoding")
			}
			if e.Pos.Offset != tt.wantOff {
				t.Errorf("offset = %d, want %d (line %d col %d)", e.Pos.Offset, tt.wantOff, e.Pos.Line, e.Pos.Column)
			}
		})
	}
}

func TestScanBOMRejection(t *testing.T) {
	tests := []struct {
		name    string
		src     string
		wantOff int
	}{
		{"bom at start", "\xEF\xBB\xBFtype x uint8;", 0},
		{"bom after trivia", "  \t\xEF\xBB\xBFtype x uint8;", 3},
		{"bom between decls", "type x uint8;\n\xEF\xBB\xBFtype y uint8;", 14},
		{"bom after token", "type\xEF\xBB\xBFx;", 4},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := scanAll(t, tt.src)
			if err == nil {
				t.Fatal("scan succeeded, want error")
			}
			e, ok := err.(*Error)
			if !ok {
				t.Fatalf("error type %T, want *Error", err)
			}
			if e.Msg != "invalid byte-order mark" {
				t.Errorf("msg = %q, want %q", e.Msg, "invalid byte-order mark")
			}
			if e.Pos.Offset != tt.wantOff {
				t.Errorf("offset = %d, want %d", e.Pos.Offset, tt.wantOff)
			}
			if e.Span != (Span{tt.wantOff, tt.wantOff + 3}) {
				t.Errorf("span = %v, want %v", e.Span, Span{tt.wantOff, tt.wantOff + 3})
			}
		})
	}
}

func TestScanInvalidCharacters(t *testing.T) {
	tests := []struct {
		name    string
		src     string
		wantOff int
		wantMsg string
	}{
		{"punct", "!", 0, "invalid character '!'"},
		{"at", "@", 0, "invalid character '@'"},
		{"backslash", "\\", 0, "invalid character '\\\\'"},
		{"double quote", "\"", 0, "invalid character '\"'"},
		{"hash", "#", 0, "invalid character '#'"},
		{"control", "\x01", 0, "invalid character '\\x01'"},
		{"ascii in comment only ok", "/* ok */", 0, ""},
		{"non-ascii letter", "type héllo;", 6, "invalid character 'é'"},
		{"non-ascii whitespace", "a\u00A0b", 1, "invalid character '\\u00a0'"},
		{"snowman", "☃", 0, "invalid character '☃'"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := scanAll(t, tt.src)
			if tt.wantMsg == "" {
				if err != nil {
					t.Fatalf("unexpected error: %v", err)
				}
				return
			}
			if err == nil {
				t.Fatal("scan succeeded, want error")
			}
			e, ok := err.(*Error)
			if !ok {
				t.Fatalf("error type %T, want *Error", err)
			}
			if e.Msg != tt.wantMsg {
				t.Errorf("msg = %q, want %q", e.Msg, tt.wantMsg)
			}
			// The invalid character position must be its own start offset.
			if e.Pos.Offset != tt.wantOff {
				t.Errorf("offset = %d, want %d", e.Pos.Offset, tt.wantOff)
			}
		})
	}
}

func TestScanInvalidCharacterAfterTokens(t *testing.T) {
	src := "type x uint8; !"
	toks, err := scanAll(t, src)
	if err == nil {
		t.Fatalf("scan succeeded with tokens %v", toks)
	}
	e := err.(*Error)
	if e.Pos.Offset != 14 {
		t.Errorf("offset = %d, want 14", e.Pos.Offset)
	}
	if e.Pos.Line != 1 || e.Pos.Column != 15 {
		t.Errorf("position = %d:%d, want 1:15", e.Pos.Line, e.Pos.Column)
	}
}

func TestScanStickyError(t *testing.T) {
	s := NewScanner(NewFile("f", []byte("a\x80b")))
	if _, err := s.Next(); err != nil {
		t.Fatal(err)
	}
	_, err := s.Next()
	if err == nil {
		t.Fatal("second Next succeeded")
	}
	msg := err.Error()
	// Every further call returns the identical error and never a token.
	for i := 0; i < 3; i++ {
		tok, again := s.Next()
		if again == nil || again.Error() != msg {
			t.Fatalf("call %d: error = %v, want sticky %q", i, again, msg)
		}
		if tok.Kind != TokInvalid {
			t.Fatalf("call %d: token = %v, want zero token", i, tok)
		}
	}
}
