import { ConnectionCore, type PendingCall } from "./connection-core.js";
import {
    RequestIDsExhaustedError,
    ResourceLimitError,
} from "./errors.js";

export const MAX_OUTGOING_CALLS = 1_024;
export const MAX_REQUEST_ID = (1n << 63n) - 1n;

export class OutgoingRequestState {
    private activeValue = 0;
    private nextID: bigint;

    constructor(
        private readonly core: ConnectionCore,
        initialID = 0n,
    ) {
        if (initialID < 0n || initialID > MAX_REQUEST_ID) throw new RangeError("invalid initial request ID");
        this.nextID = initialID;
    }

    get active(): number {
        return this.activeValue;
    }

    reserve(): OutgoingCallSlot {
        if (this.core.isTerminal) throw this.core.terminal;
        if (this.activeValue >= MAX_OUTGOING_CALLS) throw new ResourceLimitError("intercall: outgoing call limit reached");
        this.activeValue += 1;
        return new OutgoingCallSlot(this);
    }

    claimResponse(id: bigint): PendingCall | undefined {
        return this.core.claimPending(id);
    }

    cancel(id: bigint, reason: Error): boolean {
        return this.core.retirePending(id, reason);
    }

    registerPending(pending: PendingCall): boolean {
        return this.core.registerPending(pending);
    }

    allocateID(): bigint {
        if (this.nextID > MAX_REQUEST_ID) throw new RequestIDsExhaustedError();
        const id = this.nextID;
        this.nextID += 1n;
        return id;
    }

    releaseSlot(): void {
        if (this.activeValue <= 0) throw new Error("outgoing call slot underflow");
        this.activeValue -= 1;
    }
}

export class OutgoingCallSlot {
    private released = false;
    private allocated = false;

    constructor(private readonly state: OutgoingRequestState) {}

    allocateID(): bigint {
        if (this.released) throw new Error("outgoing call slot is released");
        if (this.allocated) throw new Error("outgoing request ID already allocated");
        const id = this.state.allocateID();
        this.allocated = true;
        return id;
    }

    release(): void {
        if (this.released) return;
        this.released = true;
        this.state.releaseSlot();
    }

    get isReleased(): boolean {
        return this.released;
    }

    get hasID(): boolean {
        return this.allocated;
    }
}
