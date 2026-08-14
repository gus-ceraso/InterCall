import assert from "node:assert/strict";
import test from "node:test";
import {
    decodeExact,
    decodeRecord,
    encodeRecord,
} from "../../dist/runtime/record-codec.js";
import { EncoderBuffer } from "../../dist/runtime/encoder-buffer.js";
import { DecoderCursor, decodePrimitive, encodePrimitive, PrimitiveCodecError } from "../../dist/runtime/primitive-codec.js";

const fields = [
    { name: "first", encode: (buffer, value) => encodePrimitive(buffer, "uint16", value), decode: (cursor) => decodePrimitive(cursor, "uint16") },
    { name: "second", encode: (buffer, value) => encodePrimitive(buffer, "uint8", value), decode: (cursor) => decodePrimitive(cursor, "uint8") },
];

function encode(value) {
    const buffer = new EncoderBuffer(64);
    encodeRecord(buffer, value, fields);
    return buffer.finish();
}

test("encodes fields in declaration order and decodes closed records", () => {
    const wire = encode({ second: 3, first: 0x1234 });
    assert.deepEqual(wire, Uint8Array.from([0x34, 0x12, 3]));
    assert.deepEqual(decodeRecord(new DecoderCursor(wire), fields), { first: 0x1234, second: 3 });
});

test("rejects missing, extra, symbol, duplicate, and array record fields", () => {
    assert.throws(() => encode({ first: 1 }), PrimitiveCodecError);
    assert.throws(() => encode({ first: 1, second: 2, extra: 3 }), PrimitiveCodecError);
    assert.throws(() => encode({ first: 1, second: 2, [Symbol("extra")]: 3 }), PrimitiveCodecError);
    assert.throws(() => encode([1, 2]), PrimitiveCodecError);
    assert.throws(() => decodeRecord(new DecoderCursor(new Uint8Array()), [...fields, fields[0]]), PrimitiveCodecError);
});

test("requires exact root payload exhaustion", () => {
    const wire = Uint8Array.from([0x34, 0x12, 3]);
    assert.deepEqual(decodeExact(wire, (cursor) => decodeRecord(cursor, fields)), { first: 0x1234, second: 3 });
    assert.throws(() => decodeExact(Uint8Array.from([0x34, 0x12, 3, 0]), (cursor) => decodeRecord(cursor, fields)), /trailing/);
});
