# Native Unix Socket and WebSocket Support Plan

## 1. Goal

Add native, production-usable transport support for Unix domain stream sockets and binary WebSockets while preserving the existing transport-independent runtime.

The implementation will provide two layers:

1. **Low level:** establish or accept a transport stream, let the application perform authentication or other transport-specific work, and then explicitly construct a negotiated InterCall connection.
2. **High level:** dial or serve an InterCall connection with safe defaults and very little setup.

The root `intercall` package will remain usable with arbitrary `ByteStream` implementations and will continue to use only the standard library. WebSocket-specific code and its dependency will live under `transport/websocket`. Unix socket code will live under `transport/unixsocket`.

This work does not change InterCall frames, values, calls, handlers, cancellation, or the lifecycle of an already constructed `Connection`.

## 2. Confirmed design decisions

- Support both low-level transport access and high-level connection helpers.
- Put implementations in:
  - `github.com/cerasos/intercall/go/transport/unixsocket`
  - `github.com/cerasos/intercall/go/transport/websocket`
- Support dialing and serving for both transports.
- Unix sockets use filesystem-backed `SOCK_STREAM` sockets. Linux abstract sockets are not part of the initial API.
- WebSockets expose a continuous ordered binary byte stream:
  - an InterCall frame may span multiple WebSocket messages;
  - one WebSocket message may contain multiple InterCall frames;
  - text messages are rejected.
- Use `github.com/coder/websocket`, initially pinned to `v1.8.15` (the current tagged release when this plan was written). Its `NetConn` adapter already implements the required continuous-stream behavior and rejects messages of the wrong type.
- Do not require or negotiate a WebSocket subprotocol.
- High-level connections perform interface agreement before starting the existing InterCall receive loop.
- Keep interface agreement deliberately small: no magic bytes, protocol-version record, capability list, downgrade path, or interface-document exchange.
- Each endpoint sends **only the interface ID it expects the other endpoint to export**—that is, its local import binding's interface ID. It never sends its own export ID as part of the handshake.
- The receiver compares the received ID to its local export binding's interface ID.
- Use the existing SHA-256 artifact digest of the canonical interface body as the interface ID. Do not add an exact-file SHA3-256 identity.
- Existing raw `NewConnection` and the existing metadata-free binding constructors remain supported. Negotiated helpers require metadata-bearing bindings.
- Add matching built-in empty-direction bindings so ordinary one-way clients and servers do not need generated reverse-interface packages.
- High-level Unix listeners:
  - default the socket mode to `0600`;
  - refuse every pre-existing path rather than deleting a presumed stale socket;
  - remove only the socket path that the listener itself created;
  - close the listener and active connections when the serving context is canceled.
- High-level WebSocket servers are plain HTTP servers suitable for a loopback origin behind cloudflared. TLS remains cloudflared's responsibility.
- WebSocket origin checking remains enabled by default. Authentication can be applied as ordinary HTTP middleware around the lower-level handler.
- WebSocket compression is disabled by default.
- The default WebSocket message limit is `64 MiB + 24 bytes` (67,108,888 bytes), enough for one maximum-size InterCall frame. It is configurable in the low-level API.
- High-level transport dialing/upgrading and interface negotiation each select timeout at the earlier of the caller's deadline and a ten-second phase timeout. With no caller deadline, the two sequential phases may therefore use just under twenty seconds before cleanup. Low-level `DialStream` adds no implicit phase timeout: WebSocket `DialStream` uses its context as stream lifetime, while Unix `DialStream` follows `net.Dialer.DialContext` and uses it only for dialing. Native adapter `Close` implementations must complete promptly; as with existing `Connection.Wait`, an arbitrary custom `ByteStream` whose `Close` blocks can delay final return after timeout selection. The original high-level dial context continues to own the resulting connection; canceling it terminates the connection.
- One accepted socket or WebSocket always represents exactly one InterCall connection. There is no reconnect, retry, pooling, multiplexing of several InterCall connections, or session resumption.
- `ListenAndServe` follows `net/http` lifecycle style: it does not translate orderly server shutdown into `nil`. Callers can distinguish the package's server-closed result just as they distinguish `http.ErrServerClosed`.

### 2.1 Validation conventions

- Every public function or method taking a `context.Context` rejects nil with an error wrapping `intercall.ErrInvalidArgument` before dialing, binding, upgrading, or taking stream ownership.
- A nil options pointer selects documented defaults. Option structs are copied before asynchronous use.
- Zero bindings and metadata-free bindings are rejected by negotiated/high-level APIs before transport mutation where possible. Raw stream APIs do not require bindings.
- Empty path, address, or URL arguments are rejected before transport mutation; transport-specific syntax checks then apply.
- Nil `*Listener` receivers return an error wrapping `intercall.ErrInvalidArgument` from methods that return errors; `Addr`, whose `net.Listener` signature has no error result, returns nil. A nil `*Handler` writes HTTP 500 from `ServeHTTP`, while `Shutdown` returns an error wrapping `intercall.ErrInvalidArgument`.
- Nil `http.ResponseWriter` or `*http.Request` arguments to `AcceptStream` are invalid and must fail before calling coder/websocket.
- Argument and already-available context validation precedes transport state inspection. Once a negotiated constructor starts setup, its ownership rules in section 4.4 apply.

## 3. Intended user experience

### 3.1 One-way Unix socket server

```go
err := unixsocket.ListenAndServe(
    ctx,
    "/run/user/1000/hello.sock",
    helloserver.ExportBinding(),
    intercall.EmptyImportBinding(),
)
```

### 3.2 One-way Unix socket client

```go
conn, err := unixsocket.Dial(
    ctx,
    "/run/user/1000/hello.sock",
    intercall.EmptyExportBinding(),
    helloclient.ImportBinding(),
)
```

### 3.3 WebSocket server behind cloudflared

```go
err := websocket.ListenAndServe(
    ctx,
    "127.0.0.1:8080",
    "/intercall",
    helloserver.ExportBinding(),
    intercall.EmptyImportBinding(),
)
```

Cloudflared forwards the public route to `http://127.0.0.1:8080`; the Go process does not load certificates or terminate TLS.

### 3.4 WebSocket client

```go
conn, err := websocket.Dial(
    ctx,
    "wss://hello.example.com/intercall",
    intercall.EmptyExportBinding(),
    helloclient.ImportBinding(),
)
```

### 3.5 Authenticated/custom WebSocket server

```go
intercallHandler := websocket.NewHandler(
    helloserver.ExportBinding(),
    intercall.EmptyImportBinding(),
)

mux.Handle("/intercall", authMiddleware(intercallHandler))
```

The authentication middleware runs before the WebSocket upgrade. Request context values are retained in provider contexts, but request cancellation after HTTP hijacking is not used as the connection lifetime signal.

### 3.6 Fully controlled low-level setup

```go
stream, response, err := websocket.DialStream(ctx, url, options)
// Inspect response or perform any additional application checks.
conn, err := intercall.NewNegotiatedClientConnection(
    ctx, stream, exportBinding, importBinding,
)
```

The server-side counterpart accepts or wraps a stream, authenticates the peer, and calls `NewNegotiatedServerConnection`.

## 4. Root runtime changes

### 4.1 Interface ID type and binding metadata

Add a comparable public type:

```go
type InterfaceID [32]byte
```

Extend the private binding states with:

- an `InterfaceID`;
- a `hasInterfaceID` boolean, kept separate so an explicitly supplied all-zero ID is distinguishable from missing metadata.

Add metadata-aware constructors for generated and advanced manual use:

```go
func NewExportBindingWithInterfaceID(
    Dispatch,
    InterfaceID,
) (ExportBinding, error)

func NewImportBindingWithInterfaceID(InterfaceID) ImportBinding
```

Preserve these constructors unchanged:

```go
func NewExportBinding(Dispatch) (ExportBinding, error)
func NewImportBinding() ImportBinding
```

Bindings produced by the old constructors remain valid for raw `NewConnection`. They fail local validation when passed to a negotiated helper. That failure occurs before dialing, listening-path mutation, WebSocket upgrade, or stream ownership whenever the calling layer can validate that early.

Add read-only methods so transport packages can validate before creating transport state:

```go
func (b ExportBinding) InterfaceID() (InterfaceID, bool)
func (b ImportBinding) InterfaceID() (InterfaceID, bool)
```

These expose protocol metadata, not the process-local binding identity used by `Call`. Because the existing artifact stamp hashes the canonical semantic body, retained documentation changes the ID; formatting and unattached comments that disappear during canonicalization do not.

### 4.2 Built-in empty-direction bindings

Add process-wide singleton accessors:

```go
func EmptyExportBinding() ExportBinding
func EmptyImportBinding() ImportBinding
```

“Empty” means no callable procedures. The represented canonical interface contains the three fixed runtime exceptions in canonical order, matching what `intercall-go export` produces when no application procedures or exceptions are selected:

```text
exception internal_exception;

exception invalid_arguments;

exception procedure_not_found;
```

The implementation will:

- calculate or embed the SHA-256 ID of exactly that canonical body;
- give both singleton bindings the same interface ID;
- have the empty export dispatch return `procedure_not_found` for every fully framed request;
- keep singleton binding identity stable and safe to share across connections;
- test the embedded ID against a fresh SHA-256 calculation so the constant cannot drift from the documented body.

This keeps a one-way server/client setup concise while still negotiating both directions.

### 4.3 Negotiation errors

Add:

```go
var ErrInterfaceMismatch error
```

A received ID that differs from the local export ID wraps this sentinel. Diagnostic text may include lowercase hexadecimal expected and received IDs.

Missing interface metadata is a local invalid-argument error and wraps `ErrInvalidArgument`; it is not a peer mismatch. Transport read/write failures retain and wrap their original errors with role-and-operation prefixes such as `client write interface ID`, `client read interface ID`, `server read interface ID`, and `server write interface ID`.

Do not classify negotiation failures as `ErrProtocol`: negotiation occurs before the InterCall frame protocol starts.

### 4.4 Role-specific negotiated constructors

Add low-level constructors:

```go
func NewNegotiatedClientConnection(
    context.Context,
    ByteStream,
    ExportBinding,
    ImportBinding,
) (*Connection, error)

func NewNegotiatedServerConnection(
    context.Context,
    ByteStream,
    ExportBinding,
    ImportBinding,
) (*Connection, error)
```

The role names affect only the small setup exchange. Once constructed, both return the same symmetric `Connection` type and use the existing runtime unchanged.

Validation and ownership order:

1. Reject nil context, nil stream, zero bindings, and missing interface IDs.
2. Return an already available `ctx.Err()`.
3. Only after local validation, wrap the stream in a private close-once owner and take ownership of that wrapper.
4. Derive a setup context with the earlier of:
   - the caller's existing deadline; or
   - ten seconds from setup start.
5. Perform the role-specific exchange below.
6. On success, call `NewConnection` with the **original** context and the same close-once wrapper, not the temporary setup-timeout context or the original unwrapped stream.
7. On every failure after ownership, close the wrapper and wait for any setup I/O or cancellation callback to exit before returning.

The private wrapper ensures setup cancellation, setup failure, and later `Connection` teardown collectively call the underlying stream's `Close` at most once. The original context owns the complete connection lifetime, while the derived setup context bounds handshake I/O. A caller retains ownership only for local validation or an already available context error; after setup starts, the negotiated constructor owns the stream even when it returns an error. The constructor waits for owned cleanup before returning. Therefore a custom stream whose `Close` itself blocks can delay return after a timeout has selected its error; native Unix and WebSocket adapters must not do so.

### 4.5 Exact handshake

The handshake consists of one 32-byte value in each direction and is sequential:

1. The client sends its local import interface ID—the interface it expects the server to export.
2. The server reads exactly 32 bytes and compares them to its local export interface ID.
3. If they differ, the server closes the stream and returns an error wrapping `ErrInterfaceMismatch`. It sends no interface ID.
4. If they match, the server sends its local import interface ID—the interface it expects the client to export.
5. The client reads exactly 32 bytes and compares them to its local export interface ID.
6. If they differ, the client closes the stream and returns an error wrapping `ErrInterfaceMismatch`.
7. If they match, each side starts its ordinary InterCall connection at the next byte.

Properties:

- A peer never sends its own export ID.
- A peer sends only its expectation of the other peer.
- A passive echo client cannot satisfy the server role because the server reads first. An asymmetric echo server cannot satisfy the client merely by returning the client's expected-server ID. Interface agreement is not authentication, and a deliberately compatible or symmetric peer is not distinguished by this exchange.
- There is no acknowledgment after the server's ID. If the client rejects the server's expectation, the server may briefly construct a connection and then observe the client's close as a transport failure. This is an intentional consequence of keeping negotiation to two IDs without an acknowledgment record.
- There is no magic, version, length, result byte, or extensible envelope.
- Future incompatible negotiation, if ever required, should use a separate API or endpoint rather than silently extending these 32-byte records.

Use full-write and full-read semantics. A short read is a setup transport error. A short successful write is retried. An invalid write count or no-progress write is an error, using the same defensive principles as frame writing but setup-specific diagnostics.

Context cancellation must unblock setup I/O by closing the owned stream, relying on the existing `ByteStream` close-unblocks-I/O contract. Exact context errors win when cancellation is the event that terminates setup; already observed concrete I/O or mismatch errors are retained according to an explicitly tested ordering.

### 4.6 Preserve raw connection behavior

`NewConnection` remains byte-for-byte compatible on the wire:

- no negotiation;
- no interface metadata requirement;
- starts at the first 24-byte InterCall frame header;
- existing applications and custom transports continue to work unchanged.

Do not make `NewConnection` call either negotiated constructor.

## 5. Generator changes

### 5.1 Emit metadata-aware constructors

Both generated directions already have the canonical interface body and its SHA-256 artifact stamp at generation time. Reuse that exact digest.

Change generated import initialization from:

```go
intercall.NewImportBinding()
```

to a deterministic `NewImportBindingWithInterfaceID` call containing the 32 digest bytes.

Change generated export initialization from:

```go
intercall.NewExportBinding(dispatch)
```

to `NewExportBindingWithInterfaceID(dispatch, id)`.

Emit the ID as a deterministic Go composite literal. Do not calculate it at package initialization and do not add the complete interface text to export bindings.

### 5.2 Preserve artifact identity

The generated Go source changes, but the artifact stamp remains the SHA-256 digest of the canonical interface body. Therefore:

- unchanged interfaces retain their current ownership stamp;
- export binding and interface ownership still match;
- regeneration safely replaces the owned Go file even though its source changes;
- runtime interface negotiation and artifact ownership use one existing digest, not parallel digest systems.

### 5.3 Update the synthetic runtime model

Update `internal/tool/checker.go` for every exported root API addition in the same commit that adds it. The current durable parity test requires the synthetic model and actual root package to have identical exported object sets, even for symbols generated code does not call. This includes:

- `InterfaceID` and its binding accessors;
- both metadata-aware constructors;
- empty binding functions;
- `ErrInterfaceMismatch`; and
- both negotiated connection constructors.

Extend the durable parity tests before changing generated fixtures. No commit may leave the real root package and synthetic model out of sync.

### 5.4 Regenerate fixtures

Regenerate all checked-in import and export bindings, then verify:

- canonical interface files and ownership stamps are unchanged;
- only generated Go constructor calls and required literals change;
- regeneration tests remain deterministic;
- generated packages still share stable singleton binding values.

## 6. Unix socket package

### 6.1 Scope and portability

Use standard-library APIs (`net`, `os`, `io/fs`, `path/filepath`, and `context`) and avoid Linux-specific syscalls in the core implementation. Build and test on Linux; keep implementation files usable under Go's `unix` build constraint where the same semantics are available.

Explicitly reject:

- empty paths;
- Linux abstract-socket forms, including leading `@` conventions or embedded NUL bytes;
- non-stream network selection (the package never exposes one).

The package always uses network `"unix"`, which creates `SOCK_STREAM` sockets.

### 6.2 Low-level API

Proposed surface:

```go
type ListenOptions struct {
    // Mode is applied after a successful bind. Zero means 0600.
    Mode fs.FileMode
}

type Listener struct { /* owns net.UnixListener and created path */ }

func DialStream(context.Context, string) (*net.UnixConn, error)
func ListenStream(string, *ListenOptions) (*Listener, error)
func (l *Listener) AcceptStream() (*net.UnixConn, error)
func (l *Listener) Close() error
func (l *Listener) Addr() net.Addr
```

`Listener` also implements `net.Listener`: its standard `Accept() (net.Conn, error)` delegates to `AcceptStream`, while low-level callers that need Unix-specific methods use `AcceptStream` directly.

Low-level authenticated server flow:

1. `ListenStream` safely creates the socket.
2. `AcceptStream` returns `*net.UnixConn` so Linux applications can inspect peer credentials through `SyscallConn` if desired.
3. The application authenticates the peer.
4. It calls `intercall.NewNegotiatedServerConnection`.

Add a convenience accepted-connection method without removing raw access:

```go
func (l *Listener) AcceptConnection(
    context.Context,
    intercall.ExportBinding,
    intercall.ImportBinding,
) (*intercall.Connection, error)
```

`AcceptConnection` validates the context and bindings before accepting. Like `net.Listener.Accept`, however, cancellation that occurs after the accept begins does not interrupt the blocked accept; callers close the listener to unblock it. Once a socket is accepted, the context owns negotiation and the resulting connection. If it became canceled while accept was blocked, the method closes the newly accepted socket and returns the exact context error without starting negotiation.

The implementation should build `ListenAndServe` from `ListenStream`, `AcceptStream`, and the root negotiated constructor rather than maintaining a second socket path.

### 6.3 Safe socket path ownership

`ListenStream` will:

1. Capture one stable ownership path before the first filesystem operation. An absolute supplied path is retained byte for byte. A relative path is anchored once to the current working directory without cleaning it or resolving symlinks; the original spelling may be retained for diagnostics, but every filesystem operation and the bind use the anchored path. A later process-wide `os.Chdir` therefore cannot retarget setup or cleanup.
2. use `Lstat` on that stable path before bind and reject every existing filesystem entry, including a socket, symlink, regular file, directory, or device;
3. bind the stable path without deleting a stale socket;
4. call `SetUnlinkOnClose(false)` immediately after `ListenUnix` succeeds so cleanup is controlled explicitly;
5. immediately `Lstat` the newly bound stable path, require `ModeSocket`, and record its filesystem identity before any later fallible setup step;
6. apply the requested permission bits to the stable path, defaulting to exact `0600`;
7. on identity-read or permission failure, close the listener and remove the stable path only when a recorded identity still matches it;
8. on `Close`, close the listener and remove the stable path only if its current filesystem identity still matches the created socket;
9. never remove a replacement already observable at the identity check. As with the repository's existing trusted-local filesystem model, hostile mutation between the final `Lstat` and `Remove` is outside scope because POSIX path APIs provide no atomic compare-and-unlink operation.

Document that POSIX applies the process umask during bind and the package applies the exact requested mode immediately afterward. Applications requiring protection against even that small bind-to-`chmod` window should place the socket in a private directory.

`Close` is idempotent. A cleanup failure is returned without replacing an earlier serving failure; joining errors is acceptable only where callers retain both classifications through `errors.Is`.

### 6.4 High-level client

```go
func Dial(
    context.Context,
    string,
    intercall.ExportBinding,
    intercall.ImportBinding,
) (*intercall.Connection, error)
```

Steps:

1. Validate path and negotiation-capable bindings before dialing.
2. Derive a dial context with the earlier of the caller's deadline and a ten-second phase timeout.
3. Dial with `net.Dialer.DialContext` and network `unix`, then cancel only the temporary dial context.
4. Wrap the resulting `*net.UnixConn` in a transport close-once owner and pass that same wrapper to `NewNegotiatedClientConnection` with the original context.
5. If the negotiated constructor returns a pre-ownership validation/context error, close the wrapper; otherwise it already owns the wrapper. Calling `Close` on the wrapper is safe in either branch and closes the underlying socket at most once.
6. Return a connection owned by the original context.

### 6.5 High-level server

```go
var ErrServerClosed error

func ListenAndServe(
    context.Context,
    string,
    intercall.ExportBinding,
    intercall.ImportBinding,
) error
```

Behavior:

- Validate all arguments, binding metadata, and an already available context error before creating the socket path.
- Create a default `0600` listener through `ListenStream`.
- Accept sockets until context cancellation or a fatal listener error.
- Start negotiation in a separate goroutine per accepted socket so one peer cannot block further accepts during its ten-second setup window.
- Track sockets during setup and connections after construction.
- Ignore individual connection terminal errors at the serving-loop level, just as `net/http` does not return each handler/connection error from `ListenAndServe`.
- On context cancellation:
  - stop accepting;
  - close setup sockets;
  - call `Close` on active InterCall connections;
  - wait for setup goroutines and connection `Wait` calls to finish;
  - remove the owned socket path;
  - return `ErrServerClosed`, not `nil` or `ctx.Err()`, once serving had started. An already canceled context returns its exact error before listener creation.
- Return a wrapped fatal listener error for unexpected accept failures.
- Never expose reconnect or retry behavior.

No per-connection callback is needed in this high-level API: generated export dispatch already handles incoming procedures. Applications that need to initiate calls from each accepted connection use `ListenStream`/`AcceptStream` and retain the constructed connection themselves.

## 7. WebSocket package

### 7.1 Dependency

Add this direct module dependency:

```text
github.com/coder/websocket v1.8.15
```

Use an import alias such as `coderws` internally to avoid confusion with the local `transport/websocket` package name.

The selected library is appropriate because it:

- has a maintained tagged release;
- supports client and `net/http` server operation;
- has a `NetConn` adapter designed for tunneling byte protocols;
- rejects messages whose type differs from the adapter's configured binary type;
- supports context-controlled I/O;
- defaults compression to disabled;
- has no transitive third-party dependencies at the selected release.

Before merging, verify the pinned checksum in `go.sum` and run the complete race suite against the pinned version. Upstream references used for this decision:

- [`coder/websocket` package documentation](https://pkg.go.dev/github.com/coder/websocket@v1.8.15)
- [`NetConn` implementation and contract](https://github.com/coder/websocket/blob/v1.8.15/netconn.go)
- [`CloseNow` and close-handshake behavior](https://github.com/coder/websocket/blob/v1.8.15/close.go)
- [upstream releases](https://github.com/coder/websocket/releases)

### 7.2 Stream adapter

Create a private stream type that owns:

- `*coderws.Conn`;
- its `net.Conn` view from `coderws.NetConn(ctx, conn, coderws.MessageBinary)`;
- a private lifetime context and cancel function;
- a stopped-on-close `context.AfterFunc` that closes the adapter when the parent context ends; and
- close-once state.

Important library behavior to account for:

- every `net.Conn.Write` becomes one binary WebSocket message;
- `Read` continues across message boundaries, giving InterCall a continuous stream;
- receiving a text message closes with unsupported-data status and returns an error;
- `NetConn` disables the coder library's read limit when constructed.

Therefore, immediately after constructing `NetConn`, restore the configured message limit with `Conn.SetReadLimit`. The default is exactly 67,108,888 bytes. A negative explicit low-level value may select unlimited input; zero selects the default; invalid values are rejected.

Do not rely on canceled `NetConn` read/write contexts alone to release the underlying WebSocket, and do not use `NetConn.Close` as the first close operation because it can perform a multi-second graceful close handshake. Register the parent callback to invoke a private `closeOwned` operation guarded by `sync.Once`; public `Close` first tries to stop that callback and then invokes the same `closeOwned`, so it safely joins a callback that already started without the callback trying to stop itself. `closeOwned` cancels the private `NetConn` context, calls `coderws.Conn.CloseNow`, and then calls `net.Conn.Close` only to stop the `NetConn` timers and contexts after the coder connection is already closed. Suppress the expected later `net.ErrClosed`, retain the first meaningful close error, and cache it for repeated callers. This must unblock both a read and a write and release an otherwise idle WebSocket. Because cleanup is deliberately immediate rather than a graceful close handshake, the peer may observe an abrupt transport close; no API or test may promise a normal WebSocket close status for InterCall teardown.

### 7.3 Low-level client options and dial

Proposed options:

```go
type DialOptions struct {
    HTTPClient   *http.Client
    HTTPHeader   http.Header
    MessageLimit int64
    Compression  bool
}

func DialStream(
    context.Context,
    string,
    *DialOptions,
) (intercall.ByteStream, *http.Response, error)
```

Semantics:

- `HTTPClient` gives callers control over proxying, cookies, custom TLS roots, and transport policy.
- `HTTPHeader` carries authorization and application headers. After cloning it, reject `Connection`, `Upgrade`, and every `Sec-WebSocket-*` header with an error wrapping `intercall.ErrInvalidArgument` before network I/O. These fields belong to the WebSocket handshake implementation rather than the application-header option.
- No InterCall WebSocket subprotocol is offered or required; in particular, `Sec-WebSocket-Protocol` cannot be injected through `HTTPHeader`.
- Compression false maps explicitly to `CompressionDisabled`; `Sec-WebSocket-Extensions` likewise cannot be injected through `HTTPHeader`.
- Compression true uses no-context-takeover mode unless a later measured requirement justifies a broader public enum.
- The passed context bounds both setup and the returned stream lifetime.
- Return the HTTP response supplied by coder/websocket for status and header inspection.

High-level dial:

```go
func Dial(
    context.Context,
    string,
    intercall.ExportBinding,
    intercall.ImportBinding,
) (*intercall.Connection, error)
```

It derives a transport-dial context with the earlier of the caller's deadline and a ten-second phase timeout, uses default `DialOptions`, and then calls `NewNegotiatedClientConnection` with the original lifetime context. The internal dial path must therefore accept separate dial and stream-lifetime contexts: canceling the temporary dial context after a successful upgrade must not cancel the returned stream. `ws://` and `wss://` are both accepted; public TLS uses the normal Go trust store.

### 7.4 Low-level server options and accept

Proposed options:

```go
type AcceptOptions struct {
    OriginPatterns    []string
    InsecureSkipOrigin bool
    MessageLimit      int64
    Compression       bool
}

func AcceptStream(
    context.Context,
    http.ResponseWriter,
    *http.Request,
    *AcceptOptions,
) (intercall.ByteStream, error)
```

Semantics:

- Defaults use coder/websocket's same-origin verification.
- `OriginPatterns` supports explicit additional origins.
- The deliberately prominent insecure option is needed only for applications that understand the CSRF implications.
- Authentication remains ordinary HTTP middleware or pre-upgrade request handling.
- No subprotocol is selected.
- The explicit context owns the accepted stream. Do not implicitly use `r.Context()` after the connection has been hijacked.

The low-level authenticated flow is:

1. Validate request authentication/authorization.
2. Derive a connection context carrying needed request values.
3. Call `AcceptStream`.
4. Call `intercall.NewNegotiatedServerConnection`.
5. Retain the connection if server-initiated calls are required.

### 7.5 Reusable HTTP handler

Add:

```go
type Handler struct { /* bindings, shutdown context, active registry */ }

func NewHandler(
    intercall.ExportBinding,
    intercall.ImportBinding,
) *Handler

func (h *Handler) ServeHTTP(http.ResponseWriter, *http.Request)
func (h *Handler) Shutdown(context.Context) error
```

`ServeHTTP` will:

1. Reject invalid or metadata-free bindings with an HTTP 500 response before upgrade.
2. Accept with binary-only, no-compression, same-origin defaults.
3. Preserve request context values with `context.WithoutCancel(r.Context())` so authenticated identity can reach provider handlers.
4. Add a separate handler-owned cancellation signal rather than relying on post-hijack request cancellation.
5. Track the raw stream before negotiation.
6. Perform server-role interface negotiation.
7. Track the resulting InterCall connection.
8. Block until `Connection.Wait` returns.
9. Remove the connection from the active registry and release all per-request resources.

Errors before upgrade use an appropriate HTTP status. Errors after upgrade close the WebSocket and are not written as HTTP responses. Routine peer disconnects and per-connection protocol errors are not returned by `ServeHTTP` or logged by the library.

`Shutdown` will:

- reject later upgrades;
- cancel registered post-upgrade setup contexts;
- close registered setup streams and active InterCall connections;
- wait for active handler executions, bounded by its context;
- be safe to call more than once.

`coderws.Accept` has no context parameter. A request already inside the pre-upgrade `Accept` call is still owned by the surrounding `http.Server`, not yet by the handler's stream registry. Custom servers must stop/close their HTTP server as well as calling `Handler.Shutdown`; the high-level `ListenAndServe` does both.

This allows authentication middleware to wrap the handler without requiring a separate InterCall callback API.

### 7.6 High-level WebSocket server

```go
func ListenAndServe(
    context.Context,
    string, // address
    string, // path
    intercall.ExportBinding,
    intercall.ImportBinding,
) error
```

Implementation:

1. Validate context, an already available context error, address, path, and metadata-bearing bindings before binding.
2. Treat the path argument as a literal decoded HTTP request path, not an `http.ServeMux` pattern. Require it to begin with `/` and reject query or fragment delimiters; braces and a trailing slash remain ordinary literal path bytes.
3. Create a `Handler` and a private wrapper that compares `r.URL.Path` byte for byte with the configured path. A mismatch receives HTTP 404 without upgrade. Assign that wrapper directly to `http.Server.Handler`, so mux wildcards, subtree matching, cleaning, and redirects cannot broaden or alter the endpoint. Because comparison uses the decoded `URL.Path`, differently escaped request spellings that decode to the same bytes are intentionally equivalent.
4. Create an `http.Server` bound to the supplied address with `ReadHeaderTimeout: 10 * time.Second`. Do not set HTTP `ReadTimeout` or `WriteTimeout` across upgraded WebSockets.
5. Start plain HTTP serving; do not load TLS certificates.
6. On context cancellation, call `http.Server.Close` to interrupt non-hijacked and pre-upgrade HTTP connections, and call `Handler.Shutdown(context.Background())` to close hijacked WebSockets and InterCall connections. Wait for both; do not rely on `http.Server.Shutdown` alone because hijacked connections are not tracked by it.
7. Return `http.ErrServerClosed` for context-driven orderly shutdown after serving starts, matching `http.ListenAndServe` style instead of returning `nil` or `ctx.Err()`. An already canceled context returns its exact error before binding.
8. Return bind and serve failures with their original error identity preserved.

The convenience server intentionally does not add authentication. Applications using Cloudflare Access or application middleware use `NewHandler` with their own `http.Server`.

## 8. Concurrency and lifecycle invariants

The implementation must preserve these invariants:

- Negotiation bytes are fully consumed before the first InterCall frame read.
- The existing receive loop remains the sole InterCall frame reader.
- Transport adapters do not add a second frame parser.
- One connection continues to have one shared write gate.
- WebSocket message boundaries never become InterCall frame boundaries.
- `Connection.Close` remains prompt because stream cleanup stays in the existing asynchronous teardown owner.
- Every adapter `Close` unblocks concurrent `Read` and `Write`.
- Every accepted raw transport has exactly one owner at every point:
  - serving registry during setup;
  - `Connection` after successful construction;
  - immediate cleanup on failure.
- Server cancellation closes listeners, setup streams, and active connections.
- Server shutdown waits for owned setup and connection goroutines but still does not wait for provider handlers that ignore connection-context cancellation, matching existing `Connection.Wait` semantics.
- Dial context cancellation remains the permanent connection context cancellation, not merely a dial timeout.
- No setup goroutine can outlive a failed constructor once the stream's close-unblocks-I/O contract is satisfied.

## 9. Testing plan

### 9.1 Root binding and negotiation tests

Add tests for:

- metadata-aware import/export constructors;
- preservation of process-local handle identity when IDs match;
- missing metadata on legacy constructors;
- raw `NewConnection` accepting legacy bindings unchanged;
- negotiated constructors rejecting legacy bindings before stream ownership;
- binding ID accessors, if added;
- empty binding singleton identity and matching interface IDs;
- empty export returning `procedure_not_found`;
- empty ID matching SHA-256 of the documented canonical body;
- exact 32-byte client record containing only the client's import ID;
- exact 32-byte server record containing only the server's import ID;
- successful asymmetric negotiation;
- mismatch in each direction;
- an asymmetric echo server being rejected and a passive echo client being unable to start the client-first server handshake;
- fragmented reads and short writes;
- no-progress and invalid-count writes;
- EOF before 32 bytes;
- context cancellation and deadline;
- the default ten-second setup-timeout branch through the timeout-parameterized internal helper, without a real ten-second test sleep;
- failure closes the underlying stream exactly once through the close-once wrapper;
- successful negotiation hands ownership to `Connection` exactly once;
- the first post-negotiation byte is treated as the first frame-header byte;
- no goroutine leaks under setup failure races.

### 9.2 Generator tests

Update tests to assert:

- imports call `NewImportBindingWithInterfaceID` exactly once;
- exports call `NewExportBindingWithInterfaceID` exactly once;
- emitted bytes match the artifact stamp;
- semantic formatting changes that preserve the canonical body preserve the ID;
- semantic interface changes alter the ID;
- deterministic output across repeated runs;
- generated source passes the synthetic and real runtime type check;
- checked-in fixtures regenerate exactly.

### 9.3 Unix socket tests

Use real filesystem Unix sockets in `t.TempDir()` on Linux. Cover:

- high-level hello-world call;
- bidirectional calls over one socket;
- multiple concurrent clients;
- default `0600` mode;
- configured mode;
- a relative listener path remaining anchored to its construction-time working directory after `os.Chdir`;
- preservation of `.` and `..` components and symlink traversal semantics when anchoring a relative listener path, proving the implementation does not use lexical cleaning or `EvalSymlinks`;
- refusal of pre-existing regular files, symlinks, directories, and socket files;
- refusal of abstract-socket syntax;
- no automatic stale-socket deletion;
- successful path removal on listener close;
- replacement-path protection using filesystem identity;
- bind, permission, accept, and cleanup failures;
- dial cancellation and the separate ten-second dial-phase bound;
- negotiation timeout without blocking later accepts;
- negotiation mismatch;
- authenticated low-level acceptance retaining `*net.UnixConn` access;
- serving-context cancellation closing listener, setup sockets, and active connections;
- `ListenAndServe` returning `ErrServerClosed` on orderly shutdown;
- no goroutine, descriptor, or socket-path leaks;
- race-detector coverage for concurrent clients and shutdown.

### 9.4 WebSocket adapter tests

Use `httptest.Server` and direct coder/websocket peers. Cover:

- high-level hello-world call over `ws://`;
- a TLS test over `wss://` with a custom low-level `HTTPClient` trust configuration;
- one InterCall frame split across several binary WebSocket messages;
- several InterCall frames in one binary WebSocket message;
- rejection of text messages;
- default message limit and a small configured limit without allocating 64 MiB in routine tests;
- a unit assertion that the default is exactly `64 MiB + 24`, plus scaled small-limit integration tests; routine tests do not allocate a 64 MiB message;
- no subprotocol offered, selected, or required;
- rejection before network I/O of caller-supplied WebSocket handshake headers, including subprotocol and extension headers;
- compression absent by default and explicitly enabled only by option;
- same-origin acceptance and cross-origin rejection;
- authentication headers and middleware;
- preservation of authenticated request context values in provider handlers;
- dial HTTP response visibility;
- client and server negotiation mismatch;
- asymmetric echo-server rejection and passive echo-client rejection;
- malformed/truncated setup records;
- connection close, peer close, context cancellation of an idle stream, and blocked read/write unblocking;
- `Handler.Shutdown` preventing upgrades and closing active WebSockets;
- `ListenAndServe` accepting only the configured literal decoded path, without mux wildcard, subtree, cleaning, or redirect behavior;
- `ListenAndServe` returning `http.ErrServerClosed` on context-driven shutdown;
- concurrent clients and bidirectional calls;
- no goroutine leaks under repeated connection and shutdown races.

### 9.5 Full regression gates

Run from `go/`:

```sh
gofmt -w <changed-go-files>
go test ./...
go test -race ./...
go vet ./...
```

Also run focused repetition for lifecycle-sensitive packages, for example:

```sh
go test -count=20 ./transport/unixsocket ./transport/websocket
go test -race -count=10 ./transport/unixsocket ./transport/websocket
```

Cross-compile the Unix package for selected POSIX targets where the standard-library implementation is expected to compile, while keeping Linux as the required runtime/test platform.

## 10. Documentation updates

### `README.md`

- Keep the general statement that InterCall defines a wire format rather than one mandatory transport.
- Add the Go negotiated-transport profile as one concrete interface-agreement mechanism:
  - canonical-body SHA-256 IDs;
  - each endpoint sends only its imported/expected peer interface ID;
  - exact client/server ordering;
  - no claim that IDs authenticate a peer.
- Do not change InterCall frame encoding.
- Clarify that WebSocket message boundaries are independent of InterCall frames.

### `go/SPEC.md`

- Move Unix sockets and WebSockets out of deferred features.
- Define binding interface metadata, empty bindings, negotiation constructors, setup timeout, and errors.
- Specify transport package behavior and lifecycle ownership.
- Keep TLS, authentication policy, reconnect, transport cancellation, WebTransport, QUIC, and WebTransport-style independent streams deferred.

### `go/GO.md`

- Add complete Unix and WebSocket hello-world server/client examples.
- Add cloudflared guidance using a loopback plain-HTTP WebSocket server.
- Show HTTP authentication middleware with `NewHandler`.
- Show low-level Unix peer authentication flow without prescribing Linux credential policy.
- Explain raw `NewConnection` versus negotiated helpers.
- Document empty-direction bindings.
- Update the out-of-scope section.

### Package documentation

Add `doc.go` files for both transport packages covering:

- high- versus low-level APIs;
- ownership and context behavior;
- security boundaries;
- interface negotiation;
- message/path limits;
- shutdown behavior.

Add executable examples for the four high-level paths and at least one low-level authenticated/custom path.

## 11. Ordered commit plan

Each numbered task below is exactly one commit. Do not combine adjacent tasks and do not leave the tree failing between them. Tests for a behavior belong in the same commit that introduces that behavior unless a later task explicitly adds adversarial or cross-transport coverage.

Every commit must:

1. start from the passing previous commit;
2. include all generated files affected by that commit;
3. run `gofmt` on changed Go files;
4. run the focused tests named in the task;
5. run `go test ./...` from `go/`;
6. pass `git diff --check`;
7. avoid unrelated cleanup or renaming; and
8. update the relevant normative `SPEC.md` surface in the same commit whenever it adds or changes exported API, ownership, errors, or wire/setup behavior, even when `SPEC.md` is not repeated in that task's file list.

Lifecycle-sensitive commits additionally run the focused package under `-race`. The final documentation commits audit and consolidate the incremental specification changes; they must not be the first place implemented public behavior is specified. The final commit runs the complete race and vet gates.

### Task 1 — Add interface metadata to runtime bindings — **Complete**

**Commit:** `runtime: add interface IDs to binding metadata`

**Status:** Implemented and verified in the current working tree.

**Files:**

- `binding.go`
- `binding_test.go`
- `binding_internal_test.go`
- `SPEC.md`
- `internal/tool/checker.go`
- `internal/tool/checker_test.go` only where the existing parity fixture must recognize the new surface

**Implementation:**

1. Declare `InterfaceID` as a named `[32]byte` value. Keep it comparable and free of methods that imply the ID authenticates anything.
2. Add `interfaceID InterfaceID` and `hasInterfaceID bool` to both private binding states. Keep the existing non-zero identity byte.
3. Add `NewExportBindingWithInterfaceID`. Reuse the existing nil-dispatch validation and allocate a fresh identity state containing the supplied ID.
4. Add `NewImportBindingWithInterfaceID`. It allocates a fresh identity state containing the supplied ID.
5. Leave `NewExportBinding` and `NewImportBinding` behavior unchanged except for explicitly leaving `hasInterfaceID` false.
6. Add read-only `InterfaceID() (InterfaceID, bool)` methods to `ExportBinding` and `ImportBinding`. A zero binding returns the zero ID and `false` without panicking.
7. Keep binding equality and `Connection.checkImport` based on the private state pointer, never on the interface ID.
8. Update API comments to distinguish process-local binding identity from negotiated interface identity.
9. In the same commit, add `InterfaceID`, both constructors, and both binding methods to the synthetic runtime model. The all-exported-object parity test makes this inseparable from the root API change.
10. Extend the parity comparator with explicit `*types.Array` support that compares both length and element type; without this, the new named `[32]byte` type cannot compare equal even when modeled correctly.

**Tests:**

- Metadata-aware constructors report the exact supplied ID.
- An explicitly supplied all-zero ID reports `ok == true`.
- Legacy constructors report `ok == false`.
- Zero bindings report `ok == false`.
- Independently constructed bindings with the same ID remain unequal handles.
- Copies retain both handle identity and metadata.
- Nil export dispatch still returns `ErrInvalidArgument` and no binding.
- Existing binding concurrency tests continue to pass.

**Gate:**

```sh
go test . ./internal/tool
go test ./...
```

### Task 2 — Pin interface-aware runtime-model parity — **Complete**

**Commit:** `tool: pin interface-aware runtime model parity`

**Status:** Implemented and verified in the current working tree.

**Files:**

- `internal/tool/checker_test.go`

**Implementation:**

1. Add generated-source checker probes for correct and incorrect uses of both metadata-aware constructors.
2. Add a targeted parity-drift test for `InterfaceID`'s array length.
3. Add targeted parity-drift tests for one constructor signature and one binding accessor method.
4. Keep the production synthetic model unchanged from Task 1; this commit strengthens regression detection rather than repairing an intentionally broken intermediate model.
5. Do not change generator output in this commit.

**Tests:**

- Correct calls to both new constructors type-check in generated source.
- Wrong ID types, missing arguments, and wrong result arity fail checking.
- Array-length, constructor-signature, and method-signature drift are each detected.
- The complete modeled exported bridge remains equal to the actual root package.
- Existing generated files using legacy constructors still check.

**Gate:**

```sh
go test ./internal/tool
go test ./...
```

### Task 3 — Embed canonical interface IDs in generated bindings — **Complete**

**Commit:** `generator: embed canonical interface IDs in bindings`

**Status:** Implemented, fixtures regenerated, and focused generator tests verified in the current working tree.

**Files:**

- `internal/tool/import.go`
- `internal/tool/export_emit.go`
- generator emission/unit tests
- `internal/tool/importfixture/binding_gen.go`
- `internal/tool/exportfixture/binding_gen.go`
- `internal/integration/fixtures/e2eimport/binding_gen.go`
- `internal/integration/fixtures/e2eexport/binding_gen.go`

**Implementation:**

1. Add one deterministic emitter helper that renders an `intercall.InterfaceID{...}` literal from a `[32]byte` SHA-256 sum. Emit fixed-width lowercase hexadecimal bytes in source order with deterministic wrapping.
2. In import generation, compute `sha256.Sum256(canonicalBody)` and initialize the singleton with `NewImportBindingWithInterfaceID`.
3. In export generation, compute the sum over `m.CanonicalBody()` and pass it to `NewExportBindingWithInterfaceID` alongside dispatch.
4. Do not hash the ownership marker, exact input formatting, or generated Go source.
5. Do not decode the artifact stamp string to recover the bytes when the canonical body is already available; calculate once directly from the body.
6. Update source-fragment tests to require exactly one metadata-aware constructor call and no legacy constructor call.
7. Regenerate all four checked-in binding fixtures in the same commit.
8. Byte-compare every owned interface file before and after regeneration and assert its ownership stamp is unchanged.

**Tests:**

- The emitted literal equals `sha256.Sum256(body)` byte for byte.
- Equivalent noncanonical input formatting produces the same ID.
- A semantic interface change produces a different ID.
- Import and export generated from the same canonical body embed the same ID.
- Generator output remains deterministic.
- Generated source passes both synthetic and real package checking.
- Fixture regeneration tests pass without changing `.intercall` files.

**Gate:**

```sh
go test ./internal/tool ./internal/integration
go test ./...
```

### Task 4 — Add built-in empty-direction bindings — **Complete**

**Commit:** `runtime: add empty negotiated binding pair`

**Status:** Implemented, parity model updated, and focused runtime/tool tests verified in the current working tree.

**Files:**

- new `empty.go`
- new `empty_test.go`
- `internal/tool/checker.go`
- `internal/tool/checker_test.go`
- `doc.go` if the root package overview needs a short API mention

**Implementation:**

1. Define the exact canonical no-procedure interface body containing the three fixed exceptions in canonical order.
2. Embed its precomputed SHA-256 as an `InterfaceID` literal. Do not hash at connection setup.
3. Construct one package-level import singleton with `NewImportBindingWithInterfaceID`.
4. Construct one package-level export singleton with `NewExportBindingWithInterfaceID`.
5. Implement the export dispatch so every request returns `procedure_not_found` with an empty payload.
6. Return harmless value copies from `EmptyExportBinding` and `EmptyImportBinding`; repeated calls retain singleton handle identity.
7. Document that “empty” means no procedures, not a zero-byte generated export interface.
8. Add both exported empty-binding functions to the synthetic runtime model in this same commit and pin their signatures in parity tests.

**Tests:**

- Both bindings carry the same ID.
- The ID equals a fresh SHA-256 of the documented canonical body.
- Repeated accessor calls return equal handles.
- Empty import and export handles are nonzero.
- Empty export dispatch returns the fixed key and no payload for arbitrary request keys and payloads.
- A raw connection can use the pair without special runtime branches.

**Gate:**

```sh
go test . ./internal/tool
go test ./...
```

### Task 5 — Implement the exact directional interface handshake — **Complete**

**Commit:** `runtime: negotiate expected peer interface IDs`

**Status:** Implemented and verified with focused handshake, raw-frame-position, runtime-model, and generator tests in the current working tree.

**Files:**

- `errors.go`
- new `negotiate.go`
- new `negotiate_internal_test.go`
- `doc.go`
- `internal/tool/checker.go`
- `internal/tool/checker_test.go`

**Implementation:**

1. Add `ErrInterfaceMismatch` as a comparable root sentinel.
2. Add private validation that checks context, stream, nonzero bindings, and present interface IDs in the same argument-before-state style as `NewConnection`.
3. After validation, wrap the claimed stream in a private close-once `ByteStream`; use that same wrapper for setup I/O and the eventual `NewConnection`. Its first `Close` calls the underlying stream, stores that result, and every later `Close` returns the stored result without calling through again.
4. Add a setup-specific full-write helper with defenses for short writes, impossible counts, and zero progress. Keep its diagnostics distinct from frame-writing diagnostics.
5. Add a full-read helper that reads exactly one 32-byte `InterfaceID`.
6. Implement the client sequence:
   - write only the local import ID;
   - read exactly one server ID;
   - compare it to the local export ID.
7. Implement the server sequence:
   - read exactly one client ID;
   - compare it to the local export ID;
   - only after a match, write only the local import ID.
8. Add `NewNegotiatedClientConnection` and `NewNegotiatedServerConnection`.
9. After a successful exchange, call `NewConnection` with the close-once wrapper at the next unread byte.
10. After ownership begins, close the wrapper on every setup failure. Do not close the original stream for local validation failures.
11. Wrap mismatches with `ErrInterfaceMismatch` and preserve underlying setup I/O errors.
12. Update the root package overview: raw `NewConnection` does not negotiate, while the two new constructors perform only interface agreement and still do not dial or listen.
13. Add the sentinel and both exported constructors to the synthetic runtime model in this same commit; extend parity tests for their signatures.
14. Do not add magic, versions, lengths, status bytes, acknowledgments, or export-ID transmission.

**Tests:**

- Capture and assert the exact 32 bytes written by each role.
- Prove each side writes its import ID and never its export ID.
- Complete a valid asymmetric exchange over `net.Pipe` with IDs `A→B` and `B→A`.
- Reject the client expectation at the server before the server writes anything.
- Reject the server expectation at the client.
- Wrap `ErrInterfaceMismatch` through `errors.Is`.
- Handle one-byte reads and writes.
- Handle impossible write counts and no-progress writes.
- Treat EOF at byte 0 and bytes 1–31 as setup transport failures.
- Begin frame parsing at byte 33 in each direction, proving no handshake byte leaks into the runtime.
- Preserve raw `NewConnection` behavior and first-frame position.

**Gate:**

```sh
go test . ./internal/tool
go test ./...
```

### Task 6 — Make negotiated setup cancellation-safe and leak-free — **Complete**

**Commit:** `runtime: harden negotiated connection setup lifecycle`

**Status:** Implemented and verified with focused cancellation/timeout tests and repeated race-detector runs in the current working tree.

**Files:**

- `negotiate.go`
- `negotiate_internal_test.go`
- `liveness_internal_test.go` where existing helpers are reusable

**Implementation:**

1. Derive an internal setup context with the earlier of the caller deadline and the ten-second default.
2. Keep the original context for the eventual `NewConnection`; never pass the temporary setup deadline into the connection lifetime.
3. Add a private setup-outcome arbiter with a mutex, selected error, and handed-off flag. Cancellation, mismatch, and concrete I/O failure all attempt selection under this one lock; the first selected cause is permanent.
4. Register one `context.AfterFunc` only after stream ownership. It selects the setup-context error through the arbiter and closes the close-once wrapper when setup context ends. On every return, stop it; if stopping loses a race, wait for the callback to finish before deciding success or returning.
5. Define and implement error ordering for races:
   - local validation precedes ownership;
   - an already canceled context precedes setup I/O;
   - mismatch and concrete I/O failures attempt selection immediately when observed;
   - cancellation that selects first and unblocks pending I/O returns the exact `ctx.Err()` or setup deadline error;
   - final successful handoff is one lock-protected transition that can occur only while no error is selected.
6. Ensure the cancellation callback and any I/O helper goroutine exit before the constructor returns.
7. Ensure failed setup closes the underlying stream exactly once even when cancellation and mismatch race.
8. Ensure successful setup transfers the same close-once wrapper to `Connection` and stops setup cancellation machinery before handoff. If the original context becomes canceled between callback stop and `NewConnection`, close the wrapper after `NewConnection` returns its pre-ownership context error.
9. Add an unexported timeout-parameterized construction helper so tests use millisecond deadlines without mutating a package global or sleeping ten seconds.
10. Test the documented no-ack consequence: if the client rejects the server's expected-client ID, the server may construct and then terminates from the client close.

**Tests:**

- Already canceled context does not claim the stream.
- Cancellation while client write, client read, server read, and server write are blocked returns promptly.
- Default-timeout path is exercised through the short internal test timeout.
- A caller deadline earlier than the default wins exactly; `WithCancelCause` still returns `ctx.Err()` rather than `context.Cause`.
- Original context remains active after the temporary setup deadline is canceled on successful construction.
- Canceling the original context after construction terminates the connection.
- Stream close count is exactly one on every failed setup path.
- No setup goroutine remains after 100 repeated cancellation races with conforming prompt-close streams.
- A deliberately blocking custom `Close` delays constructor return until released but does not change the already selected timeout/cancellation cause; document this parity with cleanup waiting in `Connection.Wait`.
- An asymmetric echo server fails when local export and import IDs differ.
- A passive echo client cannot pass the server handshake because the server waits for the client's expectation first.

**Gate:**

```sh
go test .
go test -race .
go test ./...
```

### Task 7 — Add Unix path validation and low-level dialing — **Complete**

**Commit:** `unixsocket: add filesystem stream dialing`

**Status:** Implemented and verified with focused Unix socket and race-detector tests in the current working tree.

**Files:**

- new `transport/unixsocket/doc.go`
- new `transport/unixsocket/path_unix.go`
- new `transport/unixsocket/dial_unix.go`
- new Unix-focused test files

**Implementation:**

1. Create the package under a Go `unix` build constraint while treating Linux as the required platform.
2. Add one path validator shared by dial and listen.
3. Reject empty paths, embedded NUL bytes, and Linux `@name` abstract-socket notation.
4. Do not resolve symlinks or create parent directories. `DialStream` passes the supplied path to `net.Dialer` unchanged. `ListenStream`, added in Task 8, instead anchors a relative path once so later working-directory changes cannot retarget listener setup or cleanup.
5. Implement `DialStream` with `net.Dialer.DialContext(ctx, "unix", path)`.
6. Assert and return `*net.UnixConn`; if the standard library unexpectedly returns another type, close it and return an internal transport error.
7. Preserve `net.OpError`, context, and filesystem error identities through wrapping.
8. Keep this commit independent of interface negotiation.

**Tests:**

- Reject nil context and every invalid path before dialing.
- Dial a real temporary `net.UnixListener` and exchange bytes in both directions.
- Return exact cancellation/deadline classifications.
- Preserve connection-refused and missing-parent errors through `errors.Is`/`errors.As`.
- Confirm the returned value exposes `SyscallConn` for later peer-credential use.

**Gate:**

```sh
go test ./transport/unixsocket
go test ./...
```

### Task 8 — Add safe Unix listener path ownership — **Complete**

**Commit:** `unixsocket: own listener paths safely`

**Status:** Implemented and verified with focused Unix listener and race-detector tests in the current working tree.

**Files:**

- new `transport/unixsocket/listen_unix.go`
- Unix listener tests

**Implementation:**

1. Add `ListenOptions{Mode fs.FileMode}` with zero selecting `0600`.
2. Reject file-type bits and permission bits outside the supported POSIX permission mask.
3. Add `ListenStream(path, options)` and an owning `Listener` wrapper.
4. Before any filesystem operation, retain an absolute path unchanged or anchor a relative path to one `os.Getwd` result without lexical cleaning or symlink resolution. Store and use only this stable ownership path for bind, mode changes, identity checks, and cleanup.
5. `Lstat` the stable path before bind and reject every existing leaf, including sockets and symlinks. Continue only on `fs.ErrNotExist`.
6. Bind with `net.ListenUnix("unix", &net.UnixAddr{Name: stablePath, Net: "unix"})`.
7. Immediately disable automatic unlink-on-close and make the wrapper the only path cleanup owner.
8. Immediately `Lstat` the bound stable path, require `ModeSocket`, and record its `FileInfo` before `Chmod` or another fallible setup step. If no trustworthy identity can be recorded, close without unlinking an unverified leaf.
9. Apply the exact selected mode with `os.Chmod` to the stable path.
10. On setup failure after identity capture, close the listener and remove only a stable path that still matches the recorded socket.
11. Add `AcceptStream() (*net.UnixConn, error)`, standard `Accept() (net.Conn, error)` delegation, `Addr`, and idempotent `Close`. `Addr` returns nil on a nil receiver; methods with error results wrap `intercall.ErrInvalidArgument` for a nil receiver.
12. In `Close`, close the listener first, then `Lstat` and unlink only if the stable path still identifies the created socket.
13. Serialize close state, store the first close/cleanup result for repeated callers, and never hold the state lock across `Accept`, listener close, filesystem I/O, or unlink.
14. Document that post-bind identity capture protects ordinary replacement cases in the trusted-local model; it is not a defense against a hostile process replacing entries in either the bind-to-`Lstat` or final-`Lstat`-to-`Remove` interval.

**Tests:**

- Default and configured permissions.
- Refusal of existing regular file, directory, symlink, FIFO, and Unix socket.
- A stale Unix socket is never deleted automatically.
- Listener close unblocks `AcceptStream`.
- Nil receiver methods with error results return `ErrInvalidArgument`, while `Addr` returns nil; first and repeated `Close` calls on a real listener are safe.
- Normal close removes the owned path even if the process changes its working directory after `ListenStream` returns; the listener address and cleanup path remain anchored to the directory selected at construction.
- Replacing or deleting the path before close never deletes the replacement.
- Chmod/setup failure cleans up only the created socket.
- Accepted connections carry bytes reliably in both directions.
- `Listener` reports the underlying Unix address and satisfies `net.Listener` at compile time.

**Gate:**

```sh
go test ./transport/unixsocket
go test -race ./transport/unixsocket
go test ./...
```

### Task 9 — Add negotiated Unix dial and accept helpers — **Complete**

**Commit:** `unixsocket: construct negotiated connections`

**Status:** Implemented and verified with negotiated Unix connection and race-detector tests in the current working tree.

**Files:**

- new `transport/unixsocket/connection_unix.go`
- Unix connection tests

**Implementation:**

1. Add high-level `Dial(ctx, path, export, import)`.
2. Validate path, context state, and binding interface metadata before opening a socket.
3. Derive a dial context bounded by the earlier caller deadline or ten seconds and use it only for `DialStream`; canceling it after success must not own the `net.UnixConn`. Route the public function through an unexported timeout- and dial-function-parameterized helper so tests exercise blocked dialing and the default branch without relying on Unix backlog behavior.
4. Add a private close-once stream wrapper and pass that wrapper, not a separately closable raw socket, to `NewNegotiatedClientConnection`.
5. Add `Listener.AcceptConnection(ctx, export, import)` as a convenience over `AcceptStream` and `NewNegotiatedServerConnection`, also using the close-once wrapper. Validate the context, its already available error, and binding metadata before entering `AcceptStream`.
6. Document that `AcceptConnection` serially accepts one socket and performs negotiation; callers needing concurrent setup should accept raw streams and launch goroutines themselves. Cancellation after `AcceptStream` begins does not unblock that accept. Closing the listener does; if accept instead succeeds after cancellation, close the accepted wrapper and return the exact context error before negotiation.
7. If a negotiated constructor returns before taking ownership, close through the same wrapper. If it took ownership, another wrapper `Close` is harmless and does not close the underlying descriptor twice.
8. Return the root mismatch sentinel unchanged through wrapping.

**Tests:**

- End-to-end call over a real socket using metadata-aware bindings.
- Empty reverse bindings support a one-way call.
- A server can initiate a reverse-direction call when both real bindings are supplied.
- Client and server interface mismatches fail.
- Legacy bindings fail before client dial and before `AcceptConnection` blocks in accept.
- Cancellation that becomes available while `AcceptConnection` is blocked does not return until the listener is closed or a socket is accepted; an accepted socket is then closed before negotiation and the exact context error is returned.
- Dial context cancellation owns and terminates the returned connection.
- Explicit connection close removes no listener path and leaves the listener accepting later clients.

**Gate:**

```sh
go test ./transport/unixsocket
go test -race ./transport/unixsocket
go test ./...
```

### Task 10 — Add Unix `ListenAndServe`

**Commit:** `unixsocket: serve negotiated connections`

**Files:**

- new `transport/unixsocket/server_unix.go`
- Unix server tests

**Implementation:**

1. Add package sentinel `ErrServerClosed` and `ListenAndServe(ctx, path, export, import)`.
2. Validate context, path, and metadata-bearing bindings before creating the listener.
3. Use `ListenStream` with default options; do not duplicate bind or cleanup logic.
4. Derive one internal serving context from the caller context. Pass it to every negotiated constructor and cancel it on every server exit path, including fatal accept failure.
5. Start one accept loop owned by the calling goroutine.
6. Immediately wrap every accepted socket in the Task 9 close-once stream, register that wrapper, and launch negotiation in its own goroutine.
7. Maintain a mutex-protected registry with explicit ownership transfer from setup wrapper state to constructed `Connection`.
8. Never close the raw `*net.UnixConn` separately from its wrapper, and never hold the registry lock while canceling the serving context, closing a wrapper, calling `Connection.Close`, or waiting.
9. For each successful connection, launch/retain one waiter that calls `Wait` and removes it from the registry.
10. Ignore individual setup and connection terminal errors at the server return boundary.
11. On context cancellation or fatal serve exit, close the listener, cancel the internal serving context, close all setup wrappers, close all active connections, and wait for server-owned setup/waiter goroutines. Shared close-once wrappers prevent context teardown and registry cleanup from closing one descriptor twice.
12. If the caller context is already canceled before listener creation, return its exact error without creating a path. After serving starts, context-driven shutdown returns `ErrServerClosed`. Return a wrapped accept/listener error for an unexpected serving failure after completing the same owned-resource cleanup.
13. Let `Listener.Close` perform identity-safe path cleanup.

**Tests:**

- Serve one and many sequential clients.
- Serve many concurrent clients.
- A client stalled in negotiation does not block later clients.
- Canceling the server closes stalled setup sockets and active connections.
- The server waits for its setup and connection waiter goroutines.
- Provider handlers that ignore cancellation do not prevent return, matching root `Wait` semantics.
- Context-driven shutdown returns `ErrServerClosed`, not nil or `ctx.Err()`.
- Fatal accept errors retain their identity.
- The socket path is removed after return.
- Replaced paths remain untouched.

**Gate:**

```sh
go test ./transport/unixsocket
go test -race ./transport/unixsocket
go test ./...
```

### Task 11 — Harden Unix serving under repeated races

**Commit:** `unixsocket: cover shutdown and ownership races`

**Files:**

- Unix package test files
- Unix implementation files only for fixes demonstrated by the new tests

**Implementation:**

1. Add deterministic barriers around accept, setup registration, negotiation completion, registry transfer, waiter removal, and shutdown snapshotting.
2. Test cancellation at every ownership-transfer boundary.
3. Add descriptor-count checks in a `_linux_test.go` file where they can be made stable without assuming a global pristine process; keep portable Unix tests free of `/proc` assumptions.
4. Add repeated socket creation/removal tests in one directory.
5. Add a compile-only portability gate for selected POSIX `GOOS` targets supported by the implementation.
6. Make only the synchronization fixes required by these tests; do not add new public API.

**Tests:**

- No double close when cancellation races negotiation success.
- No untracked connection when shutdown races registry transfer.
- No accepted socket survives a failed negotiation.
- No path or descriptor leak over repeated start/stop cycles.
- Concurrent `Listener.Close`, `AcceptStream`, and server cancellation are race-free.
- `go test -race -count=20 ./transport/unixsocket` passes.

**Gate:**

```sh
go test -count=20 ./transport/unixsocket
go test -race -count=20 ./transport/unixsocket
tmp=$(mktemp -d)
GOOS=darwin GOARCH=amd64 go test -c -o "$tmp/unixsocket-darwin.test" ./transport/unixsocket
GOOS=freebsd GOARCH=amd64 go test -c -o "$tmp/unixsocket-freebsd.test" ./transport/unixsocket
rm -rf "$tmp"
go test ./...
```

### Task 12 — Add the WebSocket dependency and binary stream adapter

**Commit:** `websocket: add binary byte-stream adapter`

**Files:**

- `go.mod`
- `go.sum`
- new `transport/websocket/doc.go`
- new `transport/websocket/stream.go`
- stream adapter tests

**Implementation:**

1. Add direct dependency `github.com/coder/websocket v1.8.15`, run `go mod tidy`, and verify the resulting `go.mod`/`go.sum` diff adds no unexpected dependency.
2. Define `const DefaultMessageLimit int64 = 64*1024*1024 + 24`.
3. Implement a private adapter around `*coderws.Conn` and `coderws.NetConn` configured for `MessageBinary`.
4. Derive a private cancellable lifetime context for `NetConn` so adapter `Close` can stop reads and writes independently of parent cancellation.
5. Define message-limit normalization: zero means default, positive means exact limit, `-1` means unlimited for low-level callers, and other negative values are invalid.
6. Call `SetReadLimit` **after** constructing `NetConn`, because `NetConn` resets it to unlimited.
7. Only after full adapter construction and read-limit application, register a `context.AfterFunc` on the parent lifetime context. The callback calls the private `closeOwned` operation; public `Close` stops the callback when possible and then enters the same `sync.Once`, which joins an already-running callback without self-deadlock.
8. Implement the fixed close sequence from section 7.2: public `Close` stops the parent callback, both paths share `closeOwned`, and `closeOwned` cancels adapter I/O, calls `CloseNow`, then calls `NetConn.Close` to stop its timers while suppressing the expected already-closed result.
9. Preserve close and I/O error identity where coder/websocket exposes it.
10. Keep the adapter private; the public boundary remains `intercall.ByteStream`.

**Tests:**

- Compile-time `ByteStream` conformance.
- Binary bytes cross in both directions.
- Parent context cancellation closes an idle WebSocket and unblocks active reads and writes.
- Adapter close unblocks simultaneous read and write.
- Repeated close is safe.
- Default, explicit, and unlimited message limits are applied after `NetConn` creation.
- Invalid limits are rejected without leaking the coder connection.
- Normal WebSocket close maps to EOF through `NetConn` as expected by the root runtime.

**Gate:**

```sh
go test ./transport/websocket
go test -race ./transport/websocket
go test ./...
```

### Task 13 — Add low-level WebSocket dial and accept

**Commit:** `websocket: expose configurable stream dial and accept`

**Files:**

- new `transport/websocket/options.go`
- new `transport/websocket/dial.go`
- new `transport/websocket/accept.go`
- low-level WebSocket tests

**Implementation:**

1. Add local `DialOptions` with `HTTPClient`, `HTTPHeader`, `MessageLimit`, and `Compression`.
2. Add local `AcceptOptions` with `OriginPatterns`, `InsecureSkipOrigin`, `MessageLimit`, and `Compression`.
3. Copy each option struct and clone caller-owned `HTTPHeader` maps and `OriginPatterns` slices before validation or use so later mutation cannot race active setup.
4. Reject `Connection`, `Upgrade`, and names with the case-insensitive prefix `Sec-WebSocket-` in the cloned dial headers before network I/O. Return an error wrapping `intercall.ErrInvalidArgument`; do not silently overwrite or remove caller input.
5. Translate options to coder/websocket without exposing coder option types in the public API.
6. Map compression false explicitly to disabled and true to no-context-takeover.
7. Do not populate subprotocol options, and prevent `Sec-WebSocket-Protocol` or `Sec-WebSocket-Extensions` from reaching coder/websocket through `HTTPHeader`.
8. Implement an unexported dial core that accepts separate HTTP-dial and returned-stream lifetime contexts.
9. Implement public `DialStream(ctx, url, options)` by passing its one context as both contexts, returning `ByteStream`, `*http.Response`, and error.
10. Validate URL scheme and message options before network I/O. Accept `ws` and `wss` only.
11. Preserve the coder HTTP response on failed upgrades when available.
12. Implement `AcceptStream(ctx, w, r, options)` using the explicit lifetime context rather than `r.Context()`.
13. Keep coder's safe same-origin verification by default; map explicit origin patterns and the deliberately named insecure override.

**Tests:**

- Non-reserved custom request headers reach the HTTP server.
- Every reserved handshake header, including `Sec-WebSocket-Protocol` and `Sec-WebSocket-Extensions`, fails before an HTTP request is sent and wraps `ErrInvalidArgument`.
- HTTP response/status is returned on rejected upgrade.
- A custom `HTTPClient` supports an `httptest` TLS server.
- Same-origin requests pass by default.
- Cross-origin requests fail by default and pass only with configured policy.
- No WebSocket subprotocol is offered or selected.
- Compression is absent by default and negotiated only when enabled on both sides.
- Caller mutation of option headers/origin slices after invocation does not race use.
- Nil contexts, nil HTTP arguments, invalid URL schemes, and invalid limits fail before dialing/upgrading.
- Explicit accept context cancellation owns and closes the resulting stream.
- The internal dial core can cancel its HTTP-dial context after success without canceling a stream owned by a distinct lifetime context.

**Gate:**

```sh
go test ./transport/websocket
go test -race ./transport/websocket
go test ./...
```

### Task 14 — Prove WebSocket message boundaries are transparent

**Commit:** `websocket: enforce InterCall stream semantics`

**Files:**

- WebSocket stream/interoperability tests
- adapter implementation only for fixes required by tests

**Implementation:**

1. Build a direct coder/websocket peer test harness that can choose message type and message contents independently of InterCall writes.
2. Feed one valid InterCall frame across multiple binary messages and verify one complete frame is read.
3. Put multiple complete InterCall frames into one binary message and verify they are read consecutively.
4. Split the 24-byte header itself across messages.
5. Split the payload across messages.
6. Send a text message between binary data and require terminal transport failure with coder's unsupported-data status.
7. Exercise a small configured message limit and verify the connection closes before unbounded buffering.
8. Verify two small frames in one message are accepted when their combined message is below the configured limit.
9. Do not parse or reframe InterCall data in the adapter.

**Tests:**

- All boundary combinations above.
- No `(0, nil)` read results escape the adapter.
- Message-limit errors remain transport errors rather than `ErrProtocol`.
- Root frame payload limit still independently rejects an oversized declared InterCall payload.

**Gate:**

```sh
go test ./transport/websocket
go test -race ./transport/websocket
go test ./...
```

### Task 15 — Add high-level negotiated WebSocket dialing

**Commit:** `websocket: dial negotiated InterCall connections`

**Files:**

- new `transport/websocket/connection.go`
- high-level dial tests

**Implementation:**

1. Add `Dial(ctx, url, export, import)`.
2. Validate context state and binding metadata before HTTP dialing.
3. Derive an HTTP-dial context bounded by the earlier caller deadline or ten seconds. Route the public function through an unexported timeout-parameterized helper so tests exercise the default branch with a short duration.
4. Call Task 13's internal dial core with the temporary HTTP context and the original context as the stream lifetime; canceling the temporary context after upgrade must not close the stream.
5. Pass the close-once WebSocket adapter to `NewNegotiatedClientConnection` with the original context, which applies its own ten-second negotiation phase bound.
6. On any returned error, close through the adapter. Its close-once implementation prevents a second underlying close if the negotiated constructor had already taken ownership.
7. Return HTTP upgrade errors with response context preserved by the low-level dial core.
8. Ensure the original dial context continues to own the returned InterCall connection.

**Tests:**

- Complete one-way hello call against a low-level server.
- Complete a bidirectional call with two real interface IDs.
- Empty reverse bindings work.
- Interface mismatch returns `ErrInterfaceMismatch` where the client can observe it.
- Legacy binding metadata and an already canceled context fail before an HTTP request is sent.
- A stalled HTTP upgrade hits the short test form of the ten-second dial-phase timeout; a stalled interface exchange independently hits the negotiation timeout.
- Canceling the dial context after return terminates `Connection.Wait` with the exact context error.
- No subprotocol is needed for successful dial.

**Gate:**

```sh
go test ./transport/websocket
go test -race ./transport/websocket
go test ./...
```

### Task 16 — Add the reusable InterCall WebSocket handler

**Commit:** `websocket: serve connections through an HTTP handler`

**Files:**

- new `transport/websocket/handler.go`
- handler tests

**Implementation:**

1. Add `Handler` and `NewHandler(export, import) *Handler`.
2. Cache local binding-validation failure in the handler so invalid bindings produce HTTP 500 before upgrade.
3. In `ServeHTTP`, preserve request values through `context.WithoutCancel(r.Context())`.
4. Derive a separate cancellable connection context from that value-preserving base and attach handler-wide shutdown with `context.AfterFunc`; stop/join the callback when `ServeHTTP` exits. Do not use post-hijack request cancellation as the lifetime signal.
5. Accept a default binary WebSocket stream with same-origin checks, compression disabled, and default message limit.
6. Call `NewNegotiatedServerConnection`, which performs server-role negotiation and constructs the root connection.
7. Block in `Connection.Wait` until terminal.
8. Do not log routine setup, mismatch, protocol, or peer-close errors.
9. Before upgrade, return ordinary HTTP errors. After upgrade, close the stream and return without attempting an HTTP response.
10. Ensure provider handler contexts retain authentication values installed by middleware.

**Tests:**

- Middleware-injected identity reaches the generated/provider dispatch context.
- Invalid bindings return 500 without an upgrade.
- Successful negotiation serves a call and blocks the HTTP handler for the connection lifetime.
- Setup mismatch closes after upgrade without writing a second HTTP response.
- Same-origin and binary-only defaults are active.
- Request cancellation caused by hijacking does not immediately kill the InterCall connection.
- Peer close lets `ServeHTTP` return and releases all per-request state.

**Gate:**

```sh
go test ./transport/websocket
go test -race ./transport/websocket
go test ./...
```

### Task 17 — Add handler shutdown and active connection ownership

**Commit:** `websocket: shut down active handler connections`

**Files:**

- `transport/websocket/handler.go`
- handler lifecycle tests

**Implementation:**

1. Add `Handler.Shutdown(ctx) error` with standard idempotent shutdown semantics.
2. Add an admission lock/closed flag that orders new `ServeHTTP` registrations against shutdown; requests admitted after shutdown begins receive HTTP 503 before upgrade.
3. Register the raw stream before negotiation begins.
4. Atomically transfer registry ownership from setup stream to `Connection` after construction.
5. Track active `ServeHTTP` executions with a wait group whose `Add` cannot race a completed `Wait`.
6. On shutdown, reject new upgrades, cancel registered post-upgrade connection/setup contexts, close registered setup streams, and close active InterCall connections. Pre-upgrade `coderws.Accept` remains the surrounding HTTP server's responsibility.
7. Wait for active handler executions up to the shutdown context deadline.
8. Return the exact shutdown context error if it expires; otherwise return nil.
9. Never hold the registry lock while closing or waiting.

**Tests:**

- Nil handler receiver behavior follows section 2.1; shutdown before any request is idempotent.
- Shutdown rejects later requests with 503.
- Shutdown during negotiation read, negotiation write, active call, and peer close terminates each registered resource.
- A request blocked before WebSocket registration makes standalone `Handler.Shutdown` wait until its context expires; closing the surrounding HTTP server releases it.
- A negotiation-success/shutdown race cannot lose ownership.
- A timed-out shutdown returns the exact context error.
- A later shutdown can finish after an earlier timed-out caller returns.
- Concurrent repeated shutdown calls are race-free.
- No hijacked WebSocket survives successful shutdown.

**Gate:**

```sh
go test ./transport/websocket
go test -race -count=20 ./transport/websocket
go test ./...
```

### Task 18 — Add WebSocket `ListenAndServe`

**Commit:** `websocket: add cloudflared-friendly ListenAndServe`

**Files:**

- new `transport/websocket/server.go`
- server tests

**Implementation:**

1. Add `ListenAndServe(ctx, addr, path, export, import)`.
2. Validate context, an already available context error, nonempty address, literal decoded request path, and binding metadata before binding a TCP listener. The path must begin with `/` and contain no query or fragment delimiter.
3. Build a `Handler` and assign `http.Server.Handler` a private exact-path wrapper, not a caller-derived `http.ServeMux` pattern. Compare `r.URL.Path` byte for byte, return HTTP 404 for every mismatch, and never redirect or clean a request into a match. A trailing slash and braces in the configured value are literals, not subtree or wildcard syntax. Differently escaped request spellings that decode to the same `URL.Path` are intentionally equivalent.
4. Create an `http.Server` with `ReadHeaderTimeout: 10 * time.Second` and plain HTTP serving. Do not set HTTP `ReadTimeout` or `WriteTimeout` across upgraded WebSockets.
5. Do not configure certificates, TLS listeners, Cloudflare headers, or trusted proxies.
6. On context cancellation after serving starts, call `http.Server.Close` to stop admission and interrupt pre-upgrade HTTP work, and invoke `Handler.Shutdown` so hijacked WebSockets are also closed.
7. Call `http.Server.Close` and `Handler.Shutdown(context.Background())` concurrently, then wait for both. The non-canceled cleanup context ensures the canceled serving context cannot make handler cleanup a no-op; native adapter close tests are responsible for proving this wait terminates.
8. On every serve exit, including an unexpected listener error, close both ownership classes and wait for handler cleanup before returning.
9. Return `http.ErrServerClosed` for context-driven orderly shutdown after serving starts. An already canceled context returns its exact error before binding.
10. Preserve bind and unexpected serve errors after cleanup; cleanup errors do not replace the primary serve error.
11. Add an unexported `serve(listener, ...)` helper so tests can use an OS-assigned port without changing the public API.

**Tests:**

- Serve a hello call over plain loopback HTTP.
- Only the exact configured decoded path upgrades. Cover suffixes, a configured trailing slash, literal braces and wildcard-looking text, repeated slashes, and escaped separators; no case may trigger mux matching or a panic.
- Context cancellation returns `http.ErrServerClosed`.
- Active WebSockets and stalled handshakes close before orderly return.
- Bind errors preserve `net.OpError` identity.
- No TLS configuration is required.
- A simulated cloudflared-style request host/origin works under the documented same-origin policy.
- Concurrent clients complete before shutdown and fail cleanly during shutdown.

**Gate:**

```sh
go test ./transport/websocket
go test -race ./transport/websocket
go test ./...
```

### Task 19 — Harden WebSocket lifecycle and echo rejection

**Commit:** `websocket: cover transport and negotiation races`

**Files:**

- WebSocket package tests
- implementation files only for fixes demonstrated by tests

**Implementation:**

1. Add deterministic barriers around upgrade, stream registration, negotiation, connection transfer, call activity, peer close, and handler shutdown.
2. Implement both a raw byte-echo WebSocket server and a passive echo client. Prove the server cannot satisfy an asymmetric client handshake and the passive client cannot satisfy the server's client-first handshake.
3. Repeat connect/call/close and connect/shutdown races.
4. Exercise normal-close, going-away, abrupt TCP close, text-message close, setup timeout, and context cancellation mappings.
5. Confirm compression remains off and subprotocol remains empty across all high-level paths.
6. Make only required synchronization/error-mapping fixes; do not add public API.

**Tests:**

- No goroutine or connection leak across repeated start/stop cycles.
- No double close or registry loss under shutdown races.
- The asymmetric echo server is rejected with `ErrInterfaceMismatch`; the passive echo client is rejected by the server setup timeout before any InterCall frame is read.
- Abrupt failures preserve coder/network error identity through root terminal wrapping.
- `go test -race -count=20 ./transport/websocket` passes.

**Gate:**

```sh
go test -count=20 ./transport/websocket
go test -race -count=20 ./transport/websocket
go test ./...
```

### Task 20 — Add executable transport examples and package documentation

**Commit:** `transport: document high and low level APIs`

**Files:**

- `transport/unixsocket/doc.go`
- `transport/unixsocket/example_test.go`
- `transport/websocket/doc.go`
- `transport/websocket/example_test.go`

**Implementation:**

1. Document ownership, contexts, setup timeout, negotiation direction, and shutdown in both package overviews.
2. Add `//go:build unix` compile-checked examples for Unix `Dial`, `ListenAndServe`, `DialStream`, and authenticated raw accept.
3. Add compile-checked examples for WebSocket `Dial`, `ListenAndServe`, `NewHandler` with authentication middleware, and custom `DialStream` TLS/header options.
4. Use `EmptyExportBinding`/`EmptyImportBinding` in one-way examples.
5. State that interface IDs detect contract mismatch but do not authenticate peers.
6. State that Unix peer credentials and HTTP authentication remain application policy.
7. State that WebSocket message boundaries are invisible to InterCall.
8. Keep examples concise enough to serve as API compile tests.

**Tests:**

- All examples compile.
- Executable examples use local temporary resources and terminate deterministically.
- Example output, where asserted, is stable.

**Gate:**

```sh
go test ./transport/unixsocket ./transport/websocket
go test ./...
```

### Task 21 — Reconcile the native transport specification

**Commit:** `docs: reconcile Go transport negotiation`

**Files:**

- `README.md`
- `SPEC.md`
- root `doc.go` as needed

**Implementation:**

1. Audit the incrementally updated Go specification against the final source and correct any drift in the exact two-record handshake, including client/server order and the rule that each side sends only its import/expected-peer ID.
2. Reconcile the canonical-body SHA-256 definition with artifact ownership and clearly distinguish it from process binding identity and credentials.
3. Verify legacy/raw versus metadata-aware/negotiated constructors and empty-direction bindings are complete in the public runtime listing.
4. Reconcile setup phase timeouts, close-once ownership, mismatch errors, pre-serving context errors, server-closed results, and the deliberate absence of an acknowledgment.
5. Reconcile Unix path, permission, identity-capture limitations, cleanup, and server-shutdown behavior.
6. Reconcile binary WebSocket stream behavior, text rejection, message limit, origin checking, compression, reserved dial headers, literal exact-path routing, cloudflared deployment boundary, and shutdown.
7. Update the general README interface-agreement discussion without making this Go profile mandatory for every implementation.
8. Confirm implemented Unix socket and WebSocket items are absent from deferred features while WebTransport, TLS setup, authentication policy, reconnect, and streaming remain deferred.
9. Check every public signature in prose against source and remove duplicated or contradictory text introduced by incremental edits.

**Tests:**

- Run documentation link/path checks available in the repository.
- Run the full Go suite to catch stale examples and generated references.

**Gate:**

```sh
go test ./...
git diff --check
```

### Task 22 — Add the end-user Unix, WebSocket, and cloudflared guide

**Commit:** `docs: add native transport usage guide`

**Files:**

- `GO.md`
- any README navigation link that points to the new sections

**Implementation:**

1. Add complete provider, generation, Unix server, and Unix client walkthroughs.
2. Add complete loopback WebSocket server and public `wss://` client walkthroughs.
3. Show cloudflared forwarding to `http://127.0.0.1:8080` and clearly assign public TLS termination to cloudflared.
4. Show `NewHandler` wrapped by authentication middleware.
5. Show low-level Unix acceptance followed by application peer authentication and `NewNegotiatedServerConnection`.
6. Explain when to choose raw `NewConnection`, negotiated root constructors, low-level transport streams, or high-level helpers.
7. Explain the two empty-direction bindings.
8. Document `ErrInterfaceMismatch`, local missing-metadata errors, Unix `ErrServerClosed`, and WebSocket `http.ErrServerClosed`.
9. Update requirements, dependency, runtime-reference, and out-of-scope tables.
10. Copy final snippets from compile-checked examples where possible to prevent drift.

**Tests:**

- Manually compare all shown signatures to package docs/source.
- Run all example and full tests.

**Gate:**

```sh
go test ./...
git diff --check
```

### Task 23 — Add cross-transport acceptance and compatibility gates

**Commit:** `integration: verify native transport compatibility`

**Files:**

- new integration tests under `internal/integration` or a dedicated transport integration package
- test helpers only; production changes only for defects exposed by acceptance tests

**Implementation:**

1. Run the same generated hello/fixture call over a real Unix socket and a real WebSocket server.
2. Run bidirectional generated calls over each transport.
3. Verify import/export generated from the same canonical body negotiate, while a one-byte semantic-body change does not.
4. Verify empty reverse bindings work identically on both transports.
5. Verify a raw legacy `NewConnection` pair still starts directly with a 24-byte frame and performs no negotiation.
6. Verify generated artifact ownership stamps remain unchanged by runtime metadata embedding.
7. Verify transport shutdown does not alter existing `Connection.Close`/`Wait` first-cause semantics.
8. Add compile-time assertions for the intended public API signatures.
9. Run selected POSIX cross-compiles for `transport/unixsocket`.
10. Record no golden timestamps, ports, temporary paths, or environment-specific bytes.

**Tests and final gate:**

```sh
gofmt -w <all-changed-go-files>
go test ./...
go test -race ./...
go vet ./...
go test -count=20 ./transport/unixsocket ./transport/websocket
go test -race -count=10 ./transport/unixsocket ./transport/websocket
tmp=$(mktemp -d)
GOOS=darwin GOARCH=amd64 go test -c -o "$tmp/unixsocket-darwin.test" ./transport/unixsocket
GOOS=freebsd GOARCH=amd64 go test -c -o "$tmp/unixsocket-freebsd.test" ./transport/unixsocket
rm -rf "$tmp"
git diff --check
```

This commit is complete only when all commands pass from a clean checkout with generated fixtures already current. It must not be used as a catch-all cleanup commit; any substantial defect found here should be fixed by revising the earlier owning commit before the series is finalized.

## 12. Explicit non-goals

This plan does not add:

- TCP convenience APIs;
- TLS certificate loading or TLS termination in the WebSocket convenience server;
- Cloudflare-specific headers or trust policy;
- built-in user authentication or authorization;
- Linux peer-credential policy (the low-level Unix API merely makes it possible);
- WebSocket subprotocol negotiation;
- handshake version negotiation, magic bytes, capabilities, or acknowledgments;
- reconnect, retry, pooling, or session resumption;
- multiple InterCall connections over one WebSocket;
- WebTransport, QUIC, or independent per-call streams;
- wire-level cancellation;
- streaming procedure parameters or results;
- changes to the 24-byte InterCall frame format;
- interface IDs as credentials or security capabilities.

## 13. Completion criteria

The work is complete when:

- the four high-level hello-world examples compile and pass;
- low-level Unix and WebSocket APIs permit authentication before negotiation;
- generated bindings carry the existing canonical SHA-256 interface ID;
- only the peer's expected interface ID is sent in each handshake direction;
- an asymmetric echo server and a passive echo client do not pass their respective negotiation roles;
- empty-direction bindings make one-way client/server setup concise;
- Unix path permissions, refusal, ownership, and cleanup behavior are tested;
- WebSocket text rejection, message-boundary independence, message limits, origin defaults, compression defaults, reserved dial headers, and literal exact-path routing are tested;
- context cancellation closes listeners, setup transports, and active connections without leaks;
- raw `NewConnection` remains compatible and metadata-free;
- all generated fixtures regenerate deterministically;
- `go test ./...`, `go test -race ./...`, and `go vet ./...` pass.
