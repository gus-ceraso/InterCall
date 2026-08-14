import assert from "node:assert/strict";
import { readFileSync, statSync } from "node:fs";
import { resolve } from "node:path";
import test from "node:test";
import { loadCompilerProject } from "../../dist/tool/index.js";

const project = resolve("test/fixtures/compiler/tsconfig.json");

test("loads one no-emit TypeScript program from the pinned compiler API", () => {
    const before = readFileSync(project);
    const loaded = loadCompilerProject(project);
    assert.equal(loaded.options.noEmit, true);
    assert.ok(loaded.program.getSourceFiles().length > 0);
    assert.deepEqual(readFileSync(project), before);
    assert.equal(statSync(project).isFile(), true);
});

test("rejects missing project files deterministically", () => {
    assert.throws(() => loadCompilerProject("test/fixtures/compiler/missing-tsconfig.json"), /Cannot read file|invalid/i);
});
