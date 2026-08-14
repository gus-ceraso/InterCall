import type {
    Connection,
    ExportBinding,
    ImportBinding,
} from "../runtime/types.js";
import {
    makeExportBinding,
    makeImportBinding,
} from "../runtime/binding.js";
import type { CodecProgram } from "../runtime/codec-program.js";
import type { CallOptions } from "../runtime/types.js";
import { connectionRuntimeFor } from "../runtime/connection-runtime.js";
export type { CodecProgram } from "../runtime/codec-program.js";
export { makeCodecProgram } from "../runtime/codec-program.js";
export { decodeProgram, decodeProgramsFromPayload, encodeProgram } from "../runtime/codec-vm.js";
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

export function freezeDispatch(dispatch: Dispatch): Dispatch {
    return freezeFunction(dispatch, "dispatch");
}

export function freezeRequestEncoder(encode: RequestEncoder): RequestEncoder {
    return freezeFunction(encode, "request encoder");
}

export function freezeResponseDecoder(decode: ResponseDecoder): ResponseDecoder {
    return freezeFunction(decode, "response decoder");
}

export function requireCodecProgram(program: CodecProgram): CodecProgram {
    if (!Object.isFrozen(program) || !Object.isFrozen(program.instructions)) {
        throw new TypeError("codec program must be immutable");
    }
    return program;
}

function freezeFunction<T extends Function>(value: T, label: string): T {
    if (typeof value !== "function") throw new TypeError(`${label} must be a function`);
    return Object.freeze(value);
}

export function call(
    connection: Connection,
    binding: ImportBinding,
    procedureKey: bigint,
    encode: RequestEncoder,
    decode: ResponseDecoder,
    options?: CallOptions,
): Promise<void> {
    return connectionRuntimeFor(connection).call(binding, procedureKey, encode, decode, options);
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
