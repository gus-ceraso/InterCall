import assert from "node:assert/strict";
import test from "node:test";
import { parseInterface, validateInterface } from "../../dist/syntax/index.js";
import { buildImportGeneration, buildValidatedImportGeneration, emitImportTypes } from "../../dist/tool/index.js";

test("emits readonly named types, EmptyRecord, bytes, and list forms deterministically", () => {
    const file = parseInterface("emit.intercall", new TextEncoder().encode(
        "type empty record {}; type blob bytes; type values list uint8; type user record { id uint64; tags values; data blob; marker empty; };",
    ));
    validateInterface(file);
    const output = emitImportTypes(buildValidatedImportGeneration(file, buildImportGeneration(file)));
    assert.match(output, /import type \{ EmptyRecord/);
    assert.match(output, /export type Empty = EmptyRecord;/);
    assert.match(output, /export type Blob = Uint8Array;/);
    assert.match(output, /export type Values = ReadonlyArray<Uint8>;/);
    assert.match(output, /readonly id: Uint64;/);
    assert.match(output, /readonly tags: Values;/);
    assert.match(output, /readonly marker: Empty;/);
    assert.equal(output, emitImportTypes(buildValidatedImportGeneration(file, buildImportGeneration(file))));
});
