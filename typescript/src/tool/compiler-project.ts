import ts from "typescript";
import { resolve } from "node:path";
import { InvalidArgumentError } from "../runtime/errors.js";

export interface CompilerProject {
    readonly projectPath: string;
    readonly program: ts.Program;
    readonly rootNames: readonly string[];
    readonly options: ts.CompilerOptions;
}

export function loadCompilerProject(projectPath: string): CompilerProject {
    const absoluteProject = resolve(projectPath);
    const config = ts.readConfigFile(absoluteProject, ts.sys.readFile);
    if (config.error !== undefined) throw new InvalidArgumentError(formatDiagnostics([config.error]));
    const parsed = ts.parseJsonConfigFileContent(config.config, ts.sys, resolve(absoluteProject, ".."), { noEmit: true }, absoluteProject);
    if (parsed.errors.length > 0) throw new InvalidArgumentError(formatDiagnostics(parsed.errors));
    const options = { ...parsed.options, noEmit: true };
    const program = ts.createProgram({ rootNames: parsed.fileNames, options });
    const diagnostics = [...program.getOptionsDiagnostics(), ...program.getSyntacticDiagnostics()];
    if (diagnostics.length > 0) throw new InvalidArgumentError(formatDiagnostics(diagnostics));
    return { projectPath: absoluteProject, program, rootNames: parsed.fileNames, options };
}

function formatDiagnostics(diagnostics: readonly ts.Diagnostic[]): string {
    return ts.flattenDiagnosticMessageText(
        ts.formatDiagnosticsWithColorAndContext(diagnostics, {
            getCanonicalFileName: (fileName) => fileName,
            getCurrentDirectory: () => ts.sys.getCurrentDirectory(),
            getNewLine: () => "\n",
        }),
        "\n",
    );
}
