import assert from "node:assert/strict";
import test from "node:test";
import {
    manglePrivate,
    PublicNameScope,
} from "../../dist/tool/index.js";

test("private names are deterministic and content-derived", () => {
    const first = manglePrivate("codec", ["record", "x", "uint32"]);
    assert.equal(first, manglePrivate("codec", ["record", "x", "uint32"]));
    assert.notEqual(first, manglePrivate("codec", ["record", "x", "uint64"]));
    assert.notEqual(first, manglePrivate("dispatch", ["record", "x", "uint32"]));
    assert.match(first, /^_[A-Za-z0-9_$]+$/u);
    assert.equal(first.length, "_intercall_codec_".length + 64);
});

test("public names reject collisions instead of being numbered", () => {
    const scope = new PublicNameScope();
    scope.claim("User");
    assert.equal(scope.has("User"), true);
    assert.throws(() => scope.claim("User"), /public TypeScript name collision/);
    assert.equal(scope.has("Other"), false);
});
