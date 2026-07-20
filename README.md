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
cancellation conventions, ownership rules, exact generated signatures, and
other native-language mappings are implementation-defined. They do not affect
the portable contract except through the InterCall IR an implementation emits.

An exporter might generate this inspectable InterCall definition IR:

```text
namespace intercall {
    errno unknown_function 1;
    errno malformed_request 2;
    errno internal_error 3;
    errno resource_exhausted 4;

    /* Arithmetic operations. */
    namespace arithmetic {
        /* Adds two unsigned 32-bit integers. */
        function add(a uint32, b uint32) uint32;
    }

    /* Peer preferences. */
    namespace preferences {
        type locale string;

        /* The requested locale is not supported. */
        errno unsupported_locale 5;

        type preference record {
            name string;
            value string;
            metadata record {
                source string;
            };
        };

        function get_locale() locale;
        function set_locale(value locale);
        function list_preferences() list{preference};
    }
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
decoders do not coerce values. Native-language mappings are
implementation-defined, and two implementations targeting the same language or
language revision may expose different APIs. Those mappings must preserve the
portable contract and may use wrappers, checked conversions, or distinct
generated types where a language lacks a direct representation.

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
The element may be any type expression, including another list, an anonymous
record, or an anonymous enum.

### Records

A record is a closed collection of ordered fields. Every declared field is
required and additional fields are not allowed. A record expression may appear
anywhere a type expression is accepted; it does not need its own declaration.

```text
type user record {
    id uint64;
    name string;
    avatar bytes;
    preferences list{record {
        name string;
        value string;
    }};
};

function create_user(input record {
    name string;
    avatar bytes;
}) record {
    id uint64;
};
```

An anonymous record has no portable generated type name. An implementation may
synthesize a helper name from its use site. Authors declare a named type when a
record must be reused or exposed under a stable generated name.

### Enums

An enum is a closed set of symbolic members backed by an exact-width integer
type. Its integer representation and every member discriminant are explicit:

```text
type delivery_state enum uint8 {
    pending 0;
    in_transit 1;
    delivered 2;
};
```

The backing type must be `int8`, `int16`, `int32`, `int64`, `uint8`, `uint16`,
`uint32`, or `uint64`. An enum has at least one member. Member names are unique,
as are numeric discriminants, and every discriminant must fit the backing type.
Enums have no implicit discriminants or numeric aliases and do not require a
zero member.

Enum expressions, like record expressions, may be anonymous and nested. An
anonymous enum has the same generated-name considerations as an anonymous
record. Enum member order has no semantic meaning because every discriminant is
explicit.

Enums are closed. Decoders reject an integer that is not one of the declared
discriminants, and encoders reject invalid native values. An implementation may
use a native enum where its representation and validation are suitable, or
generate a checked integer wrapper and named constants. Native enum ordinals
and memory layouts are never portable.

Adding, removing, renaming, or renumbering an enum member changes every
function contract that reaches the enum, but it does not change any function
ID. Reordering members or editing their documentation changes neither the
contract nor a function ID.

### Named Types

A type declaration binds a reusable name to any type expression. The syntax has
no `=`:

```text
type user_id uint64;
type users list{user};
```

A declaration adds no tag or other representation to the wire value, but its
name remains significant to the contract and generated source API. An
implementation may represent a declared type as an alias, wrapper, structure,
class, or other checked type. Replacing a named type reference with an identical
anonymous expression therefore preserves the wire layout but changes the
contract. It does not change a function ID.

A local type is referenced by name. A type in another namespace is referenced
by its absolute name, such as `intercall.accounts.user`. Namespace nesting does
not restrict this: a function in a parent namespace may use a type declared in
a child namespace, provided that it uses the type's absolute name:

```text
namespace intercall {
    namespace parent {
        function process(value intercall.parent.child.item);

        namespace child {
            type item string;
        }
    }
}
```

Named types may be recursive only through lists. Every cycle in the type graph,
including a cycle that passes through an anonymous record, must pass through at
least one list:

```text
type tree record {
    value string;
    children list{tree};
};
```

Direct record recursion and alias-only cycles are invalid:

```text
type invalid_record record {
    next invalid_record;
};

type invalid_alias_a invalid_alias_b;
type invalid_alias_b invalid_alias_a;
```

All values are finite, acyclic trees. Pointers, references, object identity,
shared substructure as an observable property, and cyclic runtime values are
not portable. Encoders must reject cyclic values.

### Deliberately Unsupported Types and Declarations

InterCall deliberately has no optional type, general variant or sum type, map,
character, unit type, pointer, or function value. These types are outside the
portable data model. Applications must model any corresponding concepts
explicitly with the supported types and must not rely on language-specific
coercions to supply additional wire semantics. A failed call has no return
value; it does not produce a zero value of its declared result type.

The definition IR also has no application-defined constant declarations or
composite literal grammar. Enum members and errnos are specialized declarations,
not a general constant facility.

## Definition IR

An InterCall definition document is UTF-8 text that contains exactly one
namespace tree and describes the exported contract of exactly one peer. Every
function declared in the document belongs to that peer's function registry; the types
and errnos support those function contracts. A caller uses the exporting
peer's exact document to encode requests and decode responses. How
implementations generate, distribute, select, or install peer documents is
implementation-defined and occurs out of band.

The document's root is the reserved `intercall` namespace, and every other
namespace is nested beneath it. The current draft has no embedded edition
header. Documentation and formatting do not affect function IDs.

Outside documentation blocks, spaces, tabs, carriage returns, line feeds, form
feeds, and vertical tabs may appear between tokens without changing their
meaning. Whitespace is required only when its absence would combine adjacent
tokens. Semicolons terminate declarations, record fields, and enum members;
commas separate function parameters; braces delimit namespaces and composite
declarations; and parentheses delimit parameter lists. Newlines and indentation
have no semantic meaning.

### Documentation

A `/* ... */` block documents the namespace, type, errno, function, parameter,
record field, or enum member that immediately follows it. Documentation
association does not depend on whitespace or line breaks. Documentation is
optional, and an item may have at most one documentation block. Comments do not
nest and end at the first `*/`.

The documentation value is the UTF-8 text between the delimiters with leading
and trailing spaces, tabs, carriage returns, line feeds, form feeds, and
vertical tabs removed. A function's documentation also describes its unnamed
return value when one is present.

### Grammar

```ebnf
document ::=
    root-namespace
    EOF
    ;

root-namespace ::=
    documentation?
    "namespace" "intercall" "{"
        namespace-member*
    "}"
    ;

namespace-member ::=
      namespace-declaration
    | declaration
    ;

namespace-declaration ::=
    documentation?
    "namespace" IDENT "{"
        namespace-member*
    "}"
    ;

declaration ::=
    documentation?
    (
        type-declaration
      | errno-declaration
      | function-declaration
    )
    ;

type-declaration ::=
    "type" IDENT type-expression ";"
    ;

errno-declaration ::=
    "errno" IDENT unsigned-integer-literal ";"
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
    | record-type
    | enum-type
    ;

type-reference ::=
      IDENT
    | "intercall" "." IDENT ("." IDENT)*
    ;

list-type ::=
    "list" "{" type-expression "}"
    ;

record-type ::=
    "record" "{"
        field*
    "}"
    ;

field ::=
    documentation?
    IDENT type-expression
    ";"
    ;

enum-type ::=
    "enum" integer-primitive-type "{"
        enum-member+
    "}"
    ;

enum-member ::=
    documentation?
    IDENT integer-literal ";"
    ;

integer-primitive-type ::=
      "int8"
    | "int16"
    | "int32"
    | "int64"
    | "uint8"
    | "uint16"
    | "uint32"
    | "uint64"
    ;

primitive-type ::=
      "boolean"
    | integer-primitive-type
    | "float32"
    | "float64"
    | "string"
    | "bytes"
    ;

integer-literal ::=
      unsigned-integer-literal
    | "-" NONZERO_DIGIT DIGIT*
    ;

unsigned-integer-literal ::=
      "0"
    | NONZERO_DIGIT DIGIT*
    ;

DIGIT ::=
    "0" ... "9"
    ;

NONZERO_DIGIT ::=
    "1" ... "9"
    ;

IDENT ::=
    IDENT_WORD ("_" IDENT_WORD)*
    ;

IDENT_WORD ::=
    LOWER (LOWER | DIGIT)*
    ;

LOWER ::=
    "a" ... "z"
    ;

documentation ::=
    "/*" DOCUMENTATION_TEXT "*/"
    ;
```

`DOCUMENTATION_TEXT` is any sequence of Unicode scalar values that does not
contain `*/`. Integer literals are single decimal tokens and have no sign
except a leading `-` for a negative enum discriminant. They have no leading
zeroes, `+` sign, or negative-zero form. Portable identifiers consist of
lowercase ASCII words separated by single underscores. Digits may occur after
the first letter of a word. The reserved words are `namespace`, `intercall`,
`type`, `errno`, `function`, `list`, `record`, and `enum`, together with every
primitive type name.

A document declares its root namespace once. A nested namespace has one lexical
declaration and cannot be reopened. Child namespace names and declaration names
share one scope and are unique within their parent. Field names are unique
within a record, parameter names are unique within a function, and member names
and discriminants are each unique within an enum. Enum discriminants must fit
the enum's backing type. Every fully qualified declaration name begins with
`intercall`.

Application contracts are declared in child namespaces such as
`intercall.arithmetic`; those namespaces may themselves be nested. Direct type,
errno, and function declarations in `intercall` are reserved for the protocol.
Protocol declarations and application child namespaces share the root member
name scope, so their names must not collide.

Declarations may refer to types declared later in the document. An unqualified
type reference resolves only in the current namespace. A qualified type
reference is absolute, begins with `intercall`, traverses zero or more nested
namespaces, and ends at a type declaration. Definition processors must preserve
record field and function parameter order. Enum member order is not semantic.
After resolving references, processors must reject every type cycle that is not
list-guarded. The Errors section defines errno scope and validation.

A function has ordered parameters and either no return value or one unnamed
return value:

```text
function ping();
function notify(message string);
function add(a int32, b int32) int32;
function get_users() list{intercall.accounts.user};
function create_user(input record { name string; }) intercall.accounts.user;
```

Functions are declarations, not values. Whether a native implementation is
synchronous or asynchronous does not change its portable contract or function
ID. A function's possible errnos are derived from its namespace lineage rather
than listed on the function.

### Identifier Projection

Portable identifiers encode word boundaries in lower snake case. Projection
between portable identifiers and native names is implementation-defined; native
spelling is not preserved in the IR. Two implementations targeting the same
language or language revision may use different abbreviation dictionaries,
keyword escaping, namespace mappings, source annotations, helper names, or
other conventions.

An exporter must resolve any native projection collisions before emitting a
valid document. Changing its projection rules can change the portable function
names it emits and therefore the IDs of newly generated functions. It cannot
change the IDs in an existing IR document, whose portable function names are
already fixed. An importer may expose any native API that preserves the
portable contract.

Only a function's fully qualified portable IR name participates in its function
ID. Native projection choices do not.

## Wire Protocol

### Transport and Peers

InterCall requires a reliable, ordered sequence of bytes. It does not depend on
transport message boundaries. Unix domain stream sockets, TCP, TLS over TCP,
and binary WebSocket streams are possible transports; transport bindings define
how their APIs expose the InterCall byte stream.

Each connection has an initiator and an acceptor only for allocating request ID
ranges. After transport establishment, the peers are otherwise symmetric and
either may send a request immediately. There is no InterCall handshake,
registry exchange, magic value, or embedded protocol version. Before exchanging
InterCall bytes, peers must establish compatible wire and definition semantics,
the initiator and acceptor roles, and the exact export document for each peer
out of band. How they do so is implementation-defined. InterCall does not detect
or negotiate a mismatch.

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
| Record, named or anonymous | Fields encoded in declaration order |
| Enum, named or anonymous | Its backing integer encoding, restricted to declared discriminants |
| Named type | Encoding of its underlying type expression |

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
or total length; its definition determines its layout. An empty record,
including an anonymous one, therefore occupies zero payload bytes. An enum uses
its declared discriminant rather than a native ordinal; an undeclared
underlying value is malformed.

Request arguments are encoded consecutively in declaration order. A successful
return value uses the same encoding as any other value of its declared type.

### Function IDs

Every function is identified by the complete 64-bit FNV-0 hash of the ASCII
bytes of its fully qualified portable name:

```text
hash = 0
prime = 1099511628211

for each name byte:
    hash = hash * prime modulo 2^64
    hash = hash XOR byte
```

The hash input contains exactly the name bytes, with `.` between namespace and
function identifiers. It has no length prefix, terminator, initial offset, seed,
or domain marker. Because portable identifiers are ASCII, their ASCII and UTF-8
encodings are identical. Hash value `0` is valid and not reserved. The resulting
`uint64` is encoded little-endian.

For example:

```text
name:        intercall.users.add_user
function ID: 0x55f5399d9023d46f
wire bytes:  6f d4 23 90 9d 39 f5 55
```

Parameter names and types, return types, named and anonymous type definitions,
enum members, errnos, documentation, source formatting, native identifier
spelling, and unrelated declarations do not participate in a function ID.
Changing any of them without changing the fully qualified function name leaves
the ID unchanged. Moving or renaming the function changes the ID.

A function ID is therefore a name-derived dispatch key, not a fingerprint of the
function contract. Peers must use matching out-of-band documents. If their
contracts for one name differ, InterCall does not detect the mismatch; each peer
encodes, decodes, and validates the call according to its own document.

A definition processor must reject a peer document in which distinct functions
have the same ID. Protocol-level functions use the same algorithm and ID space
as other functions. A collision between mismatched peer documents is not
detected in band. Function IDs are not authentication or authorization tokens.

There is no compatibility negotiation. An unchanged function ID does not imply
a compatible contract:

- independently updated peers can continue calling a function only while they
  use compatible contracts for its name;
- changing a contract under the same name preserves its ID but requires the
  peers' out-of-band documents to be updated together;
- a missing function receives `intercall.unknown_function` after its bounded
  payload has been skipped; and
- old and new contracts can coexist only under distinct fully qualified names.

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
contains its return value, or is empty for a function with no return value. A
response with nonzero `errno` has `payload_size == 0` and no payload.

A decoder must bound all decoding to the declared frame payload. Request
arguments and successful return values must consume exactly that many bytes. A
decoder must not read into the next frame to complete a malformed value. A
bounded request for an unknown function is skipped as opaque bytes and answered
with `intercall.unknown_function`. A frame whose declared size exceeds a safe
implementation limit may cause the connection to close without consuming the
payload.

Both peers may have multiple outstanding requests, and responses may be sent in
any order. Bytes from different frames must not be interleaved. Implementations
must continue processing inbound frames while calls are outstanding to avoid
reader-loop deadlocks. InterCall does not prevent deadlocks caused by
application-level dependency cycles.

## Errors

Every response contains one numeric `errno` field. Value `0` is the fixed
success sentinel and cannot be declared. Every errno value must fit `uint64` and
be nonzero, so every value from `1` through `2^64-1` is available to an explicit
declaration. No numeric range is reserved for the protocol or for applications,
and an undeclared value has no implicit meaning.

### Protocol Declarations

Every document must contain these direct declarations in its root `intercall`
namespace. Their names, values, and protocol meanings are fixed. The comments
are documentation and do not affect function IDs.

```text
namespace intercall {
    /* The receiver does not export the requested function ID. */
    errno unknown_function 1;

    /* A known function's payload is not its exact declared argument encoding. */
    errno malformed_request 2;

    /* The receiver failed internally while executing or encoding the call. */
    errno internal_error 3;

    /* A safely bounded request was rejected by a receiver resource limit. */
    errno resource_exhausted 4;
}
```

These declarations assign only the individual values `1`, `2`, `3`, and `4`.
For example, value `5` has no protocol significance unless an explicit errno in
the relevant namespace lineage declares it.

The root namespace is not limited to errnos. An InterCall protocol edition may
also declare protocol-level types and functions directly in `intercall`.
Protocol functions use the ordinary request, response, function-ID, and errno
rules. Direct type, errno, and function declarations in the root are owned by
the protocol; application declarations belong in child namespaces.

Peers select their protocol and definition semantics out of band; InterCall has
no embedded edition mechanism or edition negotiation. Definition processors
validate every protocol-owned declaration against the selected semantics and
reject missing, unknown, or mismatched declarations. An application cannot
create an arbitrary direct root declaration by calling it protocol-level.

Only the runtime, including a protocol-function implementation, may produce a
root protocol errno. An application handler result is resolved against the
function's lineage: a root protocol value or a value absent from the lineage
becomes `intercall.internal_error` when the runtime can respond safely.
Implementations may expose symbolic native errors, but symbolic namespace
provenance is not carried on the wire. If sibling namespaces reuse a number,
that number in an application handler result denotes the unique declaration in
the current function's lineage. Transport failures and local timeout or cancellation
failures are not response errnos and are not declared in the IR.

### Namespace Lineage

For a function declared in namespace `N`, the permitted errno set is the union
of errnos declared directly in `N` and in every ancestor namespace through
`intercall`. There is no function-level `errors` clause. Shared errnos belong in
the narrowest ancestor that contains every function allowed to return them.
With the required root protocol errnos omitted, an application subtree can
contain:

```text
namespace intercall {
    namespace preferences {
        /* Available to functions here and in child namespaces. */
        errno unsupported_locale 5;

        function set_locale(value string);

        namespace administration {
            errno permission_denied 6;
            function set_default_locale(value string);
        }
    }
}
```

`intercall.preferences.set_locale` permits the root protocol errnos and
`intercall.preferences.unsupported_locale`. The administration function also
permits `intercall.preferences.administration.permission_denied`.

Errno values must be unambiguous along every namespace lineage. No two errnos in
a namespace may share a value, and an errno cannot reuse an ancestor's value.
Sibling namespaces may reuse a value because the pending function identifies
one lineage. Adding an ancestor errno that collides with a descendant errno
makes the document invalid.

A requester maps a nonzero response using the pending function's lineage.
Receiving a value absent from that lineage is a malformed response and is fatal
to the connection. Adding, removing, renaming, or renumbering an errno changes
the contracts of functions in that namespace and its descendants, but not their
function IDs. Functions in sibling namespaces are unaffected.

An errno's human-facing description is documentation rather than a string
literal or wire value. InterCall deliberately has no portable runtime error
message. Documentation can be reworded or localized without changing a
function ID. Generated runtime errors should identify at least the fully
qualified symbolic name and numeric value; implementations may also expose the
description through normal API documentation. Dynamic error details are not
supported by this first error model.

Remote calls remain fallible even when their native implementation has no
application failure path. Generated bindings project failures into the target
language's idiom: for example, a Go `error`, a Rust `Result`, Swift error
handling, Java, Kotlin, C#, Python, or Ruby exceptions, JavaScript or
TypeScript promise rejection, or a C status result. InterCall defines no
exception hierarchy or typed error payload. Exact native error mappings are
implementation-defined.

For `errno == 0`, the payload encodes the declared return value exactly, or is
empty when the function has no return value. For `errno != 0`, `payload_size`
must be zero. A failed call has no return value.

### Replyable and Fatal Failures

Framing separates failures that can be attributed to one request from failures
that make the connection unsafe. After consuming or skipping a safely bounded
request payload, a receiver responds as follows:

- an unknown function returns `intercall.unknown_function`;
- invalid, truncated, or trailing argument encoding for a known function,
  including an invalid string, boolean, enum, or composite value, returns
  `intercall.malformed_request`;
- rejection by a decoding or execution resource policy returns
  `intercall.resource_exhausted` when the frame can still be safely contained;
- an unexpected handler failure or invalid return value returns
  `intercall.internal_error` if no response bytes have been sent; and
- an application failure returns an application-owned errno from the function's
  namespace lineage.

A peer closes the connection when it cannot continue unambiguously or safely.
Examples include an invalid or impossible header or state transition, an
unknown response ID, a reused request ID that makes state ambiguous, an unsafe
oversized frame, arithmetic overflow needed to process a frame, transport
truncation, or a partially written invalid response. A malformed response is
also fatal, including a successful payload that does not exactly match its
return type, a nonzero response with a nonempty payload, or an errno absent from
the pending function's lineage. Because a response cannot be answered with
another response, closing is the only protocol action. Closing fails all
outstanding calls.

A declared application errno is a normal response and does not by itself close
the connection.

## Connection Lifecycle and Resource Safety

Transport establishment establishes the InterCall connection; there is no
additional handshake. Closing or half-closing the transport closes the
InterCall connection. Active handlers are abandoned or cancelled according to
implementation policy. InterCall currently defines no graceful shutdown, retry,
or wire cancellation operation.

A request completes after its successful return value has been decoded or its
nonzero response has been validated to have an empty payload. The requester may
release its pending state but must not reuse the request ID. If a local timeout
or cancellation stops waiting, a tombstone must retain the function's return
contract and lineage errno mapping. The eventual response is validated as if
the caller were still waiting, then its result is discarded. An implementation
unwilling to retain that state must close the connection when the call is
abandoned.

Implementations may bound frame payload size, string and byte lengths, list
counts, nesting depth, decoded allocation, buffered output, outstanding calls,
concurrent handlers, definition complexity, and connection lifetime. They must
check limits and arithmetic overflow before allocation or conversion from
`uint64` to a native size. The wire format's numeric range does not require an
implementation to allocate or accept every representable size.

## Open Design Questions

The following protocol points are intentionally unsettled:

- request-ID reuse detection, abandoned-call state, and the complete connection
  state machine;
- failure precedence and response construction when encoding or writing fails;
  and
- default resource limits and exact limit-related behavior.

Native exporter and importer APIs, source annotations, identifier projection,
error mapping, generated signatures, ownership conventions, supported language
revisions, and implementation-language priorities are implementation choices,
not protocol design questions.
