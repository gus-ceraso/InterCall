import ts from "typescript";
import type { CompilerProject, SourceOperand } from "./compiler-project.js";
import { scanTypeScriptDirectives, type TypeScriptDirective } from "./directives.js";
import { isCanonicalWireName, typeScriptToWire } from "./name.js";

export interface DiscoveredProcedure {
    readonly sourceName: string;
    readonly wireName: string;
    readonly sourceFile: ts.SourceFile;
    readonly declaration: ts.FunctionDeclaration;
    readonly signature: ts.Type;
}
export interface DiscoveredException {
    readonly sourceName: string;
    readonly wireName: string;
    readonly sourceFile: ts.SourceFile;
    readonly declaration: ts.VariableStatement | ts.ClassDeclaration;
    readonly payloadClass: boolean;
}
export interface DiscoveredType {
    readonly sourceName: string;
    readonly wireName: string;
    readonly sourceFile: ts.SourceFile;
    readonly declaration: ts.TypeAliasDeclaration | ts.InterfaceDeclaration;
}
export interface SourceDiscovery {
    readonly procedures: readonly DiscoveredProcedure[];
    readonly exceptions: readonly DiscoveredException[];
    readonly namedTypes: readonly DiscoveredType[];
}

export interface DiscoveryFilterOptions {
    readonly include?: readonly string[];
    readonly exclude?: readonly string[];
}

export function discoverSourceExports(project: CompilerProject, operands: readonly SourceOperand[], filters: DiscoveryFilterOptions = {}): SourceDiscovery {
    const checker = project.program.getTypeChecker();
    const procedures: DiscoveredProcedure[] = [];
    const exceptions: DiscoveredException[] = [];
    const namedTypes: DiscoveredType[] = [];
    for (const operand of operands) {
        const sourceFile = project.program.getSourceFile(operand.fileName);
        if (sourceFile === undefined) throw new Error(`source file is not in the program: ${operand.fileName}`);
        const directives = scanTypeScriptDirectives(sourceFile);
        for (const declaration of directExportDeclarations(sourceFile)) {
            const directive = directiveFor(declaration, directives);
            if (directive === undefined) continue;
            const sourceName = declarationName(declaration);
            if (sourceName === undefined) continue;
            if (directive.kind === "procedure" && ts.isFunctionDeclaration(declaration)) {
                procedures.push({ sourceName, wireName: resolveWireName(sourceName, directive, "camel"), sourceFile, declaration, signature: checker.getTypeAtLocation(declaration) });
            } else if (directive.kind === "exception" && (ts.isVariableStatement(declaration) || ts.isClassDeclaration(declaration))) {
                exceptions.push({ sourceName, wireName: resolveWireName(sourceName, directive, "pascal"), sourceFile, declaration, payloadClass: ts.isClassDeclaration(declaration) });
            } else if (directive.kind === "type" && (ts.isTypeAliasDeclaration(declaration) || ts.isInterfaceDeclaration(declaration))) {
                namedTypes.push({ sourceName, wireName: resolveWireName(sourceName, directive, "pascal"), sourceFile, declaration });
            }
        }
    }
    const known = [...procedures, ...exceptions, ...namedTypes];
    validateWireNames(known);
    const include = validateFilters(filters.include, known);
    const exclude = new Set(validateFilters(filters.exclude, known));
    const selected = <T extends { readonly sourceName: string; readonly wireName: string }>(records: readonly T[]): T[] => records.filter((record) => (include.length === 0 || include.includes(record.sourceName) || include.includes(record.wireName)) && !exclude.has(record.sourceName) && !exclude.has(record.wireName));
    return { procedures: selected(procedures), exceptions: selected(exceptions), namedTypes: selected(namedTypes) };
}

function validateWireNames(known: readonly { readonly sourceName: string; readonly wireName: string }[]): void {
    const fixed = new Set(["call", "connection", "handler", "metadata", "payload"]);
    const seen = new Map<string, string>();
    for (const record of known) {
        if (fixed.has(record.wireName)) throw new Error(`wire name ${JSON.stringify(record.wireName)} is reserved by the runtime`);
        const previous = seen.get(record.wireName);
        if (previous !== undefined) throw new Error(`duplicate wire name ${JSON.stringify(record.wireName)} for ${previous} and ${record.sourceName}`);
        seen.set(record.wireName, record.sourceName);
    }
}

function validateFilters(filters: readonly string[] | undefined, known: readonly { readonly sourceName: string; readonly wireName: string }[]): string[] {
    if (filters === undefined) return [];
    const seen = new Set<string>();
    const names = new Set(known.flatMap((record) => [record.sourceName, record.wireName]));
    for (const filter of filters) {
        if (typeof filter !== "string" || filter.trim() === "") throw new Error("source selector must be a non-empty string");
        if (seen.has(filter)) throw new Error(`duplicate source selector ${JSON.stringify(filter)}`);
        if (!names.has(filter)) throw new Error(`unknown or non-explicit source selector ${JSON.stringify(filter)}`);
        seen.add(filter);
    }
    return [...seen];
}

function directExportDeclarations(sourceFile: ts.SourceFile): ts.Node[] {
    const result: ts.Node[] = [];
    for (const statement of sourceFile.statements) {
        const modifiers = ts.canHaveModifiers(statement) ? ts.getModifiers(statement) : undefined;
        if (!modifiers?.some((modifier) => modifier.kind === ts.SyntaxKind.ExportKeyword)) continue;
        if (ts.isVariableStatement(statement)) result.push(statement);
        else if (ts.isFunctionDeclaration(statement) || ts.isClassDeclaration(statement) || ts.isInterfaceDeclaration(statement) || ts.isTypeAliasDeclaration(statement)) result.push(statement);
    }
    return result;
}

function declarationName(declaration: ts.Node): string | undefined {
    if (ts.isVariableStatement(declaration)) return declaration.declarationList.declarations[0]?.name.getText();
    if (ts.isFunctionDeclaration(declaration) || ts.isClassDeclaration(declaration) || ts.isInterfaceDeclaration(declaration) || ts.isTypeAliasDeclaration(declaration)) return declaration.name?.getText();
    return undefined;
}

function directiveFor(node: ts.Node, directives: readonly TypeScriptDirective[]): TypeScriptDirective | undefined {
    return directives
        .filter((directive) => directive.start >= node.getFullStart() && directive.end <= node.getStart())
        .at(-1);
}

function resolveWireName(sourceName: string, directive: TypeScriptDirective, nameCase: "camel" | "pascal"): string {
    const value = directive.arguments.trim();
    const wireName = value === "" ? typeScriptToWire(sourceName, nameCase) : value;
    if (!isCanonicalWireName(wireName)) throw new Error(`invalid wire name ${JSON.stringify(wireName)} at ${directive.line}:${directive.character}`);
    return wireName;
}
