#!/usr/bin/env node

import { readFile } from "node:fs/promises";
import { join } from "node:path";
import { parseInterface, validateInterface } from "../syntax/index.js";
import { buildValidatedImportSource } from "../tool/import-source.js";
import { formatGeneratedSource } from "../tool/generator-format.js";
import { parseCliArguments, HELP } from "./args.js";
import { replaceOwnedLeaf } from "./ownership.js";

async function main(): Promise<void> {
    const command = parseCliArguments(process.argv.slice(2));
    if (command.kind === "help") {
        process.stdout.write(HELP);
        return;
    }
    if (command.kind === "export") throw new Error("intercall-ts export is not implemented yet");
    const bytes = await readFile(command.interfacePath);
    const file = parseInterface(command.interfacePath, new Uint8Array(bytes));
    validateInterface(file);
    const source = formatGeneratedSource(buildValidatedImportSource(file));
    await replaceOwnedLeaf(join(command.out, "binding_gen.ts"), "import", Buffer.from(source));
}

main().catch((error: unknown) => {
    console.error(error instanceof Error ? error.message : String(error));
    process.exitCode = 1;
});
