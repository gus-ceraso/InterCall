import type { InterfaceFile, TypeExpr } from "../syntax/index.js";
import { compileCodecProgram } from "./codec.js";

export function emitExportCodecPrograms(file: InterfaceFile): string {
    const roots: TypeExpr[] = [];
    const seen = new Set<TypeExpr>();
    const add = (type: TypeExpr | undefined) => {
        if (type !== undefined && !seen.has(type)) {
            seen.add(type);
            roots.push(type);
        }
    };
    for (const declaration of file.declarations) {
        if (declaration.kind === "procedure-decl") {
            for (const parameter of declaration.params) add(parameter.type);
            add(declaration.result);
        } else if (declaration.kind === "exception-decl") add(declaration.type);
    }
    const lines = [
        "import { makeCodecProgram, requireCodecProgram } from \"@cerasos/intercall/generated\";",
        "",
    ];
    for (let index = 0; index < roots.length; index += 1) {
        const program = compileCodecProgram(file, roots[index]!);
        lines.push(`export const codec${index} = requireCodecProgram(makeCodecProgram(${JSON.stringify(program.instructions)}, ${program.root}));`, "");
    }
    return lines.join("\n");
}
