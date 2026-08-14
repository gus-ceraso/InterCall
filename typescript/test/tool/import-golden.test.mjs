import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import test from "node:test";
import { parseInterface, validateInterface } from "../../dist/syntax/index.js";
import {
    buildImportGeneration,
    buildValidatedImportGeneration,
    emitImportBinding,
    emitImportClient,
    emitImportCodecPrograms,
    emitImportExceptions,
    emitImportMetadata,
    emitImportTypes,
    formatGeneratedSource,
    validateGeneratedSource,
} from "../../dist/tool/index.js";

function generate(fileName) {
    const source = new TextEncoder().encode(readFileSync(new URL(`../fixtures/import/${fileName}`, import.meta.url)));
    const file = parseInterface(fileName, source);
    validateInterface(file);
    const generation = buildValidatedImportGeneration(file, buildImportGeneration(file));
    return formatGeneratedSource([
        emitImportTypes(generation),
        emitImportExceptions(generation),
        emitImportCodecPrograms(file, generation),
        emitImportBinding(file),
        emitImportMetadata(file, generation),
        emitImportClient(generation),
    ].join("\n"));
}

test("empty and kitchen-sink import fixtures are deterministic and type-checkable", () => {
    for (const fileName of ["empty.intercall", "kitchen-sink.intercall"]) {
        const output = generate(fileName);
        assert.equal(output, generate(fileName));
        validateGeneratedSource(output, fileName.replace(".intercall", ".ts"));
    }
});
