import assert from "node:assert/strict";
import test from "node:test";
import { decodeBytes, encodeBytes } from "../../dist/runtime/bytes-codec.js";
import { EncoderBuffer } from "../../dist/runtime/encoder-buffer.js";
import { DecoderCursor, PrimitiveCodecError } from "../../dist/runtime/primitive-codec.js";

function encode(value) {
    const buffer = new EncoderBuffer(1024);
    encodeBytes(buffer, value);
    return buffer.finish();
}

test("encodes length-prefixed bytes and owns decoded storage", () => {
    const source = Uint8Array.from([1, 2, 3]);
    const wire = encode(source);
    assert.deepEqual(wire.slice(0, 8), Uint8Array.from([3, 0, 0, 0, 0, 0, 0, 0]));
    source[0] = 9;
    const decoded = decodeBytes(new DecoderCursor(wire));
    assert.deepEqual(decoded, Uint8Array.from([1, 2, 3]));
    wire[8] = 8;
    assert.deepEqual(decoded, Uint8Array.from([1, 2, 3]));
});

test("supports empty bytes and rejects invalid values or lengths", () => {
    assert.deepEqual(decodeBytes(new DecoderCursor(encode(new Uint8Array()))), new Uint8Array());
    assert.throws(() => encode([1, 2, 3]), PrimitiveCodecError);
    assert.throws(() => decodeBytes(new DecoderCursor(Uint8Array.from([
        1, 0, 0, 0, 0, 0, 0, 0,
    ]))), PrimitiveCodecError);
});
