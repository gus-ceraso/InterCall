import type { Dispatch } from "../generated-spi/index.js";
import { makeExportBinding, makeImportBinding } from "./binding.js";
import type { ExportBinding, ImportBinding } from "./types.js";

export const EMPTY_INTERFACE_CANONICAL_BODY =
    "exception internal_exception;\n\nexception invalid_arguments;\n\nexception procedure_not_found;\n";

const EMPTY_INTERFACE_ID_BYTES = Uint8Array.from([
    0xc3, 0x1c, 0x47, 0x0d, 0xd8, 0xdb, 0x21, 0xdb,
    0x3b, 0xc8, 0x70, 0x9b, 0xdc, 0xad, 0x77, 0x78, 0xa3,
    0xd2, 0xde, 0xad, 0x33, 0x19, 0x3c, 0x95, 0xb9,
    0x69, 0x1a, 0x4f, 0x0b, 0xa5, 0x0d, 0xc8,
]);

export const EMPTY_PROCEDURE_NOT_FOUND_KEY = 0x970e76fcc5e2dacbn;

const emptyDispatch: Dispatch = async () => ({
    exceptionKey: EMPTY_PROCEDURE_NOT_FOUND_KEY,
    payload: new Uint8Array(),
});

export const emptyImportBinding: ImportBinding = makeImportBinding(EMPTY_INTERFACE_ID_BYTES);
export const emptyExportBinding: ExportBinding = makeExportBinding(emptyDispatch, EMPTY_INTERFACE_ID_BYTES);

export function emptyInterfaceID(): Uint8Array {
    return EMPTY_INTERFACE_ID_BYTES.slice();
}
