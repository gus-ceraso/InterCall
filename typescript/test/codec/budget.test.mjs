import assert from "node:assert/strict";
import test from "node:test";
import { makeCodecProgram } from "../../dist/runtime/codec-program.js";
import { CODEC_NODE_BUDGET, CodecBudget, CodecResourceError } from "../../dist/runtime/codec-budget.js";
import { decodeProgram, encodeProgram } from "../../dist/runtime/codec-vm.js";

function countWire(count) {
    const wire = new Uint8Array(8);
    new DataView(wire.buffer).setBigUint64(0, BigInt(count), true);
    return wire;
}

test("accepts exactly the list node budget and rejects one more before allocation", () => {
    const program = makeCodecProgram([{ op: "list", element: 1 }, { op: "zero" }], 0);
    const accepted = new Array(CODEC_NODE_BUDGET - 1);
    const wire = encodeProgram(program, accepted);
    assert.deepEqual(wire, countWire(CODEC_NODE_BUDGET - 1));
    assert.throws(() => decodeProgram(program, countWire(CODEC_NODE_BUDGET)), CodecResourceError);
});

test("charges records and named targets against a shared budget", () => {
    const program = makeCodecProgram([
        { op: "named", target: 1 },
        { op: "record", fields: [] },
    ], 0);
    const budget = new CodecBudget(1);
    encodeProgram(program, {}, budget);
    assert.equal(budget.used, 1);
    assert.throws(() => encodeProgram(program, {}, budget), CodecResourceError);
    assert.throws(() => decodeProgram(program, new Uint8Array(), new CodecBudget(0)), CodecResourceError);
});
