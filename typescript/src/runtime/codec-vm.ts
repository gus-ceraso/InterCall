import type { CodecInstruction, CodecProgram } from "./codec-program.js";
import { EncoderBuffer } from "./encoder-buffer.js";
import { encodeBytes, decodeBytes } from "./bytes-codec.js";
import { MAX_LIST_ELEMENTS } from "./list-codec.js";
import { assertClosedRecord } from "./record-codec.js";
import {
    DecoderCursor,
    decodePrimitive,
    encodePrimitive,
    PrimitiveCodecError,
} from "./primitive-codec.js";
import { decodeString, encodeString } from "./text-codec.js";

export function encodeProgram(program: CodecProgram, value: unknown): Uint8Array {
    const buffer = new EncoderBuffer();
    const stack: Array<{ readonly index: number; readonly value: unknown }> = [{ index: program.root, value }];
    while (stack.length > 0) {
        const frame = stack.pop()!;
        const instruction = requireInstruction(program, frame.index);
        switch (instruction.op) {
            case "primitive":
                encodePrimitiveValue(buffer, instruction, frame.value);
                break;
            case "zero":
                assertClosedRecord(frame.value, []);
                break;
            case "named":
                stack.push({ index: instruction.target, value: frame.value });
                break;
            case "list":
                encodeListFrame(program, instruction, frame.value, stack, buffer);
                break;
            case "record":
                encodeRecordFrame(instruction, frame.value, stack);
                break;
        }
    }
    return buffer.finish();
}

export function decodeProgram(program: CodecProgram, bytes: Uint8Array): unknown {
    const cursor = new DecoderCursor(bytes);
    let result: unknown;
    const stack: Array<{ readonly index: number; readonly assign: (value: unknown) => void }> = [
        { index: program.root, assign: (value) => { result = value; } },
    ];
    while (stack.length > 0) {
        const frame = stack.pop()!;
        if (program.zeroWidthInstructions[frame.index]) {
            frame.assign(program.zeroValues[frame.index]);
            continue;
        }
        const instruction = requireInstruction(program, frame.index);
        switch (instruction.op) {
            case "primitive":
                frame.assign(decodePrimitiveValue(cursor, instruction));
                break;
            case "zero":
                frame.assign(program.zeroValues[frame.index]);
                break;
            case "named":
                stack.push({ index: instruction.target, assign: frame.assign });
                break;
            case "list":
                decodeListFrame(program, instruction, cursor, frame.assign, stack);
                break;
            case "record":
                decodeRecordFrame(instruction, cursor, frame.assign, stack);
                break;
        }
    }
    if (cursor.remaining !== 0) throw new PrimitiveCodecError("trailing bytes after payload");
    return result;
}

function encodePrimitiveValue(buffer: EncoderBuffer, instruction: Extract<CodecInstruction, { readonly op: "primitive" }>, value: unknown): void {
    if (instruction.primitive === "string") encodeString(buffer, value);
    else if (instruction.primitive === "bytes") encodeBytes(buffer, value);
    else encodePrimitive(buffer, instruction.primitive, value);
}

function decodePrimitiveValue(cursor: DecoderCursor, instruction: Extract<CodecInstruction, { readonly op: "primitive" }>): unknown {
    if (instruction.primitive === "string") return decodeString(cursor);
    if (instruction.primitive === "bytes") return decodeBytes(cursor);
    return decodePrimitive(cursor, instruction.primitive);
}

function encodeListFrame(
    program: CodecProgram,
    instruction: Extract<CodecInstruction, { readonly op: "list" }>,
    value: unknown,
    stack: Array<{ readonly index: number; readonly value: unknown }>,
    buffer: EncoderBuffer,
): void {
    if (!Array.isArray(value)) throw new PrimitiveCodecError("list value is not a JavaScript array");
    if (value.length > MAX_LIST_ELEMENTS) throw new PrimitiveCodecError("list exceeds the element limit");
    encodePrimitive(buffer, "uint64", BigInt(value.length));
    if (program.zeroWidthInstructions[instruction.element]) return;
    for (let index = value.length - 1; index >= 0; index -= 1) {
        stack.push({ index: instruction.element, value: value[index] });
    }
}

function encodeRecordFrame(
    instruction: Extract<CodecInstruction, { readonly op: "record" }>,
    value: unknown,
    stack: Array<{ readonly index: number; readonly value: unknown }>,
): void {
    const object = assertClosedRecord(value, instruction.fields.map((field) => ({ name: field.name })));
    for (let index = instruction.fields.length - 1; index >= 0; index -= 1) {
        const field = instruction.fields[index]!;
        stack.push({ index: field.value, value: object[field.name] });
    }
}

function decodeListFrame(
    program: CodecProgram,
    instruction: Extract<CodecInstruction, { readonly op: "list" }>,
    cursor: DecoderCursor,
    assign: (value: unknown) => void,
    stack: Array<{ readonly index: number; readonly assign: (value: unknown) => void }>,
): void {
    const count = decodePrimitive(cursor, "uint64");
    if (typeof count !== "bigint" || count > BigInt(MAX_LIST_ELEMENTS)) {
        throw new PrimitiveCodecError("wire list count exceeds the element limit");
    }
    const length = Number(count);
    const result = new Array<unknown>(length);
    assign(result);
    if (program.zeroWidthInstructions[instruction.element]) {
        result.fill(program.zeroValues[instruction.element]);
        return;
    }
    for (let index = length - 1; index >= 0; index -= 1) {
        stack.push({ index: instruction.element, assign: (value) => { result[index] = value; } });
    }
}

function decodeRecordFrame(
    instruction: Extract<CodecInstruction, { readonly op: "record" }>,
    cursor: DecoderCursor,
    assign: (value: unknown) => void,
    stack: Array<{ readonly index: number; readonly assign: (value: unknown) => void }>,
): void {
    const result: Record<string, unknown> = {};
    assign(result);
    for (let index = instruction.fields.length - 1; index >= 0; index -= 1) {
        const field = instruction.fields[index]!;
        stack.push({
            index: field.value,
            assign: (value) => Object.defineProperty(result, field.name, {
                configurable: true,
                enumerable: true,
                value,
                writable: true,
            }),
        });
    }
}

function requireInstruction(program: CodecProgram, index: number): CodecInstruction {
    const instruction = program.instructions[index];
    if (instruction === undefined) throw new PrimitiveCodecError(`missing codec instruction ${index}`);
    return instruction;
}
