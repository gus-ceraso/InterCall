import assert from "node:assert/strict";
import test from "node:test";
import {
    DecoderCursor,
    encodePrimitive,
    decodePrimitive,
    PrimitiveCodecError,
} from "../../dist/runtime/primitive-codec.js";
import { EncoderBuffer } from "../../dist/runtime/encoder-buffer.js";

function encode(primitive, value) {
    const buffer = new EncoderBuffer(64);
    encodePrimitive(buffer, primitive, value);
    return buffer.finish();
}

test("encodes exact-width integers in little-endian order", () => {
    assert.deepEqual(encode("int8", -1), Uint8Array.from([0xff]));
    assert.deepEqual(encode("int16", -2), Uint8Array.from([0xfe, 0xff]));
    assert.deepEqual(encode("uint32", 0x12345678), Uint8Array.from([0x78, 0x56, 0x34, 0x12]));
    assert.deepEqual(encode("int64", -2n), Uint8Array.from([0xfe, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff]));
    assert.deepEqual(encode("uint64", 0x0807060504030201n), Uint8Array.from([1, 2, 3, 4, 5, 6, 7, 8]));
});

test("round-trips all numeric primitive categories", () => {
    const values = [
        ["int8", -128], ["uint8", 255], ["int16", -32768], ["uint16", 65535],
        ["int32", -2147483648], ["uint32", 4294967295],
        ["int64", -(1n << 63n)], ["uint64", (1n << 64n) - 1n],
        ["float32", -0], ["float64", Infinity],
    ];
    for (const [primitive, value] of values) {
        const decoded = decodePrimitive(new DecoderCursor(encode(primitive, value)), primitive);
        if (Object.is(value, -0)) assert.equal(Object.is(decoded, -0), true);
        else assert.equal(decoded, value);
    }
});

test("canonicalizes NaNs and rejects noncanonical NaNs", () => {
    assert.deepEqual(encode("float32", NaN), Uint8Array.from([0, 0, 0xc0, 0x7f]));
    assert.deepEqual(encode("float64", NaN), Uint8Array.from([0, 0, 0, 0, 0, 0, 0xf8, 0x7f]));
    assert.throws(() => decodePrimitive(new DecoderCursor(Uint8Array.from([1, 0, 0xc0, 0x7f])), "float32"), PrimitiveCodecError);
    assert.throws(() => decodePrimitive(new DecoderCursor(Uint8Array.from([1, 0, 0, 0, 0, 0, 0xf8, 0x7f])), "float64"), PrimitiveCodecError);
});

test("rejects integer range/type errors and truncation", () => {
    assert.throws(() => encode("uint8", -1), PrimitiveCodecError);
    assert.throws(() => encode("int32", 1.5), PrimitiveCodecError);
    assert.throws(() => encode("int64", 1), PrimitiveCodecError);
    assert.throws(() => encode("float64", 1n), PrimitiveCodecError);
    assert.throws(() => decodePrimitive(new DecoderCursor(Uint8Array.of(1)), "uint32"), PrimitiveCodecError);
});
