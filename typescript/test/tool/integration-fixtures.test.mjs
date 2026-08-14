import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import test from "node:test";
import { formatInterface, parseInterface, validateInterface } from "../../dist/syntax/index.js";
import { interfaceID, interfaceIDHex } from "../../dist/tool/index.js";

function load(name) {
    const source = new TextEncoder().encode(readFileSync(new URL(`../fixtures/integration/${name}`, import.meta.url)));
    const file = parseInterface(name, source);
    validateInterface(file);
    return file;
}

test("backend and browser interoperability fixtures are canonical and equivalent", () => {
    const backend = load("backend.intercall");
    const browser = load("browser.intercall");
    assert.equal(formatInterface(backend), formatInterface(browser));
    assert.equal(interfaceIDHex(interfaceID(formatInterface(backend))), interfaceIDHex(interfaceID(formatInterface(browser))));
    assert.ok(backend.declarations.some((declaration) => declaration.kind === "procedure-decl" && declaration.result === undefined));
    assert.ok(backend.declarations.some((declaration) => declaration.kind === "type-decl" && declaration.type.kind === "record" && declaration.type.fields.length === 0));
});
