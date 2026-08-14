import assert from "node:assert/strict";
import test from "node:test";
import {
    declarationKey,
    parseInterface,
    SyntaxDiagnostic,
    validateInterface,
} from "../../dist/syntax/index.js";

const encode = (source) => new TextEncoder().encode(source);
const valid = (source) => validateInterface(parseInterface("fixture.intercall", encode(source)));

function invalid(source, message) {
    assert.throws(() => valid(source), (error) => {
        assert.ok(error instanceof SyntaxDiagnostic);
        assert.equal(error.message, message);
        return true;
    });
}

test("computes README FNV-0 key vectors", () => {
    assert.equal(declarationKey("procedure", "get_user"), 0x4c63cc5048869eb7n);
    assert.equal(declarationKey("exception", "procedure_not_found"), 0x970e76fcc5e2dacbn);
});

test("validates earlier named types and independent local scopes", () => {
    valid(`type id uint64;
procedure get { id id; } id;`);
});

test("rejects forward and unknown references", () => {
    invalid("type value later; type later uint8;", 'unresolved type reference "later" in type value');
    invalid("procedure get { value missing; };", 'unresolved type reference "missing" in parameter "value" of procedure get');
});

test("rejects duplicate global, parameter, and record-field names", () => {
    invalid("type value uint8; exception value;", 'duplicate exception name "value" (first declared at line 1)');
    invalid("procedure get { value uint8; value uint8; };", 'duplicate parameter "value" in procedure get');
    invalid("type value record { x uint8; x uint8; };", 'duplicate field "x" in type value');
});

test("rejects the checked-in key collision fixtures", () => {
    invalid(
        "procedure oopqz20a4nrzy {}; exception k53gkqh4kufnc;",
        'key collision: exception "k53gkqh4kufnc" collides with procedure "oopqz20a4nrzy"',
    );
    invalid(
        "exception can20qcvtehol; exception ejmcku0cl4u50;",
        'key collision: exception "ejmcku0cl4u50" collides with exception "can20qcvtehol"',
    );
    invalid(
        "procedure gaft2bn2kl5il {}; procedure an1bk5lqs3ekj {};",
        'key collision: procedure "an1bk5lqs3ekj" collides with procedure "gaft2bn2kl5il"',
    );
});

test("validates declarations that are not referenced", () => {
    valid("type unused record { nested list record { value uint8; }; }; exception unused_error record {}; procedure ping {};");
});
