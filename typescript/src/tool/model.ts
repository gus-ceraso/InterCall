import type {
    Declaration,
    Field,
    InterfaceFile,
    Param,
    ProcDecl,
    TypeDecl,
    TypeExpr,
} from "../syntax/index.js";

export interface ImportDeclarationFact {
    readonly declaration: Declaration;
    readonly nativeName: string | undefined;
}

export interface ImportFieldFact {
    readonly field: Field;
    readonly nativeName: string;
}

export interface ImportParameterFact {
    readonly parameter: Param;
    readonly nativeName: string;
}

export interface ImportGeneration {
    readonly source: InterfaceFile;
    readonly declarations: readonly ImportDeclarationFact[];
    readonly fields: readonly ImportFieldFact[];
    readonly parameters: readonly ImportParameterFact[];
}

export interface ExportTypeFact {
    readonly declaration: TypeDecl;
    readonly nativeName: string;
    readonly compilerType: unknown;
}

export interface ExportProcedureFact {
    readonly declaration: ProcDecl;
    readonly sourceModule: string;
    readonly sourceName: string;
    readonly compilerSignature: unknown;
}

export interface ExportExceptionFact {
    readonly declaration: Extract<Declaration, { readonly kind: "exception-decl" }>;
    readonly sourceModule: string;
    readonly sourceName: string;
    readonly compilerType: unknown;
}

export interface ExportGeneration {
    readonly source: InterfaceFile;
    readonly namedTypes: readonly ExportTypeFact[];
    readonly exceptions: readonly ExportExceptionFact[];
    readonly procedures: readonly ExportProcedureFact[];
}

export interface CodecRootFact {
    readonly type: TypeExpr;
    readonly wirePath: readonly string[];
}
