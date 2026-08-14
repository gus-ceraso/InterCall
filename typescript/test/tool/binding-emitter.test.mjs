import assert from "node:assert/strict";
import test from "node:test";
import { parseInterface, validateInterface } from "../../dist/syntax/index.js";
import { emitImportBinding } from "../../dist/tool/index.js";

test("embeds the canonical interface ID in one metadata-aware import binding", () => {
    const file = parseInterface("empty.intercall", new TextEncoder().encode(""));
    validateInterface(file);
    const output = emitImportBinding(file);
    assert.match(output, /createImportBindingWithInterfaceID/);
    assert.match(output, /0xe3, 0xb0, 0xc4/);
    assert.equal(output, emitImportBinding(file));
});
