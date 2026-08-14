import { bindingInterfaceID } from "../runtime/binding.js";
import { abortReason } from "../runtime/call-order.js";
import { InterfaceMismatchError, InvalidArgumentError, TransportError } from "../runtime/errors.js";
import type { ImportBinding, ExportBinding } from "../runtime/types.js";
import { WebSocketMessageQueue } from "./message-queue.js";
import type { WebSocketLike } from "./socket.js";
import type { NormalizedWebSocketOptions } from "./options.js";

export async function negotiateClient(
    socket: WebSocketLike,
    queue: WebSocketMessageQueue,
    importBinding: ImportBinding,
    exportBinding: ExportBinding,
    options: NormalizedWebSocketOptions,
): Promise<void> {
    const importID = bindingInterfaceID(importBinding);
    const exportID = bindingInterfaceID(exportBinding);
    if (importID === undefined || exportID === undefined) {
        throw new InvalidArgumentError("negotiated bindings require interface IDs");
    }
    try {
        socket.send(importID);
    } catch (error) {
        throw new TransportError("intercall: interface negotiation send failed", { cause: error });
    }
    const received = await readNegotiationID(queue, options);
    if (!constantTimeEqual(received, exportID)) {
        throw new InterfaceMismatchError("intercall: interface IDs do not match");
    }
}

async function readNegotiationID(
    queue: WebSocketMessageQueue,
    options: NormalizedWebSocketOptions,
): Promise<Uint8Array> {
    return new Promise<Uint8Array>((resolve, reject) => {
        let settled = false;
        let timer: ReturnType<typeof setTimeout> | undefined;
        const cleanup = () => {
            if (timer !== undefined) clearTimeout(timer);
            options.signal?.removeEventListener("abort", onAbort);
        };
        const finish = (error: Error | undefined, value?: Uint8Array) => {
            if (settled) return;
            settled = true;
            cleanup();
            if (error === undefined) resolve(value!);
            else reject(error);
        };
        const poll = () => {
            const value = queue.read(32);
            if (value !== undefined) finish(undefined, value);
            else timer = setTimeout(poll, 10);
        };
        const onAbort = () => finish(abortReason(options.signal!));
        timer = setTimeout(() => finish(new TransportError("intercall: interface negotiation timeout")), options.negotiationTimeoutMs);
        options.signal?.addEventListener("abort", onAbort, { once: true });
        if (options.signal?.aborted) onAbort();
        else poll();
    });
}

function constantTimeEqual(left: Uint8Array, right: Uint8Array): boolean {
    if (left.byteLength !== right.byteLength) return false;
    let difference = 0;
    for (let index = 0; index < left.byteLength; index += 1) difference |= left[index]! ^ right[index]!;
    return difference === 0;
}
