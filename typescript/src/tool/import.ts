import type {
    Declaration,
    ExceptionDecl,
    Field,
    InterfaceFile,
    Param,
    ProcDecl,
    TypeDecl,
    TypeExpr,
} from "../syntax/index.js";
import { isCanonicalWireName, isValidTypeScriptIdentifier, wireToTypeScript } from "./name.js";

export interface ImportDeclarationRecord {
    readonly declaration: Declaration;
    readonly nativeName: string;
}
export interface ImportNamedTypeRecord { readonly declaration: TypeDecl; readonly nativeName: string; readonly type: TypeExpr; }
export interface ImportFieldRecord { readonly field: Field; readonly nativeName: string; }
export interface ImportParameterRecord { readonly procedure: ProcDecl; readonly parameter: Param; readonly nativeName: string; }
export interface ImportProcedureRecord { readonly declaration: ProcDecl; readonly nativeName: string; readonly parameters: readonly ImportParameterRecord[]; }
export interface ImportExceptionRecord { readonly declaration: ExceptionDecl; readonly nativeName: string; }
export interface ImportGenerationRecord {
    readonly source: InterfaceFile;
    readonly declarations: readonly ImportDeclarationRecord[];
    readonly namedTypes: readonly ImportNamedTypeRecord[];
    readonly fields: readonly ImportFieldRecord[];
    readonly parameters: readonly ImportParameterRecord[];
    readonly procedures: readonly ImportProcedureRecord[];
    readonly exceptions: readonly ImportExceptionRecord[];
}

export function buildImportGeneration(file: InterfaceFile): ImportGenerationRecord {
    const declarations = file.declarations.map((declaration) => ({ declaration, nativeName: declarationNativeName(declaration) }));
    const namedTypes = file.declarations
        .filter((declaration): declaration is TypeDecl => declaration.kind === "type-decl")
        .map((declaration) => ({ declaration, nativeName: defaultNativeName(declaration.name.name, "pascal"), type: declaration.type }));
    const fields: ImportFieldRecord[] = [];
    const visited = new Set<TypeExpr>();
    const work: TypeExpr[] = file.declarations.flatMap(declarationTypes);
    while (work.length > 0) {
        const type = work.pop()!;
        if (visited.has(type)) continue;
        visited.add(type);
        switch (type.kind) {
            case "primitive":
            case "named":
                break;
            case "list":
                work.push(type.elem);
                break;
            case "record":
                for (const field of type.fields) {
                    fields.push({ field, nativeName: defaultNativeName(field.name.name, "camel") });
                    work.push(field.type);
                }
                break;
        }
    }
    const parameters: ImportParameterRecord[] = [];
    const procedures = file.declarations
        .filter((declaration): declaration is ProcDecl => declaration.kind === "procedure-decl")
        .map((declaration) => {
            const procedureParameters = declaration.params.map((parameter) => {
                const record = { procedure: declaration, parameter, nativeName: defaultNativeName(parameter.name.name, "camel") };
                parameters.push(record);
                return record;
            });
            return { declaration, nativeName: defaultNativeName(declaration.name.name, "camel"), parameters: procedureParameters };
        });
    const exceptions = file.declarations
        .filter((declaration): declaration is ExceptionDecl => declaration.kind === "exception-decl")
        .map((declaration) => ({ declaration, nativeName: defaultNativeName(declaration.name.name, "pascal") }));
    return { source: file, declarations, namedTypes, fields, parameters, procedures, exceptions };
}

function defaultNativeName(wireName: string, nameCase: "camel" | "pascal"): string {
    if (isCanonicalWireName(wireName)) return wireToTypeScript(wireName, nameCase);
    if (isValidTypeScriptIdentifier(wireName)) return wireName;
    return `_${wireName}`;
}

function declarationNativeName(declaration: Declaration): string {
    switch (declaration.kind) {
        case "type-decl": return defaultNativeName(declaration.name.name, "pascal");
        case "exception-decl": return defaultNativeName(declaration.name.name, "pascal");
        case "procedure-decl": return defaultNativeName(declaration.name.name, "camel");
    }
}

function declarationTypes(declaration: Declaration): TypeExpr[] {
    if (declaration.kind === "procedure-decl") return [...declaration.params.map((parameter) => parameter.type), ...(declaration.result === undefined ? [] : [declaration.result])];
    return declaration.type === undefined ? [] : [declaration.type];
}
