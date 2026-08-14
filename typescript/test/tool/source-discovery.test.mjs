import assert from "node:assert/strict";
import { resolve } from "node:path";
import test from "node:test";
import { discoverSourceExports, loadCompilerProject, normalizeSourceOperands } from "../../dist/tool/index.js";

test("discovers directly exported tagged procedures, exceptions, and types", () => {
    const project = loadCompilerProject(resolve("test/fixtures/compiler/tsconfig-discovery.json"));
    const operands = normalizeSourceOperands(project, ["test/fixtures/compiler/discovery.ts"]);
    const discovered = discoverSourceExports(project, operands);
    assert.deepEqual(discovered.procedures.map((item) => [item.sourceName, item.wireName]), [["add", "add"]]);
    assert.deepEqual(discovered.exceptions.map((item) => [item.sourceName, item.wireName, item.payloadClass]), [["Denied", "denied", false], ["Failed", "failed", true]]);
    assert.deepEqual(discovered.namedTypes.map((item) => [item.sourceName, item.wireName]), [["Point", "point"]]);
});
