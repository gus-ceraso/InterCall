import { abortReason } from "../runtime/call-order.js";
import { TransportError } from "../runtime/errors.js";
import { resolveWebSocketURL } from "./url.js";
import type { NormalizedWebSocketOptions } from "./options.js";

export interface WebSocketLike {
    binaryType: BinaryType;
    readonly readyState: number;
    readonly bufferedAmount: number;
    send(data: ArrayBuffer | ArrayBufferView): void;
    close(): void;
    addEventListener(type: string, listener: EventListener): void;
    removeEventListener(type: string, listener: EventListener): void;
}

export type WebSocketConstructor = new (url: string, protocols?: string | string[]) => WebSocketLike;

export const WEB_SOCKET_OPEN = 1;

export async function openWebSocket(
    input: string | URL,
    options: NormalizedWebSocketOptions,
    constructor: WebSocketConstructor = globalThis.WebSocket,
): Promise<WebSocketLike> {
    const url = resolveWebSocketURL(input);
    let socket: WebSocketLike;
    try {
        socket = options.protocols === undefined
            ? new constructor(url.href)
            : new constructor(url.href, Array.isArray(options.protocols) ? [...options.protocols] as string[] : options.protocols as string);
    } catch (error) {
        throw new TransportError("intercall: WebSocket construction failed", { cause: error });
    }
    socket.binaryType = "arraybuffer";
    return new Promise<WebSocketLike>((resolve, reject) => {
        let settled = false;
        const timer = globalThis.setTimeout(() => finish(new TransportError("intercall: WebSocket open timeout")), options.openTimeoutMs);
        const cleanup = () => {
            globalThis.clearTimeout(timer);
            socket.removeEventListener("open", onOpen);
            socket.removeEventListener("error", onError);
            socket.removeEventListener("close", onClose);
            options.signal?.removeEventListener("abort", onAbort);
        };
        const finish = (error?: Error) => {
            if (settled) return;
            settled = true;
            cleanup();
            if (error === undefined) resolve(socket);
            else {
                try { socket.close(); } catch { /* setup failure is already selected */ }
                reject(error);
            }
        };
        const onOpen = () => finish();
        const onError = () => finish(new TransportError("intercall: WebSocket open failed"));
        const onClose = () => finish(new TransportError("intercall: WebSocket closed before open"));
        const onAbort = () => finish(abortReason(options.signal!));
        socket.addEventListener("open", onOpen);
        socket.addEventListener("error", onError);
        socket.addEventListener("close", onClose);
        if (options.signal?.aborted) onAbort();
        else options.signal?.addEventListener("abort", onAbort, { once: true });
    });
}
