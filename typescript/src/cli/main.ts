#!/usr/bin/env node

import { readFile } from "node:fs/promises";
import { join, relative, resolve } from "node:path";
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
    const sourceDiagnostics = [];
    for (const procedure of discovery.procedures) {
        try { validateDiscoveredProcedure(project, procedure); }
        catch (error) { sourceDiagnostics.push(sourceDiagnostic(project, procedure.declaration, error)); }
    }
    for (const exception of discovery.exceptions) {
        try { validateDiscoveredException(project, exception); }
        catch (error) { sourceDiagnostics.push(sourceDiagnostic(project, exception.declaration, error)); }
    }
    if (sourceDiagnostics.length > 0) throw new Error(formatDiagnostics(sourceDiagnostics));
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

function sourceDiagnostic(project: ReturnType<typeof loadCompilerProject>, node: import("typescript").Node, error: unknown): { readonly path: string; readonly line: number; readonly column: number; readonly message: string } {
    const sourceFile = node.getSourceFile();
    const position = sourceFile.getLineAndCharacterOfPosition(node.getStart(sourceFile));
    return {
        path: relative(resolve(project.projectPath, ".."), sourceFile.fileName).replaceAll("\\", "/"),
        line: position.line + 1,
        column: position.character + 1,
        message: error instanceof Error ? error.message.replaceAll("\n", " ") : String(error).replaceAll("\n", " "),
    };
}

function formatCliError(error: unknown): string {
    const message = error instanceof Error ? error.message : String(error);
    const diagnostics = message.split("\n").map((line) => /^(.*):(\d+):(\d+):\s*(.*)$/u.exec(line)).filter((match): match is RegExpExecArray => match !== null);
    if (diagnostics.length === message.split("\n").length && diagnostics.length > 0) {
        return formatDiagnostics(diagnostics.map((match) => ({ path: match[1]!, line: Number(match[2]), column: Number(match[3]), message: match[4]! })));
    }
    return message;
}
