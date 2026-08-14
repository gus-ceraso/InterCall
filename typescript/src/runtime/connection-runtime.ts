import type { ResponseDecoder } from "../generated-spi/index.js";
import { assertImportBinding, assertExportBinding, bindingDispatch } from "./binding.js";
import { runOrderedCall, type OrderedCallDriver } from "./call-order.js";
import { ConnectionCore } from "./connection-core.js";
import { invokeDispatch } from "./dispatch-result.js";
import { buildFrame, FRAME_HEADER_SIZE, FrameReceiver } from "./frame.js";
import { BindingMismatchError, ProtocolError, ResourceLimitError, TransportError } from "./errors.js";
import { IncomingRequestState } from "./incoming-state.js";
import { OutgoingRequestState } from "./request-state.js";
import { SendGate } from "./send-gate.js";
import type { CallOptions, Connection, ExportBinding, ImportBinding, HandlerContext } from "./types.js";

export const MAX_OWNED_FRAME_BYTES = 134_217_776;
const runtimeConnections = new WeakMap<object, ConnectionRuntime>();

export interface RuntimeTransport {
    readonly bufferedAmount: number | (() => number);
    readonly isOpen: boolean | (() => boolean);
    send(frame: Uint8Array): void;
    close(): void;
}

export class ConnectionRuntime {
    readonly core: ConnectionCore;
    private readonly receiver = new FrameReceiver();
    private readonly outgoing: OutgoingRequestState;
    private readonly incoming: IncomingRequestState;
    private ownedFrameBytes = 0;
    private readonly sendGate = new SendGate();

    constructor(
        private readonly transport: RuntimeTransport,
        private readonly importBinding: ImportBinding,
        private readonly exportBinding: ExportBinding,
    ) {
        assertImportBinding(importBinding);
        assertExportBinding(exportBinding);
        this.core = new ConnectionCore(() => transport.close());
        this.outgoing = new OutgoingRequestState(this.core);
        this.incoming = new IncomingRequestState(this.core);
        runtimeConnections.set(this.connection as unknown as object, this);
    }

    get connection(): Connection {
        return this.core.asConnection();
    }

    get closed(): Promise<Error> { return this.core.closed; }

    close(): void { this.core.close(); }

    receiveChunk(chunk: Uint8Array): void {
        if (this.core.isTerminal) return;
        try {
            this.receiver.push(chunk);
            let frame;
            while ((frame = this.receiver.next()) !== undefined) this.receiveFrame(frame);
        } catch (error) {
            this.core.terminate(error instanceof ProtocolError ? error : new ProtocolError("intercall: malformed frame", { cause: error }));
            this.core.markReceiveStopped();
        }
    }

    transportClosed(reason: unknown): void {
        this.core.terminate(reason instanceof Error ? reason : new TransportError("intercall: transport closed", { cause: reason }));
        this.core.markReceiveStopped();
    }

    call(binding: ImportBinding, procedureKey: bigint, encode: () => Uint8Array, decode: ResponseDecoder, options?: CallOptions): Promise<void> {
        const driver: OrderedCallDriver<void> = {
            validate: () => {
                assertImportBinding(binding);
                if (binding !== this.importBinding) throw new BindingMismatchError();
                if (typeof procedureKey !== "bigint" || procedureKey === 0n) throw new TypeError("invalid procedure key");
                if (typeof encode !== "function" || typeof decode !== "function") throw new TypeError("invalid call codec");
            },
            ready: () => this.isTransportOpen() && !this.core.isTerminal,
            reserveCall: () => this.outgoing.reserve(),
            encode,
            reserveFrameBytes: (length) => this.reserveFrameBytes(length + FRAME_HEADER_SIZE),
            waitForSend: async (signal, frameLength = 0) => {
                if (!this.isTransportOpen()) throw new TransportError();
                const gateRequest = signal === undefined
                    ? {
                        frameLength: frameLength + FRAME_HEADER_SIZE,
                        bufferedAmount: () => this.transportBufferedAmount(),
                        terminalCause: () => this.core.terminal,
                    }
                    : {
                        frameLength: frameLength + FRAME_HEADER_SIZE,
                        bufferedAmount: () => this.transportBufferedAmount(),
                        signal,
                        terminalCause: () => this.core.terminal,
                    };
                sendLease = await this.sendGate.acquire(gateRequest);
            },
            releaseSend: () => {
                sendLease?.();
                sendLease = undefined;
            },
            allocateID: (slot) => slot.allocateID(),
            registerPending: (id, slot) => new Promise<void>((resolve, reject) => {
                this.core.registerPending({ id, reject, release: () => slot.release() });
                this.pendingResolvers.set(id, { resolve, reject, decode });
            }),
            send: (id, payload) => {
                try { this.transport.send(buildFrame("request", id, procedureKey, payload)); }
                finally { sendLease?.(); sendLease = undefined; }
            },
            cancel: (id, reason) => this.outgoing.cancel(id, reason),
            fail: (cause) => this.core.terminate(cause),
        };
        let sendLease: (() => void) | undefined;
        return runOrderedCall(driver, options?.signal);
    }

    private readonly pendingResolvers = new Map<bigint, { readonly resolve: () => void; readonly reject: (error: Error) => void; readonly decode: ResponseDecoder }>();

    private receiveFrame(frame: { readonly header: { readonly kind: "request" | "response"; readonly requestID: bigint; readonly key: bigint }; readonly payload: Uint8Array }): void {
        if (frame.header.kind === "response") {
            const pending = this.outgoing.claimResponse(frame.header.requestID);
            if (pending === undefined) return;
            const resolver = this.pendingResolvers.get(frame.header.requestID);
            this.pendingResolvers.delete(frame.header.requestID);
            if (resolver === undefined) return;
            try {
                resolver.decode(frame.header.key, frame.payload);
                resolver.resolve();
            } catch (error) {
                resolver.reject(error instanceof Error ? error : new ProtocolError("intercall: response decode failed", { cause: error }));
                this.core.terminate(new ProtocolError("intercall: malformed matched response", { cause: error }));
            }
            return;
        }
        const lease = this.incoming.admit(frame.header.requestID, frame.payload.byteLength);
        const context: HandlerContext = { connection: this.connection, signal: lease.signal };
        void invokeDispatch(bindingDispatch(this.exportBinding), context, frame.header.key, frame.payload).then((result) => {
            if (!this.core.sendIfActive(() => this.transport.send(buildFrame("response", frame.header.requestID, result.exceptionKey, result.payload)))) return;
        }).catch(() => undefined).finally(() => lease.finish());
    }

    private transportBufferedAmount(): number {
        return typeof this.transport.bufferedAmount === "function" ? this.transport.bufferedAmount() : this.transport.bufferedAmount;
    }

    private reserveFrameBytes(length: number): () => void {
        if (this.ownedFrameBytes > MAX_OWNED_FRAME_BYTES - length) throw new ResourceLimitError("intercall: owned frame-byte limit reached");
        this.ownedFrameBytes += length;
        let released = false;
        return () => {
            if (released) return;
            released = true;
            this.ownedFrameBytes -= length;
        };
    }

    private isTransportOpen(): boolean {
        return typeof this.transport.isOpen === "function" ? this.transport.isOpen() : this.transport.isOpen;
    }
}

export function connectionRuntimeFor(value: Connection): ConnectionRuntime {
    const runtime = runtimeConnections.get(value as unknown as object);
    if (runtime === undefined) throw new TypeError("invalid InterCall connection");
    return runtime;
}
