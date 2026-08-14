import assert from "node:assert/strict";
import { execFileSync } from "node:child_process";
import { resolve } from "node:path";
import test from "node:test";

test("checked-out Go integration suite passes", () => {
    const output = execFileSync("go", ["test", "./internal/integration"], {
        cwd: resolve(process.cwd(), "../go"),
        encoding: "utf8",
        stdio: ["ignore", "pipe", "pipe"],
    });
    assert.match(output, /ok\s+/);
});
