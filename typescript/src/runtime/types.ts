declare const exportBindingBrand: unique symbol;
declare const importBindingBrand: unique symbol;
declare const connectionBrand: unique symbol;

export type Int8 = number;
export type Int16 = number;
export type Int32 = number;
export type Int64 = bigint;
export type Uint8 = number;
export type Uint16 = number;
export type Uint32 = number;
export type Uint64 = bigint;
export type Float32 = number;
export type Float64 = number;
export type EmptyRecord = { readonly [name: string]: never };

export type InterfaceID = Uint8Array;

export interface ExportBinding {
    readonly [exportBindingBrand]: "ExportBinding";
}

export interface ImportBinding {
    readonly [importBindingBrand]: "ImportBinding";
}

export interface Connection {
    readonly [connectionBrand]: "Connection";
    readonly closed: Promise<Error>;
    close(): void;
}

export interface CallOptions {
    readonly signal?: AbortSignal;
}

export interface HandlerContext {
    readonly connection: Connection;
    readonly signal: AbortSignal;
}

export abstract class PayloadException<T> extends Error {
    readonly payload: T;

    protected constructor(payload: T) {
        super("payload exception");
        this.name = new.target.name;
        this.payload = payload;
        Object.setPrototypeOf(this, new.target.prototype);
    }
}
