import { dirname, extname, relative, resolve } from "node:path";
import ts from "typescript";
import type { CompilerProject } from "./compiler-project.js";
import { emitExportBinding, type ExportProviderBinding } from "./export-binding-emitter.js";
import { emitExportCodecPrograms } from "./export-codec-emitter.js";
import { emitProcedureSwitch } from "./procedure-emitter.js";
import { buildExportInterface, type ExportInterfaceResult } from "./export-interface.js";
import type { SourceDiscovery } from "./source-discovery.js";
import { validateGeneratedSource } from "./generated-check.js";

export interface ExportSourceOptions {
    readonly generatedFile?: string;
}

export interface ExportSourceResult extends ExportInterfaceResult {
    readonly generatedSource: string;
}

export function buildValidatedExportSource(project: CompilerProject, discovery: SourceDiscovery, options: ExportSourceOptions = {}): ExportSourceResult {
    const interfaceResult = buildExportInterface(project, discovery);
    const generatedFile = options.generatedFile ?? `${dirname(project.projectPath)}/binding_gen.ts`;
    const providers = providerBindings(project, discovery, generatedFile);
    const generatedSource = [
        emitExportCodecPrograms(interfaceResult.source),
        emitProcedureSwitch(interfaceResult.source),
        emitExportBinding(interfaceResult.source, { discovery, providers }),
    ].join("\n");
    const validationSource = `${[...new Set([...providers.values()].map((provider) => provider.localName))].map((name) => `declare const ${name}: any;`).join("\n")}\n${generatedSource.replace(/^import \* as provider_\d+ from .*;\n/gmu, "")}`;
    validateGeneratedSource(validationSource, "export_gen.ts");
    return { ...interfaceResult, generatedSource };
}

function providerBindings(project: CompilerProject, discovery: SourceDiscovery, generatedFile: string): Map<string, ExportProviderBinding> {
    const files = [...new Set([...discovery.procedures, ...discovery.exceptions].map((record) => record.sourceFile.fileName))].sort();
    const byFile = new Map(files.map((fileName, index) => [fileName, `provider_${index}`]));
    const result = new Map<string, ExportProviderBinding>();
    for (const record of [...discovery.procedures, ...discovery.exceptions]) {
        const extension = extname(record.sourceFile.fileName).toLowerCase();
        const emittedExtension = extension === ".tsx" && project.options.jsx === 1 ? ".jsx" : ".js";
        let specifier = relative(dirname(generatedFile), record.sourceFile.fileName).replaceAll("\\", "/");
        specifier = specifier.slice(0, -extension.length) + emittedExtension;
        if (!specifier.startsWith(".")) specifier = `./${specifier}`;
        const resolvedModule = ts.resolveModuleName(specifier, generatedFile, project.options, ts.sys).resolvedModule;
        if (resolvedModule === undefined || resolve(resolvedModule.resolvedFileName) !== resolve(record.sourceFile.fileName)) throw new Error(`cannot resolve generated provider import ${JSON.stringify(specifier)} to ${record.sourceFile.fileName}`);
        result.set(record.wireName, { localName: byFile.get(record.sourceFile.fileName)!, sourceName: record.sourceName, specifier });
    }
    return result;
}
