import assert from "node:assert/strict";
import test from "node:test";
import { call, createExportBindingWithInterfaceID, createImportBindingWithInterfaceID } from "../../dist/generated-spi/index.js";
import { connectWebSocket } from "../../dist/browser/index.js";
import { buildFrame, FrameReceiver } from "../../dist/runtime/frame.js";

function id(value) { return Uint8Array.from({ length: 32 }, () => value); }

class NegotiatingSocket {
    static importID = id(1);
    static exportID = id(2);
    constructor() {
        this.binaryType = "blob";
        this.readyState = 0;
        this.bufferedAmount = 0;
        this.listeners = new Map();
        queueMicrotask(() => { this.readyState = 1; this.emit("open"); });
    }
    addEventListener(type, listener) { const list = this.listeners.get(type) ?? []; list.push(listener); this.listeners.set(type, list); }
    removeEventListener(type, listener) { this.listeners.set(type, (this.listeners.get(type) ?? []).filter((item) => item !== listener)); }
    emit(type, data) { for (const listener of this.listeners.get(type) ?? []) listener({ type, data }); }
    send(data) {
        const bytes = new Uint8Array(data);
        if (bytes.byteLength === 32) this.emit("message", NegotiatingSocket.exportID.buffer.slice(0));
        else {
            const receiver = new FrameReceiver();
            receiver.push(bytes);
            const request = receiver.next();
            this.emit("message", buildFrame("response", request.header.requestID, 0n, Uint8Array.of(7)).buffer);
        }
    }
    close() { this.readyState = 3; }
}

test("connects, negotiates, preserves WebSocket binary mode, and returns a callable connection", async () => {
    const previous = globalThis.WebSocket;
    globalThis.WebSocket = NegotiatingSocket;
    try {
        const importBinding = createImportBindingWithInterfaceID(NegotiatingSocket.importID);
        const exportBinding = createExportBindingWithInterfaceID(async () => ({ exceptionKey: 0n, payload: new Uint8Array() }), NegotiatingSocket.exportID);
        const connection = await connectWebSocket("ws://example.test/rpc", { importBinding, exportBinding });
        await call(connection, importBinding, 1n, () => new Uint8Array(), (key, payload) => {
            assert.equal(key, 0n);
            assert.deepEqual(payload, Uint8Array.of(7));
        });
        connection.close();
    } finally {
        globalThis.WebSocket = previous;
    }
});
