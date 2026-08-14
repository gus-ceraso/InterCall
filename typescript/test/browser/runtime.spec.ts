import { createServer, type Server } from "node:http";
import { readFile } from "node:fs/promises";
import { join, normalize } from "node:path";
import { test, expect } from "@playwright/test";

let server: Server;
let origin: string;

 test.beforeAll(async () => {
    server = createServer(async (request, response) => {
        const pathname = new URL(request.url ?? "/", "http://localhost").pathname;
        if (pathname === "/") {
            response.setHeader("content-type", "text/html");
            response.end("<!doctype html><title>InterCall</title>");
            return;
        }
        if (!pathname.startsWith("/dist/")) {
            response.writeHead(404).end();
            return;
        }
        try {
            const root = process.cwd();
            const file = join(root, normalize(pathname));
            response.setHeader("content-type", file.endsWith(".js") ? "text/javascript" : "application/octet-stream");
            response.end(await readFile(file));
        } catch {
            response.writeHead(404).end();
        }
    });
    await new Promise<void>((resolve) => server.listen(0, "127.0.0.1", resolve));
    const address = server.address();
    if (address === null || typeof address === "string") throw new Error("test server did not bind TCP");
    origin = `http://127.0.0.1:${address.port}`;
});

test.afterAll(async () => {
    await new Promise<void>((resolve, reject) => server.close((error) => error ? reject(error) : resolve()));
});

test("browser entry has no Node dependency and resolves URLs", async ({ page }) => {
    await page.goto(origin);
    const result = await page.evaluate(async () => {
        // @ts-expect-error The browser test server provides this absolute module path.
        const browser = await import("/dist/browser/index.js");
        // @ts-expect-error The browser test server provides this absolute module path.
        const url = await import("/dist/browser/url.js");
        return {
            hasProcess: typeof (globalThis as { process?: unknown }).process !== "undefined",
            connect: typeof browser.connectWebSocket,
            resolved: url.resolveWebSocketURL("/rpc").protocol,
        };
    });
    expect(result.hasProcess).toBe(false);
    expect(result.connect).toBe("function");
    expect(result.resolved).toBe("ws:");
});

test("negotiates and calls through browser binary WebSocket APIs", async ({ page }) => {
    await page.goto(origin);
    const result = await page.evaluate(async () => {
        // @ts-expect-error The browser test server provides this absolute module path.
        const { call, createExportBindingWithInterfaceID, createImportBindingWithInterfaceID } = await import("/dist/generated-spi/index.js");
        // @ts-expect-error The browser test server provides this absolute module path.
        const { connectWebSocket } = await import("/dist/browser/index.js");
        const importID = Uint8Array.from({ length: 32 }, () => 1);
        const exportID = Uint8Array.from({ length: 32 }, () => 2);
        const sends: number[] = [];
        class FakeWebSocket {
            static OPEN = 1;
            static instances: FakeWebSocket[] = [];
            binaryType = "blob";
            readyState = 0;
            bufferedAmount = 0;
            listeners = new Map<string, ((event: { data?: unknown }) => void)[]>();
            constructor() {
                FakeWebSocket.instances.push(this);
                queueMicrotask(() => { this.readyState = 1; this.emit("open"); });
            }
            addEventListener(type: string, listener: (event: { data?: unknown }) => void) { this.listeners.set(type, [...(this.listeners.get(type) ?? []), listener]); }
            removeEventListener(type: string, listener: (event: { data?: unknown }) => void) { this.listeners.set(type, (this.listeners.get(type) ?? []).filter((item) => item !== listener)); }
            emit(type: string, data?: unknown) { for (const listener of this.listeners.get(type) ?? []) listener({ data }); }
            send(data: ArrayBuffer | ArrayBufferView) {
                const bytes = data instanceof ArrayBuffer ? new Uint8Array(data) : new Uint8Array(data.buffer, data.byteOffset, data.byteLength);
                sends.push(bytes.byteLength);
                if (bytes.byteLength === 32) this.emit("message", exportID.buffer.slice(0));
                else {
                    const id = new DataView(bytes.buffer, bytes.byteOffset, bytes.byteLength).getBigUint64(0, true);
                    const response = new Uint8Array(24);
                    const view = new DataView(response.buffer);
                    view.setBigUint64(0, id | (1n << 63n), true);
                    this.emit("message", response.buffer);
                }
            }
            close() { this.readyState = 3; }
        }
        (globalThis as { WebSocket?: unknown }).WebSocket = FakeWebSocket;
        const imports = createImportBindingWithInterfaceID(importID);
        const exports = createExportBindingWithInterfaceID(async () => ({ exceptionKey: 0n, payload: new Uint8Array() }), exportID);
        const connection = await connectWebSocket("ws://example.test/rpc", { importBinding: imports, exportBinding: exports });
        await call(connection, imports, 1n, () => new Uint8Array(), () => {});
        connection.close();
        return { sends, binaryType: FakeWebSocket.instances[0]?.binaryType };
    });
    expect(result.binaryType).toBe("arraybuffer");
    expect(result.sends).toEqual([32, 24]);
});
