import { abortReason } from "./call-order.js";
import { InvalidArgumentError, ResourceLimitError } from "./errors.js";

export const MAX_NATIVE_QUEUED_BYTES = 134_217_776;
export const SEND_POLL_INTERVAL_MS = 10;

export interface SendGateScheduler {
    setTimeout(callback: () => void, delayMs: number): unknown;
    clearTimeout(handle: unknown): void;
}

export interface SendGateRequest {
    readonly frameLength: number;
    readonly bufferedAmount: () => number;
    readonly signal?: AbortSignal;
    readonly terminalCause: () => Error | undefined;
    readonly send: () => void;
}

const defaultScheduler: SendGateScheduler = {
    setTimeout: (callback, delayMs) => globalThis.setTimeout(callback, delayMs),
    clearTimeout: (handle) => globalThis.clearTimeout(handle as ReturnType<typeof setTimeout>),
};

export class SendGate {
    private tail: Promise<void> = Promise.resolve();

    constructor(private readonly scheduler: SendGateScheduler = defaultScheduler) {}

    enqueue(request: SendGateRequest): Promise<void> {
        validateFrameLength(request.frameLength);
        const previous = this.tail;
        let release!: () => void;
        this.tail = new Promise<void>((resolve) => { release = resolve; });
        return (async () => {
            await previous;
            try {
                await this.waitForCapacity(request);
                request.send();
            } finally {
                release();
            }
        })();
    }

    private async waitForCapacity(request: SendGateRequest): Promise<void> {
        while (true) {
            const terminal = request.terminalCause();
            if (terminal !== undefined) throw terminal;
            if (request.signal?.aborted) throw abortReason(request.signal);
            const buffered = request.bufferedAmount();
            if (!Number.isSafeInteger(buffered) || buffered < 0) {
                throw new InvalidArgumentError("WebSocket bufferedAmount is invalid");
            }
            if (buffered + request.frameLength <= MAX_NATIVE_QUEUED_BYTES) return;
            await this.waitForPoll(request.signal);
        }
    }

    private waitForPoll(signal: AbortSignal | undefined): Promise<void> {
        return new Promise<void>((resolve, reject) => {
            let settled = false;
            let timer: unknown;
            const finish = (error?: unknown) => {
                if (settled) return;
                settled = true;
                this.scheduler.clearTimeout(timer);
                signal?.removeEventListener("abort", onAbort);
                if (error === undefined) resolve();
                else reject(error);
            };
            const onAbort = () => finish(abortReason(signal!));
            timer = this.scheduler.setTimeout(() => finish(), SEND_POLL_INTERVAL_MS);
            if (signal?.aborted) onAbort();
            else signal?.addEventListener("abort", onAbort, { once: true });
        });
    }
}

function validateFrameLength(length: number): void {
    if (!Number.isSafeInteger(length) || length < 0) throw new InvalidArgumentError("frame length is invalid");
    if (length > MAX_NATIVE_QUEUED_BYTES) throw new ResourceLimitError("frame exceeds native send-buffer capacity");
}
