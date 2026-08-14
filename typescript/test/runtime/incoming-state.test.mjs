import assert from "node:assert/strict";
import test from "node:test";
import {
    IncomingRequestState,
    MAX_ACTIVE_INCOMING_HANDLERS,
    MAX_ACTIVE_INCOMING_PAYLOAD_BYTES,
} from "../../dist/runtime/incoming-state.js";
import { ConnectionCore } from "../../dist/runtime/connection-core.js";
import { ProtocolError, ResourceLimitError } from "../../dist/runtime/errors.js";

test("admits active requests, rejects duplicate IDs, and releases once", () => {
    const core = new ConnectionCore();
    const state = new IncomingRequestState(core);
    const lease = state.admit(1n, 10);
    assert.equal(state.activeHandlers, 1);
    assert.equal(state.activeBytes, 10);
    assert.equal(lease.signal.aborted, false);
    assert.throws(() => state.admit(1n, 1), ProtocolError);
    lease.finish();
    lease.finish();
    assert.equal(state.activeHandlers, 0);
    assert.equal(state.activeBytes, 0);
    const reused = state.admit(1n, 2);
    reused.finish();
});

test("enforces handler and aggregate payload limits", () => {
    const handlerCore = new ConnectionCore();
    const handlers = new IncomingRequestState(handlerCore);
    const leases = Array.from({ length: MAX_ACTIVE_INCOMING_HANDLERS }, (_, index) => handlers.admit(BigInt(index), 0));
    assert.throws(() => handlers.admit(999n, 0), ResourceLimitError);
    leases.forEach((lease) => lease.finish());

    const bytesCore = new ConnectionCore();
    const bytes = new IncomingRequestState(bytesCore);
    const full = bytes.admit(1n, MAX_ACTIVE_INCOMING_PAYLOAD_BYTES);
    assert.throws(() => bytes.admit(2n, 1), ResourceLimitError);
    full.finish();
    assert.equal(bytes.activeBytes, 0);
});

test("aborts active leases on connection termination", () => {
    const core = new ConnectionCore();
    const state = new IncomingRequestState(core);
    const lease = state.admit(1n, 8);
    const reason = new Error("closed");
    core.terminate(reason);
    assert.equal(lease.signal.aborted, true);
    lease.finish();
    assert.equal(state.activeHandlers, 0);
});
