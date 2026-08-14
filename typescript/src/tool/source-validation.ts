import ts from "typescript";
import type { CompilerProject } from "./compiler-project.js";
import type { DiscoveredException, DiscoveredProcedure } from "./source-discovery.js";
import { walkReachableType } from "./type-graph.js";

const markerNames = new Set(["Int8", "Int16", "Int32", "Int64", "Uint8", "Uint16", "Uint32", "Uint64", "Float32", "Float64", "EmptyRecord"]);

export function validateDiscoveredProcedure(project: CompilerProject, procedure: DiscoveredProcedure): void {
    const checker = project.program.getTypeChecker();
    const declaration = procedure.declaration;
    if (declaration.parameters.length === 0) throw new Error(`procedure ${procedure.sourceName} must receive HandlerContext first`);
    if (declaration.parameters.some((parameter) => parameter.questionToken !== undefined || parameter.dotDotDotToken !== undefined)) throw new Error(`procedure ${procedure.sourceName} cannot have optional or rest parameters`);
    if (declaration.parameters.slice(1).some((parameter) => !ts.isIdentifier(parameter.name))) throw new Error(`procedure ${procedure.sourceName} parameters must have simple names`);
    const contextType = checker.getTypeAtLocation(declaration.parameters[0]!);
    if (!hasExactImportedType(project, declaration.getSourceFile(), contextType, "HandlerContext")) throw new Error(`procedure ${procedure.sourceName} first parameter must be HandlerContext`);
    const signature = checker.getSignatureFromDeclaration(declaration);
    const returnType = signature === undefined ? undefined : checker.getReturnTypeOfSignature(signature);
    const globalPromise = checker.resolveName("Promise", declaration, ts.SymbolFlags.Type, false);
    if (returnType === undefined || returnType.symbol?.name !== "Promise" || globalPromise === undefined || returnType.symbol !== globalPromise) throw new Error(`procedure ${procedure.sourceName} must return Promise`);
    const resultTypes = checker.getTypeArguments(returnType as ts.TypeReference);
    if (resultTypes.length !== 1) throw new Error(`procedure ${procedure.sourceName} must return Promise<T>`);
    for (const parameter of declaration.parameters.slice(1)) walkReachableType(project, parameter.type!);
    const resultNode = declaration.type !== undefined && ts.isTypeReferenceNode(declaration.type) ? declaration.type.typeArguments?.[0] : undefined;
    if (resultNode !== undefined && (resultTypes[0]!.flags & ts.TypeFlags.Void) === 0) walkReachableType(project, resultNode);
}

export function validateDiscoveredException(project: CompilerProject, exception: DiscoveredException): void {
    if (!exception.payloadClass) return;
    const declaration = exception.declaration;
    if (!ts.isClassDeclaration(declaration)) throw new Error("payload exception must be a class");
    const heritage = declaration.heritageClauses?.flatMap((clause) => clause.types) ?? [];
    const checker = project.program.getTypeChecker();
    if (!heritage.some((type) => hasExactImportedType(project, declaration.getSourceFile(), checker.getTypeAtLocation(type.expression), "PayloadException"))) {
        throw new Error(`exception ${exception.sourceName} must extend PayloadException`);
    }
    const payload = heritage.find((type) => type.typeArguments?.length === 1)?.typeArguments?.[0];
    if (payload !== undefined) walkReachableType(project, payload);
}

export function isExactRuntimeMarker(project: CompilerProject, sourceFile: ts.SourceFile, type: ts.Type, name: string, node?: ts.Node): boolean {
    return markerNames.has(name) && hasExactImportedType(project, sourceFile, type, name, node);
}

function hasExactImportedType(project: CompilerProject, sourceFile: ts.SourceFile, type: ts.Type, name: string, node?: ts.Node): boolean {
    const checker = project.program.getTypeChecker();
    const expected = importedSymbol(checker, sourceFile, name);
    if (expected === undefined || !isRuntimeImport(project, sourceFile, name)) return false;
    const nodeSymbol = node === undefined ? undefined : checker.getSymbolAtLocation(ts.isTypeReferenceNode(node) ? node.typeName : node);
    const candidates = [type.aliasSymbol, type.symbol, nodeSymbol].filter((symbol): symbol is ts.Symbol => symbol !== undefined);
    return candidates.some((candidate) => sameSymbol(checker, candidate, expected));
}

function isRuntimeImport(project: CompilerProject, sourceFile: ts.SourceFile, name: string): boolean {
    for (const statement of sourceFile.statements) {
        if (!ts.isImportDeclaration(statement) || !ts.isStringLiteral(statement.moduleSpecifier) || statement.importClause?.namedBindings === undefined || !ts.isNamedImports(statement.importClause.namedBindings)) continue;
        if (!statement.importClause.namedBindings.elements.some((element) => element.name.text === name || element.propertyName?.text === name)) continue;
        if (statement.moduleSpecifier.text === "@cerasos/intercall") return true;
        const resolved = ts.resolveModuleName(statement.moduleSpecifier.text, sourceFile.fileName, project.options, ts.sys).resolvedModule;
        if (resolved?.resolvedFileName.endsWith("/src/index.ts") === true) return true;
    }
    return false;
}

function importedSymbol(checker: ts.TypeChecker, sourceFile: ts.SourceFile, name: string): ts.Symbol | undefined {
    for (const statement of sourceFile.statements) {
        if (!ts.isImportDeclaration(statement) || statement.importClause?.namedBindings === undefined || !ts.isNamedImports(statement.importClause.namedBindings)) continue;
        for (const element of statement.importClause.namedBindings.elements) {
            if (element.name.text !== name && element.propertyName?.text !== name) continue;
            const symbol = checker.getSymbolAtLocation(element.name);
            if (symbol === undefined) continue;
            return symbol.flags & ts.SymbolFlags.Alias ? checker.getAliasedSymbol(symbol) : symbol;
        }
    }
    return undefined;
}

function sameSymbol(checker: ts.TypeChecker, left: ts.Symbol, right: ts.Symbol): boolean {
    const resolvedLeft = left.flags & ts.SymbolFlags.Alias ? checker.getAliasedSymbol(left) : left;
    const resolvedRight = right.flags & ts.SymbolFlags.Alias ? checker.getAliasedSymbol(right) : right;
    return resolvedLeft === resolvedRight;
}
