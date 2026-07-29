# InterCall Go Implementation

This document records the working design for the initial Go proof of concept.
It describes the boundary between generated code and the reusable runtime, the
intended Go API, and questions that have deliberately not yet been resolved.
It does not change the language or wire format defined in `../README.md`.

## Scope

The proof of concept consists of:

- a reusable Go runtime for one InterCall connection over a raw byte stream;
- an `intercall-go export` command that discovers tagged Go functions, writes an
  InterCall interface, and generates dispatch code;
- an `intercall-go import` command that reads an InterCall interface and
  generates typed Go callers; and
- generated codecs for InterCall values, arguments, returns, and errors.

The proof of concept prioritizes a small implementation and transparent Go APIs.
It is not intended to be safe for untrusted peers. In particular, it imposes no
resource limits.

## Terminology

A **connection** is one bidirectional InterCall conversation carried by one raw
byte stream.

An **export binding** is a generated Go package for the interface implemented by
the local peer. It contains the dispatcher and internal wrappers for the Go
functions named by that interface.

An **import binding** is a generated Go package for the interface implemented by
the remote peer. It contains typed Go functions that make calls over a
connection found in `context.Context`.

An **implementation function** is a handwritten Go function selected by
`intercall-go export` and called by a generated wrapper.

Each connection has exactly one local exported interface and one remote imported
interface. The two bindings need not share a Go package and do not depend on one
another.

## Settled Design

### Interface and package model

Import and export are independent operations:

- **Export** means allowing the remote peer to call selected local Go functions.
- **Import** means generating Go functions that call the remote peer.

One exported InterCall interface may aggregate implementation functions from
many importable Go packages. For example, one backend interface may contain
functions implemented by `users`, `orders`, `billing`, and `health`, allowing a
frontend to call all of them over one connection.

The export CLI knows the source package and symbol for every selected function.
It generates private, statically typed wrappers that directly call those
symbols. There is no runtime registration, reflection, or public wrapper table.

The import and export packages are separate, so an implementation package may
use an import binding for calls in the opposite direction without creating a
cycle with the export binding:

```text
backendexport -> users   -> frontendimport
              -> orders  -> frontendimport
              -> billing -> frontendimport
```

Every function in the generated exported interface is callable. The proof of
concept has no runtime function whitelist. Documentation tags and CLI overrides
determine the exported function set at generation time, and the generated
interface and dispatcher contain exactly the same set.

### Raw byte-stream boundary

The runtime operates on an established, reliable, ordered, full-duplex byte
stream with the shape of `io.ReadWriteCloser`:

```go
type ByteStream interface {
	io.Reader
	io.Writer
	io.Closer
}
```

The stream contract is stronger than the three embedded interfaces alone:

- one read and one write may proceed concurrently;
- `Close` unblocks pending reads and writes;
- bytes are delivered reliably and in order; and
- the stream is positioned at the first InterCall frame when it is given to the
  runtime.

The runtime performs at most one read and one write at a time.

A small helper may combine distinct readers and writers for standard input,
standard output, and pipe pairs. TCP connections and Unix stream sockets already
have the required basic shape. WebSocket and WebTransport adapters are outside
the proof of concept.

### No roles or handshake

The Go runtime has no initiator or acceptor role. As specified by InterCall, the
high bit of the wire `request_id` distinguishes responses from requests. Both
peers allocate independently from the same 63-bit request ID space.

The proof of concept has no InterCall handshake, authentication, protocol
negotiation, or interface-digest exchange. Transport setup is outside the
runtime. The first bytes read by the runtime are an InterCall frame.

Interface compatibility is assumed. A mismatch may be noticed only when a frame
cannot be decoded according to the locally generated interface.

### No runtime policy or resource limits

The runtime receives no identity, authorization policy, function whitelist, or
limit configuration. Authentication and connection-level admission are outside
the runtime. Once a connection is running, every function in the exported
interface may be called.

The proof of concept intentionally defines no limits for frame payloads, value
sizes, list counts, buffered bytes, pending calls, or concurrent handlers. It
therefore does not defend against memory exhaustion, goroutine exhaustion, or
excessive work. Structural wire validation, checked arithmetic, and native-size
conversion checks are still required for correctness.

### Connection lifecycle and public API

A connection is created from a byte stream without roles, handshakes,
whitelists, or options:

```go
conn := intercall.NewConnection(stream)
```

The generated export binding exposes a blocking `Run` function:

```go
err := backendexport.Run(ctx, conn)
```

`Run`:

- starts and owns the connection's sole receive loop;
- may be called only once for a connection;
- closes the connection when its context is canceled;
- closes the connection before returning;
- returns after transport or terminal protocol failure; and
- wakes pending callers when the connection terminates.

Calling `Run` more than once returns a local Go error.

### Imported function syntax

Generated imported functions are ordinary package functions. They locate the
current connection in `context.Context`:

```go
locale, err := frontendimport.Locale(ctx)
```

`Run` gives every generated handler wrapper a context bound to the current
connection. When an implementation function accepts a context, its wrapper can
pass that context through. This permits the function to call the peer while
processing an incoming call:

```go
func Hello(ctx context.Context, name string) (string, error) {
	locale, err := frontendimport.Locale(ctx)
	if err != nil {
		return "", err
	}
	return greeting(locale, name), nil
}
```

Code that initiates a call outside an incoming handler binds the connection
explicitly:

```go
ctx = intercall.WithConnection(ctx, conn)
locale, err := frontendimport.Locale(ctx)
```

There is no generated client object in the proof of concept. A generated
function called without a bound connection returns a local Go error.

### Concurrent call processing

The connection has one goroutine that reads frames from the shared stream.
Every admitted incoming call runs in its own goroutine. There is no handler pool
or concurrency limit.

The read goroutine:

1. consumes and validates one frame from the stream;
2. distinguishes a request from a response using the high bit of `request_id`;
3. delivers a response to the corresponding pending local call; or
4. starts a goroutine for an incoming request.

Generated handlers are wrappers around implementation functions. A wrapper is
responsible for decoding the arguments, calling the implementation function,
mapping its result or error, encoding a response, and writing that response
through the connection. The buffering and ownership needed to hand a payload
from the read goroutine to a handler remain open design questions.

Every request and response is serialized onto the same stream. All writers share
one connection-wide mutex. The mutex covers the complete 24-byte header and
payload, so bytes from different frames cannot interleave. The exact point at
which a writer acquires the mutex relative to response encoding remains open. A
partial frame write is a terminal connection failure.

Outgoing imported calls use the calling goroutine. They register a pending
request, write the request through the same connection-wide mutex, and wait for
a response delivered by the read goroutine or for local cancellation.

### Runtime errors

InterCall itself defines no canonical errors, but the Go implementation does.
The initial Go runtime wire errors are:

```intercall
error function_not_found;
error invalid_arguments;
error internal_error;
```

These names are reserved by the Go implementation. `intercall-go export`
automatically inserts all Go runtime wire errors into every generated exported
interface. Consequently, every runtime error that the implementation may send
is explicitly present in the interface file, as required by InterCall.

The initial errors have no payloads. Their names establish broad categories,
but the exact local conditions that select each error remain open. More Go
runtime errors are expected to be added later.

Errors that are never transmitted,
such as local context cancellation, missing context binding, transport failure,
and malformed remote responses, are ordinary local Go errors and do not appear
in an InterCall interface.

## Open Questions

The following matters are intentionally unresolved. They should not be inferred
from the illustrative API above.

### Application error declarations and Go behavior

The exporter needs a way to discover application errors and associate native Go
values with InterCall error declarations. Questions include:

- Are no-payload errors declared by tagged sentinel variables, tagged named
  types, or either?
- Are payload errors declared by tagged struct types?
- What is the exact documentation-tag syntax for an error name and payload?
- Since InterCall errors are global to an interface, are errors collected from
  all selected packages independently of individual functions, or must each
  function document the errors it may return?
- Does generated dispatch use `errors.Is` for no-payload errors and `errors.As`
  for payload errors?
- Are wrapped declared errors accepted?
- If more than one declared error matches a returned Go error, which one wins?
- Must an error payload be carried by a pointer, a value, or either?
- How are error comments and payload-field comments obtained?
- How do imported no-payload errors appear in Go: shared sentinels, generated
  zero-sized types, or both?
- How do imported payload errors format their `Error` strings?
- Is every undeclared non-nil error converted to `internal_error`?

A possible, but unapproved, syntax is:

```go
// @intercall error user_not_found
var ErrUserNotFound = errors.New("user not found")

// @intercall error invalid_input
type InvalidInputError struct {
	Field  string
	Reason string
}
```

### Accepted implementation-function signatures

The exact Go signature subset accepted by `intercall-go export` is open:

- Is `context.Context` required as the first parameter, or merely optional?
- If context is optional, does the generated wrapper omit it when calling the
  implementation?
- May `context.Context` appear anywhere except the first parameter?
- Are the supported result forms `(T, error)`, `error`, `T`, and no result?
- Must `error`, when present, be the final result?
- Are multiple non-error results rejected because InterCall has at most one
  return value?
- Are variadic functions rejected or projected to lists?
- Are methods supported? If so, how does generated code obtain a receiver?
- Are generic functions rejected, or may instantiated functions be selected by
  a CLI override?
- Are function-typed parameters and results always rejected?
- Must every selected Go symbol be exported?
- May multiple Go functions project to the same native wrapper name after
  renaming?

The likely minimal subset is non-generic package functions with a first
`context.Context` parameter, zero or more InterCall parameters, at most one data
result, and an optional final `error`, but this has not been approved.

### Go type mapping

Both import and export require an exact native type mapping. Open questions
include:

- Are InterCall integers mapped only to the corresponding exact-width Go types?
- Are Go `int`, `uint`, `uintptr`, and aliases based on them always rejected?
- Is `bytes` always `[]byte`?
- Is `list T` always `[]T`, or may arrays be exported?
- How are nil slices treated when InterCall has no null value?
- Are records mapped only to structs with exported fields?
- How is record field order determined: source order, tags, or an explicit
  annotation?
- Are embedded fields rejected?
- Are struct tags recognized for naming or ignored?
- Are pointers rejected even when they point to otherwise supported values?
- Are named Go types preserved as named InterCall declarations or reduced to
  their underlying type?
- How are Go aliases distinguished from defined types on import and export?
- May one exported Go type refer to a supported type from another package?
- How are anonymous structs and unnamed nested record types represented?
- Are maps, interfaces, channels, functions, complex numbers, booleans, and
  unsafe pointers always rejected?
- How are comments associated with generated type and field declarations?
- What naming conversion maps InterCall identifiers to exported Go identifiers,
  and how are initialisms and Go keywords handled?
- Does an encoder reject a Go string containing invalid UTF-8?
- Do encoders canonicalize every NaN and decoders reject noncanonical NaNs as
  required by InterCall?
- How are zero-width records and lists of zero-width values represented without
  accidental unbounded work?

### Documentation tags and CLI overrides

The documentation-tag language and command-line precedence remain open:

- What exact tag selects a function: `@intercall`, `@intercall function`, or
  another spelling?
- How are wire names overridden in documentation?
- How are parameter, return, type, field, and error comments written?
- Does a CLI `--include` option permit exporting an otherwise untagged function?
- Does `--exclude` always override a source tag?
- Can the CLI rename functions, types, fields, and errors, or only functions?
- Are CLI symbols written as import paths plus identifiers, package-qualified
  names, filesystem paths, or a combination?
- How are methods named on the CLI if methods are supported?
- May the same tagged function be included in several separately generated
  exported interfaces?
- Does the exporter scan only explicitly listed packages or support Go package
  patterns such as `./internal/...`?
- Are generated files ignored automatically on later export runs?
- How are conflicting comments or duplicate declarations reported?
- Are unknown tags errors or ignored for forward compatibility?

Whatever syntax is selected, CLI overrides must affect the emitted interface and
the generated dispatcher identically.

### Source package and output restrictions

A generated export package that directly calls functions from several packages
must be able to import those packages. Questions include:

- Does the proof of concept reject implementation functions in `package main`?
- Must every implementation package be importable from the generated package
  under Go's `internal` visibility rules?
- Must all selected functions and referenced native types be exported?
- Can generated dispatch code instead be placed in a user-selected composition
  package when provider packages cannot be imported from the normal output?
- May source packages come from several modules or workspaces?
- How does the CLI choose the generated Go package name?
- What happens when the output package already contains handwritten Go files?
- How are import aliases chosen when provider package names collide?
- Does export fail on any Go import cycle, or attempt an alternate generated
  layout?

The likely PoC restriction is to accept only exported functions from importable,
non-`main` packages, but this has not been approved.

### Generated code and runtime dependency

The generated artifact model is undecided:

- Does generated code import one shared `github.com/cerasos/intercall` runtime,
  or does each output contain a private copy?
- Which API between generated wrappers and the runtime is public and stable?
- Does an internal handler return an encoded response to the runtime, or call a
  runtime response-writing method directly?
- Are dispatch tables generated as maps, sorted slices, perfect switches, or
  another representation?
- Are codecs generated per declaration, inlined per function, or shared?
- Are generated files intended to be checked in?
- Can import and export generation safely rerun independently in the same output
  directory?
- How are stale generated files detected and removed?
- Does generated code contain a source-interface digest for tooling even though
  the PoC performs no handshake?
- Is the CLI itself restricted to standard-library dependencies, or only the
  runtime?

A shared runtime dependency is the current likely direction, but it remains an
open decision.

### Payload buffering and encoding

Concurrency over one byte stream requires a precise ownership model for frame
payloads. Questions include:

- Does the read goroutine buffer every complete request payload before starting
  its handler, or decode arguments synchronously and pass native values to the
  handler goroutine?
- If decoding occurs in the read goroutine, can expensive decoding delay a
  response needed by an already running handler?
- If raw payloads are buffered, what exact allocation and ownership rules avoid
  data races?
- Are response payloads always encoded into `[]byte` before taking the write
  mutex?
- Alternatively, do generated codecs compute an exact encoded size and stream
  directly while holding the mutex?
- If size and encoding are separate passes, what happens if mutable Go values
  change between passes?
- How does the implementation avoid corrupting the shared stream if encoding
  fails after a header has been written?
- Does a handler return only after its response has been fully written, or after
  it has been handed to another internal component?
- Are request and response payload buffers copied when delivered between
  goroutines, or is ownership transferred without copying?

The simplest PoC likely buffers complete payloads before dispatch and before
writing, but this is especially unsafe without resource limits and has not been
approved.

### Request IDs, cancellation, and pending calls

The symmetric request ID format leaves implementation choices open:

- Does the runtime allocate monotonically increasing 63-bit IDs without reuse?
- If IDs are reused, what data structure identifies IDs made available by a
  received response?
- After local context cancellation, is the pending entry retained as a tombstone
  until its response arrives, or is the ID permanently retired?
- If monotonically allocated IDs are exhausted, what local error is returned?
- Does the runtime track active incoming request IDs and close the connection on
  premature reuse?
- When exactly does an incoming ID cease to be active: after handler return,
  response encoding, or completion of the response write?
- How are cancellation and response-delivery races resolved?
- Is a late response decoded before being ignored, or consumed as opaque bytes
  as the protocol permits for unmatched responses?
- Does local cancellation ever close the whole connection while a request frame
  is being written?
- Are handler contexts canceled only when the connection closes, or can another
  local event cancel them?

InterCall defines no cancellation frame, so local cancellation cannot guarantee
that the remote implementation stops executing.

### Connection startup, context binding, and shutdown

Although `Run` owns the receive loop and connection closure, details remain:

- May an imported function start a call before `Run` has begun reading?
- Does `WithConnection` accept a connection that has not started, has stopped, or
  is already bound to another running export binding?
- How is the one-local-interface/one-remote-interface pairing represented or
  enforced when import and export bindings are independent packages?
- What happens if the same connection is passed to the wrong export binding or
  placed in a context used by the wrong import binding?
- May any number of distinct connections use the same pair of generated
  interfaces concurrently?
- What local error is returned when no connection is present in the context?
- Does a nested `WithConnection` always replace the previous connection?
- Is the context key private to the runtime or separately defined by each import
  package?
- How does `Run` derive handler contexts from its own context?
- Are handler contexts canceled before or after the underlying stream is closed?
- If an implementation function ignores cancellation and never returns, may
  `Run` return while its goroutine remains alive?
- Does `Connection.Close` wait for handlers, or only terminate I/O and pending
  calls?
- Which terminal error is returned when read failure, write failure, and context
  cancellation race?
- Is explicit user closure distinguishable from transport failure?
- Are half-closures recognized or treated as complete connection failure?

### Runtime error conditions and future errors

The initial runtime error names are fixed, but exact condition mapping and future
extension policy need refinement:

- Is every unknown function key answered with `function_not_found` after its
  payload is consumed?
- Which decoding failures produce `invalid_arguments`, and which malformed
  frames close the connection without a response?
- Does trailing request payload data produce `invalid_arguments`?
- Does a handler panic always produce `internal_error`?
- Does failure to encode a handler result produce `internal_error`, or require
  connection closure because no valid response can be constructed?
- Is an undeclared application error always hidden as `internal_error`?
- Should runtime errors ever gain payloads?
- How are new runtime errors introduced without unexpectedly changing every
  generated exported interface?
- Does the importer map recognized Go runtime errors to shared runtime sentinel
  values or generated interface-specific errors?
- Are reserved runtime error names prohibited for user types and functions as
  well as user error declarations because all declarations share one global
  InterCall namespace?

### Local Go errors and diagnostics

Local failures need an idiomatic API even though they are not wire declarations:

- Which errors are exported sentinels suitable for `errors.Is`?
- Is there a structured protocol-error type containing the operation and request
  ID?
- Does a transport error remain discoverable through `errors.Is` or
  `errors.As`?
- Is `context.Canceled` or `context.DeadlineExceeded` returned directly?
- How are concurrent terminal failures combined or prioritized?
- Is there an optional logging or panic-reporting hook, or does the runtime remain
  silent?
- Are panic values and stack traces made available locally without exposing them
  to the peer?

## Deferred Features

The following are explicitly outside the initial proof of concept rather than
open design requirements:

- runtime function whitelists or identity-based authorization;
- protocol or interface handshakes;
- interface digest negotiation;
- configurable resource limits;
- transport dialing, listening, authentication, or encryption;
- WebSocket and WebTransport adapters;
- one bidirectional transport stream per call;
- transport-level cancellation; and
- support guarantees for older Go toolchains.
