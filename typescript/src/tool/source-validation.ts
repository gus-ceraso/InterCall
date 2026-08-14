import ts from "typescript";
import type { CompilerProject } from "./compiler-project.js";
import type { DiscoveredException, DiscoveredProcedure } from "./source-discovery.js";

export function validateDiscoveredProcedure(project: CompilerProject, procedure: DiscoveredProcedure): void {
    const checker = project.program.getTypeChecker();
    const declaration = procedure.declaration;
    if (declaration.parameters.length === 0) throw new Error(`procedure ${procedure.sourceName} must receive HandlerContext first`);
    const contextType = checker.getTypeAtLocation(declaration.parameters[0]!);
    if (!hasTypeName(contextType, "HandlerContext")) throw new Error(`procedure ${procedure.sourceName} first parameter must be HandlerContext`);
    const signature = checker.getSignatureFromDeclaration(declaration);
    if (signature === undefined || !hasTypeName(checker.getReturnTypeOfSignature(signature), "Promise")) {
        throw new Error(`procedure ${procedure.sourceName} must return Promise`);
    }
}

export function validateDiscoveredException(project: CompilerProject, exception: DiscoveredException): void {
    if (!exception.payloadClass) return;
    const declaration = exception.declaration;
    if (!ts.isClassDeclaration(declaration)) throw new Error("payload exception must be a class");
    const heritage = declaration.heritageClauses?.flatMap((clause) => clause.types) ?? [];
    const checker = project.program.getTypeChecker();
    if (!heritage.some((type) => hasTypeName(checker.getTypeAtLocation(type.expression), "PayloadException"))) {
        throw new Error(`exception ${exception.sourceName} must extend PayloadException`);
    }
}

function hasTypeName(type: ts.Type, name: string): boolean {
    return type.symbol?.name === name || type.aliasSymbol?.name === name;
}
