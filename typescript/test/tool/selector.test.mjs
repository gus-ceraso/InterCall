import assert from "node:assert/strict";
import test from "node:test";
import {
    parseOverride,
    parseSelector,
    selectorToString,
} from "../../dist/tool/index.js";

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
