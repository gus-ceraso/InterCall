import { InvalidArgumentError } from "../runtime/errors.js";
import type { WebSocketConnectionOptions } from "./types.js";

export const DEFAULT_OPEN_TIMEOUT_MS = 10_000;
export const DEFAULT_NEGOTIATION_TIMEOUT_MS = 10_000;
export const DEFAULT_MESSAGE_LIMIT = 67_108_888;

export interface NormalizedWebSocketOptions {
    readonly signal?: AbortSignal;
    readonly protocols?: string | readonly string[];
    readonly openTimeoutMs: number;
    readonly negotiationTimeoutMs: number;
    readonly messageLimit: number;
}

export function normalizeWebSocketOptions(options?: WebSocketConnectionOptions): NormalizedWebSocketOptions {
    if (options !== undefined && (options === null || typeof options !== "object")) {
        throw new InvalidArgumentError("WebSocket options must be an object");
    }
    const value = options ?? {};
    validateSignal(value.signal);
    const protocols = validateProtocols(value.protocols);
    const openTimeoutMs = positiveInteger(value.openTimeoutMs ?? DEFAULT_OPEN_TIMEOUT_MS, "open timeout");
    const negotiationTimeoutMs = positiveInteger(value.negotiationTimeoutMs ?? DEFAULT_NEGOTIATION_TIMEOUT_MS, "negotiation timeout");
    const messageLimit = positiveInteger(value.messageLimit ?? DEFAULT_MESSAGE_LIMIT, "message limit");
    if (messageLimit > DEFAULT_MESSAGE_LIMIT) throw new InvalidArgumentError("message limit exceeds the default maximum");
    return {
        ...(value.signal === undefined ? {} : { signal: value.signal }),
        ...(protocols === undefined ? {} : { protocols }),
        openTimeoutMs,
        negotiationTimeoutMs,
        messageLimit,
    };
}

function validateSignal(signal: AbortSignal | undefined): void {
    if (signal !== undefined && (signal === null || typeof signal !== "object" || typeof signal.addEventListener !== "function")) {
        throw new InvalidArgumentError("signal must be an AbortSignal");
    }
}

function validateProtocols(protocols: string | readonly string[] | undefined): string | readonly string[] | undefined {
    if (protocols === undefined || typeof protocols === "string") return protocols;
    if (!Array.isArray(protocols) || protocols.some((protocol) => typeof protocol !== "string")) {
        throw new InvalidArgumentError("protocols must be a string or string array");
    }
    return protocols.slice();
}

function positiveInteger(value: number, label: string): number {
    if (!Number.isSafeInteger(value) || value <= 0) throw new InvalidArgumentError(`${label} must be a positive integer`);
    return value;
}
