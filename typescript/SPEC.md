# InterCall TypeScript Profile

This document defines the TypeScript mapping and browser runtime for InterCall.
It is subordinate to `../README.md`, which defines the interface language and
wire protocol, and to `../go/SPEC.md`, which defines the Go compatibility
profile. If this document conflicts with either protocol document, the README
wins.

The implementation targets web browsers. Its build-time CLI runs on Node.js,
but emitted runtime code has no Node.js dependency. The TypeScript peer is a
full InterCall peer: it may import procedures, export procedures, make calls
while handling a call, and participate in nested calls. It does not provide a
TypeScript WebSocket server.

## 1. Compatibility profile

The implementation is compatible with the Go profile in these areas:

- exact interface grammar, UTF-8 rules, comments, validation, and canonical
  formatting;
- FNV-0 procedure and exception keys;
- little-endian exact-width value encodings;
- canonical floating-point NaNs and strict UTF-8 validation;
- 24-byte request and response frames;
- independent 63-bit request-ID spaces;
- fixed Go runtime exception names and keys;
- SHA-256 IDs of canonical interface bodies;
- client-first interface-ID negotiation;
- bidirectional concurrent calls and nested calls;
- matched-response validation and opaque unmatched responses;
- 64 MiB maximum accepted frame payload.

The browser binding adds implementation-safety limits permitted by the README.
The implementation-safety limits other than the per-message WebSocket option
are fixed. They are not wire fields or negotiated. `messageLimit` may lower the
maximum accepted WebSocket message size, but may not exceed the default
67,108,888-byte ceiling. A peer may close when any applicable limit is exceeded.

## 2. Package and build targets

The package name is `@cerasos/intercall` unless registry availability requires
an explicit change before publication. It is ESM-only and targets ES2022.
Browser runtime entry points use only standard browser APIs and have no runtime
dependencies. The build-time CLI uses Node.js 22 or newer and one pinned
TypeScript compiler version.

Published entry points are:

- `@cerasos/intercall`: public browser-safe markers, errors, bindings, and
  connection types;
- `@cerasos/intercall/browser`: browser WebSocket construction;
- `@cerasos/intercall/generated`: generated-code bridge declarations and
  implementations;
- `intercall-ts`: the Node.js generation CLI.

The package does not provide CommonJS output, a Node transport, a server, Unix
sockets, TCP, WebTransport, or a service-worker transport.

Generated output is ESM TypeScript. Generated relative imports use the emitted
extension: `.js` for `.ts` modules and transformed `.tsx` modules, and `.jsx`
for `.tsx` modules under `jsx: "preserve"`. The generator validates every
emitted specifier against the configured TypeScript project.

## 3. TypeScript value mapping

### 3.1 Public markers

The package exports structural numeric markers and one exact empty-record marker:

```ts
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
```

The export tool recognizes these declarations by compiler symbol identity,
including through ordinary alias chains. Same-named declarations from another
package are not markers. Bare `number` and `bigint` are rejected in exported
wire signatures because they do not identify an exact primitive.

Markers do not brand or box runtime values. The application may perform normal
number and bigint operations; generated encoders enforce the wire range and
representation at runtime.

### 3.2 Wire mapping

| InterCall type | Generated TypeScript type | Runtime representation |
| --- | --- | --- |
| `int8`, `int16`, `int32` | `Int8`, `Int16`, `Int32` | finite integral `number` |
| `int64` | `Int64` | `bigint` |
| `uint8`, `uint16`, `uint32` | `Uint8`, `Uint16`, `Uint32` | finite integral `number` |
| `uint64` | `Uint64` | `bigint` |
| `float32`, `float64` | `Float32`, `Float64` | `number` |
| `string` | `string` | well-formed UTF-16 string |
| `bytes` | `Uint8Array` | `Uint8Array` |
| `list T` | `ReadonlyArray<T>` | JavaScript array |
| nonempty record | readonly object properties | plain object |
| `record {}` | `EmptyRecord` | plain empty object |
| omitted return | `void` | `undefined` |

Generated records use property-level `readonly` modifiers rather than
`Readonly<T>` or another mapped generic. `EmptyRecord` is recognized specially
by the exporter before ordinary index-signature rejection. `Record<K, T>` and
`Readonly<T>` are not general wire mappings.

Named InterCall types are structural TypeScript aliases or interfaces. They do
not add runtime wrappers or brands. A `bytes` value is never collapsed into a
`list uint8`; the former is a `Uint8Array`, and the latter is a readonly array
of `Uint8` values.

### 3.3 Runtime validation

Encoders validate all values at the wire boundary:

- signed and unsigned `number` integers are finite, integral, and within the
  exact primitive range;
- 64-bit integers are `bigint` values in the exact range;
- float values are JavaScript numbers;
- float32 values are rounded by IEEE 754 binary32 encoding;
- every encoded NaN uses the required canonical quiet-NaN bit pattern;
- decoded NaNs are accepted only when their bits are canonical;
- strings containing unpaired UTF-16 surrogates are rejected before
  `TextEncoder` can replace them;
- decoded UTF-8 uses fatal decoding and rejects invalid scalar encodings;
- records contain exactly their declared own enumerable string fields;
- arrays and `Uint8Array` values are checked before iteration or copying.

No implicit coercion occurs between numeric types, `bytes`, and lists.

## 4. Export projection

The TypeScript export tool uses the TypeScript compiler API for semantic
identity and source AST nodes for declaration order, comments, directives, and
source positions.

### 4.1 Supported source forms

The initial exporter accepts:

- exact numeric marker aliases and exact `EmptyRecord`;
- `string`;
- the exact global or imported `Uint8Array` type for `bytes`;
- `Array<T>`, `ReadonlyArray<T>`, `T[]`, and `readonly T[]` for lists;
- anonymous object type literals with required identifier-named properties for
  nonempty inline records;
- exported, nongeneric tagged type aliases and interfaces;
- untagged, nonrecursive aliases and interfaces, flattened into their wire
  structure;
- exact global `Promise<T>` in provider return positions;
- exact `PayloadException<T>` in exception declaration positions.

It rejects bare `number` and `bigint`, bare `{}`, optional properties, unions,
intersections, nullish types, tuples, enums, fixed-length arrays, index
signatures except the special `EmptyRecord` marker, methods, accessors, call
signatures, constructors, classes as ordinary records, other generic types or
instantiations, conditional types, mapped types, utility types, `any`,
`unknown`, `never`, `object`, symbols, functions, recursive graphs, and
unsupported platform types.

Every reachable declaration is validated, including unreachable declarations
in an otherwise marked generated metadata file. Recursive type graphs are
errors even when recursion occurs through lists, aliases, or inline records.

The strict TypeScript projection has a maximum resolved depth of 4,096
occurrences. A root occurrence is a type declaration underlying type, exception
payload, procedure parameter, or procedure return. Each list-element,
record-field, named-reference-to-underlying, defined-type-to-underlying, or
alias-expansion edge adds one. Preflight is iterative and rejects the first
occurrence beyond the limit before recursive mapping or emission. The pinned
TypeScript 5.9.3 compiler experiment compiles a 4,096-edge alias chain; the
4,096 boundary is therefore retained as the TypeScript profile limit rather
than lowered to a compiler-specific value.

### 4.2 Procedure providers

A selected provider is an exported, top-level, nongeneric, non-overloaded,
non-rest function declaration in an explicit `.ts` or `.tsx` source file. Its
signature is exactly one of:

```ts
function P(HandlerContext, P1, ..., Pn): Promise<void>;
function P(HandlerContext, P1, ..., Pn): Promise<T>;
```

The first parameter has the exact exported `HandlerContext` type. Every wire
parameter has a nonoptional identifier name. The return is the exact global
`Promise` with zero or one supported wire value. Thenables and unions such as
`Promise<T | undefined>` are rejected. Re-exports, overload sets, methods,
class members, arrow-function variables, and default exports are rejected in
the initial profile.

Providers receive:

```ts
export interface HandlerContext {
    readonly connection: Connection;
    readonly signal: AbortSignal;
}
```

The signal is aborted when the handler finishes or the connection terminates.
The connection allows a provider to construct a generated client and make a
nested call to the other peer. Context state is never encoded.

### 4.3 Exceptions

A no-payload application exception is one exported `const` value assignable to
`Error` and marked with `@intercall exception`:

```ts
/** @intercall exception denied */
export const Denied = new Error("denied");
```

A payload application exception is one exported, nongeneric class directly
extending the exact runtime `PayloadException<T>` class. The runtime base class
has a readonly payload and a constructor taking that payload:

```ts
export abstract class PayloadException<T> extends Error {
    readonly payload: T;
    constructor(payload: T);
}

/** @intercall exception failed */
export class Failed extends PayloadException<{
    readonly code: Int32;
    readonly detail: string;
}> {
}

/** @intercall exception blank */
export class Blank extends PayloadException<EmptyRecord> {
}
```

The generated runtime tests a no-payload exception by identity and a payload
exception with `instanceof`, then encodes exactly one matching declaration.
No match, multiple matches, a non-Error rejection, a thrown matcher, a typed
invalid payload, or an encoding failure selects `internal_exception`. Provider
errors are never sent as unrecognized arbitrary exceptions.

`PayloadException<EmptyRecord>` is payload-bearing and distinct from a
no-payload sentinel. Generated remote payload exceptions expose the value as a
readonly `.payload`; fields are not flattened onto `Error`, where names such as
`message`, `name`, `cause`, and `stack` would collide.

## 5. Names, directives, and generated APIs

### 5.1 Names

Use the Go profile's ASCII lower-snake, Pascal, camel, and fixed-initialism
algorithms. Default mappings are:

- TypeScript type and exception declarations: Pascal case to lower snake case;
- procedure functions, parameters, and fields: lower camel case to lower snake
  case;
- imported wire types and exception classes: Pascal case;
- imported procedures, parameters, and fields: lower camel case.

Explicit overrides are required for valid noncanonical wire names and for names
that cannot be represented by the default conversion. Public collisions are
errors; public names are never silently numbered.

### 5.2 Directives

Recognize complete JSDoc directive lines:

```text
@intercall procedure [wire_name]
@intercall exception [wire_name]
@intercall type [wire_name]
@intercall param TypeScriptName wire_name
@intercall field wire_name
@param TypeScriptName text
@returns text
```

Declaration and parameter overrides bypass default conversion. Field overrides
apply to the property carrying the documentation. Malformed, duplicate,
contradictory, misplaced, unresolved, and unknown InterCall-looking directives
are errors at physical source positions. Directive text is removed from
retained prose. Source prose, `@param`, `@returns`, and property documentation
populate only the semantic documentation slots that TypeScript can represent.

### 5.3 Import-generated client UX

`intercall-ts import` produces one generated `binding_gen.ts` containing named
types, exception mappings, codecs, interface metadata, and an immutable
`importBinding`. It exposes:

```ts
export const importBinding: ImportBinding;
export function createClient(connection: Connection): Client;
```

Each client procedure is an async method with positional wire parameters and an
optional final `CallOptions` value:

```ts
interface CallOptions {
    readonly signal?: AbortSignal;
}

client.hello("world");
client.longOperation(value, { signal });
```

The factory does not open or close a connection. A missing return is
`Promise<void>` and requires an empty successful response payload.

### 5.4 Export-generated binding UX

`intercall-ts export` produces one generated `binding_gen.ts` containing static
provider imports, codecs, procedure dispatch, interface metadata, and an
immutable `exportBinding`. It contains no reflection, registration, handler
registry, or user callback framework.

## 6. Browser runtime

### 6.1 Bindings and metadata

`ExportBinding` and `ImportBinding` are opaque immutable handles. Their object
reference identity is process-local and separate from interface metadata. The
zero value is invalid. Metadata presence is distinct from an all-zero ID.

The generated bridge provides constructors equivalent to:

```ts
type DispatchResult = {
    readonly exceptionKey: bigint;
    readonly payload: Uint8Array;
};

type Dispatch = (
    context: HandlerContext,
    procedureKey: bigint,
    payload: Uint8Array,
) => Promise<DispatchResult>;

type RequestEncoder = () => Uint8Array;
type ResponseDecoder = (
    exceptionKey: bigint,
    payload: Uint8Array,
) => void;

createExportBinding(dispatch: Dispatch): ExportBinding;
createExportBindingWithInterfaceID(
    dispatch: Dispatch,
    id: Uint8Array,
): ExportBinding;
createImportBinding(): ImportBinding;
createImportBindingWithInterfaceID(id: Uint8Array): ImportBinding;

call(
    connection: Connection,
    binding: ImportBinding,
    procedureKey: bigint,
    encode: RequestEncoder,
    decode: ResponseDecoder,
): Promise<void>;
```

The runtime copies and validates each 32-byte interface ID at construction.
`Dispatch` is asynchronous so generated providers may await a Promise; the
runtime catches both synchronous throws and rejected dispatch Promises. A
request encoder and response decoder are synchronous generated closures and
may throw codec errors.

The empty bindings are process-wide singletons for:

```text
exception internal_exception;

exception invalid_arguments;

exception procedure_not_found;
```

The TypeScript exporter inserts these three fixed no-payload exceptions into
every generated interface, in exact wire-name order, and rejects application
declarations that reuse their reserved names or keys. The empty bindings use
the same canonical body and dispatch unknown procedures to
`procedure_not_found`.

Their interface ID is the SHA-256 digest:

```text
c31c470dd8db21db3bc8709bdcad7778a3d2dead33193c95b9691a4f0ba50dc8
```

### 6.2 Connection API

The browser API is:

```ts
interface ConnectionBindings {
    readonly exportBinding: ExportBinding;
    readonly importBinding: ImportBinding;
}

interface WebSocketConnectionOptions {
    readonly signal?: AbortSignal;
    readonly protocols?: string | readonly string[];
    readonly openTimeoutMs?: number;        // default 10,000
    readonly negotiationTimeoutMs?: number; // default 10,000
    readonly messageLimit?: number;         // default 67,108,888
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

Relative HTTP and HTTPS URLs are resolved against the document and converted to
WS and WSS. Explicit WS and WSS URLs are accepted. `connectWebSocket` performs
client-role interface-ID negotiation. `connectRawWebSocket` starts at the first
InterCall frame and performs no negotiation.

`openTimeoutMs` applies to the WebSocket opening phase. After opening,
`negotiationTimeoutMs` applies to interface agreement. Both race the caller's
signal. Zero, negative, nonfinite, and nonintegral timeout values are invalid.

`close()` publishes the local closed cause and initiates socket closure without
waiting for the browser close event, handlers, or native networking. `closed`
resolves, never rejects, with the permanent first terminal `Error` after socket
cleanup and receive-loop shutdown. It does not wait for handlers that ignore
cancellation. Repeated close calls are idempotent.

### 6.3 Errors and cancellation

Stable errors have a readonly code and standard `cause` when wrapping another
value. The runtime defines classifications corresponding to the Go sentinels:

- invalid argument;
- binding mismatch;
- explicit connection close;
- request-ID exhaustion;
- protocol failure;
- interface mismatch;
- transport failure;
- browser implementation-safety resource exhaustion.

The fixed wire exceptions `procedure_not_found`, `invalid_arguments`, and
`internal_exception` are process-wide no-payload remote exception singletons.
Application payload exceptions are typed generated classes.

Per-call cancellation rejects with the exact `AbortSignal.reason`, except that
an undefined reason becomes `DOMException("The operation was aborted",
"AbortError")`. It retires only that request ID and sends no cancellation frame.

A connection's permanent cause is always an `Error`. A connection-level abort
reason that is already an `Error` is preserved. Any other reason, including
undefined, is wrapped in `InterCallAbortError` with name `AbortError` and the
original reason as `cause`.

### 6.4 Frames and call state

Use `bigint` for all wire uint64 values internally. Never convert an untrusted
wire integer to `number` before checking its bound.

The frame parser accepts exactly 24-byte headers and payloads through 64 MiB.
The sole receiver buffers complete payloads before response lookup or dispatch.
Matched responses must decode exactly one declared selected value; unknown
exception keys, malformed values, noncanonical NaNs, and trailing bytes are
terminal protocol errors. Unmatched responses are consumed and ignored without
validating their exception key or payload.

Outgoing IDs increase from `0n` through `0x7fffffffffffffffn` and never reuse.
Incoming and outgoing ID spaces are independent. Calls and responses share one
send gate. A request encoder runs at most once, before ID allocation. Pending
ownership transfers exactly once to a response, local cancellation, or terminal
teardown.

The runtime enforces these browser safety limits. All limits except
`messageLimit` are fixed; `messageLimit` may be lowered by the caller but may
not exceed its default.

| Limit | Value |
| --- | ---: |
| accepted frame payload | `67,108,864` bytes |
| default WebSocket message limit | `67,108,888` bytes |
| native queued outgoing application bytes | `134,217,776` bytes |
| owned encoded frame bytes awaiting send | `134,217,776` bytes |
| queued unread incoming bytes | `134,217,808` bytes |
| active incoming request payload bytes | `134,217,728` bytes |
| active incoming handlers | `256` |
| outstanding outgoing calls | `1,024` |
| codec value nodes per complete payload | `1,048,576` |

The outgoing call limit is reserved after argument and connection validation but
before encoding and released on every outcome. Owned frame-byte capacity is
reserved before waiting for send admission and released after `send` returns or
fails. The incoming queue and active request-payload limits are checked before
retaining or dispatching data. Exceeding a visible limit selects
`ResourceLimitError` and closes the connection, except local outgoing encoding
or capacity failures, which reject that call without allocating an ID.

Before sending, the runtime requires:

```text
WebSocket.bufferedAmount + frame.byteLength <= 134,217,776
```

Otherwise it polls at a fixed ten-millisecond interval while observing terminal
state and outgoing-call cancellation. It clears timers on every completion path.
A successful `WebSocket.send()` is local queue admission, not network delivery;
the browser API has no completion callback. The runtime treats it as the local
successful-write point for request-ID reuse semantics. Native browser buffering
outside `bufferedAmount` remains an unavoidable residual risk.

### 6.5 WebSocket messages and negotiation

Set `binaryType = "arraybuffer"` before processing messages. Text messages and
unsupported binary values are terminal transport/protocol failures. Message
boundaries are not frame boundaries: one frame may span messages, and one
message may contain several frames. Incremental chunk queues preserve residual
bytes between negotiation and frame parsing.

Negotiated client setup:

1. validates both nonzero bindings and their present 32-byte IDs;
2. races WebSocket opening against the caller's signal and the ten-second open
   timer;
3. sets binary mode after opening;
4. starts a fresh ten-second negotiation timer;
5. sends exactly the import binding's ID as one binary message;
6. reads exactly 32 bytes from the ordered queue;
7. compares them to the export binding's ID;
8. retains any bytes after the ID for the frame receiver; and
9. closes the socket on every setup failure before returning the failure.

Interface IDs detect contract mismatches and are not credentials. Browser
WebSockets cannot set arbitrary HTTP headers. Authentication uses deployment
mechanisms such as same-origin cookies, query tokens, or surrounding server
middleware, with WSS recommended for confidentiality.

### 6.6 Browser API constraints

The implementation must account for these browser API facts rather than model
itself as a literal Go `io.ReadWriter`:

- `WebSocket.send()` reports local queue admission and has no completion
  callback for network delivery;
- `WebSocket.bufferedAmount` is observational and does not provide receive or
  send backpressure by itself;
- standard browser WebSockets may buffer incoming messages in the user agent
  before JavaScript receives them, so application-level queue limits cannot
  bound all native memory;
- a browser WebSocket cannot be force-closed synchronously in the Go sense;
  `close()` requests closure and returns without waiting for the close event;
- message event data is already allocated by the browser and may contain one
  complete frame, several frames, or a fragment of a frame;
- binary data arrives as `ArrayBuffer` after setting `binaryType`, while text
  data must be rejected;
- browser code cannot set arbitrary HTTP upgrade headers, so authentication
  belongs to same-origin credentials, URL-level deployment policy, or server
  middleware;
- browser event callbacks run on the JavaScript event loop, so synchronous
  send-gate admission is the local ordering point for frame writes.

These facts are observable binding differences. They do not change frame bytes,
value encodings, request IDs, or interface negotiation.

## 7. Codec execution

Generated codecs are immutable flat programs executed by iterative encoder and
decoder stacks. Runtime reflection does not infer schemas from values. Programs
contain all primitive operations, list edges, record field order, named
references, and zero-width facts.

The encoder and decoder:

- use checked buffer growth and exact little-endian primitive operations;
- use two's-complement signed integer representations;
- canonicalize and strictly validate NaNs;
- validate well-formed UTF-16 before UTF-8 encoding;
- use fatal UTF-8 decoding;
- count string lengths in UTF-8 bytes;
- distinguish `Uint8Array` from `list uint8`;
- validate closed records and encode fields in declaration order;
- check lengths, counts, arithmetic, conversion, allocation, and iteration
  before performing them;
- consume selected payloads exactly;
- decode empty bytes and lists to nonnull zero-length values;
- avoid per-element codec execution for zero-width lists;
- never expose a reused or pooled result buffer.

Every complete request, response, or exception payload shares a value-node budget
of 1,048,576 across all roots and named references. A list container, each list
element, and each record object consumes one node before traversal or
allocation. Counts are rejected when they exceed the remaining budget or the
JavaScript maximum array length. Ordinary allocation `RangeError`s become
codec errors; the implementation must not rely on catching host out-of-memory
termination.

Codec failures have context-dependent outcomes:

- malformed incoming arguments select `invalid_arguments`;
- matched malformed responses select terminal `ProtocolError`;
- provider result or exception encoding failures select `internal_exception`;
- local request encoding failures reject only the local call and allocate no ID.

## 8. Generated metadata and re-export

Import-generated output embeds one private unpadded base64url value containing
the canonical semantic interface body, split into deterministic chunks of at
most 4,096 bytes. Generated named type rows carry an exact machine-readable
`@intercall type` line.

Export interprets rows only in a file carrying the exact InterCall generated-file
marker. Before consuming one row it validates the complete marked file, the
canonical base64url value, valid UTF-8, successful interface parsing and
validation, canonical byte equality, machine-row bijection, and documentation-
free TypeScript/wire structural parity. Missing, extra, malformed, duplicate,
conflicting, misplaced, stale, or unreachable rows are errors.

Handwritten files and third-party generated files without the exact marker are
ordinary source; lookalike constants and comments are not metadata. Decoded
semantic documentation is assigned to AST slots and is never rescanned as
TypeScript directives.

## 9. CLI contracts

### 9.1 Import

```text
intercall-ts import --out DIR
    [--ts-name SELECTOR=TypeScriptIdentifier]...
    INTERFACE_FILE
```

Import accepts exactly one regular interface file and never stdin. It reads
exact bytes, validates syntax and semantics, attaches documentation, computes
the canonical body and ID, applies TypeScript projection and overrides, emits
one `DIR/binding_gen.ts`, and validates the complete generated source before
filesystem mutation.

### 9.2 Export

```text
intercall-ts export --project TSCONFIG --out DIR --interface FILE
    [--include SOURCE_FILE#Symbol]...
    [--exclude SOURCE_FILE#Symbol]...
    SOURCE_FILE...
```

Export loads one TypeScript `Program` under the exact project configuration.
Explicit source files are implementation `.ts` or `.tsx` files belonging to the
program; declaration files, JavaScript files, and generated binding files are
rejected. With no include filter, every eligible tagged provider in explicit
files is selected. Excludes win. Tagged application exceptions in explicit
files remain available to every selected provider.

The command validates all sources, projection, generated TypeScript, canonical
interface bytes, and generated-source type checking in memory. It emits exactly
one `binding_gen.ts` and one owned interface target.

Generated export providers use deterministic relative runtime imports. The
emitted extension is `.js` for `.ts` and transformed `.tsx`, and `.jsx` for
`.tsx` under `jsx: "preserve"`. The command rejects unresolved specifiers,
inaccessible modules, output/provider cycles, declaration-only providers, and
ambiguous module identities.

### 9.3 Diagnostics and ownership

Diagnostics use `path:line:column: message`, with one-based physical byte
positions and slash-normalized logical paths. Interface diagnostics are based
on exact input bytes. TypeScript diagnostics use physical source offsets and
ignore source maps. Multiple diagnostics sort by path, line, column, and
message. Staging paths, absolute temporary paths, and compiler object identities
never appear.

Generated files begin with:

```text
// Code generated by intercall-ts; DO NOT EDIT.
// intercall-ts binding: import sha256:<artifact-id>
```

The second line says `export` for export bindings. Exported interface files
begin with:

```text
/* Code generated by intercall-ts; artifact sha256:<artifact-id>; DO NOT EDIT. */
```

followed by one blank line and the canonical body. The artifact ID hashes only
the canonical body.

Validation completes before output-directory creation. Ownership checks reject
symlink, directory, device, and other nonregular target leaves. The generator
never overwrites handwritten or differently generated files, never deletes
stale paths, and never truncates an owned target in place. It stages in the
destination directory and replaces by rename. Unchanged bytes are not replaced.
Non-code files in the output directory are preserved. Export repairs a prior
interrupted two-target update when both existing targets are valid owned files
with differing stamps or exactly one is missing.

## 10. Required interoperability tests

The implementation must test against the checked-out Go implementation, not an
independently installed version. Tests cover:

- every primitive, list, named type, nested record, bytes value, and zero-width
  value;
- all application exception forms and the three fixed runtime exceptions;
- canonical keys, semantic bodies, interface IDs, frame headers, and codec
  vectors;
- TypeScript-to-Go and Go-to-TypeScript calls;
- simultaneous and nested calls in both directions;
- concurrent and out-of-order responses;
- interface-ID agreement and mismatch;
- per-call cancellation followed by a late ignored response;
- malformed frames, noncanonical NaNs, invalid UTF-8, unknown matched keys,
  trailing values, duplicate IDs, text messages, and split/coalesced messages;
- implementation-safety limits before unsafe buffering or allocation;
- `.ts` and `.tsx` provider generation under supported JSX modes.

Browser tests run in current Chromium, Firefox, and WebKit. Fuzz and mutation
regressions use deterministic seeds. Fixture regeneration is separate from
ordinary fixture verification.
