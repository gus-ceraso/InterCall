import type { CodecProgram } from "../runtime/codec-program.js";
import { decodeProgram } from "../runtime/codec-vm.js";

export interface ExportDecodeSuccess {
    readonly ok: true;
    readonly values: readonly unknown[];
}
export interface ExportDecodeFailure {
    readonly ok: false;
    readonly exception: "invalid_arguments";
}

export function decodeExportArguments(programs: readonly CodecProgram[], payloads: readonly Uint8Array[]): ExportDecodeSuccess | ExportDecodeFailure {
    if (programs.length !== payloads.length) return { ok: false, exception: "invalid_arguments" };
    try {
        return { ok: true, values: programs.map((program, index) => decodeProgram(program, payloads[index]!)) };
    } catch {
        return { ok: false, exception: "invalid_arguments" };
    }
}
