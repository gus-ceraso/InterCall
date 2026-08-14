import assert from "node:assert/strict";
import test from "node:test";
import { resolveWebSocketURL } from "../../dist/browser/url.js";

test("resolves relative HTTP(S) URLs and preserves WebSocket URLs", () => {
    assert.equal(resolveWebSocketURL("/rpc", "https://example.test/app/index.html").href, "wss://example.test/rpc");
    assert.equal(resolveWebSocketURL("http://example.test/rpc").protocol, "ws:");
    assert.equal(resolveWebSocketURL(new URL("ws://example.test/rpc")).href, "ws://example.test/rpc");
    assert.equal(resolveWebSocketURL("wss://example.test/rpc").protocol, "wss:");
});

test("rejects unsupported and malformed URL schemes", () => {
    assert.throws(() => resolveWebSocketURL("file:///tmp/socket", "https://example.test/"), /must use/);
    assert.throws(() => resolveWebSocketURL("ftp://example.test/rpc", "https://example.test/"), /must use/);
});
