#!/usr/bin/env node

import { readFile } from "node:fs/promises";
import { join, resolve } from "node:path";
import { attachDocumentation, formatInterface, parseInterface, validateInterface } from "../syntax/index.js";
import { buildValidatedImportSource } from "../tool/import-source.js";
import { buildValidatedExportSource } from "../tool/export-source.js";
import { discoverSourceExports, normalizeSourceOperands, validateDiscoveredException, validateDiscoveredProcedure } from "../tool/index.js";
import { loadCompilerProject } from "../tool/compiler-project.js";
import { formatGeneratedSource } from "../tool/generator-format.js";
import { parseCliArguments, HELP } from "./args.js";
import { formatDiagnostics } from "./diagnostics.js";
import { assertReplaceableInterface, assertReplaceableLeaf, replaceOwnedLeaf, replaceOwnedPair, validateOutputDirectory } from "./ownership.js";

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
        attachDocumentation(file);
        const source = formatGeneratedSource(buildValidatedImportSource(file, command.tsNames));
        await replaceOwnedLeaf(join(command.out, "binding_gen.ts"), "import", Buffer.from(source));
        return;
    }
    const bindingPath = join(command.out, "binding_gen.ts");
    if (resolve(command.interfacePath) === resolve(bindingPath)) throw new Error("--interface must not equal the generated binding path");
    await validateOutputDirectory(command.out);
    const project = loadCompilerProject(command.project);
    const operands = normalizeSourceOperands(project, command.sources);
    const discovery = discoverSourceExports(project, operands, { include: command.include, exclude: command.exclude });
    for (const procedure of discovery.procedures) validateDiscoveredProcedure(project, procedure);
    for (const exception of discovery.exceptions) validateDiscoveredException(project, exception);
    const generated = buildValidatedExportSource(project, discovery, { generatedFile: bindingPath });
    const interfaceBytes = new TextEncoder().encode(formatInterface(generated.source));
    const bindingBytes = Buffer.from(formatGeneratedSource(generated.generatedSource));
    await assertReplaceableInterface(command.interfacePath);
    await assertReplaceableLeaf(bindingPath, "export", bindingBytes);
    await replaceOwnedPair(command.interfacePath, interfaceBytes, bindingPath, "export", bindingBytes);
}

main().catch((error: unknown) => {
    console.error(formatCliError(error));
    process.exitCode = 1;
});

function formatCliError(error: unknown): string {
    const message = error instanceof Error ? error.message : String(error);
    const match = /^(.*):(\d+):(\d+):\s*(.*)$/su.exec(message);
    if (match === null) return message;
    return formatDiagnostics([{ path: match[1]!, line: Number(match[2]), column: Number(match[3]), message: match[4]! }]);
}
