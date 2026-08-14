import assert from "node:assert/strict";
import test from "node:test";
import { ConnectionCore } from "../../dist/runtime/connection-core.js";
import { ConnectionClosedError, InterCallAbortError } from "../../dist/runtime/errors.js";

test("selects one terminal cause, transfers pending calls, and cleans once", async () => {
    let cleanups = 0;
    let rejected = 0;
    let releases = 0;
    const core = new ConnectionCore(() => { cleanups += 1; });
    const first = new Error("first");
    const second = new Error("second");
    assert.equal(core.registerPending({ id: 1n, reject: (cause) => { rejected += 1; assert.equal(cause, first); }, release: () => { releases += 1; } }), true);
    assert.equal(core.terminate(first), true);
    assert.equal(core.terminate(second), false);
    assert.equal(core.terminal, first);
    assert.equal(rejected, 1);
    assert.equal(releases, 1);
    assert.equal(cleanups, 1);
    assert.equal(core.claimPending(1n), undefined);
    core.markReceiveStopped();
    assert.equal(await core.closed, first);
    core.markReceiveStopped();
    assert.equal(cleanups, 1);
});

test("aborts active handlers and delays closed until receive shutdown", async () => {
    const core = new ConnectionCore();
    const lease = core.registerHandler();
    assert.ok(lease);
    let closed = false;
    void core.closed.then(() => { closed = true; });
    const reason = "socket failed";
    core.terminate(reason);
    assert.equal(lease.signal.aborted, true);
    assert.equal(lease.signal.reason instanceof InterCallAbortError, true);
    assert.equal(lease.signal.reason.cause, reason);
    await Promise.resolve();
    assert.equal(closed, false);
    core.markReceiveStopped();
    const cause = await core.closed;
    assert.equal(cause, lease.signal.reason);
    assert.equal(core.registerHandler(), undefined);
    lease.finish();
});

test("prevents late handler responses after terminal selection", () => {
    const core = new ConnectionCore();
    let sends = 0;
    assert.equal(core.sendIfActive(() => { sends += 1; }), true);
    core.terminate(new Error("closed"));
    assert.equal(core.sendIfActive(() => { sends += 1; }), false);
    assert.equal(sends, 1);
    core.markReceiveStopped();
});

test("explicit close uses the stable closed error", () => {
    const core = new ConnectionCore();
    core.close();
    assert.equal(core.terminal instanceof ConnectionClosedError, true);
    core.markReceiveStopped();
});
