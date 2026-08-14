import assert from "node:assert/strict";
import test from "node:test";
import { parseInterface, validateInterface } from "../../dist/syntax/index.js";
import { buildImportGeneration, buildValidatedImportGeneration, emitImportClient } from "../../dist/tool/index.js";

test("emits a frozen positional client facade with optional call options", () => {
    const file = parseInterface("client.intercall", new TextEncoder().encode(
        "procedure ping {} ; procedure greet { first string; count uint32; } string;",
    ));
    validateInterface(file);
    const output = emitImportClient(buildValidatedImportGeneration(file, buildImportGeneration(file)));
    assert.match(output, /return Object\.freeze/);
    assert.match(output, /ping: async \(options\?: CallOptions\): Promise<void>/);
    assert.match(output, /greet: async \(first: string, count: Uint32, options\?: CallOptions\): Promise<string>/);
    assert.match(output, /call\(connection, importBinding/);
    assert.match(output, /ping: async \(options\?: CallOptions\): Promise<void>/);
});
