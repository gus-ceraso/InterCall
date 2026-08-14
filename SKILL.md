---
name: intercall
description: Design, validate, explain, integrate, and troubleshoot language-agnostic InterCall bidirectional RPC interfaces and wire contracts. Use for .intercall files, interface grammar, procedure and exception design, the intercall-validate.lua validator, transports, frames, interoperability, or connecting Go and browser TypeScript peers.
license: CC0-1.0
compatibility: Core guidance is language-agnostic. The validator requires Lua with UTF-8 and integer bitwise support plus LPeg. Go and browser TypeScript commands are optional implementation examples.
---

# InterCall

Use this skill primarily for the language-agnostic InterCall contract. Treat a `.intercall` file—not a native API—as the interoperability authority. Prefer an existing implementation's parser, generator, and runtime over handwritten codecs or dispatch.

## What InterCall is

InterCall is a bidirectional remote procedure call meta-protocol. It defines:

- a portable interface language;
- a compact binary value and frame format; and
- concurrent request/response behavior.

A connection joins two peers. Each peer **exports** the procedures it accepts and the exceptions it may return, and **imports** the opposing peer's interface to call it. Either peer may call the other while handling an incoming call, enabling simultaneous and nested calls.

For peers A and B:

| Peer | Exports | Imports |
| --- | --- | --- |
| A | A's interface | B's interface |
| B | B's interface | A's interface |

A bidirectional application therefore normally has two interface files. A one-way application still has two binding directions, though one may use an implementation-defined empty interface binding.

InterCall models finite, acyclic values. It does not define streaming parameters or results, service discovery, dialing/listening, deployment, retries, reconnects, authentication, authorization, object identity, shared memory, offline delivery, or distributed workflows. A streaming transport carries complete InterCall frames; it does not make procedure values streaming.

## Interface files

An interface is a UTF-8 file, conventionally ending in `.intercall`, containing the complete exported contract of one peer. It is only a sequence of declarations. There is no interface name, namespace, enclosing block, header, version, import, include, constant section, or `throws` clause. An empty or comments-only core interface is valid.

### Example

```intercall
/* Stable identifier for a user. */
type user_id uint64;

type user record {
    id user_id;
    name string;
    aliases list string;
};

exception user_not_found record {
    id user_id;
};

procedure get_user {
    id user_id;
} user;

procedure audit {
    message string;
};
```

Every field and parameter is required. `get_user` has one unnamed return value; `audit` has no return value. `user_not_found` has one unnamed payload value.

## Interface grammar

Comments and whitespace may appear before, between, and after tokens and are omitted from the syntactic productions below.

```ebnf
interface             ::= declaration* EOF ;
declaration           ::= type-declaration
                        | exception-declaration
                        | procedure-declaration ;
type-declaration      ::= "type" IDENT type-specifier ";" ;
exception-declaration ::= "exception" IDENT type-specifier? ";" ;
procedure-declaration ::= "procedure" IDENT "{" parameter* "}"
                          type-specifier? ";" ;
parameter             ::= IDENT type-specifier ";" ;
type-specifier        ::= primitive-type | IDENT | list-type | record-type ;
list-type             ::= "list" type-specifier ;
record-type           ::= "record" "{" field* "}" ;
field                 ::= IDENT type-specifier ";" ;
primitive-type        ::= "int8" | "int16" | "int32" | "int64"
                        | "uint8" | "uint16" | "uint32" | "uint64"
                        | "float32" | "float64" | "string" | "bytes" ;
IDENT                 ::= (ASCII_LETTER | "_")
                          (ASCII_LETTER | DIGIT | "_")* ;
ASCII_LETTER           ::= "a"..."z" | "A"..."Z" ;
DIGIT                  ::= "0"..."9" ;
comment               ::= "/*" COMMENT_TEXT "*/" ;
```

`COMMENT_TEXT` is any sequence of Unicode scalar values not containing `*/`.

### Lexical and validation rules

- Input must be valid UTF-8 and must not begin with a UTF-8 byte-order mark.
- Whitespace is space, tab, carriage return, line feed, form feed, or vertical tab.
- Comments are `/* ... */`, do not nest, and end at the first `*/`. `//` comments are invalid.
- Identifiers are ASCII and case-sensitive. `_name` and `Name2` are valid; `2name` is not.
- Reserved words are `type`, `exception`, `procedure`, `list`, `record`, and every primitive name. Only those exact lowercase spellings are reserved.
- Type, exception, and procedure declarations share one global name scope.
- Each procedure parameter list and each record has its own local name scope.
- Names must be unique in their scope. Local names may equal global names.
- In a type position, an identifier must resolve to an **earlier** type declaration.
- Forward references, self-reference, and recursive named types are invalid, including recursion through lists.
- Every declaration is validated even if nothing references it.
- Nested declarations, unresolved references, unknown syntax, and implementation-specific extensions are invalid core InterCall.

Comments conventionally precede what they document, but core comment association and preservation are implementation-defined. A processor may discard all comments.

## Types

| Type | Values |
| --- | --- |
| `int8`…`int64` | Exact-width signed integers |
| `uint8`…`uint64` | Exact-width unsigned integers |
| `float32`, `float64` | IEEE 754 binary32/binary64, including infinities, signed zero, and NaN |
| `string` | Unicode scalar values; no normalization |
| `bytes` | Ordered raw bytes |
| `list T` | Ordered sequence of zero or more `T` values |
| `record { ... }` | Closed ordered collection of required fields |
| named type | Representation of its declared underlying type |

`list` consumes one complete type specifier, so `list list uint8` is a list of lists. Records may appear anywhere a type is accepted. `record {}` is the zero-width value; a list of zero-width values still encodes its element count.

InterCall has no boolean, enum, optional, map, variant/union, character, pointer, callable, native-width integer, or general constant type, and it performs no implicit coercion. Do not invent syntax for these. Model application conventions explicitly with records, lists, and integer tags, remembering that every declared field is still encoded.

## Procedures and exceptions

A procedure has ordered named parameters and at most one unnamed return value:

```intercall
procedure ping {};
procedure notify { message string; };
procedure add { a int32; b int32; } int32;
```

An exception has a name and at most one unnamed payload:

```intercall
exception unavailable;
exception invalid_input record {
    field string;
    reason string;
};
```

All exceptions in a peer's exported interface are available to all of its procedures; there is no per-procedure exception list. Core InterCall defines no standard exception names or meanings. Every exception the peer can send, including runtime failures, must be declared by that peer.

Every request gets a response unless the connection closes. A successful response uses exception key `0` and carries the procedure's return value. A nonzero exception key selects one declared exception and its payload. Omitted returns and omitted exception payloads use empty payloads. A no-payload exception and an exception carrying `record {}` are wire-width-equivalent but semantically distinct.

The Go and browser TypeScript compatibility profiles reserve and automatically export these no-payload runtime exceptions:

```intercall
exception internal_exception;
exception invalid_arguments;
exception procedure_not_found;
```

These names are profile conventions, not core InterCall declarations. Do not redeclare them in provider source. A handwritten interface intended for those profiles may use them only with the exact no-payload shapes shown.

## Validate interfaces

Use the provided validator rather than approximating the grammar with regular expressions. It requires Lua and LPeg.

```sh
# Validate one or more files.
intercall-validate.lua api/backend.intercall api/browser.intercall

# Validate standard input; '-' may also appear among file operands.
cat api/backend.intercall | intercall-validate.lua -
```

With no operands, the validator reads standard input. It checks UTF-8 and BOM rules, grammar, reserved words, declaration and local scopes, earlier-type references, key zero, and procedure/exception key collisions.

Output and status:

- valid files print `path: ok`;
- diagnostics use `path:line:column: message`;
- exit `0`: every input is valid;
- exit `1`: at least one interface is invalid;
- exit `2`: a file could not be read or LPeg is unavailable.

Run it after handwritten interface changes and before generation or interoperability tests. Language-specific import/export commands should also parse and validate their input or generated interface.

## Basic workflow

1. Identify each peer and assign every procedure to the peer that accepts it.
2. Create one exported interface per peer.
3. Put named types before all references, in dependency order.
4. Choose exact-width values and preserve parameter/field order deliberately.
5. Declare every application and runtime exception the peer may send.
6. Validate each interface with `intercall-validate.lua`.
7. Generate or write an export binding for each local interface and an import binding for the opposing interface.
8. Connect each peer with its local export plus remote import.
9. Verify exact interface agreement before exchanging calls.
10. Test concurrent, nested, exceptional, malformed, and connection-close behavior.

Prefer lower snake case wire names such as `get_user` and `user_id`. The protocol permits any valid identifier, but native generators often project canonical lower snake case most reliably.

## Go ↔ browser TypeScript example

The maintained implementation hints are:

- Go runtime/module: `github.com/cerasos/intercall/go`; generator: `intercall-go`.
- Browser ESM package: `@cerasos/intercall`; generator: `intercall-ts` on Node.js 22+.

These are profiles over the same language-agnostic interface and wire format. Keep each generator and runtime on matching versions. Native mappings and directives are implementation details; consult the installed command's help when doing more than this basic flow.

This example makes a nested bidirectional call:

1. the browser calls Go's `hello(name)`;
2. Go calls the browser's `locale()` while handling `hello`; and
3. Go returns a localized greeting.

### Browser provider

```ts
import type { HandlerContext } from "@cerasos/intercall";

/**
 * Returns the browser locale.
 * @intercall procedure locale
 * @returns The effective locale.
 */
export async function locale(_context: HandlerContext): Promise<string> {
    return navigator.language;
}
```

### Go provider

```go
package hello

import (
    "context"
    "fmt"

    "example.com/app/gen/browserimport"
)

// Hello returns a localized greeting.
// @intercall procedure hello
// @param name The name to greet.
// @return A localized greeting.
func Hello(ctx context.Context, name string) (string, error) {
    locale, err := browserimport.Locale(ctx)
    if err != nil {
        return "", fmt.Errorf("get browser locale: %w", err)
    }
    return fmt.Sprintf("Hello, %s! (%s)", name, locale), nil
}
```

### Generate both directions

Generate the browser export first because the Go provider imports its generated caller; then export Go and import that interface into TypeScript:

```sh
npx intercall-ts export \
    --project frontend/tsconfig.json \
    --out frontend/src/generated/browser \
    --interface api/browser.intercall \
    frontend/src/providers.ts

(cd backend && intercall-go import \
    --out gen/browserimport ../api/browser.intercall)

(cd backend && intercall-go export \
    --out gen/backendexport \
    --interface ../api/backend.intercall \
    ./hello)

npx intercall-ts import \
    --out frontend/src/generated/backend \
    api/backend.intercall

intercall-validate.lua api/browser.intercall api/backend.intercall
```

The generated interfaces conceptually export `locale` from the browser and `hello` from Go; the profiles also insert their three fixed runtime exceptions.

### Connect the peers

Expose the negotiated Go WebSocket handler using the local Go export and remote browser import:

```go
import "github.com/cerasos/intercall/go/transport/websocket"

handler := websocket.NewHandler(
    backendexport.ExportBinding(),
    browserimport.ImportBinding(),
)
mux.Handle("/intercall", authMiddleware(handler))
```

Connect from the browser using the local browser export and remote Go import:

```ts
import { connectWebSocket } from "@cerasos/intercall/browser";
import {
    createClient as createBackendClient,
    importBinding as backendImportBinding,
} from "./generated/backend/binding_gen.js";
import { exportBinding as browserExportBinding } from "./generated/browser/binding_gen.js";

const connection = await connectWebSocket("/intercall", {
    exportBinding: browserExportBinding,
    importBinding: backendImportBinding,
});

const backend = createBackendClient(connection);
console.log(await backend.hello("Ada"));
```

The WebSocket carries binary transport chunks. Message boundaries are not InterCall frame boundaries: one frame may span messages, and one message may contain several frames. Wrap the Go handler with authentication/authorization middleware and use WSS where confidentiality is required.

## Wire-format quick reference

Use this section for interoperability diagnostics or a specifically requested new binding. Otherwise, rely on generated codecs.

### Procedure and exception keys

Keys are unsigned 64-bit FNV-0 values derived from the exact case-sensitive declaration kind and name:

```text
hash = 0
prime = 1099511628211
for byte in ASCII(kind + " " + exact_name):
    hash = ((hash * prime) modulo 2^64) XOR byte
```

`kind` is exactly `procedure` or `exception`; the single space is part of the input. Key `0` is invalid. Procedure and exception keys in one interface must not collide, including across kinds. Types have no keys. Parameters, return/payload types, comments, and unrelated declarations do not affect a key, so a key is not a contract fingerprint.

### Value encoding

All multibyte values are little-endian with no implicit alignment or padding:

| Type | Encoding |
| --- | --- |
| 8/16/32/64-bit integer | Exact width; signed integers use two's complement |
| `float32`, `float64` | IEEE 754 bit pattern |
| `string` | `uint64` UTF-8 byte length, then valid UTF-8 bytes |
| `bytes` | `uint64` byte length, then raw bytes |
| `list T` | `uint64` element count, then consecutive encoded elements |
| record | Fields in declaration order, with no names/count/padding |
| named type | Underlying type encoding |

Encoders emit only canonical quiet NaNs: `0x7fc00000` for `float32` and `0x7ff8000000000000` for `float64`. Decoders reject every other NaN bit pattern. Strings contain Unicode scalar values and must reject invalid UTF-8. A selected value must consume its frame payload exactly; trailing bytes are invalid.

### Frames and calls

A request frame is:

```text
request_id uint64 | procedure_key uint64 | payload_length uint64 | payload
```

A response frame is:

```text
request_id uint64 | exception_key uint64 | payload_length uint64 | payload
```

Each header is exactly 24 bytes; fields begin at offsets 0, 8, and 16. `payload_length` counts only the bytes after the header. A request clears the most-significant `request_id` bit. Its response copies the lower 63 ID bits and sets that bit. Request ID zero is valid, and opposing peers allocate IDs independently.

Request payloads contain parameters consecutively in declaration order. Success uses exception key `0` and the declared return payload. A nonzero key selects a declared exception payload. Omitted and zero-width values require empty payloads.

Multiple requests may be outstanding, responses may arrive out of order, and independent calls have no execution-order guarantee. A peer must continue receiving while its own calls are outstanding. A matched malformed response is terminal. A response with no pending request is consumed and ignored opaquely, allowing late responses after local cancellation. InterCall defines no cancellation frame.

Core InterCall permits implementation safety/resource limits. The current Go and browser TypeScript profiles cap accepted frame payloads at exactly 64 MiB (67,108,864 bytes).

### Transport and interface agreement

A transport must reliably deliver every frame in full and preserve byte order within each frame. On a shared byte stream, frame bytes are consecutive and never interleaved. Transport messages need not correspond one-to-one with frames.

Core InterCall does not mandate interface negotiation. Implementations should verify that each peer uses the exact interface expected by the other because keys do not fingerprint types. The Go/TypeScript profiles use SHA-256 IDs of canonical interface bodies and a client-first exchange:

1. client sends its import ID—the server interface it expects;
2. server compares it with its export ID;
3. server sends its import ID—the client interface it expects; and
4. client compares it with its export ID.

Interface IDs detect mismatched contracts; they are not credentials. Raw connections may skip this exchange and begin with the first InterCall frame only when exact agreement is established out of band.

## Evolution, troubleshooting, and security

InterCall has no built-in versioning or compatibility scheme. Renaming a procedure changes its key. Adding or reordering parameters/fields changes payload interpretation. Adding declarations changes exact profile interface identity. Regenerate both sides after every contract change; use versioned names or separate endpoints when contracts must coexist.

Common profile failures:

- `procedure_not_found`: stale binding or unknown procedure name/key;
- `invalid_arguments`: incompatible interface, malformed values, wrong order, or trailing bytes;
- `internal_exception`: provider failure, undeclared/unmatched application exception, or response encoding failure;
- interface mismatch: the peers generated bindings from different canonical interfaces;
- connection close after a response: undeclared exception key, invalid UTF-8, noncanonical NaN, truncation, wrong zero-width payload, or trailing bytes.

InterCall provides no confidentiality, authentication, authorization, or integrity. Procedure keys and interface IDs are not credentials or capabilities. Use a secure transport, authenticate peers, authorize procedures locally, apply resource limits, and define shutdown and cancellation policy outside the core protocol.

Before finishing, verify both binding directions, earlier type declaration order, scope uniqueness, exact field/parameter order, exception completeness, validator success, interface agreement, binary transport behavior, and external security policy.
