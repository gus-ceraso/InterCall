import assert from "node:assert/strict";
import test from "node:test";
import { normalizeWebSocketOptions } from "../../dist/browser/options.js";
import { openWebSocket, WEB_SOCKET_OPEN } from "../../dist/browser/socket.js";

class FakeSocket {
    static instances = [];
    constructor(url, protocols) {
        this.url = url;
        this.protocols = protocols;
        this.binaryType = "blob";
        this.readyState = 0;
        this.bufferedAmount = 0;
        this.listeners = new Map();
        FakeSocket.instances.push(this);
    }
    addEventListener(type, listener) { const list = this.listeners.get(type) ?? []; list.push(listener); this.listeners.set(type, list); }
    removeEventListener(type, listener) { this.listeners.set(type, (this.listeners.get(type) ?? []).filter((item) => item !== listener)); }
    emit(type) { for (const listener of this.listeners.get(type) ?? []) listener(new Event(type)); }
    send() {}
    close() { this.readyState = 3; }
}

test("opens a socket with binary mode and cleans setup listeners", async () => {
    FakeSocket.instances.length = 0;
    const promise = openWebSocket("ws://example.test/rpc", normalizeWebSocketOptions(), FakeSocket);
    const socket = FakeSocket.instances[0];
    assert.equal(socket.binaryType, "arraybuffer");
    assert.equal(socket.url, "ws://example.test/rpc");
    socket.readyState = WEB_SOCKET_OPEN;
    socket.emit("open");
    assert.equal(await promise, socket);
    assert.equal(socket.listeners.get("open").length, 0);
});

test("races open against abort, error, close, and timeout", async () => {
    const controller = new AbortController();
    const aborted = openWebSocket("ws://example.test/rpc", normalizeWebSocketOptions({ signal: controller.signal }), FakeSocket);
    const reason = new Error("abort");
    controller.abort(reason);
    await assert.rejects(aborted, (error) => error === reason);

    const failed = openWebSocket("ws://example.test/rpc", normalizeWebSocketOptions(), FakeSocket);
    FakeSocket.instances.at(-1).emit("error");
    await assert.rejects(failed, /open failed/);

    const timed = openWebSocket("ws://example.test/rpc", normalizeWebSocketOptions({ openTimeoutMs: 1 }), FakeSocket);
    await assert.rejects(timed, /open timeout/);
});
