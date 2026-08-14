import assert from "node:assert/strict";
import fs from "node:fs";
import { builtinModules } from "node:module";
import path from "node:path";
import test from "node:test";
import { fileURLToPath } from "node:url";

const testDirectory = path.dirname(fileURLToPath(import.meta.url));
const packageDirectory = path.resolve(testDirectory, "../..");
const distDirectory = path.join(packageDirectory, "dist");
const roots = [
    path.join(distDirectory, "index.js"),
    path.join(distDirectory, "browser", "index.js"),
    path.join(distDirectory, "generated-spi", "index.js"),
];

const nodeOnlyModules = new Set(
    builtinModules.map((specifier) =>
        specifier.startsWith("node:") ? specifier.slice(5) : specifier,
    ),
);

function importsFrom(source) {
    const imports = [];
    const pattern = /(?:\bfrom\s*|\bimport\s*\()(['"])([^'"\n]+)\1/g;
    for (let match = pattern.exec(source); match !== null; match = pattern.exec(source)) {
        imports.push(match[2]);
    }
    return imports;
}

function resolveRelative(from, specifier) {
    const base = path.resolve(path.dirname(from), specifier);
    const candidates = [base, `${base}.js`, path.join(base, "index.js")];
    for (const candidate of candidates) {
        if (fs.existsSync(candidate) && fs.statSync(candidate).isFile()) {
            return candidate;
        }
    }
    throw new Error(`cannot resolve emitted browser import ${specifier} from ${from}`);
}

test("emitted browser graph contains no Node-only imports", () => {
    for (const root of roots) {
        assert.ok(fs.existsSync(root), `missing emitted browser entry ${root}`);
    }

    const pending = [...roots];
    const visited = new Set();
    while (pending.length > 0) {
        const current = pending.pop();
        assert.ok(current);
        const normalized = path.resolve(current);
        if (visited.has(normalized)) continue;
        visited.add(normalized);

        const relative = path.relative(distDirectory, normalized);
        assert.ok(!relative.startsWith(".."), `browser graph escaped dist: ${normalized}`);
        const source = fs.readFileSync(normalized, "utf8");
        for (const specifier of importsFrom(source)) {
            if (specifier.startsWith("node:") || nodeOnlyModules.has(specifier)) {
                assert.fail(`${relative} imports Node-only module ${specifier}`);
            }
            if (specifier.startsWith(".")) {
                pending.push(resolveRelative(normalized, specifier));
            } else {
                assert.fail(`${relative} imports external browser dependency ${specifier}`);
            }
        }
    }
});
