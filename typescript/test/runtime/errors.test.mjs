import assert from "node:assert/strict";
import test from "node:test";
import {
    BindingMismatchError,
    InterCallAbortError,
    InterCallError,
    InternalException,
    InvalidArguments,
    ProcedureNotFound,
    ProtocolError,
    RemoteException,
    ResourceLimitError,
} from "../../dist/runtime/index.js";

test("local errors expose stable codes and standard causes", () => {
    const cause = { reason: "test" };
    const error = new ProtocolError("bad frame", { cause });
    assert.equal(error instanceof InterCallError, true);
    assert.equal(error.code, "protocol");
    assert.equal(error.cause, cause);
    assert.equal(error.name, "ProtocolError");
    assert.equal(new BindingMismatchError().code, "binding_mismatch");
    assert.equal(new ResourceLimitError().code, "resource_limit");
});

test("abort errors preserve arbitrary reasons and fixed wire errors are singletons", () => {
    const reason = "cancelled";
    const aborted = new InterCallAbortError(reason);
    assert.equal(aborted.name, "AbortError");
    assert.equal(aborted.code, "aborted");
    assert.equal(aborted.cause, reason);
    assert.equal(ProcedureNotFound, ProcedureNotFound);
    assert.equal(InvalidArguments, InvalidArguments);
    assert.equal(InternalException, InternalException);
    assert.equal(Object.isFrozen(ProcedureNotFound), true);
    assert.equal(ProcedureNotFound.key, 0x970e76fcc5e2dacbn);
    assert.equal(ProcedureNotFound instanceof RemoteException, true);
});
