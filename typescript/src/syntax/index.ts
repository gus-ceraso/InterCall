export { attachDocumentation, normalizeDocumentation } from "./docs.js";
export { formatInterface } from "./format.js";
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
export { declarationKey } from "./key.js";
export type { KeyDeclaration, KeyKind } from "./key.js";
export { parseInterface } from "./parser.js";
export { Scanner } from "./scanner.js";
export {
    SourceFile,
    SyntaxDiagnostic,
} from "./source.js";
export { validateInterface } from "./validator.js";
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
