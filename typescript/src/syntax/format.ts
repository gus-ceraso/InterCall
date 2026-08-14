import type {
    Declaration,
    Field,
    InterfaceFile,
    Param,
    TypeExpr,
} from "./ast.js";
import { tokenLiteral, TokenKind } from "./token.js";

export function formatInterface(file: InterfaceFile): string {
    if (file.declarations.length === 0) return "";
    const output: string[] = [];
    file.declarations.forEach((declaration, index) => {
        if (index > 0) output.push("\n");
        writeDeclaration(output, declaration, "");
    });
    return output.join("");
}

type Action =
    | { readonly kind: "type"; readonly type: TypeExpr; readonly indent: string }
    | { readonly kind: "field"; readonly field: Field; readonly indent: string }
    | { readonly kind: "param"; readonly param: Param; readonly indent: string }
    | { readonly kind: "semi" }
    | { readonly kind: "tail"; readonly indent: string };

function runActions(output: string[], actions: Action[]): void {
    while (actions.length > 0) {
        const action = actions.pop()!;
        switch (action.kind) {
            case "type":
                writeType(output, actions, action.type, action.indent);
                break;
            case "field":
                writeDocumentation(output, action.indent, action.field.doc);
                output.push(action.indent, action.field.name.name);
                actions.push(
                    { kind: "semi" },
                    { kind: "type", type: action.field.type, indent: action.indent },
                );
                break;
            case "param":
                writeDocumentation(output, action.indent, action.param.doc);
                output.push(action.indent, action.param.name.name);
                actions.push(
                    { kind: "semi" },
                    { kind: "type", type: action.param.type, indent: action.indent },
                );
                break;
            case "semi":
                output.push(";\n");
                break;
            case "tail":
                output.push(action.indent, "}");
                break;
        }
    }
}

function writeDeclaration(output: string[], declaration: Declaration, indent: string): void {
    writeDocumentation(output, indent, declaration.doc);
    switch (declaration.kind) {
        case "type-decl":
            output.push(indent, "type ", declaration.name.name);
            runActions(output, [
                { kind: "semi" },
                { kind: "type", type: declaration.type, indent },
            ]);
            return;
        case "exception-decl": {
            output.push(indent, "exception ", declaration.name.name);
            const actions: Action[] = [{ kind: "semi" }];
            if (declaration.type !== undefined) {
                actions.push({ kind: "type", type: declaration.type, indent });
            }
            runActions(output, actions);
            return;
        }
        case "procedure-decl":
            output.push(indent, "procedure ", declaration.name.name, " {");
            if (declaration.params.length === 0) {
                output.push("}");
                if (declaration.result === undefined) {
                    output.push(";\n");
                } else {
                    runActions(output, [
                        { kind: "semi" },
                        { kind: "type", type: declaration.result, indent },
                    ]);
                }
                return;
            }

            output.push("\n");
            const actions: Action[] = [{ kind: "semi" }];
            if (declaration.result !== undefined) {
                actions.push({ kind: "type", type: declaration.result, indent });
            }
            actions.push({ kind: "tail", indent });
            for (let i = declaration.params.length - 1; i >= 0; i -= 1) {
                actions.push({
                    kind: "param",
                    param: declaration.params[i]!,
                    indent: `${indent}    `,
                });
            }
            runActions(output, actions);
            return;
    }
}

function writeType(output: string[], actions: Action[], type: TypeExpr, indent: string): void {
    if (type.doc !== "") {
        output.push("\n");
        writeDocumentation(output, indent, type.doc);
        output.push(indent);
    } else {
        output.push(" ");
    }
    writeTypeBody(output, actions, type, indent);
}

function writeTypeBody(output: string[], actions: Action[], type: TypeExpr, indent: string): void {
    switch (type.kind) {
        case "primitive":
            output.push(tokenLiteral(type.primitive) ?? primitiveFallback(type.primitive));
            return;
        case "named":
            output.push(type.name.name);
            return;
        case "list":
            output.push("list");
            actions.push({ kind: "type", type: type.elem, indent });
            return;
        case "record":
            output.push("record {");
            if (type.fields.length === 0) {
                output.push("}");
                return;
            }
            output.push("\n");
            actions.push({ kind: "tail", indent });
            for (let i = type.fields.length - 1; i >= 0; i -= 1) {
                actions.push({ kind: "field", field: type.fields[i]!, indent: `${indent}    ` });
            }
            return;
    }
}

function primitiveFallback(kind: TokenKind): string {
    throw new Error(`syntax formatter: primitive token ${kind} has no literal`);
}

function writeDocumentation(output: string[], indent: string, documentation: string): void {
    if (documentation === "") return;
    if (!documentation.includes("\n")) {
        output.push(indent, "/* ", documentation, " */\n");
        return;
    }
    output.push(indent, "/*\n");
    for (const line of documentation.split("\n")) {
        if (line === "") output.push("\n");
        else output.push(indent, "    ", line, "\n");
    }
    output.push(indent, "*/\n");
}
