declare module "@cerasos/intercall/generated" {
    export interface CodecProgram { readonly instructions: readonly unknown[]; readonly root: number; }
    export function makeCodecProgram(instructions: readonly unknown[], root: number): CodecProgram;
    export function requireCodecProgram(program: CodecProgram): CodecProgram;
    export function decodeProgram(program: CodecProgram, payload: Uint8Array): unknown;
    export function decodeProgramsFromPayload(programs: readonly CodecProgram[], payload: Uint8Array): unknown[];
    export function encodeProgram(program: CodecProgram, value: unknown): Uint8Array;
    export function encodeProgramsToPayload(programs: readonly CodecProgram[], values: readonly unknown[]): Uint8Array;
    export function freezeDispatch(dispatch: (...args: any[]) => Promise<any>): (...args: any[]) => Promise<any>;
    export function createExportBindingWithInterfaceID(dispatch: (...args: any[]) => Promise<any>, id: Uint8Array): unknown;
}
