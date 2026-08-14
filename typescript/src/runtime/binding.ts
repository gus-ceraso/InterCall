import type { Dispatch } from "../generated-spi/index.js";
import type { ExportBinding, ImportBinding } from "./types.js";

const bindingStates = new WeakMap<object, BindingState>();

interface BindingState {
    readonly kind: "export" | "import";
    readonly dispatch?: Dispatch;
    readonly interfaceID?: Uint8Array;
}

export function makeExportBinding(dispatch: Dispatch, interfaceID?: Uint8Array): ExportBinding {
    if (typeof dispatch !== "function") throw new TypeError("export binding dispatch must be a function");
    const copiedID = copyInterfaceID(interfaceID);
    const state: BindingState = copiedID === undefined
        ? { kind: "export", dispatch }
        : { kind: "export", dispatch, interfaceID: copiedID };
    return makeBinding(state) as ExportBinding;
}

export function makeImportBinding(interfaceID?: Uint8Array): ImportBinding {
    const copiedID = copyInterfaceID(interfaceID);
    const state: BindingState = copiedID === undefined
        ? { kind: "import" }
        : { kind: "import", interfaceID: copiedID };
    return makeBinding(state) as ImportBinding;
}

export function assertExportBinding(value: unknown): ExportBinding {
    const state = assertBinding(value);
    if (state.kind !== "export") throw new TypeError("expected an export binding");
    return value as ExportBinding;
}

export function assertImportBinding(value: unknown): ImportBinding {
    const state = assertBinding(value);
    if (state.kind !== "import") throw new TypeError("expected an import binding");
    return value as ImportBinding;
}

export function bindingInterfaceID(value: ExportBinding | ImportBinding): Uint8Array | undefined {
    return copyInterfaceID(assertBinding(value).interfaceID);
}

export function bindingDispatch(value: ExportBinding): Dispatch {
    const state = assertBinding(value);
    if (state.kind !== "export" || state.dispatch === undefined) throw new TypeError("expected an export binding");
    return state.dispatch;
}

function makeBinding(state: BindingState): ExportBinding | ImportBinding {
    const handle = Object.freeze({});
    bindingStates.set(handle, Object.freeze(state));
    return handle as ExportBinding | ImportBinding;
}

function assertBinding(value: unknown): BindingState {
    if ((typeof value !== "object" && typeof value !== "function") || value === null) {
        throw new TypeError("invalid InterCall binding");
    }
    const state = bindingStates.get(value);
    if (state === undefined) throw new TypeError("invalid InterCall binding");
    return state;
}

function copyInterfaceID(value: Uint8Array | undefined): Uint8Array | undefined {
    if (value === undefined) return undefined;
    if (!(value instanceof Uint8Array) || value.byteLength !== 32) {
        throw new TypeError("interface ID must be exactly 32 bytes");
    }
    return value.slice();
}
