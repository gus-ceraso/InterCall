import { tokenLiteral } from "../syntax/index.js";
import type { TokenKind } from "../syntax/index.js";
import type { TypeExpr } from "../syntax/index.js";
import type { ImportGenerationRecord } from "./import.js";

const primitiveTypes: Record<string, string> = {
    int8: "Int8", int16: "Int16", int32: "Int32", int64: "Int64",
    uint8: "Uint8", uint16: "Uint16", uint32: "Uint32", uint64: "Uint64",
    float32: "Float32", float64: "Float64", string: "string", bytes: "Uint8Array",
};

export function emitImportTypes(generation: ImportGenerationRecord): string {
    const names = new Map(generation.namedTypes.map((record) => [record.declaration.name.name, record.nativeName]));
    const fields = new Map(generation.fields.map((record) => [record.field, record.nativeName]));
    const lines = [
        "import type { EmptyRecord, Float32, Float64, Int8, Int16, Int32, Int64, Uint8, Uint16, Uint32, Uint64 } from \"@cerasos/intercall\";",
        "",
    ];
    for (const record of generation.namedTypes) {
        lines.push(`export type ${record.nativeName} = ${emitTypeExpression(record.type, names, fields)};`, "");
    }
    return lines.join("\n");
}

export function emitTypeExpression(root: TypeExpr, names: ReadonlyMap<string, string>, fields: ReadonlyMap<object, string> = new Map()): string {
    const output: string[] = [];
    type Action = { readonly kind: "text"; readonly value: string } | { readonly kind: "type"; readonly value: TypeExpr };
    const stack: Action[] = [{ kind: "type", value: root }];
    while (stack.length > 0) {
        const action = stack.pop()!;
        if (action.kind === "text") {
            output.push(action.value);
            continue;
        }
        const type = action.value;
        switch (type.kind) {
            case "primitive":
                output.push(primitiveTypeName(type.primitive));
                break;
            case "named":
                output.push(names.get(type.name.name) ?? type.name.name);
                break;
            case "list":
                output.push("ReadonlyArray<");
                stack.push({ kind: "text", value: ">" });
                stack.push({ kind: "type", value: type.elem });
                break;
            case "record":
                if (type.fields.length === 0) {
                    output.push("EmptyRecord");
                    break;
                }
                output.push("{\n");
                stack.push({ kind: "text", value: "}" });
                for (let index = type.fields.length - 1; index >= 0; index -= 1) {
                    const field = type.fields[index]!;
                    stack.push({ kind: "text", value: ";\n" });
                    stack.push({ kind: "type", value: field.type });
                    stack.push({ kind: "text", value: `    readonly ${fields.get(field) ?? field.name.name}: ` });
                }
                break;
        }
    }
    return output.join("");
}

function primitiveTypeName(primitive: TokenKind): string {
    const name = tokenLiteral(primitive) ?? String(primitive);
    return primitiveTypes[name] ?? name;
}
