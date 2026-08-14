import { ChunkQueue } from "../runtime/chunk-queue.js";
import { ProtocolError, ResourceLimitError } from "../runtime/errors.js";
import { DEFAULT_MESSAGE_LIMIT } from "./options.js";

export const MAX_QUEUED_UNREAD_BYTES = 134_217_808;

export class WebSocketMessageQueue {
    private readonly queue = new ChunkQueue();

    constructor(private readonly messageLimit = DEFAULT_MESSAGE_LIMIT) {
        if (!Number.isSafeInteger(messageLimit) || messageLimit <= 0 || messageLimit > DEFAULT_MESSAGE_LIMIT) {
            throw new RangeError("invalid WebSocket message limit");
        }
    }

    get unreadBytes(): number { return this.queue.unreadBytes; }

    pushMessage(data: unknown): void {
        const chunk = toBinaryChunk(data);
        if (chunk.byteLength > this.messageLimit) throw new ResourceLimitError("intercall: WebSocket message limit reached");
        if (chunk.byteLength > MAX_QUEUED_UNREAD_BYTES - this.queue.unreadBytes) {
            throw new ResourceLimitError("intercall: WebSocket receive queue limit reached");
        }
        this.queue.append(chunk);
    }

    read(length: number): Uint8Array | undefined { return this.queue.read(length); }
    clear(): void { this.queue.clear(); }
}

function toBinaryChunk(data: unknown): Uint8Array {
    if (typeof data === "string") throw new ProtocolError("intercall: text WebSocket message");
    if (data instanceof ArrayBuffer) return new Uint8Array(data).slice();
    throw new ProtocolError("intercall: unsupported WebSocket message value");
}
