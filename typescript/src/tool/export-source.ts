import type { CompilerProject } from "./compiler-project.js";
import { emitExportBinding } from "./export-binding-emitter.js";
import { emitExportCodecPrograms } from "./export-codec-emitter.js";
import { emitProcedureSwitch } from "./procedure-emitter.js";
import { buildExportInterface, type ExportInterfaceResult } from "./export-interface.js";
import type { SourceDiscovery } from "./source-discovery.js";
import { validateGeneratedSource } from "./generated-check.js";

export interface ExportSourceResult extends ExportInterfaceResult {
    readonly generatedSource: string;
}

export function buildValidatedExportSource(project: CompilerProject, discovery: SourceDiscovery): ExportSourceResult {
    const interfaceResult = buildExportInterface(project, discovery);
    const generatedSource = [
        emitExportCodecPrograms(interfaceResult.source),
        emitProcedureSwitch(interfaceResult.source),
        emitExportBinding(interfaceResult.source),
    ].join("\n");
    validateGeneratedSource(generatedSource, "export_gen.ts");
    return { ...interfaceResult, generatedSource };
}
