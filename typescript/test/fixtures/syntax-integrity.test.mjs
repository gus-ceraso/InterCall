import assert from "node:assert/strict";
import fs from "node:fs";
import path from "node:path";
import test from "node:test";
import { fileURLToPath } from "node:url";

const repository = path.resolve(path.dirname(fileURLToPath(import.meta.url)), "../../..");
const goFixtures = path.join(repository, "go/internal/syntax/testdata");
const tsFixtures = path.join(repository, "typescript/test/fixtures/syntax");

function files(root) {
    const result = [];
    function visit(directory, relative) {
        for (const entry of fs.readdirSync(directory, { withFileTypes: true })) {
            const childRelative = path.join(relative, entry.name);
            const child = path.join(directory, entry.name);
            if (entry.isDirectory()) visit(child, childRelative);
            else if (entry.isFile()) result.push(childRelative);
        }
    }
    visit(root, "");
    return result.sort();
}

test("TypeScript syntax fixtures preserve the Go fixture corpus", () => {
    const expected = files(goFixtures);
    const actual = files(tsFixtures);
    assert.deepEqual(actual, expected);
    for (const relative of expected) {
        assert.deepEqual(
            fs.readFileSync(path.join(tsFixtures, relative)),
            fs.readFileSync(path.join(goFixtures, relative)),
            `fixture differs: ${relative}`,
        );
    }
});
