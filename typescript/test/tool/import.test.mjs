import assert from "node:assert/strict";
import test from "node:test";
import { parseInterface, validateInterface } from "../../dist/syntax/index.js";
import { buildImportGeneration } from "../../dist/tool/index.js";

test("builds deterministic import records with resolved source declarations", () => {
    const file = parseInterface("import.intercall", new TextEncoder().encode(
        "type user_id uint64; type user record { id user_id; name string; }; exception failed user; procedure get { value user; } user;",
    ));
    validateInterface(file);
    const generation = buildImportGeneration(file);
    assert.deepEqual(generation.namedTypes.map((item) => item.nativeName), ["UserID", "User"]);
    assert.deepEqual(generation.procedures[0].parameters.map((item) => item.nativeName), ["value"]);
    assert.equal(generation.exceptions[0].nativeName, "Failed");
    assert.equal(generation.fields.some((item) => item.field.name.name === "id" && item.nativeName === "id"), true);
    assert.equal(generation.namedTypes[1].type.kind, "record");
});

test("keeps empty import records empty", () => {
    const file = parseInterface("empty.intercall", new TextEncoder().encode(""));
    validateInterface(file);
    const generation = buildImportGeneration(file);
    assert.deepEqual(generation.declarations, []);
    assert.deepEqual(generation.namedTypes, []);
    assert.deepEqual(generation.procedures, []);
});
