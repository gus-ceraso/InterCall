import assert from "node:assert/strict";
import test from "node:test";
import {
    attachDocumentation,
    formatInterface,
    parseInterface,
} from "../../dist/syntax/index.js";

function format(source, attach = true) {
    const file = parseInterface("fixture.intercall", new TextEncoder().encode(source));
    if (attach) attachDocumentation(file);
    return formatInterface(file);
}

test("formats declarations and nested values canonically", () => {
    assert.equal(format("type point record { x float64; y float64; };"),
        "type point record {\n    x float64;\n    y float64;\n};\n");
    assert.equal(format("exception denied; procedure ping {};"),
        "exception denied;\n\nprocedure ping {};\n");
    assert.equal(format("procedure add { a int32; b int32; } int32;"),
        "procedure add {\n    a int32;\n    b int32;\n} int32;\n");
});

test("keeps empty records and parameter blocks inline", () => {
    assert.equal(format("type empty record {}; procedure ping {} record {};"),
        "type empty record {};\n\nprocedure ping {} record {};\n");
});

test("formats documented and nested type occurrences", () => {
    const source = `/* A procedure. */ procedure draw {
/* Value. */ value list /* Element. */ record {
/* X. */ x uint8;
};
} /* Return. */ record { /* Width. */ width uint16; };`;
    assert.equal(format(source), `/* A procedure. */
procedure draw {
    /* Value. */
    value list
    /* Element. */
    record {
        /* X. */
        x uint8;
    };
}
/* Return. */
record {
    /* Width. */
    width uint16;
};
`);
});

test("formats multiline documentation with indentation", () => {
    const source = `/*
    first

    second
*/
type value uint8;`;
    assert.equal(format(source), `/*
    first

    second
*/
type value uint8;\n`);
});

test("formats deeply nested lists without recursive calls", () => {
    let source = "type deep ";
    for (let i = 0; i < 5_000; i += 1) source += "list ";
    source += "uint8;";
    const output = format(source);
    assert.equal(output.startsWith("type deep list list "), true);
    assert.equal(output.endsWith("uint8;\n"), true);
});

test("empty interfaces format to empty bytes", () => {
    assert.equal(format("/* unattached */ \t\n"), "");
});
