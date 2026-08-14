import assert from "node:assert/strict";
import test from "node:test";
import {
    parseOverride,
    parseOverrides,
    parseSelector,
    resolveOverride,
    resolveSelector,
    selectorToString,
} from "../../dist/tool/index.js";
import { parseInterface, validateInterface } from "../../dist/syntax/index.js";

test("parses declaration, parameter, return, element, and field selectors", () => {
    for (const text of [
        "type:user",
        "type:user/field:address/field:city",
        "exception:failed",
        "exception:failed/element/field:code",
        "procedure:get_user",
        "procedure:get_user/param:name",
        "procedure:get_user/param:request/field:id",
        "procedure:get_user/return/field:id",
        "procedure:get_user/return/element/field:value",
    ]) {
        assert.equal(selectorToString(parseSelector(text)), text);
    }
});

test("keeps exact case-sensitive wire names", () => {
    const selector = parseSelector("procedure:GetUser/param:UserID");
    assert.equal(selector.name, "GetUser");
    assert.equal(selector.param, "UserID");
});

test("parses TypeScript name overrides", () => {
    const override = parseOverride("procedure:get_user/param:name=UserName");
    assert.equal(selectorToString(override.selector), "procedure:get_user/param:name");
    assert.equal(override.name, "UserName");
});

test("detects duplicate and unresolved overrides against an interface", () => {
    const file = parseInterface("fixture.intercall", new TextEncoder().encode("type user record { id uint64; }; procedure get { value record { id uint64; }; } user;"));
    validateInterface(file);
    assert.throws(() => parseOverrides(["type:user=User", "type:user=Other"]), /duplicate/);
    assert.throws(() => resolveSelector(file, parseSelector("type:missing")), /no declaration/);
    assert.throws(() => resolveOverride(file, "procedure:get/param:value/field:missing=Missing"), /no field/);
    const target = resolveOverride(file, "procedure:get/param:value/field:id=Identifier");
    assert.equal(target.target.field.name.name, "id");
});

test("rejects malformed selectors and overrides", () => {
    for (const text of [
        "user",
        "type:",
        "procedure:get/param:",
        "procedure:get/return",
        "procedure:get/field:value",
        "type:value/element",
        "type:value/field:",
        "type:value/field:x/element",
        "type:value/unknown:x",
    ]) {
        assert.throws(() => parseSelector(text), /invalid selector/);
    }
    assert.throws(() => parseOverride("type:value"), /expected SELECTOR/);
    assert.throws(() => parseOverride("type:value=not-valid"), /invalid TypeScript identifier/);
    assert.throws(() => parseOverride("type:value=class"), /invalid TypeScript identifier/);
});
