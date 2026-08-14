import { declarationKey } from "../syntax/index.js";
import type { ImportGenerationRecord } from "./import.js";
import { emitTypeExpression } from "./import-emitter.js";

export function emitImportClient(generation: ImportGenerationRecord): string {
    const names = new Map(generation.namedTypes.map((record) => [record.declaration.name.name, record.nativeName]));
    const lines = [
        "import { call } from \"@cerasos/intercall/generated\";",
        "import type { CallOptions, Connection } from \"@cerasos/intercall\";",
        "",
        "export function createClient(connection: Connection) {",
        "    return Object.freeze({",
    ];
    for (const procedure of generation.procedures) {
        const parameters = procedure.parameters.map((parameter) => `${parameter.nativeName}: ${emitTypeExpression(parameter.parameter.type, names)}`);
        parameters.push("options?: CallOptions");
        const result = procedure.declaration.result === undefined ? "void" : emitTypeExpression(procedure.declaration.result, names);
        const key = declarationKey("procedure", procedure.declaration.name.name);
        lines.push(`        ${procedure.nativeName}: async (${parameters.join(", ")}): Promise<${result}> => {`);
        lines.push(`            return call(connection, importBinding, ${key}n, () => { throw new Error("generated codec not attached"); }, () => {}) as unknown as Promise<${result}>;`);
        lines.push("        },");
    }
    lines.push("    });", "}", "");
    return lines.join("\n");
}
