import assert from "node:assert/strict";
import test from "node:test";
import { createExportBindingWithInterfaceID, createImportBindingWithInterfaceID } from "../../dist/generated-spi/index.js";
import { normalizeWebSocketOptions } from "../../dist/browser/options.js";
import { negotiateClient } from "../../dist/browser/negotiation.js";
import { WebSocketMessageQueue } from "../../dist/browser/message-queue.js";
import { InterfaceMismatchError, InvalidArgumentError } from "../../dist/runtime/errors.js";

class Socket {
    constructor() { this.binaryType = "arraybuffer"; this.readyState = 1; this.bufferedAmount = 0; this.sent = []; }
    send(value) { this.sent.push(value.slice()); }
    close() {}
    addEventListener() {}
    removeEventListener() {}
}

const dispatch = async () => ({ exceptionKey: 0n, payload: new Uint8Array() });
function id(value) { return Uint8Array.from({ length: 32 }, () => value); }

test("sends the import ID and preserves residual frame bytes after negotiation", async () => {
    const socket = new Socket();
    const queue = new WebSocketMessageQueue();
    const importBinding = createImportBindingWithInterfaceID(id(1));
    const exportBinding = createExportBindingWithInterfaceID(dispatch, id(2));
    queue.pushMessage(new Uint8Array([...id(2), 9, 8]).buffer);
    await negotiateClient(socket, queue, importBinding, exportBinding, normalizeWebSocketOptions({ negotiationTimeoutMs: 100 }));
    assert.deepEqual(socket.sent[0], id(1));
    assert.deepEqual(queue.read(2), Uint8Array.of(9, 8));
});

test("rejects missing metadata, mismatch, abort, and timeout", async () => {
    const socket = new Socket();
    const noMetadata = new WebSocketMessageQueue();
    await assert.rejects(negotiateClient(socket, noMetadata, createImportBindingWithInterfaceID(id(1)), createExportBindingWithInterfaceID(dispatch, id(2)), normalizeWebSocketOptions({ negotiationTimeoutMs: 1 })), /timeout/);
    await assert.rejects(negotiateClient(socket, new WebSocketMessageQueue(), createImportBindingWithInterfaceID(id(1)), createExportBindingWithInterfaceID(dispatch, id(2)), normalizeWebSocketOptions({ negotiationTimeoutMs: 1 })), /timeout/);
    const mismatchQueue = new WebSocketMessageQueue();
    mismatchQueue.pushMessage(id(3).buffer);
    await assert.rejects(negotiateClient(socket, mismatchQueue, createImportBindingWithInterfaceID(id(1)), createExportBindingWithInterfaceID(dispatch, id(2)), normalizeWebSocketOptions()), InterfaceMismatchError);
    await assert.rejects(negotiateClient(socket, new WebSocketMessageQueue(), createImportBindingWithInterfaceID(id(1)), createExportBindingWithInterfaceID(dispatch), normalizeWebSocketOptions()), InvalidArgumentError);
});
