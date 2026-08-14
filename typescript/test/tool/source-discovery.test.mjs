import assert from "node:assert/strict";
import { resolve } from "node:path";
import test from "node:test";
import { buildExportInterface, decodeExportArguments, discoverSourceExports, emitExportCodecPrograms, emitProcedureSwitch, emitProviderImports, encodeExportResult, invokeExportProvider, loadCompilerProject, matchExportException, normalizeSourceOperands, orderDiscoveredExports, resolveProviderImports, validateDiscoveredException, validateDiscoveredProcedure, walkReachableType } from "../../dist/tool/index.js";

test("matches no-payload identity and payload exception classes", () => {
    const sentinel = new Error("sentinel");
    assert.equal(matchExportException(sentinel, [{ name: "sentinel", noPayload: sentinel }]).spec.name, "sentinel");
    class Failed extends Error { constructor(payload) { super("failed"); this.payload = payload; } }
    const failed = new Failed({ code: 3 });
    assert.deepEqual(matchExportException(failed, [{ name: "failed", payloadClass: Failed }]).payload, { code: 3 });
    assert.throws(() => matchExportException(failed, [{ name: "a", payloadClass: Failed }, { name: "b", payloadClass: Failed }]), /ambiguous/);
    assert.deepEqual(encodeExportResult(undefined, undefined), { ok: true, payload: new Uint8Array() });
});

test("invokes providers with one immutable context and positional values", async () => {
    const controller = new AbortController();
    let received;
    const result = await invokeExportProvider(async (context, first, second) => {
        received = [context, first, second];
        return first + second;
    }, controller.signal, [2, 3]);
    assert.equal(result, 5);
    assert.deepEqual(received.slice(1), [2, 3]);
    assert.equal(Object.isFrozen(received[0]), true);
    await assert.rejects(() => invokeExportProvider(() => 1, controller.signal, []), /Promise/);
});

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
    assert.deepEqual(decodeExportArguments([], []).values, []);
    assert.deepEqual(decodeExportArguments([], [new Uint8Array()]), { ok: false, exception: "invalid_arguments" });
    const filtered = discoverSourceExports(project, operands, { include: ["add"] });
    assert.deepEqual(filtered.procedures.map((item) => item.sourceName), ["add"]);
    assert.deepEqual(discoverSourceExports(project, operands, { exclude: ["add"] }).procedures, []);
    assert.throws(() => discoverSourceExports(project, operands, { include: ["missing"] }), /unknown/);
    assert.throws(() => discoverSourceExports(project, operands, { include: ["add", "add"] }), /duplicate/);
});
