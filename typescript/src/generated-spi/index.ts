import type {
    Connection,
    ExportBinding,
    ImportBinding,
} from "../runtime/types.js";

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

export function createExportBinding(_dispatch: Dispatch): ExportBinding {
    throw new Error("InterCall runtime is not implemented");
}

export function createExportBindingWithInterfaceID(
    _dispatch: Dispatch,
    _interfaceID: Uint8Array,
): ExportBinding {
    throw new Error("InterCall runtime is not implemented");
}

export function createImportBinding(): ImportBinding {
    throw new Error("InterCall runtime is not implemented");
}

export function createImportBindingWithInterfaceID(
    _interfaceID: Uint8Array,
): ImportBinding {
    throw new Error("InterCall runtime is not implemented");
}
