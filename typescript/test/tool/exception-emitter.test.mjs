import assert from "node:assert/strict";
import test from "node:test";
import { parseInterface, validateInterface } from "../../dist/syntax/index.js";
import { buildImportGeneration, buildValidatedImportGeneration, emitImportExceptions } from "../../dist/tool/index.js";

test("emits application exception shapes and fixed mappings", () => {
    const file = parseInterface("exceptions.intercall", new TextEncoder().encode(
        "exception denied; exception failed record { code int32; detail string; }; exception procedure_not_found;",
    ));
    validateInterface(file);
    const generation = buildValidatedImportGeneration(file, buildImportGeneration(file));
    const output = emitImportExceptions(generation);
    assert.match(output, /export const Denied = Object\.freeze\(new RemoteException\("denied", \d+n\)\);/);
    assert.match(output, /export class Failed extends PayloadException/);
    assert.match(output, /readonly code: Int32/);
    assert.match(output, /ProcedureNotFound as ProcedureNotFound/);
});
