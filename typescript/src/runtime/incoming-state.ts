import { ConnectionCore, type HandlerLease } from "./connection-core.js";
import { ProtocolError, ResourceLimitError } from "./errors.js";

export const MAX_ACTIVE_INCOMING_HANDLERS = 256;
export const MAX_ACTIVE_INCOMING_PAYLOAD_BYTES = 134_217_728;

export interface IncomingLease {
    readonly signal: AbortSignal;
    readonly finish: () => void;
}

export class IncomingRequestState {
    private readonly activeIDs = new Set<bigint>();
    private activeBytesValue = 0;
    private activeHandlersValue = 0;

    constructor(private readonly core: ConnectionCore) {}

    get activeHandlers(): number { return this.activeHandlersValue; }
    get activeBytes(): number { return this.activeBytesValue; }

    admit(id: bigint, payloadLength: number): IncomingLease {
        if (this.core.isTerminal) throw this.core.terminal;
        if (!Number.isSafeInteger(payloadLength) || payloadLength < 0) throw new ProtocolError("invalid incoming payload length");
        if (this.activeIDs.has(id)) throw new ProtocolError(`duplicate active incoming request ID ${id}`);
        if (this.activeHandlersValue >= MAX_ACTIVE_INCOMING_HANDLERS) {
            throw new ResourceLimitError("intercall: active incoming handler limit reached");
        }
        if (payloadLength > MAX_ACTIVE_INCOMING_PAYLOAD_BYTES - this.activeBytesValue) {
            throw new ResourceLimitError("intercall: active incoming payload limit reached");
        }
        const handler = this.core.registerHandler();
        if (handler === undefined) throw this.core.terminal;
        this.activeIDs.add(id);
        this.activeHandlersValue += 1;
        this.activeBytesValue += payloadLength;
        let finished = false;
        return {
            signal: handler.signal,
            finish: () => {
                if (finished) return;
                finished = true;
                this.activeIDs.delete(id);
                this.activeHandlersValue -= 1;
                this.activeBytesValue -= payloadLength;
                handler.finish();
            },
        };
    }
}
