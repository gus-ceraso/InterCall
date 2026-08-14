export interface LogicalDiagnostic {
    readonly path: string;
    readonly line: number;
    readonly column: number;
    readonly message: string;
}

export function sortDiagnostics(diagnostics: readonly LogicalDiagnostic[]): LogicalDiagnostic[] {
    return [...diagnostics].sort((left, right) => compareText(left.path, right.path) || left.line - right.line || left.column - right.column || compareText(left.message, right.message));
}

function compareText(left: string, right: string): number {
    return left < right ? -1 : left > right ? 1 : 0;
}

export function formatDiagnostics(diagnostics: readonly LogicalDiagnostic[]): string {
    return sortDiagnostics(diagnostics).map((diagnostic) => `${diagnostic.path}:${diagnostic.line}:${diagnostic.column}: ${diagnostic.message}`).join("\n");
}
