# InterCall TypeScript Implementation Plan

## 1. Purpose and authority

This plan defines an implementation of InterCall for TypeScript applications
running in web browsers. A browser peer must be able to import procedures from
and export procedures to the existing Go implementation over a browser
WebSocket. Calls remain bidirectional after the browser establishes the
connection: the browser may call Go, Go may call the browser, and either side
may make a nested call while handling an incoming call.

The implementation must follow these sources in order:

1. [`../README.md`](../README.md) is normative for the InterCall interface
   language and wire protocol.
2. [`../go/SPEC.md`](../go/SPEC.md) defines the compatibility profile used by
   the existing Go generator, runtime, fixed exceptions, interface IDs, and
   negotiated WebSocket transport.
3. A new `SPEC.md` in this directory will define only the TypeScript-native
   mapping and browser-specific behavior. It must not redefine the interface
   language or wire format.
4. A new `TYPESCRIPT.md` will be the user-facing build and usage guide.

If the documents disagree about the language or wire protocol, the repository
README wins. TypeScript-specific behavior may differ where the browser API has
no Go equivalent, but every such difference must be explicit in
`typescript/SPEC.md` and covered by a Go/TypeScript interoperability test.

## 2. Committed scope

The TypeScript implementation will provide:

- the complete `.intercall` scanner, parser, validator, semantic documentation
  attachment model, and canonical formatter;
- FNV-0 procedure and exception keys and SHA-256 canonical interface IDs;
- `intercall-ts import`, generating TypeScript types, exceptions, a static
  import binding, typed positional client methods, and static codecs from an
  interface file;
- `intercall-ts export`, discovering tagged TypeScript providers, application
  exceptions, and reachable types through the TypeScript compiler API, then
  generating an interface file and static export binding;
- one symmetric, bidirectional connection runtime with concurrent outgoing
  calls and concurrent incoming handlers;
- raw and negotiated browser WebSocket client constructors;
- the fixed 64 MiB frame-payload ceiling, explicit browser implementation-safety
  limits, and the three fixed Go-profile wire exceptions;
- deterministic, generator-owned artifacts with safe replacement;
- browser tests and black-box interoperability tests against the Go
  implementation in both call directions.

The initial implementation will not provide:

- a WebSocket, HTTP, TCP, Unix-socket, or other listening server in TypeScript;
- Node.js, Deno, Bun, WebTransport, or service-worker transports;
- CommonJS output;
- reconnect, retry, pooling, session resumption, or offline delivery;
- authentication or authorization policy;
- transport-level or wire-level cancellation;
- streaming parameters or results;
- an interpreter for making untyped calls from arbitrary runtime interface
  text; generated bindings remain the application API.

The Node.js requirement applies only to the build-time CLI. Runtime code emitted
into a browser bundle must not import Node built-ins, the TypeScript compiler,
CLI code, or filesystem code.

## 3. Recommended package and toolchain baseline

Use one npm package named `@cerasos/intercall`, subject to final registry
availability before publication.

- Package format: ESM only (`"type": "module"`).
- Browser compilation target: ES2022 with DOM library declarations.
- CLI runtime: Node.js 22 or newer.
- Package manager: npm with a checked-in `package-lock.json`.
- Browser runtime dependencies: none.
- CLI dependency: one exact, pinned `typescript` version for compiler-API
  discovery and generated-source validation.
- Development dependencies: exact pinned versions of `@types/node` and
  Playwright. Prefer the Node test runner over another unit-test framework.
- Production exports:
  - `@cerasos/intercall` for value markers, bindings, connection APIs, errors,
    and handler/call types;
  - `@cerasos/intercall/browser` for browser WebSocket construction;
  - `@cerasos/intercall/generated` for the stable generated-code SPI;
  - an `intercall-ts` executable for the CLI.
- Mark the package as side-effect-free. Generated bindings may construct and
  freeze immutable binding singletons at module initialization, but importing
  the browser runtime itself must not open a socket or mutate global state.

Do not add a browser polyfill for `BigInt`, `DataView` BigInt operations,
`TextEncoder`, `TextDecoder`, `AbortController`, or `WebSocket`. Test the
published browser matrix in current Chromium, Firefox, and WebKit through
Playwright.

## 4. Intended user experience

### 4.1 Importing a Go backend interface

```sh
intercall-ts import \
    --out frontend/src/generated/backend \
    api/backend.intercall
```

The generated directory contains one owned file:

```text
frontend/src/generated/backend/binding_gen.ts
```

It exports an immutable `importBinding` and a `createClient` factory. Procedure
parameters are positional because InterCall parameters are ordered and required
and because this mirrors the Go API. A final optional `CallOptions` value is
native API state, not a wire parameter.

```ts
import {
    createClient as createBackendClient,
    importBinding as backendImportBinding,
} from "./generated/backend/binding_gen.js";

const backend = createBackendClient(connection);
const greeting = await backend.hello("world");
const result = await backend.longOperation(value, { signal });
```

The factory keeps connection ownership explicit. It does not open or close a
connection. Generated procedure names are methods of the returned frozen client
facade, preventing top-level procedure declarations from colliding with
exported named types or exception classes.

### 4.2 Exporting browser procedures

A browser provider is an exported function tagged in JSDoc. It receives an
exact `HandlerContext` first, followed by ordered wire parameters, and returns a
`Promise<void>` or `Promise<T>`. It throws or rejects with declared application
exceptions.

```ts
import type { HandlerContext, Uint32 } from "@cerasos/intercall";

/**
 * Reports progress shown by the browser.
 * @intercall procedure report_progress
 * @param completed Completed work units.
 */
export async function reportProgress(
    context: HandlerContext,
    completed: Uint32,
): Promise<void> {
    if (context.signal.aborted) {
        throw context.signal.reason;
    }
    document.querySelector("#progress")!.textContent = String(completed);
}
```

Generate its interface and export binding with:

```sh
intercall-ts export \
    --project frontend/tsconfig.json \
    --out frontend/src/generated/browser \
    --interface api/browser.intercall \
    frontend/src/providers.ts
```

The generated binding imports the provider module statically and exports one
immutable `exportBinding`. There is no runtime registration or handler map.

### 4.3 Establishing a bidirectional browser connection

```ts
import { connectWebSocket } from "@cerasos/intercall/browser";
import {
    createClient as createBackendClient,
    importBinding as backendImportBinding,
} from "./generated/backend/binding_gen.js";
import {
    exportBinding as browserExportBinding,
} from "./generated/browser/binding_gen.js";

const connection = await connectWebSocket("/intercall", {
    exportBinding: browserExportBinding,
    importBinding: backendImportBinding,
});

const backend = createBackendClient(connection);
const greeting = await backend.hello("world");

window.addEventListener("pagehide", () => connection.close());
```

The matching Go server uses its generated backend export binding and generated
browser import binding:

```go
websocket.NewHandler(
    backendexport.ExportBinding(),
    browserimport.ImportBinding(),
)
```

`connectWebSocket` resolves relative URLs against the document and changes
`http:`/`https:` to `ws:`/`wss:`. It performs the Go client-role interface-ID
exchange before returning. A one-way browser may pass `emptyExportBinding`; a
Go server that does not call the browser uses `intercall.EmptyImportBinding()`.

## 5. TypeScript-native mapping to freeze in `SPEC.md`

### 5.1 Wire values

Use these public source marker aliases:

| InterCall | TypeScript API | Runtime value |
| --- | --- | --- |
| `int8` | `Int8` | `number` |
| `int16` | `Int16` | `number` |
| `int32` | `Int32` | `number` |
| `int64` | `Int64` | `bigint` |
| `uint8` | `Uint8` | `number` |
| `uint16` | `Uint16` | `number` |
| `uint32` | `Uint32` | `number` |
| `uint64` | `Uint64` | `bigint` |
| `float32` | `Float32` | `number` |
| `float64` | `Float64` | `number` |
| `string` | `string` | `string` |
| `bytes` | `Uint8Array` | `Uint8Array` |
| `list T` | `ReadonlyArray<T>` | JavaScript array |
| nonempty record | object type with readonly properties | plain object |
| `record {}` | `EmptyRecord` | plain empty object |
| omitted return | `void` | `undefined` |

The numeric marker aliases are structural aliases of `number` or `bigint`, not
branded or boxed values. They communicate exact wire intent to the export tool
without forcing casts in ordinary application arithmetic. Bare `number` and
`bigint` are rejected in hand-written export signatures because they do not
identify an exact InterCall primitive. `EmptyRecord` is an exact exported marker
for `record {}`; application values remain ordinary empty objects. The export
tool recognizes marker symbols by exact compiler identity from
`@cerasos/intercall`, including through untagged alias chains; same-named
lookalikes are rejected. Declare the public marker as
`type EmptyRecord = { readonly [name: string]: never }`, but recognize it by
exact symbol before ordinary index-signature rejection.

Generated encoders validate all numeric runtime values:

- integer `number` values must be finite integral values in the exact range;
- `bigint` values must be in the exact signed or unsigned 64-bit range;
- float values must be JavaScript numbers;
- `float32` values are rounded by IEEE binary32 encoding;
- every JavaScript NaN encodes as the required canonical quiet NaN;
- decoders inspect bits before conversion and reject noncanonical NaNs.

Named types are structural TypeScript aliases or interfaces. They do not add a
runtime wrapper or brand. Generated record fields use property-level `readonly`
modifiers, and generated lists use readonly array types, though JavaScript
object identity remains implementation-defined. `bytes` remains `Uint8Array`,
while `list uint8` remains `ReadonlyArray<Uint8>`; these forms must never
collapse.

Use the exact `EmptyRecord` marker for an empty record rather than `{}` or
`Record<string, never>`. TypeScript's `{}` accepts almost every non-null value,
while `Record` is a mapped generic type that the ordinary record projection does
not accept. A generated encoder must still validate values at runtime and reject
missing or extra own enumerable string fields. Record fields are encoded in
interface order, never JavaScript enumeration order.

### 5.2 Supported hand-written type forms

The export projection accepts:

- exact InterCall numeric marker aliases and the exact `EmptyRecord` marker;
- `string`;
- the exact global or imported `Uint8Array` type for `bytes`;
- `Array<T>`, `ReadonlyArray<T>`, `T[]`, and `readonly T[]` for lists;
- anonymous object type literals with required properties for nonempty inline
  records;
- exported, nongeneric type aliases and interfaces tagged with
  `@intercall type [wire_name]` for named types;
- untagged, nonrecursive type aliases and interfaces as flattened aliases;
- required identifier-named object properties, with property-level `readonly`
  ignored for wire purposes;
- the exact global `Promise<T>` in a provider return and the exact runtime
  `PayloadException<T>` base in an exception declaration, only in those
  enclosing positions.

It rejects bare `number`/`bigint`, bare `{}` as an empty record, optional
properties, unions, intersections, `null`, `undefined`, tuples, enums, arrays of
fixed length, index signatures, methods, getters/setters, call signatures,
constructors, classes as ordinary records, all other generic declarations or
instantiations, conditional and mapped types, utility types such as `Readonly<T>`
and `Record<K, T>`, `any`, `unknown`, `never`, `object`, symbols, functions,
recursive graphs, and all platform types not explicitly mapped.

Every reachable source declaration is checked, including declarations made
reachable only by an exception payload. The export tool must use TypeScript
compiler objects for semantic identity but retain source AST nodes for source
order, exact spelling, documentation, directives, and physical diagnostics.

### 5.3 Procedures and handler context

A selected provider is an exported, top-level, nongeneric, non-overloaded,
non-rest function declaration in an explicit source file. Its signature is:

```ts
function P(HandlerContext, P1, ..., Pn): Promise<void>
function P(HandlerContext, P1, ..., Pn): Promise<T>
```

The first parameter must have the exact exported runtime `HandlerContext` type.
Every wire parameter must have a nonoptional, non-rest identifier name. The
return must be the exact global `Promise` with zero or one supported wire value;
thenables and unions such as `Promise<T | undefined>` are rejected. Providers
may use `async` or return a `Promise` explicitly.

`HandlerContext` contains:

```ts
interface HandlerContext {
    readonly connection: Connection;
    readonly signal: AbortSignal;
}
```

The signal aborts when the handler finishes or the connection terminates. It is
local runtime state and is never encoded. The bound connection permits nested
and reverse-direction calls by passing it to a generated `createClient`.

### 5.4 Application exceptions

Use two source forms, preserving the distinction between no payload and a
zero-width payload:

```ts
/** @intercall exception denied */
export const Denied = new Error("denied");

/** @intercall exception failed */
export class Failed extends PayloadException<{
    readonly code: Int32;
    readonly detail: string;
}> {}

/** @intercall exception blank */
export class Blank extends PayloadException<EmptyRecord> {}
```

- A no-payload exception is one exported `const` value assignable to `Error`.
  Generated dispatch compares the thrown value to the sentinel with `===`.
- A payload exception is one exported, nongeneric class directly extending the
  exact runtime `PayloadException<T>`, where `T` is one supported wire type.
  Generated dispatch tests `instanceof` and encodes its `payload` property.
- `PayloadException<EmptyRecord>` is a payload-bearing zero-width exception and
  remains distinct from a no-payload sentinel.
- Dispatch evaluates every application exception in the interface. Exactly one
  direct match is required. No match, multiple matches through class
  inheritance, a non-`Error` rejection, a payload encoding failure, or an
  exception while matching maps to `internal_exception`.
- Fixed runtime exceptions are selected only by runtime conditions and cannot
  be thrown by a provider to force a wire result.

Generated imports map no-payload application exceptions to exported singleton
`RemoteException` values and payload exceptions to exported
`RemotePayloadException<T>` subclasses with a typed `.payload`. Record payloads
remain inside `.payload`; do not flatten fields onto the `Error` object because
a wire field such as `message`, `name`, `cause`, or `stack` would collide with
JavaScript `Error` state. Fixed exceptions map to process-wide runtime
singletons.

### 5.5 Naming and directives

Reuse the Go profile's fixed initialism list and ASCII conversion rules.
Defaults are:

- TypeScript type and exception declarations: Pascal case to lower snake case;
- TypeScript procedure functions, parameters, and fields: lower camel case to
  lower snake case;
- wire types and exception symbols: Pascal case;
- wire procedures, parameters, and fields: lower camel case.

Require explicit overrides when a valid InterCall name is not canonical or a
TypeScript identifier would be invalid or colliding. Do not silently append
numbers to public names.

Recognize complete JSDoc tag lines with these forms:

```text
@intercall procedure [wire_name]
@intercall exception [wire_name]
@intercall type [wire_name]
@intercall param TypeScriptName wire_name
@intercall field wire_name
@param TypeScriptName text
@returns text
```

`@intercall field` applies to the one property carrying the JSDoc. Declaration
and parameter overrides bypass default conversion. All malformed, duplicate,
contradictory, misplaced, unresolved, or InterCall-looking unknown directives
are errors at exact source positions. Remove InterCall directives from retained
prose. Use declaration prose, `@param`, `@returns`, and property prose to fill
the semantic documentation slots that TypeScript source can represent; leave
other nested type-occurrence slots empty.

Import-side `--ts-name` selectors use the same wire-path grammar as Go's
`--go-name`, replacing only the native identifier and never wire names, keys,
interface bytes, or IDs.

## 6. Browser runtime contracts

### 6.1 Bindings

Implement opaque immutable `ExportBinding` and `ImportBinding` objects. Object
reference identity is process-local binding identity. Keep optional interface
metadata separate so an all-zero ID can be present. Freeze public handles and
store dispatch and metadata in module-private state that application code cannot
forge by structural assignment.

Expose generated-code constructors equivalent to the Go SPI:

```ts
createExportBinding(dispatch): ExportBinding
createExportBindingWithInterfaceID(dispatch, id): ExportBinding
createImportBinding(): ImportBinding
createImportBindingWithInterfaceID(id): ImportBinding
emptyExportBinding: ExportBinding
emptyImportBinding: ImportBinding
```

The empty bindings are process-wide singletons for the exact canonical body:

```text
exception internal_exception;

exception invalid_arguments;

exception procedure_not_found;
```

Embed and test the same ID as Go:
`c31c470dd8db21db3bc8709bdcad7778a3d2dead33193c95b9691a4f0ba50dc8`.

### 6.2 Public connection API

The browser-facing API is:

```ts
interface ConnectionBindings {
    readonly exportBinding: ExportBinding;
    readonly importBinding: ImportBinding;
}

interface CallOptions {
    readonly signal?: AbortSignal;
}

interface WebSocketConnectionOptions {
    readonly signal?: AbortSignal;
    readonly protocols?: string | readonly string[];
    readonly openTimeoutMs?: number;        // default 10,000
    readonly negotiationTimeoutMs?: number; // default 10,000
    readonly messageLimit?: number;         // default 64 MiB + 24
}

function connectWebSocket(
    url: string | URL,
    bindings: ConnectionBindings,
    options?: WebSocketConnectionOptions,
): Promise<Connection>;

function connectRawWebSocket(
    url: string | URL,
    bindings: ConnectionBindings,
    options?: Omit<WebSocketConnectionOptions, "negotiationTimeoutMs">,
): Promise<Connection>;

class Connection {
    close(): void;
    readonly closed: Promise<Error>;
}
```

`connectWebSocket` applies `openTimeoutMs` to the WebSocket opening phase and,
after opening succeeds, applies a fresh `negotiationTimeoutMs` to interface
agreement. `connectRawWebSocket` applies only the opening timeout. Both phases
race the caller's signal, and zero, negative, nonfinite, or nonintegral timeout
values are invalid.

Generated code calls the `call(connection, binding, key, encode, decode)` bridge
exported only from `@cerasos/intercall/generated`. The bridge validates opaque
connection and binding identity before entering the connection state machine.
Do not expose an untyped `Connection.call` method from the application entry
point.

`close()` synchronously publishes the local closed cause and initiates a browser
WebSocket close; it does not wait for the close event, handlers, or queued
browser networking. `closed` resolves, never rejects, with the permanent first
terminal cause after socket cleanup and receive-loop shutdown. It does not wait
for handlers that ignore their abort signal. Repeated `close()` calls are
idempotent.

### 6.3 Errors and cancellation

Define stable error classes with a readonly `code` and standard `cause` where a
wrapped error exists. Include inherited Go classifications and the one
browser-specific resource classification:

- invalid argument;
- binding mismatch;
- explicit connection close;
- request-ID exhaustion;
- protocol failure;
- interface mismatch;
- transport failure;
- browser implementation-safety resource exhaustion.

Use process-wide singleton `RemoteException` values for
`procedure_not_found`, `invalid_arguments`, and `internal_exception`.
Application code may compare no-payload remote exceptions by identity and
payload exceptions with `instanceof`.

Per-call cancellation rejects with the exact `signal.reason`; if it is
`undefined`, create a standard `DOMException("The operation was aborted",
"AbortError")`. It retires the request ID without terminating the connection or
sending a cancellation frame.

A connection's permanent cause is always an `Error`, matching
`closed: Promise<Error>`. If a connection-level signal's reason is already an
`Error`, preserve that object. Otherwise wrap the value, including `undefined`,
in a stable `InterCallAbortError` whose `name` is `"AbortError"` and whose
`cause` is the original value. The normalized connection cause competes in
first-terminal-cause selection.

### 6.4 Frames, ownership, and IDs

Use `bigint` internally for procedure keys, exception keys, request IDs, wire
lengths, and counts. Never convert a wire `uint64` to `number` before checking
its native and local bounds.

- Parse the exact 24-byte little-endian header.
- Accept frame payloads only through exactly 67,108,864 bytes.
- Allocate outgoing request IDs monotonically from `0n` through
  `0x7fffffffffffffffn` and never reuse them, including after cancellation.
- Keep incoming and outgoing ID spaces independent.
- Buffer a complete frame before dispatch or response lookup.
- Ignore an unmatched response after framing, without decoding its exception
  key or payload.
- Decode a matched response in the sole receive path; any malformed selected
  value, unknown exception key, or trailing byte terminates the connection.
- Copy or transfer every incoming message/frame payload so generated decoders
  may safely retain bytes. Never reuse a mutable frame buffer visible to a
  result.
- Encode each complete request or response before write admission.
- Serialize requests and responses through one connection-wide send gate.
- Before sending, wait at the gate until adding the complete frame would keep
  `WebSocket.bufferedAmount` at or below the fixed browser send-buffer ceiling.
  The wait observes terminal selection and, for outgoing calls, cancellation.
- Recheck `WebSocket.readyState === WebSocket.OPEN`, insert a pending call, and
  allocate its ID immediately before `WebSocket.send`.
- If `send` throws or the socket is no longer open, select a terminal transport
  cause.

The browser `WebSocket.send` method reports queue admission rather than network
completion and has no completion callback. Treat a successful return from
`send` as the local completion point corresponding to a successful Go stream
write. JavaScript cannot process another message event during the synchronous
`send`, so a duplicate incoming request cannot be observed inside that write
interval. A duplicate observed before response send admission is terminal; reuse
observed after a successful `send` is accepted. Document this browser-binding
interpretation in `SPEC.md`.

Use these fixed, nonconfigurable browser implementation-safety limits:

| Limit | Value |
| --- | ---: |
| native queued outgoing application bytes | `134,217,776` (`2 * (64 MiB + 24)`) |
| owned encoded frame bytes awaiting send | `134,217,776` (`2 * (64 MiB + 24)`) |
| queued unread incoming bytes | `134,217,808` (`2 * (64 MiB + 24) + 32`) |
| active incoming request payload bytes | `134,217,728` (`2 * 64 MiB`) |
| active incoming handlers | `256` |
| outstanding outgoing calls | `1,024` |
| codec value nodes per payload | `1,048,576` |

Before a send, require
`bufferedAmount + frame.byteLength <= 134,217,776`; otherwise keep the send gate
and poll at a fixed ten-millisecond interval while also observing terminal state
and cancellation. Clear every poll timer on success, cancellation, or teardown.
An outgoing call reserves one of the 1,024 call slots after validation and ready
checks but before encoding, and releases it on every local error, response,
cancellation, or terminal outcome. After encoding, a local request reserves its
complete frame length against the owned-frame-byte limit before waiting for the
send gate; if it cannot, the call fails locally with `ResourceLimitError` without
an ID. Release owned-frame bytes immediately after `send` returns or fails.
Generated handler dispatch applies the same owned-frame accounting to responses;
an oversized or over-budget provider result falls back to the small
`internal_exception` response.

Incoming admission reserves both a handler slot and the complete request payload
length until that handler sends or abandons its response. A request above either
active-handler bound, or unread input above the receive-byte limit, terminates
the connection with `ResourceLimitError`. These ceilings are TypeScript
implementation-safety bounds permitted by the protocol, not wire fields or
configurable policy.

These application-level checks cannot bound bytes already hidden in a browser's
native WebSocket implementation, because the standard browser WebSocket API has
no receive backpressure. State that residual risk in `SPEC.md`; process each
binary message promptly and close as soon as a visible bound is exceeded.

### 6.5 Browser WebSocket adaptation

Set `binaryType = "arraybuffer"` before processing messages. Reject text
messages. Treat WebSocket messages as arbitrary ordered byte chunks:

- one InterCall frame may span messages;
- one message may contain several frames;
- negotiation bytes and the first frame may be adjacent in the byte queue;
- message boundaries never drive frame parsing.

Maintain one chunk queue with incremental reads rather than repeatedly
concatenating the entire buffered stream. Before retaining a message, check that
its bytes plus the queue's unread bytes do not exceed 134,217,808; select
`ResourceLimitError` and close otherwise. Enforce the configured per-message
limit after receipt; the default is 67,108,888 bytes, matching the Go WebSocket
transport. The frame payload and aggregate queue ceilings remain fixed and
nonconfigurable.

Negotiated client setup must:

1. validate both bindings and require present 32-byte interface IDs before
   constructing a WebSocket;
2. race opening the browser WebSocket against the caller's signal and a separate
   ten-second open timer, then establish binary mode;
3. after open succeeds, start a fresh ten-second negotiation timer;
4. send exactly the import binding's ID as one binary message;
5. read exactly 32 bytes from the ordered chunk queue;
6. compare those bytes to the export binding's ID in constant-length byte
   comparison;
7. preserve any queued bytes after the ID for the frame receiver;
8. race negotiation against the caller's signal and negotiation timer;
9. publish `InterfaceMismatchError` on a locally observed mismatch and close the
   owned socket on every open or setup failure;
10. hand the socket and residual byte queue to the ordinary connection core on
    success.

Interface IDs are mismatch detectors, not credentials. Browser WebSockets
cannot set arbitrary HTTP headers. Document same-origin cookies, query tokens,
and server-side HTTP authentication as deployment concerns; recommend `wss:`
and the Go handler's same-origin default.

## 7. Codec design

Generate immutable, flat codec programs and execute them with iterative runtime
encoders/decoders. Do not use JavaScript reflection to infer schemas from values
at runtime. The generator determines every operation, field name, named-type
reference, list edge, and zero-width property ahead of time. A flat program
avoids JavaScript call-stack dependence on deeply nested interface types and
keeps generated source smaller than one recursive function per occurrence.

The codec VM must:

- append to an owned growable byte buffer with checked arithmetic;
- encode and decode exact-width little-endian integers;
- use two's-complement signed integer bit patterns;
- canonicalize encoded NaNs and reject every noncanonical decoded NaN pattern;
- preserve positive and negative zero and infinities;
- validate JavaScript UTF-16 strings for unpaired surrogates before
  `TextEncoder`, because `TextEncoder` would otherwise replace them;
- use fatal `TextDecoder` behavior and reject every invalid UTF-8 input;
- count string lengths in UTF-8 bytes;
- distinguish `Uint8Array` bytes from arrays of `Uint8`;
- encode records in declared order and validate closed required fields;
- check every length, count, addition, multiplication, buffer growth, array
  length, and typed-array allocation before conversion or allocation;
- consume a selected payload exactly;
- decode empty lists and bytes to nonnull zero-length values;
- decode zero-width lists without a JavaScript per-element codec walk, using a
  precomputed frozen zero value and a native array fill where representable;
- maintain one value-node budget of exactly 1,048,576 for each complete request,
  response, or exception payload encode or decode, shared across all procedure
  parameter roots and named references, charging one node for each list
  container, list element, and record object before traversal or allocation;
- reject a list count that exceeds either the remaining value-node budget or
  JavaScript's maximum array length before allocation, including for zero-width
  elements;
- check fixed bounds before allocation and translate ordinary allocation
  `RangeError` failures into deterministic codec errors; do not rely on catching
  host out-of-memory termination;
- reject an outgoing payload above 64 MiB before a frame is admitted, since the
  Go peer cannot accept it;
- never expose a pooled or subsequently reused backing buffer.

A codec node-budget failure has the same enclosing outcome as another value
error: malformed incoming request arguments select `invalid_arguments`, a
matched malformed response terminates with `ProtocolError`, a provider return or
exception encoding failure selects `internal_exception`, and a local request
encoding failure rejects that call without allocating an ID.

Keep the generated-code SPI typed and minimal: codec programs, binding
constructors, and call/dispatch bridges only. Application code must not register
or construct descriptors manually.

## 8. CLI contracts

### 8.1 Import

```text
intercall-ts import --out DIR
    [--ts-name SELECTOR=TypeScriptIdentifier]...
    INTERFACE_FILE
```

- Require exactly one regular interface file; do not accept stdin.
- Read exact bytes, validate UTF-8 and syntax, attach semantic docs, and compute
  the canonical body without rewriting the source.
- Apply strict TypeScript projection and naming validation before filesystem
  mutation.
- Emit exactly `DIR/binding_gen.ts`.
- Emit all named types, application exception mappings, immutable codec
  programs, `importBinding`, and `createClient`.
- Embed the canonical-body SHA-256 interface ID and semantic metadata.
- Validate the complete generated TypeScript against a synthetic declaration of
  the generated-code SPI. Maintain a parity test between that synthetic model
  and the package's actual exported declarations.

### 8.2 Export

```text
intercall-ts export --project TSCONFIG --out DIR --interface FILE
    [--include SOURCE_FILE#Symbol]...
    [--exclude SOURCE_FILE#Symbol]...
    SOURCE_FILE...
```

- Require one `tsconfig.json`, at least one explicit source file, an owned
  output directory, and a distinct interface target.
- Build one TypeScript `Program` using the exact project options and active
  compiler version. Exclude test variants only by the user's project config;
  do not invent another build configuration.
- Explicit files must belong to the program and be implementation `.ts` or
  `.tsx` files. Declaration files, JavaScript files, and generated binding files
  are rejected. TSX providers are analyzed under the project's exact `jsx`
  compiler option.
- Select directly exported, tagged provider function declarations in explicit
  files. Re-exports, overload sets, methods, class members, arrow-function
  variables, and default exports are rejected in the initial profile.
- With no `--include`, select every eligible procedure in explicit files.
  Includes restrict procedures and excludes win. Filters do not remove tagged
  application exceptions.
- Treat every valid tagged exception in every explicit file as global to every
  selected procedure.
- Resolve reachable named and alias types through compiler symbols, checking
  project visibility, exportability, recursion, unsupported forms, generated
  metadata, and output-to-provider import cycles.
- Generate deterministic relative ESM imports from the binding to provider
  modules, using the extension the project emits: `.js` for `.ts` and
  transformed `.tsx`, and `.jsx` for `.tsx` under `jsx: "preserve"`. Validate
  that each emitted specifier resolves under the project configuration. Use
  stable private namespace aliases.
- Emit types in stable topological wire-name order, then exceptions and
  procedures in exact wire-name byte order. Insert the three fixed exceptions.
- Format the canonical interface, compute its SHA-256 ID, emit static codecs and
  dispatch, and validate both products in memory before filesystem mutation.
- Emit exactly `DIR/binding_gen.ts` and the specified interface file.

### 8.3 Diagnostics

All diagnostics use:

```text
path:line:column: message
```

Positions are one-based physical byte positions in the exact input file.
Interface diagnostics follow UTF-8 bytes. TypeScript diagnostics must derive
physical source offsets from source files and must not be rewritten by source
maps. Sort multiple diagnostics by logical slash-normalized path, line, column,
and message. Never include staging paths, absolute temporary directories, or
compiler object identities.

### 8.4 Generated ownership and replacement

Use exact first lines:

```text
// Code generated by intercall-ts; DO NOT EDIT.
// intercall-ts binding: import sha256:<artifact-id>
```

The second line says `export` for export bindings. Exported interfaces begin
with:

```text
/* Code generated by intercall-ts; artifact sha256:<artifact-id>; DO NOT EDIT. */
```

followed by one blank line and the canonical body. The artifact ID hashes the
canonical body, not the ownership marker.

Mirror the Go tool's safety properties:

- validate all source, interfaces, projections, generated bytes, and generated
  TypeScript before creating the output directory;
- complete ownership checks before creating or replacing target leaves;
- reject target symlinks, directories, devices, and other nonregular leaves;
- never follow a target-leaf symlink;
- never overwrite handwritten or differently generated files;
- use a dedicated output directory and reject code files other than the owned
  `binding_gen.ts` while preserving noncode entries;
- stage in each destination directory and replace by rename, never truncate an
  owned file in place or delete a target first;
- leave hard links to the previous inode untouched;
- do not replace unchanged bytes;
- never delete stale paths;
- for export's two targets, recognize differing valid stamps or one missing
  owned target as an interrupted update and repair both deterministically;
- fail safely on platforms that cannot atomically replace an existing leaf by
  rename;
- emit no timestamps, absolute paths, temporary paths, random private names, or
  map-order-dependent content.

## 9. Safe semantic metadata and TypeScript re-export

Generated import types may be used in a later TypeScript export provider. Carry
semantic metadata so re-export preserves exact wire structure and nested
attached documentation instead of reconstructing it from lossy generated
TypeScript prose.

- Embed one private unpadded base64url value containing the canonical semantic
  interface body, split into deterministic chunks no larger than 4,096 bytes.
- Mark generated named type rows with an exact machine-readable JSDoc line that
  is interpreted only inside a file carrying the exact InterCall generated-file
  marker.
- On first use of a generated type, validate the complete marked file and the
  bijection between machine rows and semantic type declarations before using
  any row.
- Require canonical base64url, valid UTF-8, a valid decoded interface, and
  byte-for-byte equality with canonical reformatting.
- Verify that the generated TypeScript type still projects to the same
  documentation-free wire structure before accepting metadata.
- Reject missing, extra, malformed, duplicate, conflicting, or misplaced rows,
  including rows not otherwise reached.
- Never interpret lookalike metadata in handwritten or third-party generated
  files.
- Never rescan decoded documentation as TypeScript directives.

Keep artifact ownership, process-local binding identity, and runtime interface
IDs separate even though they use the same canonical-body digest bytes.

## 10. Ordered implementation tasks

Complete the following phases in order. Keep every phase buildable and commit
source, tests, documentation, and regenerated fixtures together.

### Phase 0 — Freeze the TypeScript profile

1. **Done.** Create `typescript/SPEC.md` from Sections 2–9 of this plan,
   resolving the TypeScript/browser profile before runtime code depends on it.
2. **Done.** Create `typescript/TYPESCRIPT.md` with the bidirectional
   hello-world flow, build requirements, JSDoc directives, exception examples,
   generation commands, same-origin deployment guidance, and connection
   lifecycle.
3. **Done.** Record browser constraints that prevent literal Go API behavior:
   `WebSocket.send` has no completion callback, browser sockets cannot be
   force-closed synchronously, messages arrive already allocated, and custom
   upgrade headers cannot be supplied.
4. **Done.** Write small compiler experiments under test fixtures for:
   - exact marker-symbol recognition through alias chains;
   - JSDoc tag ranges and physical positions;
   - generated `.js`/`.jsx` relative specifiers resolving to `.ts` and `.tsx`
     providers under each supported `jsx` mode;
   - source projection of readonly arrays, type literals, interfaces,
     `EmptyRecord`, and `PayloadException<T>`;
   - generated alias/type chains at exactly 4,096 resolved occurrences.

   The fixtures pin TypeScript 5.9.3 and pass under the transformed and
   preserved JSX configurations.
5. **Done.** Adopt the Go profile's exact strict projection depth of 4,096
   occurrences. The pinned TypeScript 5.9.3 compiler compiles the tested
   4,096-edge alias chain, so the profile limit is retained rather than lowered
   to a compiler-specific value.
6. **Done.** Add a compatibility table mapping every normative README wire rule
   and every inherited Go-profile rule to its intended TypeScript implementation
   and test file. See `COMPATIBILITY.md`.

**Gate:** profile review complete; experiments run with the pinned compiler;
there are no unresolved wire, native mapping, exception, or public API choices.

### Phase 1 — Scaffold the package without browser side effects

1. **Done.** Add `package.json`, lockfile, build scripts, exports, executable
   mapping, license metadata, repository metadata, and Node/browser engine
   declarations.
2. **Done.** Add separate TypeScript configs for browser runtime, Node CLI,
   tests, and declaration emission. Enable strict checking,
   `noUncheckedIndexedAccess`, `exactOptionalPropertyTypes`, and deterministic
   casing checks.
3. **Done.** Create this source layout:

   ```text
   src/
       runtime/
       browser/
       syntax/
       tool/
       cli/
       generated-spi/
   test/
       syntax/
       codec/
       runtime/
       tool/
       browser/
       integration/
       fixtures/
   ```

4. **Done.** Add public numeric marker aliases, the exact `EmptyRecord` marker,
   placeholder interfaces, and export maps without importing Node code from
   browser entry points.
5. **Done.** Add a static import-boundary test that walks emitted browser
   modules and rejects `node:*`, all Node built-ins, CLI/tool dependencies, and
   every external browser dependency.
6. **Done.** Add build, unit-test, browser-test, fixture-check, and
   package-dry-run npm scripts. Current scaffold gates are executable without
   accidentally running TypeScript fixture sources as JavaScript.

**Gate:** `npm ci`, declaration build, browser build, an empty Node test run,
and `npm pack --dry-run` pass.

### Phase 2 — Port interface syntax and canonical semantics

1. **Done.** Implement a byte-oriented UTF-8 scanner preserving exact byte
   spans, comments, CR/LF behavior, reserved words, identifier rules, and EOF
   positions.
2. **Done.** Implement the grammar parser. It uses explicit stacks for
   unbounded list and record nesting; malformed input does not overflow the
   JavaScript call stack.
3. **Done.** Implement protocol validation for global/local scopes, earlier
   type references, duplicate names, all declarations, FNV-0 key zero, and
   cross-kind key collisions.
4. **Done.** Implement FNV-0 with `bigint` masked to 64 bits after
   multiplication.
5. **Done.** Implement semantic documentation grouping and normalization
   exactly as `go/SPEC.md`, including CRLF/bare-CR normalization, blank-line
   attachment, trailing comments, nested type occurrences, and one-use comment
   ownership.
6. **Done.** Implement the canonical formatter iteratively, including empty
   files, zero-field records, documented nested type occurrences, indentation,
   final newline rules, and discarded unattached comments.
7. **Done.** Implement canonical interface SHA-256 in Node tool code and a
   browser-free `InterfaceID` byte representation.
8. **Done.** Port every relevant fixture from `go/internal/syntax/testdata`,
   preserving raw bytes for BOM, invalid UTF-8, CRLF, and deep nesting cases.
   `test/fixtures/syntax-integrity.test.mjs` compares the complete corpus byte
   for byte.
9. **Done.** Add byte-for-byte differential tests against canonical Go
   formatter output, key vectors from the README, and the empty-interface ID.
10. **Done.** Add deterministic mutation/fuzz tests using the checked-in Go
    fuzz corpus and bounded seeded byte mutations.

**Gate:** all Go syntax fixtures produce the same validity, canonical bytes,
keys, semantic docs, and source positions, except diagnostic wording explicitly
specified as TypeScript-specific.

### Phase 3 — Implement naming, selectors, and the target-neutral generation records

1. **Done.** Port the fixed initialism table and checked ASCII
   lower-snake/Pascal/camel conversion algorithms.
2. **Done.** Implement TypeScript keyword and identifier checks without
   accepting quoted or computed names as a silent escape.
3. **Done.** Implement collision-safe deterministic private mangling using
   content-derived suffixes; public collisions remain diagnostics.
4. **Done.** Implement `--ts-name` parsing and the complete
   root/element/field selector path grammar.
5. **Done.** Define small command-specific generation records. Syntax AST
   nodes remain the source of wire order, names, docs, and positions; the
   records add only projected names, compiler-object slots, codec roots, and
   dispatch/provider facts.
6. **Done.** Implement the iterative strict projection-depth preflight and
   exact boundary diagnostics before recursive compiler/emitter work.
7. **Done.** Add unit tests for every initialism, inverse projection, invalid
   identifier, selector resolution, duplicate override, unresolved override,
   collision, and 4,096 boundary case.

**Gate:** naming and selectors are deterministic and all boundary tests run
without stack overflow.

### Phase 4 — Implement the codec VM and vectors

1. **Done.** Define a private immutable flat instruction format for primitive,
   list, record, named-reference, root, and zero-value operations.
2. **Done.** Implement a checked growable encoder buffer that never exposes
   reused storage.
3. **Done.** Implement numeric primitive encoders and decoders with `DataView`,
   explicit raw NaN bit checks, and exact numeric validation. String and bytes
   use the dedicated codecs implemented in the following tasks.
4. **Done.** Implement UTF-16 scalar validation, UTF-8 byte-length encoding,
   and fatal UTF-8 decoding.
5. **Done.** Implement bytes with defensive copies and list values with
   JavaScript arrays.
6. **Done.** Implement closed ordered records and exact payload exhaustion.
7. **Done.** Implement iterative list/record execution stacks, checked lengths
   and counts, iterative zero-width analysis, frozen zero values, and no
   per-element codec execution for zero-width lists.
8. **Done.** Implement the exact 1,048,576-node per-payload encode/decode
   budget, shared across all procedure parameter roots and named references,
   charging containers, list elements, and record objects before allocation or
   traversal.
9. **Done.** Enforce the node budget, JavaScript array limits, allocation
   checks, the outgoing payload ceiling, and deterministic codec/resource error
   classes.
10. **Done.** Generate codec programs in memory from syntax generation
    records.
11. **Done.** Port Go codec fixtures and add golden wire vectors for every
    primitive, signed boundaries, infinities, signed zero,
    canonical/noncanonical NaNs, invalid strings, bytes versus list-uint8,
    nested records, named chains, zero-width records/lists, truncation, trailing
    data, excessive counts, and allocation boundaries.
12. **Done.** Add focused zero-width-list and nested-container tests immediately
    below and above the node budget, proving rejection occurs before a large
    allocation.
13. **Done.** Add randomized Go/TypeScript round-trip vector exchange: Go
    writes fixture bytes decoded by TypeScript and TypeScript writes bytes
    decoded by Go.

**Gate:** codec vectors are byte-identical to Go for values within the fixed
TypeScript safety bounds. Tests classify node-budget rejection separately and
prove that it occurs before allocation; all other malformed values have the
same accept/reject classification.

### Phase 5 — Implement bindings, errors, and empty interfaces

1. **Done.** Implement opaque binding state with frozen public handles and
   reference identity checks.
2. **Done.** Implement metadata-free and metadata-aware import/export
   constructors, preserving presence separately from an all-zero ID.
3. **Done.** Implement fixed local error classes, including
   `InterCallAbortError` and `ResourceLimitError`, and process-wide fixed
   wire-exception singletons.
4. **Done.** Implement `PayloadException<T>`, generated remote payload
   exception support, and exact payload ownership.
5. **Done.** Implement the empty import/export singletons, fixed dispatch,
   exact canonical body, fixed keys, and Go-identical interface ID.
6. **Done.** Define and freeze the generated-code dispatch, request encoder,
   response decoder, and codec-program SPI.
7. **Done.** Add constructor validation, identity, copy/reference, zero-ID,
   singleton, fixed-key, and synthetic-SPI parity tests.

**Gate:** binding metadata and empty interface values match Go byte for byte and
cannot be forged through ordinary structural objects.

### Phase 6 — Implement the transport-independent connection state machine

1. **Done.** Build an internal ordered chunk transport abstraction used only
   between the connection core and browser WebSocket adapter; do not expose a
   general Node stream promise.
2. **Done.** Implement the two-state active/terminal lifecycle with one
   permanent first cause, terminal publication, pending-call transfer, handler
   abort, and one cleanup owner.
3. **Done.** Implement the sole incremental frame receiver over arbitrary
   chunks, exact header/payload reads, fixed payload ceiling, and owned payload
   transfer.
4. **Done.** Implement outgoing monotonic IDs, pending ownership, the exact
   1,024-call admission limit, response claims, per-call abort claims, terminal
   claims, and unmatched opaque responses.
5. **Done.** Implement generated call ordering: validation, ready check,
   outgoing-call slot reservation, one synchronous encode, post-encode check,
   owned-frame-byte reservation, frame construction, abortable gate/backpressure
   wait, final check, ID allocation/pending insertion, send admission,
   reservation release, and outcome wait.
6. **Done.** Implement the shared send gate for requests and responses,
   including the exact `bufferedAmount` ceiling, ten-millisecond drain polling,
   ready-state rechecks, and timer cleanup.
7. **Done.** Implement incoming request admission, active-ID tracking, the
   exact 256-handler and 134,217,728-byte active-payload limits, concurrent
   Promise handlers, per-handler `AbortSignal`, static dispatch invocation,
   response encoding, owned-frame accounting, and post-send ID/resource
   release.
8. **Done.** Map unknown procedures, malformed/trailing arguments,
   provider/matcher failures, and response-encoding failures to the three fixed
   exceptions.
9. **Done.** Catch every synchronous throw and rejected Promise crossing
   generated dispatch. Never let a provider failure escape the receive task.
10. Ensure a terminal connection prevents a completed late handler from sending
    a response.
11. Implement `close()` and `closed`, including normalization of arbitrary
    connection abort reasons to `Error` and handler-independent teardown.
12. Write deterministic fake-transport tests for:
    - concurrent and out-of-order calls;
    - simultaneous calls in both directions;
    - nested calls from a handler;
    - cancellation before encode, during encode recheck, at gate wait, and after
      send admission;
    - terminal/cancel/response races;
    - request-ID exhaustion, outgoing-call admission, and owned-frame-byte
      limits;
    - a native send buffer that never drains, drain cancellation, and timer
      cleanup;
    - duplicate incoming IDs before response admission and reuse after send;
    - the active-handler, active-request-payload, and aggregate receive-queue
      limits;
    - default, `Error`, string, numeric, and undefined connection abort reasons;
    - matched malformed versus unmatched opaque responses;
    - partial headers/payloads across chunks and several frames in one chunk;
    - handler rejection, codec failure, and late handler completion;
    - explicit close, transport close, protocol failure, and first-cause races.

Use barriers, manually controlled promises, and fake event queues; do not use
sleep-based race tests.

**Gate:** the runtime state-machine tests cover every ownership transition and
no promise is left unresolved after terminal teardown.

### Phase 7 — Implement browser WebSocket transport and negotiation

1. Implement URL resolution and `ws:`/`wss:` conversion for relative HTTP(S)
   locations while accepting explicit WebSocket URLs.
2. Validate timeout/message options, then open native WebSockets with optional
   subprotocols and set binary mode before data handling. Race opening against a
   fresh ten-second default timer and the caller's signal.
3. Convert message events into the ordered chunk queue, reject strings and
   unsupported binary values, and enforce both the per-message and exact
   aggregate unread-byte limits before retaining message data.
4. Translate error/close events into one transport cause without allowing a
   later browser event to replace an earlier local/protocol cause.
5. Implement raw connection construction beginning at the first frame.
6. After a successful open, implement client-role negotiation with exact
   32-byte records, a separate fresh ten-second default timer, `AbortSignal`,
   residual queue handoff, mismatch diagnostics, and complete failure cleanup.
7. Ensure event listeners are removed or made inert on teardown and no retained
   queue keeps payloads alive after close.
8. Add a fake WebSocket implementation for deterministic unit tests of open and
   negotiation timeouts, send throws, non-open sends, `bufferedAmount` drain and
   saturation, message ordering, aggregate queue overflow, text rejection,
   close races, and negotiation residual bytes.
9. Add Playwright tests in Chromium, Firefox, and WebKit for binary sends,
   chunk queues, abort behavior, relative URLs, and browser close behavior.

**Gate:** raw and negotiated browser connections pass the same runtime suite,
and browser entry points contain no Node imports.

### Phase 8 — Implement import generation in memory

1. Build import generation records from validated syntax and resolved named
   declarations.
2. Apply TypeScript projection depth, naming, scopes, overrides, fixed-exception
   shape checks, and helper collision checks.
3. Emit numeric and `EmptyRecord` marker imports and generated named type
   aliases/interfaces, preserving `bytes` versus `list uint8`, named references,
   property-level readonly records, and exact empty-record types.
4. Emit application exception singletons/classes and fixed-exception mappings.
5. Emit immutable flat codec programs for every request, return, and exception
   payload.
6. Emit one metadata-aware `importBinding` singleton.
7. Emit `createClient(connection)` returning a frozen object with one positional
   async method per procedure and a final optional `CallOptions` parameter.
8. Ensure a missing return produces `Promise<void>` and every successful response
   must have an exactly empty payload.
9. Embed canonical semantic metadata and machine type rows.
10. Format output with the generator's own deterministic emitter; do not depend
    on a user's formatter.
11. Parse and type-check complete output against the synthetic SPI before
    writing.
12. Add golden fixtures for empty and kitchen-sink interfaces, every exception
    shape, all naming overrides, deep types, helper collisions, docs/metadata,
    and generated-source determinism.
13. Compile generated fixtures under strict TypeScript settings and execute
    their codecs against the runtime tests.

**Gate:** checked-in generated import fixtures are byte-for-byte reproducible,
compile strictly, and call a fake Go-compatible peer successfully.

### Phase 9 — Implement TypeScript source discovery and export projection

1. Load the exact pinned compiler API and one `Program` from `--project` without
   mutating project files or invoking emit.
2. Normalize explicit `.ts` and `.tsx` source operands and deterministic
   logical paths, preserving each source's project `jsx` behavior.
3. Implement exact JSDoc directive scanning and physical source positions from
   source AST ranges.
4. Discover directly exported eligible procedures, no-payload sentinels,
   payload classes, and tagged named types.
5. Implement include/exclude filters and deterministic diagnostics for malformed,
   duplicate, unknown, ineligible, or non-explicit selectors.
6. Validate exact `HandlerContext`, `Promise`, marker aliases, `Uint8Array`,
   arrays, records, aliases, named types, and `PayloadException<T>` by compiler
   symbol identity.
7. Walk reachable type graphs iteratively, preserving source property order,
   flattening untagged aliases, retaining tagged types, rejecting cycles and
   unsupported TypeScript constructs, and enforcing the strict depth boundary.
8. Attach and normalize representable source documentation.
9. Compute default wire names, apply directives, check all wire/native scopes,
   and reserve fixed runtime names.
10. Determine deterministic runtime provider imports with the project's emitted
    `.js` or `.jsx` extension, and reject unresolvable specifiers, inaccessible
    modules, output/provider cycles, declaration-only providers, and ambiguous
    module identities.
11. Implement stable topological type ordering and sorted exception/procedure
    ordering.
12. Add compiler fixture projects covering `.ts` and `.tsx` providers, JSX
    transform and preserve modes, project references, path aliases, aliases,
    rejections, directives, Unicode source names, generated sources, classes,
    exceptions, recursion, overloads, rest/optional parameters, unsupported
    types, import cycles, and exact physical diagnostics.

**Gate:** discovery results are independent of filesystem enumeration and
compiler map order, and every supported/rejected source form has a focused
fixture.

### Phase 10 — Implement export generation and dispatch

1. Convert discovery results into a canonical interface AST and insert fixed
   exceptions.
2. Emit static provider imports using each module's validated deterministic
   `.js` or `.jsx` relative specifier.
3. Emit decode programs for request arguments and encode programs for return and
   exception values.
4. Emit a static procedure-key switch. Unknown keys return
   `procedure_not_found` after full framing.
5. Decode all arguments and require exact exhaustion before invoking a provider;
   malformed input returns `invalid_arguments`.
6. Construct one `HandlerContext`, invoke each provider with positional values,
   and await its exact Promise result.
7. Match no-payload exceptions by identity and payload exceptions by
   `instanceof`, requiring exactly one match.
8. Encode successful returns and matched exception payloads before returning to
   the runtime. Map provider, matching, and encoding failures to
   `internal_exception` with an empty payload.
9. Emit one metadata-aware `exportBinding` singleton with immutable dispatch.
10. Emit and validate the canonical interface and generated TypeScript entirely
    in memory.
11. Add golden generated export fixtures and execute dispatch directly for
    success, each declared exception shape, unknown key, malformed arguments,
    trailing bytes, rejected promises, ambiguous exception matches, invalid
    payloads, and return encoding failures.

**Gate:** generated export fixtures are deterministic, strict-compile, and their
raw dispatch bytes match Go-generated import expectations.

### Phase 11 — Implement artifact ownership and CLI commands

1. Implement argument parsing without a CLI framework, exact help text, repeatable
   flags, and exit status 0/1.
2. Implement logical diagnostics and deterministic multi-error sorting.
3. Implement source/generated validation sequencing so no validation error
   creates an output directory.
4. Implement ownership parsing, regular-leaf/symlink checks, output-directory
   collision rules, interrupted export detection, in-directory staging,
   byte-equality no-op, and rename replacement.
5. Connect the in-memory import pipeline to `intercall-ts import`.
6. Connect discovery/export pipelines to `intercall-ts export`.
7. Add filesystem tests for handwritten collisions, malformed markers, wrong
   stamps, symlinks, directories, devices where supported, hard links,
   permissions, unchanged output, interrupted two-target updates, failed rename,
   and no-deletion guarantees.
8. Add CLI example tests, help snapshots, deterministic repeated-run tests, and
   tests proving diagnostics never expose staging or absolute temporary paths.
9. Run generated-source validation before every fixture write and make ordinary
   fixture tests compare only; use a separate explicit maintenance command for
   regeneration.

**Gate:** both commands satisfy validation-before-mutation and safe replacement
contracts on Linux, macOS, and Windows CI.

### Phase 12 — Implement generated metadata re-export

1. Emit canonical semantic metadata and generated machine rows from import.
2. Detect the exact generated marker when export reaches a generated type.
3. Validate the complete marked file before consuming one row.
4. Decode, parse, validate, and recanonicalize metadata.
5. Verify the machine-row bijection and TypeScript/wire structural parity.
6. Transfer exact nested semantic documentation to the export AST without
   directive rescanning.
7. Add round-trip tests:
   `.intercall -> TypeScript import -> TypeScript export -> .intercall`, requiring
   byte-identical canonical semantic bodies and IDs.
8. Add hostile metadata tests for malformed base64url, invalid UTF-8, duplicate
   constants/rows, unknown/missing/extra rows, forged handwritten markers,
   stale structure, directive-like docs, and otherwise unreachable malformed
   rows.

**Gate:** safe generated re-export preserves all semantic slots and rejects every
forged or stale metadata case before output mutation.

### Phase 13 — Add Go/browser interoperability fixtures

1. Create two canonical interfaces:
   - a Go-exported backend interface imported by TypeScript;
   - a TypeScript-exported browser interface imported by Go.
2. Cover every primitive, named chains, lists, inline/named records, bytes,
   zero-width values, all exception payload forms, and no-return procedures.
3. Generate and check in both Go and TypeScript bindings with explicit
   regeneration commands. Ordinary tests must verify rather than rewrite them.
4. Build a Go test server using `transport/websocket.NewHandler` with generated
   export and import bindings. Serve the Playwright test page from the same
   origin.
5. Test TypeScript-to-Go calls, Go-to-TypeScript calls, nested/reentrant calls,
   simultaneous bidirectional calls, concurrent calls, and out-of-order
   responses.
6. Test all application exceptions in both directions and the three fixed
   exceptions.
7. Test interface-ID success and both mismatch directions.
8. Test local browser cancellation followed by an ignored late Go response and
   continued connection use.
9. Add malformed-peer modes for over-ceiling frames, over-budget zero-width
   lists, saturated send/receive queues, excess concurrent handlers, truncated
   logical frames followed by close, unknown matched exception keys,
   noncanonical NaNs, trailing payloads, duplicate active IDs, unmatched opaque
   responses, text WebSocket messages, and multiple/split frames across
   messages.
10. Compare canonical interface IDs, procedure keys, exception keys, frame
    headers, and codec vectors directly between Go and TypeScript.
11. Run the full matrix in Chromium, Firefox, and WebKit. Keep a smaller Chromium
    smoke subset for fast local iteration.

**Gate:** a real browser and the checked-out Go implementation interoperate in
both directions for every supported value and failure class.

### Phase 14 — Hardening, documentation, and release acceptance

1. Add parser and codec fuzz/mutation jobs with checked-in regression seeds.
2. Add payload-boundary tests at 64 MiB minus one, exactly 64 MiB, and 64 MiB
   plus one, plus codec-node, pending-call, active-handler, send-buffer, and
   receive-queue boundary tests, without multiplying large allocations across
   parallel tests.
3. Add deep projection tests at 4,095, 4,096, and 4,097 occurrences in isolated
   subprocesses/workers so an implementation stack failure is reported cleanly.
4. Audit every conversion from `bigint` to `number`, every allocation, every
   buffer growth, and every externally supplied property access.
5. Audit browser event-listener and payload retention after close using heap or
   weak-reference tests where reliable.
6. Verify no generated artifact contains a timestamp, absolute source path,
   temporary path, nondeterministic ordering, or platform path separator.
7. Complete `TYPESCRIPT.md` command reference, mapping tables, exception/catch
   examples, cancellation, lifecycle, same-origin/WSS/authentication guidance,
   and Makefile integration.
8. Add a complete bidirectional hello-world example and a kitchen-sink example.
9. Verify package exports and declarations in a clean consumer project with a
   representative browser bundler, without making that bundler a runtime
   dependency.
10. Run final gates:

    ```sh
    cd typescript
    npm ci
    npm run build
    npm test
    npm run test:browser
    npm run test:integration
    npm run check:fixtures
    npm pack --dry-run

    cd ../go
    go test ./...
    go vet ./...

    cd ..
    git diff --check
    ```

11. Inspect the packed tarball to ensure it contains browser ESM, declarations,
    CLI ESM, required metadata, license/readme files, and no source fixtures,
    Go binaries, Playwright downloads, temporary files, or secrets.
12. Publish only after the packed package passes the clean-consumer and live Go
    interoperability smoke tests.

**Gate:** all unit, browser, cross-language, fixture, packaging, Go regression,
and documentation checks pass from a clean checkout.

## 11. Testing principles

- Prefer black-box public API tests for runtime and generated bindings; use
  same-module tests only for ownership state transitions and codec internals.
- Use controlled promises, fake transports, and explicit barriers rather than
  timing sleeps.
- Make all randomized tests deterministic and print their seed on failure.
- Keep fixture regeneration separate from ordinary tests.
- Test the exact checked-in Go module rather than a separately installed
  version.
- Do not weaken byte comparisons to accept stale generated files.
- Every protocol bug found by fuzzing or interoperability testing gets a minimal
  durable regression fixture.
- Browser failures must report browser name/version and preserve Go server logs.
- Resource-boundary tests should run serially when parallel allocation could
  make results machine-dependent.

## 12. Completion criteria

The TypeScript implementation is complete when:

1. A Go export binding can generate an interface that `intercall-ts import`
   consumes, and a browser can call every generated Go procedure over the Go
   WebSocket handler.
2. `intercall-ts export` can generate a browser interface that `intercall-go
   import` consumes, and Go can call every generated browser provider over the
   same connection.
3. Calls can be simultaneous and nested in both directions.
4. Both implementations produce identical keys, canonical semantic bodies,
   SHA-256 interface IDs, value bytes, and frame bytes for shared fixtures.
5. Malformed matched responses terminate the browser connection, unmatched
   responses remain opaque, malformed requests select the proper fixed
   exception, and over-ceiling frames or fixed browser safety limits are
   rejected before unsafe allocation or buffering.
6. Per-call `AbortSignal` cancellation retires only that call and a late response
   is ignored without damaging the connection.
7. Generated outputs are deterministic, strict-compile, safely owned, and
   validated before filesystem mutation.
8. The browser runtime has no production dependency and imports no Node-only
   code.
9. `.ts` and `.tsx` provider modules both generate valid deterministic runtime
   imports under supported JSX transform and preserve modes.
10. Current Chromium, Firefox, and WebKit pass the full bidirectional Go
    interoperability suite.
11. The documented UX is the actual exported API, not pseudocode or a wrapper
    maintained only by examples.
