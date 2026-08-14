import ts from "typescript";
import type { CompilerProject, SourceOperand } from "./compiler-project.js";
import { scanTypeScriptDirectives, sourceDocumentation, type TypeScriptDirective } from "./directives.js";
import { isValidWireName, typeScriptToWire } from "./name.js";
import { decodeGeneratedInterface, hasGeneratedTypeScriptMetadata, readGeneratedMetadata, validateMetadataRows } from "./metadata-reader.js";

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
    readonly metadataDeclaration?: import("../syntax/index.js").TypeDecl;
}
export interface SourceDiscovery {
    readonly procedures: readonly DiscoveredProcedure[];
    readonly exceptions: readonly DiscoveredException[];
    readonly namedTypes: readonly DiscoveredType[];
    readonly parameterWireNames: ReadonlyMap<ts.ParameterDeclaration, string>;
    readonly fieldWireNames: ReadonlyMap<ts.Node, string>;
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
    const parameterWireNames = new Map<ts.ParameterDeclaration, string>();
    const fieldWireNames = new Map<ts.Node, string>();
    const memberDirectiveFiles = new Set<string>();
    const directiveFiles = new Map<string, readonly TypeScriptDirective[]>();
    const consumedProviderDirectives = new Set<string>();
    const metadataCache = new Map<string, ReadonlyMap<string, { readonly wireName: string; readonly documentation: string; readonly declaration: import("../syntax/index.js").TypeDecl }>>();
    for (const operand of operands) {
        const sourceFile = project.program.getSourceFile(operand.fileName);
        if (sourceFile === undefined) throw new Error(`source file is not in the program: ${operand.fileName}`);
        const directives = scanTypeScriptDirectives(sourceFile);
        directiveFiles.set(sourceFile.fileName, directives);
        if (!memberDirectiveFiles.has(sourceFile.fileName)) {
            collectMemberWireNames(sourceFile, directives, parameterWireNames, fieldWireNames);
            memberDirectiveFiles.add(sourceFile.fileName);
        }
        for (const declaration of directExportDeclarations(sourceFile, checker)) {
            const declarationFile = declaration.getSourceFile();
            const metadataRows = metadataRowsFor(declarationFile, metadataCache);
            const declarationDirectives = declarationFile === sourceFile ? directives : scanTypeScriptDirectives(declarationFile);
            directiveFiles.set(declarationFile.fileName, declarationDirectives);
            if (declarationFile !== sourceFile && !memberDirectiveFiles.has(declarationFile.fileName)) {
                collectMemberWireNames(declarationFile, declarationDirectives, parameterWireNames, fieldWireNames);
                memberDirectiveFiles.add(declarationFile.fileName);
            }
            const directive = directiveFor(declaration, declarationDirectives);
            if (directive !== undefined && (directive.kind === "procedure" || directive.kind === "exception" || directive.kind === "type")) consumedProviderDirectives.add(directiveKey(declarationFile, directive));
            if (directive === undefined) {
                const sourceName = declarationName(declaration);
                const row = sourceName === undefined ? undefined : metadataRows.get(sourceName);
                if (sourceName !== undefined && (ts.isTypeAliasDeclaration(declaration) || ts.isInterfaceDeclaration(declaration))) {
                    if (hasGeneratedTypeScriptMetadata(declarationFile.getFullText()) && row === undefined) throw new Error(`generated metadata has no row for exported type ${sourceName}`);
                    if (row !== undefined) namedTypes.push({ sourceName, wireName: row.wireName, sourceFile: declarationFile, declaration, documentation: row.documentation, metadataDeclaration: row.declaration });
                }
                continue;
            }
            const sourceName = declarationName(declaration);
            if (sourceName === undefined) continue;
            if (directive.kind === "procedure" && !ts.isFunctionDeclaration(declaration) || directive.kind === "exception" && !ts.isVariableStatement(declaration) && !ts.isClassDeclaration(declaration) || directive.kind === "type" && !ts.isTypeAliasDeclaration(declaration) && !ts.isInterfaceDeclaration(declaration)) {
                throw new Error(`directive ${directive.kind} is misplaced on ${sourceName}`);
            }
            if (directive.kind === "procedure" && ts.isFunctionDeclaration(declaration)) {
                procedures.push({ sourceName, wireName: resolveWireName(sourceName, directive, "camel"), sourceFile: declarationFile, declaration, signature: checker.getTypeAtLocation(declaration) });
            } else if (directive.kind === "exception" && (ts.isVariableStatement(declaration) || ts.isClassDeclaration(declaration))) {
                exceptions.push({ sourceName, wireName: resolveWireName(sourceName, directive, "pascal"), sourceFile: declarationFile, declaration, payloadClass: ts.isClassDeclaration(declaration) });
            } else if (directive.kind === "type" && (ts.isTypeAliasDeclaration(declaration) || ts.isInterfaceDeclaration(declaration))) {
                const documentation = sourceDocumentation(declaration);
                namedTypes.push(documentation === undefined ? { sourceName, wireName: resolveWireName(sourceName, directive, "pascal"), sourceFile: declarationFile, declaration } : { sourceName, wireName: resolveWireName(sourceName, directive, "pascal"), sourceFile: declarationFile, declaration, documentation });
            }
        }
    }
    for (const [fileName, directives] of directiveFiles) {
        for (const directive of directives) {
            if ((directive.kind === "procedure" || directive.kind === "exception" || directive.kind === "type") && !consumedProviderDirectives.has(directiveKey(fileName, directive))) throw new Error(`misplaced InterCall ${directive.kind} at ${fileName}:${directive.line}:${directive.character}`);
        }
    }
    const knownGeneratedTypes = new Set(namedTypes.map((type) => `${type.sourceFile.fileName}\u0000${type.sourceName}`));
    for (const sourceFile of project.program.getSourceFiles().filter((file) => hasGeneratedTypeScriptMetadata(file.getFullText())).sort((left, right) => left.fileName.localeCompare(right.fileName))) {
        for (const [nativeName, row] of metadataRowsFor(sourceFile, metadataCache)) {
            const declaration = sourceFile.statements.find((statement) => (ts.isTypeAliasDeclaration(statement) || ts.isInterfaceDeclaration(statement)) && statement.name?.text === nativeName);
            if (declaration === undefined || (!ts.isTypeAliasDeclaration(declaration) && !ts.isInterfaceDeclaration(declaration))) throw new Error(`generated metadata has no TypeScript declaration for ${nativeName}`);
            const key = `${sourceFile.fileName}\u0000${nativeName}`;
            if (knownGeneratedTypes.has(key)) continue;
            knownGeneratedTypes.add(key);
            namedTypes.push({ sourceName: nativeName, wireName: row.wireName, sourceFile, declaration, documentation: row.documentation, metadataDeclaration: row.declaration });
        }
    }
    const known = [...procedures, ...exceptions, ...namedTypes];
    validateWireNames(known);
    const include = validateFilters(filters.include, procedures);
    const exclude = new Set(validateFilters(filters.exclude, procedures));
    const selectedProcedures = procedures.filter((record) => (include.length === 0 || include.includes(record.sourceName) || include.includes(record.wireName)) && !exclude.has(record.sourceName) && !exclude.has(record.wireName));
    return { procedures: selectedProcedures, exceptions, namedTypes, parameterWireNames, fieldWireNames };
}

function collectMemberWireNames(
    sourceFile: ts.SourceFile,
    directives: readonly TypeScriptDirective[],
    parameterNames: Map<ts.ParameterDeclaration, string>,
    fieldNames: Map<ts.Node, string>,
): void {
    const consumed = new Set<TypeScriptDirective>();
    const directiveFor = (node: ts.Node, kind: TypeScriptDirective["kind"]): TypeScriptDirective | undefined => {
        const matches = directives.filter((directive) => directive.explicit && directive.kind === kind && directive.start >= node.getFullStart() && directive.end <= node.getStart());
        if (matches.length > 1) throw new Error(`multiple InterCall ${kind} directives precede ${node.getText()}`);
        if (matches[0] !== undefined) consumed.add(matches[0]);
        return matches[0];
    };
    const visit = (node: ts.Node): void => {
        if (ts.isFunctionDeclaration(node)) {
            const functionDirectives = directives.filter((directive) => directive.explicit && directive.kind === "param" && directive.start >= node.getFullStart() && directive.end <= node.getStart());
            for (const directive of functionDirectives) {
                consumed.add(directive);
                const parts = directive.arguments.trim().split(/\s+/u);
                if (parts.length !== 2 || !parts[0] || !parts[1]) throw new Error(`malformed @intercall param at ${sourceFile.fileName}:${directive.line}:${directive.character}`);
                const parameter = node.parameters.find((candidate) => candidate.name.getText() === parts[0]);
                if (parameter === undefined) throw new Error(`@intercall param names unknown parameter ${JSON.stringify(parts[0])}`);
                if (!isValidWireName(parts[1])) throw new Error(`invalid wire name ${JSON.stringify(parts[1])} at ${directive.line}:${directive.character}`);
                if (parameterNames.has(parameter)) throw new Error(`duplicate @intercall param for ${parts[0]}`);
                parameterNames.set(parameter, parts[1]);
            }
        }
        if (ts.isPropertySignature(node) || ts.isPropertyDeclaration(node)) {
            const directive = directiveFor(node, "field");
            if (directive !== undefined) {
                const wireName = directive.arguments.trim();
                if (!isValidWireName(wireName)) throw new Error(`invalid wire name ${JSON.stringify(wireName)} at ${directive.line}:${directive.character}`);
                fieldNames.set(node, wireName);
            }
        }
        ts.forEachChild(node, visit);
    };
    visit(sourceFile);
    for (const directive of directives) {
        if (directive.explicit && (directive.kind === "param" || directive.kind === "field") && !consumed.has(directive)) throw new Error(`misplaced @intercall ${directive.kind} at ${sourceFile.fileName}:${directive.line}:${directive.character}`);
    }
}

function metadataRowsFor(
    sourceFile: ts.SourceFile,
    cache: Map<string, ReadonlyMap<string, { readonly wireName: string; readonly documentation: string; readonly declaration: import("../syntax/index.js").TypeDecl }>>,
): ReadonlyMap<string, { readonly wireName: string; readonly documentation: string; readonly declaration: import("../syntax/index.js").TypeDecl }> {
    const cached = cache.get(sourceFile.fileName);
    if (cached !== undefined) return cached;
    if (!hasGeneratedTypeScriptMetadata(sourceFile.getFullText())) {
        const empty = new Map<string, { readonly wireName: string; readonly documentation: string; readonly declaration: import("../syntax/index.js").TypeDecl }>();
        cache.set(sourceFile.fileName, empty);
        return empty;
    }
    const metadata = readGeneratedMetadata(sourceFile.getFullText());
    const semanticFile = decodeGeneratedInterface(metadata);
    validateMetadataRows(semanticFile, metadata.machineTypes);
    const documentation = new Map<string, string>();
    for (const declaration of semanticFile.declarations) documentation.set(declaration.name.name, declaration.doc ?? "");
    const declarations = new Map(semanticFile.declarations.filter((declaration) => declaration.kind === "type-decl").map((declaration) => [declaration.name.name, declaration]));
    const rows = new Map(metadata.machineTypes.map((row) => [row.nativeName, { wireName: row.wireName, documentation: documentation.get(row.wireName) ?? "", declaration: declarations.get(row.wireName)! }]));
    cache.set(sourceFile.fileName, rows);
    return rows;
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

function directExportDeclarations(sourceFile: ts.SourceFile, checker: ts.TypeChecker): ts.Node[] {
    const result: ts.Node[] = [];
    for (const statement of sourceFile.statements) {
        if (ts.isExportDeclaration(statement)) {
            if (statement.exportClause !== undefined && ts.isNamedExports(statement.exportClause)) {
                for (const element of statement.exportClause.elements) {
                    const symbol = checker.getSymbolAtLocation(element.name);
                    const target = symbol === undefined ? undefined : exportTarget(checker, symbol);
                    if (target !== undefined) {
                        rejectReexportedProvider(target);
                        result.push(target);
                    }
                }
            } else if (statement.exportClause === undefined && statement.moduleSpecifier !== undefined) {
                const moduleSymbol = checker.getSymbolAtLocation(statement.moduleSpecifier);
                for (const symbol of moduleSymbol === undefined ? [] : checker.getExportsOfModule(moduleSymbol)) {
                    const target = exportTarget(checker, symbol);
                    if (target !== undefined) {
                        rejectReexportedProvider(target);
                        result.push(target);
                    }
                }
            }
            continue;
        }
        const modifiers = ts.canHaveModifiers(statement) ? ts.getModifiers(statement) : undefined;
        if (!modifiers?.some((modifier) => modifier.kind === ts.SyntaxKind.ExportKeyword)) continue;
        if (modifiers.some((modifier) => modifier.kind === ts.SyntaxKind.DefaultKeyword)) {
            if (ts.isFunctionDeclaration(statement) || ts.isClassDeclaration(statement)) {
                const directive = directiveFor(statement, scanTypeScriptDirectives(sourceFile));
                if (directive !== undefined) throw new Error(`default export ${declarationName(statement) ?? "<anonymous>"} is unsupported`);
            }
            continue;
        }
        if (ts.isVariableStatement(statement)) result.push(statement);
        else if (ts.isFunctionDeclaration(statement) || ts.isClassDeclaration(statement) || ts.isInterfaceDeclaration(statement) || ts.isTypeAliasDeclaration(statement)) result.push(statement);

    }
    return result;
}

function rejectReexportedProvider(target: ts.Node): void {
    if (!ts.isFunctionDeclaration(target) && !ts.isVariableStatement(target) && !ts.isClassDeclaration(target)) return;
    const directives = scanTypeScriptDirectives(target.getSourceFile());
    if (directiveFor(target, directives) !== undefined) throw new Error(`re-exported provider ${declarationName(target) ?? "<anonymous>"} is unsupported`);
}

function exportTarget(checker: ts.TypeChecker, symbol: ts.Symbol): ts.Node | undefined {
    const resolved = symbol.flags & ts.SymbolFlags.Alias ? checker.getAliasedSymbol(symbol) : symbol;
    return resolved.declarations?.find((declaration) => ts.isVariableStatement(declaration) || ts.isFunctionDeclaration(declaration) || ts.isClassDeclaration(declaration) || ts.isInterfaceDeclaration(declaration) || ts.isTypeAliasDeclaration(declaration));
}

function declarationName(declaration: ts.Node): string | undefined {
    if (ts.isVariableStatement(declaration)) return declaration.declarationList.declarations[0]?.name.getText();
    if (ts.isFunctionDeclaration(declaration) || ts.isClassDeclaration(declaration) || ts.isInterfaceDeclaration(declaration) || ts.isTypeAliasDeclaration(declaration)) return declaration.name?.getText();
    return undefined;
}

function directiveFor(node: ts.Node, directives: readonly TypeScriptDirective[]): TypeScriptDirective | undefined {
    const matches = directives.filter((directive) => directive.explicit && (directive.kind === "procedure" || directive.kind === "exception" || directive.kind === "type") && directive.start >= node.getFullStart() && directive.end <= node.getStart());
    if (matches.length > 1) throw new Error(`contradictory or duplicate InterCall directives precede ${declarationName(node) ?? "declaration"}`);
    return matches[0];
}

function directiveKey(sourceFile: ts.SourceFile | string, directive: TypeScriptDirective): string {
    return `${typeof sourceFile === "string" ? sourceFile : sourceFile.fileName}:${directive.start}`;
}

function resolveWireName(sourceName: string, directive: TypeScriptDirective, nameCase: "camel" | "pascal"): string {
    const value = directive.arguments.trim();
    const wireName = value === "" ? typeScriptToWire(sourceName, nameCase) : value;
    if (!isValidWireName(wireName)) throw new Error(`invalid wire name ${JSON.stringify(wireName)} at ${directive.line}:${directive.character}`);
    return wireName;
}
