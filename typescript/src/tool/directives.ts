import ts from "typescript";

export type TypeScriptDirectiveKind = "procedure" | "exception" | "type" | "param" | "field" | "returns";

export interface TypeScriptDirective {
    readonly kind: TypeScriptDirectiveKind;
    readonly explicit: boolean;
    readonly arguments: string;
    readonly text: string;
    readonly start: number;
    readonly end: number;
    readonly line: number;
    readonly character: number;
}

const directivePattern = /^\s*\*?\s*@(intercall\s+(?:procedure|exception|type|param|field)|param|returns)(?:\s+(.*?))?\s*$/u;

export function sourceParameterDocumentation(node: ts.Node, parameterName: string): string | undefined {
    if (!ts.isFunctionDeclaration(node)) return undefined;
    const parameter = node.parameters.find((candidate) => candidate.name.getText() === parameterName);
    const tag = parameter === undefined ? undefined : ts.getJSDocParameterTags(parameter)[0];
    return tag === undefined ? undefined : jsDocCommentText(tag.comment);
}

export function sourceReturnDocumentation(node: ts.Node): string | undefined {
    const tag = ts.getJSDocReturnTag(node);
    return tag === undefined ? undefined : jsDocCommentText(tag.comment);
}

export function sourceDocumentation(node: ts.Node): string | undefined {
    const parts: string[] = [];
    for (const comment of ts.getJSDocCommentsAndTags(node)) {
        if (!ts.isJSDoc(comment)) continue;
        const lines = comment.getText().replace(/^\/\*\*?|\*\/$/gu, "").split(/\r\n|\r|\n/u);
        for (const line of lines) {
            const value = line.replace(/^\s*\*?\s?/u, "").trim();
            if (value === "" || value.startsWith("@")) continue;
            parts.push(value);
        }
    }
    return parts.length === 0 ? undefined : parts.join("\n");
}

function jsDocCommentText(comment: string | ts.NodeArray<ts.JSDocComment> | undefined): string | undefined {
    if (comment === undefined) return undefined;
    const text = typeof comment === "string" ? comment : comment.map((part) => part.text).join("");
    return text.trim() === "" ? undefined : text.trim();
}

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
                const normalizedLine = line.replace(/\/\*\*?|\*\//gu, "");
                const match = directivePattern.exec(normalizedLine);
                if (normalizedLine.includes("@intercall") && match === null) {
                    const position = sourceFile.getLineAndCharacterOfPosition(range.pos + offset + normalizedLine.indexOf("@intercall"));
                    throw new Error(`malformed InterCall directive at ${sourceFile.fileName}:${position.line + 1}:${position.character + 1}`);
                }
                if (match !== null) {
                    const tag = match[1]!;
                    const kind = tag.startsWith("intercall") ? tag.slice("intercall ".length) as TypeScriptDirectiveKind : tag as TypeScriptDirectiveKind;
                    const lineStart = range.pos + offset;
                    const start = lineStart + Math.max(0, line.search(/@/u));
                    const end = lineStart + line.length;
                    const position = sourceFile.getLineAndCharacterOfPosition(start);
                    directives.push({ kind, explicit: tag.startsWith("intercall"), arguments: match[2] ?? "", text: line.trim(), start, end, line: position.line + 1, character: position.character + 1 });
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
