import assert from "node:assert/strict";
import test from "node:test";
import {
    interfaceID,
    interfaceIDHex,
    sha256,
} from "../../dist/tool/index.js";

test("computes the Go-compatible empty interface ID", () => {
    const body = "exception internal_exception;\n\nexception invalid_arguments;\n\nexception procedure_not_found;\n";
    const id = interfaceID(body);
    assert.equal(id.byteLength, 32);
    assert.equal(interfaceIDHex(id), "c31c470dd8db21db3bc8709bdcad7778a3d2dead33193c95b9691a4f0ba50dc8");
});

test("hashes exact bytes and returns independent byte storage", () => {
    const first = sha256(new Uint8Array([0, 1, 2, 255]));
    const second = sha256(new Uint8Array([0, 1, 2, 255]));
    assert.deepEqual(first, second);
    first[0] ^= 0xff;
    assert.notDeepEqual(first, second);
});

test("rejects malformed interface ID formatting input", () => {
    assert.throws(() => interfaceIDHex(new Uint8Array(31)), RangeError);
});
