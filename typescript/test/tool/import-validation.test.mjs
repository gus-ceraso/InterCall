import assert from "node:assert/strict";
import test from "node:test";
import { parseInterface, validateInterface } from "../../dist/syntax/index.js";
import { buildImportGeneration } from "../../dist/tool/index.js";
import { buildValidatedImportGeneration } from "../../dist/tool/index.js";

test("applies selectors and validates helper, local, fixed, and depth scopes", () => {
    const file = parseInterface("validated.intercall", new TextEncoder().encode(
        "type user record { first uint8; second uint8; }; exception procedure_not_found; procedure get { value record { first uint8; second uint8; }; } user;",
    ));
    validateInterface(file);
    const base = buildImportGeneration(file);
    const generation = buildValidatedImportGeneration(file, base, ["type:user=UserValue", "procedure:get/param:value/field:first=initial"]);
    assert.equal(generation.namedTypes[0].nativeName, "UserValue");
    assert.equal(generation.fields.find((field) => field.field.name.name === "first").nativeName, "initial");
    assert.throws(() => buildValidatedImportGeneration(file, base, ["type:user=EmptyRecord"]), /collision/);
    assert.throws(() => buildValidatedImportGeneration(file, base, ["procedure:get/param:value/field:first=second", "procedure:get/param:value/field:second=second"]), /collision/);
});

test("rejects payloads on fixed exceptions", () => {
    const file = parseInterface("fixed.intercall", new TextEncoder().encode("exception internal_exception record { value uint8; };"));
    validateInterface(file);
    assert.throws(() => buildValidatedImportGeneration(file, buildImportGeneration(file)), /fixed exception/);
});
