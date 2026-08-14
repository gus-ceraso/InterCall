import assert from "node:assert/strict";
import { access, mkdtemp, rm } from "node:fs/promises";
import { tmpdir } from "node:os";
import { join } from "node:path";
import test from "node:test";
import { validateBeforeOutput } from "../../dist/cli/transaction.js";

test("does not create output after validation failure", async () => {
    const root = await mkdtemp(join(tmpdir(), "intercall-test-"));
    const out = join(root, "generated");
    await assert.rejects(() => validateBeforeOutput(out, () => { throw new Error("invalid"); }, async () => {}), /invalid/);
    await assert.rejects(() => access(out));
    await rm(root, { recursive: true, force: true });
});
