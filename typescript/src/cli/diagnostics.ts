export interface LogicalDiagnostic {
    readonly path: string;
    readonly line: number;
    readonly column: number;
    readonly message: string;
}

export function sortDiagnostics(diagnostics: readonly LogicalDiagnostic[]): LogicalDiagnostic[] {
    return [...diagnostics].sort((left, right) => left.path.localeCompare(right.path) || left.line - right.line || left.column - right.column || left.message.localeCompare(right.message));
}

export function formatDiagnostics(diagnostics: readonly LogicalDiagnostic[]): string {
    return sortDiagnostics(diagnostics).map((diagnostic) => `${diagnostic.path}:${diagnostic.line}:${diagnostic.column}: ${diagnostic.message}`).join("\n");
}
