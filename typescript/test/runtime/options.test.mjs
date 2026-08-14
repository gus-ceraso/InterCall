import assert from "node:assert/strict";
import test from "node:test";
import {
    DEFAULT_MESSAGE_LIMIT,
    DEFAULT_NEGOTIATION_TIMEOUT_MS,
    DEFAULT_OPEN_TIMEOUT_MS,
    normalizeWebSocketOptions,
} from "../../dist/browser/options.js";

test("applies independent browser defaults and copies protocols", () => {
    const protocols = ["intercall", "v1"];
    const options = normalizeWebSocketOptions({ protocols });
    assert.equal(options.openTimeoutMs, DEFAULT_OPEN_TIMEOUT_MS);
    assert.equal(options.negotiationTimeoutMs, DEFAULT_NEGOTIATION_TIMEOUT_MS);
    assert.equal(options.messageLimit, DEFAULT_MESSAGE_LIMIT);
    assert.deepEqual(options.protocols, protocols);
    protocols.push("mutated");
    assert.deepEqual(options.protocols, ["intercall", "v1"]);
});

test("rejects invalid timeout, message, protocol, and signal options", () => {
    for (const key of ["openTimeoutMs", "negotiationTimeoutMs", "messageLimit"]) {
        for (const value of [0, -1, Infinity, NaN, 1.5]) {
            assert.throws(() => normalizeWebSocketOptions({ [key]: value }), /positive integer/);
        }
    }
    assert.throws(() => normalizeWebSocketOptions({ messageLimit: DEFAULT_MESSAGE_LIMIT + 1 }), /default maximum/);
    assert.throws(() => normalizeWebSocketOptions({ protocols: ["ok", 1] }), /protocols/);
    assert.throws(() => normalizeWebSocketOptions({ protocols: /** @type {any} */ ({}) }), /protocols/);
    assert.throws(() => normalizeWebSocketOptions({ signal: /** @type {any} */ ({}) }), /AbortSignal/);
});
