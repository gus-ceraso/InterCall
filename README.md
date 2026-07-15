# InterCall

## Introduction

InterCall provides portable function definitions and bidirectional remote
procedure calls.

Applications often describe the same capability several times: as a server
function, a wire message, client types, validation rules, documentation, SDK
methods, and AI tools. These descriptions can drift as the application changes.

InterCall defines a remotely callable capability once as a documented function
with portable argument and return types. Language-specific toolchains export
InterCall definition documents and generate bindings for other popular
general-purpose programming languages. The same definitions can also drive
validation, documentation, and agent tools. Each language can provide an
idiomatic toolchain; the definitions and runtime protocol are the portable
contracts between them.

InterCall carries function calls over a small binary, bidirectional
request-response protocol. Either peer may call functions, so server operations,
callbacks, and requests for client state use the same mechanism. Multiple
calls may be outstanding concurrently.

InterCall deliberately supports only data types that map predictably across
popular general-purpose programming languages. It does not attempt to model
every distributed-system interaction or replace interfaces that need different
semantics, such as streaming or offline delivery. Its goal is to provide a
small, portable foundation for generated function calls.

## Installation

TODO

## Examples

The following definition document declares types and functions in two
namespaces:

```text
/* Arithmetic operations. */
namespace arithmetic {
    /* Adds two 32-bit integers. */
    function add(
        /* The first integer. */
        a int,

        /* The second integer. */
        b int
    ) int;
}

/* Peer preferences. */
namespace preferences {
    /* A locale identifier such as "en-US". */
    type locale = string;

    /* Returns the peer's locale. */
    function get_locale() locale;

    /* Changes the peer's locale. */
    function set_locale(value locale);
}
```

If Peer A exports `arithmetic.add` and Peer B exports the `preferences`
functions, Peer B can call `arithmetic.add` with `a = 2` and `b = 3` and receive
`5`. Peer A can use the same connection to call `preferences.get_locale` and
receive a value such as `"en-US"`. A call to `preferences.set_locale` returns no
value.

On the wire, each function is identified by a 64-bit hash of its definition,
and values are encoded according to their declared types.

## Specification

### Data Types

InterCall supports booleans, signed and unsigned integers, floating-point
numbers, strings, bytes, lists, records, and application-defined types.
Nullable, optional, union, and function types are not supported.

#### Boolean

`boolean` has two values: `true` and `false`.

#### Integers

InterCall supports fixed-width signed and unsigned integers. `int` is an alias
for `int32`, and `uint` is an alias for `uint32`.

| Type | Minimum | Maximum |
| --- | ---: | ---: |
| `int8` | -128 | 127 |
| `int16` | -32,768 | 32,767 |
| `int`, `int32` | -2,147,483,648 | 2,147,483,647 |
| `int64` | -9,223,372,036,854,775,808 | 9,223,372,036,854,775,807 |
| `uint8` | 0 | 255 |
| `uint16` | 0 | 65,535 |
| `uint`, `uint32` | 0 | 4,294,967,295 |
| `uint64` | 0 | 18,446,744,073,709,551,615 |

#### Floating Point

`float32` and `float64` are IEEE 754 binary32 and binary64 values,
respectively. They support finite values, positive and negative zero, positive
and negative infinity, and NaN.

#### String

`string` is a sequence of Unicode scalar values. It represents text rather than
encoded binary data.

#### Bytes

`bytes` is an ordered sequence of zero or more raw bytes. It represents binary
data directly, without Base64 or another text encoding.

#### List

A list is an ordered sequence of zero or more values of one element type. All
values in a list have the same type. The element type may be any InterCall type,
including another list or an anonymous record.

#### Record

A record is a fixed collection of named fields. Fields may have different
types, but every declared field is required. Additional fields are not allowed.

#### Application-Defined Types

An application-defined type is a named, transparent alias for any InterCall
type, including another application-defined type, a list, or a record. A local
type is referenced by its name. A type in another namespace is referenced as
`namespace.type`.

Application-defined types may be recursive only through lists. Every cycle of
type references must pass through at least one list. For example, this tree type
is valid:

```text
type tree = record {
    value string;
    children list{tree};
};
```

Direct record recursion and pure alias cycles are invalid:

```text
type invalid_record = record {
    next invalid_record;
};

type invalid_alias_a = invalid_alias_b;
type invalid_alias_b = invalid_alias_a;
```

All InterCall values are finite trees. Object identity and cyclic runtime values
are not part of the InterCall data model. Implementations must reject cyclic
values when encoding them.

#### Zero Values

Every InterCall type has a zero value:

| Type | Zero value |
| --- | --- |
| `boolean` | `false` |
| Signed and unsigned integers | `0` |
| `float32`, `float64` | Positive zero |
| `string` | Empty string |
| `bytes` | Empty sequence |
| List | Empty list |
| Record | A record containing the zero value of every field |
| Application-defined type | The zero value of its underlying type |

List-guarded recursion ensures that the zero value of every valid recursive type
is finite.

### Definition Format

An InterCall definition document is a UTF-8 text file containing one or more
namespaces. It has no header or embedded format version. Consumers interpret a
document according to the InterCall specification version they support.

Outside documentation blocks, whitespace may appear between tokens without
changing their meaning. Spaces, tabs, carriage returns, line feeds, form feeds,
and vertical tabs are ignored between tokens. Whitespace is required only when
its absence would combine adjacent tokens. Semicolons terminate type and
function declarations and record fields. Commas separate function arguments,
braces delimit namespaces and composite types, and parentheses delimit
argument lists. Newlines and indentation have no semantic meaning.

#### Documentation

A `/* ... */` block contains documentation for the namespace, type, function,
argument, or record field that immediately follows it. Every comment is
documentation; there are no non-documentation or line comments. Documentation
association does not depend on whitespace or line breaks.

Documentation is optional, and an item may have at most one documentation
block. Comments cannot nest and end at the first `*/`. The documentation value
is the UTF-8 text between the delimiters with leading and trailing whitespace
removed. It therefore cannot contain the sequence `*/`.

A function's documentation also describes its unnamed return value when one is
present.

#### Grammar

The definition format uses the following grammar:

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
    "type" IDENT "=" type-expression ";"
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
    ;

type-reference ::=
    IDENT ("." IDENT)?
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

primitive-type ::=
      "boolean"
    | "int"
    | "int8"
    | "int16"
    | "int32"
    | "int64"
    | "uint"
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
contain `*/`. Text matching `IDENT` is recognized as an identifier only when it
is not a reserved keyword or primitive type name. Identifiers are
case-sensitive.

#### Namespaces and Declarations

A document must declare each namespace exactly once. Declaration names must be
unique within their namespace. Field names must be unique within their record,
and argument names must be unique within their function.

A namespace may contain type and function declarations in any order. Type
references may refer to types declared later in the document. An unqualified
reference resolves within the current namespace; a qualified reference has the
form `namespace.type` and resolves within the same document.

Definition processors must preserve record field and function argument order.

#### Types

A type declaration introduces a transparent alias for its type expression. Type
expressions may contain primitive types, references to application-defined
types, lists, or records.

A list encloses exactly one element type in braces:

```text
type names = list{string};
type matrix = list{list{float64}};
type points = list{record {
    x float64;
    y float64;
}};
```

A record contains zero or more ordered fields. Each field consists of a name, a
type expression, and a semicolon:

```text
type user = record {
    id uint64;
    name string;
    avatar bytes;
};
```

After resolving all type references, every reference cycle must pass through at
least one list. This rule permits finite recursive structures while rejecting
direct record recursion and alias-only cycles.

#### Functions

A function declaration contains a name, an ordered argument list, and an
optional unnamed return type. Arguments consist of a name followed by a type
expression and are separated by commas.

```text
function ping();
function notify(message string);
function add(a int32, b int32) int32;
function get_users() list{user};
```

If no type follows the closing parenthesis, the function returns no value.
Otherwise, it returns exactly one value of the declared type.

Functions are declarations rather than values. They cannot be used as
arguments, return values, list elements, record fields, or targets of type
aliases.

### Wire Protocol

#### Transport

InterCall requires a reliable, ordered sequence of bytes. The protocol does not
select a transport and does not use transport message boundaries.

> **Implementation note:** TCP, TLS over TCP, Unix domain stream sockets, and
> binary WebSocket connections are possible transports. Authentication,
> encryption, and the mapping between a transport API and the InterCall byte
> stream are implementation concerns.

Each transport integration designates one peer as the connection initiator and
the other as the connection acceptor. Once the transport is established, either
peer may send a request immediately. InterCall has no handshake, magic value,
or embedded protocol version. Peers must ensure protocol and definition
compatibility out of band.

#### Byte Order

All multibyte integers, lengths, counts, function IDs, request IDs, and
floating-point bit patterns use little-endian byte order. Values are encoded
without alignment or padding. Implementations must not serialize native record
or structure memory directly.

#### Value Encoding

Values are encoded according to their resolved InterCall types:

| Type | Encoding |
| --- | --- |
| `boolean` | One byte: `0` for false or `1` for true; other values are invalid |
| `int8`, `uint8` | One byte |
| `int16`, `uint16` | Two bytes |
| `int`, `int32`, `uint`, `uint32` | Four bytes |
| `int64`, `uint64` | Eight bytes |
| `float32` | Four-byte IEEE 754 binary32 bit pattern |
| `float64` | Eight-byte IEEE 754 binary64 bit pattern |
| `string` | `uint64` byte length followed by UTF-8 bytes |
| `bytes` | `uint64` byte length followed by raw bytes |
| `list{T}` | `uint64` element count followed by each encoded `T` value |
| Record | Encoded fields in declaration order |
| Application-defined type | Encoding of the underlying type |

Signed integers use two's-complement representation. Integers and counts are
fixed-width rather than variable-length.

Strings must contain valid UTF-8 encodings of Unicode scalar values. Overlong
encodings, surrogate code points, truncated sequences, and other invalid UTF-8
are not allowed. InterCall does not normalize Unicode strings.

Finite floating-point values, infinities, and positive and negative zero use
their corresponding IEEE 754 bit patterns. NaN has no portable sign or payload.
Encoders must use the canonical quiet NaN bit patterns `0x7fc00000` for
`float32` and `0x7ff8000000000000` for `float64`; decoders must reject other NaN
bit patterns.

A list count describes values rather than bytes. List elements are encoded
consecutively without individual lengths. A record has no encoded field count,
field names, or total length because its definition supplies that information.
An empty record therefore occupies zero bytes.

Function arguments are encoded consecutively in declaration order. A return
value, when present, uses the same encoding as any other value of its declared
type.

#### Function IDs

A function ID is the complete 64-bit FNV-0 hash of a canonical representation
of the function definition. FNV-0 is defined as:

```text
hash = 0
prime = 1099511628211

for each canonical byte:
    hash = hash * prime modulo 2^64
    hash = hash XOR byte
```

There is no initial offset, seed, or domain marker. Hash value `0` is valid and
is not reserved. The resulting `uint64` is encoded in little-endian order.

The exact canonical function representation is **TODO**. It must:

- be deterministic across all implementations;
- include the fully qualified function name, ordered arguments, optional return
  type, and every reachable application-defined type needed by the contract;
- handle recursive types without infinitely expanding them;
- normalize `int` to `int32` and `uint` to `uint32`;
- exclude documentation, whitespace, source formatting, and unrelated
  declarations; and
- remain stable when unrelated declarations are added or reordered.

Definition toolchains must reject distinct functions that produce the same
64-bit ID within one generated function registry. Function IDs identify
functions for dispatch; they do not provide authentication or authorization.

#### Request IDs

The most significant bit of a request ID identifies the peer that initiated the
request:

- The connection initiator uses IDs from `0x0000000000000000` through
  `0x7fffffffffffffff`.
- The connection acceptor uses IDs from `0x8000000000000000` through
  `0xffffffffffffffff`.

The remaining 63 bits are selected by the initiating peer. A peer must not reuse
a request ID during the lifetime of a connection. If a peer exhausts its ID
space, it may continue responding to requests but cannot initiate another one
on that connection.

A response repeats the request ID exactly. When a peer receives an ID from the
remote peer's range, the message is a request. When it receives an ID from its
own range, the message is a response.

#### Messages

A request consists of:

```text
request_id  uint64
function_id uint64
arguments   encoded according to the function definition
```

A response consists of:

```text
request_id   uint64
function_id  uint64
return_value encoded according to the function definition, when present
```

Both message headers occupy 16 bytes. A request without arguments and a
successful response without a return value consist only of their headers.

A response must repeat both the request ID and function ID of its request. The
requester uses its pending request state and the repeated function ID to select
and validate the return type.

The function definition determines how to decode the bytes following a header,
and embedded lengths and counts delimit variable-size values. Messages therefore
need no total length. The next byte after the final argument or return value
begins the next message.

Both peers may have multiple outstanding requests. Responses may be sent in any
order. Bytes belonging to separate messages must never be interleaved; each
implementation must serialize complete outgoing messages onto the transport.
Implementations must continue processing inbound messages while requests are
outstanding so bidirectional calls cannot deadlock.

#### Application Errors

InterCall does not give application errors a special wire representation. They
are ordinary return values defined by the application. For example:

```text
type user_result = record {
    value user;
    error string;
};
```

An application may use an empty `error` as success and return the zero value of
`value` on failure. It may instead define another convention or return partial
values. InterCall assigns no special meaning to the field name `error` or to an
empty string.

A function that declares no return value has no application-level error result.
It must declare a return type if the application needs to report one.

#### Connection Lifecycle

Transport establishment completes InterCall connection establishment. There is
no additional negotiation. Both peers may initiate calls, and a response may be
sent as soon as its handler completes.

A request completes after its response has been fully decoded. The requester
may then release its pending state, but it must never reuse the request ID on
that connection.

Local timeouts and cancellation are implementation concerns and do not send
InterCall messages. An implementation that stops waiting for a response must
retain a tombstone for the request so it can recognize and discard a late
response, or it must close the connection. A response for a retained tombstone
is not an unknown response.

Closing the transport closes the InterCall connection, fails every outstanding
request, and abandons or cancels active handlers according to implementation
policy. InterCall does not define graceful shutdown or retry behavior. A
transport half-close is treated as complete connection closure.

#### Failures

A peer closes the connection when it detects any condition that prevents
unambiguous continued decoding or execution, including:

- an unknown function ID;
- an unknown response ID;
- a response whose function ID does not match its request;
- a reused request ID, when detected;
- malformed or invalid encoded data;
- invalid handler outputs;
- a handler crash; or
- an implementation resource limit being exceeded.

No InterCall error message is sent before closing. Every outstanding request on
the connection fails. A valid application error returned as ordinary data is
not a protocol failure and does not close the connection.

#### Implementation Limits

The InterCall specification does not prescribe numeric resource limits. An
implementation may limit value lengths, list counts, recursive nesting, decoded
allocation, buffered output, outstanding requests, concurrent handlers, and
connection lifetime. Definition toolchains may separately reject definitions
that exceed their local complexity limits.

Implementations should check local limits and arithmetic overflow before
allocating memory or converting `uint64` values to native sizes. Exceeding a
connection or decoding limit closes the connection. Implementations are not
required to accept every length or count representable by the wire format.
