import ts from "typescript";
import type { CompilerProject } from "./compiler-project.js";
import type { DiscoveredException, DiscoveredProcedure } from "./source-discovery.js";

const markerNames = new Set(["Int8", "Int16", "Int32", "Int64", "Uint8", "Uint16", "Uint32", "Uint64", "Float32", "Float64", "EmptyRecord"]);

export function validateDiscoveredProcedure(project: CompilerProject, procedure: DiscoveredProcedure): void {
    const checker = project.program.getTypeChecker();
    const declaration = procedure.declaration;
    if (declaration.parameters.length === 0) throw new Error(`procedure ${procedure.sourceName} must receive HandlerContext first`);
    const contextType = checker.getTypeAtLocation(declaration.parameters[0]!);
    if (!hasExactImportedType(project, declaration.getSourceFile(), contextType, "HandlerContext")) throw new Error(`procedure ${procedure.sourceName} first parameter must be HandlerContext`);
    const signature = checker.getSignatureFromDeclaration(declaration);
    const returnType = signature === undefined ? undefined : checker.getReturnTypeOfSignature(signature);
    if (returnType === undefined || returnType.symbol?.name !== "Promise") throw new Error(`procedure ${procedure.sourceName} must return Promise`);
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
}

export function isExactRuntimeMarker(project: CompilerProject, sourceFile: ts.SourceFile, type: ts.Type, name: string, node?: ts.Node): boolean {
    return markerNames.has(name) && hasExactImportedType(project, sourceFile, type, name, node);
}

function hasExactImportedType(project: CompilerProject, sourceFile: ts.SourceFile, type: ts.Type, name: string, node?: ts.Node): boolean {
    const checker = project.program.getTypeChecker();
    const expected = importedSymbol(checker, sourceFile, name);
    if (expected === undefined) return false;
    const nodeSymbol = node === undefined ? undefined : checker.getSymbolAtLocation(ts.isTypeReferenceNode(node) ? node.typeName : node);
    const candidates = [type.aliasSymbol, type.symbol, nodeSymbol].filter((symbol): symbol is ts.Symbol => symbol !== undefined);
    return candidates.some((candidate) => sameSymbol(checker, candidate, expected));
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
