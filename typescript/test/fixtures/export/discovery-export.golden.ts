import { makeCodecProgram, requireCodecProgram } from "@cerasos/intercall/generated";

export const codec0 = requireCodecProgram(makeCodecProgram([{"op":"primitive","primitive":"int32"}], 0));

export const codec1 = requireCodecProgram(makeCodecProgram([{"op":"primitive","primitive":"int32"}], 0));

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
                value = decodeProgram(codec0, payload);
            } catch {
                return { exceptionKey: 0x3f5fc972f8477b07n, payload: new Uint8Array() };
            }
            try {
                const result = await provider_0["add"](context, value);
                return { exceptionKey: 0n, payload: encodeProgram(codec1, result) };
            } catch (error) {
                if (error === provider_0["Denied"]) return { exceptionKey: 9204783932309381296n, payload: new Uint8Array() };
                if (error instanceof provider_0["Failed"]) return { exceptionKey: 6358998032231655626n, payload: new Uint8Array() };
                return { exceptionKey: 0x1aaec22e85996f50n, payload: new Uint8Array() };
            }
        }
        default: return { exceptionKey: 0x970e76fcc5e2dacbn, payload: new Uint8Array() };
    }
});
export const exportBinding = createExportBindingWithInterfaceID(dispatch, Uint8Array.from([0x38, 0x47, 0x53, 0xf6, 0xe7, 0xce, 0xfa, 0xde, 0x1b, 0xd8, 0xf4, 0x88, 0x8c, 0xea, 0xc9, 0x1f, 0x7d, 0xb6, 0xd4, 0x8f, 0x08, 0x16, 0xe8, 0xad, 0xef, 0x56, 0xd8, 0xd7, 0x7d, 0x1e, 0xc3, 0xb8]));
