# TypeScript InterCall instructions

## Scope

These instructions apply to `typescript/` and its descendants. The TypeScript
runtime is browser-only; Node.js is permitted for the CLI, build tooling, and
tests.

## Project layout

- `src/browser/`: browser WebSocket transport and connection setup. Do not add
  Node imports here.
- `src/runtime/`: dependency-free codecs, framing, connection state, bindings,
  and dispatch runtime.
- `src/generated-spi/`: the public SPI used by generated bindings.
- `src/syntax/`: InterCall parser, formatter, validator, and AST.
- `src/tool/`: compiler discovery, validation, import/export generation, and
  metadata handling. This code runs in the CLI/tooling build.
- `src/cli/`: the `intercall-ts` Node.js CLI.
- `test/`: black-box runtime, browser, codec, syntax, tool, compiler-fixture,
  and integration tests.
- `PLAN.md`: sequential implementation plan; update completed tasks when
  making corresponding changes.

## Commands

From `typescript/`:

```sh
npm ci
npm run build
npm run typecheck
npm test
npm run test:tool
npm run test:browser
npm run test:integration
npm run check:fixtures
npm pack --dry-run
```

Run focused tests with `npm run build:cli` or `npm run build:browser` first,
then use `node --test path/to/test.mjs`.

## Implementation rules

- Keep the package ESM and dependency-free at browser runtime.
- Preserve strict TypeScript checking. Do not weaken compiler options or use
  broad type assertions to bypass validation.
- Use iterative algorithms for deep syntax, type, codec, and payload graphs;
  do not introduce recursion on attacker-controlled depth.
- Preserve immutable ownership: freeze public programs, generated adapters,
  zero-width values, finished buffers, and decoded byte storage as required by
  nearby APIs.
- Preserve fixed resource ceilings and stable error codes/messages. Add tests
  at exact boundaries when changing limits or allocation behavior.
- Generated calls use positional wire parameters and `createClient(connection)`.
  Keep generated code isolated through `@cerasos/intercall/generated`.
- Generated output must be deterministic, canonical, strictly type-checked in
  memory, and free of timestamps, absolute paths, temporary paths, or map-order
  dependence.
- Validate all inputs before creating or mutating output directories. Generated
  artifacts must use ownership markers and atomic, same-directory replacement;
  never overwrite handwritten files, follow target symlinks, or delete stale
  paths.
- Browser entry points must not import Node modules. Check browser-boundary
  tests when changing exports or shared modules.
- Treat generated fixtures as checked-in artifacts: ordinary tests compare them
  and must not rewrite them. Use an explicit maintenance command for updates.
- When changing wire behavior, run the Go interoperability/vector tests as well
  as TypeScript tests.

## Editing and review

- Prefer small, focused commits that complete one `PLAN.md` task.
- Follow existing naming, formatting, error taxonomy, and ownership patterns.
- Add focused regression tests for every bug and boundary condition.
- Run `git diff --check`, relevant focused tests, `npm run typecheck`, and the
  appropriate full suite before committing.
- Do not commit `node_modules/`, `dist/`, temporary outputs, Playwright browser
  downloads, or generated artifacts unless the fixture explicitly requires
  them.
