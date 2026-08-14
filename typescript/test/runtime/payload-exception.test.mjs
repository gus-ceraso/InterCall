import assert from "node:assert/strict";
import test from "node:test";
import { PayloadException } from "../../dist/runtime/index.js";

class Failed extends PayloadException {
    constructor(payload) {
        super(payload);
    }
}

test("generated payload exceptions retain exact runtime identity and payload shape", () => {
    const payload = Object.freeze({ code: 7, detail: "denied" });
    const error = new Failed(payload);
    assert.equal(error instanceof Error, true);
    assert.equal(error instanceof PayloadException, true);
    assert.equal(error.payload, payload);
    assert.deepEqual(error.payload, { code: 7, detail: "denied" });
    assert.equal(error.message, "payload exception");
    assert.equal("code" in error, false);
});
