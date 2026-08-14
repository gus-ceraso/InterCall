export enum TokenKind {
    Invalid,
    EOF,
    Ident,
    Comment,
    Type,
    Exception,
    Procedure,
    List,
    Record,
    Int8,
    Int16,
    Int32,
    Int64,
    Uint8,
    Uint16,
    Uint32,
    Uint64,
    Float32,
    Float64,
    String,
    Bytes,
    LBrace,
    RBrace,
    Semicolon,
}

const literals = new Map<TokenKind, string>([
    [TokenKind.Type, "type"],
    [TokenKind.Exception, "exception"],
    [TokenKind.Procedure, "procedure"],
    [TokenKind.List, "list"],
    [TokenKind.Record, "record"],
    [TokenKind.Int8, "int8"],
    [TokenKind.Int16, "int16"],
    [TokenKind.Int32, "int32"],
    [TokenKind.Int64, "int64"],
    [TokenKind.Uint8, "uint8"],
    [TokenKind.Uint16, "uint16"],
    [TokenKind.Uint32, "uint32"],
    [TokenKind.Uint64, "uint64"],
    [TokenKind.Float32, "float32"],
    [TokenKind.Float64, "float64"],
    [TokenKind.String, "string"],
    [TokenKind.Bytes, "bytes"],
    [TokenKind.LBrace, "{"],
    [TokenKind.RBrace, "}"],
    [TokenKind.Semicolon, ";"],
]);

const keywords = new Map<string, TokenKind>(
    [...literals.entries()]
        .filter(([kind]) => kind >= TokenKind.Type && kind <= TokenKind.Bytes)
        .map(([kind, literal]) => [literal, kind]),
);

export interface Span {
    readonly start: number;
    readonly end: number;
}

export interface Token {
    readonly kind: TokenKind;
    readonly span: Span;
    readonly literal: string;
}

export function tokenLiteral(kind: TokenKind): string | undefined {
    return literals.get(kind);
}

export function tokenDescription(kind: TokenKind): string {
    const literal = literals.get(kind);
    if (literal !== undefined) return literal;
    switch (kind) {
        case TokenKind.Invalid: return "invalid token";
        case TokenKind.EOF: return "end of file";
        case TokenKind.Ident: return "identifier";
        case TokenKind.Comment: return "comment";
        default: return "unknown token";
    }
}

export function tokenExpected(kind: TokenKind): string {
    const literal = literals.get(kind);
    if (literal !== undefined) return `'${literal}'`;
    return tokenDescription(kind);
}

export function tokenFromIdentifier(literal: string): TokenKind {
    return keywords.get(literal) ?? TokenKind.Ident;
}

export function tokenToString(token: Token): string {
    if (token.kind === TokenKind.EOF) return "end of file";
    if (token.kind === TokenKind.Ident || token.kind === TokenKind.Comment) {
        return `${tokenDescription(token.kind)} '${token.literal}'`;
    }
    return tokenExpected(token.kind);
}
