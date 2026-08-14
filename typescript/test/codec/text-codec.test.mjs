import assert from "node:assert/strict";
import test from "node:test";
import {
    decodeString,
    encodeString,
} from "../../dist/runtime/text-codec.js";
import { EncoderBuffer } from "../../dist/runtime/encoder-buffer.js";
import { DecoderCursor, PrimitiveCodecError } from "../../dist/runtime/primitive-codec.js";

function encode(value) {
    const buffer = new EncoderBuffer(1024);
    encodeString(buffer, value);
    return buffer.finish();
}

test("encodes UTF-8 byte lengths and decodes Unicode without normalization", () => {
    const decomposed = "e\u0301";
    const composed = "\u00e9";
    const decomposedBytes = encode(decomposed);
    const composedBytes = encode(composed);
    assert.deepEqual(decomposedBytes.slice(0, 8), Uint8Array.from([3, 0, 0, 0, 0, 0, 0, 0]));
    assert.deepEqual(composedBytes.slice(0, 8), Uint8Array.from([2, 0, 0, 0, 0, 0, 0, 0]));
    assert.equal(decodeString(new DecoderCursor(decomposedBytes)), decomposed);
    assert.equal(decodeString(new DecoderCursor(composedBytes)), composed);
});

test("accepts scalar Unicode and rejects unpaired UTF-16 surrogates", () => {
    const value = "hello \u{1f30d}";
    assert.equal(decodeString(new DecoderCursor(encode(value))), value);
    for (const invalid of ["\uD800", "\uDC00", "x\uD800y", "x\uDC00y"]) {
        assert.throws(() => encode(invalid), PrimitiveCodecError);
    }
});

test("rejects invalid UTF-8 and impossible lengths", () => {
    assert.throws(() => decodeString(new DecoderCursor(Uint8Array.from([
        1, 0, 0, 0, 0, 0, 0, 0, 0x80,
    ]))), PrimitiveCodecError);
    assert.throws(() => decodeString(new DecoderCursor(Uint8Array.from([
        2, 0, 0, 0, 0, 0, 0, 0, 0xc0,
    ]))), PrimitiveCodecError);
    assert.throws(() => decodeString(new DecoderCursor(Uint8Array.from([
        8, 0, 0, 0, 0, 0, 0, 0,
    ]))), PrimitiveCodecError);
});
