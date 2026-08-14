import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import test from "node:test";
import { parseInterface, validateInterface } from "../../dist/syntax/index.js";
import { compileCodecProgram } from "../../dist/tool/index.js";
import { decodeProgram, encodeProgram } from "../../dist/runtime/codec-vm.js";

const source = new TextEncoder().encode(readFileSync(new URL("../fixtures/codec/import.intercall", import.meta.url)));
const file = parseInterface("go-import-fixture.intercall", source);
validateInterface(file);

function declaration(kind, name) {
    return file.declarations.find((item) => item.kind === kind && item.name.name === name);
}
function type(name) {
    return declaration("type-decl", name).type;
}
function parameter(procedure, name) {
    return declaration("procedure-decl", procedure).params.find((item) => item.name.name === name).type;
}
function result(procedure) {
    return declaration("procedure-decl", procedure).result;
}
function hex(bytes) {
    return [...bytes].map((byte) => byte.toString(16).padStart(2, "0")).join("");
}
function encoded(root, value) {
    return encodeProgram(compileCodecProgram(file, root), value);
}
function check(root, value, expected) {
    const wire = encoded(root, value);
    assert.equal(hex(wire), expected);
    assert.deepEqual(decodeProgram(compileCodecProgram(file, root), wire), value);
}

test("matches Go fixture vectors for named and primitive values", () => {
    check(type("user_id"), 0x0102030405060708n, "0807060504030201");
    check(type("point"), { x: 1.5, y: -2.25 }, "000000000000f83f00000000000002c0");
    check(type("empty"), {}, "");
    check(type("names"), ["a", "é"], "02000000000000000100000000000000610200000000000000c3a9");
    check(type("blob"), Uint8Array.from([0, 255, 2]), "030000000000000000ff02");
    check(declaration("exception-decl", "failed").type, { code: -7, message: "bad" }, "f9ffffff0300000000000000626164");
});

test("matches Go fixture procedure payload vectors", () => {
    check(parameter("echo", "value"), "hé", "030000000000000068c3a9");
    check(parameter("add", "a"), -2n, "feffffffffffffff");
    check(parameter("sample", "data"), Uint8Array.from([1, 2, 3]), "0300000000000000010203");
    check(parameter("sample", "channel"), 255, "ff");
    check(parameter("wave", "samples"), [1, 2, 255], "03000000000000000102ff");
    check(parameter("paint", "origin"), { x: 1, y: -2 }, "01000000feffffff");
    check(parameter("paint", "size"), { width: 3, height: 4 }, "0300000004000000");
    check(parameter("locate", "box"), { corner: { x: -1, y: 2 }, label: "ok" }, "ffffffff0200000002000000000000006f6b");
    check(parameter("fetch", "id"), 0x0102030405060708n, "0807060504030201");
    check(result("load"), [{}, {}], "0200000000000000");
    check(result("stamp"), {}, "");
    check(undefined, {}, "");
});
