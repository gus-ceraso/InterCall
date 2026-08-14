import { createHash } from "node:crypto";

export type InterfaceID = Uint8Array;

export function interfaceID(canonicalBody: string): InterfaceID {
    return sha256(new TextEncoder().encode(canonicalBody));
}

export function sha256(bytes: Uint8Array): InterfaceID {
    const digest = createHash("sha256").update(bytes).digest();
    return new Uint8Array(digest);
}

export function interfaceIDHex(id: InterfaceID): string {
    if (id.byteLength !== 32) throw new RangeError("an interface ID must be 32 bytes");
    let result = "";
    for (const byte of id) result += byte.toString(16).padStart(2, "0");
    return result;
}
