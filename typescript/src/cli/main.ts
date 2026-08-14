#!/usr/bin/env node

import { HELP, parseCliArguments } from "./args.js";

try {
    const command = parseCliArguments(process.argv.slice(2));
    if (command.kind === "help") {
        process.stdout.write(HELP);
        process.exitCode = 0;
    } else {
        console.error(`intercall-ts ${command.kind} is not implemented yet`);
        process.exitCode = 1;
    }
} catch (error) {
    console.error(error instanceof Error ? error.message : String(error));
    process.exitCode = 1;
}
