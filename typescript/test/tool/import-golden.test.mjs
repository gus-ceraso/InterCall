import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import test from "node:test";
import { parseInterface, validateInterface } from "../../dist/syntax/index.js";
import { compileCodecProgram } from "../../dist/tool/index.js";
import { decodeProgram, encodeProgram } from "../../dist/runtime/codec-vm.js";
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

test("executes kitchen-sink fixture codecs under strict runtime values", () => {
    const source = new TextEncoder().encode(readFileSync(new URL("../fixtures/import/kitchen-sink.intercall", import.meta.url)));
    const file = parseInterface("kitchen-sink.intercall", source);
    validateInterface(file);
    const user = file.declarations.find((item) => item.kind === "type-decl" && item.name.name === "user");
    const program = compileCodecProgram(file, user.type);
    const value = { id: 7n, name: "Ada", data: Uint8Array.of(1, 2), tags: [3, 4] };
    assert.deepEqual(decodeProgram(program, encodeProgram(program, value)), value);
});

test("empty and kitchen-sink import fixtures are deterministic and type-checkable", () => {
    for (const fileName of ["empty.intercall", "kitchen-sink.intercall"]) {
        const output = generate(fileName);
        assert.equal(output, generate(fileName));
        validateGeneratedSource(output, fileName.replace(".intercall", ".ts"));
    }
});
