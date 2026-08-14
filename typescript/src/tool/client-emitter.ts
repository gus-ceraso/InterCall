import { declarationKey } from "../syntax/index.js";
import type { TypeExpr } from "../syntax/index.js";
import type { ImportGenerationRecord } from "./import.js";
import { emitTypeExpression } from "./import-emitter.js";

export function emitImportClient(generation: ImportGenerationRecord): string {
    const names = new Map(generation.namedTypes.map((record) => [record.declaration.name.name, record.nativeName]));
    const fields = new Map(generation.fields.map((record) => [record.field, record.nativeName]));
    const codecIndices = buildCodecIndices(generation);
    const lines = [
        "import { call, decodeProgram, encodeProgramsToPayload } from \"@cerasos/intercall/generated\";",
        "import type { CallOptions, Connection } from \"@cerasos/intercall\";",
        "",
        "export function createClient(connection: Connection) {",
        "    return Object.freeze({",
    ];
    for (const procedure of generation.procedures) {
        const parameters = procedure.parameters.map((parameter) => `${parameter.nativeName}: ${emitTypeExpression(parameter.parameter.type, names, fields)}`);
        parameters.push("options?: CallOptions");
        const result = procedure.declaration.result === undefined ? "void" : emitTypeExpression(procedure.declaration.result, names, fields);
        const key = declarationKey("procedure", procedure.declaration.name.name);
        const parameterIndices = procedure.parameters.map((parameter) => codecIndices.get(parameter.parameter.type)!);
        const resultIndex = procedure.declaration.result === undefined ? undefined : codecIndices.get(procedure.declaration.result);
        lines.push(`        ${procedure.nativeName}: async (${parameters.join(", ")}): Promise<${result}> => {`);
        lines.push("            let result!: " + result + ";");
        const values = procedure.parameters.map((parameter) => parameter.nativeName).join(", ");
        const programs = parameterIndices.map((index) => `codec${index}`).join(", ");
        lines.push(`            await call(connection, importBinding, ${key}n, () => encodeProgramsToPayload([${programs}], [${values}]), (exceptionKey, payload) => {`);
        lines.push("                if (exceptionKey === 0n) {");
        if (resultIndex === undefined) lines.push("                    if (payload.byteLength !== 0) throw new Error(\"unexpected response payload\");");
        else lines.push(`                    result = decodeProgram(codec${resultIndex}, payload) as ${result};`);
        lines.push("                    return;", "                }");
        emitExceptionDecoder(lines, generation, codecIndices, names, fields);
        lines.push("            }, options);", "            return result;", "        },");
    }
    lines.push("    });", "}", "");
    return lines.join("\n");
}

function buildCodecIndices(generation: ImportGenerationRecord): Map<TypeExpr, number> {
    const indices = new Map<TypeExpr, number>();
    const add = (type: TypeExpr | undefined) => {
        if (type !== undefined && !indices.has(type)) indices.set(type, indices.size);
    };
    for (const procedure of generation.procedures) {
        for (const parameter of procedure.declaration.params) add(parameter.type);
        add(procedure.declaration.result);
    }
    for (const exception of generation.exceptions) add(exception.declaration.type);
    return indices;
}

function emitExceptionDecoder(lines: string[], generation: ImportGenerationRecord, codecIndices: ReadonlyMap<TypeExpr, number>, names: ReadonlyMap<string, string>, fields: ReadonlyMap<object, string>): void {
    for (const exception of generation.exceptions) {
        const key = declarationKey("exception", exception.declaration.name.name);
        lines.push(`                if (exceptionKey === ${key}n) {`);
        if (exception.declaration.type === undefined) lines.push(`                    if (payload.byteLength !== 0) throw new Error("unexpected exception payload");`, `                    return { kind: "remote-exception", error: ${exception.nativeName} };`);
        else {
            const payloadType = emitTypeExpression(exception.declaration.type, names, fields);
            lines.push(`                    return { kind: "remote-exception", error: new ${exception.nativeName}(decodeProgram(codec${codecIndices.get(exception.declaration.type)!}, payload) as ${payloadType}) };`);
        }
        lines.push("                }");
    }
    lines.push("                throw new Error(`unknown exception key ${exceptionKey.toString()}`);");
}
