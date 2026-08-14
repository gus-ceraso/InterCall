import assert from "node:assert/strict";
import test from "node:test";
import { makeCodecProgram } from "../../dist/runtime/codec-program.js";
import { CODEC_NODE_BUDGET, CodecResourceError } from "../../dist/runtime/codec-budget.js";
import { decodeProgram, encodeProgram } from "../../dist/runtime/codec-vm.js";

function countWire(count) {
    const wire = new Uint8Array(8);
    new DataView(wire.buffer).setBigUint64(0, BigInt(count), true);
    return wire;
}

test("accepts and rejects the boundary for a zero-width list inside a record", () => {
    const program = makeCodecProgram([
        { op: "record", fields: [{ name: "items", value: 1 }] },
        { op: "list", element: 2 },
        { op: "zero" },
    ], 0);
    const below = CODEC_NODE_BUDGET - 2;
    const wire = encodeProgram(program, { items: new Array(below) });
    assert.equal(wire.byteLength, 8);
    assert.deepEqual(decodeProgram(program, wire).items.length, below);
    assert.throws(() => decodeProgram(program, countWire(below + 1)), CodecResourceError);
});

test("rejects an over-budget nested list before its large allocation", () => {
    const program = makeCodecProgram([
        { op: "list", element: 1 },
        { op: "list", element: 2 },
        { op: "zero" },
    ], 0);
    const outer = new Uint8Array(16);
    const view = new DataView(outer.buffer);
    view.setBigUint64(0, 1n, true);
    view.setBigUint64(8, BigInt(CODEC_NODE_BUDGET - 1), true);
    assert.throws(() => decodeProgram(program, outer), CodecResourceError);
});
