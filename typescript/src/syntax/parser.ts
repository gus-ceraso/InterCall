import { SourceFile, SyntaxDiagnostic } from "./source.js";
import { Scanner } from "./scanner.js";
import {
    TokenKind,
    tokenDescription,
    tokenExpected,
} from "./token.js";
import type {
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
import type { Span, Token } from "./token.js";

const primitiveKinds = new Set<TokenKind>([
    TokenKind.Int8,
    TokenKind.Int16,
    TokenKind.Int32,
    TokenKind.Int64,
    TokenKind.Uint8,
    TokenKind.Uint16,
    TokenKind.Uint32,
    TokenKind.Uint64,
    TokenKind.Float32,
    TokenKind.Float64,
    TokenKind.String,
    TokenKind.Bytes,
]);

export function parseInterface(name: string, bytes: Uint8Array): InterfaceFile {
    const source = new SourceFile(name, bytes);
    const parser = new Parser(source);
    return parser.parse();
}

class Parser {
    private readonly scanner: Scanner;
    private readonly comments: Comment[] = [];
    private token: Token;

    constructor(private readonly source: SourceFile) {
        this.scanner = new Scanner(source);
        this.token = this.readToken();
    }

    parse(): InterfaceFile {
        const declarations: Declaration[] = [];
        while (this.token.kind !== TokenKind.EOF) {
            declarations.push(this.parseDeclaration());
        }
        return { source: this.source, declarations, comments: this.comments };
    }

    private readToken(): Token {
        while (true) {
            const token = this.scanner.next();
            if (token.kind !== TokenKind.Comment) return token;
            this.comments.push({ span: token.span, text: token.literal });
        }
    }

    private advance(): Token {
        const previous = this.token;
        this.token = this.readToken();
        return previous;
    }

    private expect(kind: TokenKind): Token {
        if (this.token.kind !== kind) {
            throw this.error(
                this.token.span,
                `expected ${tokenExpected(kind)}, found ${tokenDescription(this.token.kind)}`,
            );
        }
        return this.advance();
    }

    private expectIdentifier(): Ident {
        if (this.token.kind !== TokenKind.Ident) {
            throw this.error(this.token.span, `expected identifier, found ${tokenDescription(this.token.kind)}`);
        }
        const token = this.advance();
        return { name: token.literal, span: token.span };
    }

    private parseDeclaration(): Declaration {
        switch (this.token.kind) {
            case TokenKind.Type: return this.parseTypeDeclaration();
            case TokenKind.Exception: return this.parseExceptionDeclaration();
            case TokenKind.Procedure: return this.parseProcedureDeclaration();
            default:
                throw this.error(this.token.span, `expected declaration, found ${tokenDescription(this.token.kind)}`);
        }
    }

    private parseTypeDeclaration(): TypeDecl {
        const typeSpan = this.expect(TokenKind.Type).span;
        const name = this.expectIdentifier();
        const type = this.parseType();
        const semi = this.expect(TokenKind.Semicolon).span;
        return { kind: "type-decl", typeSpan, name, type, semi, doc: "" };
    }

    private parseExceptionDeclaration(): ExceptionDecl {
        const exceptionSpan = this.expect(TokenKind.Exception).span;
        const name = this.expectIdentifier();
        const type = this.token.kind === TokenKind.Semicolon ? undefined : this.parseType();
        const semi = this.expect(TokenKind.Semicolon).span;
        return { kind: "exception-decl", exceptionSpan, name, type, semi, doc: "" };
    }

    private parseProcedureDeclaration(): ProcDecl {
        const procedureSpan = this.expect(TokenKind.Procedure).span;
        const name = this.expectIdentifier();
        const lBrace = this.expect(TokenKind.LBrace).span;
        const params: Param[] = [];
        while (this.token.kind !== TokenKind.RBrace) {
            if (this.token.kind === TokenKind.EOF) {
                throw this.error(this.token.span, "expected '}' or parameter, found end of file");
            }
            const paramName = this.expectIdentifier();
            const type = this.parseType();
            const semi = this.expect(TokenKind.Semicolon).span;
            params.push({ name: paramName, type, semi, doc: "" });
        }
        const rBrace = this.expect(TokenKind.RBrace).span;
        const result = (this.token.kind as TokenKind) === TokenKind.Semicolon
            ? undefined
            : this.parseType();
        const semi = this.expect(TokenKind.Semicolon).span;
        return {
            kind: "procedure-decl",
            procedureSpan,
            name,
            lBrace,
            params,
            rBrace,
            result,
            semi,
            doc: "",
        };
    }

    private parseType(): TypeExpr {
        type Frame =
            | { readonly kind: "list"; readonly list: ListType }
            | { readonly kind: "record"; readonly record: RecordType }
            | { readonly kind: "field"; readonly field: Field };

        const stack: Frame[] = [];
        let complete: TypeExpr | undefined;
        while (true) {
            if (complete !== undefined) {
                const frame = stack.at(-1);
                if (frame === undefined) return complete;
                stack.pop();
                if (frame.kind === "list") {
                    frame.list.elem = complete;
                    complete = frame.list;
                    continue;
                }
                if (frame.kind === "field") {
                    frame.field.type = complete;
                    frame.field.semi = this.expect(TokenKind.Semicolon).span;
                    complete = undefined;
                    continue;
                }
                throw new Error("syntax parser: record frame cannot consume a completed type");
            }

            const recordFrame = stack.at(-1);
            if (recordFrame?.kind === "record") {
                if (this.token.kind === TokenKind.Ident) {
                    const fieldName = this.expectIdentifier();
                    const field: Field = { name: fieldName, type: undefined as never, semi: undefined as never, doc: "" };
                    recordFrame.record.fields.push(field);
                    stack.push({ kind: "field", field });
                    continue;
                }
                if (this.token.kind === TokenKind.RBrace) {
                    recordFrame.record.rBrace = this.advance().span;
                    stack.pop();
                    complete = recordFrame.record;
                    continue;
                }
                throw this.error(this.token.span, `expected identifier or '}', found ${tokenDescription(this.token.kind)}`);
            }

            if (primitiveKinds.has(this.token.kind)) {
                const token = this.advance();
                complete = {
                    kind: "primitive",
                    primitive: token.kind,
                    span: token.span,
                    doc: "",
                } satisfies PrimitiveType;
                continue;
            }
            if (this.token.kind === TokenKind.Ident) {
                const name = this.expectIdentifier();
                complete = { kind: "named", name, doc: "" } satisfies NamedType;
                continue;
            }
            if (this.token.kind === TokenKind.List) {
                const listSpan = this.advance().span;
                const list: ListType = { kind: "list", listSpan, elem: undefined as never, doc: "" };
                stack.push({ kind: "list", list });
                continue;
            }
            if (this.token.kind === TokenKind.Record) {
                const recordSpan = this.advance().span;
                const lBrace = this.expect(TokenKind.LBrace).span;
                const record: RecordType = {
                    kind: "record",
                    recordSpan,
                    lBrace,
                    fields: [],
                    rBrace: undefined as never,
                    doc: "",
                };
                stack.push({ kind: "record", record });
                continue;
            }
            throw this.error(this.token.span, `expected type, found ${tokenDescription(this.token.kind)}`);
        }
    }

    private error(span: Span, message: string): SyntaxDiagnostic {
        return new SyntaxDiagnostic(this.source.name, this.source.position(span.start), span, message);
    }
}
