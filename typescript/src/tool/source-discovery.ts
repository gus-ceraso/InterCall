import ts from "typescript";
import type { CompilerProject, SourceOperand } from "./compiler-project.js";
import { scanTypeScriptDirectives, sourceDocumentation, type TypeScriptDirective } from "./directives.js";
import { isCanonicalWireName, typeScriptToWire } from "./name.js";
import { decodeGeneratedInterface, hasGeneratedTypeScriptMarker, readGeneratedMetadata, validateMetadataRows } from "./metadata-reader.js";

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
    readonly documentation?: string;
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
        const sourceText = sourceFile.getFullText();
        let metadataRows: ReadonlyMap<string, { readonly nativeName: string; readonly documentation: string }> = new Map();
        if (hasGeneratedTypeScriptMarker(sourceText)) {
            const metadata = readGeneratedMetadata(sourceText);
            const semanticFile = decodeGeneratedInterface(metadata);
            validateMetadataRows(semanticFile, metadata.machineTypes);
            const documentation = new Map<string, string>();
            for (const declaration of semanticFile.declarations) documentation.set(declaration.name.name, declaration.doc ?? "");
            metadataRows = new Map(metadata.machineTypes.map((row) => [row.nativeName, { nativeName: row.nativeName, documentation: documentation.get(row.wireName) ?? "" }]));
        }
        const directives = scanTypeScriptDirectives(sourceFile);
        for (const declaration of directExportDeclarations(sourceFile)) {
            const directive = directiveFor(declaration, directives);
            if (directive === undefined) {
                const sourceName = declarationName(declaration);
                const row = sourceName === undefined ? undefined : metadataRows.get(sourceName);
                if (sourceName !== undefined && row !== undefined && (ts.isTypeAliasDeclaration(declaration) || ts.isInterfaceDeclaration(declaration))) namedTypes.push({ sourceName, wireName: row.nativeName, sourceFile, declaration, documentation: row.documentation });
                continue;
            }
            const sourceName = declarationName(declaration);
            if (sourceName === undefined) continue;
            if (directive.kind === "procedure" && !ts.isFunctionDeclaration(declaration) || directive.kind === "exception" && !ts.isVariableStatement(declaration) && !ts.isClassDeclaration(declaration) || directive.kind === "type" && !ts.isTypeAliasDeclaration(declaration) && !ts.isInterfaceDeclaration(declaration)) {
                throw new Error(`directive ${directive.kind} is misplaced on ${sourceName}`);
            }
            if (directive.kind === "procedure" && ts.isFunctionDeclaration(declaration)) {
                procedures.push({ sourceName, wireName: resolveWireName(sourceName, directive, "camel"), sourceFile, declaration, signature: checker.getTypeAtLocation(declaration) });
            } else if (directive.kind === "exception" && (ts.isVariableStatement(declaration) || ts.isClassDeclaration(declaration))) {
                exceptions.push({ sourceName, wireName: resolveWireName(sourceName, directive, "pascal"), sourceFile, declaration, payloadClass: ts.isClassDeclaration(declaration) });
            } else if (directive.kind === "type" && (ts.isTypeAliasDeclaration(declaration) || ts.isInterfaceDeclaration(declaration))) {
                const documentation = sourceDocumentation(declaration);
                namedTypes.push(documentation === undefined ? { sourceName, wireName: resolveWireName(sourceName, directive, "pascal"), sourceFile, declaration } : { sourceName, wireName: resolveWireName(sourceName, directive, "pascal"), sourceFile, declaration, documentation });
            }
        }
    }
    const known = [...procedures, ...exceptions, ...namedTypes];
    validateWireNames(known);
    const include = validateFilters(filters.include, procedures);
    const exclude = new Set(validateFilters(filters.exclude, procedures));
    const selectedProcedures = procedures.filter((record) => (include.length === 0 || include.includes(record.sourceName) || include.includes(record.wireName)) && !exclude.has(record.sourceName) && !exclude.has(record.wireName));
    return { procedures: selectedProcedures, exceptions, namedTypes };
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
    const matches = directives.filter((directive) => (directive.kind === "procedure" || directive.kind === "exception" || directive.kind === "type") && directive.start >= node.getFullStart() && directive.end <= node.getStart());
    const last = matches.at(-1);
    if (last !== undefined && matches.filter((directive) => directive.kind === last.kind).length > 1) throw new Error(`multiple InterCall directives precede ${declarationName(node) ?? "declaration"}`);
    return last;
}

function resolveWireName(sourceName: string, directive: TypeScriptDirective, nameCase: "camel" | "pascal"): string {
    const value = directive.arguments.trim();
    const wireName = value === "" ? typeScriptToWire(sourceName, nameCase) : value;
    if (!isCanonicalWireName(wireName)) throw new Error(`invalid wire name ${JSON.stringify(wireName)} at ${directive.line}:${directive.character}`);
    return wireName;
}
