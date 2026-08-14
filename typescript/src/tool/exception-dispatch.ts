export interface ExportExceptionSpec {
    readonly name: string;
    readonly noPayload?: unknown;
    readonly payloadClass?: abstract new (...arguments_: any[]) => unknown;
}

export interface MatchedExportException {
    readonly spec: ExportExceptionSpec;
    readonly value: unknown;
    readonly payload: unknown;
}

export function matchExportException(error: unknown, specifications: readonly ExportExceptionSpec[]): MatchedExportException | undefined {
    const matches: MatchedExportException[] = [];
    for (const spec of specifications) {
        if (spec.noPayload !== undefined && error === spec.noPayload) matches.push({ spec, value: error, payload: undefined });
        else if (spec.payloadClass !== undefined && error instanceof spec.payloadClass) matches.push({ spec, value: error, payload: (error as { readonly payload?: unknown }).payload });
    }
    if (matches.length > 1) throw new Error(`ambiguous exception match: ${matches.map((match) => match.spec.name).join(", ")}`);
    return matches[0];
}
