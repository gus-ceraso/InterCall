# InterCall

InterCall makes native functions portable across languages.

A function is implemented idiomatically in one language, exported as a portable
contract, and imported as an idiomatic generated binding in another language or
environment. InterCall supplies the code generation and the small bidirectional
runtime used to communicate between peers.

Native source is the primary user experience. InterCall definition text is a
generated, portable intermediate representation: stable enough to exchange,
inspect, diff, document, and use as input to other generators, but not normally
the first thing an application author writes.

## Conceptual Use

Suppose a C program implements an ordinary function:

```c
#include <stdint.h>

// Add returns the sum of a and b.
uint32_t add(uint32_t a, uint32_t b) {
    return a + b;
}
```

A C exporter selects that function and emits its InterCall contract. A Go
importer can then generate a binding used like this:

```go
sum, err := arithmetic.Add(2, 3)
if err != nil {
    // Remote calls are inherently fallible.
}
```

The reverse direction works on the same connection: Go functions can be
exported and called through generated C bindings. Neither side is inherently a
client or a server.

These snippets are illustrative. Export annotations, package APIs, context and
cancellation conventions, ownership rules, and exact generated signatures are
not yet specified.

An exporter might generate this inspectable InterCall definition IR:

```text
/* Arithmetic operations. */
namespace arithmetic {
    /* Adds two unsigned 32-bit integers. */
    function add(a uint32, b uint32) uint32;
}

/* Peer preferences. */
namespace preferences {
    type locale = string;

    type preference = record {
        name string;
        value string;
    };

    function get_locale() locale;
    function set_locale(value locale);
    function list_preferences() list{preference};
}
```

A function with no return declaration, such as `set_locale`, returns no value.
There is no separate portable `unit` type.

## Scope

InterCall consists of three parts:

- a portable function ABI and inspectable definition IR;
- language exporters and importers; and
- a small, bidirectional peer runtime.

The runtime carries framed request-response calls over a reliable ordered byte
stream. Either peer may call exported functions, and both peers may have calls
outstanding concurrently. Local processes and Unix domain sockets are
first-class uses. Web transports, service discovery, deployment, gateways,
authentication, and other service infrastructure are layers above the core.

InterCall uses C, Zig, C++, Rust, Swift, Java, Kotlin, C#, Go, Ruby, Python,
JavaScript, and TypeScript as its initial semantic portability baseline and is
intended to support other general-purpose languages. Its data model is strict:
decoders do not coerce values. Each language profile must validate and map the
portable contract into representations appropriate for that language. A
profile may need wrappers, checked conversions, or distinct generated types
where a language lacks a direct representation.

InterCall does not attempt to model streaming, shared memory, object identity,
offline delivery, or every distributed-system interaction.

## Status

InterCall is a greenfield design. This README distinguishes current design
decisions from unresolved questions. Unless a section says otherwise, the
rules below describe the current protocol draft. The final section lists work
that remains open and is not normative.

## Installation

TODO.

## Data Model

### Primitive Types

InterCall has these primitive types:

| Type | Meaning |
| --- | --- |
| `boolean` | `true` or `false` |
| `int8`, `int16`, `int32`, `int64` | Exact-width signed integers |
| `uint8`, `uint16`, `uint32`, `uint64` | Exact-width unsigned integers |
| `float32`, `float64` | IEEE 754 binary32 and binary64 |
| `string` | A sequence of Unicode scalar values |
| `bytes` | An ordered sequence of raw bytes |

Signed integer ranges are `-2^(N-1)` through `2^(N-1)-1`; unsigned ranges are
`0` through `2^N-1`. There are no portable `int` or `uint` aliases. Width is
always explicit.

`float32` and `float64` include finite values, positive and negative zero,
positive and negative infinity, and NaN, subject to the canonical wire encoding
below.

`string` represents text, not encoded binary data. It cannot contain surrogate
code points because those are not Unicode scalar values. InterCall does not
have a separate character type.

### Lists

`list{T}` is an ordered sequence of zero or more values of one element type.
Lists may contain primitives, named types, or other lists.

### Named Records

A record is a closed, named collection of ordered fields. Every declared field
is required and additional fields are not allowed. Public contracts do not
contain anonymous record types.

```text
type user = record {
    id uint64;
    name string;
    avatar bytes;
};
```

### Application-Defined Types

A type declaration introduces either a named record or a transparent alias.
Aliases do not add a distinct wire representation:

```text
type user_id = uint64;
type users = list{user};
```

A local type is referenced by name. A type in another namespace is referenced
as `namespace.type`.

Named types may be recursive only through lists. Every cycle in the type graph
must pass through at least one list:

```text
type tree = record {
    value string;
    children list{tree};
};
```

Direct record recursion and alias-only cycles are invalid:

```text
type invalid_record = record {
    next invalid_record;
};

type invalid_alias_a = invalid_alias_b;
type invalid_alias_b = invalid_alias_a;
```

All values are finite, acyclic trees. Pointers, references, object identity,
shared substructure as an observable property, and cyclic runtime values are
not portable. Encoders must reject cyclic values.

### Deliberately Unsupported Types

The current data model has no optional type, enum, general variant or sum type,
map, character, unit type, pointer, or function value. Optional values, enums,
variants, and maps remain design questions rather than implicit conventions.
Applications must not rely on language-specific coercions to emulate them.

A universal zero-value rule is also unresolved. In particular, error handling
must not assume that a failed call can return a zero value of every result type.

## Definition IR

An InterCall definition document is UTF-8 text containing one or more
namespaces. The current draft has no embedded edition header. Documentation and
formatting do not affect function identity.

Outside documentation blocks, spaces, tabs, carriage returns, line feeds, form
feeds, and vertical tabs may appear between tokens without changing their
meaning. Whitespace is required only when its absence would combine adjacent
tokens. Semicolons terminate declarations and record fields, commas separate
function parameters, braces delimit namespaces and composite declarations, and
parentheses delimit parameter lists. Newlines and indentation have no semantic
meaning.

### Documentation

A `/* ... */` block documents the namespace, type, function, parameter, or
record field that immediately follows it. Documentation association does not
depend on whitespace or line breaks. Documentation is optional, and an item may
have at most one documentation block. Comments do not nest and end at the first
`*/`.

The documentation value is the UTF-8 text between the delimiters with leading
and trailing spaces, tabs, carriage returns, line feeds, form feeds, and
vertical tabs removed. A function's documentation also describes its unnamed
return value when one is present.

### Grammar

```ebnf
document ::=
    namespace-declaration+
    EOF
    ;

namespace-declaration ::=
    documentation?
    "namespace" IDENT "{"
        declaration*
    "}"
    ;

declaration ::=
    documentation?
    (
        type-declaration
      | function-declaration
    )
    ;

type-declaration ::=
    "type" IDENT "="
    (
        type-expression
      | record-definition
    )
    ";"
    ;

function-declaration ::=
    "function" IDENT
    "(" parameter-list? ")"
    type-expression?
    ";"
    ;

parameter-list ::=
    parameter ("," parameter)*
    ;

parameter ::=
    documentation?
    IDENT type-expression
    ;

type-expression ::=
      primitive-type
    | type-reference
    | list-type
    ;

type-reference ::=
    IDENT ("." IDENT)?
    ;

list-type ::=
    "list" "{" type-expression "}"
    ;

record-definition ::=
    "record" "{"
        field*
    "}"
    ;

field ::=
    documentation?
    IDENT type-expression
    ";"
    ;

primitive-type ::=
      "boolean"
    | "int8"
    | "int16"
    | "int32"
    | "int64"
    | "uint8"
    | "uint16"
    | "uint32"
    | "uint64"
    | "float32"
    | "float64"
    | "string"
    | "bytes"
    ;

IDENT ::=
    IDENT_START IDENT_CONTINUE*
    ;

IDENT_START ::=
      "A" ... "Z"
    | "a" ... "z"
    | "_"
    ;

IDENT_CONTINUE ::=
      IDENT_START
    | "0" ... "9"
    ;

documentation ::=
    "/*" DOCUMENTATION_TEXT "*/"
    ;
```

`DOCUMENTATION_TEXT` is any sequence of Unicode scalar values that does not
contain `*/`. Identifiers are case-sensitive. The reserved words are
`namespace`, `type`, `function`, `list`, and `record`, together with every
primitive type name.

A document declares each namespace at most once. Declaration names are unique
within a namespace. Field names are unique within a record, and parameter names
are unique within a function. Declarations may refer to types declared later in
the document. Unqualified references resolve in the current namespace;
qualified references resolve in the named namespace within the same document.

Definition processors must preserve record field and function parameter order.
After resolving references, they must reject every type cycle that is not
list-guarded.

A function has ordered parameters and either no return value or one unnamed
return value:

```text
function ping();
function notify(message string);
function add(a int32, b int32) int32;
function get_users() list{user};
```

Functions are declarations, not values. Whether a native implementation is
synchronous or asynchronous does not change its portable contract or function
identity.

## Wire Protocol

### Transport and Peers

InterCall requires a reliable, ordered sequence of bytes. It does not depend on
transport message boundaries. Unix domain stream sockets, TCP, TLS over TCP,
and binary WebSocket streams are possible transports; transport bindings define
how their APIs expose the InterCall byte stream.

Each connection has an initiator and an acceptor only for allocating request ID
ranges. After transport establishment, the peers are otherwise symmetric and
either may send a request immediately. There is currently no InterCall
handshake, registry exchange, magic value, or embedded protocol version. Peers
must establish compatible wire and definition semantics out of band.

### Byte Order and Value Encoding

All multibyte integers, sizes, counts, IDs, and floating-point bit patterns are
little-endian. Values have no implicit alignment or padding. Implementations
must never serialize native structure memory directly.

Values are encoded according to their resolved types:

| Type | Encoding |
| --- | --- |
| `boolean` | One byte: `0` is false and `1` is true; every other byte is invalid |
| `int8`, `uint8` | One byte |
| `int16`, `uint16` | Two bytes |
| `int32`, `uint32` | Four bytes |
| `int64`, `uint64` | Eight bytes |
| `float32` | Four-byte IEEE 754 binary32 bit pattern |
| `float64` | Eight-byte IEEE 754 binary64 bit pattern |
| `string` | `uint64` byte length followed by UTF-8 bytes |
| `bytes` | `uint64` byte length followed by raw bytes |
| `list{T}` | `uint64` element count followed by each encoded `T` |
| Named record | Fields encoded in declaration order |
| Transparent alias | Encoding of its resolved underlying type |

Signed integers use two's-complement representation. Integers, byte lengths,
and element counts are fixed-width rather than variable-length.

Strings must be valid UTF-8 encodings of Unicode scalar values. Decoders reject
overlong encodings, surrogate code points, truncation, and all other invalid
UTF-8. InterCall does not normalize strings.

Finite floating-point values, infinities, and signed zero use their IEEE 754 bit
patterns. Encoders must emit canonical quiet NaN `0x7fc00000` for `float32` and
`0x7ff8000000000000` for `float64`. Decoders reject every other NaN bit pattern.

A list count is a count of values, not bytes. Elements are consecutive and have
no individual frame. A record has no encoded field count, field names, padding,
or total length; its definition determines its layout. An empty named record
therefore occupies zero payload bytes.

Request arguments are encoded consecutively in declaration order. A successful
return value uses the same encoding as any other value of its declared type.

### Function IDs

Every function is identified by the complete 64-bit FNV-0 hash of its canonical
function representation:

```text
hash = 0
prime = 1099511628211

for each canonical byte:
    hash = hash * prime modulo 2^64
    hash = hash XOR byte
```

There is no initial offset, seed, or domain marker. Hash value `0` is valid and
not reserved. The resulting `uint64` is encoded little-endian.

The exact canonical function representation remains TODO. It must be
deterministic across implementations and include the fully qualified function
name, ordered parameters, optional return type, and every reachable named type
needed by the contract. It must represent list-guarded recursion without
infinite expansion. Documentation, source formatting, exporter annotations,
synchronous versus asynchronous source implementation, and unrelated
declarations are excluded. Adding or reordering an unrelated declaration must
not change an existing function ID.

Generation must reject distinct functions that collide within one generated
function registry. Function IDs are dispatch identifiers, not authentication or
authorization tokens.

There is no compatibility negotiation. Subject to compatible wire and
definition semantics and the absence of a hash collision, compatibility is per
immutable function ID:

- independently updated peers can continue calling functions whose canonical
  representations are unchanged;
- a changed signature is rehashed and is expected to receive a different ID;
- a missing function receives an InterCall errno response after its bounded
  payload has been skipped; and
- old and new contracts can coexist while both IDs remain exported.

### Request IDs

The most significant bit of a request ID identifies the peer that initiated the
request:

- the connection initiator uses `0x0000000000000000` through
  `0x7fffffffffffffff`;
- the connection acceptor uses `0x8000000000000000` through
  `0xffffffffffffffff`.

The initiating peer chooses the remaining 63 bits. A peer must not reuse a
request ID during the lifetime of a connection. After exhausting its range, it
may continue responding but cannot initiate another request on that connection.

A response repeats its request ID exactly. An ID in the remote peer's range
starts a request; an ID in the local peer's range starts a response.

### Frames

A request is:

```text
request_id   uint64
function_id  uint64
payload_size uint64
payload      payload_size bytes
```

A response is:

```text
request_id   uint64
errno        uint64
payload_size uint64
payload      payload_size bytes
```

Each header is 24 bytes and uses three consecutive 64-bit fields at offsets 0,
8, and 16, with no padding. `payload_size` is the number of bytes immediately
following the header and does not include the header.

A request payload contains its encoded arguments. A successful response payload
contains its return value, or is empty for a function with no return value.

A decoder must bound all decoding to the declared frame payload. Request
arguments and successful return values must consume exactly that many bytes. A
decoder must not read into the next frame to complete a malformed value. A
bounded request for an unknown function can be skipped and answered with an
InterCall errno. Until nonzero response payload semantics are decided, such a
payload is consumed only as bounded opaque bytes. A frame whose declared size
exceeds a safe implementation limit may cause the connection to close without
consuming the payload.

Both peers may have multiple outstanding requests, and responses may be sent in
any order. Bytes from different frames must not be interleaved. Implementations
must continue processing inbound frames while calls are outstanding to avoid
reader-loop deadlocks. InterCall does not prevent deadlocks caused by
application-level dependency cycles.

## Errors

Every response has one universal numeric `errno`:

- `0` means success;
- `1` through `255` are reserved for InterCall; and
- `256` through `2^64-1` are application-defined.

The portable contract has no throws declarations, exception inheritance, error
strings, or per-function error type. Protocol and application failures use the
same response field. Exact reserved InterCall errno assignments and the way
applications declare and name their errno values are not yet specified.

Remote calls remain fallible even when their native implementation has no
application failure path. Generated bindings project failures into the target
language's idiom: for example, a Go `error`, a Rust `Result`, Swift error
handling, Java, Kotlin, C#, Python, or Ruby exceptions, JavaScript or
TypeScript promise rejection, or a C status result. The exact profile mappings
remain to be designed.

For `errno == 0`, the payload must encode the declared return value exactly, or
be empty when there is no return value. The meaning of a payload when `errno !=
0` is open. A likely first version requires it to be empty, but that is not yet
a current rule. Implementations must not substitute or depend on a universal
zero return value after failure.

### Replyable and Fatal Failures

Framing separates failures that can be attributed to one request from failures
that make the connection unsafe.

When frame integrity and peer state remain trustworthy, a peer can answer a
request with a reserved InterCall errno. Examples include an unknown function,
a contained request decoding failure, and a contained handler failure. The
exact errno for each case is open.

A peer closes the connection when it cannot continue unambiguously or safely.
Examples include an invalid or impossible header/state transition, an unknown
response ID, a reused request ID when it makes state ambiguous, a structurally
malformed response, an unsafe oversized frame, arithmetic overflow needed to
process a frame, or transport truncation. Because a response cannot be answered with
another response, an invalid response is normally fatal to the connection.
Closing fails all outstanding calls.

An application errno is a normal response and does not by itself close the
connection.

## Connection Lifecycle and Resource Safety

Transport establishment establishes the InterCall connection; there is no
additional handshake. Closing or half-closing the transport closes the
InterCall connection. Active handlers are abandoned or cancelled according to
implementation policy. InterCall currently defines no graceful shutdown, retry,
or wire cancellation operation.

A request completes after its successful return value has been decoded or its
nonzero response payload has been consumed. The requester may release its
pending state but must not reuse the request ID. If a local
timeout or cancellation stops waiting for a response, the implementation must
retain enough tombstone state to recognize and discard the eventual response,
or close the connection.

Implementations may bound frame payload size, string and byte lengths, list
counts, nesting depth, decoded allocation, buffered output, outstanding calls,
concurrent handlers, definition complexity, and connection lifetime. They must
check limits and arithmetic overflow before allocation or conversion from
`uint64` to a native size. The wire format's numeric range does not require an
implementation to allocate or accept every representable size.

## Open Design Questions

The following points are intentionally unsettled:

- the canonical function representation hashed by FNV-0;
- exact assignments and semantics for InterCall errnos `1` through `255`;
- declaration, naming, scoping, documentation, and generated-language mapping
  of application errnos; namespace-scoped explicit declarations are one
  possibility, not a decision;
- whether a nonzero response may carry a payload; an empty payload is the likely
  first-version rule but is not confirmed;
- whether any universal zero-value doctrine should exist;
- portable option, enum, general variant or sum, and map designs, if any;
- protocol and definition edition mechanisms;
- language export annotations and detailed importer/exporter mappings;
- the first language implementations to support;
- default resource limits and limit-related behavior; and
- cross-registry hash collisions and the broader security implications of
  dispatch by function ID.
