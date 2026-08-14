import assert from "node:assert/strict";
import test from "node:test";
import { parseCliArguments } from "../../dist/cli/args.js";

test("parses import and repeatable export selectors", () => {
    assert.deepEqual(parseCliArguments(["import", "--out", "gen", "api.intercall"]), { kind: "import", out: "gen", interfacePath: "api.intercall" });
    assert.deepEqual(parseCliArguments(["export", "--project", "tsconfig.json", "--out", "gen", "--interface", "api.intercall", "src/provider.ts", "--include", "add", "--exclude", "skip"]), {
        kind: "export", project: "tsconfig.json", out: "gen", interfacePath: "api.intercall", sources: ["src/provider.ts"], include: ["add"], exclude: ["skip"],
    });
});

test("rejects malformed options deterministically", () => {
    assert.throws(() => parseCliArguments(["export", "--out", "gen"]), /--project/);
    assert.throws(() => parseCliArguments(["import", "--out", "gen", "--interface", "x", "api"]), /invalid import/);
    assert.throws(() => parseCliArguments(["wat"]), /unknown command/);
});
