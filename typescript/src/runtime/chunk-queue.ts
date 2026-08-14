export class ChunkQueue {
    private readonly chunks: Uint8Array[] = [];
    private headOffset = 0;
    private unreadValue = 0;

    get unreadBytes(): number {
        return this.unreadValue;
    }

    append(chunk: Uint8Array): void {
        if (!(chunk instanceof Uint8Array)) throw new TypeError("chunk must be a Uint8Array");
        if (chunk.byteLength === 0) return;
        this.chunks.push(chunk.slice());
        this.unreadValue += chunk.byteLength;
    }

    read(length: number): Uint8Array | undefined {
        validateLength(length);
        if (length > this.unreadValue) return undefined;
        const result = new Uint8Array(length);
        let written = 0;
        while (written < length) {
            const chunk = this.chunks[0]!;
            const available = chunk.byteLength - this.headOffset;
            const copied = Math.min(available, length - written);
            result.set(chunk.subarray(this.headOffset, this.headOffset + copied), written);
            written += copied;
            this.headOffset += copied;
            this.unreadValue -= copied;
            if (this.headOffset === chunk.byteLength) {
                this.chunks.shift();
                this.headOffset = 0;
            }
        }
        return result;
    }

    clear(): void {
        this.chunks.length = 0;
        this.headOffset = 0;
        this.unreadValue = 0;
    }
}

function validateLength(length: number): void {
    if (!Number.isSafeInteger(length) || length < 0) throw new RangeError(`invalid chunk length ${length}`);
}
