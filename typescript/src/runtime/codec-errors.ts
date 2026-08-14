export class CodecError extends Error {
    constructor(message: string, options?: { readonly cause?: unknown }) {
        super(message, options);
        this.name = "CodecError";
    }
}

export class CodecValueError extends CodecError {
    constructor(message: string, options?: { readonly cause?: unknown }) {
        super(message, options);
        this.name = "CodecValueError";
    }
}

export class CodecResourceError extends CodecError {
    constructor(message: string, options?: { readonly cause?: unknown }) {
        super(message, options);
        this.name = "CodecResourceError";
    }
}
