import assert from "node:assert/strict";
import test from "node:test";
import { parseInterface, validateInterface } from "../../dist/syntax/index.js";
import { buildImportGeneration, buildValidatedImportGeneration, emitImportCodecPrograms } from "../../dist/tool/index.js";

test("emits immutable flat programs for all reachable roots", () => {
    const file = parseInterface("codec-output.intercall", new TextEncoder().encode(
        "type user record { id uint64; }; exception failed user; procedure get { value user; } list user;",
    ));
    validateInterface(file);
    const generation = buildValidatedImportGeneration(file, buildImportGeneration(file));
    const output = emitImportCodecPrograms(file, generation);
    assert.match(output, /makeCodecProgram/);
    assert.match(output, /requireCodecProgram/);
    assert.match(output, /\"record\"/);
    assert.match(output, /\"named\"/);
    assert.match(output, /\"list\"/);
    assert.equal(output, emitImportCodecPrograms(file, generation));
});
