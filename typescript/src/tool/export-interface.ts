import ts from "typescript";
import { parseInterface, validateInterface, type InterfaceFile } from "../syntax/index.js";
import { TokenKind } from "../syntax/token.js";
import type { CompilerProject } from "./compiler-project.js";
import type { DiscoveredException, DiscoveredProcedure, DiscoveredType, SourceDiscovery } from "./source-discovery.js";
import { orderDiscoveredExports } from "./source-order.js";

const fixedExceptions = ["internal_exception", "invalid_arguments", "procedure_not_found"] as const;

export interface ExportInterfaceResult {
    readonly source: InterfaceFile;
    readonly canonicalText: string;
}

export function buildExportInterface(project: CompilerProject, discovery: SourceDiscovery): ExportInterfaceResult {
    const ordered = orderDiscoveredExports(discovery);
    const checker = project.program.getTypeChecker();
    const lines: string[] = fixedExceptions.map((name) => `exception ${name};`);
    for (const type of ordered.namedTypes) lines.push(`type ${type.wireName} ${typeText(checker, checker.getTypeAtLocation(type.declaration), new Set())};`);
    for (const exception of ordered.exceptions) {
        const payload = exceptionPayloadType(checker, exception);
        lines.push(payload === undefined ? `exception ${exception.wireName};` : `exception ${exception.wireName} ${typeText(checker, payload, new Set())};`);
    }
    for (const procedure of ordered.procedures) lines.push(procedureText(checker, procedure));
    const canonicalText = lines.join("\n") + "\n";
    const source = parseInterface("generated-export.intercall", new TextEncoder().encode(canonicalText));
    validateInterface(source);
    return { source, canonicalText };
}

function procedureText(checker: ts.TypeChecker, procedure: DiscoveredProcedure): string {
    const params = procedure.declaration.parameters.slice(1).map((parameter) => `${parameter.name.getText()} ${typeText(checker, checker.getTypeAtLocation(parameter), new Set())};`).join(" ");
    const signature = checker.getSignatureFromDeclaration(procedure.declaration);
    const result = signature === undefined ? undefined : checker.getReturnTypeOfSignature(signature);
    const resultType = result === undefined ? undefined : checker.getTypeArguments(result as ts.TypeReference)[0];
    return `procedure ${procedure.wireName} {${params}}${resultType === undefined || (resultType.flags & ts.TypeFlags.Void) !== 0 ? "" : ` ${typeText(checker, resultType, new Set())}`} ;`.replace("} ;", "};");
}

function exceptionPayloadType(checker: ts.TypeChecker, exception: DiscoveredException): ts.Type | undefined {
    if (!ts.isClassDeclaration(exception.declaration)) return undefined;
    const payload = exception.declaration.members.find((member) => ts.isPropertyDeclaration(member) && member.name?.getText() === "payload");
    return payload === undefined ? undefined : checker.getTypeAtLocation(payload);
}

function typeText(checker: ts.TypeChecker, type: ts.Type, active: Set<string>): string {
    if ((type.flags & ts.TypeFlags.StringLike) !== 0) return "string";
    if ((type.flags & (ts.TypeFlags.NumberLike | ts.TypeFlags.BigIntLike)) !== 0) return "int32";
    if ((type.flags & ts.TypeFlags.BooleanLike) !== 0) return "uint8";
    if (checker.isArrayType(type) || checker.isTupleType(type)) {
        const element = checker.getTypeArguments(type as ts.TypeReference)[0];
        return `list ${element === undefined ? "uint8" : typeText(checker, element, active)}`;
    }
    const symbol = type.aliasSymbol ?? type.symbol;
    if (symbol !== undefined && symbol.name !== "__type") {
        if (active.has(symbol.name)) return symbol.name;
        if (symbol.declarations?.some((declaration) => ts.isInterfaceDeclaration(declaration) || ts.isTypeAliasDeclaration(declaration))) {
            active.add(symbol.name);
            const properties = type.getProperties().map((property) => `${property.name} ${typeText(checker, checker.getTypeOfSymbolAtLocation(property, property.valueDeclaration ?? property.declarations?.[0]!), active)};`).join(" ");
            active.delete(symbol.name);
            return `record {${properties}}`;
        }
    }
    if (type.getProperties().length > 0) return `record {${type.getProperties().map((property) => `${property.name} ${typeText(checker, checker.getTypeOfSymbolAtLocation(property, property.valueDeclaration ?? property.declarations?.[0]!), active)};`).join(" ")}}`;
    return "uint8";
}
