import assert from "node:assert/strict";
import test from "node:test";
import {
    assertExportBinding,
    assertImportBinding,
    bindingDispatch,
    bindingInterfaceID,
} from "../../dist/runtime/binding.js";
import {
    createExportBinding,
    createExportBindingWithInterfaceID,
    createImportBinding,
    createImportBindingWithInterfaceID,
} from "../../dist/generated-spi/index.js";

const dispatch = async () => ({ exceptionKey: 0n, payload: new Uint8Array() });

test("creates frozen opaque handles and preserves dispatch identity", () => {
    const exported = createExportBinding(dispatch);
    const imported = createImportBinding();
    assert.equal(Object.isFrozen(exported), true);
    assert.equal(Object.isFrozen(imported), true);
    assert.deepEqual(Object.keys(exported), []);
    assert.equal(bindingDispatch(exported), dispatch);
    assert.equal(assertExportBinding(exported), exported);
    assert.equal(assertImportBinding(imported), imported);
    assert.throws(() => assertImportBinding(exported), /import binding/);
    assert.throws(() => assertExportBinding(imported), /export binding/);
    assert.throws(() => assertImportBinding(Object.freeze({})), /invalid/);
});

test("copies optional metadata and preserves present all-zero IDs", () => {
    const input = Uint8Array.from({ length: 32 }, (_, index) => index);
    const exported = createExportBindingWithInterfaceID(dispatch, input);
    input[0] = 255;
    const first = bindingInterfaceID(exported);
    assert.equal(first[0], 0);
    first[1] = 255;
    assert.equal(bindingInterfaceID(exported)[1], 1);
    assert.equal(bindingInterfaceID(createImportBinding()), undefined);
    const zero = bindingInterfaceID(createImportBindingWithInterfaceID(new Uint8Array(32)));
    assert.equal(zero.length, 32);
    assert.equal(zero.every((byte) => byte === 0), true);
    assert.throws(() => createImportBindingWithInterfaceID(new Uint8Array(31)), /32 bytes/);
});
