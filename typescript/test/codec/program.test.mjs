import assert from "node:assert/strict";
import test from "node:test";
import { makeCodecProgram } from "../../dist/runtime/codec-program.js";

test("freezes a flat codec program and its record fields", () => {
    const program = makeCodecProgram([
        { op: "record", fields: [{ name: "value", value: 1 }] },
        { op: "primitive", primitive: "uint32" },
    ], 0);
    assert.equal(Object.isFrozen(program), true);
    assert.equal(Object.isFrozen(program.instructions), true);
    assert.equal(Object.isFrozen(program.instructions[0].fields), true);
    assert.equal(program.zeroWidth, false);
});

test("computes zero width through named records but not lists", () => {
    const zero = makeCodecProgram([
        { op: "named", target: 1 },
        { op: "record", fields: [] },
    ], 0);
    assert.equal(zero.zeroWidth, true);
    const list = makeCodecProgram([
        { op: "list", element: 1 },
        { op: "zero" },
    ], 0);
    assert.equal(list.zeroWidth, false);
});

test("rejects invalid targets and recursive instruction graphs", () => {
    assert.throws(() => makeCodecProgram([{ op: "named", target: 2 }], 0), RangeError);
    assert.throws(() => makeCodecProgram([{ op: "named", target: 0 }], 0), /recursive codec/);
    assert.throws(() => makeCodecProgram([{ op: "zero" }], 1), RangeError);
});
