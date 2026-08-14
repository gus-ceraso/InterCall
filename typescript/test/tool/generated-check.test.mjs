import assert from "node:assert/strict";
import test from "node:test";
import { validateGeneratedSource } from "../../dist/tool/index.js";

test("type-checks generated source against synthetic runtime SPI declarations", () => {
    validateGeneratedSource("import type { Uint64 } from \"@cerasos/intercall\"; export const value: Uint64 = 1n;\n");
    assert.throws(() => validateGeneratedSource("export const = ;\n"), /binding_gen.ts/);
    assert.throws(() => validateGeneratedSource("import type { Missing } from \"@cerasos/intercall\"; const value: Missing = 1;\n"), /Missing/);
});
