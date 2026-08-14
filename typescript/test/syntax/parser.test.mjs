import assert from "node:assert/strict";
import test from "node:test";
import {
    TokenKind,
    parseInterface,
    SyntaxDiagnostic,
} from "../../dist/syntax/index.js";

const encoder = new TextEncoder();
const parse = (source) => parseInterface("fixture.intercall", encoder.encode(source));

test("parses declarations, parameters, returns, and comments", () => {
    const file = parse(`/* type docs */
type user record { name string; };
exception denied;
procedure greet { value user; } string;
`);
    assert.equal(file.declarations.length, 3);
    assert.equal(file.comments.length, 1);
    const [type, exception, procedure] = file.declarations;
    assert.equal(type.kind, "type-decl");
    assert.equal(type.name.name, "user");
    assert.equal(type.type.kind, "record");
    assert.equal(type.type.fields[0].name.name, "name");
    assert.equal(type.type.fields[0].type.primitive, TokenKind.String);
    assert.equal(exception.kind, "exception-decl");
    assert.equal(exception.type, undefined);
    assert.equal(procedure.kind, "procedure-decl");
    assert.equal(procedure.params[0].type.kind, "named");
    assert.equal(procedure.result.kind, "primitive");
});

test("parses empty interfaces and zero-width records", () => {
    assert.equal(parse("").declarations.length, 0);
    const file = parse("type empty record {}; procedure ping {};\n");
    assert.equal(file.declarations[0].type.kind, "record");
    assert.equal(file.declarations[0].type.fields.length, 0);
    assert.equal(file.declarations[1].params.length, 0);
    assert.equal(file.declarations[1].result, undefined);
});

test("parses deeply nested lists and records without recursive type parsing", () => {
    let source = "type deep ";
    for (let i = 0; i < 5_000; i += 1) source += "list ";
    source += "record { value uint8; };";
    const file = parse(source);
    let type = file.declarations[0].type;
    for (let i = 0; i < 5_000; i += 1) {
        assert.equal(type.kind, "list");
        type = type.elem;
    }
    assert.equal(type.kind, "record");
    assert.equal(type.fields[0].name.name, "value");
});

test("keeps comments in source order while parsing", () => {
    const file = parse("/* a */ type x uint8; /* b */ exception e;");
    assert.deepEqual(file.comments.map((comment) => comment.text), [" a ", " b "]);
});

test("reports grammar errors as positioned diagnostics", () => {
    assert.throws(() => parse("type x ;"), (error) => {
        assert.ok(error instanceof SyntaxDiagnostic);
        assert.match(error.message, /expected type/);
        assert.equal(error.position.line, 1);
        return true;
    });
    assert.throws(() => parse("procedure p { x uint8;"), (error) => {
        assert.ok(error instanceof SyntaxDiagnostic);
        assert.match(error.message, /expected '}'/);
        return true;
    });
});
