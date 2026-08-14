import assert from "node:assert/strict";
import test from "node:test";
import {
    MAX_PROJECTION_DEPTH,
    validateProjectionDepth,
} from "../../dist/tool/index.js";
import { parseInterface, validateInterface, SyntaxDiagnostic } from "../../dist/syntax/index.js";

function deepInterface(listCount) {
    return `type deep ${"list ".repeat(listCount)}uint8;`;
}

function validate(source) {
    const file = parseInterface("depth.intercall", new TextEncoder().encode(source));
    validateInterface(file);
    validateProjectionDepth(file);
}

test("accepts exactly the 4,096-occurrence projection boundary", () => {
    validate(deepInterface(MAX_PROJECTION_DEPTH - 1));
});

test("rejects the first occurrence beyond the projection boundary", () => {
    assert.throws(() => validate(deepInterface(MAX_PROJECTION_DEPTH)), (error) => {
        assert.ok(error instanceof SyntaxDiagnostic);
        assert.equal(error.message, `resolved type depth exceeds ${MAX_PROJECTION_DEPTH} occurrences`);
        return true;
    });
});

test("handles much deeper projection checks without recursive calls", () => {
    assert.throws(() => validate(deepInterface(8_000)), SyntaxDiagnostic);
});
