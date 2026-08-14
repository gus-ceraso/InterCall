import { formatInterface } from "../syntax/index.js";
import type { InterfaceFile } from "../syntax/index.js";
import { interfaceID, interfaceIDHex } from "./interface-id.js";

export function emitExportBinding(file: InterfaceFile): string {
    const bytes = interfaceIDHex(interfaceID(formatInterface(file))).match(/../gu)!.map((byte) => `0x${byte}`);
    return [
        "import { createExportBindingWithInterfaceID, freezeDispatch } from \"@cerasos/intercall/generated\";",
        "",
        "const dispatch = freezeDispatch(async () => ({ exceptionKey: 0n, payload: new Uint8Array() }));",
        `export const exportBinding = createExportBindingWithInterfaceID(dispatch, Uint8Array.from([${bytes.join(", ")}]));`,
        "",
    ].join("\n");
}
