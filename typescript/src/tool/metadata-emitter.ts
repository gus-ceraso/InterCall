import { formatInterface } from "../syntax/index.js";
import type { InterfaceFile } from "../syntax/index.js";
import { interfaceIDHex, interfaceID } from "./interface-id.js";
import type { ImportGenerationRecord } from "./import.js";

export function emitImportMetadata(file: InterfaceFile, generation: ImportGenerationRecord): string {
    const body = formatInterface(file);
    const id = interfaceIDHex(interfaceID(body));
    const rows = generation.namedTypes.map((record) => ({ wireName: record.declaration.name.name, nativeName: record.nativeName }));
    return [
        `export const interfaceBody = ${JSON.stringify(body)};`,
        `export const interfaceIDHex = ${JSON.stringify(id)};`,
        `export const machineTypes = Object.freeze(${JSON.stringify(rows)});`,
        "",
    ].join("\n");
}
