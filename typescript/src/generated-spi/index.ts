import type {
    Connection,
    ExportBinding,
    ImportBinding,
} from "../runtime/types.js";
import {
    makeExportBinding,
    makeImportBinding,
} from "../runtime/binding.js";
export {
    emptyExportBinding,
    emptyImportBinding,
} from "../runtime/empty.js";

export interface DispatchResult {
    readonly exceptionKey: bigint;
    readonly payload: Uint8Array;
}

export type Dispatch = (
    context: import("../runtime/types.js").HandlerContext,
    procedureKey: bigint,
    payload: Uint8Array,
) => Promise<DispatchResult>;

export type RequestEncoder = () => Uint8Array;
export type ResponseDecoder = (
    exceptionKey: bigint,
    payload: Uint8Array,
) => void;

export function call(
    _connection: Connection,
    _binding: ImportBinding,
    _procedureKey: bigint,
    _encode: RequestEncoder,
    _decode: ResponseDecoder,
): Promise<void> {
    return Promise.reject(new Error("InterCall runtime is not implemented"));
}

export function createExportBinding(dispatch: Dispatch): ExportBinding {
    return makeExportBinding(dispatch);
}

export function createExportBindingWithInterfaceID(
    dispatch: Dispatch,
    interfaceID: Uint8Array,
): ExportBinding {
    return makeExportBinding(dispatch, interfaceID);
}

export function createImportBinding(): ImportBinding {
    return makeImportBinding();
}

export function createImportBindingWithInterfaceID(
    interfaceID: Uint8Array,
): ImportBinding {
    return makeImportBinding(interfaceID);
}
