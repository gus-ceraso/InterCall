import {
    tokenFromIdentifier,
    TokenKind,
} from "./token.js";
import type { Span, Token } from "./token.js";
import { SourceFile, SyntaxDiagnostic } from "./source.js";

const textDecoder = new TextDecoder("utf-8", { fatal: true });

export class Scanner {
    private offset = 0;
    private failure: SyntaxDiagnostic | undefined;

    constructor(private readonly file: SourceFile) {}

    next(): Token {
        if (this.failure !== undefined) throw this.failure;
        while (this.offset < this.file.size && isWhitespace(this.file.bytes[this.offset]!)) {
            this.offset += 1;
        }
        if (this.offset >= this.file.size) {
            return { kind: TokenKind.EOF, span: { start: this.offset, end: this.offset }, literal: "" };
        }

        const start = this.offset;
        const first = this.file.bytes[this.offset]!;
        if (first === 0x7b) return this.single(start, TokenKind.LBrace);
        if (first === 0x7d) return this.single(start, TokenKind.RBrace);
        if (first === 0x3b) return this.single(start, TokenKind.Semicolon);
        if (first === 0x2f && this.file.bytes[this.offset + 1] === 0x2a) {
            return this.scanComment();
        }
        if (isIdentifierStart(first)) return this.scanIdentifier();
        throw this.invalidCharacter();
    }

    private single(start: number, kind: TokenKind): Token {
        this.offset += 1;
        return { kind, span: { start, end: this.offset }, literal: "" };
    }

    private scanIdentifier(): Token {
        const start = this.offset;
        while (this.offset < this.file.size && isIdentifierPart(this.file.bytes[this.offset]!)) {
            this.offset += 1;
        }
        const literal = asciiDecode(this.file.bytes.subarray(start, this.offset));
        return {
            kind: tokenFromIdentifier(literal),
            span: { start, end: this.offset },
            literal,
        };
    }

    private scanComment(): Token {
        const start = this.offset;
        this.offset += 2;
        const bodyStart = this.offset;
        while (this.offset < this.file.size) {
            if (this.file.bytes[this.offset] === 0x2a && this.file.bytes[this.offset + 1] === 0x2f) {
                const bodyEnd = this.offset;
                const invalid = firstInvalidUtf8(this.file.bytes, bodyStart, bodyEnd);
                if (invalid !== undefined) {
                    throw this.fail(invalid, { start: invalid, end: invalid + 1 }, "invalid UTF-8 encoding");
                }
                this.offset += 2;
                return {
                    kind: TokenKind.Comment,
                    span: { start, end: this.offset },
                    literal: textDecoder.decode(this.file.bytes.subarray(bodyStart, bodyEnd)),
                };
            }
            this.offset += 1;
        }

        const invalid = firstInvalidUtf8(this.file.bytes, bodyStart, this.file.size);
        if (invalid !== undefined) {
            throw this.fail(invalid, { start: invalid, end: invalid + 1 }, "invalid UTF-8 encoding");
        }
        throw this.fail(start, { start, end: this.file.size }, "comment not terminated");
    }

    private invalidCharacter(): SyntaxDiagnostic {
        const start = this.offset;
        if (isCompleteBom(this.file.bytes, start)) {
            throw this.fail(start, { start, end: start + 3 }, "invalid byte-order mark");
        }
        const size = utf8SequenceSize(this.file.bytes, start);
        if (size === undefined) {
            throw this.fail(start, { start, end: start + 1 }, "invalid UTF-8 encoding");
        }
        const codePoint = decodeCodePoint(this.file.bytes, start, size);
        this.offset += size;
        throw this.fail(
            start,
            { start, end: start + size },
            `invalid character ${quoteCodePoint(codePoint)}`,
        );
    }

    private fail(offset: number, span: Span, message: string): SyntaxDiagnostic {
        const diagnostic = new SyntaxDiagnostic(this.file.name, this.file.position(offset), span, message);
        this.failure = diagnostic;
        return diagnostic;
    }
}

export function isWhitespace(byte: number): boolean {
    return byte === 0x20 || byte === 0x09 || byte === 0x0d ||
        byte === 0x0a || byte === 0x0c || byte === 0x0b;
}

export function isIdentifierStart(byte: number): boolean {
    return byte === 0x5f || (byte >= 0x41 && byte <= 0x5a) || (byte >= 0x61 && byte <= 0x7a);
}

export function isIdentifierPart(byte: number): boolean {
    return isIdentifierStart(byte) || (byte >= 0x30 && byte <= 0x39);
}

function asciiDecode(bytes: Uint8Array): string {
    let result = "";
    for (const byte of bytes) result += String.fromCharCode(byte);
    return result;
}

function isCompleteBom(bytes: Uint8Array, offset: number): boolean {
    return bytes[offset] === 0xef && bytes[offset + 1] === 0xbb && bytes[offset + 2] === 0xbf;
}

function isContinuation(byte: number | undefined): boolean {
    return byte !== undefined && byte >= 0x80 && byte <= 0xbf;
}

function utf8SequenceSize(bytes: Uint8Array, offset: number): number | undefined {
    const first = bytes[offset];
    if (first === undefined) return undefined;
    if (first <= 0x7f) return 1;
    if (first >= 0xc2 && first <= 0xdf) {
        return isContinuation(bytes[offset + 1]) ? 2 : undefined;
    }
    if (first >= 0xe0 && first <= 0xef) {
        const second = bytes[offset + 1];
        const validSecond = first === 0xe0
            ? second !== undefined && second >= 0xa0 && second <= 0xbf
            : first === 0xed
                ? second !== undefined && second >= 0x80 && second <= 0x9f
                : isContinuation(second);
        return validSecond && isContinuation(bytes[offset + 2]) ? 3 : undefined;
    }
    if (first >= 0xf0 && first <= 0xf4) {
        const second = bytes[offset + 1];
        const validSecond = first === 0xf0
            ? second !== undefined && second >= 0x90 && second <= 0xbf
            : first === 0xf4
                ? second !== undefined && second >= 0x80 && second <= 0x8f
                : isContinuation(second);
        return validSecond && isContinuation(bytes[offset + 2]) && isContinuation(bytes[offset + 3])
            ? 4
            : undefined;
    }
    return undefined;
}

function decodeCodePoint(bytes: Uint8Array, offset: number, size: number): number {
    const first = bytes[offset]!;
    if (size === 1) return first;
    if (size === 2) return ((first & 0x1f) << 6) | (bytes[offset + 1]! & 0x3f);
    if (size === 3) {
        return ((first & 0x0f) << 12) |
            ((bytes[offset + 1]! & 0x3f) << 6) |
            (bytes[offset + 2]! & 0x3f);
    }
    return ((first & 0x07) << 18) |
        ((bytes[offset + 1]! & 0x3f) << 12) |
        ((bytes[offset + 2]! & 0x3f) << 6) |
        (bytes[offset + 3]! & 0x3f);
}

function firstInvalidUtf8(bytes: Uint8Array, start: number, end: number): number | undefined {
    let offset = start;
    while (offset < end) {
        const size = utf8SequenceSize(bytes, offset);
        if (size === undefined || offset + size > end) return offset;
        offset += size;
    }
    return undefined;
}

function quoteCodePoint(codePoint: number): string {
    const escapes = new Map<number, string>([
        [0x07, "\\a"], [0x08, "\\b"], [0x09, "\\t"], [0x0a, "\\n"],
        [0x0b, "\\v"], [0x0c, "\\f"], [0x0d, "\\r"], [0x5c, "\\\\"],
        [0x27, "\\'"],
    ]);
    const escaped = escapes.get(codePoint);
    if (escaped !== undefined) return `'${escaped}'`;
    if (codePoint >= 0x20 && codePoint <= 0x7e) return `'${String.fromCodePoint(codePoint)}'`;
    if (codePoint <= 0xff) return `'\\x${codePoint.toString(16).padStart(2, "0")}'`;
    if (codePoint <= 0xffff) return `'\\u${codePoint.toString(16).padStart(4, "0")}'`;
    return `'\\U${codePoint.toString(16).padStart(8, "0")}'`;
}
