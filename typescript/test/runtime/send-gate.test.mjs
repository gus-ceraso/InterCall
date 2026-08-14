import assert from "node:assert/strict";
import test from "node:test";
import {
    MAX_NATIVE_QUEUED_BYTES,
    SEND_POLL_INTERVAL_MS,
    SendGate,
} from "../../dist/runtime/send-gate.js";

class Scheduler {
    constructor() { this.entries = []; }
    setTimeout(callback, delayMs) {
        const entry = { callback, delayMs, cleared: false };
        this.entries.push(entry);
        return entry;
    }
    clearTimeout(entry) { if (entry) entry.cleared = true; }
    fire() {
        const entries = this.entries.splice(0);
        for (const entry of entries) if (!entry.cleared) entry.callback();
    }
    get pending() { return this.entries.filter((entry) => !entry.cleared); }
}

test("serializes sends through one FIFO gate", async () => {
    const gate = new SendGate(new Scheduler());
    const order = [];
    const first = gate.enqueue({ frameLength: 4, bufferedAmount: () => 0, terminalCause: () => undefined, send: () => order.push("first") });
    const second = gate.enqueue({ frameLength: 4, bufferedAmount: () => 0, terminalCause: () => undefined, send: () => order.push("second") });
    await Promise.all([first, second]);
    assert.deepEqual(order, ["first", "second"]);
});

test("polls at ten milliseconds and clears timers on cancellation", async () => {
    const scheduler = new Scheduler();
    const gate = new SendGate(scheduler);
    let buffered = MAX_NATIVE_QUEUED_BYTES;
    const sending = gate.enqueue({ frameLength: 1, bufferedAmount: () => buffered, terminalCause: () => undefined, send: () => {} });
    await Promise.resolve();
    assert.equal(scheduler.pending[0].delayMs, SEND_POLL_INTERVAL_MS);
    buffered = 0;
    scheduler.fire();
    await sending;
    assert.equal(scheduler.pending.length, 0);

    const controller = new AbortController();
    buffered = MAX_NATIVE_QUEUED_BYTES;
    const cancelled = gate.enqueue({ frameLength: 1, bufferedAmount: () => buffered, signal: controller.signal, terminalCause: () => undefined, send: () => assert.fail("must not send") });
    await Promise.resolve();
    const reason = new Error("cancel");
    controller.abort(reason);
    await assert.rejects(cancelled, (error) => error === reason);
    assert.equal(scheduler.pending.length, 0);
});

test("rejects terminal waits and oversized frames", async () => {
    const scheduler = new Scheduler();
    const gate = new SendGate(scheduler);
    const cause = new Error("closed");
    const waiting = gate.enqueue({ frameLength: 1, bufferedAmount: () => MAX_NATIVE_QUEUED_BYTES, terminalCause: () => cause, send: () => {} });
    await Promise.resolve();
    scheduler.fire();
    await assert.rejects(waiting, (error) => error === cause);
    assert.throws(() => gate.enqueue({ frameLength: MAX_NATIVE_QUEUED_BYTES + 1, bufferedAmount: () => 0, terminalCause: () => undefined, send: () => {} }), /capacity/);
});
