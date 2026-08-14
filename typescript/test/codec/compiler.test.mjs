import assert from "node:assert/strict";
import test from "node:test";
import { parseInterface, validateInterface } from "../../dist/syntax/index.js";
import { compileCodecProgram } from "../../dist/tool/index.js";
import { decodeProgram, encodeProgram } from "../../dist/runtime/codec-vm.js";

function source() {
    return parseInterface("codec.intercall", new TextEncoder().encode(
        "type user record { id uint64; tags list string; }; type alias user; procedure get { value alias; } user;",
    ));
}

test("compiles syntax types into linked flat programs", () => {
    const file = source();
    validateInterface(file);
    const alias = file.declarations.find((declaration) => declaration.kind === "type-decl" && declaration.name.name === "alias");
    const program = compileCodecProgram(file, alias.type);
    assert.equal(program.instructions.some((instruction) => instruction.op === "named"), true);
    const value = { id: 42n, tags: ["one", "two"] };
    assert.deepEqual(decodeProgram(program, encodeProgram(program, value)), value);
});

test("compiles no-payload roots and rejects recursive codec types", () => {
    const file = source();
    const zero = compileCodecProgram(file, undefined);
    assert.equal(zero.zeroWidth, true);
    const recursive = parseInterface("recursive.intercall", new TextEncoder().encode("type node node;"));
    assert.throws(() => compileCodecProgram(recursive, recursive.declarations[0].type), /recursive codec/);
});
