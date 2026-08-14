import ts from "typescript";
import type { CompilerProject } from "./compiler-project.js";

export const MAX_SOURCE_TYPE_DEPTH = 4_096;

export interface TypeGraphResult {
    readonly nodes: number;
    readonly properties: readonly string[];
}

interface Work {
    readonly type: ts.Type;
    readonly depth: number;
    readonly active: ReadonlySet<ts.Symbol>;
}

export function walkReachableType(project: CompilerProject, root: ts.Node, limit = MAX_SOURCE_TYPE_DEPTH): TypeGraphResult {
    const checker = project.program.getTypeChecker();
    const stack: Work[] = [{ type: checker.getTypeAtLocation(root), depth: 1, active: new Set() }];
    const properties: string[] = [];
    let nodes = 0;
    while (stack.length > 0) {
        const work = stack.pop()!;
        nodes += 1;
        if (work.depth > limit) throw new Error(`resolved type depth exceeds ${limit} occurrences`);
        const type = work.type;
        if (type.isUnion() || type.isIntersection() || (type.flags & (ts.TypeFlags.Any | ts.TypeFlags.Unknown | ts.TypeFlags.Never | ts.TypeFlags.Undefined | ts.TypeFlags.Null)) !== 0) {
            throw new Error(`unsupported TypeScript type ${checker.typeToString(type)}`);
        }
        if ((type.flags & (ts.TypeFlags.BooleanLike | ts.TypeFlags.NumberLike | ts.TypeFlags.StringLike | ts.TypeFlags.BigIntLike)) !== 0) continue;
        const symbol = type.aliasSymbol ?? type.symbol;
        if (symbol !== undefined && isExpandable(symbol, type)) {
            if (work.active.has(symbol)) throw new Error(`recursive TypeScript type ${symbol.name}`);
            const active = new Set(work.active);
            active.add(symbol);
            const declarations = symbol.declarations ?? [];
            for (let i = declarations.length - 1; i >= 0; i -= 1) {
                const declaration = declarations[i]!;
                if (ts.isTypeAliasDeclaration(declaration)) stack.push({ type: checker.getTypeAtLocation(declaration.type), depth: work.depth + 1, active });
                else if (ts.isInterfaceDeclaration(declaration) || ts.isClassDeclaration(declaration)) pushProperties(checker, type, stack, properties, work.depth + 1, active);
            }
            continue;
        }
        if (isArrayType(checker, type)) {
            const element = checker.getTypeArguments(type as ts.TypeReference)[0];
            if (element !== undefined) stack.push({ type: element, depth: work.depth + 1, active: work.active });
            continue;
        }
        if (type.getProperties().length > 0) pushProperties(checker, type, stack, properties, work.depth + 1, work.active);
    }
    return { nodes, properties };
}

function pushProperties(checker: ts.TypeChecker, type: ts.Type, stack: Work[], properties: string[], depth: number, active: ReadonlySet<ts.Symbol>): void {
    const symbols = type.getProperties();
    for (const property of symbols) properties.push(property.name);
    for (let i = symbols.length - 1; i >= 0; i -= 1) {
        const property = symbols[i]!;
        stack.push({ type: checker.getTypeOfSymbolAtLocation(property, property.valueDeclaration ?? property.declarations?.[0]!), depth, active });
    }
}

function isArrayType(checker: ts.TypeChecker, type: ts.Type): boolean {
    return checker.isArrayType(type) || checker.isTupleType(type);
}

function isExpandable(symbol: ts.Symbol, type: ts.Type): boolean {
    return (symbol.flags & (ts.SymbolFlags.TypeAlias | ts.SymbolFlags.Interface | ts.SymbolFlags.Class)) !== 0 && type.getProperties().length > 0;
}
