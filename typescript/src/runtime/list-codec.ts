import { EncoderBuffer } from "./encoder-buffer.js";
import {
    DecoderCursor,
    decodePrimitive,
    encodePrimitive,
    PrimitiveCodecError,
} from "./primitive-codec.js";

export const MAX_LIST_ELEMENTS = 1_048_576;

export function encodeList<T>(
    buffer: EncoderBuffer,
    value: unknown,
    encodeElement: (buffer: EncoderBuffer, value: T) => void,
): void {
    if (!Array.isArray(value)) throw new PrimitiveCodecError("list value is not a JavaScript array");
    if (value.length > MAX_LIST_ELEMENTS) throw new PrimitiveCodecError("list exceeds the element limit");
    encodePrimitive(buffer, "uint64", BigInt(value.length));
    for (const element of value) encodeElement(buffer, element as T);
}

export function decodeList<T>(
    cursor: DecoderCursor,
    decodeElement: (cursor: DecoderCursor) => T,
): T[] {
    const count = decodePrimitive(cursor, "uint64");
    if (typeof count !== "bigint" || count > BigInt(MAX_LIST_ELEMENTS)) {
        throw new PrimitiveCodecError("wire list count exceeds the element limit");
    }
    const result = new Array<T>(Number(count));
    for (let index = 0; index < result.length; index += 1) result[index] = decodeElement(cursor);
    return result;
}
