# InterCall

Easy Remote Procedure Calling

## Introduction

InterCall is a bidirectional [remote procedure call](https://www.bitsavers.org/pdf/xerox/parc/techReports/CSL-81-9_Remote_Procedure_Call.pdf)
meta-protocol. It defines a portable interface format and a compact binary wire
format while leaving tooling, native APIs, connection management, and transport
bindings to implementations.

Each connection joins two peers. A peer exports an interface describing the
procedures it accepts and the exceptions it may throw; the other peer imports
that interface. Either peer may call the other, including while handling an
incoming call. Any binding may carry InterCall frames if it reliably delivers
every frame in full and preserves the sequence of bytes within each frame.

### Scope

InterCall models calls whose arguments, returns, and exceptions are finite,
acyclic values. It does not define streaming parameters or results, shared
memory, object identity, offline delivery, service discovery, deployment, or
general distributed-system workflows. Transport streams, including QUIC
streams, carry complete frames; they do not make procedure parameters or
returns streaming.

## Hello, world!

A small example is the quickest way to see how InterCall works. We will call a
Go function from a browser, then call a browser function from Go over the same
connection. We will use the generated code, but we will not edit it.

Assume that `intercall-go` and `intercall-ts` are on `PATH`. Make a new module:

```sh
mkdir intercall-hello
cd intercall-hello
go mod init hello
```

### Write `Hello` in Go

Create `hello.go`:

```go
package main

import (
	"context"
	"fmt"
)

// Hello returns a personalized greeting.
//
// @intercall
// @param name the name to greet
// @return the personalized greeting
func Hello(ctx context.Context, name string) (string, error) {
	return fmt.Sprintf("Hello, %s!", name), nil
}
```

### Translate it

```sh
intercall-go export hello.go \
    --interface hello.intercall \
    --out intercall
```

Here is `hello.intercall`:

```text
/* Hello returns a personalized greeting. */
procedure hello {
    /* the name to greet */
    name string;
} /* the personalized greeting */ string;
```

Generated interfaces are meant to be read and checked in, but not changed.

### Write the server

Create `main.go`:

```go
package main

import (
	"log"

	"hello/intercall"
)

func main() {
	log.Fatal(intercall.WSListenAndServe(":8080"))
}
```

`WSListenAndServe` accepts WebSocket connections at `ws://localhost:8080`. The
generated code already knows about `Hello`.

### Generate the TypeScript caller

Translate `hello.intercall` to TypeScript:

```sh
mkdir -p web
intercall-ts import hello.intercall --out web/intercall
```

The new `web/intercall` module contains the TypeScript caller and runtime.

### Write the browser

Create `web/main.ts`:

```ts
import { connect } from "./intercall";

async function main(): Promise<void> {
    const app = document.querySelector<HTMLElement>("#app")!;
    app.innerHTML = `
        <form id="hello-form">
            <label>
                Your name
                <input name="name" autocomplete="name" required>
            </label>
            <button type="submit" disabled>Greet me</button>
        </form>
        <p id="greeting" role="status"></p>
    `;

    const form = document.querySelector<HTMLFormElement>("#hello-form")!;
    const button = form.querySelector<HTMLButtonElement>("button")!;
    const greeting = document.querySelector<HTMLElement>("#greeting")!;
    const peer = await connect("ws://localhost:8080");

    button.disabled = false;
    form.addEventListener("submit", async (event) => {
        event.preventDefault();
        button.disabled = true;

        try {
            const data = new FormData(form);
            const name = String(data.get("name") ?? "");
            greeting.textContent = await peer.hello(name);
        } catch (error) {
            greeting.textContent = `Call failed: ${String(error)}`;
        } finally {
            button.disabled = false;
        }
    });
}

void main().catch((error) => {
    document.querySelector<HTMLElement>("#app")!.textContent =
        `Connection failed: ${String(error)}`;
});
```

`connect` runs once. Every form submission uses the same peer.

The program also needs a page. Create `web/index.html`:

```html
<!doctype html>
<html lang="en">
<head>
    <meta charset="utf-8">
    <meta name="viewport" content="width=device-width, initial-scale=1">
    <title>InterCall hello</title>
</head>
<body>
    <main id="app"></main>
    <script src="./app.js"></script>
</body>
</html>
```

### Build and run

Bundle the program into a classic browser script:

```sh
npx --yes esbuild web/main.ts \
    --bundle \
    --format=iife \
    --outfile=web/app.js
```

The IIFE bundle works from a local file, so the example requires neither a
TypeScript configuration nor a static server. The contents of `app.js` could be
inlined, but a separate file keeps the build command short.

In another terminal, start Go:

```sh
go run .
```

Now open `web/index.html` directly in a browser. Enter `Alice` and press
**Greet me**. The page displays:

```text
Hello, Alice!
```

### Add a TypeScript function

Now add a call in the other direction. Create `web/locale.ts`:

```ts
/**
 * Returns the browser's preferred locale.
 *
 * @intercall
 * @returns the browser's preferred locale
 */
export function locale(): string {
    return navigator.language;
}
```

### Translate `locale`

Export the TypeScript function:

```sh
intercall-ts export web/locale.ts \
    --interface browser.intercall \
    --out web/intercall
```

Here is `browser.intercall`:

```text
/* Returns the browser's preferred locale. */
procedure locale {
} /* the browser's preferred locale */ string;
```

Then translate the interface for Go:

```sh
intercall-go import browser.intercall --out intercall
```

The first command adds a `locale` dispatcher to the browser runtime. The second
adds a `locale` caller to the Go runtime.

### Call `locale` from Go

Replace `hello.go` with:

```go
package main

import (
	"context"
	"fmt"
	"strings"

	"hello/intercall"
)

// Hello returns a personalized greeting.
//
// @intercall
// @param name the name to greet
// @return the personalized greeting
func Hello(ctx context.Context, name string) (string, error) {
	locale, err := intercall.Locale(ctx)
	if err != nil {
		return "", err
	}

	greeting := "Hello"
	if strings.HasPrefix(strings.ToLower(locale), "pt") {
		greeting = "Olá"
	}
	return fmt.Sprintf("%s, %s!", greeting, name), nil
}
```

`intercall.Locale(ctx)` takes the current connection from `ctx` and calls the
browser. The connection itself remains hidden. The `hello` contract has not
changed, so there is no need to translate it again.

### Run it again

Rebuild the browser bundle:

```sh
npx --yes esbuild web/main.ts \
    --bundle \
    --format=iife \
    --outfile=web/app.js
```

Restart `go run .` and reload `web/index.html`. With the browser locale set to
English, entering `Alice` still produces:

```text
Hello, Alice!
```

Set the browser's preferred language to Portuguese and reload the page. The
same form now displays:

```text
Olá, Alice!
```

Calls now originate from both peers. The browser calls `hello`; before returning
the greeting, Go calls `locale` over the same WebSocket.

## Interface

InterCall tries to define interfaces that can be represented faithfully across
general-purpose languages. Portability takes precedence over mirroring every
source language. If a type or feature lacks a clear common representation,
InterCall omits it instead of assigning it language-specific semantics. Some
omitted concepts can be modeled explicitly with the supported constructs;
others remain outside InterCall.

An InterCall interface is a UTF-8 file containing the complete exported
interface for one peer. It declares the procedures that peer accepts and the
exceptions it may throw. The opposing peer imports the interface to make calls
and interpret responses.

The `.intercall` extension is conventional. A file is a sequence of type,
exception, and procedure declarations; it has no enclosing block, interface
name, namespace, version, header, import, or include. An empty interface is
valid.

For example:

```text
/* A user returned by this peer. */
type user record {
    name string;
    sex uint8;
};

/* An implementation-defined failure without a payload. */
exception unknown;

/* Another implementation-defined failure. */
exception procedure_not_found
    /* The procedures known to the peer. */
    record {
        available_procedures list string;
    };

/* Finds a user by name. */
procedure get_user {
    /* The name to find. */
    name string;
} /* The matching user. */ user;

/* Draws an image and returns no value. */
procedure draw_image {
    colors list record {
        red uint8;
        green uint8;
        blue uint8;
    };
};
```

The exception names in this example are not standard. InterCall defines no
canonical exceptions or procedures.

### Data Types

#### Primitive Types

InterCall has the following primitive types:

| Type | Values |
| --- | --- |
| `int8`, `int16`, `int32`, `int64` | Exact-width signed integers |
| `uint8`, `uint16`, `uint32`, `uint64` | Exact-width unsigned integers |
| `float32`, `float64` | IEEE 754 binary32 and binary64 values |
| `string` | A sequence of Unicode scalar values |
| `bytes` | An ordered sequence of raw bytes |

Signed integer ranges are `-2^(N-1)` through `2^(N-1)-1`; unsigned ranges are
`0` through `2^N-1`. There are no native-width `int` or `uint` primitives.

The floating-point types include finite values, positive and negative zero,
positive and negative infinity, and NaN. The Transport section defines their
canonical wire encodings.

A string contains text rather than encoded binary data. It cannot contain
surrogate code points, because they are not Unicode scalar values. InterCall
does not normalize strings. The `bytes` type carries data without a text
encoding.

#### Lists

`list T` is an ordered sequence of zero or more values of `T`. `list` consumes
one complete type specifier, so `list list uint8` is a list of lists. The
element may also be a record or a named type.

#### Records

A record is a closed, ordered collection of named fields. Every field is
required, and no undeclared field is part of the value. A record type specifier
may appear anywhere a type specifier is accepted:

```text
type point record {
    x float64;
    y float64;
};

procedure transform {
    points list point;
    origin record {
        x float64;
        y float64;
    };
} list point;
```

`record {}` is valid and has a zero-byte wire representation. A record whose
fields all have zero-byte representations also has a zero-byte wire
representation. A list of such records still carries its element count.

#### Named Types

A type declaration gives a name to any type specifier:

```text
type user_id uint64;
type customer_id user_id;
type user_ids list user_id;
```

A named type uses the wire representation of its underlying type. Whether an
implementation represents it as an alias, wrapper, class, structure, or some
other native construct is implementation-defined.

A type reference may name only an earlier declaration, which prevents named
types from being recursive, even through lists:

```text
type tree record {
    children list tree; /* Invalid: tree is not available here. */
};
```

#### Unsupported Types

InterCall has no boolean, enum, optional, map, variant, character, pointer, or
callable-value type and no general constant declarations. It has no separate
unit type; `record {}` provides a zero-width value when one is needed. InterCall
performs no implicit coercion between the types it does define.

### Grammar

Comments and whitespace may appear before, between, and after tokens, including
in an otherwise empty interface, and do not affect the interface format.
Comments use `/* ... */`, do not nest, and end at the first `*/`. They
conventionally precede the item they document, as in the example, but comment
association and preservation are implementation-defined. A processor may
discard every comment.

Spaces, tabs, carriage returns, line feeds, form feeds, and vertical tabs are
whitespace. Whitespace is required only where omitting it would combine
adjacent tokens. A UTF-8 byte-order mark is not whitespace and is invalid.

```ebnf
interface ::=
    declaration*
    EOF
    ;

declaration ::=
      type-declaration
    | exception-declaration
    | procedure-declaration
    ;

type-declaration ::=
    "type" IDENT type-specifier ";"
    ;

exception-declaration ::=
    "exception" IDENT type-specifier? ";"
    ;

procedure-declaration ::=
    "procedure" IDENT "{"
        parameter*
    "}" type-specifier? ";"
    ;

parameter ::=
    IDENT type-specifier ";"
    ;

type-specifier ::=
      primitive-type
    | IDENT
    | list-type
    | record-type
    ;

list-type ::=
    "list" type-specifier
    ;

record-type ::=
    "record" "{"
        field*
    "}"
    ;

field ::=
    IDENT type-specifier ";"
    ;

primitive-type ::=
      "int8"
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
    (ASCII_LETTER | "_") (ASCII_LETTER | DIGIT | "_")*
    ;

ASCII_LETTER ::=
      "a" ... "z"
    | "A" ... "Z"
    ;

DIGIT ::=
    "0" ... "9"
    ;

comment ::=
    "/*" COMMENT_TEXT "*/"
    ;
```

`COMMENT_TEXT` is any sequence of Unicode scalar values that does not contain
`*/`. Comments and whitespace are omitted from the syntactic productions
above.

Identifiers follow the lexical form used by C: `_aJ1234z` is valid, while
`123Go` is not. Identifiers are ASCII and case-sensitive. The reserved words
are `type`, `exception`, `procedure`, `list`, and `record`, together with every
primitive type name. These exact lowercase spellings are unavailable in every
identifier position; differently cased spellings such as `Procedure` are valid.

Type, exception, and procedure declarations share one global name scope. Each
procedure parameter list and each record has its own local name scope. Names
must be unique within their scope, but a local name may equal a global name.
Local names do not affect type resolution. In a type position, an identifier
resolves case-sensitively to an earlier global type declaration.

Every declaration is validated even when no other declaration references it.
An interface is invalid if it contains unknown syntax, an unresolved type
reference, or a duplicate name in one scope. Nested type, exception, and
procedure declarations are not part of the format. Implementation-specific
extensions are outside the InterCall interface format.

### Procedures and Exceptions

#### Procedures

A procedure has an ordered list of named parameters and at most one unnamed
return value. Omitting the return type declares no return value:

```text
procedure ping {};
procedure notify {
    message string;
};
procedure add {
    a int32;
    b int32;
} int32;
```

A request payload contains the parameters in declaration order. Parameter names
and the enclosing braces are not encoded.

#### Exceptions and Responses

An exception has a name and at most one unnamed payload value. Omitting the
payload type declares an exception without a payload:

```text
exception unavailable;
exception invalid_input record {
    field string;
    reason string;
};
```

Every declared exception is available to every procedure, regardless of
declaration order. An interface may contain no exceptions.

InterCall assigns no special meaning to an exception name or payload. Runtime
exceptions are ordinary declarations chosen by an implementation. An
implementation must include every exception it can send in the peer interface,
whether that exception comes from application code or the runtime.

The exception key determines a response's payload:

| Exception key | Payload |
| --- | --- |
| `0` | The procedure's return value |
| A declared nonzero exception key | That exception's payload |

An exception response has no return value. An omitted return or exception
payload produces an empty payload, as does any selected type with a zero-byte
representation. The format does not require implementations to expose these
cases in the same way; a named zero-width type remains available to the
implementation.

In every case, the selected value must consume the frame payload exactly. The
Transport section defines the frame and value encodings.

### Procedure and Exception Keys

Procedures and exceptions have unsigned 64-bit keys derived from their exact,
case-sensitive names. The keys are intended for fast dispatch-table lookups.
The calculation uses 64-bit FNV-0:

```text
hash = 0
prime = 1099511628211

for each input byte:
    hash = hash * prime modulo 2^64
    hash = hash XOR byte
```

A procedure key hashes the ASCII bytes of the literal `procedure ` followed by
the procedure name. An exception key hashes `exception ` followed by the
exception name:

| Declaration | Key |
| --- | --- |
| `procedure get_user` | `0x4c63cc5048869eb7` |
| `exception procedure_not_found` | `0x970e76fcc5e2dacb` |

The space after each declaration kind is part of the input. The input has no
length prefix, terminator, initial offset, seed, or other marker. Because
identifiers contain only ASCII characters, encoding them as ASCII or UTF-8
produces the same bytes.

Key `0` is invalid for both procedure and exception declarations. A processor
must also reject a collision between any two procedure or exception
declarations in the same interface, including a collision between a procedure
key and an exception key. Keys are validated independently for each peer
interface and may repeat in opposing interfaces. Types do not have keys.

Only the declaration kind and exact name participate in the key. Parameters,
return and payload types, comments, native names, and unrelated declarations do
not. A key is a lookup value, not a fingerprint of the declaration's contract.

### Implementation-Defined Mappings

An implementation decides how interface declarations appear in a native
language. This includes identifier projection, keyword escaping, wrappers,
ownership, exception mapping, native error handling, synchronous or asynchronous
APIs, source annotations, comment handling, and the use of generated code,
reflection, interpreters, or handwritten adapters. An implementation may also
choose not to expose an otherwise valid declaration in a native API. These
choices do not change the interface or wire formats.

## Transport

InterCall defines a wire format, not a transport protocol. An implementation
chooses how to establish and close connections, agree on interfaces and protocol
versions, and carry InterCall frames. The transport must deliver the bytes of
each frame reliably and in order, but it need not permit both peers to transmit
at the same time.

A frame may span multiple transport messages, and one transport message may
contain multiple frames. On a shared byte stream, frames are consecutive and
their bytes are not interleaved. A binding may instead carry frames on
independent ordered streams and deliver the frames in any order. For example,
a QUIC binding may use independent streams to avoid head-of-line blocking. Other
possible bindings include a pair of Unix pipes such as standard input and
output, Unix domain stream sockets, TCP with or without TLS, and binary
WebSockets with or without TLS.

### Value Encoding

All multibyte integers, lengths, counts, request IDs, procedure and exception
keys, and floating-point bit patterns are little-endian. Values have no implicit
alignment or padding. Implementations must not serialize native structure memory
directly.

Values are encoded according to their resolved types:

| Type | Encoding |
| --- | --- |
| `int8`, `uint8` | One byte |
| `int16`, `uint16` | Two bytes |
| `int32`, `uint32` | Four bytes |
| `int64`, `uint64` | Eight bytes |
| `float32` | Four-byte IEEE 754 binary32 bit pattern |
| `float64` | Eight-byte IEEE 754 binary64 bit pattern |
| `string` | `uint64` byte length followed by UTF-8 bytes |
| `bytes` | `uint64` byte length followed by raw bytes |
| `list T` | `uint64` element count followed by each encoded `T` |
| Record | Fields encoded in declaration order |
| Named type | Encoding of its underlying type |

Signed integers use two's-complement representation. Integers, lengths, and
counts are fixed-width rather than variable-length.

A string must be a valid UTF-8 encoding of Unicode scalar values. Decoders
reject overlong encodings, surrogate code points, truncation, and all other
invalid UTF-8. InterCall does not normalize strings. A string length counts
encoded bytes, not Unicode scalar values.

Finite floating-point values, infinities, and signed zero use their IEEE 754 bit
patterns. Encoders emit the canonical quiet NaN `0x7fc00000` for `float32` and
`0x7ff8000000000000` for `float64`. Their wire bytes are `00 00 c0 7f` and
`00 00 00 00 00 00 f8 7f`, respectively. Decoders reject every other NaN bit
pattern; for example, `01 00 c0 7f` is invalid.

A list count is a count of values, not bytes. Elements are consecutive and have
no individual lengths or frames. A record has no encoded field count, field
names, padding, or enclosing length. Consequently, `record {}` occupies zero
bytes. A list of zero-width values still carries its element count.

### Frames

The most significant bit of the `request_id` field distinguishes requests from
responses. It is clear in a request and set in a response. The remaining 63 bits
form the request ID, from `0x0000000000000000` through
`0x7fffffffffffffff`. A response copies the request's ID into those bits.

Both peers use the same request ID range independently, so requests in opposing
directions may have the same ID. Request ID zero is valid. A peer must not have
two outstanding requests with the same ID. Once it receives the response to a
request, it may reuse that request's ID. A request abandoned locally, such as
after a timeout, remains outstanding for ID-allocation purposes until its
response arrives or the connection closes. The other peer is not required to
remember IDs across completed requests.

A request frame is:

```text
request_id     uint64
procedure_key  uint64
payload_length uint64
payload        payload_length bytes
```

A response frame is:

```text
request_id     uint64
exception_key  uint64
payload_length uint64
payload        payload_length bytes
```

Each header is 24 bytes. Its three fields begin at offsets 0, 8, and 16, with no
padding. `payload_length` counts only the bytes immediately following the
header. Value decoding is bounded by that length and must never consume bytes
from another frame.

A receiver uses the most significant bit of `request_id` to select the frame
layout, then clears that bit before matching a response to a pending request. No
separate frame-kind field is encoded.

Both peers may have multiple outstanding requests. Responses may arrive in any
order, and InterCall gives separate requests no execution-order guarantee. An
application that requires ordering waits for one request to complete before
issuing a dependent request. A peer must continue receiving frames while its
own requests are outstanding; handler scheduling is implementation-defined.
InterCall does not prevent deadlocks caused by dependency cycles between
application handlers.

### Requests and Responses

A request payload contains the procedure parameters encoded consecutively in
declaration order. It contains no parameter names, parameter count, or enclosing
record marker. The arguments must consume the frame payload exactly.

Every request has a response unless the connection closes. A procedure with no
return value still receives a successful response with an empty payload. The
response's exception key selects its payload:

| Exception key | Payload |
| --- | --- |
| `0` | The procedure's return value |
| A declared nonzero exception key | That exception's payload |

`payload_length` must be zero for an omitted return or exception payload and for
any selected zero-width type. Otherwise, the selected value must consume the
response payload exactly. An exception response contains no return value.

Every nonzero exception key sent by a peer must be declared in that peer's
interface. InterCall defines no canonical exceptions or exception meanings.
Each peer decides which exceptions to declare and when to throw them.

### Wire Example

Consider this interface:

```text
exception failed;
procedure echo {
    value uint16;
} uint16;
```

The procedure key is `0x0159eb91a98f8f42`, and the exception key is
`0x583fb304d69368ca`. The following fields form a request with request ID `1`
and argument `0x1234`:

```text
request_id     01 00 00 00 00 00 00 00
procedure_key  42 8f 8f a9 91 eb 59 01
payload_length 02 00 00 00 00 00 00 00
payload        34 12
```

A successful response returning `0x1234` sets the response bit:

```text
request_id     01 00 00 00 00 00 00 80
exception_key  00 00 00 00 00 00 00 00
payload_length 02 00 00 00 00 00 00 00
payload        34 12
```

The same procedure may instead throw the declared `failed` exception without a
payload:

```text
request_id     01 00 00 00 00 00 00 80
exception_key  ca 68 93 d6 04 b3 3f 58
payload_length 00 00 00 00 00 00 00 00
```

### Failures and Limits

After reading a request header, a receiver that keeps the connection open must
consume or discard exactly `payload_length` bytes before reading another frame
from the same ordered stream. For an unknown procedure, malformed arguments,
authorization rejection, resource rejection, or unexpected implementation
failure, it should throw an appropriate declared exception when practical. It
may close the connection instead. A handler is not invoked when its arguments
are malformed.

A response matching a pending request is malformed if it contains an undeclared
exception key, does not encode the selected return or exception value exactly,
or has trailing bytes. The receiver closes the InterCall connection after such a
response.

A response whose request ID does not correspond to a pending request is ignored.
Its payload is consumed or discarded as opaque bytes and is not validated. A
recipient is not required to detect duplicate responses.

Handling of incomplete frames, local timeouts, cancellation, half-closure, and
transport failure is implementation-defined. Closing a connection is always
permitted; InterCall makes no guarantees after the connection closes.

Implementations may limit frame payload length, string and byte lengths, list
counts, decoded allocation, buffered data, outstanding calls, concurrent
handlers, and connection lifetime. They need not accept every value
representable by a wire `uint64`. Lengths and counts must be checked before
native-size conversion, arithmetic, allocation, or iteration. An implementation
may close the connection rather than consume data that exceeds a safe local
limit.

### Interface Agreement

Procedure and exception keys are derived only from the declaration kind and
name. They are not globally unique and do not identify parameter, return, or
payload types. Peer interfaces may associate the same key with different
declarations or with incompatible contracts for the same name, causing peers to
misinterpret payload bytes.

Implementations should therefore verify that each peer uses the exact interface
expected by the other. One simple approach is for each peer to send the
SHA3-256 digest of the interface file it imported. The receiver computes the
SHA3-256 digest of the interface file it exported and compares the two digests.

The digest input is the exact file bytes and is not canonicalized. Comments,
whitespace, formatting, and line endings therefore affect it. The SHA3-256
digest of a zero-byte interface file is:

```text
a7ffc6f8bf1ed76651c14756a061d662f580ff4de43b49fa82d80a4b80f8434a
```

The exchange format, timing, mismatch handling, and any version agreement are
implementation-defined.

### Security

The InterCall wire format provides no confidentiality, authentication,
authorization, or integrity mechanism. Interface digests and procedure keys are
not credentials or capabilities. Implementations use an appropriate secure
transport and authenticate peers as required by their environment.

Procedure whitelists and other authorization rules are local policy, not wire
data. A peer may reject a call with one of its declared exceptions, such as an
implementation-defined `forbidden_procedure`, or close the connection.
