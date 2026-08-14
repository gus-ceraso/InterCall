import assert from "node:assert/strict";
import { mkdtemp, readFile, symlink, lstat, rm } from "node:fs/promises";
import { tmpdir } from "node:os";
import { join } from "node:path";
import test from "node:test";
import { artifactStamp, replaceOwnedLeaf, validateOutputDirectory } from "../../dist/cli/ownership.js";

test("writes owned leaves, preserves unchanged bytes, and rejects symlinks", async () => {
    const root = await mkdtemp(join(tmpdir(), "intercall-owner-"));
    const path = join(root, "binding_gen.ts");
    const body = Buffer.from("export const value = 1;\n");
    await replaceOwnedLeaf(path, "import", body);
    const first = await readFile(path);
    assert.match(first.toString(), new RegExp(`sha256:${artifactStamp(body)}`));
    await replaceOwnedLeaf(path, "import", body);
    assert.deepEqual(await readFile(path), first);
    const target = join(root, "target.ts");
    await symlink(path, target);
    await assert.rejects(() => replaceOwnedLeaf(target, "import", body), /replaceable/);
    assert.equal((await lstat(path)).isFile(), true);
    const collision = join(root, "collision");
    await (await import("node:fs/promises")).mkdir(collision);
    await (await import("node:fs/promises")).writeFile(join(collision, "handwritten.ts"), "const x = 1;");
    await assert.rejects(() => validateOutputDirectory(collision), /unowned code/);
    await rm(root, { recursive: true, force: true });
});
