import ts from "typescript";
import type { DiscoveredException, DiscoveredProcedure, DiscoveredType, SourceDiscovery } from "./source-discovery.js";

export function orderDiscoveredExports(discovery: SourceDiscovery): SourceDiscovery {
    return {
        procedures: [...discovery.procedures].sort(compareWireNames),
        exceptions: [...discovery.exceptions].sort(compareWireNames),
        namedTypes: stableTypeOrder(discovery.namedTypes),
    };
}

function stableTypeOrder(types: readonly DiscoveredType[]): DiscoveredType[] {
    const byName = new Map(types.map((type) => [type.sourceName, type]));
    const dependencies = new Map<string, Set<string>>();
    for (const type of types) {
        const found = new Set<string>();
        const visit = (node: ts.Node) => {
            if (ts.isIdentifier(node) && node.text !== type.sourceName && byName.has(node.text)) found.add(node.text);
            ts.forEachChild(node, visit);
        };
        visit(type.declaration);
        dependencies.set(type.sourceName, found);
    }
    const remaining = new Set(types.map((type) => type.sourceName));
    const result: DiscoveredType[] = [];
    while (remaining.size > 0) {
        const ready = [...remaining].filter((name) => [...dependencies.get(name)!].every((dependency) => !remaining.has(dependency))).sort((left, right) => compareWireNames(byName.get(left)!, byName.get(right)!));
        if (ready.length === 0) throw new Error("recursive named type dependency graph");
        for (const name of ready) {
            remaining.delete(name);
            result.push(byName.get(name)!);
        }
    }
    return result;
}

function compareWireNames(left: { readonly wireName: string; readonly sourceName: string }, right: { readonly wireName: string; readonly sourceName: string }): number {
    return compareText(left.wireName, right.wireName) || compareText(left.sourceName, right.sourceName);
}

function compareText(left: string, right: string): number {
    return left < right ? -1 : left > right ? 1 : 0;
}
