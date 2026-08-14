import assert from "node:assert/strict";
import test from "node:test";
import { makeCodecProgram } from "../../dist/runtime/codec-program.js";
import { decodeProgram, encodeProgram } from "../../dist/runtime/codec-vm.js";

function wireListCount(count) {
    const wire = new Uint8Array(8);
    new DataView(wire.buffer).setBigUint64(0, BigInt(count), true);
    return wire;
}

test("executes lists and records without recursive calls", () => {
    const program = makeCodecProgram([
        { op: "record", fields: [{ name: "items", value: 1 }] },
        { op: "list", element: 2 },
        { op: "primitive", primitive: "uint16" },
    ], 0);
    const value = { items: [1, 2, 65535] };
    const wire = encodeProgram(program, value);
    assert.deepEqual(decodeProgram(program, wire), value);
});

test("uses frozen zero values and skips zero-width list elements", () => {
    const program = makeCodecProgram([{ op: "list", element: 1 }, { op: "zero" }], 0);
    const wire = encodeProgram(program, [{}, {}, {}]);
    assert.deepEqual(wire, wireListCount(3));
    const value = decodeProgram(program, wire);
    assert.equal(Object.isFrozen(value[0]), true);
    assert.equal(value[0], value[1]);
    assert.equal(value[1], value[2]);
});

test("handles deep named and record chains iteratively", () => {
    const depth = 20_000;
    const named = Array.from({ length: depth }, (_, index) =>
        index === depth - 1 ? { op: "primitive", primitive: "uint8" } : { op: "named", target: index + 1 });
    const namedProgram = makeCodecProgram(named, 0);
    assert.deepEqual(decodeProgram(namedProgram, Uint8Array.of(7)), 7);
    assert.deepEqual(encodeProgram(namedProgram, 7), Uint8Array.of(7));

    const records = Array.from({ length: depth }, (_, index) =>
        index === depth - 1
            ? { op: "record", fields: [{ name: "value", value: depth }] }
            : { op: "record", fields: [{ name: "next", value: index + 1 }] });
    records.push({ op: "primitive", primitive: "uint8" });
    const recordProgram = makeCodecProgram(records, 0);
    let value = 7;
    for (let index = depth - 1; index >= 0; index -= 1) value = index === depth - 1 ? { value } : { next: value };
    let decoded = decodeProgram(recordProgram, encodeProgram(recordProgram, value));
    for (let index = 0; index < depth - 1; index += 1) decoded = decoded.next;
    assert.equal(decoded.value, 7);
});
