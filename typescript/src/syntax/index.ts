export type {
    Comment,
    Declaration,
    ExceptionDecl,
    Field,
    Ident,
    InterfaceFile,
    ListType,
    NamedType,
    Param,
    PrimitiveType,
    ProcDecl,
    RecordType,
    TypeDecl,
    TypeExpr,
} from "./ast.js";
export { parseInterface } from "./parser.js";
export { Scanner } from "./scanner.js";
export {
    SourceFile,
    SyntaxDiagnostic,
} from "./source.js";
export {
    TokenKind,
    tokenDescription,
    tokenExpected,
    tokenFromIdentifier,
    tokenLiteral,
    tokenToString,
} from "./token.js";
export type {
    Position,
} from "./source.js";
export type {
    Span,
    Token,
} from "./token.js";
