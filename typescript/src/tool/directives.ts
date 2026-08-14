import ts from "typescript";

export type TypeScriptDirectiveKind = "procedure" | "exception" | "type" | "param" | "field" | "returns";

export interface TypeScriptDirective {
    readonly kind: TypeScriptDirectiveKind;
    readonly arguments: string;
    readonly text: string;
    readonly start: number;
    readonly end: number;
    readonly line: number;
    readonly character: number;
}

const directivePattern = /^\s*\*?\s*@(intercall\s+(?:procedure|exception|type|param|field)|param|returns)(?:\s+(.*?))?\s*$/u;

export function scanTypeScriptDirectives(sourceFile: ts.SourceFile): TypeScriptDirective[] {
    const text = sourceFile.getFullText();
    const starts = new Set<number>();
    const directives: TypeScriptDirective[] = [];
    const visit = (node: ts.Node) => {
        for (const range of ts.getLeadingCommentRanges(text, node.getFullStart()) ?? []) {
            if (starts.has(range.pos)) continue;
            starts.add(range.pos);
            const comment = text.slice(range.pos, range.end);
            let offset = 0;
            for (const line of comment.split(/\r\n|\r|\n/u)) {
                const match = directivePattern.exec(line.replace(/\/\*\*?|\*\//gu, ""));
                if (match !== null) {
                    const tag = match[1]!;
                    const kind = tag.startsWith("intercall") ? tag.slice("intercall ".length) as TypeScriptDirectiveKind : tag as TypeScriptDirectiveKind;
                    const lineStart = range.pos + offset;
                    const start = lineStart + Math.max(0, line.search(/@/u));
                    const end = lineStart + line.length;
                    const position = sourceFile.getLineAndCharacterOfPosition(start);
                    directives.push({ kind, arguments: match[2] ?? "", text: line.trim(), start, end, line: position.line + 1, character: position.character + 1 });
                }
                offset += line.length + 1;
            }
        }
        ts.forEachChild(node, visit);
    };
    visit(sourceFile);
    directives.sort((left, right) => left.start - right.start);
    return directives;
}
