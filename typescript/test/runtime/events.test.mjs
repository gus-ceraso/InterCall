import assert from "node:assert/strict";
import test from "node:test";
import { bindWebSocketEvents } from "../../dist/browser/events.js";
import { WebSocketMessageQueue } from "../../dist/browser/message-queue.js";

class Socket {
    constructor() { this.listeners = new Map(); }
    addEventListener(type, listener) { const list = this.listeners.get(type) ?? []; list.push(listener); this.listeners.set(type, list); }
    removeEventListener(type, listener) { this.listeners.set(type, (this.listeners.get(type) ?? []).filter((item) => item !== listener)); }
    emit(type, data) { for (const listener of this.listeners.get(type) ?? []) listener({ type, data }); }
    send() {}
    close() {}
}

test("forwards ordered messages and removes all listeners on terminal event", () => {
    const socket = new Socket();
    const chunks = [];
    const causes = [];
    const binding = bindWebSocketEvents(socket, new WebSocketMessageQueue(), (chunk) => chunks.push(chunk), (cause) => causes.push(cause));
    socket.emit("message", Uint8Array.of(1, 2).buffer);
    assert.deepEqual(chunks[0], Uint8Array.of(1, 2));
    socket.emit("error");
    socket.emit("close");
    assert.equal(causes.length, 1);
    binding.cleanup();
    assert.equal(socket.listeners.get("message").length, 0);
});

test("maps unsupported messages to one terminal cause", () => {
    const socket = new Socket();
    const causes = [];
    bindWebSocketEvents(socket, new WebSocketMessageQueue(), () => {}, (cause) => causes.push(cause));
    socket.emit("message", "text");
    assert.equal(causes.length, 1);
    assert.match(causes[0].message, /text/);
});
