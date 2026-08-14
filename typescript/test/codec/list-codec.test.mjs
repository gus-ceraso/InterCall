import assert from "node:assert/strict";
import test from "node:test";
import { decodeList, encodeList, MAX_LIST_ELEMENTS } from "../../dist/runtime/list-codec.js";
import { EncoderBuffer } from "../../dist/runtime/encoder-buffer.js";
import { DecoderCursor, PrimitiveCodecError, decodePrimitive, encodePrimitive } from "../../dist/runtime/primitive-codec.js";

function encode(value) {
    const buffer = new EncoderBuffer(1024);
    encodeList(buffer, value, (target, item) => encodePrimitive(target, "uint16", item));
    return buffer.finish();
}

test("uses JavaScript arrays for list values", () => {
    const wire = encode([1, 2, 65535]);
    const decoded = decodeList(new DecoderCursor(wire), (cursor) => decodePrimitive(cursor, "uint16"));
    assert.deepEqual(decoded, [1, 2, 65535]);
    assert.equal(Array.isArray(decoded), true);
});

test("rejects non-arrays and excessive counts before allocation", () => {
    assert.throws(() => encode(new Uint8Array()), PrimitiveCodecError);
    assert.throws(() => encodeList(new EncoderBuffer(16), new Array(MAX_LIST_ELEMENTS + 1), () => {}), PrimitiveCodecError);
    const count = new EncoderBuffer(16);
    encodePrimitive(count, "uint64", BigInt(MAX_LIST_ELEMENTS + 1));
    assert.throws(() => decodeList(new DecoderCursor(count.finish()), () => 0), PrimitiveCodecError);
});
