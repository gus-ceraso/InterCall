import { ConnectionRuntime, type RuntimeTransport } from "../runtime/connection-runtime.js";
import { WebSocketMessageQueue } from "./message-queue.js";
import { bindWebSocketEvents } from "./events.js";
import type { ConnectionBindings, WebSocketConnectionOptions } from "./types.js";
import type { WebSocketLike, WebSocketConstructor } from "./socket.js";
import { WEB_SOCKET_OPEN, openWebSocket } from "./socket.js";
import { normalizeWebSocketOptions } from "./options.js";
import { resolveWebSocketURL } from "./url.js";
import { assertExportBinding, assertImportBinding } from "../runtime/binding.js";
import type { Connection } from "../runtime/types.js";

export async function connectRawSocket(
    url: string | URL,
    bindings: ConnectionBindings,
    options?: WebSocketConnectionOptions,
    constructor?: WebSocketConstructor,
): Promise<Connection> {
    const normalized = normalizeWebSocketOptions(options);
    assertImportBinding(bindings.importBinding);
    assertExportBinding(bindings.exportBinding);
    // Resolve before constructing so malformed URLs fail without opening a socket.
    resolveWebSocketURL(url);
    const socket = await openWebSocket(url, normalized, constructor);
    return attachRawSocket(socket, bindings, normalized.messageLimit);
}

export function attachRawSocket(
    socket: WebSocketLike,
    bindings: ConnectionBindings,
    messageLimit: number,
    existingQueue?: WebSocketMessageQueue,
): Connection {
    const queue = existingQueue ?? new WebSocketMessageQueue(messageLimit);
    let runtime: ConnectionRuntime | undefined;
    let events: { cleanup: () => void } | undefined;
    const transport: RuntimeTransport = {
        bufferedAmount: () => socket.bufferedAmount,
        isOpen: () => socket.readyState === WEB_SOCKET_OPEN,
        send: (frame) => socket.send(frame),
        stopReceiving: () => {
            events?.cleanup();
            queue.clear();
            runtime?.core.markReceiveStopped();
        },
        close: () => { try { socket.close(); } catch { /* terminal cause is already selected */ } },
    };
    runtime = new ConnectionRuntime(transport, bindings.importBinding, bindings.exportBinding);
    events = bindWebSocketEvents(socket, queue, (chunk) => runtime!.receiveChunk(chunk), (cause) => {
        events?.cleanup();
        queue.clear();
        runtime!.transportClosed(cause);
    });
    void runtime.closed.then(() => {
        events?.cleanup();
        queue.clear();
    });
    return runtime.connection;
}
