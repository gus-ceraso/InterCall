import type { Dispatch, DispatchResult } from "../generated-spi/index.js";
import type { HandlerContext } from "./types.js";
import {
    InternalException,
    InvalidArguments,
    ProcedureNotFound,
} from "./errors.js";

export type FixedDispatchFailure = "procedure_not_found" | "invalid_arguments" | "internal_exception";

export function fixedDispatchResult(failure: FixedDispatchFailure): DispatchResult {
    const exception = failure === "procedure_not_found"
        ? ProcedureNotFound
        : failure === "invalid_arguments" ? InvalidArguments : InternalException;
    return { exceptionKey: exception.key, payload: new Uint8Array() };
}

export async function invokeDispatch(
    dispatch: Dispatch,
    context: HandlerContext,
    procedureKey: bigint,
    payload: Uint8Array,
): Promise<DispatchResult> {
    try {
        const value = await dispatch(context, procedureKey, payload);
        if (!isDispatchResult(value)) return fixedDispatchResult("internal_exception");
        return { exceptionKey: value.exceptionKey, payload: value.payload.slice() };
    } catch (error) {
        if (error === ProcedureNotFound) return fixedDispatchResult("procedure_not_found");
        if (error === InvalidArguments) return fixedDispatchResult("invalid_arguments");
        return fixedDispatchResult("internal_exception");
    }
}

export function isDispatchResult(value: unknown): value is DispatchResult {
    if (value === null || typeof value !== "object") return false;
    const result = value as Partial<DispatchResult>;
    return typeof result.exceptionKey === "bigint" && result.payload instanceof Uint8Array;
}
