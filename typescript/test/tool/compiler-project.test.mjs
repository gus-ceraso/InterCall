import assert from "node:assert/strict";
import { readFileSync, statSync } from "node:fs";
import { resolve } from "node:path";
import test from "node:test";
import { loadCompilerProject, normalizeSourceOperands } from "../../dist/tool/index.js";

const project = resolve("test/fixtures/compiler/tsconfig.json");

test("loads one no-emit TypeScript program from the pinned compiler API", () => {
    const before = readFileSync(project);
    const loaded = loadCompilerProject(project);
    assert.equal(loaded.options.noEmit, true);
    assert.ok(loaded.program.getSourceFiles().length > 0);
    assert.deepEqual(readFileSync(project), before);
    assert.equal(statSync(project).isFile(), true);
    const tsOperand = normalizeSourceOperands(loaded, ["test/fixtures/compiler/provider.ts"])[0];
    assert.equal(tsOperand.extension, ".ts");
    const preserve = loadCompilerProject(resolve("test/fixtures/compiler/tsconfig-preserve.json"));
    const operands = normalizeSourceOperands(preserve, ["test/fixtures/compiler/provider-tsx.tsx"]);
    assert.deepEqual(operands.map((operand) => operand.extension), [".tsx"]);
    assert.equal(operands[0].jsx, 1);
});

test("rejects unsupported and non-project source operands", () => {
    const loaded = loadCompilerProject(project);
    assert.throws(() => normalizeSourceOperands(loaded, ["test/fixtures/compiler/README.md"]), /\.ts or \.tsx/);
    assert.throws(() => normalizeSourceOperands(loaded, ["typescript.ts"]), /not part/);
});

test("rejects missing project files deterministically", () => {
    assert.throws(() => loadCompilerProject("test/fixtures/compiler/missing-tsconfig.json"), /Cannot read file|invalid/i);
});
