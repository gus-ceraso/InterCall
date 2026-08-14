import assert from "node:assert/strict";
import test from "node:test";
import { MAX_QUEUED_UNREAD_BYTES, WebSocketMessageQueue } from "../../dist/browser/message-queue.js";
import { ProtocolError, ResourceLimitError } from "../../dist/runtime/errors.js";

test("retains ordered binary messages with owned storage", () => {
    const queue = new WebSocketMessageQueue(8);
    const buffer = Uint8Array.of(1, 2, 3);
    queue.pushMessage(buffer.buffer);
    buffer[0] = 9;
    queue.pushMessage(Uint8Array.of(4, 5).buffer);
    assert.equal(queue.unreadBytes, 5);
    assert.deepEqual(queue.read(5), Uint8Array.of(1, 2, 3, 4, 5));
});

test("rejects text, unsupported values, per-message overflow, and aggregate overflow before retention", () => {
    const queue = new WebSocketMessageQueue(2);
    assert.throws(() => queue.pushMessage("text"), ProtocolError);
    assert.throws(() => queue.pushMessage(new Blob()), ProtocolError);
    assert.throws(() => queue.pushMessage(new ArrayBuffer(3)), ResourceLimitError);
    const aggregate = new WebSocketMessageQueue();
    /** @type {any} */ (aggregate).queue.unreadValue = MAX_QUEUED_UNREAD_BYTES;
    assert.throws(() => aggregate.pushMessage(new ArrayBuffer(1)), ResourceLimitError);
});
