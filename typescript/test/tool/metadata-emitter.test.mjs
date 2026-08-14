import assert from "node:assert/strict";
import test from "node:test";
import { parseInterface, validateInterface } from "../../dist/syntax/index.js";
import { buildImportGeneration, buildValidatedImportGeneration, emitImportMetadata } from "../../dist/tool/index.js";

test("emits canonical semantic metadata and machine type rows", () => {
    const file = parseInterface("meta.intercall", new TextEncoder().encode("type user_id uint64;"));
    validateInterface(file);
    const generation = buildValidatedImportGeneration(file, buildImportGeneration(file));
    const output = emitImportMetadata(file, generation);
    assert.match(output, /export const interfaceBody/);
    assert.match(output, /type user_id uint64;/);
    assert.match(output, /interfaceIDHex/);
    assert.match(output, /machineTypes/);
    assert.equal(output, emitImportMetadata(file, generation));
});
