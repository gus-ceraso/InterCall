export type ErrorCode =
    | "invalid_argument"
    | "binding_mismatch"
    | "connection_closed"
    | "request_ids_exhausted"
    | "protocol"
    | "interface_mismatch"
    | "transport"
    | "resource_limit"
    | "aborted";

export class InterCallError extends Error {
    readonly code: ErrorCode;

    constructor(code: ErrorCode, message: string, options?: { readonly cause?: unknown }) {
        super(message, options);
        this.name = new.target.name;
        this.code = code;
        Object.setPrototypeOf(this, new.target.prototype);
    }
}

export class InvalidArgumentError extends InterCallError {
    constructor(message = "intercall: invalid argument", options?: { readonly cause?: unknown }) {
        super("invalid_argument", message, options);
    }
}

export class BindingMismatchError extends InterCallError {
    constructor(message = "intercall: binding mismatch", options?: { readonly cause?: unknown }) {
        super("binding_mismatch", message, options);
    }
}

export class ConnectionClosedError extends InterCallError {
    constructor(message = "intercall: connection closed", options?: { readonly cause?: unknown }) {
        super("connection_closed", message, options);
    }
}

export class RequestIDsExhaustedError extends InterCallError {
    constructor(message = "intercall: request IDs exhausted", options?: { readonly cause?: unknown }) {
        super("request_ids_exhausted", message, options);
    }
}

export class ProtocolError extends InterCallError {
    constructor(message = "intercall: protocol error", options?: { readonly cause?: unknown }) {
        super("protocol", message, options);
    }
}

export class InterfaceMismatchError extends InterCallError {
    constructor(message = "intercall: interface mismatch", options?: { readonly cause?: unknown }) {
        super("interface_mismatch", message, options);
    }
}

export class TransportError extends InterCallError {
    constructor(message = "intercall: transport error", options?: { readonly cause?: unknown }) {
        super("transport", message, options);
    }
}

export class ResourceLimitError extends InterCallError {
    constructor(message = "intercall: resource limit", options?: { readonly cause?: unknown }) {
        super("resource_limit", message, options);
    }
}

export class InterCallAbortError extends InterCallError {
    constructor(cause: unknown) {
        super("aborted", "The operation was aborted", { cause });
        this.name = "AbortError";
    }
}

export class RemoteException extends Error {
    readonly key: bigint;

    constructor(name: string, key: bigint) {
        super(name);
        this.name = name;
        this.key = key;
        Object.setPrototypeOf(this, new.target.prototype);
    }
}

export const ProcedureNotFound = Object.freeze(new RemoteException("procedure_not_found", 0x970e76fcc5e2dacbn));
export const InvalidArguments = Object.freeze(new RemoteException("invalid_arguments", 0x3f5fc972f8477b07n));
export const InternalException = Object.freeze(new RemoteException("internal_exception", 0x1aaec22e85996f50n));

export const procedureNotFound = ProcedureNotFound;
export const invalidArguments = InvalidArguments;
export const internalException = InternalException;
