import assert from "node:assert/strict";
import fs from "node:fs";
import path from "node:path";
import test from "node:test";
import { fileURLToPath } from "node:url";
import {
    attachDocumentation,
    formatInterface,
    parseInterface,
    validateInterface,
} from "../../dist/syntax/index.js";

const fixtureRoot = path.resolve(
    path.dirname(fileURLToPath(import.meta.url)),
    "../fixtures/syntax",
);

function sourceFiles(directory) {
    return fs.readdirSync(path.join(fixtureRoot, directory), { withFileTypes: true })
        .filter((entry) => entry.isFile() && entry.name.endsWith(".intercall"))
        .map((entry) => path.join(directory, entry.name))
        .sort();
}

test("valid fixtures parse, validate, and attach documentation", () => {
    for (const relative of sourceFiles("valid")) {
        const file = parseInterface(relative, fs.readFileSync(path.join(fixtureRoot, relative)));
        validateInterface(file);
        attachDocumentation(file);
    }
});

test("canonical formatting matches every Go format golden", () => {
    for (const relative of sourceFiles("format")) {
        const inputPath = path.join(fixtureRoot, relative);
        const file = parseInterface(relative, fs.readFileSync(inputPath));
        validateInterface(file);
        attachDocumentation(file);
        const actual = Buffer.from(formatInterface(file), "utf8");
        const expected = fs.readFileSync(`${inputPath}.golden`);
        assert.deepEqual(actual, expected, `canonical output differs: ${relative}`);
    }
});
