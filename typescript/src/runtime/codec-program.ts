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
    return Object.freeze({
        instructions: frozen,
        root,
        zeroWidth: instructionIsZeroWidth(frozen, root),
    });
}

function validateTarget(target: number, length: number): void {
    if (!Number.isInteger(target) || target < 0 || target >= length) {
        throw new RangeError(`codec instruction target ${target} is outside the instruction table`);
    }
}

function instructionIsZeroWidth(
    instructions: readonly CodecInstruction[],
    root: number,
): boolean {
    const states = new Map<number, boolean>();
    const visiting = new Set<number>();
    const visit = (index: number): boolean => {
        const known = states.get(index);
        if (known !== undefined) return known;
        if (visiting.has(index)) throw new Error("recursive codec instruction graph");
        const instruction = instructions[index];
        if (instruction === undefined) throw new RangeError(`missing codec instruction ${index}`);
        visiting.add(index);
        let zero: boolean;
        switch (instruction.op) {
            case "zero":
                zero = true;
                break;
            case "primitive":
                zero = false;
                break;
            case "named":
            case "list":
                zero = instruction.op === "named" ? visit(instruction.target) : false;
                break;
            case "record":
                zero = instruction.fields.every((field) => visit(field.value));
                break;
        }
        visiting.delete(index);
        states.set(index, zero);
        return zero;
    };
    return visit(root);
}
