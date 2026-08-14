import { formatInterface } from "../syntax/index.js";
import type { InterfaceFile } from "../syntax/index.js";
import { interfaceIDHex } from "./interface-id.js";
import { interfaceID } from "./interface-id.js";

export function emitImportBinding(file: InterfaceFile): string {
    const id = interfaceIDHex(interfaceID(formatInterface(file)));
    const bytes = id.match(/../gu)!.map((byte) => `0x${byte}`);
    return [
        "import { createImportBindingWithInterfaceID } from \"@cerasos/intercall/generated\";",
        "",
        `export const importBinding = createImportBindingWithInterfaceID(Uint8Array.from([${bytes.join(", ")}]));`,
        "",
    ].join("\n");
}
