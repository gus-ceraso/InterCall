import assert from "node:assert/strict";
import test from "node:test";
import {
    initialisms,
    isCanonicalWireName,
    isTypeScriptKeyword,
    isValidTypeScriptIdentifier,
    isValidWireName,
    longestInitialism,
    requireTypeScriptIdentifier,
    typeScriptToWire,
    wireToTypeScript,
} from "../../dist/tool/index.js";

test("keeps the fixed initialism table and longest-prefix behavior", () => {
    assert.equal(initialisms.length, 36);
    for (const initialism of initialisms) {
        const wire = initialism.toLowerCase();
        assert.equal(wireToTypeScript(wire, "pascal"), initialism);
        assert.equal(wireToTypeScript(wire, "camel"), wire);
        assert.equal(typeScriptToWire(initialism, "pascal"), wire);
    }
    assert.equal(longestInitialism("HTTPSClient"), "HTTPS");
    assert.equal(longestInitialism("UIDValue"), "UID");
    assert.equal(longestInitialism("unknown"), "");
});

test("projects wire names using the documented examples", () => {
    const cases = [
        ["user_id", "UserID", "userID"],
        ["http_url", "HTTPURL", "httpURL"],
        ["https_client", "HTTPSClient", "httpsClient"],
        ["utf8_value", "UTF8Value", "utf8Value"],
        ["api2_client", "Api2Client", "api2Client"],
        ["sha_value", "ShaValue", "shaValue"],
        ["a1_b2", "A1B2", "a1B2"],
    ];
    for (const [wire, pascal, camel] of cases) {
        assert.equal(wireToTypeScript(wire, "pascal"), pascal);
        assert.equal(wireToTypeScript(wire, "camel"), camel);
        assert.equal(typeScriptToWire(pascal, "pascal"), wire);
        assert.equal(typeScriptToWire(camel, "camel"), wire);
    }
});

test("requires canonical wire names for default projection", () => {
    for (const name of ["UserID", "userID", "user__id", "_user", "user_", "A", ""]) {
        assert.equal(isCanonicalWireName(name), false);
        assert.throws(() => wireToTypeScript(name, "pascal"));
    }
    for (const name of ["a", "a1", "a_b", "user_id", "id2", "utf8"]) {
        assert.equal(isCanonicalWireName(name), true);
    }
});

test("validates wire and native identifiers", () => {
    assert.equal(isValidWireName("_user"), true);
    assert.equal(isValidWireName("user_"), true);
    assert.equal(isValidWireName("123user"), false);
    assert.equal(isValidWireName("user-id"), false);
    assert.equal(isValidTypeScriptIdentifier("UserID"), true);
    assert.equal(isValidTypeScriptIdentifier("日本語"), true);
    assert.equal(isValidTypeScriptIdentifier("class"), false);
    assert.equal(isValidTypeScriptIdentifier("_"), true);
    assert.equal(isTypeScriptKeyword("class"), true);
    assert.equal(isTypeScriptKeyword("from"), true);
});

test("rejects quoted, dotted, and computed spellings instead of escaping them", () => {
    for (const spelling of ["\"value\"", "'value'", "value.name", "[value]", "value-name", "value name"]) {
        assert.equal(isValidTypeScriptIdentifier(spelling), false);
        assert.throws(() => requireTypeScriptIdentifier(spelling));
    }
    assert.equal(requireTypeScriptIdentifier("valueName"), "valueName");
});

test("checks the exact source-to-wire inverse", () => {
    const accepted = [
        ["HTTPServer", "http_server", "pascal"],
        ["HTTPURL", "http_url", "pascal"],
        ["HTTPSClient", "https_client", "pascal"],
        ["UserID", "user_id", "pascal"],
        ["Version42ID", "version42_id", "pascal"],
        ["UTF8Value", "utf8_value", "pascal"],
        ["ShaValue", "sha_value", "pascal"],
        ["userID", "user_id", "camel"],
        ["httpURL", "http_url", "camel"],
        ["api2Client", "api2_client", "camel"],
        ["a1B2", "a1_b2", "camel"],
    ];
    for (const [name, wire, nameCase] of accepted) {
        assert.equal(typeScriptToWire(name, nameCase), wire);
    }
    for (const [name, nameCase] of [["SHAValue", "pascal"], ["API2Client", "pascal"], ["User_ID", "pascal"], ["userID", "pascal"], ["UserID", "camel"], ["日本語", "pascal"]]) {
        assert.throws(() => typeScriptToWire(name, nameCase));
    }
});
