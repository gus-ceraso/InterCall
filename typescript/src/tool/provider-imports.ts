import ts from "typescript";
import { dirname, extname, relative, resolve } from "node:path";
import type { CompilerProject, SourceOperand } from "./compiler-project.js";

export interface ProviderImport {
    readonly from: string;
    readonly specifier: string;
    readonly resolvedFile: string;
    readonly emittedSpecifier: string;
}

export function resolveProviderImports(project: CompilerProject, operands: readonly SourceOperand[]): ProviderImport[] {
    const operandFiles = new Set(operands.map((operand) => resolve(operand.fileName)));
    const imports: ProviderImport[] = [];
    const edges = new Map<string, string[]>();
    for (const operand of operands) {
        const sourceFile = project.program.getSourceFile(operand.fileName);
        if (sourceFile === undefined) throw new Error(`source file is not in the program: ${operand.fileName}`);
        const targets: string[] = [];
        for (const statement of sourceFile.statements) {
            if (!ts.isImportDeclaration(statement) || ts.isStringLiteral(statement.moduleSpecifier) === false) continue;
            if (statement.importClause?.isTypeOnly === true) continue;
            const specifier = statement.moduleSpecifier.text;
            const resolved = ts.resolveModuleName(specifier, operand.fileName, project.options, ts.sys).resolvedModule;
            if (resolved === undefined) throw new Error(`cannot resolve provider import ${JSON.stringify(specifier)} from ${operand.fileName}`);
            const resolvedFile = resolve(resolved.resolvedFileName);
            if (extname(resolvedFile).toLowerCase() === ".d.ts") throw new Error(`provider import resolves to declaration-only module ${resolvedFile}`);
            const emittedSpecifier = emittedModuleSpecifier(operand.fileName, resolvedFile, project.options.jsx);
            imports.push({ from: operand.fileName, specifier, resolvedFile, emittedSpecifier });
            if (operandFiles.has(resolvedFile)) targets.push(resolvedFile);
        }
        edges.set(resolve(operand.fileName), targets);
    }
    detectCycles(edges);
    return imports.sort((left, right) => left.from.localeCompare(right.from) || left.specifier.localeCompare(right.specifier));
}

function emittedModuleSpecifier(from: string, resolvedFile: string, jsx: ts.JsxEmit | undefined): string {
    const extension = extname(resolvedFile).toLowerCase();
    const outputExtension = extension === ".tsx" && jsx === ts.JsxEmit.Preserve ? ".jsx" : ".js";
    const target = resolvedFile.slice(0, -extension.length) + outputExtension;
    let value = relative(dirname(from), target).replaceAll("\\", "/");
    if (!value.startsWith(".")) value = `./${value}`;
    return value;
}

function detectCycles(edges: ReadonlyMap<string, readonly string[]>): void {
    const active = new Set<string>();
    const complete = new Set<string>();
    const visit = (file: string): void => {
        if (active.has(file)) throw new Error(`provider import cycle includes ${file}`);
        if (complete.has(file)) return;
        active.add(file);
        for (const target of edges.get(file) ?? []) visit(target);
        active.delete(file);
        complete.add(file);
    };
    for (const file of edges.keys()) visit(file);
}
