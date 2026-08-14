import { ChunkQueue } from "./chunk-queue.js";
import { ProtocolError } from "./errors.js";

export const FRAME_HEADER_SIZE = 24;
export const MAX_FRAME_PAYLOAD = 64 * 1024 * 1024;
export const RESPONSE_BIT = 1n << 63n;
export const REQUEST_ID_MASK = (1n << 63n) - 1n;

export type FrameKind = "request" | "response";

export interface FrameHeader {
    readonly kind: FrameKind;
    readonly requestID: bigint;
    readonly key: bigint;
    readonly payloadLength: bigint;
}

export interface Frame {
    readonly header: FrameHeader;
    readonly payload: Uint8Array;
}

export function buildFrame(
    kind: FrameKind,
    requestID: bigint,
    key: bigint,
    payload: Uint8Array,
): Uint8Array {
    if (requestID < 0n || requestID > REQUEST_ID_MASK) throw new ProtocolError("request ID is outside the 63-bit range");
    if (!(payload instanceof Uint8Array) || payload.byteLength > MAX_FRAME_PAYLOAD) {
        throw new ProtocolError("frame payload exceeds the accepted ceiling");
    }
    const frame = new Uint8Array(FRAME_HEADER_SIZE + payload.byteLength);
    const view = new DataView(frame.buffer);
    view.setBigUint64(0, kind === "response" ? requestID | RESPONSE_BIT : requestID, true);
    view.setBigUint64(8, key, true);
    view.setBigUint64(16, BigInt(payload.byteLength), true);
    frame.set(payload, FRAME_HEADER_SIZE);
    return frame;
}

export function parseFrameHeader(bytes: Uint8Array): FrameHeader {
    if (bytes.byteLength !== FRAME_HEADER_SIZE) throw new ProtocolError("intercall: frame header must be exactly 24 bytes");
    const view = new DataView(bytes.buffer, bytes.byteOffset, bytes.byteLength);
    const rawID = view.getBigUint64(0, true);
    const key = view.getBigUint64(8, true);
    const length = view.getBigUint64(16, true);
    if (length > BigInt(MAX_FRAME_PAYLOAD)) {
        throw new ProtocolError(`intercall: frame payload length ${length} exceeds ${MAX_FRAME_PAYLOAD} bytes`);
    }
    return {
        kind: (rawID & RESPONSE_BIT) === 0n ? "request" : "response",
        requestID: rawID & REQUEST_ID_MASK,
        key,
        payloadLength: length,
    };
}

export class FrameReceiver {
    private readonly queue = new ChunkQueue();
    private header: FrameHeader | undefined;

    get unreadBytes(): number {
        return this.queue.unreadBytes;
    }

    push(chunk: Uint8Array): void {
        this.queue.append(chunk);
    }

    next(): Frame | undefined {
        if (this.header === undefined) {
            if (this.queue.unreadBytes < FRAME_HEADER_SIZE) return undefined;
            this.header = parseFrameHeader(this.queue.read(FRAME_HEADER_SIZE)!);
        }
        const payloadLength = Number(this.header.payloadLength);
        if (this.queue.unreadBytes < payloadLength) return undefined;
        const header = this.header;
        const payload = this.queue.read(payloadLength)!;
        this.header = undefined;
        return { header, payload };
    }

    clear(): void {
        this.queue.clear();
        this.header = undefined;
    }
}
