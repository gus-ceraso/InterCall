import type { ProviderImport } from "./provider-imports.js";

export interface EmittedProviderImport {
    readonly importRecord: ProviderImport;
    readonly localName: string;
    readonly source: string;
}

export function emitProviderImports(imports: readonly ProviderImport[]): EmittedProviderImport[] {
    const result: EmittedProviderImport[] = [];
    const used = new Set<string>();
    for (const [index, importRecord] of [...imports].sort((left, right) => left.from.localeCompare(right.from) || left.specifier.localeCompare(right.specifier)).entries()) {
        let localName = `provider_${index}`;
        while (used.has(localName)) localName += "_";
        used.add(localName);
        result.push({ importRecord, localName, source: `import * as ${localName} from ${JSON.stringify(importRecord.emittedSpecifier)};` });
    }
    return result;
}
