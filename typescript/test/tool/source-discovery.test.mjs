import assert from "node:assert/strict";
import { resolve } from "node:path";
import test from "node:test";
import { buildExportInterface, discoverSourceExports, emitExportCodecPrograms, emitProcedureSwitch, emitProviderImports, loadCompilerProject, normalizeSourceOperands, orderDiscoveredExports, resolveProviderImports, validateDiscoveredException, validateDiscoveredProcedure, walkReachableType } from "../../dist/tool/index.js";

test("rejects invalid procedure context and result signatures", () => {
    const project = loadCompilerProject(resolve("test/fixtures/compiler/tsconfig-discovery-invalid.json"));
    const operands = normalizeSourceOperands(project, ["test/fixtures/compiler/discovery-invalid.ts"]);
    const discovered = discoverSourceExports(project, operands);
    assert.throws(() => validateDiscoveredProcedure(project, discovered.procedures[0]), /HandlerContext/);
    assert.throws(() => validateDiscoveredProcedure(project, discovered.procedures[1]), /Promise/);
});

test("discovers directly exported tagged procedures, exceptions, and types", () => {
    const project = loadCompilerProject(resolve("test/fixtures/compiler/tsconfig-discovery.json"));
    const operands = normalizeSourceOperands(project, ["test/fixtures/compiler/discovery.ts"]);
    const providerImports = resolveProviderImports(project, operands);
    assert.equal(providerImports[0].emittedSpecifier, "./runtime.js");
    assert.match(emitProviderImports(providerImports)[0].source, /^import \* as provider_0 from \"\.\/runtime\.js\";/);
    const discovered = discoverSourceExports(project, operands);
    validateDiscoveredProcedure(project, discovered.procedures[0]);
    for (const exception of discovered.exceptions) validateDiscoveredException(project, exception);
    assert.deepEqual(walkReachableType(project, discovered.namedTypes[0].declaration).properties, ["x"]);
    const sourceFile = project.program.getSourceFile(resolve("test/fixtures/compiler/discovery.ts"));
    const recursive = sourceFile.statements.find((statement) => statement.name?.text === "Recursive");
    assert.throws(() => walkReachableType(project, recursive), /recursive/);
    assert.deepEqual(discovered.procedures.map((item) => [item.sourceName, item.wireName]), [["add", "add"]]);
    assert.deepEqual(discovered.exceptions.map((item) => [item.sourceName, item.wireName, item.payloadClass]), [["Denied", "denied", false], ["Failed", "failed", true]]);
    assert.deepEqual(discovered.namedTypes.map((item) => [item.sourceName, item.wireName]), [["Alias", "alias"], ["Point", "point"]]);
    const ordered = orderDiscoveredExports(discovered);
    assert.deepEqual(ordered.namedTypes.map((item) => item.sourceName), ["Point", "Alias"]);
    const generated = buildExportInterface(project, discovered);
    assert.match(generated.canonicalText, /^exception internal_exception;/);
    assert.ok(generated.source.declarations.some((item) => item.kind === "procedure-decl" && item.name.name === "add"));
    assert.match(emitExportCodecPrograms(generated.source), /requireCodecProgram/);
    assert.match(emitProcedureSwitch(generated.source), /case \"add\"/);
    assert.match(emitProcedureSwitch(generated.source), /procedure_not_found/);
    const filtered = discoverSourceExports(project, operands, { include: ["add"] });
    assert.deepEqual(filtered.procedures.map((item) => item.sourceName), ["add"]);
    assert.deepEqual(discoverSourceExports(project, operands, { exclude: ["add"] }).procedures, []);
    assert.throws(() => discoverSourceExports(project, operands, { include: ["missing"] }), /unknown/);
    assert.throws(() => discoverSourceExports(project, operands, { include: ["add", "add"] }), /duplicate/);
});
