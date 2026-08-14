import assert from "node:assert/strict";
import test from "node:test";
import {
    EMPTY_INTERFACE_CANONICAL_BODY,
    EMPTY_PROCEDURE_NOT_FOUND_KEY,
    emptyExportBinding,
    emptyImportBinding,
    emptyInterfaceID,
} from "../../dist/runtime/index.js";
import { bindingDispatch } from "../../dist/runtime/binding.js";
import {
    emptyExportBinding as generatedExport,
    emptyImportBinding as generatedImport,
} from "../../dist/generated-spi/index.js";

function hex(bytes) {
    return [...bytes].map((byte) => byte.toString(16).padStart(2, "0")).join("");
}

test("exports process-wide empty bindings with the Go canonical body and ID", async () => {
    assert.equal(EMPTY_INTERFACE_CANONICAL_BODY, "exception internal_exception;\n\nexception invalid_arguments;\n\nexception procedure_not_found;\n");
    assert.equal(hex(emptyInterfaceID()), "c31c470dd8db21db3bc8709bdcad7778a3d2dead33193c95b9691a4f0ba50dc8");
    assert.equal(generatedExport, emptyExportBinding);
    assert.equal(generatedImport, emptyImportBinding);
    assert.equal(Object.isFrozen(emptyExportBinding), true);
    const response = await bindingDispatch(emptyExportBinding)(undefined, 123n, Uint8Array.of(1));
    assert.equal(response.exceptionKey, EMPTY_PROCEDURE_NOT_FOUND_KEY);
    assert.equal(response.payload.byteLength, 0);
});
