import type {
    InterfaceFile,
    TypeDecl,
    TypeExpr,
} from "../syntax/index.js";
import { TokenKind } from "../syntax/index.js";
import type { CodecInstruction, CodecProgram } from "../runtime/codec-program.js";
import { makeCodecProgram } from "../runtime/codec-program.js";
import type { CodecRootFact } from "./model.js";

const primitiveNames = new Map<TokenKind, Extract<CodecInstruction, { readonly op: "primitive" }>['primitive']>([
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
]);

export function compileCodecProgram(
    file: InterfaceFile,
    root: TypeExpr | undefined,
): CodecProgram {
    if (root === undefined) return makeCodecProgram([{ op: "zero" }], 0);
    const declarations = new Map<string, TypeDecl>();
    for (const declaration of file.declarations) {
        if (declaration.kind === "type-decl") declarations.set(declaration.name.name, declaration);
    }

    const instructions: CodecInstruction[] = [];
    const cache = new Map<TypeExpr, number>();
    const active = new Set<TypeExpr>();
    type Work = { readonly type: TypeExpr; readonly exit: boolean };
    const work: Work[] = [{ type: root, exit: false }];
    while (work.length > 0) {
        const item = work.pop()!;
        if (cache.has(item.type)) continue;
        if (item.exit) {
            const index = finishType(item.type, declarations, cache, instructions);
            cache.set(item.type, index);
            active.delete(item.type);
            continue;
        }
        if (active.has(item.type)) throw new Error(`recursive codec type involving ${typeDescription(item.type)}`);
        active.add(item.type);
        work.push({ type: item.type, exit: true });
        switch (item.type.kind) {
            case "primitive":
                break;
            case "list":
                work.push({ type: item.type.elem, exit: false });
                break;
            case "record":
                for (let index = item.type.fields.length - 1; index >= 0; index -= 1) {
                    work.push({ type: item.type.fields[index]!.type, exit: false });
                }
                break;
            case "named": {
                const declaration = declarations.get(item.type.name.name);
                if (declaration === undefined) throw new Error(`unresolved codec type ${JSON.stringify(item.type.name.name)}`);
                work.push({ type: declaration.type, exit: false });
                break;
            }
        }
    }
    return makeCodecProgram(instructions, cache.get(root)!);
}

export function compileCodecPrograms(
    file: InterfaceFile,
    roots: readonly CodecRootFact[],
): CodecProgram[] {
    return roots.map((root) => compileCodecProgram(file, root.type));
}

function finishType(
    type: TypeExpr,
    declarations: ReadonlyMap<string, TypeDecl>,
    cache: ReadonlyMap<TypeExpr, number>,
    instructions: CodecInstruction[],
): number {
    switch (type.kind) {
        case "primitive": {
            const primitive = primitiveNames.get(type.primitive);
            if (primitive === undefined) throw new Error(`unsupported codec primitive ${type.primitive}`);
            return append(instructions, { op: "primitive", primitive });
        }
        case "named": {
            const declaration = declarations.get(type.name.name);
            if (declaration === undefined) throw new Error(`unresolved codec target ${JSON.stringify(type.name.name)}`);
            return append(instructions, { op: "named", target: requireCached(cache, declaration.type) });
        }
        case "list":
            return append(instructions, { op: "list", element: requireCached(cache, type.elem) });
        case "record":
            return append(instructions, {
                op: "record",
                fields: type.fields.map((field) => ({ name: field.name.name, value: requireCached(cache, field.type) })),
            });
    }
}

function requireCached(cache: ReadonlyMap<TypeExpr, number>, type: TypeExpr): number {
    const index = cache.get(type);
    if (index === undefined) throw new Error(`codec child was not compiled`);
    return index;
}

function append(instructions: CodecInstruction[], instruction: CodecInstruction): number {
    const index = instructions.length;
    instructions.push(instruction);
    return index;
}

function typeDescription(type: TypeExpr): string {
    return type.kind === "named" ? JSON.stringify(type.name.name) : type.kind;
}
