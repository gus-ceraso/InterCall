# Bidirectional hello world

This example connects a TypeScript browser client to a Go server over a
negotiated InterCall WebSocket. It demonstrates a nested call in the opposite
direction:

1. the browser calls Go's `hello(name)` procedure;
2. while handling that call, Go calls the browser's `locale()` procedure;
3. the browser returns the selected locale, or `navigator.language` when no
   override is selected; and
4. Go returns a localized greeting.

English, Spanish, French, German, and Brazilian Portuguese (`pt-BR` or
`pt_BR`) are recognized. Other locales fall back to English.

## Requirements

- Go 1.26.5;
- Node.js 22.12 or newer;
- GNU Make.

The example uses the repository's `../../go` and `../../typescript`
implementations. Vite is used to build the browser application and provide its
development server.

## Run the built application

From this directory:

```sh
make run
```

Then open <http://127.0.0.1:8080>. `make run` installs the local build tools,
builds the Vite application, builds the Go server, and serves both the static
application and `/intercall` from one origin.

## Development mode

```sh
make dev
```

Open <http://localhost:5173>. Vite serves the frontend with hot reload and
proxies `/intercall` to the Go server on `127.0.0.1:8080`. The proxy rewrites
the WebSocket origin only for this loopback development setup so the Go
handler's same-origin check remains enabled. Production deployments should
serve a same-origin endpoint or configure trusted origins explicitly rather
than treating origin rewriting as authentication.

## Regenerate bindings

The generated interfaces and bindings are checked in. Regenerate all four
artifacts after changing either provider:

```sh
make generate
```

Generation runs in dependency order:

1. export `frontend/src/providers.ts` to `api/browser.intercall` and the browser
   export binding;
2. import that interface for the Go client used by `hello`;
3. export `backend/hello` to `api/backend.intercall` and the Go export binding;
4. import that interface for the browser's typed backend client.

Do not edit files under `backend/gen`, `frontend/src/generated`, or
`api/*.intercall` by hand.

## Other targets

```sh
make install   # install/build local generators and frontend dependencies
make build     # build the Go server and production frontend
make check     # type-check TypeScript, test Go, and build the frontend
make clean     # remove example build output and local tools
make distclean # also remove frontend/node_modules
```

## Layout

```text
api/                         generated peer interfaces
backend/
  cmd/server/                HTTP, static-file, and WebSocket server
  hello/                     Go hello provider and translation test
  gen/backendexport/         generated Go export binding
  gen/browserimport/         generated Go client for browser.locale
frontend/
  src/providers.ts           browser locale provider and override state
  src/main.ts                connection and form behavior
  src/generated/backend/     generated TypeScript client for Go.hello
  src/generated/browser/     generated browser export binding
  vite.config.ts             local WebSocket development proxy
Makefile                     install, generation, build, and run targets
```
