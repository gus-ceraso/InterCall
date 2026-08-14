import type { CodecInstruction, CodecProgram } from "./codec-program.js";
import { CodecBudget, CodecResourceError } from "./codec-budget.js";
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

export function encodeProgram(program: CodecProgram, value: unknown, budget = new CodecBudget()): Uint8Array {
    const buffer = new EncoderBuffer();
    encodeProgramInto(program, value, buffer, budget);
    return buffer.finish();
}

export function encodeProgramsToPayload(
    programs: readonly CodecProgram[],
    values: readonly unknown[],
    budget = new CodecBudget(),
): Uint8Array {
    if (programs.length !== values.length) throw new RangeError("codec program/value count mismatch");
    const buffer = new EncoderBuffer();
    for (let index = 0; index < programs.length; index += 1) encodeProgramInto(programs[index]!, values[index], buffer, budget);
    return buffer.finish();
}

function encodeProgramInto(program: CodecProgram, value: unknown, buffer: EncoderBuffer, budget: CodecBudget): void {
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
                encodeListFrame(program, instruction, frame.value, stack, buffer, budget);
                break;
            case "record":
                encodeRecordFrame(instruction, frame.value, stack, budget);
                break;
        }
    }
}

export function decodeProgram(program: CodecProgram, bytes: Uint8Array, budget = new CodecBudget()): unknown {
    const cursor = new DecoderCursor(bytes);
    const result = decodeProgramValue(program, cursor, budget);
    if (cursor.remaining !== 0) throw new PrimitiveCodecError("trailing bytes after payload");
    return result;
}

export function decodeProgramsFromPayload(programs: readonly CodecProgram[], bytes: Uint8Array, budget = new CodecBudget()): unknown[] {
    const cursor = new DecoderCursor(bytes);
    const values = programs.map((program) => decodeProgramValue(program, cursor, budget));
    if (cursor.remaining !== 0) throw new PrimitiveCodecError("trailing bytes after payload");
    return values;
}

function decodeProgramValue(program: CodecProgram, cursor: DecoderCursor, budget: CodecBudget): unknown {
    let result: unknown;
    const stack: Array<{ readonly index: number; readonly assign: (value: unknown) => void }> = [
        { index: program.root, assign: (value) => { result = value; } },
    ];
    while (stack.length > 0) {
        const frame = stack.pop()!;
        if (program.zeroWidthInstructions[frame.index]) {
            budget.charge(program.zeroWidthNodeCosts[frame.index]!);
            frame.assign(program.zeroValues[frame.index]);
            continue;
        }
        const instruction = requireInstruction(program, frame.index);
        switch (instruction.op) {
            case "primitive": frame.assign(decodePrimitiveValue(cursor, instruction)); break;
            case "zero": frame.assign(program.zeroValues[frame.index]); break;
            case "named": stack.push({ index: instruction.target, assign: frame.assign }); break;
            case "list": decodeListFrame(program, instruction, cursor, frame.assign, stack, budget); break;
            case "record": decodeRecordFrame(instruction, cursor, frame.assign, stack, budget); break;
        }
    }
    return result;
}

export function encodePrograms(
    programs: readonly CodecProgram[],
    values: readonly unknown[],
): Uint8Array[] {
    if (programs.length !== values.length) throw new RangeError("codec program/value count mismatch");
    const budget = new CodecBudget();
    return programs.map((program, index) => encodeProgram(program, values[index], budget));
}

export function decodePrograms(
    programs: readonly CodecProgram[],
    payloads: readonly Uint8Array[],
): unknown[] {
    if (programs.length !== payloads.length) throw new RangeError("codec program/payload count mismatch");
    const budget = new CodecBudget();
    return programs.map((program, index) => decodeProgram(program, payloads[index]!, budget));
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
    budget: CodecBudget,
): void {
    if (!Array.isArray(value)) throw new PrimitiveCodecError("list value is not a JavaScript array");
    if (value.length > MAX_LIST_ELEMENTS) throw new PrimitiveCodecError("list exceeds the element limit");
    budget.charge(1 + value.length);
    encodePrimitive(buffer, "uint64", BigInt(value.length));
    if (program.zeroWidthInstructions[instruction.element]) {
        budget.charge(value.length * program.zeroWidthNodeCosts[instruction.element]!);
        return;
    }
    for (let index = value.length - 1; index >= 0; index -= 1) {
        stack.push({ index: instruction.element, value: value[index] });
    }
}

function encodeRecordFrame(
    instruction: Extract<CodecInstruction, { readonly op: "record" }>,
    value: unknown,
    stack: Array<{ readonly index: number; readonly value: unknown }>,
    budget: CodecBudget,
): void {
    budget.charge(1);
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
    budget: CodecBudget,
): void {
    const count = decodePrimitive(cursor, "uint64");
    if (typeof count !== "bigint" || count > BigInt(MAX_LIST_ELEMENTS)) {
        throw new PrimitiveCodecError("wire list count exceeds the element limit");
    }
    const length = Number(count);
    budget.charge(1 + length);
    let result: unknown[];
    try {
        result = new Array<unknown>(length);
    } catch (error) {
        throw new CodecResourceError("unable to allocate decoded list", { cause: error });
    }
    assign(result);
    if (program.zeroWidthInstructions[instruction.element]) {
        budget.charge(length * program.zeroWidthNodeCosts[instruction.element]!);
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
    budget: CodecBudget,
): void {
    budget.charge(1);
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
