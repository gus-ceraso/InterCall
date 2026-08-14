export type Int32 = number;
export type EmptyRecord = { readonly [name: string]: never };

export interface HandlerContext {
    readonly signal: AbortSignal;
}

export abstract class PayloadException<T> extends Error {
    readonly payload: T;
    constructor(payload: T) {
        super("payload exception");
        this.payload = payload;
    }
}
