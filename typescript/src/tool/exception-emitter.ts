import type { ImportGenerationRecord } from "./import.js";
import { emitTypeExpression } from "./import-emitter.js";

const fixed = new Map([
    ["procedure_not_found", "ProcedureNotFound"],
    ["invalid_arguments", "InvalidArguments"],
    ["internal_exception", "InternalException"],
]);

export function emitImportExceptions(generation: ImportGenerationRecord): string {
    const names = new Map(generation.namedTypes.map((record) => [record.declaration.name.name, record.nativeName]));
    const lines = ["import { PayloadException } from \"@cerasos/intercall\";", "", ""];
    for (const record of generation.exceptions) {
        const fixedName = fixed.get(record.declaration.name.name);
        if (fixedName !== undefined) {
            lines.push(`export { ${fixedName} as ${record.nativeName} } from "@cerasos/intercall";`, "");
        } else if (record.declaration.type === undefined) {
            lines.push(`export const ${record.nativeName} = new Error(${JSON.stringify(record.declaration.name.name)});`, "");
        } else {
            lines.push(`export class ${record.nativeName} extends PayloadException<${emitTypeExpression(record.declaration.type, names)}> {`, "}", "");
        }
    }
    return lines.join("\n");
}
