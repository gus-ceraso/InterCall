import type { Span, TokenKind } from "./token.js";
import type { SourceFile } from "./source.js";

export interface Ident {
    readonly name: string;
    readonly span: Span;
}

export interface PrimitiveType {
    readonly kind: "primitive";
    readonly primitive: TokenKind;
    readonly span: Span;
    doc: string;
}

export interface NamedType {
    readonly kind: "named";
    readonly name: Ident;
    doc: string;
}

export interface ListType {
    readonly kind: "list";
    readonly listSpan: Span;
    elem: TypeExpr;
    doc: string;
}

export interface RecordType {
    readonly kind: "record";
    readonly recordSpan: Span;
    readonly lBrace: Span;
    fields: Field[];
    rBrace: Span;
    doc: string;
}

export type TypeExpr = PrimitiveType | NamedType | ListType | RecordType;

export interface Field {
    readonly name: Ident;
    type: TypeExpr;
    semi: Span;
    doc: string;
}

export interface Param {
    readonly name: Ident;
    type: TypeExpr;
    semi: Span;
    doc: string;
}

export interface TypeDecl {
    readonly kind: "type-decl";
    readonly typeSpan: Span;
    readonly name: Ident;
    type: TypeExpr;
    readonly semi: Span;
    doc: string;
}

export interface ExceptionDecl {
    readonly kind: "exception-decl";
    readonly exceptionSpan: Span;
    readonly name: Ident;
    type: TypeExpr | undefined;
    readonly semi: Span;
    doc: string;
}

export interface ProcDecl {
    readonly kind: "procedure-decl";
    readonly procedureSpan: Span;
    readonly name: Ident;
    readonly lBrace: Span;
    params: Param[];
    readonly rBrace: Span;
    result: TypeExpr | undefined;
    readonly semi: Span;
    doc: string;
}

export type Declaration = TypeDecl | ExceptionDecl | ProcDecl;

export interface Comment {
    readonly span: Span;
    readonly text: string;
}

export interface InterfaceFile {
    readonly source: SourceFile;
    readonly declarations: Declaration[];
    readonly comments: Comment[];
}
