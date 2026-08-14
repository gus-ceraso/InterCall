import { createHash } from "node:crypto";

export class PublicNameScope {
    private readonly names = new Set<string>();

    claim(name: string): void {
        if (this.names.has(name)) throw new Error(`public TypeScript name collision: ${JSON.stringify(name)}`);
        this.names.add(name);
    }

    has(name: string): boolean {
        return this.names.has(name);
    }
}

export function manglePrivate(prefix: string, parts: readonly string[]): string {
    const safePrefix = prefix.replace(/[^A-Za-z0-9_$]/gu, "_");
    const content = [prefix, ...parts].join("\u0000");
    const digest = createHash("sha256").update(content, "utf8").digest("hex");
    return `_intercall_${safePrefix}_${digest}`;
}
