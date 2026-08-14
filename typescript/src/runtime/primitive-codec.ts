import { EncoderBuffer } from "./encoder-buffer.js";

export type NumericPrimitive =
    | "int8" | "int16" | "int32" | "int64"
    | "uint8" | "uint16" | "uint32" | "uint64"
    | "float32" | "float64";

export type Primitive = NumericPrimitive | "string" | "bytes";
export type NumericValue = number | bigint;

export const CANONICAL_FLOAT32_NAN = 0x7fc00000;
export const CANONICAL_FLOAT64_NAN = 0x7ff8000000000000n;

export class PrimitiveCodecError extends Error {
    constructor(message: string, options?: { readonly cause?: unknown }) {
        super(message, options);
        this.name = "PrimitiveCodecError";
    }
}

export class DecoderCursor {
    private offsetValue = 0;

    constructor(readonly bytes: Uint8Array) {}

    get offset(): number {
        return this.offsetValue;
    }

    get remaining(): number {
        return this.bytes.byteLength - this.offsetValue;
    }

    readByte(): number {
        this.require(1);
        return this.bytes[this.offsetValue++]!;
    }

    readBytes(length: number): Uint8Array {
        if (!Number.isSafeInteger(length) || length < 0) throw new PrimitiveCodecError(`invalid read length ${length}`);
        this.require(length);
        const result = this.bytes.subarray(this.offsetValue, this.offsetValue + length);
        this.offsetValue += length;
        return result;
    }

    private require(length: number): void {
        if (this.offsetValue + length > this.bytes.byteLength) {
            throw new PrimitiveCodecError("truncated wire value");
        }
    }
}

export function encodePrimitive(buffer: EncoderBuffer, primitive: Primitive, value: unknown): void {
    if (primitive === "string" || primitive === "bytes") {
        throw new PrimitiveCodecError(`${primitive} requires its dedicated codec`);
    }
    const scratch = new Uint8Array(8);
    const view = new DataView(scratch.buffer);
    switch (primitive) {
        case "int8":
            view.setInt8(0, checkedNumber(value, primitive, -0x80, 0x7f));
            buffer.append(scratch.subarray(0, 1));
            return;
        case "uint8":
            view.setUint8(0, checkedNumber(value, primitive, 0, 0xff));
            buffer.append(scratch.subarray(0, 1));
            return;
        case "int16":
            view.setInt16(0, checkedNumber(value, primitive, -0x8000, 0x7fff), true);
            buffer.append(scratch.subarray(0, 2));
            return;
        case "uint16":
            view.setUint16(0, checkedNumber(value, primitive, 0, 0xffff), true);
            buffer.append(scratch.subarray(0, 2));
            return;
        case "int32":
            view.setInt32(0, checkedNumber(value, primitive, -0x80000000, 0x7fffffff), true);
            buffer.append(scratch.subarray(0, 4));
            return;
        case "uint32":
            view.setUint32(0, checkedNumber(value, primitive, 0, 0xffffffff), true);
            buffer.append(scratch.subarray(0, 4));
            return;
        case "int64":
            view.setBigInt64(0, checkedBigInt(value, primitive, -(1n << 63n), (1n << 63n) - 1n), true);
            buffer.append(scratch);
            return;
        case "uint64":
            view.setBigUint64(0, checkedBigInt(value, primitive, 0n, (1n << 64n) - 1n), true);
            buffer.append(scratch);
            return;
        case "float32":
            encodeFloat32(view, value);
            buffer.append(scratch.subarray(0, 4));
            return;
        case "float64":
            encodeFloat64(view, value);
            buffer.append(scratch);
            return;
    }
}

export function decodePrimitive(cursor: DecoderCursor, primitive: NumericPrimitive): number | bigint {
    switch (primitive) {
        case "int8": return new DataView(cursor.readBytes(1).buffer, cursor.bytes.byteOffset + cursor.offset - 1, 1).getInt8(0);
        case "uint8": return cursor.readByte();
        case "int16": return readNumber(cursor, 2, (view) => view.getInt16(0, true));
        case "uint16": return readNumber(cursor, 2, (view) => view.getUint16(0, true));
        case "int32": return readNumber(cursor, 4, (view) => view.getInt32(0, true));
        case "uint32": return readNumber(cursor, 4, (view) => view.getUint32(0, true));
        case "int64": return readBigInt(cursor, 8, (view) => view.getBigInt64(0, true));
        case "uint64": return readBigInt(cursor, 8, (view) => view.getBigUint64(0, true));
        case "float32": return decodeFloat32(cursor);
        case "float64": return decodeFloat64(cursor);
    }
}

function readNumber(cursor: DecoderCursor, length: number, read: (view: DataView) => number): number {
    const bytes = cursor.readBytes(length);
    return read(new DataView(bytes.buffer, bytes.byteOffset, bytes.byteLength));
}

function readBigInt(cursor: DecoderCursor, length: number, read: (view: DataView) => bigint): bigint {
    const bytes = cursor.readBytes(length);
    return read(new DataView(bytes.buffer, bytes.byteOffset, bytes.byteLength));
}

function decodeFloat32(cursor: DecoderCursor): number {
    const bytes = cursor.readBytes(4);
    const view = new DataView(bytes.buffer, bytes.byteOffset, bytes.byteLength);
    const bits = view.getUint32(0, true);
    const value = view.getFloat32(0, true);
    if (Number.isNaN(value) && bits !== CANONICAL_FLOAT32_NAN) {
        throw new PrimitiveCodecError("noncanonical float32 NaN bit pattern");
    }
    return value;
}

function decodeFloat64(cursor: DecoderCursor): number {
    const bytes = cursor.readBytes(8);
    const view = new DataView(bytes.buffer, bytes.byteOffset, bytes.byteLength);
    const bits = view.getBigUint64(0, true);
    const value = view.getFloat64(0, true);
    if (Number.isNaN(value) && bits !== CANONICAL_FLOAT64_NAN) {
        throw new PrimitiveCodecError("noncanonical float64 NaN bit pattern");
    }
    return value;
}

function encodeFloat32(view: DataView, value: unknown): void {
    if (typeof value !== "number") throw new PrimitiveCodecError("float32 requires a number");
    if (Number.isNaN(value)) view.setUint32(0, CANONICAL_FLOAT32_NAN, true);
    else view.setFloat32(0, value, true);
}

function encodeFloat64(view: DataView, value: unknown): void {
    if (typeof value !== "number") throw new PrimitiveCodecError("float64 requires a number");
    if (Number.isNaN(value)) view.setBigUint64(0, CANONICAL_FLOAT64_NAN, true);
    else view.setFloat64(0, value, true);
}

function checkedNumber(value: unknown, primitive: NumericPrimitive, minimum: number, maximum: number): number {
    if (typeof value !== "number" || !Number.isFinite(value) || !Number.isInteger(value) || value < minimum || value > maximum) {
        throw new PrimitiveCodecError(`${primitive} value is outside its exact integer range`);
    }
    return value;
}

function checkedBigInt(value: unknown, primitive: NumericPrimitive, minimum: bigint, maximum: bigint): bigint {
    if (typeof value !== "bigint" || value < minimum || value > maximum) {
        throw new PrimitiveCodecError(`${primitive} value is outside its exact integer range`);
    }
    return value;
}
