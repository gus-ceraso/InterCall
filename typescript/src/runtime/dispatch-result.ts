import type { DispatchResult } from "../generated-spi/index.js";
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

export function isDispatchResult(value: unknown): value is DispatchResult {
    if (value === null || typeof value !== "object") return false;
    const result = value as Partial<DispatchResult>;
    return typeof result.exceptionKey === "bigint" && result.payload instanceof Uint8Array;
}
