import assert from "node:assert/strict";
import test from "node:test";
import { ChunkQueue } from "../../dist/runtime/chunk-queue.js";

test("preserves order across arbitrary chunks and partial reads", () => {
    const queue = new ChunkQueue();
    const first = Uint8Array.of(1, 2);
    queue.append(first);
    first[0] = 9;
    queue.append(Uint8Array.of(3, 4, 5));
    assert.equal(queue.unreadBytes, 5);
    assert.deepEqual(queue.read(3), Uint8Array.of(1, 2, 3));
    assert.equal(queue.unreadBytes, 2);
    assert.deepEqual(queue.read(2), Uint8Array.of(4, 5));
    assert.equal(queue.unreadBytes, 0);
});

test("does not consume incomplete reads and clears owned chunks", () => {
    const queue = new ChunkQueue();
    queue.append(Uint8Array.of(7, 8));
    assert.equal(queue.read(3), undefined);
    assert.equal(queue.unreadBytes, 2);
    assert.deepEqual(queue.read(0), new Uint8Array());
    queue.clear();
    assert.equal(queue.unreadBytes, 0);
    assert.equal(queue.read(1), undefined);
    assert.throws(() => queue.read(-1), RangeError);
    queue.append(new Uint8Array());
    assert.throws(() => queue.append(/** @type {any} */ ({})), /Uint8Array/);
});
