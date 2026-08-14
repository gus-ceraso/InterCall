import assert from "node:assert/strict";
import test from "node:test";
import {
    attachDocumentation,
    normalizeDocumentation,
    parseInterface,
} from "../../dist/syntax/index.js";

const parseDocs = (source) => {
    const file = parseInterface("fixture.intercall", new TextEncoder().encode(source));
    attachDocumentation(file);
    return file;
};

test("normalizes comment bodies and joins comment groups", () => {
    assert.equal(normalizeDocumentation("\r\n  first  \r\n    second\r"), "first\n  second");
    assert.equal(normalizeDocumentation("\n \t\n"), "");
    const file = parseDocs("/* first */\n/* second */\ntype value uint8;");
    assert.equal(file.declarations[0].doc, "first\n\nsecond");
});

test("attaches declaration, parameter, field, and type-occurrence docs", () => {
    const file = parseDocs(`/* declaration */
procedure draw {
    /* parameter */
    value record {
        /* field */
        x /* field type */ uint8;
    };
} /* return prefix */ list /* return element */ uint8;
`);
    const procedure = file.declarations[0];
    assert.equal(procedure.doc, "declaration");
    assert.equal(procedure.params[0].doc, "parameter");
    assert.equal(procedure.params[0].type.doc, "");
    assert.equal(procedure.params[0].type.fields[0].doc, "field");
    assert.equal(procedure.params[0].type.fields[0].type.doc, "field type");
    assert.equal(procedure.result.doc, "return prefix");
    assert.equal(procedure.result.elem.doc, "return element");
});

test("supports comments after type prefixes on the same line", () => {
    const file = parseDocs("type value /* underlying */ list /* element */ uint8;");
    const type = file.declarations[0];
    assert.equal(type.doc, "");
    assert.equal(type.type.doc, "underlying");
    assert.equal(type.type.elem.doc, "element");
});

test("does not attach trailing comments or comments separated by blank lines", () => {
    const file = parseDocs(`type first uint8; /* trailing */ exception second;

/* detached */

/* attached */
procedure third {};
`);
    assert.equal(file.declarations[1].doc, "");
    assert.equal(file.declarations[2].doc, "attached");
});

test("attaching repeatedly is idempotent", () => {
    const file = parseDocs("/* value */ type value uint8;");
    attachDocumentation(file);
    assert.equal(file.declarations[0].doc, "value");
});
