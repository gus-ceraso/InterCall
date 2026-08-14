export interface ExportHandlerContext {
    readonly signal: AbortSignal;
}

export type ExportProvider = (context: ExportHandlerContext, ...arguments_: readonly unknown[]) => Promise<unknown>;

export async function invokeExportProvider(provider: ExportProvider, signal: AbortSignal, arguments_: readonly unknown[]): Promise<unknown> {
    const context = Object.freeze({ signal });
    const result = provider(context, ...arguments_);
    if (!(result instanceof Promise)) throw new Error("provider must return Promise");
    return await result;
}
