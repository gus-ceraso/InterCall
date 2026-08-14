import type { Declaration, InterfaceFile, TypeExpr } from "../syntax/index.js";
import { validateProjectionDepth } from "./depth.js";
import { PublicNameScope } from "./mangle.js";
import { isCanonicalWireName } from "./name.js";
import { parseOverrides, resolveOverride } from "./selector.js";
import type { ImportGenerationRecord } from "./import.js";

const fixedExceptions = new Set(["procedure_not_found", "invalid_arguments", "internal_exception"]);
const generatedHelpers = ["EmptyRecord", "PayloadException", "Connection", "CallOptions", "createClient", "importBinding", "exportBinding"];

export function buildValidatedImportGeneration(
    file: InterfaceFile,
    generation: ImportGenerationRecord,
    overrideTexts: readonly string[] = [],
): ImportGenerationRecord {
    validateProjectionDepth(file);
    const overrides = parseOverrides(overrideTexts);
    const declarationNames = new Map<Declaration, string>(generation.declarations.map((record) => [record.declaration, record.nativeName]));
    const fieldNames = new Map(generation.fields.map((record) => [record.field, record.nativeName]));
    const parameterNames = new Map(generation.parameters.map((record) => [record.parameter, record.nativeName]));
    const declarationOverrides = new Set<Declaration>();
    const fieldOverrides = new Set<object>();
    const parameterOverrides = new Set<object>();
    for (const override of overrides) {
        const target = resolveOverride(file, override.text).target;
        if (target.field !== undefined) {
            fieldNames.set(target.field, override.name);
            fieldOverrides.add(target.field);
        } else if (target.parameter !== undefined) {
            parameterNames.set(target.parameter, override.name);
            parameterOverrides.add(target.parameter);
        } else {
            declarationNames.set(target.declaration, override.name);
            declarationOverrides.add(target.declaration);
        }
    }

    for (const declaration of file.declarations) if (!isCanonicalWireName(declaration.name.name) && !declarationOverrides.has(declaration)) throw new Error(`wire name ${JSON.stringify(declaration.name.name)} requires an explicit TypeScript-name override`);
    for (const field of generation.fields) if (!isCanonicalWireName(field.field.name.name) && !fieldOverrides.has(field.field)) throw new Error(`wire name ${JSON.stringify(field.field.name.name)} requires an explicit TypeScript-name override`);
    for (const parameter of generation.parameters) if (!isCanonicalWireName(parameter.parameter.name.name) && !parameterOverrides.has(parameter.parameter)) throw new Error(`wire name ${JSON.stringify(parameter.parameter.name.name)} requires an explicit TypeScript-name override`);
    const topLevel = new PublicNameScope();
    for (const helper of generatedHelpers) topLevel.claim(helper);
    for (const declaration of file.declarations) topLevel.claim(declarationNames.get(declaration)!);
    for (const declaration of file.declarations) {
        if (declaration.kind === "exception-decl" && fixedExceptions.has(declaration.name.name) && declaration.type !== undefined) {
            throw new Error(`fixed exception ${JSON.stringify(declaration.name.name)} must have no payload`);
        }
    }
    validateLocalScopes(file, fieldNames, parameterNames);
    return {
        ...generation,
        declarations: generation.declarations.map((record) => ({ ...record, nativeName: declarationNames.get(record.declaration)! })),
        namedTypes: generation.namedTypes.map((record) => ({ ...record, nativeName: declarationNames.get(record.declaration)! })),
        fields: generation.fields.map((record) => ({ ...record, nativeName: fieldNames.get(record.field)! })),
        parameters: generation.parameters.map((record) => ({ ...record, nativeName: parameterNames.get(record.parameter)! })),
        procedures: generation.procedures.map((record) => ({
            ...record,
            nativeName: declarationNames.get(record.declaration)!,
            parameters: record.parameters.map((parameter) => ({ ...parameter, nativeName: parameterNames.get(parameter.parameter)! })),
        })),
        exceptions: generation.exceptions.map((record) => ({ ...record, nativeName: declarationNames.get(record.declaration)! })),
    };
}

function validateLocalScopes(
    file: InterfaceFile,
    fieldNames: ReadonlyMap<object, string>,
    parameterNames: ReadonlyMap<object, string>,
): void {
    const work: TypeExpr[] = file.declarations.flatMap((declaration) => declarationTypes(declaration));
    const visited = new Set<TypeExpr>();
    while (work.length > 0) {
        const type = work.pop()!;
        if (visited.has(type)) continue;
        visited.add(type);
        if (type.kind === "record") {
            const scope = new PublicNameScope();
            for (const field of type.fields) {
                scope.claim(fieldNames.get(field)!);
                work.push(field.type);
            }
        } else if (type.kind === "list") work.push(type.elem);
    }
    for (const declaration of file.declarations) {
        if (declaration.kind !== "procedure-decl") continue;
        const scope = new PublicNameScope();
        scope.claim("options");
        scope.claim("result");
        for (const parameter of declaration.params) scope.claim(parameterNames.get(parameter)!);
    }
}

function declarationTypes(declaration: Declaration): TypeExpr[] {
    if (declaration.kind === "procedure-decl") return [...declaration.params.map((parameter) => parameter.type), ...(declaration.result === undefined ? [] : [declaration.result])];
    return declaration.type === undefined ? [] : [declaration.type];
}
