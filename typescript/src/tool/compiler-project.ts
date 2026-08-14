import ts from "typescript";
import { extname, relative, resolve } from "node:path";
import { InvalidArgumentError } from "../runtime/errors.js";

export interface CompilerProject {
    readonly projectPath: string;
    readonly program: ts.Program;
    readonly rootNames: readonly string[];
    readonly options: ts.CompilerOptions;
}

export interface SourceOperand {
    readonly fileName: string;
    readonly logicalPath: string;
    readonly extension: ".ts" | ".tsx";
    readonly jsx: ts.JsxEmit | undefined;
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

export function normalizeSourceOperands(project: CompilerProject, operands: readonly string[]): SourceOperand[] {
    const rootNames = new Set(project.rootNames.map((fileName) => resolve(fileName)));
    return operands.map((operand) => {
        const fileName = resolve(operand);
        const extension = extname(fileName).toLowerCase();
        if (extension !== ".ts" && extension !== ".tsx") throw new InvalidArgumentError(`source operand must end in .ts or .tsx: ${operand}`);
        if (!rootNames.has(fileName)) throw new InvalidArgumentError(`source operand is not part of the project: ${operand}`);
        const logicalPath = relative(resolve(project.projectPath, ".."), fileName).replaceAll("\\", "/");
        return { fileName, logicalPath, extension, jsx: project.options.jsx };
    });
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
