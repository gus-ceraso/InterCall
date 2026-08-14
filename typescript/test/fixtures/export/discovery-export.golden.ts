import { makeCodecProgram, requireCodecProgram } from "@cerasos/intercall/generated";

export const codec0 = requireCodecProgram(makeCodecProgram([{"op":"primitive","primitive":"int32"},{"op":"record","fields":[{"name":"code","value":0}]}], 1));

export const codec1 = requireCodecProgram(makeCodecProgram([{"op":"primitive","primitive":"int32"}], 0));

export const codec2 = requireCodecProgram(makeCodecProgram([{"op":"primitive","primitive":"int32"}], 0));

export function dispatchProcedure(procedureKey: string): string {
    switch (procedureKey) {
        case "add": return "add";
        default: return "procedure_not_found";
    }
}

import { createExportBindingWithInterfaceID, decodeProgram, decodeProgramsFromPayload, encodeProgram, freezeDispatch } from "@cerasos/intercall/generated";
import * as provider_0 from "./discovery.js";

const dispatch = freezeDispatch(async (context, procedureKey, payload) => {
    switch (procedureKey) {
        case 9522261952191621886n: {
            let value: any;
            try {
                value = decodeProgram(codec1, payload);
            } catch {
                return { exceptionKey: 0x3f5fc972f8477b07n, payload: new Uint8Array() };
            }
            try {
                const result = await provider_0["add"](context, value);
                return { exceptionKey: 0n, payload: encodeProgram(codec2, result) };
            } catch (error: any) {
                let matchCount = 0;
                let matchedKey = 0n;
                let matchedPayload: Uint8Array = new Uint8Array();
                try {
                    if (error === provider_0["Denied"]) { matchCount += 1; matchedKey = 9204783932309381296n; }
                    if (error instanceof provider_0["Failed"]) { matchCount += 1; matchedKey = 6358998032231655626n; matchedPayload = encodeProgram(codec0, error.payload); }
                    if (matchCount === 1) return { exceptionKey: matchedKey, payload: matchedPayload };
                    return { exceptionKey: 0x1aaec22e85996f50n, payload: new Uint8Array() };
                } catch {
                    return { exceptionKey: 0x1aaec22e85996f50n, payload: new Uint8Array() };
                }
            }
        }
        default: return { exceptionKey: 0x970e76fcc5e2dacbn, payload: new Uint8Array() };
    }
});
export const exportBinding = createExportBindingWithInterfaceID(dispatch, Uint8Array.from([0xda, 0xab, 0xb1, 0xbb, 0xbe, 0x0b, 0xd5, 0xf9, 0x60, 0x8d, 0x8f, 0x51, 0x2b, 0x46, 0x30, 0xb5, 0x95, 0xc1, 0xc8, 0x76, 0xa4, 0x05, 0x94, 0xc3, 0x97, 0xd6, 0x86, 0x31, 0xa8, 0xf4, 0xed, 0x8f]));
