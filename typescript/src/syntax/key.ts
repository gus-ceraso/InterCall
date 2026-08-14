import type { Position } from "./source.js";

const MASK64 = 0xffff_ffff_ffff_ffffn;
const FNV_PRIME = 1_099_511_628_211n;

export type KeyKind = "procedure" | "exception";

export function declarationKey(kind: KeyKind, name: string): bigint {
    const input = new TextEncoder().encode(`${kind} ${name}`);
    let hash = 0n;
    for (const byte of input) {
        hash = ((hash * FNV_PRIME) & MASK64) ^ BigInt(byte);
    }
    return hash;
}

export interface KeyDeclaration {
    readonly kind: KeyKind;
    readonly name: string;
    readonly position: Position;
}
