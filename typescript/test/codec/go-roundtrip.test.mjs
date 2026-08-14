import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import { spawnSync } from "node:child_process";
import test from "node:test";
import { parseInterface, validateInterface } from "../../dist/syntax/index.js";
import { compileCodecProgram } from "../../dist/tool/index.js";
import { decodeProgram, encodeProgram } from "../../dist/runtime/codec-vm.js";

const fixture = parseInterface("go-import-fixture.intercall", new TextEncoder().encode(
    readFileSync(new URL("../fixtures/codec/import.intercall", import.meta.url)),
));
validateInterface(fixture);
function type(name) { return fixture.declarations.find((item) => item.kind === "type-decl" && item.name.name === name).type; }
function parameter(procedure, name) { return fixture.declarations.find((item) => item.kind === "procedure-decl" && item.name.name === procedure).params.find((item) => item.name.name === name).type; }
function hex(bytes) { return [...bytes].map((byte) => byte.toString(16).padStart(2, "0")).join(""); }

function random(seed) {
    let state = BigInt(seed);
    return () => {
        state = (state * 6364136223846793005n + 1442695040888963407n) & ((1n << 64n) - 1n);
        return state;
    };
}

function vectors() {
    const next = random(0x51a7n);
    const result = [];
    for (let index = 0; index < 48; index += 1) {
        const unsigned = next();
        const signed = BigInt.asIntN(64, next());
        const bytes = Uint8Array.from({ length: Number(next() % 24n) }, () => Number(next() & 0xffn));
        const list = Array.from({ length: Number(next() % 24n) }, () => Number(next() & 0xffn));
        const point = { x: Number(BigInt.asIntN(32, next()) % 100000n) / 10, y: Number(BigInt.asIntN(32, next()) % 100000n) / 10 };
        const text = ["", "hello", "héllo", "\u{1f30d}", `value-${index}`][index % 5];
        result.push({ kind: "uint64", root: type("user_id"), value: unsigned.toString() });
        result.push({ kind: "int64", root: parameter("add", "a"), value: signed.toString() });
        result.push({ kind: "string", root: parameter("echo", "value"), value: text });
        result.push({ kind: "bytes", root: type("blob"), value: [...bytes] });
        result.push({ kind: "list-uint8", root: parameter("wave", "samples"), value: list });
        result.push({ kind: "point", root: type("point"), value: point });
    }
    return result;
}

test("exchanges randomized fixture vectors with Go in both directions", () => {
    const all = vectors();
    const requests = [];
    for (const vector of all) {
        const program = compileCodecProgram(fixture, vector.root);
        vector.wire = encodeProgram(program, vector.kind === "uint64" || vector.kind === "int64" ? BigInt(vector.value) : vector.kind === "bytes" ? Uint8Array.from(vector.value) : vector.value);
        requests.push(JSON.stringify({ op: "encode", kind: vector.kind, value: vector.value }));
        requests.push(JSON.stringify({ op: "decode", kind: vector.kind, hex: hex(vector.wire) }));
    }
    const goRoot = new URL("../../../go", import.meta.url);
    const result = spawnSync("go", ["run", "./internal/tool/tsvector"], {
        cwd: goRoot,
        encoding: "utf8",
        input: `${requests.join("\n")}\n`,
        maxBuffer: 8 * 1024 * 1024,
    });
    assert.equal(result.status, 0, result.stderr);
    const responses = result.stdout.trim().split("\n").map((line) => JSON.parse(line));
    assert.equal(responses.length, requests.length);
    for (let index = 0; index < all.length; index += 1) {
        const vector = all[index];
        const encoded = responses[index * 2];
        const decoded = responses[index * 2 + 1];
        assert.equal(encoded.error, undefined);
        assert.equal(encoded.hex, hex(vector.wire));
        assert.equal(decoded.error, undefined);
        assert.deepEqual(decoded.value, vector.value);
    }
});
