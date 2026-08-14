import assert from "node:assert/strict";
import test from "node:test";
import { fixedDispatchResult, invokeDispatch, isDispatchResult } from "../../dist/runtime/dispatch-result.js";
import { InternalException, InvalidArguments, ProcedureNotFound } from "../../dist/runtime/errors.js";

test("maps fixed dispatch failures to exact keys and empty payloads", () => {
    assert.deepEqual(fixedDispatchResult("procedure_not_found"), { exceptionKey: ProcedureNotFound.key, payload: new Uint8Array() });
    assert.deepEqual(fixedDispatchResult("invalid_arguments"), { exceptionKey: InvalidArguments.key, payload: new Uint8Array() });
    assert.deepEqual(fixedDispatchResult("internal_exception"), { exceptionKey: InternalException.key, payload: new Uint8Array() });
});

test("contains synchronous throws, rejections, and malformed results", async () => {
    const context = /** @type {any} */ ({});
    const cases = [
        () => { throw new Error("sync"); },
        async () => { throw new Error("async"); },
        async () => ({ exceptionKey: 1n, payload: [] }),
    ];
    for (const dispatch of cases) {
        const result = await invokeDispatch(dispatch, context, 1n, Uint8Array.of(1));
        assert.equal(result.exceptionKey, InternalException.key);
        assert.equal(result.payload.byteLength, 0);
    }
    const original = Uint8Array.of(1);
    const copied = await invokeDispatch(async () => ({ exceptionKey: 2n, payload: original }), context, 1n, new Uint8Array());
    original[0] = 9;
    assert.deepEqual(copied.payload, Uint8Array.of(1));
    assert.equal((await invokeDispatch(async () => { throw InvalidArguments; }, context, 1n, new Uint8Array())).exceptionKey, InvalidArguments.key);
});

test("validates dispatch result shape without accepting structural values", () => {
    assert.equal(isDispatchResult({ exceptionKey: 1n, payload: new Uint8Array() }), true);
    assert.equal(isDispatchResult({ exceptionKey: 1, payload: new Uint8Array() }), false);
    assert.equal(isDispatchResult({ exceptionKey: 1n, payload: [] }), false);
    assert.equal(isDispatchResult(null), false);
});
