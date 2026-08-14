import type { Connection } from "./types.js";
import {
    ConnectionClosedError,
    InterCallAbortError,
} from "./errors.js";

export interface PendingCall {
    readonly id: bigint;
    readonly reject: (cause: Error) => void;
    readonly release: () => void;
}

export interface HandlerLease {
    readonly signal: AbortSignal;
    readonly finish: () => void;
}

export class ConnectionCore {
    private readonly pending = new Map<bigint, PendingCall>();
    private readonly handlers = new Set<AbortController>();
    private readonly closedPromise: Promise<Error>;
    private resolveClosed!: (cause: Error) => void;
    private terminalCause: Error | undefined;
    private receiveStopped = false;
    private cleanupDone = false;
    private closedResolved = false;

    constructor(private readonly cleanupOwner?: () => void) {
        this.closedPromise = new Promise<Error>((resolve) => { this.resolveClosed = resolve; });
    }

    get closed(): Promise<Error> {
        return this.closedPromise;
    }

    get terminal(): Error | undefined {
        return this.terminalCause;
    }

    get isTerminal(): boolean {
        return this.terminalCause !== undefined;
    }

    asConnection(): Connection {
        return this as unknown as Connection;
    }

    close(): void {
        this.terminate(new ConnectionClosedError());
    }

    terminate(reason: unknown): boolean {
        if (this.terminalCause !== undefined) return false;
        const cause = normalizeConnectionCause(reason);
        this.terminalCause = cause;
        for (const pending of this.pending.values()) {
            try { pending.reject(cause); } catch { /* promise rejection callbacks are not lifecycle owners */ }
            try { pending.release(); } catch { /* release is best-effort during terminal teardown */ }
        }
        this.pending.clear();
        for (const controller of this.handlers) controller.abort(cause);
        this.handlers.clear();
        this.runCleanup();
        this.maybeResolveClosed();
        return true;
    }

    registerPending(pending: PendingCall): boolean {
        if (this.terminalCause !== undefined) {
            pending.reject(this.terminalCause);
            pending.release();
            return false;
        }
        if (this.pending.has(pending.id)) throw new Error(`duplicate pending request ID ${pending.id}`);
        this.pending.set(pending.id, pending);
        return true;
    }

    claimPending(id: bigint): PendingCall | undefined {
        const pending = this.pending.get(id);
        if (pending === undefined) return undefined;
        this.pending.delete(id);
        pending.release();
        return pending;
    }

    retirePending(id: bigint, reason: Error): boolean {
        const pending = this.pending.get(id);
        if (pending === undefined) return false;
        this.pending.delete(id);
        try { pending.reject(reason); } finally { pending.release(); }
        return true;
    }

    registerHandler(): HandlerLease | undefined {
        if (this.terminalCause !== undefined) return undefined;
        const controller = new AbortController();
        this.handlers.add(controller);
        let finished = false;
        return {
            signal: controller.signal,
            finish: () => {
                if (finished) return;
                finished = true;
                this.handlers.delete(controller);
            },
        };
    }

    markReceiveStopped(): void {
        this.receiveStopped = true;
        this.maybeResolveClosed();
    }

    private runCleanup(): void {
        if (this.cleanupDone) return;
        this.cleanupDone = true;
        try { this.cleanupOwner?.(); } catch { /* terminal state must remain observable */ }
    }

    private maybeResolveClosed(): void {
        if (!this.receiveStopped || this.terminalCause === undefined || this.closedResolved) return;
        this.closedResolved = true;
        this.resolveClosed(this.terminalCause);
    }
}

function normalizeConnectionCause(reason: unknown): Error {
    if (reason instanceof Error) return reason;
    return new InterCallAbortError(reason);
}
