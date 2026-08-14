import { EncoderBuffer } from "./encoder-buffer.js";
import {
    DecoderCursor,
    decodePrimitive,
    encodePrimitive,
    PrimitiveCodecError,
} from "./primitive-codec.js";

const encoder = new TextEncoder();
const decoder = new TextDecoder("utf-8", { fatal: true });

export function encodeString(buffer: EncoderBuffer, value: unknown): void {
    if (typeof value !== "string") throw new PrimitiveCodecError("string value is not a string");
    validateWellFormedString(value);
    const encoded = encoder.encode(value);
    encodePrimitive(buffer, "uint64", BigInt(encoded.byteLength));
    buffer.append(encoded);
}

export function decodeString(cursor: DecoderCursor): string {
    const length = decodePrimitive(cursor, "uint64");
    if (typeof length !== "bigint") throw new PrimitiveCodecError("decoded string length is not uint64");
    if (length > BigInt(cursor.remaining)) throw new PrimitiveCodecError("wire string length exceeds available bytes");
    const bytes = cursor.readBytes(Number(length));
    try {
        return decoder.decode(bytes);
    } catch (error) {
        throw new PrimitiveCodecError("string is not valid UTF-8", { cause: error });
    }
}

export function validateWellFormedString(value: string): void {
    for (let index = 0; index < value.length; index += 1) {
        const code = value.charCodeAt(index);
        if (code >= 0xd800 && code <= 0xdbff) {
            const next = value.charCodeAt(index + 1);
            if (!(next >= 0xdc00 && next <= 0xdfff)) throw new PrimitiveCodecError("string contains an unpaired high surrogate");
            index += 1;
        } else if (code >= 0xdc00 && code <= 0xdfff) {
            throw new PrimitiveCodecError("string contains an unpaired low surrogate");
        }
    }
}
