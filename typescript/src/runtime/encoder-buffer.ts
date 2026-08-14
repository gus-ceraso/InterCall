export const MAX_ENCODER_BYTES = 64 * 1024 * 1024;

export class CodecBufferError extends Error {
    constructor(message: string, options?: { readonly cause?: unknown }) {
        super(message, options);
        this.name = "CodecBufferError";
    }
}

export class EncoderBuffer {
    private buffer: Uint8Array;
    private used = 0;

    constructor(
        private readonly maximum = MAX_ENCODER_BYTES,
        initialCapacity = 256,
    ) {
        if (!Number.isSafeInteger(maximum) || maximum < 0) throw new RangeError("invalid encoder maximum");
        if (!Number.isSafeInteger(initialCapacity) || initialCapacity < 0) throw new RangeError("invalid encoder capacity");
        this.buffer = new Uint8Array(Math.min(maximum, initialCapacity));
    }

    get length(): number {
        return this.used;
    }

    appendByte(value: number): void {
        if (!Number.isInteger(value) || value < 0 || value > 0xff) {
            throw new CodecBufferError(`invalid byte ${value}`);
        }
        this.ensure(1);
        this.buffer[this.used] = value;
        this.used += 1;
    }

    append(bytes: Uint8Array): void {
        this.ensure(bytes.byteLength);
        this.buffer.set(bytes, this.used);
        this.used += bytes.byteLength;
    }

    finish(): Uint8Array {
        return this.buffer.slice(0, this.used);
    }

    private ensure(additional: number): void {
        if (!Number.isSafeInteger(additional) || additional < 0) {
            throw new CodecBufferError(`invalid append length ${additional}`);
        }
        const required = this.used + additional;
        if (!Number.isSafeInteger(required) || required > this.maximum) {
            throw new CodecBufferError(`encoded value exceeds ${this.maximum} bytes`);
        }
        if (required <= this.buffer.byteLength) return;

        let capacity = Math.max(1, this.buffer.byteLength);
        while (capacity < required) {
            const next = Math.min(this.maximum, capacity * 2);
            if (next <= capacity) {
                capacity = required;
                break;
            }
            capacity = next;
        }
        try {
            const replacement = new Uint8Array(capacity);
            replacement.set(this.buffer.subarray(0, this.used));
            this.buffer = replacement;
        } catch (error) {
            throw new CodecBufferError("unable to allocate encoder buffer", { cause: error });
        }
    }
}
