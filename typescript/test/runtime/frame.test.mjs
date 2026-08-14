import assert from "node:assert/strict";
import test from "node:test";
import {
    FRAME_HEADER_SIZE,
    FrameReceiver,
    MAX_FRAME_PAYLOAD,
    RESPONSE_BIT,
    parseFrameHeader,
} from "../../dist/runtime/frame.js";

function frame(rawID, key, payload) {
    const bytes = new Uint8Array(FRAME_HEADER_SIZE + payload.length);
    const view = new DataView(bytes.buffer);
    view.setBigUint64(0, rawID, true);
    view.setBigUint64(8, key, true);
    view.setBigUint64(16, BigInt(payload.length), true);
    bytes.set(payload, FRAME_HEADER_SIZE);
    return bytes;
}

test("parses partial headers/payloads and preserves owned frames", () => {
    const receiver = new FrameReceiver();
    const first = frame(7n, 11n, Uint8Array.of(1, 2, 3));
    const second = frame(RESPONSE_BIT | 8n, 12n, Uint8Array.of(4));
    receiver.push(first.subarray(0, 5));
    assert.equal(receiver.next(), undefined);
    receiver.push(first.subarray(5, 26));
    assert.equal(receiver.next(), undefined);
    receiver.push(new Uint8Array(first.subarray(26)));
    const decoded = receiver.next();
    assert.equal(decoded.header.kind, "request");
    assert.equal(decoded.header.requestID, 7n);
    assert.equal(decoded.header.key, 11n);
    assert.deepEqual(decoded.payload, Uint8Array.of(1, 2, 3));
    first[24] = 99;
    assert.equal(decoded.payload[0], 1);
    receiver.push(second);
    const response = receiver.next();
    assert.equal(response.header.kind, "response");
    assert.equal(response.header.requestID, 8n);
    assert.deepEqual(response.payload, Uint8Array.of(4));
});

test("parses multiple frames in one chunk and rejects oversized lengths before payload reads", () => {
    const receiver = new FrameReceiver();
    receiver.push(new Uint8Array([...frame(1n, 2n, Uint8Array.of()), ...frame(3n, 4n, Uint8Array.of(5, 6))]));
    assert.equal(receiver.next().header.requestID, 1n);
    assert.deepEqual(receiver.next().payload, Uint8Array.of(5, 6));
    const oversized = new Uint8Array(FRAME_HEADER_SIZE);
    new DataView(oversized.buffer).setBigUint64(16, BigInt(MAX_FRAME_PAYLOAD) + 1n, true);
    assert.throws(() => parseFrameHeader(oversized), /exceeds/);
    const bad = new FrameReceiver();
    bad.push(oversized);
    assert.throws(() => bad.next(), /exceeds/);
});
