import type { OutgoingCallSlot } from "./request-state.js";
import { InvalidArgumentError } from "./errors.js";

export interface OrderedCallDriver<Result> {
    validate(): void;
    ready(): boolean;
    reserveCall(): OutgoingCallSlot;
    encode(): Uint8Array;
    reserveFrameBytes(length: number): () => void;
    waitForSend(signal: AbortSignal | undefined, frameLength?: number): Promise<void>;
    releaseSend(): void;
    allocateID(slot: OutgoingCallSlot): bigint;
    registerPending(id: bigint, slot: OutgoingCallSlot): Promise<Result>;
    send(id: bigint, payload: Uint8Array): void;
    cancel(id: bigint, reason: Error): void;
    fail(cause: unknown): void;
    terminalCause?(): Error | undefined;
}

export async function runOrderedCall<Result>(
    driver: OrderedCallDriver<Result>,
    signal?: AbortSignal,
): Promise<Result> {
    driver.validate();
    throwIfAborted(signal);
    if (!driver.ready()) throw driver.terminalCause?.() ?? new Error("intercall: connection is not ready");

    const slot = driver.reserveCall();
    let releaseFrame: (() => void) | undefined;
    let requestID: bigint | undefined;
    let pending: Promise<Result> | undefined;
    let cancelled = false;
    try {
        throwIfAborted(signal);
        const payload = driver.encode();
        if (!(payload instanceof Uint8Array)) throw new InvalidArgumentError("request encoder did not return Uint8Array");
        if (!driver.ready()) throw driver.terminalCause?.() ?? new Error("intercall: connection closed during encoding");
        releaseFrame = driver.reserveFrameBytes(payload.byteLength);
        await driver.waitForSend(signal, payload.byteLength);
        if (!driver.ready()) throw driver.terminalCause?.() ?? new Error("intercall: connection closed before send");
        requestID = driver.allocateID(slot);
        pending = driver.registerPending(requestID, slot);
        void pending.catch(() => undefined);
        driver.send(requestID, payload);
        return await awaitOutcome(pending, signal, requestID, driver, () => { cancelled = true; });
    } catch (error) {
        if (requestID !== undefined && pending !== undefined && !cancelled) {
            driver.cancel(requestID, asError(error));
        } else {
            slot.release();
        }
        throw error;
    } finally {
        driver.releaseSend();
        releaseFrame?.();
    }
}

async function awaitOutcome<Result>(
    outcome: Promise<Result>,
    signal: AbortSignal | undefined,
    requestID: bigint,
    driver: OrderedCallDriver<Result>,
    markCancelled: () => void,
): Promise<Result> {
    if (signal === undefined) return outcome;
    const abort = new Promise<never>((_, reject) => {
        const onAbort = () => {
            signal.removeEventListener("abort", onAbort);
            const reason = abortReason(signal);
            markCancelled();
            driver.cancel(requestID, reason);
            reject(reason);
        };
        if (signal.aborted) onAbort();
        else signal.addEventListener("abort", onAbort, { once: true });
        void outcome.then(
            () => signal.removeEventListener("abort", onAbort),
            () => signal.removeEventListener("abort", onAbort),
        );
    });
    return Promise.race([outcome, abort]);
}

function throwIfAborted(signal: AbortSignal | undefined): void {
    if (signal?.aborted) throw abortReason(signal);
}

export function abortReason(signal: AbortSignal): Error {
    if (signal.reason !== undefined) return signal.reason;
    return new DOMException("The operation was aborted", "AbortError");
}

function asError(value: unknown): Error {
    return value instanceof Error ? value : new Error(String(value));
}
