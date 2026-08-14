import { EncoderBuffer } from "./encoder-buffer.js";
import {
    DecoderCursor,
    decodePrimitive,
    encodePrimitive,
    PrimitiveCodecError,
} from "./primitive-codec.js";

export function encodeBytes(buffer: EncoderBuffer, value: unknown): void {
    if (!(value instanceof Uint8Array)) throw new PrimitiveCodecError("bytes value is not a Uint8Array");
    encodePrimitive(buffer, "uint64", BigInt(value.byteLength));
    buffer.append(value);
}

export function decodeBytes(cursor: DecoderCursor): Uint8Array {
    const length = decodePrimitive(cursor, "uint64");
    if (typeof length !== "bigint") throw new PrimitiveCodecError("decoded bytes length is not uint64");
    if (length > BigInt(cursor.remaining)) throw new PrimitiveCodecError("wire bytes length exceeds available bytes");
    return cursor.readBytes(Number(length)).slice();
}
