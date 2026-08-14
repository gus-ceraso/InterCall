import { TransportError } from "../runtime/errors.js";
import { WebSocketMessageQueue } from "./message-queue.js";
import type { WebSocketLike } from "./socket.js";

export interface SocketEventBinding {
    readonly cleanup: () => void;
}

export function bindWebSocketEvents(
    socket: WebSocketLike,
    queue: WebSocketMessageQueue,
    onChunk: (chunk: Uint8Array) => void,
    onTerminal: (cause: Error) => void,
): SocketEventBinding {
    let terminal = false;
    const cleanup = () => {
        socket.removeEventListener("message", onMessage);
        socket.removeEventListener("error", onError);
        socket.removeEventListener("close", onClose);
    };
    const terminate = (cause: Error) => {
        if (terminal) return;
        terminal = true;
        cleanup();
        onTerminal(cause);
    };
    const onMessage = (event: Event) => {
        try {
            queue.pushMessage((event as MessageEvent).data);
            const chunk = queue.read(queue.unreadBytes);
            if (chunk !== undefined) onChunk(chunk);
        } catch (error) {
            terminate(error instanceof Error ? error : new TransportError("intercall: WebSocket message failure", { cause: error }));
        }
    };
    const onError = () => terminate(new TransportError("intercall: WebSocket error"));
    const onClose = () => terminate(new TransportError("intercall: WebSocket closed"));
    socket.addEventListener("message", onMessage);
    socket.addEventListener("error", onError);
    socket.addEventListener("close", onClose);
    return { cleanup };
}
