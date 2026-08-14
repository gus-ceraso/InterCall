export type PrimitiveInstruction = {
    readonly op: "primitive";
    readonly primitive:
        | "int8" | "int16" | "int32" | "int64"
        | "uint8" | "uint16" | "uint32" | "uint64"
        | "float32" | "float64" | "string" | "bytes";
};

export type ListInstruction = {
    readonly op: "list";
    readonly element: number;
};

export interface RecordFieldInstruction {
    readonly name: string;
    readonly value: number;
}

export type RecordInstruction = {
    readonly op: "record";
    readonly fields: readonly RecordFieldInstruction[];
};

export type NamedInstruction = {
    readonly op: "named";
    readonly target: number;
};

export type ZeroInstruction = {
    readonly op: "zero";
};

export type CodecInstruction =
    | PrimitiveInstruction
    | ListInstruction
    | RecordInstruction
    | NamedInstruction
    | ZeroInstruction;

export interface CodecProgram {
    readonly instructions: readonly CodecInstruction[];
    readonly root: number;
    readonly zeroWidth: boolean;
    readonly zeroWidthInstructions: readonly boolean[];
    readonly zeroValues: readonly (unknown | undefined)[];
}

export function makeCodecProgram(
    instructions: readonly CodecInstruction[],
    root: number,
): CodecProgram {
    if (!Number.isInteger(root) || root < 0 || root >= instructions.length) {
        throw new RangeError(`codec root ${root} is outside the instruction table`);
    }
    const copied = instructions.map((instruction) => {
        if (instruction.op === "list" || instruction.op === "named") {
            const target = instruction.op === "list" ? instruction.element : instruction.target;
            validateTarget(target, instructions.length);
        }
        if (instruction.op === "record") {
            for (const field of instruction.fields) validateTarget(field.value, instructions.length);
        }
        if (instruction.op !== "record") return Object.freeze({ ...instruction });
        const fields = instruction.fields.map((field) => Object.freeze({ ...field }));
        return Object.freeze({ ...instruction, fields: Object.freeze(fields) });
    });
    const frozen = Object.freeze(copied);
    const zeroWidthInstructions = instructionZeroWidths(frozen);
    const zeroValues = buildZeroValues(frozen, zeroWidthInstructions);
    return Object.freeze({
        instructions: frozen,
        root,
        zeroWidth: zeroWidthInstructions[root]!,
        zeroWidthInstructions: Object.freeze(zeroWidthInstructions),
        zeroValues: Object.freeze(zeroValues),
    });
}

function validateTarget(target: number, length: number): void {
    if (!Number.isInteger(target) || target < 0 || target >= length) {
        throw new RangeError(`codec instruction target ${target} is outside the instruction table`);
    }
}

function buildZeroValues(
    instructions: readonly CodecInstruction[],
    zeroWidths: readonly boolean[],
): (unknown | undefined)[] {
    const values: (unknown | undefined)[] = new Array(instructions.length);
    for (let root = 0; root < instructions.length; root += 1) {
        if (!zeroWidths[root] || values[root] !== undefined) continue;
        const stack: Array<{ readonly index: number; readonly expanded: boolean }> = [{ index: root, expanded: false }];
        while (stack.length > 0) {
            const current = stack.pop()!;
            if (values[current.index] !== undefined) continue;
            const instruction = instructions[current.index]!;
            if (!current.expanded) {
                stack.push({ index: current.index, expanded: true });
                if (instruction.op === "named") stack.push({ index: instruction.target, expanded: false });
                else if (instruction.op === "record") {
                    for (let index = instruction.fields.length - 1; index >= 0; index -= 1) {
                        stack.push({ index: instruction.fields[index]!.value, expanded: false });
                    }
                }
                continue;
            }
            if (instruction.op === "zero") values[current.index] = Object.freeze({});
            else if (instruction.op === "named") values[current.index] = values[instruction.target];
            else if (instruction.op === "record") {
                const value: Record<string, unknown> = {};
                for (const field of instruction.fields) {
                    Object.defineProperty(value, field.name, {
                        configurable: false,
                        enumerable: true,
                        value: values[field.value],
                        writable: false,
                    });
                }
                values[current.index] = Object.freeze(value);
            }
        }
    }
    return values;
}

function instructionZeroWidths(instructions: readonly CodecInstruction[]): boolean[] {
    const states = new Map<number, boolean>();
    for (let root = 0; root < instructions.length; root += 1) {
        if (states.has(root)) continue;
        const visiting = new Set<number>();
        const stack: Array<{ readonly index: number; readonly expanded: boolean }> = [{ index: root, expanded: false }];
        while (stack.length > 0) {
            const current = stack.pop()!;
            if (states.has(current.index)) continue;
            const instruction = instructions[current.index];
            if (instruction === undefined) throw new RangeError(`missing codec instruction ${current.index}`);
            if (!current.expanded) {
                if (visiting.has(current.index)) throw new Error("recursive codec instruction graph");
                visiting.add(current.index);
                stack.push({ index: current.index, expanded: true });
                if (instruction.op === "named") stack.push({ index: instruction.target, expanded: false });
                else if (instruction.op === "record") {
                    for (let index = instruction.fields.length - 1; index >= 0; index -= 1) {
                        stack.push({ index: instruction.fields[index]!.value, expanded: false });
                    }
                }
                continue;
            }
            let zero: boolean;
            switch (instruction.op) {
                case "zero": zero = true; break;
                case "primitive": zero = false; break;
                case "list": zero = false; break;
                case "named": zero = states.get(instruction.target) === true; break;
                case "record": zero = instruction.fields.every((field) => states.get(field.value) === true); break;
            }
            visiting.delete(current.index);
            states.set(current.index, zero);
        }
    }
    return instructions.map((_, index) => states.get(index) === true);
}
