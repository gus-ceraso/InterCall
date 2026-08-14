import assert from "node:assert/strict";
import test from "node:test";
import { fixedDispatchResult, isDispatchResult } from "../../dist/runtime/dispatch-result.js";
import { InternalException, InvalidArguments, ProcedureNotFound } from "../../dist/runtime/errors.js";

test("maps fixed dispatch failures to exact keys and empty payloads", () => {
    assert.deepEqual(fixedDispatchResult("procedure_not_found"), { exceptionKey: ProcedureNotFound.key, payload: new Uint8Array() });
    assert.deepEqual(fixedDispatchResult("invalid_arguments"), { exceptionKey: InvalidArguments.key, payload: new Uint8Array() });
    assert.deepEqual(fixedDispatchResult("internal_exception"), { exceptionKey: InternalException.key, payload: new Uint8Array() });
});

test("validates dispatch result shape without accepting structural values", () => {
    assert.equal(isDispatchResult({ exceptionKey: 1n, payload: new Uint8Array() }), true);
    assert.equal(isDispatchResult({ exceptionKey: 1, payload: new Uint8Array() }), false);
    assert.equal(isDispatchResult({ exceptionKey: 1n, payload: [] }), false);
    assert.equal(isDispatchResult(null), false);
});
