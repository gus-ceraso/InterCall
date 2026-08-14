import assert from "node:assert/strict";
import { access, mkdtemp, readFile, rm } from "node:fs/promises";
import { tmpdir } from "node:os";
import { join } from "node:path";
import { execFile } from "node:child_process";
import { promisify } from "node:util";
import test from "node:test";

const run = promisify(execFile);

test("CLI export discovers providers and writes owned artifacts", async () => {
    const root = await mkdtemp(join(tmpdir(), "intercall-export-"));
    const out = join(root, "generated");
    const interfacePath = join(root, "api", "browser.intercall");
    await run(process.execPath, ["dist/cli/main.js", "export", "--project", "test/fixtures/compiler/tsconfig-discovery.json", "--out", out, "--interface", interfacePath, "test/fixtures/compiler/discovery.ts"], { cwd: process.cwd() });
    const binding = (await readFile(join(out, "binding_gen.ts"))).toString();
    assert.match(binding, /binding: export sha256:/);
    assert.match(binding, /provider_0/);
    assert.match(binding, /decodeProgram/);
    assert.match((await readFile(interfacePath)).toString(), /artifact sha256:/);
    await assert.rejects(() => access(join(root, "missing")));
    await rm(root, { recursive: true, force: true });
});
