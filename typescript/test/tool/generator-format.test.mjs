import assert from "node:assert/strict";
import test from "node:test";
import { formatGeneratedSource } from "../../dist/tool/index.js";

test("formats generated source independently and deterministically", () => {
    assert.equal(formatGeneratedSource("const x = 1;  \r\n\r\n"), "const x = 1;\n");
    assert.equal(formatGeneratedSource(""), "\n");
});
