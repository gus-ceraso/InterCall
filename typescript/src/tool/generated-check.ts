import ts from "typescript";

const runtimeDeclarations = `
declare module "@cerasos/intercall" {
  export type EmptyRecord = { readonly [name: string]: never };
  export type Int8 = number; export type Int16 = number; export type Int32 = number; export type Int64 = bigint;
  export type Uint8 = number; export type Uint16 = number; export type Uint32 = number; export type Uint64 = bigint;
  export type Float32 = number; export type Float64 = number;
  export class PayloadException<T> extends Error { readonly payload: T; protected constructor(payload: T); }
  export const ProcedureNotFound: Error; export const InvalidArguments: Error; export const InternalException: Error;
  export interface Connection { readonly closed: Promise<Error>; close(): void; }
  export interface CallOptions { readonly signal?: AbortSignal; }
}
`;
const spiDeclarations = `
declare module "@cerasos/intercall/generated" {
  export interface CodecProgram { readonly instructions: readonly unknown[]; readonly root: number; readonly zeroWidth: boolean; }
  export function makeCodecProgram(instructions: readonly unknown[], root: number): CodecProgram;
  export function requireCodecProgram(program: CodecProgram): CodecProgram;
  export function call(connection: import("@cerasos/intercall").Connection, binding: unknown, key: bigint, encode: () => Uint8Array, decode: (key: bigint, payload: Uint8Array) => void, options?: import("@cerasos/intercall").CallOptions): Promise<void>;
  export function createImportBindingWithInterfaceID(id: Uint8Array): unknown;
  export function freezeDispatch(dispatch: (...args: any[]) => Promise<any>): (...args: any[]) => Promise<any>;
  export function createExportBindingWithInterfaceID(dispatch: (...args: any[]) => Promise<any>, id: Uint8Array): unknown;
}
`;

export function validateGeneratedSource(source: string, fileName = "binding_gen.ts"): void {
    const virtualRoot = "/__intercall_generated__";
    const sourcePath = `${virtualRoot}/${fileName}`;
    const runtimePath = `${virtualRoot}/runtime.d.ts`;
    const spiPath = `${virtualRoot}/spi.d.ts`;
    const files = new Map([[sourcePath, source], [runtimePath, runtimeDeclarations], [spiPath, spiDeclarations]]);
    const options: ts.CompilerOptions = {
        noEmit: true,
        strict: true,
        target: ts.ScriptTarget.ES2022,
        module: ts.ModuleKind.ESNext,
        moduleResolution: ts.ModuleResolutionKind.Bundler,
        baseUrl: virtualRoot,
        paths: {
            "@cerasos/intercall": ["runtime.d.ts"],
            "@cerasos/intercall/generated": ["spi.d.ts"],
        },
    };
    const host = ts.createCompilerHost(options);
    const originalRead = host.readFile;
    host.fileExists = (fileName) => files.has(fileName) || ts.sys.fileExists(fileName);
    host.readFile = (fileName) => files.get(fileName) ?? originalRead(fileName);
    host.getSourceFile = (fileName, languageVersion) => {
        const text = host.readFile(fileName);
        return text === undefined ? undefined : ts.createSourceFile(fileName, text, languageVersion, true);
    };
    const program = ts.createProgram([sourcePath, runtimePath, spiPath], options, host);
    const diagnostics = [...program.getSyntacticDiagnostics(), ...program.getSemanticDiagnostics()];
    if (diagnostics.length > 0) throw new Error(formatDiagnostics(diagnostics, sourcePath));
}

function formatDiagnostics(diagnostics: readonly ts.Diagnostic[], fileName: string): string {
    return ts.flattenDiagnosticMessageText(diagnostics.map((diagnostic) => {
        const position = diagnostic.file?.getLineAndCharacterOfPosition(diagnostic.start ?? 0);
        return `${fileName}:${position === undefined ? "" : `${position.line + 1}:${position.character + 1}: `}${ts.flattenDiagnosticMessageText(diagnostic.messageText, " ")}`;
    }).join("\n"), "\n");
}
