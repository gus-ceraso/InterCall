import assert from "node:assert/strict";
import test from "node:test";
import {
    CodecBufferError,
    EncoderBuffer,
} from "../../dist/runtime/encoder-buffer.js";

test("grows and finishes with independent storage", () => {
    const buffer = new EncoderBuffer(64, 1);
    buffer.appendByte(1);
    buffer.append(Uint8Array.from([2, 3, 4]));
    const result = buffer.finish();
    assert.deepEqual(result, Uint8Array.from([1, 2, 3, 4]));
    result[0] = 99;
    assert.deepEqual(buffer.finish(), Uint8Array.from([1, 2, 3, 4]));
});

test("enforces maximum encoded size before allocation", () => {
    const buffer = new EncoderBuffer(3, 1);
    buffer.append(Uint8Array.from([1, 2, 3]));
    assert.equal(buffer.length, 3);
    assert.throws(() => buffer.appendByte(4), CodecBufferError);
    assert.throws(() => new EncoderBuffer(-1), RangeError);
    assert.throws(() => buffer.appendByte(256), CodecBufferError);
});

test("rejects invalid append lengths through typed arrays", () => {
    const buffer = new EncoderBuffer(8);
    assert.throws(() => buffer.append({ byteLength: -1 }), CodecBufferError);
});
