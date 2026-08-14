# InterCall TypeScript

This document describes the TypeScript browser implementation of InterCall.
The protocol and interface language are defined by [`../README.md`](../README.md).
The TypeScript mapping and browser contracts are defined by
[`SPEC.md`](SPEC.md). The existing Go implementation is documented by
[`../go/GO.md`](../go/GO.md) and [`../go/SPEC.md`](../go/SPEC.md).

The TypeScript peer runs in a browser and connects as a WebSocket client. It
can both call procedures exported by Go and export procedures that Go calls.
There is no TypeScript WebSocket server in this package.

## Requirements

- Node.js 22 or newer for the `intercall-ts` build CLI;
- a current browser with `BigInt`, `WebSocket`, `AbortSignal`, `TextEncoder`,
  `TextDecoder`, and `DataView`;
- a TypeScript project configured for ESM output;
- the Go InterCall package and a Go WebSocket endpoint when using a Go backend.

The browser runtime has no production dependency. The CLI uses the pinned
TypeScript compiler shipped with this package and does not execute application
code while discovering providers.

## Package layout

A typical project keeps the shared interface files, Go backend, and browser
source separate:

```text
api/
    backend.intercall
    browser.intercall
backend/
    go.mod
    hello/
        hello.go
    gen/
        backendexport/
        browserimport/
frontend/
    package.json
    tsconfig.json
    src/
        providers.ts
        main.ts
        generated/
            backend/
            browser/
Makefile
```

The Go export command owns the backend interface and backend export binding.
The TypeScript import command consumes that interface and creates the browser
client. The TypeScript export command owns the browser interface and browser
export binding. Go imports that interface to call browser procedures.

## Install and build

Install the package in the browser project:

```sh
npm install @cerasos/intercall
```

Install the CLI where the build runs:

```sh
npm install --save-dev @cerasos/intercall typescript
```

Use ESM and strict TypeScript checking. A minimal `frontend/tsconfig.json` is:

```json
{
    "compilerOptions": {
        "target": "ES2022",
        "module": "NodeNext",
        "moduleResolution": "NodeNext",
        "lib": ["ES2022", "DOM"],
        "strict": true,
        "noUncheckedIndexedAccess": true,
        "exactOptionalPropertyTypes": true,
        "jsx": "preserve",
        "outDir": "dist"
    },
    "include": ["src"]
}
```

The application bundler or TypeScript compiler must preserve the generated
`.js` or `.jsx` import specifiers selected by the generator. The CLI validates
those specifiers against the project configuration.

## Interface files

An interface file is the shared contract. It contains the complete exported
procedures and exceptions of one peer. For example, `api/backend.intercall`
may contain:

```intercall
exception internal_exception;

exception invalid_arguments;

exception procedure_not_found;

procedure hello {
    name string;
} string;
```

The three fixed exceptions are inserted automatically by the Go and TypeScript
exporters. They must be declared with their required no-payload shapes if they
are written by hand. They are reserved names in the generated profiles.

The file has no interface name, namespace, imports, version header, or enclosing
block. Type declarations must precede references to named types. Procedure and
exception keys are derived from their exact wire names, so changing a name
changes the wire contract even when its types do not change.

## Generate a Go backend and browser client

### 1. Write the Go provider

`backend/hello/hello.go`:

```go
package hello

import "context"

// Hello returns a greeting.
// @intercall procedure
// @param name The name to greet.
// @return The greeting.
func Hello(ctx context.Context, name string) (string, error) {
	return "Hello, " + name + "!", nil
}
```

The Go function is an exported, nongeneric, nonvariadic package-level function.
Its first parameter is exactly `context.Context`, and its final result is
`error`. The remaining parameters and optional preceding result are wire
values.

### 2. Generate the Go export binding

With the `intercall-go` CLI available on `PATH`, from the Go application
module root:

```sh
intercall-go export \
    --out gen/backendexport \
    --interface ../api/backend.intercall \
    ./hello
```

This writes an owned backend export binding and the canonical
`api/backend.intercall` file. The generated interface includes the fixed
runtime exceptions.

### 3. Generate the TypeScript import binding

From the frontend project or repository root:

```sh
npx intercall-ts import \
    --out frontend/src/generated/backend \
    api/backend.intercall
```

The generated `frontend/src/generated/backend/binding_gen.ts` exports named
wire types, remote exception types, static codecs, interface metadata, an
`importBinding`, and a typed `createClient` factory.

Generated procedures use positional arguments because InterCall parameters are
ordered wire values and the Go generated API is positional:

```ts
const backend = createBackendClient(connection);
const greeting = await backend.hello("world");
```

A final `CallOptions` object is runtime state, not a wire parameter:

```ts
const controller = new AbortController();
const pending = backend.longOperation(value, { signal: controller.signal });
controller.abort();
await pending; // rejects with the signal's reason
```

The generated client does not create or own the connection.

## Export browser procedures

The browser can export procedures for Go to call. A provider is an exported
function marked with `@intercall procedure`. Its first parameter is the exact
`HandlerContext` type, followed by positional wire parameters. It returns
`Promise<void>` or `Promise<T>`.

`frontend/src/providers.ts`:

```ts
import type {
    HandlerContext,
    Uint32,
} from "@cerasos/intercall";

/**
 * Reports progress to the browser UI.
 * @intercall procedure report_progress
 * @param completed Number of completed work units.
 */
export async function reportProgress(
    context: HandlerContext,
    completed: Uint32,
): Promise<void> {
    if (context.signal.aborted) {
        return;
    }
    const element = document.querySelector("#progress");
    if (element !== null) {
        element.textContent = String(completed);
    }
}
```

Use the exact numeric marker aliases (`Int8`, `Uint32`, `Int64`, and so on) in
exported wire signatures. Bare `number` and `bigint` do not identify an exact
wire type and are rejected by the exporter. Use `Uint8Array` for `bytes`; use a
readonly array of `Uint8` for `list uint8`.

### Application exceptions

A no-payload exception is an exported `const` `Error` value:

```ts
/** @intercall exception denied */
export const Denied = new Error("denied");
```

A payload exception extends the runtime `PayloadException<T>` class. Its
payload is encoded as the declared wire value:

```ts
import {
    PayloadException,
    type Int32,
} from "@cerasos/intercall";

/** @intercall exception failed */
export class Failed extends PayloadException<{
    readonly code: Int32;
    readonly detail: string;
}> {
}
```

A zero-width payload uses the exact `EmptyRecord` marker:

```ts
import {
    PayloadException,
    type EmptyRecord,
} from "@cerasos/intercall";

/** @intercall exception blank */
export class Blank extends PayloadException<EmptyRecord> {
}
```

A provider throws the sentinel or a payload exception instance:

```ts
export async function protectedAction(
    context: HandlerContext,
): Promise<void> {
    if (!hasPermission()) {
        throw Denied;
    }
    if (hasInvalidState()) {
        throw new Failed({ code: 7, detail: "invalid state" });
    }
}
```

Only declared exceptions are sent. A provider failure that does not match
exactly one declared exception becomes the fixed `internal_exception` response.

### Generate the browser export binding

```sh
npx intercall-ts export \
    --project frontend/tsconfig.json \
    --out frontend/src/generated/browser \
    --interface api/browser.intercall \
    frontend/src/providers.ts
```

The exporter accepts `.ts` and `.tsx` implementation files. Generate the
matching Go import binding from the Go application module root:

```sh
intercall-go import \
    --out gen/browserimport \
    ../api/browser.intercall
```

The Go server then uses `gen/browserimport` as its import binding when it needs
to call browser procedures. It follows the
project's `jsx` setting when validating generated runtime import specifiers.
It rejects declaration files, JavaScript files, generated binding files,
methods, overload sets, arrow-function variables, default exports, unsupported
wire types, and recursive values.

The generated binding imports providers statically and exposes an immutable
`exportBinding`. It does not use reflection, a runtime registry, or callbacks
registered by application code.

## Connect the browser to Go

The browser uses a negotiated client WebSocket connection:

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
console.log(greeting);

window.addEventListener("pagehide", () => connection.close());
```

A relative URL is resolved against the page URL. `http:` becomes `ws:` and
`https:` becomes `wss:`. Use `wss:` in deployments that require confidentiality.
The negotiated constructor requires interface IDs on both bindings and performs
the Go-compatible client-first 32-byte exchange before returning.

For a one-way browser binding, use the empty export binding and import only the
backend interface. The Go server can use `intercall.EmptyImportBinding()` for
the unused reverse direction. Use `connectRawWebSocket` only when the transport
is already configured to agree on exactly matching interfaces out of band.

The browser WebSocket must use binary messages. InterCall frames may span
messages, and one message may contain several frames. The browser runtime
rejects text messages and does not interpret message boundaries as frame
boundaries.

## Go WebSocket endpoint

The Go process owns the server side of the WebSocket upgrade. A minimal handler
uses the generated backend export binding and generated browser import binding:

```go
package main

import (
	"log"
	"net/http"

	intercall "github.com/cerasos/intercall/go"
	"github.com/cerasos/intercall/go/transport/websocket"

	"example.com/project/backend/gen/backendexport"
	"example.com/project/backend/gen/browserimport"
)

func main() {
	mux := http.NewServeMux()
	mux.Handle("/intercall", websocket.NewHandler(
		backendexport.ExportBinding(),
		browserimport.ImportBinding(),
	))
	mux.Handle("/", http.FileServer(http.Dir("../frontend/dist")))

	log.Fatal(http.ListenAndServe(":8080", mux))
}
```

Authentication and authorization remain application policy around the Go HTTP
handler. Interface IDs detect mismatched contracts; they are not credentials.
For same-origin deployments, serve the browser application and WebSocket route
from the same origin. For cross-origin deployments, configure the Go WebSocket
handler's origin policy and surrounding authentication deliberately.

## Bidirectional calls

A browser provider can call a Go procedure using a generated client constructed
from its handler connection. The connection is available on `HandlerContext`:

```ts
import type { HandlerContext } from "@cerasos/intercall";
import {
    createClient as createBackendClient,
} from "./generated/backend/binding_gen.js";

export async function refreshFromBackend(
    context: HandlerContext,
): Promise<void> {
    const backend = createBackendClient(context.connection);
    const greeting = await backend.hello("browser");
    document.querySelector("#greeting")!.textContent = greeting;
}
```

Go handlers can likewise call browser procedures when the Go connection was
constructed with the browser import binding. Either peer may have multiple
outstanding calls, and either peer may issue a nested call while handling an
incoming request. The protocol does not impose execution order between
independent calls.

## Cancellation and lifecycle

Per-call cancellation is local. It retires the request ID, rejects the local
Promise with the exact `AbortSignal.reason` (or a default `AbortError`), and
does not send a cancellation frame. The remote handler may continue running.

```ts
const controller = new AbortController();
const request = backend.longOperation(value, {
    signal: controller.signal,
});
controller.abort(new Error("navigation changed"));

try {
    await request;
} catch (error) {
    if (error instanceof Error && error.message === "navigation changed") {
        // The connection remains usable.
    }
}
```

`connection.close()` publishes the local terminal cause immediately and starts
socket cleanup. `connection.closed` resolves after the socket and receive loop
have finished. It does not wait for a handler that ignores its abort signal.
There is no reconnect or retry policy in the package.

## Limits and security

The frame payload ceiling is exactly 64 MiB. The browser profile additionally
limits visible aggregate receive bytes, active handler payload bytes, active
handlers, outstanding calls, owned outgoing frame bytes, browser send-buffer
admission, and codec value nodes. These are implementation-safety limits, not
application authorization policy. The browser WebSocket API has no receive
backpressure, so native buffering remains a residual browser risk; the runtime
closes when its visible queue limit is exceeded.

InterCall supplies no confidentiality, authentication, authorization, or
integrity. Use WSS and authenticate the HTTP/WebSocket connection as required.
Do not treat procedure keys or interface IDs as credentials.

## Makefile integration

A project can regenerate both directions with ordinary build tooling:

```make
.PHONY: generate

generate:
	cd backend && intercall-go export \
		--out gen/backendexport \
		--interface ../api/backend.intercall \
		./hello
	npx intercall-ts import \
		--out frontend/src/generated/backend \
		api/backend.intercall
	npx intercall-ts export \
		--project frontend/tsconfig.json \
		--out frontend/src/generated/browser \
		--interface api/browser.intercall \
		frontend/src/providers.ts
	cd backend && intercall-go import \
		--out gen/browserimport \
		../api/browser.intercall
```

Generated files are deterministic and generator-owned. The commands validate
source, interfaces, generated bytes, and generated TypeScript before replacing
owned targets. They never overwrite handwritten files or delete stale paths.

## Troubleshooting

### The browser fails during negotiation

Check that the browser import binding contains the exact interface ID expected
by the Go export binding and that the browser export binding contains the exact
ID expected by the Go import binding. A mismatch means the interfaces differ;
it is not an authentication result.

### A generated procedure has the wrong name

Use an explicit `@intercall procedure wire_name`, `@intercall param`,
or `@intercall field` directive. Import-side generated identifiers can be
changed with `--ts-name`. These change native names only; wire names, keys, and
interface IDs remain unchanged.

### A call fails with `invalid_arguments`

The request payload did not encode the declared parameters exactly. Check
parameter order, exact numeric marker types, `bytes` versus `list uint8`,
required record fields, and named type declarations.

### A call fails with `internal_exception`

The provider panicked or rejected unexpectedly, exception matching was
ambiguous, or the selected return/exception value could not be encoded. Declare
the application exception and throw exactly its sentinel or payload class.

### The connection closes after a response

A matched response had an undeclared exception, an invalid value, a
noncanonical NaN, a truncated value, or trailing bytes. An unmatched late
response is intentionally ignored and does not undergo value validation.
