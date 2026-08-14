import type { CodecProgram } from "../runtime/codec-program.js";
import { encodeProgram } from "../runtime/codec-vm.js";

export type ExportEncodeResult =
    | { readonly ok: true; readonly payload: Uint8Array; readonly exception?: never }
    | { readonly ok: false; readonly payload: Uint8Array; readonly exception: "internal_exception" };

export function encodeExportResult(program: CodecProgram | undefined, value: unknown): ExportEncodeResult {
    if (program === undefined) return { ok: true, payload: new Uint8Array() };
    try {
        return { ok: true, payload: encodeProgram(program, value) };
    } catch {
        return { ok: false, payload: new Uint8Array(), exception: "internal_exception" };
    }
}
