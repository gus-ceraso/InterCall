import assert from "node:assert/strict";
import test from "node:test";
import { runOrderedCall } from "../../dist/runtime/call-order.js";
import { ConnectionCore } from "../../dist/runtime/connection-core.js";
import { OutgoingRequestState } from "../../dist/runtime/request-state.js";

class Driver {
    constructor() {
        this.log = [];
        this.core = new ConnectionCore();
        this.state = new OutgoingRequestState(this.core);
        this.readyValue = true;
        this.outcomeResolve = undefined;
        this.outcomeReject = undefined;
        this.cancelled = [];
    }
    validate() { this.log.push("validate"); }
    ready() { this.log.push("ready"); return this.readyValue; }
    reserveCall() { this.log.push("reserve"); return this.state.reserve(); }
    encode() { this.log.push("encode"); return Uint8Array.of(1, 2); }
    reserveFrameBytes(length) { this.log.push(`frame:${length}`); return () => this.log.push("release-frame"); }
    waitForSend() { this.log.push("wait"); return Promise.resolve(); }
    allocateID(slot) { this.log.push("allocate"); return slot.allocateID(); }
    registerPending(id, slot) {
        this.log.push("register");
        return new Promise((resolve, reject) => {
            this.outcomeResolve = (value) => { slot.release(); resolve(value); };
            this.outcomeReject = (error) => { slot.release(); reject(error); };
            this.core.registerPending({ id, reject: this.outcomeReject, release: () => {} });
        });
    }
    send() { this.log.push("send"); }
    cancel(id, reason) { this.cancelled.push([id, reason]); this.outcomeReject?.(reason); }
    fail() {}
}

test("enforces generated call ordering and releases frame bytes after outcome admission", async () => {
    const driver = new Driver();
    const call = runOrderedCall(driver);
    await Promise.resolve();
    assert.deepEqual(driver.log, ["validate", "ready", "reserve", "encode", "ready", "frame:2", "wait", "ready", "allocate", "register", "send"]);
    driver.outcomeResolve("ok");
    assert.equal(await call, "ok");
    assert.deepEqual(driver.log.at(-1), "release-frame");
});

test("does not encode on pre-aborted calls and cancels after send with the exact reason", async () => {
    const pre = new AbortController();
    pre.abort("before");
    const preDriver = new Driver();
    await assert.rejects(runOrderedCall(preDriver, pre.signal), (error) => error === "before");
    assert.deepEqual(preDriver.log, ["validate"]);

    const driver = new Driver();
    const controller = new AbortController();
    const call = runOrderedCall(driver, controller.signal);
    await Promise.resolve();
    const reason = new Error("cancel");
    controller.abort(reason);
    await assert.rejects(call, (error) => error === reason);
    assert.equal(driver.cancelled.length, 1);
    assert.equal(driver.cancelled[0][1], reason);
    assert.equal(driver.state.active, 0);
});
