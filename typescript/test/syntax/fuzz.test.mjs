import assert from "node:assert/strict";
import fs from "node:fs";
import path from "node:path";
import test from "node:test";
import { fileURLToPath } from "node:url";
import {
    parseInterface,
    SyntaxDiagnostic,
} from "../../dist/syntax/index.js";

const corpus = path.resolve(
    path.dirname(fileURLToPath(import.meta.url)),
    "../fixtures/syntax/fuzz/FuzzParse",
);

function mutations(seed) {
    const result = [seed.slice(), seed.slice(0, Math.floor(seed.length / 2))];
    const positions = new Set([0, Math.floor(seed.length / 2), Math.max(0, seed.length - 1)]);
    for (const position of positions) {
        if (position < seed.length) {
            const flipped = seed.slice();
            flipped[position] ^= 0xff;
            result.push(flipped);
        }
        const inserted = new Uint8Array(seed.length + 1);
        inserted.set(seed.slice(0, position));
        inserted[position] = 0x2f;
        inserted.set(seed.slice(position), position + 1);
        result.push(inserted);
    }
    const appended = new Uint8Array(seed.length + 3);
    appended.set(seed);
    appended.set([0x2f, 0x2a, 0x2f], seed.length);
    result.push(appended);
    return result;
}

test("deterministic fuzz mutations never escape parser diagnostics", () => {
    const seeds = fs.readdirSync(corpus).sort();
    assert.ok(seeds.length > 0);
    let cases = 0;
    for (const name of seeds) {
        const seed = fs.readFileSync(path.join(corpus, name));
        for (const input of mutations(new Uint8Array(seed))) {
            cases += 1;
            try {
                parseInterface(`fuzz/${name}`, input);
            } catch (error) {
                assert.ok(error instanceof SyntaxDiagnostic, `${name}: unexpected ${error}`);
            }
        }
    }
    assert.ok(cases >= seeds.length * 5);
});
