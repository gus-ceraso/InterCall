import type { InterfaceFile, TypeExpr } from "../syntax/index.js";
import { compileCodecProgram } from "./codec.js";
import type { ImportGenerationRecord } from "./import.js";

export function emitImportCodecPrograms(
    file: InterfaceFile,
    generation: ImportGenerationRecord,
): string {
    const roots: TypeExpr[] = [];
    const seen = new Set<TypeExpr>();
    const add = (type: TypeExpr | undefined) => {
        if (type !== undefined && !seen.has(type)) {
            seen.add(type);
            roots.push(type);
        }
    };
    for (const procedure of generation.procedures) {
        for (const parameter of procedure.declaration.params) add(parameter.type);
        add(procedure.declaration.result);
    }
    for (const exception of generation.exceptions) add(exception.declaration.type);
    const lines = [
        "import { makeCodecProgram, requireCodecProgram } from \"@cerasos/intercall/generated\";",
        "",
    ];
    for (let index = 0; index < roots.length; index += 1) {
        const program = compileCodecProgram(file, roots[index]!);
        const instructions = program.instructions.map((instruction) => instruction.op === "record"
            ? { ...instruction, fields: instruction.fields.map((field) => ({ ...field })) }
            : { ...instruction });
        lines.push(`export const codec${index} = requireCodecProgram(makeCodecProgram(${JSON.stringify(instructions)}, ${program.root}));`, "");
    }
    return lines.join("\n");
}
