import type {
    Comment,
    Declaration,
    Field,
    InterfaceFile,
    ListType,
    Param,
    RecordType,
    TypeExpr,
} from "./ast.js";
import { Scanner } from "./scanner.js";
import type { Span } from "./token.js";
import { TokenKind } from "./token.js";

interface Anchor {
    readonly start: number;
    readonly set: (value: string) => void;
}

export function attachDocumentation(file: InterfaceFile): void {
    new DocumentationAttacher(file).attach();
}

export function normalizeDocumentation(text: string): string {
    let normalized = text.replaceAll("\r\n", "\n").replaceAll("\r", "\n");
    let lines = normalized.split("\n").map((line) => line.replace(/[ \t]+$/u, ""));
    while (lines.length > 0 && lines[0] === "") lines = lines.slice(1);
    while (lines.length > 0 && lines.at(-1) === "") lines = lines.slice(0, -1);
    if (lines.length === 0) return "";

    let prefix = leadingWhitespace(lines[0]!);
    for (const line of lines.slice(1)) {
        if (line === "") continue;
        const other = leadingWhitespace(line);
        let length = 0;
        while (length < prefix.length && length < other.length && prefix[length] === other[length]) {
            length += 1;
        }
        prefix = prefix.slice(0, length);
        if (prefix === "") break;
    }
    if (prefix !== "") {
        lines = lines.map((line) => line === "" ? line : line.slice(prefix.length));
    }
    return lines.join("\n");
}

function leadingWhitespace(value: string): string {
    const match = /^[ \t]*/u.exec(value);
    return match?.[0] ?? "";
}

function groupDocumentation(group: readonly Comment[]): string {
    return group
        .map((comment) => normalizeDocumentation(comment.text))
        .filter((text) => text !== "")
        .join("\n\n");
}

class DocumentationAttacher {
    private readonly tokenEnds: number[] = [];
    private readonly nodeEnds: number[] = [];
    private readonly prefixEnds: number[] = [];
    private readonly anchors: Anchor[] = [];

    constructor(private readonly file: InterfaceFile) {}

    attach(): void {
        this.collectTokens();
        for (const declaration of this.file.declarations) this.collectDeclaration(declaration);
        this.nodeEnds.sort((a, b) => a - b);
        this.prefixEnds.sort((a, b) => a - b);
        this.attachAnchors();
    }

    private collectTokens(): void {
        const scanner = new Scanner(this.file.source);
        while (true) {
            const token = scanner.next();
            if (token.kind === TokenKind.EOF) return;
            if (token.kind !== TokenKind.Comment) this.tokenEnds.push(token.span.end);
        }
    }

    private collectDeclaration(declaration: Declaration): void {
        switch (declaration.kind) {
            case "type-decl":
                declaration.doc = "";
                this.nodeEnds.push(declaration.semi.end);
                this.prefixEnds.push(declaration.name.span.end);
                this.anchors.push({ start: declaration.typeSpan.start, set: (value) => { declaration.doc = value; } });
                this.walkType(declaration.type);
                return;
            case "exception-decl":
                declaration.doc = "";
                this.nodeEnds.push(declaration.semi.end);
                this.prefixEnds.push(declaration.name.span.end);
                this.anchors.push({ start: declaration.exceptionSpan.start, set: (value) => { declaration.doc = value; } });
                if (declaration.type !== undefined) this.walkType(declaration.type);
                return;
            case "procedure-decl":
                declaration.doc = "";
                this.nodeEnds.push(declaration.semi.end);
                this.prefixEnds.push(declaration.rBrace.end);
                this.anchors.push({ start: declaration.procedureSpan.start, set: (value) => { declaration.doc = value; } });
                for (const parameter of declaration.params) {
                    this.collectParameter(parameter);
                    this.walkType(parameter.type);
                }
                if (declaration.result !== undefined) this.walkType(declaration.result);
                return;
        }
    }

    private collectParameter(parameter: Param): void {
        parameter.doc = "";
        this.nodeEnds.push(parameter.semi.end);
        this.prefixEnds.push(parameter.name.span.end);
        this.anchors.push({ start: parameter.name.span.start, set: (value) => { parameter.doc = value; } });
    }

    private walkType(root: TypeExpr): void {
        const records: { readonly record: RecordType; next: number }[] = [];
        let current: TypeExpr | undefined = root;
        while (current !== undefined) {
            if (current.kind === "list") {
                current = this.walkListChain(current);
                continue;
            }

            this.collectType(current);
            if (current.kind === "record") records.push({ record: current, next: 0 });
            current = undefined;
            while (records.length > 0) {
                const frame = records.at(-1)!;
                if (frame.next >= frame.record.fields.length) {
                    records.pop();
                    continue;
                }
                const field = frame.record.fields[frame.next]!;
                frame.next += 1;
                this.collectField(field);
                current = field.type;
                break;
            }
        }
    }

    private walkListChain(list: ListType): TypeExpr {
        let current = list;
        let count = 0;
        while (true) {
            const node = current;
            node.doc = "";
            this.prefixEnds.push(node.listSpan.end);
            this.anchors.push({ start: node.listSpan.start, set: (value) => { node.doc = value; } });
            count += 1;
            if (node.elem.kind === "list") {
                current = node.elem;
                continue;
            }
            const end = typeEnd(node.elem);
            for (let i = 0; i < count; i += 1) this.nodeEnds.push(end);
            return node.elem;
        }
    }

    private collectType(type: Exclude<TypeExpr, ListType>): void {
        type.doc = "";
        const span = typeSpan(type);
        this.nodeEnds.push(span.end);
        this.anchors.push({ start: span.start, set: (value) => { type.doc = value; } });
    }

    private collectField(field: Field): void {
        field.doc = "";
        this.nodeEnds.push(field.semi.end);
        this.prefixEnds.push(field.name.span.end);
        this.anchors.push({ start: field.name.span.start, set: (value) => { field.doc = value; } });
    }

    private attachAnchors(): void {
        const comments = this.file.comments;
        const commentEnds = comments.map((comment) => comment.span.end);
        for (const anchor of this.anchors) {
            const tokenIndex = firstGreater(this.tokenEnds, anchor.start);
            const previousEnd = tokenIndex === 0 ? 0 : this.tokenEnds[tokenIndex - 1]!;
            const lower = firstGreater(commentEnds, previousEnd);
            const upper = firstGreater(commentEnds, anchor.start);
            if (lower >= upper) continue;
            const group = comments.slice(lower, upper);
            let segment = group.length - 1;
            while (
                segment > 0 &&
                !hasBlankLine(this.file.source.bytes, group[segment - 1]!.span.end, group[segment]!.span.start)
            ) {
                segment -= 1;
            }
            if (hasBlankLine(this.file.source.bytes, group.at(-1)!.span.end, anchor.start)) continue;

            let first = segment;
            if (!this.isPrefixEnd(previousEnd)) {
                while (first < group.length && this.isTrailing(group[first]!)) first += 1;
            }
            if (first < group.length) anchor.set(groupDocumentation(group.slice(first)));
        }
    }

    private isPrefixEnd(end: number): boolean {
        const index = firstAtLeast(this.prefixEnds, end);
        return index < this.prefixEnds.length && this.prefixEnds[index] === end;
    }

    private isTrailing(comment: Comment): boolean {
        const index = firstGreater(this.nodeEnds, comment.span.start);
        if (index === 0) return false;
        return this.file.source.position(this.nodeEnds[index - 1]! - 1).line ===
            this.file.source.position(comment.span.start).line;
    }
}

function firstGreater(values: readonly number[], value: number): number {
    let low = 0;
    let high = values.length;
    while (low < high) {
        const middle = (low + high) >>> 1;
        if (values[middle]! <= value) low = middle + 1;
        else high = middle;
    }
    return low;
}

function firstAtLeast(values: readonly number[], value: number): number {
    let low = 0;
    let high = values.length;
    while (low < high) {
        const middle = (low + high) >>> 1;
        if (values[middle]! < value) low = middle + 1;
        else high = middle;
    }
    return low;
}

function typeSpan(type: Exclude<TypeExpr, ListType>): Span {
    if (type.kind === "primitive") return type.span;
    if (type.kind === "named") return type.name.span;
    return { start: type.recordSpan.start, end: type.rBrace.end };
}

function typeEnd(type: TypeExpr): number {
    let current = type;
    while (current.kind === "list") current = current.elem;
    return typeSpan(current as Exclude<TypeExpr, ListType>).end;
}

function hasBlankLine(bytes: Uint8Array, start: number, end: number): boolean {
    let previousLineEnd = -1;
    for (let i = start; i < end; i += 1) {
        if (bytes[i] !== 0x0a) continue;
        let contentEnd = i;
        if (i > start && bytes[i - 1] === 0x0d) contentEnd -= 1;
        if (previousLineEnd >= 0 && isBlankContent(bytes.subarray(previousLineEnd, contentEnd))) return true;
        previousLineEnd = i + 1;
    }
    return false;
}

function isBlankContent(bytes: Uint8Array): boolean {
    return [...bytes].every((byte) => byte === 0x20 || byte === 0x09);
}
