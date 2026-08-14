import ts from "typescript";
import { attachDocumentation, formatInterface, parseInterface, validateInterface, type InterfaceFile } from "../syntax/index.js";
import type { CompilerProject } from "./compiler-project.js";
import type { DiscoveredException, DiscoveredProcedure, DiscoveredType, SourceDiscovery } from "./source-discovery.js";
import { orderDiscoveredExports } from "./source-order.js";
import { sourceDocumentation, sourceParameterDocumentation, sourceReturnDocumentation } from "./directives.js";
import { hasGeneratedTypeScriptMarker } from "./metadata-reader.js";
import { isExactRuntimeMarker } from "./source-validation.js";
import { typeScriptToWire } from "./name.js";
import { walkReachableType } from "./type-graph.js";

const fixedExceptions = ["internal_exception", "invalid_arguments", "procedure_not_found"] as const;
const primitiveNames: Readonly<Record<string, string>> = {
    Int8: "int8", Int16: "int16", Int32: "int32", Int64: "int64",
    Uint8: "uint8", Uint16: "uint16", Uint32: "uint32", Uint64: "uint64",
    Float32: "float32", Float64: "float64",
};

export interface ExportInterfaceResult {
    readonly source: InterfaceFile;
    readonly canonicalText: string;
}

export function buildExportInterface(project: CompilerProject, discovery: SourceDiscovery): ExportInterfaceResult {
    const ordered = orderDiscoveredExports(discovery);
    const checker = project.program.getTypeChecker();
    const wireNames = new Map([...ordered.namedTypes, ...ordered.exceptions, ...ordered.procedures].map((record) => [record.sourceName, record.wireName]));
    const lines: string[] = fixedExceptions.map((name) => `exception ${name};`);
    for (const type of ordered.namedTypes) {
        const node = ts.isTypeAliasDeclaration(type.declaration) ? type.declaration.type : type.declaration;
        walkReachableType(project, node);
        const projected = typeText(project, checker, type.sourceFile, checker.getTypeAtLocation(node), new Set(), wireNames, node, discovery.fieldWireNames);
        if (type.metadataDeclaration !== undefined) {
            const expected = formatInterface({ source: type.sourceFile as unknown as import("../syntax/index.js").SourceFile, declarations: [type.metadataDeclaration], comments: [] });
            const actual = formatInterface(parseInterface("projected.intercall", new TextEncoder().encode(`type ${type.wireName} ${projected};`)));
            if (stripDocumentation(expected) !== stripDocumentation(actual)) throw new Error(`generated metadata structure does not match exported TypeScript type ${type.sourceName}`);
            lines.push(expected.trimEnd());
        } else lines.push(...docLines(type.documentation ?? sourceDocumentation(type.declaration)), `type ${type.wireName} ${projected};`);
    }
    for (const exception of ordered.exceptions) {
        const payload = exceptionPayloadType(exception, checker);
        lines.push(...docLines(sourceDocumentation(exception.declaration)), payload === undefined ? `exception ${exception.wireName};` : `exception ${exception.wireName} ${typeText(project, checker, exception.sourceFile, checker.getTypeAtLocation(payload), new Set(), wireNames, payload, discovery.fieldWireNames)};`);
    }
    for (const procedure of ordered.procedures) lines.push(...docLines(sourceDocumentation(procedure.declaration)), procedureText(project, checker, procedure, wireNames, discovery.parameterWireNames, discovery.fieldWireNames));
    const canonicalText = lines.join("\n") + "\n";
    const source = parseInterface("generated-export.intercall", new TextEncoder().encode(canonicalText));
    attachDocumentation(source);
    validateInterface(source);
    return { source, canonicalText };
}

function procedureText(project: CompilerProject, checker: ts.TypeChecker, procedure: DiscoveredProcedure, wireNames: ReadonlyMap<string, string>, parameterWireNames: ReadonlyMap<ts.ParameterDeclaration, string>, fieldWireNames: ReadonlyMap<ts.Node, string>): string {
    const params = procedure.declaration.parameters.slice(1).map((parameter) => `${docInline(sourceParameterDocumentation(procedure.declaration, parameter.name.getText()) ?? sourceDocumentation(parameter))}${parameterWireNames.get(parameter) ?? typeScriptToWire(parameter.name.getText(), "camel")} ${typeText(project, checker, procedure.sourceFile, checker.getTypeAtLocation(parameter.type!), new Set(), wireNames, parameter.type!, fieldWireNames)};`).join(" ");
    const signature = checker.getSignatureFromDeclaration(procedure.declaration);
    const result = signature === undefined ? undefined : checker.getReturnTypeOfSignature(signature);
    const resultType = result === undefined ? undefined : checker.getTypeArguments(result as ts.TypeReference)[0];
    const resultNode = procedure.declaration.type !== undefined && ts.isTypeReferenceNode(procedure.declaration.type) ? procedure.declaration.type.typeArguments?.[0] : undefined;
    const resultDoc = sourceReturnDocumentation(procedure.declaration) ?? (procedure.declaration.type === undefined ? undefined : sourceDocumentation(procedure.declaration.type));
    return `procedure ${procedure.wireName} {${params}}${resultType === undefined || (resultType.flags & ts.TypeFlags.Void) !== 0 ? "" : ` ${docInline(resultDoc)}${typeText(project, checker, procedure.sourceFile, resultType, new Set(), wireNames, resultNode)}`} ;`.replace("} ;", "};");
}

function exceptionPayloadType(exception: DiscoveredException, checker: ts.TypeChecker): ts.TypeNode | undefined {
    if (!ts.isClassDeclaration(exception.declaration)) return undefined;
    for (const clause of exception.declaration.heritageClauses ?? []) {
        for (const heritage of clause.types) {
            const symbol = checker.getSymbolAtLocation(heritage.expression);
            const name = symbol === undefined ? undefined : checker.getFullyQualifiedName(symbol);
            if (name?.endsWith(".PayloadException") || heritage.expression.getText() === "PayloadException") return heritage.typeArguments?.[0];
        }
    }
    return undefined;
}

function arrayElementNode(node: ts.Node | undefined): ts.TypeNode | undefined {
    if (node === undefined) return undefined;
    if (ts.isArrayTypeNode(node)) return node.elementType;
    if (ts.isTypeReferenceNode(node)) return node.typeArguments?.[0];
    if (ts.isTypeOperatorNode(node)) return arrayElementNode(node.type);
    return undefined;
}

function stripDocumentation(value: string): string {
    return value.replace(/\/\*[\\s\\S]*?\*\//gu, "").replace(/[ \t]+$/gmu, "").replace(/\n{2,}/gu, "\n").trim();
}

function docLines(value: string | undefined): string[] {
    if (value === undefined || value === "") return [];
    return [`/* ${value.replaceAll("*/", "* /")} */`];
}

function docInline(value: string | undefined): string {
    return value === undefined || value === "" ? "" : `/* ${value.replaceAll("*/", "* /")} */ `;
}

function typeText(project: CompilerProject, checker: ts.TypeChecker, sourceFile: ts.SourceFile, type: ts.Type, active: Set<string>, wireNames: ReadonlyMap<string, string>, node?: ts.Node, fieldWireNames: ReadonlyMap<ts.Node, string> = new Map(), expandedGenerated = new Set<ts.Symbol>()): string {
    for (const [name, primitive] of Object.entries(primitiveNames)) if (isExactRuntimeMarker(project, sourceFile, type, name, node)) return primitive;
    if (isExactRuntimeMarker(project, sourceFile, type, "EmptyRecord", node)) return "record {}";
    if (node !== undefined && ts.isTypeReferenceNode(node)) {
        const typeName = node.typeName.getText();
        if (wireNames.has(typeName)) return wireNames.get(typeName)!;
        const nodeSymbol = checker.getSymbolAtLocation(node.typeName);
        const aliasDeclaration = nodeSymbol?.declarations?.find(ts.isTypeAliasDeclaration);
        if (aliasDeclaration !== undefined) return typeText(project, checker, sourceFile, checker.getTypeAtLocation(aliasDeclaration.type), active, wireNames, aliasDeclaration.type, fieldWireNames);
    }
    const symbol = type.aliasSymbol ?? type.symbol;
    if (symbol?.declarations?.some((declaration) => ts.isTypeAliasDeclaration(declaration))) {
        const declaration = symbol.declarations.find(ts.isTypeAliasDeclaration);
        if (declaration !== undefined) {
            const generated = hasGeneratedTypeScriptMarker(declaration.getSourceFile().getFullText());
            if (!(generated && expandedGenerated.has(symbol))) {
                if (active.has(symbol.name)) throw new Error(`recursive TypeScript type ${symbol.name}`);
                const next = new Set(active);
                next.add(symbol.name);
                const expanded = new Set(expandedGenerated);
                if (generated) expanded.add(symbol);
                return typeText(project, checker, sourceFile, checker.getTypeAtLocation(declaration.type), next, wireNames, declaration.type, fieldWireNames, expanded);
            }
        }
    }
    const globalBytes = checker.resolveName("Uint8Array", node, ts.SymbolFlags.Type, false);
    if (type.symbol?.name === "Uint8Array" && expandedGenerated.size > 0) return "bytes";
    if (type.symbol !== undefined && type.symbol === globalBytes && checker.isArrayLikeType(type)) return "bytes";
    if (symbol?.declarations?.some((declaration) => ts.isClassDeclaration(declaration))) throw new Error(`unsupported class type ${checker.typeToString(type)}`);
    if (checker.isTupleType(type)) throw new Error(`unsupported tuple type ${checker.typeToString(type)}`);
    if (checker.isArrayType(type) || (type.symbol?.name === "ReadonlyArray" && checker.isArrayLikeType(type))) {
        const element = checker.getTypeArguments(type as ts.TypeReference)[0];
        if (element === undefined) throw new Error("array type has no element type");
        const elementNode = arrayElementNode(node);
        return `list ${typeText(project, checker, sourceFile, element, active, wireNames, elementNode, fieldWireNames, expandedGenerated)}`;
    }
    if ((type.flags & ts.TypeFlags.StringLike) !== 0 && expandedGenerated.size > 0) return "string";
    if ((type.flags & (ts.TypeFlags.StringLike | ts.TypeFlags.NumberLike | ts.TypeFlags.BigIntLike | ts.TypeFlags.BooleanLike)) !== 0) throw new Error(`unsupported unmarked primitive ${checker.typeToString(type)}`);
    if (type.getProperties().length > 0) {
        if (type.getProperties().some((property) => (property.flags & ts.SymbolFlags.Optional) !== 0)) throw new Error(`optional properties are unsupported in ${checker.typeToString(type)}`);
        if (symbol !== undefined && !expandedGenerated.has(symbol)) {
            if (active.has(symbol.name)) throw new Error(`recursive TypeScript type ${symbol.name}`);
            active.add(symbol.name);
        }
        const fields = type.getProperties().map((property) => {
            const propertyNode = property.valueDeclaration;
            if (propertyNode !== undefined && (ts.isPropertySignature(propertyNode) || ts.isPropertyDeclaration(propertyNode)) && !ts.isIdentifier(propertyNode.name)) throw new Error(`unsupported computed TypeScript field ${property.name}`);
            const propertyType = checker.getTypeOfSymbolAtLocation(property, propertyNode ?? property.declarations?.[0]!);
            const propertyTypeNode = propertyNode !== undefined && (ts.isPropertySignature(propertyNode) || ts.isPropertyDeclaration(propertyNode)) ? propertyNode.type : undefined;
            return `${docInline(propertyNode === undefined ? undefined : sourceDocumentation(propertyNode))}${propertyNode === undefined ? typeScriptToWire(property.name, "camel") : fieldWireNames.get(propertyNode) ?? typeScriptToWire(property.name, "camel")} ${typeText(project, checker, sourceFile, propertyType, active, wireNames, propertyTypeNode, fieldWireNames, expandedGenerated)};`;
        }).join(" ");
        return `record {${fields}}`;
    }
    throw new Error(`unsupported TypeScript type ${checker.typeToString(type)}`);
}
