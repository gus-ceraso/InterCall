package syntax

// TokenKind identifies one lexical token kind.
type TokenKind uint8

// Token kinds. Keywords and primitive names keep distinct kinds so that
// reserved-token positions are exact: "list", "record", "type",
// "exception", "procedure", and every primitive name are unavailable in
// identifier positions regardless of spelling capitalization.
const (
	TokInvalid TokenKind = iota
	TokEOF
	TokIdent
	TokComment

	// Reserved words.
	TokType
	TokException
	TokProcedure
	TokList
	TokRecord

	// Primitive type names.
	TokInt8
	TokInt16
	TokInt32
	TokInt64
	TokUint8
	TokUint16
	TokUint32
	TokUint64
	TokFloat32
	TokFloat64
	TokString
	TokBytes

	// Punctuation.
	TokLBrace
	TokRBrace
	TokSemicolon
)

// String returns a stable bare name for the kind: the source spelling for
// keywords and primitives, "identifier", "comment", "end of file", or
// "invalid token".
func (k TokenKind) String() string {
	if lit, ok := tokenLiterals[k]; ok {
		return lit
	}
	switch k {
	case TokInvalid:
		return "invalid token"
	case TokEOF:
		return "end of file"
	case TokIdent:
		return "identifier"
	case TokComment:
		return "comment"
	}
	return "unknown token"
}

// literal returns the source spelling of a kind for parser diagnostics,
// quoted for tokens that appear literally.
func (k TokenKind) literal() string {
	if lit, ok := tokenLiterals[k]; ok {
		return "'" + lit + "'"
	}
	switch k {
	case TokEOF:
		return "end of file"
	case TokIdent:
		return "identifier"
	case TokComment:
		return "comment"
	}
	return "invalid token"
}

// tokenLiterals maps keyword and punctuation kinds to their exact source
// spellings. Identifiers and comments are not literals.
var tokenLiterals = map[TokenKind]string{
	TokType:      "type",
	TokException: "exception",
	TokProcedure: "procedure",
	TokList:      "list",
	TokRecord:    "record",
	TokInt8:      "int8",
	TokInt16:     "int16",
	TokInt32:     "int32",
	TokInt64:     "int64",
	TokUint8:     "uint8",
	TokUint16:    "uint16",
	TokUint32:    "uint32",
	TokUint64:    "uint64",
	TokFloat32:   "float32",
	TokFloat64:   "float64",
	TokString:    "string",
	TokBytes:     "bytes",
	TokLBrace:    "{",
	TokRBrace:    "}",
	TokSemicolon: ";",
}

// keywords maps every reserved word to its token kind. The zero value
// TokInvalid marks a non-keyword identifier.
var keywords = map[string]TokenKind{
	"type":      TokType,
	"exception": TokException,
	"procedure": TokProcedure,
	"list":      TokList,
	"record":    TokRecord,
	"int8":      TokInt8,
	"int16":     TokInt16,
	"int32":     TokInt32,
	"int64":     TokInt64,
	"uint8":     TokUint8,
	"uint16":    TokUint16,
	"uint32":    TokUint32,
	"uint64":    TokUint64,
	"float32":   TokFloat32,
	"float64":   TokFloat64,
	"string":    TokString,
	"bytes":     TokBytes,
}

// Token is one lexical token with its exact source span.
//
// Lit holds the exact source text for TokIdent (the identifier) and the raw
// body between the delimiters for TokComment; it is empty for every other
// kind.
type Token struct {
	Kind TokenKind
	Span Span
	Lit  string
}

// String renders the token for debugging and diagnostics, e.g. "'list'",
// "identifier 'name'", or "end of file".
func (t Token) String() string {
	switch t.Kind {
	case TokEOF:
		return "end of file"
	case TokIdent, TokComment:
		return t.Kind.String() + " '" + t.Lit + "'"
	}
	return t.Kind.literal()
}
