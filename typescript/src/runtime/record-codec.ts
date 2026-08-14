import { EncoderBuffer } from "./encoder-buffer.js";
import { DecoderCursor, PrimitiveCodecError } from "./primitive-codec.js";

export interface RecordCodecField<T = unknown> {
    readonly name: string;
    readonly encode: (buffer: EncoderBuffer, value: T) => void;
    readonly decode: (cursor: DecoderCursor) => T;
}

export function encodeRecord(
    buffer: EncoderBuffer,
    value: unknown,
    fields: readonly RecordCodecField[],
): void {
    if (value === null || typeof value !== "object" || Array.isArray(value)) {
        throw new PrimitiveCodecError("record value is not an object");
    }
    const object = value as Record<string, unknown>;
    const expected = new Set<string>();
    for (const field of fields) {
        if (expected.has(field.name)) throw new PrimitiveCodecError(`duplicate record field ${field.name}`);
        expected.add(field.name);
        if (!Object.prototype.hasOwnProperty.call(object, field.name)) {
            throw new PrimitiveCodecError(`record is missing field ${JSON.stringify(field.name)}`);
        }
    }
    for (const key of Reflect.ownKeys(object)) {
        if (typeof key !== "string" || !expected.has(key)) {
            throw new PrimitiveCodecError(`record has an unknown field ${String(key)}`);
        }
    }
    for (const field of fields) field.encode(buffer, object[field.name]);
}

export function decodeRecord(
    cursor: DecoderCursor,
    fields: readonly RecordCodecField[],
): Record<string, unknown> {
    const result: Record<string, unknown> = {};
    const seen = new Set<string>();
    for (const field of fields) {
        if (seen.has(field.name)) throw new PrimitiveCodecError(`duplicate record field ${field.name}`);
        seen.add(field.name);
        Object.defineProperty(result, field.name, {
            configurable: true,
            enumerable: true,
            value: field.decode(cursor),
            writable: true,
        });
    }
    return result;
}

export function decodeExact<T>(
    bytes: Uint8Array,
    decode: (cursor: DecoderCursor) => T,
): T {
    const cursor = new DecoderCursor(bytes);
    const value = decode(cursor);
    if (cursor.remaining !== 0) throw new PrimitiveCodecError("trailing bytes after payload");
    return value;
}
