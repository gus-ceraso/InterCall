import { makeCodecProgram, requireCodecProgram } from "@cerasos/intercall/generated";

export const codec0 = requireCodecProgram(makeCodecProgram([{"op":"primitive","primitive":"int32"}], 0));

export const codec1 = requireCodecProgram(makeCodecProgram([{"op":"primitive","primitive":"int32"}], 0));

export function dispatchProcedure(procedureKey: string): string {
    switch (procedureKey) {
        case "add": return "add";
        default: return "procedure_not_found";
    }
}

import { createExportBindingWithInterfaceID, freezeDispatch } from "@cerasos/intercall/generated";

const dispatch = freezeDispatch(async () => ({ exceptionKey: 0n, payload: new Uint8Array() }));
export const exportBinding = createExportBindingWithInterfaceID(dispatch, Uint8Array.from([0x37, 0x60, 0x80, 0x99, 0xaf, 0x10, 0xcf, 0xfd, 0x16, 0x4f, 0x71, 0xf1, 0x03, 0x2c, 0x8d, 0xfc, 0xd0, 0x47, 0x04, 0x8d, 0xf5, 0xfd, 0xb8, 0x3e, 0x21, 0x52, 0x4d, 0x29, 0xa9, 0x42, 0x01, 0x2b]));
