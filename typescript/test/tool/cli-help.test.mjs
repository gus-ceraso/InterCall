import assert from "node:assert/strict";
import { execFile } from "node:child_process";
import { promisify } from "node:util";
import test from "node:test";
import { HELP } from "../../dist/cli/args.js";

const run = promisify(execFile);

test("CLI help is an exact stable snapshot", async () => {
    const result = await run(process.execPath, ["dist/cli/main.js", "--help"], { cwd: process.cwd() });
    assert.equal(result.stdout, HELP);
    assert.equal(result.stderr, "");
});
