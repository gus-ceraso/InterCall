#!/usr/bin/env node

import { readFile } from "node:fs/promises";
import { join } from "node:path";
import { formatInterface, parseInterface, validateInterface } from "../syntax/index.js";
import { buildValidatedImportSource } from "../tool/import-source.js";
import { buildValidatedExportSource } from "../tool/export-source.js";
import { discoverSourceExports, normalizeSourceOperands, validateDiscoveredException, validateDiscoveredProcedure } from "../tool/index.js";
import { loadCompilerProject } from "../tool/compiler-project.js";
import { formatGeneratedSource } from "../tool/generator-format.js";
import { parseCliArguments, HELP } from "./args.js";
import { replaceOwnedInterface, replaceOwnedLeaf, validateOutputDirectory } from "./ownership.js";

async function main(): Promise<void> {
    const command = parseCliArguments(process.argv.slice(2));
    if (command.kind === "help") {
        process.stdout.write(HELP);
        return;
    }
    if (command.kind === "import") {
        await validateOutputDirectory(command.out);
        const bytes = await readFile(command.interfacePath);
        const file = parseInterface(command.interfacePath, new Uint8Array(bytes));
        validateInterface(file);
        const source = formatGeneratedSource(buildValidatedImportSource(file));
        await replaceOwnedLeaf(join(command.out, "binding_gen.ts"), "import", Buffer.from(source));
        return;
    }
    await validateOutputDirectory(command.out);
    const project = loadCompilerProject(command.project);
    const operands = normalizeSourceOperands(project, command.sources);
    const discovery = discoverSourceExports(project, operands, { include: command.include, exclude: command.exclude });
    for (const procedure of discovery.procedures) validateDiscoveredProcedure(project, procedure);
    for (const exception of discovery.exceptions) validateDiscoveredException(project, exception);
    const generated = buildValidatedExportSource(project, discovery);
    const interfaceBody = formatInterface(generated.source);
    await replaceOwnedInterface(command.interfacePath, new TextEncoder().encode(interfaceBody));
    await replaceOwnedLeaf(join(command.out, "binding_gen.ts"), "export", Buffer.from(formatGeneratedSource(generated.generatedSource)));
}

main().catch((error: unknown) => {
    console.error(error instanceof Error ? error.message : String(error));
    process.exitCode = 1;
});
