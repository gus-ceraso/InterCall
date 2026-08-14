import type { Span } from "./token.js";

export interface Position {
    readonly offset: number;
    readonly line: number;
    readonly column: number;
}

export class SyntaxDiagnostic extends Error {
    readonly filename: string;
    readonly position: Position;
    readonly span: Span;

    constructor(filename: string, position: Position, span: Span, message: string) {
        super(message);
        this.name = "SyntaxDiagnostic";
        this.filename = filename;
        this.position = position;
        this.span = span;
    }

    override toString(): string {
        const prefix = this.filename === ""
            ? `${this.position.line}:${this.position.column}`
            : `${this.filename}:${this.position.line}:${this.position.column}`;
        return `${prefix}: ${this.message}`;
    }
}

export class SourceFile {
    readonly name: string;
    readonly size: number;
    readonly bytes: Uint8Array;
    private readonly lineStarts: number[];

    constructor(name: string, bytes: Uint8Array) {
        this.name = name;
        this.bytes = bytes;
        this.size = bytes.byteLength;
        this.lineStarts = [0];
        for (let i = 0; i < bytes.length; i += 1) {
            if (bytes[i] === 0x0a) this.lineStarts.push(i + 1);
        }
    }

    position(offset: number): Position {
        if (!Number.isInteger(offset) || offset < 0 || offset > this.size) {
            throw new RangeError(`source position ${offset} is outside [0, ${this.size}]`);
        }
        let low = 0;
        let high = this.lineStarts.length;
        while (low < high) {
            const middle = (low + high) >>> 1;
            if (this.lineStarts[middle]! <= offset) low = middle + 1;
            else high = middle;
        }
        const lineIndex = low - 1;
        return {
            offset,
            line: lineIndex + 1,
            column: offset - this.lineStarts[lineIndex]! + 1,
        };
    }

    text(span: Span): string {
        if (span.start < 0 || span.end < span.start || span.end > this.size) {
            throw new RangeError(`source span [${span.start}, ${span.end}) is invalid`);
        }
        return new TextDecoder("utf-8", { fatal: true }).decode(
            this.bytes.subarray(span.start, span.end),
        );
    }
}
