import type { InterfaceFile } from "../syntax/index.js";

export function emitProcedureSwitch(file: InterfaceFile): string {
    const procedures = file.declarations.filter((declaration) => declaration.kind === "procedure-decl");
    const lines = [
        "export function dispatchProcedure(procedureKey: string): string {",
        "    switch (procedureKey) {",
    ];
    for (const procedure of procedures) lines.push(`        case ${JSON.stringify(procedure.name.name)}: return ${JSON.stringify(procedure.name.name)};`);
    lines.push("        default: return \"procedure_not_found\";", "    }", "}", "");
    return lines.join("\n");
}
