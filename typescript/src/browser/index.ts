import type { Connection } from "../runtime/types.js";
import type {
    ConnectionBindings,
    WebSocketConnectionOptions,
} from "./types.js";

export type { ConnectionBindings, WebSocketConnectionOptions } from "./types.js";

export function connectWebSocket(
    _url: string | URL,
    _bindings: ConnectionBindings,
    _options?: WebSocketConnectionOptions,
): Promise<Connection> {
    return Promise.reject(new Error("InterCall browser runtime is not implemented"));
}

export function connectRawWebSocket(
    _url: string | URL,
    _bindings: ConnectionBindings,
    _options?: WebSocketConnectionOptions,
): Promise<Connection> {
    return Promise.reject(new Error("InterCall browser runtime is not implemented"));
}
