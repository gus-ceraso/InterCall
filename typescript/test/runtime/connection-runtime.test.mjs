import assert from "node:assert/strict";
import test from "node:test";
import { call, createExportBinding, createImportBinding } from "../../dist/generated-spi/index.js";
import { buildFrame, FrameReceiver } from "../../dist/runtime/frame.js";
import { ConnectionRuntime } from "../../dist/runtime/connection-runtime.js";

class FakeTransport {
    constructor() { this.isOpen = true; this.bufferedAmount = 0; this.sent = []; this.closed = false; }
    send(frame) { this.sent.push(frame.slice()); }
    close() { this.closed = true; this.isOpen = false; }
}

function lastHeader(transport) {
    const receiver = new FrameReceiver();
    receiver.push(transport.sent.at(-1));
    return receiver.next();
}

test("runs an outgoing call through a fake transport and claims its response", async () => {
    const transport = new FakeTransport();
    let exported;
    const exportBinding = createExportBinding(async (_context, key, payload) => {
        exported = { key, payload };
        return { exceptionKey: 0n, payload: Uint8Array.of(9) };
    });
    const importBinding = createImportBinding();
    const runtime = new ConnectionRuntime(transport, importBinding, exportBinding);
    const result = call(runtime.connection, importBinding, 55n, () => Uint8Array.of(1, 2), (key, payload) => {
        assert.equal(key, 0n);
        assert.deepEqual(payload, Uint8Array.of(3));
    });
    for (let index = 0; index < 4; index += 1) await Promise.resolve();
    const request = lastHeader(transport);
    assert.equal(request.header.requestID, 0n);
    assert.equal(request.header.key, 55n);
    transport.sent.length = 0;
    runtime.receiveChunk(buildFrame("response", 0n, 0n, Uint8Array.of(3)));
    await result;
});

test("dispatches incoming requests and sends a response without escaping provider failures", async () => {
    const transport = new FakeTransport();
    let calls = 0;
    const exportBinding = createExportBinding(async (_context, key, payload) => {
        calls += 1;
        assert.equal(key, 77n);
        assert.deepEqual(payload, Uint8Array.of(4));
        return { exceptionKey: 88n, payload: Uint8Array.of(5) };
    });
    const runtime = new ConnectionRuntime(transport, createImportBinding(), exportBinding);
    runtime.receiveChunk(buildFrame("request", 3n, 77n, Uint8Array.of(4)));
    await Promise.resolve();
    await Promise.resolve();
    const response = lastHeader(transport);
    assert.equal(calls, 1);
    assert.equal(response.header.kind, "response");
    assert.equal(response.header.requestID, 3n);
    assert.equal(response.header.key, 88n);
    assert.deepEqual(response.payload, Uint8Array.of(5));
});

test("handles concurrent out-of-order calls and ignores unmatched responses", async () => {
    const transport = new FakeTransport();
    const binding = createImportBinding();
    const runtime = new ConnectionRuntime(transport, binding, createExportBinding(async () => ({ exceptionKey: 0n, payload: new Uint8Array() })));
    const seen = [];
    const first = call(runtime.connection, binding, 1n, () => Uint8Array.of(1), (_key, payload) => seen.push(payload[0]));
    const second = call(runtime.connection, binding, 2n, () => Uint8Array.of(2), (_key, payload) => seen.push(payload[0]));
    for (let index = 0; index < 20 && transport.sent.length < 2; index += 1) await Promise.resolve();
    runtime.receiveChunk(buildFrame("response", 999n, 0n, Uint8Array.of(9)));
    runtime.receiveChunk(buildFrame("response", 1n, 0n, Uint8Array.of(2)));
    runtime.receiveChunk(buildFrame("response", 0n, 0n, Uint8Array.of(1)));
    await Promise.all([first, second]);
    assert.deepEqual(seen, [2, 1]);
});

test("supports nested calls from an incoming handler", async () => {
    const transport = new FakeTransport();
    const importBinding = createImportBinding();
    let runtime;
    const exportBinding = createExportBinding(async (context, key) => {
        if (key === 10n) {
            const nested = call(context.connection, importBinding, 11n, () => new Uint8Array(), () => {});
            for (let index = 0; index < 12 && transport.sent.length === 0; index += 1) await Promise.resolve();
            const nestedRequest = new FrameReceiver();
            nestedRequest.push(transport.sent.at(-1));
            const frame = nestedRequest.next();
            runtime.receiveChunk(buildFrame("response", frame.header.requestID, 0n, new Uint8Array()));
            await nested;
            return { exceptionKey: 12n, payload: new Uint8Array() };
        }
        return { exceptionKey: 13n, payload: new Uint8Array() };
    });
    runtime = new ConnectionRuntime(transport, importBinding, exportBinding);
    runtime.receiveChunk(buildFrame("request", 4n, 10n, new Uint8Array()));
    for (let index = 0; index < 40; index += 1) await Promise.resolve();
    const keys = transport.sent.map((sent) => {
        const receiver = new FrameReceiver();
        receiver.push(sent);
        return receiver.next().header.key;
    });
    assert.equal(keys.includes(12n), true);
});

test("terminates on malformed matched responses but keeps unmatched responses opaque", async () => {
    const transport = new FakeTransport();
    const binding = createImportBinding();
    const runtime = new ConnectionRuntime(transport, binding, createExportBinding(async () => ({ exceptionKey: 0n, payload: new Uint8Array() })));
    runtime.receiveChunk(buildFrame("response", 999n, 0xdeadn, Uint8Array.of(1, 2)));
    const pending = call(runtime.connection, binding, 3n, () => new Uint8Array(), () => { throw new Error("bad response"); });
    for (let index = 0; index < 4; index += 1) await Promise.resolve();
    runtime.receiveChunk(buildFrame("response", 0n, 0n, new Uint8Array()));
    await assert.rejects(pending, /bad response/);
    assert.equal(runtime.core.isTerminal, true);
});
