import type {
    Connection,
    ExportBinding,
    ImportBinding,
} from "../runtime/types.js";

export interface ConnectionBindings {
    readonly exportBinding: ExportBinding;
    readonly importBinding: ImportBinding;
}

export interface WebSocketConnectionOptions {
    readonly signal?: AbortSignal;
    readonly protocols?: string | readonly string[];
    readonly openTimeoutMs?: number;
    readonly negotiationTimeoutMs?: number;
    readonly messageLimit?: number;
}

export type BrowserConnection = Connection;
