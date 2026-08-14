import assert from "node:assert/strict";
import test from "node:test";
import {
    freezeDispatch,
    freezeRequestEncoder,
    freezeResponseDecoder,
    requireCodecProgram,
} from "../../dist/generated-spi/index.js";
import { makeCodecProgram } from "../../dist/runtime/codec-program.js";

test("freezes generated callback SPI and accepts immutable codec programs", () => {
    const dispatch = () => Promise.resolve({ exceptionKey: 0n, payload: new Uint8Array() });
    const encode = () => new Uint8Array();
    const decode = () => {};
    assert.equal(Object.isFrozen(freezeDispatch(dispatch)), true);
    assert.equal(Object.isFrozen(freezeRequestEncoder(encode)), true);
    assert.equal(Object.isFrozen(freezeResponseDecoder(decode)), true);
    assert.equal(requireCodecProgram(makeCodecProgram([{ op: "zero" }], 0)).zeroWidth, true);
    assert.throws(() => freezeRequestEncoder(/** @type {any} */ ({})), /function/);
    assert.throws(() => requireCodecProgram({ instructions: [], root: 0, zeroWidth: true }), /immutable/);
});
