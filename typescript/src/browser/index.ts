import type { Connection } from "../runtime/types.js";
import { bindingInterfaceID } from "../runtime/binding.js";
import { InterfaceMismatchError } from "../runtime/errors.js";
import { bindWebSocketEvents } from "./events.js";
import { negotiateClient } from "./negotiation.js";
import { WebSocketMessageQueue } from "./message-queue.js";
import { normalizeWebSocketOptions } from "./options.js";
import { attachRawSocket, connectRawSocket } from "./raw.js";
import { openWebSocket } from "./socket.js";
import type { ConnectionBindings, WebSocketConnectionOptions } from "./types.js";

export type { ConnectionBindings, WebSocketConnectionOptions } from "./types.js";

export async function connectWebSocket(
    url: string | URL,
    bindings: ConnectionBindings,
    options?: WebSocketConnectionOptions,
): Promise<Connection> {
    const normalized = normalizeWebSocketOptions(options);
    if (bindingInterfaceID(bindings.importBinding) === undefined || bindingInterfaceID(bindings.exportBinding) === undefined) {
        throw new InterfaceMismatchError("intercall: negotiated bindings require interface IDs");
    }
    const socket = await openWebSocket(url, normalized);
    const queue = new WebSocketMessageQueue(normalized.messageLimit);
    let events: { cleanup: () => void } | undefined;
    const setupController = new AbortController();
    const onExternalAbort = () => setupController.abort(normalized.signal?.reason);
    normalized.signal?.addEventListener("abort", onExternalAbort, { once: true });
    try {
        // During setup, retain message chunks in the negotiation queue rather
        // than handing them to the frame receiver. The residual bytes are
        // handed to the ordinary connection after the 32-byte ID.
        events = bindWebSocketEvents(socket, queue, (chunk) => queue.pushMessage(chunk.buffer), (cause) => {
            setupController.abort(cause);
        });
        const negotiationOptions = { ...normalized, signal: setupController.signal };
        await negotiateClient(socket, queue, bindings.importBinding, bindings.exportBinding, negotiationOptions);
        events.cleanup();
        events = undefined;
        normalized.signal?.removeEventListener("abort", onExternalAbort);
        return attachRawSocket(socket, bindings, normalized.messageLimit, queue);
    } catch (error) {
        normalized.signal?.removeEventListener("abort", onExternalAbort);
        events?.cleanup();
        queue.clear();
        try { socket.close(); } catch { /* preserve setup cause */ }
        throw error;
    }
}

export function connectRawWebSocket(
    url: string | URL,
    bindings: ConnectionBindings,
    options?: WebSocketConnectionOptions,
): Promise<Connection> {
    return connectRawSocket(url, bindings, options);
}
