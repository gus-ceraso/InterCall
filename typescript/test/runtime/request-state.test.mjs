import assert from "node:assert/strict";
import test from "node:test";
import {
    MAX_OUTGOING_CALLS,
    MAX_REQUEST_ID,
    OutgoingRequestState,
} from "../../dist/runtime/request-state.js";
import { ConnectionCore } from "../../dist/runtime/connection-core.js";
import { RequestIDsExhaustedError, ResourceLimitError } from "../../dist/runtime/errors.js";

test("admits exactly 1,024 calls and releases slots idempotently", () => {
    const core = new ConnectionCore();
    const state = new OutgoingRequestState(core);
    const slots = Array.from({ length: MAX_OUTGOING_CALLS }, () => state.reserve());
    assert.equal(state.active, MAX_OUTGOING_CALLS);
    assert.throws(() => state.reserve(), ResourceLimitError);
    slots[0].release();
    slots[0].release();
    assert.equal(state.active, MAX_OUTGOING_CALLS - 1);
    slots[1].release();
    assert.equal(state.active, MAX_OUTGOING_CALLS - 2);
});

test("allocates monotonic IDs and never reuses a cancelled ID", () => {
    const core = new ConnectionCore();
    const state = new OutgoingRequestState(core);
    const first = state.reserve();
    const second = state.reserve();
    assert.equal(first.allocateID(), 0n);
    first.release();
    assert.equal(second.allocateID(), 1n);
    second.release();
    const nearEnd = new OutgoingRequestState(core, MAX_REQUEST_ID);
    const last = nearEnd.reserve();
    assert.equal(last.allocateID(), MAX_REQUEST_ID);
    last.release();
    const exhausted = nearEnd.reserve();
    assert.throws(() => exhausted.allocateID(), RequestIDsExhaustedError);
    exhausted.release();
});

test("transfers pending ownership exactly once to response or cancellation", () => {
    const core = new ConnectionCore();
    const state = new OutgoingRequestState(core);
    const responseSlot = state.reserve();
    const responseID = responseSlot.allocateID();
    let responseRelease = 0;
    state.registerPending({ id: responseID, reject: () => { throw new Error("response must not reject"); }, release: () => { responseRelease += 1; responseSlot.release(); } });
    assert.ok(state.claimResponse(responseID));
    assert.equal(responseRelease, 1);
    assert.equal(state.claimResponse(responseID), undefined);

    const cancelSlot = state.reserve();
    const cancelID = cancelSlot.allocateID();
    let cancelRelease = 0;
    let cancelReason;
    state.registerPending({ id: cancelID, reject: (reason) => { cancelReason = reason; }, release: () => { cancelRelease += 1; cancelSlot.release(); } });
    const reason = new Error("cancel");
    assert.equal(state.cancel(cancelID, reason), true);
    assert.equal(cancelReason, reason);
    assert.equal(cancelRelease, 1);
    assert.equal(state.cancel(cancelID, reason), false);
});

test("refuses new reservations after terminal selection", () => {
    const core = new ConnectionCore();
    core.close();
    const state = new OutgoingRequestState(core);
    assert.throws(() => state.reserve(), /connection closed/);
    core.markReceiveStopped();
});
