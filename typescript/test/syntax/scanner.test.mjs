import assert from "node:assert/strict";
import test from "node:test";
import {
    Scanner,
    SourceFile,
    SyntaxDiagnostic,
    TokenKind,
} from "../../dist/syntax/index.js";

const encoder = new TextEncoder();
const bytes = (value) => typeof value === "string" ? encoder.encode(value) : Uint8Array.from(value);

function scanAll(value) {
    const scanner = new Scanner(new SourceFile("fixture.intercall", bytes(value)));
    const tokens = [];
    while (true) {
        const token = scanner.next();
        tokens.push(token);
        if (token.kind === TokenKind.EOF) return { scanner, tokens };
    }
}

test("scans basic tokens and exact byte spans", () => {
    const { tokens } = scanAll("type user record { name string; };");
    assert.deepEqual(tokens.map(({ kind, span, literal }) => ({ kind, span, literal })), [
        { kind: TokenKind.Type, span: { start: 0, end: 4 }, literal: "type" },
        { kind: TokenKind.Ident, span: { start: 5, end: 9 }, literal: "user" },
        { kind: TokenKind.Record, span: { start: 10, end: 16 }, literal: "record" },
        { kind: TokenKind.LBrace, span: { start: 17, end: 18 }, literal: "" },
        { kind: TokenKind.Ident, span: { start: 19, end: 23 }, literal: "name" },
        { kind: TokenKind.String, span: { start: 24, end: 30 }, literal: "string" },
        { kind: TokenKind.Semicolon, span: { start: 30, end: 31 }, literal: "" },
        { kind: TokenKind.RBrace, span: { start: 32, end: 33 }, literal: "" },
        { kind: TokenKind.Semicolon, span: { start: 33, end: 34 }, literal: "" },
        { kind: TokenKind.EOF, span: { start: 34, end: 34 }, literal: "" },
    ]);
});

test("scans long identifiers without argument-stack expansion", () => {
    const identifier = "a".repeat(100_000);
    const { tokens } = scanAll(identifier);
    assert.equal(tokens[0].kind, TokenKind.Ident);
    assert.equal(tokens[0].literal.length, identifier.length);
    assert.equal(tokens[0].span.end, identifier.length);
});

test("recognizes exact lowercase keywords and comments", () => {
    const { tokens } = scanAll("Type list/*a /* nested */uint8");
    assert.deepEqual(tokens.map((token) => token.kind), [
        TokenKind.Ident,
        TokenKind.List,
        TokenKind.Comment,
        TokenKind.Uint8,
        TokenKind.EOF,
    ]);
    assert.equal(tokens[2].literal, "a /* nested ");
});

test("accepts all protocol whitespace and empty input", () => {
    const { tokens } = scanAll(" \t\r\n\f\v ");
    assert.deepEqual(tokens, [{
        kind: TokenKind.EOF,
        span: { start: 7, end: 7 },
        literal: "",
    }]);
});

test("rejects invalid UTF-8 at the first bad byte", () => {
    const scanner = new Scanner(new SourceFile("f", bytes([0x61, 0xc3])));
    assert.equal(scanner.next().kind, TokenKind.Ident);
    assert.throws(() => scanner.next(), (error) => {
        assert.ok(error instanceof SyntaxDiagnostic);
        assert.equal(error.message, "invalid UTF-8 encoding");
        assert.deepEqual(error.span, { start: 1, end: 2 });
        assert.equal(error.position.offset, 1);
        return true;
    });
});

test("rejects invalid UTF-8 and BOMs inside and outside token scanning", () => {
    for (const value of [[0xc0, 0xaf], [0xed, 0xa0, 0x80], [0xf4, 0x90, 0x80, 0x80]]) {
        assert.throws(() => scanAll(value), /invalid UTF-8 encoding/);
    }
    assert.throws(() => scanAll([0xef, 0xbb, 0xbf, 0x74]), (error) => {
        assert.equal(error.message, "invalid byte-order mark");
        assert.deepEqual(error.span, { start: 0, end: 3 });
        return true;
    });
    const { tokens } = scanAll("/* \uFEFF */");
    assert.equal(tokens[0].literal, " \uFEFF ");
});

test("reports invalid characters and sticky errors", () => {
    const scanner = new Scanner(new SourceFile("f", bytes("a!")));
    assert.equal(scanner.next().literal, "a");
    let first;
    assert.throws(() => scanner.next(), (error) => {
        first = error;
        assert.match(error.message, /invalid character/);
        assert.equal(error.position.line, 1);
        assert.equal(error.position.column, 2);
        return true;
    });
    assert.throws(() => scanner.next(), (error) => {
        assert.equal(error, first);
        return true;
    });
});

test("reports unterminated comments after validating their body", () => {
    assert.throws(() => scanAll("/* never closed"), (error) => {
        assert.equal(error.message, "comment not terminated");
        assert.deepEqual(error.span, { start: 0, end: 15 });
        return true;
    });
    assert.throws(() => scanAll([0x2f, 0x2a, 0x20, 0x80]), (error) => {
        assert.equal(error.message, "invalid UTF-8 encoding");
        assert.equal(error.position.offset, 3);
        return true;
    });
});
