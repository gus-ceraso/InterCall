import type {
    InterfaceFile,
    TypeDecl,
    TypeExpr,
} from "../syntax/index.js";
import { SyntaxDiagnostic } from "../syntax/index.js";

export const MAX_PROJECTION_DEPTH = 4_096;

export function validateProjectionDepth(
    file: InterfaceFile,
    limit = MAX_PROJECTION_DEPTH,
): void {
    const types = new Map<string, TypeDecl>();
    for (const declaration of file.declarations) {
        if (declaration.kind === "type-decl") types.set(declaration.name.name, declaration);
    }

    for (const declaration of file.declarations) {
        switch (declaration.kind) {
            case "type-decl":
                walkRoot(file, types, declaration.type, declaration.name.name, limit);
                break;
            case "exception-decl":
                if (declaration.type !== undefined) walkRoot(file, types, declaration.type, undefined, limit);
                break;
            case "procedure-decl":
                for (const parameter of declaration.params) walkRoot(file, types, parameter.type, undefined, limit);
                if (declaration.result !== undefined) walkRoot(file, types, declaration.result, undefined, limit);
                break;
        }
    }
}

interface Work {
    readonly type: TypeExpr;
    readonly depth: number;
    readonly activeTypes: ReadonlySet<string>;
}

function walkRoot(
    file: InterfaceFile,
    types: ReadonlyMap<string, TypeDecl>,
    root: TypeExpr,
    rootTypeName: string | undefined,
    limit: number,
): void {
    const active = new Set<string>();
    if (rootTypeName !== undefined) active.add(rootTypeName);
    const stack: Work[] = [{ type: root, depth: 1, activeTypes: active }];

    while (stack.length > 0) {
        const work = stack.pop()!;
        if (work.depth > limit) {
            throw depthError(file, work.type, limit);
        }
        switch (work.type.kind) {
            case "primitive":
                break;
            case "list":
                stack.push({ type: work.type.elem, depth: work.depth + 1, activeTypes: work.activeTypes });
                break;
            case "record":
                for (let i = work.type.fields.length - 1; i >= 0; i -= 1) {
                    stack.push({
                        type: work.type.fields[i]!.type,
                        depth: work.depth + 1,
                        activeTypes: work.activeTypes,
                    });
                }
                break;
            case "named": {
                const declaration = types.get(work.type.name.name);
                if (declaration === undefined) break;
                if (work.activeTypes.has(declaration.name.name)) {
                    throw new SyntaxDiagnostic(
                        file.source.name,
                        file.source.position(work.type.name.span.start),
                        work.type.name.span,
                        `recursive named type reference ${JSON.stringify(declaration.name.name)}`,
                    );
                }
                const active = new Set(work.activeTypes);
                active.add(declaration.name.name);
                stack.push({ type: declaration.type, depth: work.depth + 1, activeTypes: active });
                break;
            }
        }
    }
}

function depthError(file: InterfaceFile, type: TypeExpr, limit: number): SyntaxDiagnostic {
    const span = typeSpan(type);
    return new SyntaxDiagnostic(
        file.source.name,
        file.source.position(span.start),
        span,
        `resolved type depth exceeds ${limit} occurrences`,
    );
}

function typeSpan(type: TypeExpr): { readonly start: number; readonly end: number } {
    let current = type;
    while (current.kind === "list") current = current.elem;
    if (current.kind === "primitive") return current.span;
    if (current.kind === "named") return current.name.span;
    return { start: current.recordSpan.start, end: current.rBrace.end };
}
