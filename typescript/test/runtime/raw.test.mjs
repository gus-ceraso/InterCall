import assert from "node:assert/strict";
import test from "node:test";
import { createExportBinding, createImportBinding } from "../../dist/generated-spi/index.js";
import { attachRawSocket } from "../../dist/browser/raw.js";
import { buildFrame, FrameReceiver } from "../../dist/runtime/frame.js";

class Socket {
    constructor() { this.readyState = 1; this.bufferedAmount = 0; this.binaryType = "arraybuffer"; this.sent = []; this.listeners = new Map(); }
    addEventListener(type, listener) { const list = this.listeners.get(type) ?? []; list.push(listener); this.listeners.set(type, list); }
    removeEventListener(type, listener) { this.listeners.set(type, (this.listeners.get(type) ?? []).filter((item) => item !== listener)); }
    send(frame) { this.sent.push(frame.slice()); }
    close() { this.readyState = 3; }
    emit(type, data) { for (const listener of this.listeners.get(type) ?? []) listener({ type, data }); }
}

test("constructs a raw connection at the first frame", async () => {
    const socket = new Socket();
    const binding = createImportBinding();
    const connection = attachRawSocket(socket, {
        importBinding: binding,
        exportBinding: createExportBinding(async () => ({ exceptionKey: 0n, payload: new Uint8Array() })),
    }, 1024);
    const call = (await import("../../dist/generated-spi/index.js")).call(connection, binding, 9n, () => new Uint8Array(), () => {});
    for (let index = 0; index < 10; index += 1) await Promise.resolve();
    const sent = new FrameReceiver();
    sent.push(socket.sent[0]);
    const request = sent.next();
    socket.emit("message", buildFrame("response", request.header.requestID, 0n, new Uint8Array()).buffer);
    await call;
    assert.equal(socket.binaryType, "arraybuffer");
});
