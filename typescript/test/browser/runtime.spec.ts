import { createServer, type Server } from "node:http";
import { spawn, type ChildProcessWithoutNullStreams } from "node:child_process";
import { readFile } from "node:fs/promises";
import { join, normalize } from "node:path";
import { test, expect } from "@playwright/test";

test.setTimeout(60_000);

let server: Server;
let origin: string;
let goServer: ChildProcessWithoutNullStreams;
let goURL: string;

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
    goURL = await startGoServer();
});

test.afterAll(async () => {
    goServer.kill("SIGTERM");
    await new Promise<void>((resolve) => goServer.once("exit", () => resolve()));
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

test("calls the checked-out Go WebSocket peer in both directions", async ({ page }) => {
    await page.goto(origin);
    const result = await page.evaluate(async (url) => {
        // @ts-expect-error The browser test server provides this absolute module path.
        const { call, createExportBindingWithInterfaceID, createImportBindingWithInterfaceID } = await import("/dist/generated-spi/index.js");
        // @ts-expect-error The browser test server provides this absolute module path.
        const { connectWebSocket } = await import("/dist/browser/index.js");
        const interfaceID = Uint8Array.from("ec88d7cc995138d903f338d73030e78769e95000e2f5be52385a36f1003e3707".match(/../gu)!.map((byte) => Number.parseInt(byte, 16)));
        const pingKey = 0xa3aa209174f340d1n;
        const imports = createImportBindingWithInterfaceID(interfaceID);
        const exports = createExportBindingWithInterfaceID(async (_context: unknown, key: bigint, payload: Uint8Array) => {
            if (key !== pingKey || payload.byteLength !== 0) return { exceptionKey: 0x3f5fc972f8477b07n, payload: new Uint8Array() };
            return { exceptionKey: 0n, payload: new Uint8Array() };
        }, interfaceID);
        const connection = await connectWebSocket(url, { importBinding: imports, exportBinding: exports });
        await call(connection, imports, pingKey, () => new Uint8Array(), (key: bigint, payload: Uint8Array) => {
            if (key !== 0n || payload.byteLength !== 0) throw new Error("invalid Go ping response");
        });
        connection.close();
        return true;
    }, goURL);
    expect(result).toBe(true);
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

async function startGoServer(): Promise<string> {
    const child = spawn("go", ["run", "./internal/integration/cmd/e2e-server"], { cwd: join(process.cwd(), "../go") });
    goServer = child;
    return new Promise<string>((resolve, reject) => {
        let output = "";
        const onData = (chunk: Buffer) => {
            output += chunk.toString();
            const match = /READY (ws:\/\/[^\n]+)/u.exec(output);
            if (match !== null) {
                child.stdout.off("data", onData);
                resolve(match[1]!);
            }
        };
        child.stdout.on("data", onData);
        child.once("error", reject);
        child.once("exit", (code) => reject(new Error(`Go WebSocket server exited with ${code}: ${output}`)));
    });
}
