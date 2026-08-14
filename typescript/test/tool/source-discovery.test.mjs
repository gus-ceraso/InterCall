import assert from "node:assert/strict";
import { resolve } from "node:path";
import test from "node:test";
import { discoverSourceExports, loadCompilerProject, normalizeSourceOperands, resolveProviderImports, validateDiscoveredException, validateDiscoveredProcedure, walkReachableType } from "../../dist/tool/index.js";

test("discovers directly exported tagged procedures, exceptions, and types", () => {
    const project = loadCompilerProject(resolve("test/fixtures/compiler/tsconfig-discovery.json"));
    const operands = normalizeSourceOperands(project, ["test/fixtures/compiler/discovery.ts"]);
    const providerImports = resolveProviderImports(project, operands);
    assert.equal(providerImports[0].emittedSpecifier, "./runtime.js");
    const discovered = discoverSourceExports(project, operands);
    validateDiscoveredProcedure(project, discovered.procedures[0]);
    for (const exception of discovered.exceptions) validateDiscoveredException(project, exception);
    assert.deepEqual(walkReachableType(project, discovered.namedTypes[0].declaration).properties, ["x"]);
    const sourceFile = project.program.getSourceFile(resolve("test/fixtures/compiler/discovery.ts"));
    const recursive = sourceFile.statements.find((statement) => statement.name?.text === "Recursive");
    assert.throws(() => walkReachableType(project, recursive), /recursive/);
    assert.deepEqual(discovered.procedures.map((item) => [item.sourceName, item.wireName]), [["add", "add"]]);
    assert.deepEqual(discovered.exceptions.map((item) => [item.sourceName, item.wireName, item.payloadClass]), [["Denied", "denied", false], ["Failed", "failed", true]]);
    assert.deepEqual(discovered.namedTypes.map((item) => [item.sourceName, item.wireName]), [["Point", "point"]]);
    const filtered = discoverSourceExports(project, operands, { include: ["add"] });
    assert.deepEqual(filtered.procedures.map((item) => item.sourceName), ["add"]);
    assert.deepEqual(discoverSourceExports(project, operands, { exclude: ["add"] }).procedures, []);
    assert.throws(() => discoverSourceExports(project, operands, { include: ["missing"] }), /unknown/);
    assert.throws(() => discoverSourceExports(project, operands, { include: ["add", "add"] }), /duplicate/);
});
