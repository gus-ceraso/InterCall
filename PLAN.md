# Go PoC Loop-Engineering Plan

## Purpose, authority, and boundary

This plan schedules the complete Go proof of concept as worker/reviewer loops. It
turns the frozen architecture into commit-sized execution units; it does not
restate or redesign that architecture.

Authorities, in order:

1. [`README.md`](README.md) is normative for the interface language and wire
   protocol.
2. [`SPEC.md`](SPEC.md) is the frozen Go mapping, generator, CLI, and runtime
   architecture. If it conflicts with `README.md`, `README.md` wins.
3. [`intercall-validate.lua`](intercall-validate.lua) is an optional,
   non-normative differential oracle. Lua or LPeg absence never blocks Go work;
   any difference must be investigated against `README.md`, not resolved by
   automatically following Lua.

The implementation includes every feature in `SPEC.md`. The only product
boundary is [SPEC's Deferred Features](SPEC.md#deferred-features): no
TypeScript, WebSocket/WebTransport or per-call transport adapter, handshake or
runtime interface agreement, authentication/authorization/policy, resource
limits, dialing/listening/TLS, transport or wire cancellation, streaming
values, or compatibility with older Go toolchains. These are out of scope, not
backlog items. Nothing else in `SPEC.md` may be deferred.

Implementation prerequisites are commits `74a155f` (repository preparation),
`26ca2d4` (frozen specification), and `099ac83` (architectural simplification).
The approved plan is commit `e79f6e1`. The root module is
`github.com/cerasos/intercall` and declares Go 1.26.5. Every command below uses
`/usr/local/go/bin/go`; `go` from `PATH` is not authoritative.

Repository-wide role and engineering guidance is in [`AGENTS.md`](AGENTS.md).
The step-by-step Orchestrator runbook is
[`ORCHESTRADOR.md`](ORCHESTRADOR.md). Those documents operationalize this plan
and do not override its task contracts or DAG.

## Mandatory loop protocol

### Roles and isolation

- **Orchestrator:** schedules only. It creates worktrees and branches, passes
  dependencies and handoffs, returns findings to the persistent sessions,
  stages an approved diff, commits it, records the hash, and integrates approved
  commits. It performs no technical validation, code assessment, test rerun,
  conflict resolution, or implementation edit. Its sole content-edit exception
  is a factual [Progress](#progress) update after integration; that
  administrative action grants no technical approval.
- **Worker:** one persistent read/write session per task. It implements only the
  task, adds durable tests/fixtures/docs, runs the required evidence commands,
  and never commits or creates an evidence report.
- **Reviewer:** one separate persistent, read-only session per task. It reviews
  the complete diff and relevant local/global context, independently runs the
  gates, and never edits or commits. The reviewer, not the orchestrator, owns
  the final technical gate.

Use branch `task/<branch-slug>` and sibling worktree
`../InterCall-worktrees/<branch-slug>`; the exact branch slugs appear in [the
DAG](#dependency-dag-and-scheduling). Each worktree starts from an integration
commit containing all listed dependencies. Parallel workers never
share a writable checkout. If worktrees are unavailable, run the same tasks
sequentially in isolated checkouts.

Before review, both roles use `task_status` below without optional Git locks and
confirm that the dirty worktree contains exactly the task paths declared in the
handoff. The Worker supplies the complete `task_snapshot_diff`; a plain
working-tree diff is not a complete review target. The Reviewer independently
renders that same snapshot, then reads the task's governing `README.md` and
`SPEC.md` sections and enough dependent code to trace the behavior.

### Worker handoff template

```text
TASK: IC-NN <title>
BASE: <dependency-complete commit hash>
BRANCH/WORKTREE: task/<branch-slug> | <path>
SUMMARY: <implemented behavior, without claiming untested behavior>
FILES: <added/modified/deleted paths; no unrelated files>
DURABLE TESTS: <test/fuzz/fixture names and behaviors>
EVIDENCE:
- <exact command>: PASS/FAIL (<exit status and salient result>)
- <exact command>: PASS/FAIL/SKIP (<reason; SKIP only where allowed>)
ORACLE: <Lua/LPeg result, unavailable, or not applicable; explain differences>
STATUS/DIFF: <task_status output and complete task_snapshot_diff summary>
SNAPSHOT CHECK: <task_snapshot_check result over exactly the declared task paths>
BLOCKERS/QUESTIONS: <none, or an escalation under the rules below>
```

A Worker does not hand off known task-scope failures as residual risk.

### Reviewer handoff template

When there are findings:

```text
TASK: IC-NN <title>
BASE/DIFF REVIEWED: <base hash; all paths, including untracked files>
FINDINGS:
1. [BLOCKER|MAJOR|MINOR] <path:line> — <actionable defect>
   Evidence: <trigger and violated README/SPEC/test contract>
   Required change: <smallest correction>
VALIDATION:
- <independently run exact command>: PASS/FAIL (<salient result>)
SNAPSHOT: <task_status inventory; complete task_snapshot_diff reviewed;
           task_snapshot_check; unchanged original status before/after any
           reviewer_fuzz invocation>
RESIDUAL RISKS: <only explicit out-of-scope constraints, otherwise none>
```

When there are zero actionable findings, the first line is exactly:

```text
APPROVED
```

It is followed by the task/base/diff reviewed, independent command evidence,
and `RESIDUAL RISKS: none` (or only an explicit deferred constraint). The word
`APPROVED` must not appear in a findings handoff.

### Revision, approval, commit, and integration

1. The orchestrator sends every finding to the same Worker session.
2. The Worker fixes the task, updates tests, reruns all Worker evidence, and
   returns a new handoff.
3. The same Reviewer session rereads the complete current diff and independently
   reruns the gate. Repeat until the Reviewer returns exact `APPROVED`.
4. After approval, no file in that worktree changes. The orchestrator stages the
   reviewed diff, commits it with the task's exact subject, records the hash,
   and integrates it in DAG order. These are administrative actions, not
   validation.
5. Parallel siblings integrate in dependency order, using ascending task ID to
   break ties. If cherry-picking conflicts, or an integrated sibling invalidates
   an assumption, the orchestrator does not resolve it: the Worker rebases or
   resolves against the integrated result, and the Reviewer must approve the
   resulting complete diff again.
6. A branch must pass global checks by itself. If that is impossible without a
   sibling, merge the scopes into one task or add the sibling as a dependency;
   never approve a broken intermediate commit.

Evidence remains in session transcripts. Do not commit logs, reports, coverage
files, fuzz cache, binaries, or temporary output. Durable tests, corpus seeds,
source fixtures, generated golden fixtures, examples, and user documentation
belong in commits.

### Escalation

- Fix a task-introduced finding in that task.
- Make a pre-existing blocker a separately approved prerequisite repair loop,
  then rebase and resume affected branches.
- Move a nonblocking pre-existing issue out of the current task into a
  separately scoped follow-up loop at the earliest dependency-correct point. If
  it is outside `README.md`/`SPEC.md` scope, escalate scheduling rather than
  silently expanding this implementation; do not create a generic backlog.
- If `SPEC.md` is defective or ambiguous enough to prevent implementation,
  pause affected work for a dedicated specification amendment -> review ->
  commit loop, then rebase and resume. Do not silently redesign in code or in
  this plan.
- Reviewer preferences block only for correctness, security, material
  simplicity, or project consistency. `APPROVED` means no actionable finding.

## Validation suites

Every task runs one of these suites. The Reviewer reruns it independently; a
Worker result is evidence, not the gate. Run the reusable helpers and suites in
Bash from the task worktree root.

**Snapshot and fuzz helpers**

```bash
task_status() {
    GIT_OPTIONAL_LOCKS=0 git status --short --untracked-files=all
}

task_snapshot() (
    set -eu
    top=$(git rev-parse --show-toplevel)
    objects=$(git -C "$top" rev-parse --path-format=absolute --git-path objects)
    tmp=$(mktemp -d)
    trap 'rm -rf "$tmp"' EXIT HUP INT TERM
    mkdir "$tmp/objects"
    export GIT_INDEX_FILE="$tmp/index"
    export GIT_OBJECT_DIRECTORY="$tmp/objects"
    export GIT_ALTERNATE_OBJECT_DIRECTORIES="$objects"
    git -C "$top" read-tree HEAD
    git -C "$top" add -A
    git -C "$top" diff --cached "$@"
)

task_snapshot_diff() {
    task_snapshot --binary --
}

task_snapshot_check() {
    task_snapshot --check
}

reviewer_fuzz() (
    set -uo pipefail
    top=$(git rev-parse --show-toplevel) || exit
    before=$(GIT_OPTIONAL_LOCKS=0 git -C "$top" status --short --untracked-files=all) || exit
    tmp=$(mktemp -d) || exit
    trap 'rm -rf "$tmp"' EXIT HUP INT TERM
    tar -C "$top" --exclude='./.git' -cf - . | tar -C "$tmp" -xf - || exit
    (cd "$tmp" && "$@")
    rc=$?
    after=$(GIT_OPTIONAL_LOCKS=0 git -C "$top" status --short --untracked-files=all) || exit
    if [ "$before" != "$after" ]; then
        printf '%s\n' 'reviewer fuzz changed the original worktree' >&2
        exit 1
    fi
    exit "$rc"
)
```

Before either snapshot command, each role runs `task_status` and compares every
path with the handoff's intended task files; unrelated dirt is a finding or
escalation, not part of the snapshot. `task_snapshot` starts a temporary index
at `HEAD` and adds the complete current worktree (tracked, staged, deleted, and
nonignored untracked paths). `task_snapshot_diff` renders the review diff, while
`task_snapshot_check` runs its cached-diff whitespace check. The temporary
object directory also prevents `git add` from writing new objects to the
repository. Cleanup leaves the real index and worktree untouched.

For every listed fuzz command, the Worker initializes `fuzz_gate() { "$@"; }`.
The read-only Reviewer instead initializes
`fuzz_gate() { reviewer_fuzz "$@"; }`. `reviewer_fuzz` copies the exact current
worktree, including untracked task files but excluding `.git`, into a disposable
directory, runs one command there, destroys the copy, and verifies the original
administrative status is unchanged. The Reviewer records `task_status` before
and after fuzzing. This prevents a failing fuzz run from writing
`testdata/fuzz` into the reviewed worktree.

**`G` — global green baseline**

```bash
test "$(/usr/local/go/bin/go env GOVERSION)" = go1.26.5
/usr/local/go/bin/go test -count=1 ./...
/usr/local/go/bin/go vet ./...
task_snapshot_check
test -z "$(find . -type f -name '*.go' -not -path './.git/*' -print0 | xargs -0 -r /usr/local/go/bin/gofmt -l)"
```

**`R` — runtime/concurrency gate:** run `G`, then:

```bash
/usr/local/go/bin/go test -race -count=1 ./...
```

`-count=1` prevents a Reviewer from accepting a Worker's cached normal or race
result. Narrow task-specific test commands likewise carry an explicit positive
`-count`; stress commands may use a larger count. Fuzz commands are bounded by
executions rather than left open and always use the role-specific `fuzz_gate`.
Generator tasks also run a fixture compile test and deterministic-output test.
Filesystem/output tasks run host-filesystem safety tests. A task may add
narrower commands, but never substitute them for `G` or `R`.

## Task sizing and dependency rules

One task is one Worker/Reviewer loop and one coherent, buildable commit. Use
medium tasks on the critical path where splitting would require placeholders or
an invalid intermediate API. Use finer tasks for independent syntax and runtime
work. Never parallelize two tasks that modify the same package or settle the
same API contract. Tests added in one task must pass without a future task;
private components may precede their public integration, but placeholder
production behavior and knowingly incomplete exported behavior are forbidden.

Generated code is tested as generated code: compile durable fixtures rather
than only comparing strings. Concurrency state is tested under the race
detector. Parsers and decoders retain corpus seeds and run bounded fuzz smoke.
Artifact tests regenerate into temporary directories and byte-compare outputs;
they never rewrite checked-in fixtures during validation.

## Dependency DAG and scheduling

| Wave when dependencies are available | Tasks that may run concurrently |
| --- | --- |
| 1 | `IC-01` |
| 2 | `IC-02`, `IC-03` |
| 3 | `IC-04`, `IC-05` |
| 4 | `IC-06`, `IC-07`, `IC-08` |
| 5 | `IC-09`, `IC-10` |
| 6 onward | `IC-11` -> `IC-12` -> `IC-13` -> `IC-14` -> `IC-15` |
| Join and finish | `IC-16` -> `IC-17` -> `IC-18` -> `IC-19` -> `IC-20` |

Expanded, the DAG has 15 topological waves: the first five are shown
individually, and waves 6 through 15 each contain the next single task in the
two displayed chains.

The critical tool path joins both equally deep branches at
`IC-01 -> IC-02 -> IC-04 -> {IC-06, IC-08} -> IC-10`, then continues
`IC-11 -> IC-12 -> IC-13 -> IC-14 -> IC-15 -> IC-16 -> IC-17 -> IC-18 -> IC-19 -> IC-20`.
The independent runtime path is
`IC-01 -> IC-03 -> IC-05 -> IC-07 -> IC-09 -> IC-16`.
The naming task `IC-08` branches from validated syntax and rejoins at `IC-10`.
This is the available safe concurrency; the later generator tasks remain serial
because they share `internal/tool` contracts and fixtures.

| ID | Branch/worktree slug | Dependencies | Safe parallel peers |
| --- | --- | --- | --- |
| `IC-01` | `ic-01-spi` | completed prerequisites | none |
| `IC-02` | `ic-02-syntax-parser` | `IC-01` | `IC-03` |
| `IC-03` | `ic-03-runtime-lifecycle` | `IC-01` | `IC-02` |
| `IC-04` | `ic-04-syntax-validation` | `IC-02` | `IC-05` |
| `IC-05` | `ic-05-frame-io` | `IC-03` | `IC-04` |
| `IC-06` | `ic-06-semantic-format` | `IC-04` | `IC-07`, `IC-08` |
| `IC-07` | `ic-07-pending-calls` | `IC-05` | `IC-06`, `IC-08` |
| `IC-08` | `ic-08-naming` | `IC-04` | `IC-06`, `IC-07` |
| `IC-09` | `ic-09-runtime-dispatch` | `IC-07` | `IC-10` |
| `IC-10` | `ic-10-codec-emitter` | `IC-06`, `IC-08` | `IC-09` |
| `IC-11` | `ic-11-directives` | `IC-10` | none |
| `IC-12` | `ic-12-package-discovery` | `IC-11` | none |
| `IC-13` | `ic-13-type-mapping` | `IC-12` | none |
| `IC-14` | `ic-14-export-model` | `IC-13` | none |
| `IC-15` | `ic-15-artifacts` | `IC-14` | none |
| `IC-16` | `ic-16-import-generator` | `IC-09`, `IC-15` | none |
| `IC-17` | `ic-17-export-generator` | `IC-16` | none |
| `IC-18` | `ic-18-cli` | `IC-17` | none |
| `IC-19` | `ic-19-e2e` | `IC-18` | none |
| `IC-20` | `ic-20-hardening` | `IC-19` | none |

## Progress

`[ ]` means pending and `[x]` means integrated. Append the integrated commit hash
to each completed task entry. Approval alone is not completion; in-progress and
review state remains in session transcripts. Task Workers never edit this
checklist. After parallel siblings integrate, the Orchestrator batches their
factual checkbox/hash updates into one administrative progress commit to avoid
branch conflicts.

Completed prerequisites:

- [x] `74a155f` — repository preparation
- [x] `26ca2d4` — frozen specification
- [x] `099ac83` — architectural simplification
- [x] `e79f6e1` — approved loop-engineering plan

Implementation tasks:

- [x] [IC-01 — Scaffold the module and define the generated-code SPI](#ic-01--scaffold-the-module-and-define-the-generated-code-spi) — `9be3982`
- [x] [IC-02 — Parse interface syntax with exact positions and comments](#ic-02--parse-interface-syntax-with-exact-positions-and-comments) — `48571e7`
- [x] [IC-03 — Implement connection lifecycle, binding, and context core](#ic-03--implement-connection-lifecycle-binding-and-context-core) — `9d168f1`
- [x] [IC-04 — Validate protocol semantics and calculate FNV-0 keys](#ic-04--validate-protocol-semantics-and-calculate-fnv-0-keys) — `0392b3e`
- [x] [IC-05 — Implement frame I/O and exclusive write ownership](#ic-05--implement-frame-io-and-exclusive-write-ownership) — `9972b5e`
- [x] [IC-06 — Attach semantic documentation and format canonical interfaces](#ic-06--attach-semantic-documentation-and-format-canonical-interfaces) — `0a83dea`
- [x] [IC-07 — Implement outgoing calls, pending ownership, IDs, and cancellation](#ic-07--implement-outgoing-calls-pending-ownership-ids-and-cancellation) — `0c6d096`
- [x] [IC-08 — Implement exact Go and wire naming projection](#ic-08--implement-exact-go-and-wire-naming-projection) — `bf90654`
- [x] [IC-09 — Integrate receive dispatch, incoming IDs, handlers, and shutdown](#ic-09--integrate-receive-dispatch-incoming-ids-handlers-and-shutdown) — `81c3206`
- [x] [IC-10 — Build the direct code-generation model and wire codec emitter](#ic-10--build-the-direct-code-generation-model-and-wire-codec-emitter) — `5665a05`
- [x] [IC-11 — Parse Go documentation and InterCall directives](#ic-11--parse-go-documentation-and-intercall-directives) — `c4f0375`
- [x] [IC-12 — Discover packages and enforce procedure selection and signatures](#ic-12--discover-packages-and-enforce-procedure-selection-and-signatures) — `6e9da0a`
- [ ] [IC-13 — Map Go values, named types, and generated semantic metadata](#ic-13--map-go-values-named-types-and-generated-semantic-metadata)
- [ ] [IC-14 — Model exported interfaces and application exceptions](#ic-14--model-exported-interfaces-and-application-exceptions)
- [ ] [IC-15 — Write stamped generated artifacts safely and deterministically](#ic-15--write-stamped-generated-artifacts-safely-and-deterministically)
- [ ] [IC-16 — Generate complete import bindings](#ic-16--generate-complete-import-bindings)
- [ ] [IC-17 — Generate complete export bindings](#ic-17--generate-complete-export-bindings)
- [ ] [IC-18 — Add CLI commands, options, and diagnostics](#ic-18--add-cli-commands-options-and-diagnostics)
- [ ] [IC-19 — Exercise generated peers end to end](#ic-19--exercise-generated-peers-end-to-end)
- [ ] [IC-20 — Document and harden the complete Go proof of concept](#ic-20--document-and-harden-the-complete-go-proof-of-concept)

## Executable tasks

The non-goals below assign work to later task IDs; they are not product
deferrals.

### IC-01 — Scaffold the module and define the generated-code SPI

- **Depends / peers:** completed prerequisite commits; no peer.
- **Objective and scope:** create the root runtime package and the foundational
  public generated-code bridge types, opaque immutable binding handles,
  constructors, local classifications, and fixed exception sentinels from [Immutable binding
  pair](SPEC.md#immutable-binding-pair), [Lifecycle and local
  errors](SPEC.md#lifecycle-and-local-errors), and [Fixed Go Runtime
  Exceptions](SPEC.md#fixed-go-runtime-exceptions). Keep the exported surface
  to the specification.
- **Non-goals:** connection construction/methods (`IC-03`/`IC-09`), frames,
  syntax, discovery, and generation.
- **Deliverables:** root package source and focused external/internal tests;
  package documentation may be skeletal until `IC-20` but all exported symbols
  receive accurate contract comments.
- **Durable acceptance:** nil dispatch rejection; zero, copied, and separately
  constructed handle identity; non-zero identity storage; concurrent sharing;
  sentinel text, direct comparison, and `errors.Is`; compile-time `ByteStream`
  and callback signatures.
- **Worker evidence:** `R`.
- **Reviewer:** compare every export and error classification with the cited
  sections; inspect zero-size identity and nil-interface traps; independently
  run `R`.
- **Commit:** `scaffold Go runtime SPI`

### IC-02 — Parse interface syntax with exact positions and comments

- **Depends / peers:** `IC-01`; safe with `IC-03`.
- **Objective and scope:** implement `internal/syntax` lexical scanning, the
  source-only AST with spans and documentation slots, comment/trivia capture,
  UTF-8 validation, and parsing of the complete [README grammar](README.md#grammar)
  under [Syntax model and validation](SPEC.md#syntax-model-and-validation).
  Positions use exact input byte offsets and support later physical diagnostics.
- **Non-goals:** name/key semantics (`IC-04`), comment attachment/normalization
  and formatting (`IC-06`), or Go projection.
- **Deliverables:** `internal/syntax` parser/AST/position code, table/golden
  tests, `testdata`, and `FuzzParse` seeds.
- **Durable acceptance:** empty/trivia-only inputs, every type nesting form,
  keyword boundaries, reserved-token syntax positions, nonnesting comments,
  BOM rejection, invalid UTF-8 at the first bad byte, CR/LF accounting,
  unexpected/truncated tokens, and EOF position.
- **Worker evidence:** `G`, then
  `fuzz_gate /usr/local/go/bin/go test -run '^$' -fuzz '^FuzzParse$' -fuzztime=1000x ./internal/syntax`.
- **Reviewer:** trace scanner progress and span boundaries, ensure arbitrary
  bytes cannot panic or loop, inspect corpus quality, and independently run the
  same commands.
- **Commit:** `parse InterCall interface syntax`

### IC-03 — Implement connection lifecycle, binding, and context core

- **Depends / peers:** `IC-01`; safe with `IC-02`.
- **Objective and scope:** implement the private connection state and terminal
  selection, exact-cause context observer, stream ownership/one-time close,
  handler-context cancellation foundation, binding identity checks, and public
  context bind/lookup behavior from [Immutable binding
  pair](SPEC.md#immutable-binding-pair) and [Lifecycle and local
  errors](SPEC.md#lifecycle-and-local-errors). Expose no placeholder constructor
  or method; public receive-loop integration completes in `IC-09`.
- **Non-goals:** frame I/O (`IC-05`), pending calls (`IC-07`), receive dispatch
  and complete `NewConnection`/`Wait` integration (`IC-09`).
- **Deliverables:** root lifecycle/context files and deterministic synchronization
  tests using controllable streams and contexts.
- **Durable acceptance:** constructor-core validation before ownership, exact
  cancellation causes, first-cause races, one close, cleanup-error suppression,
  nil panic/error contracts, context replacement, observer exit with nil
  `Done`, and no goroutine leak in tested paths.
- **Worker evidence:** `R`, plus
  `/usr/local/go/bin/go test -race . -run '^(TestLifecycle|TestConnectionContext)' -count=20`.
- **Reviewer:** trace lock/channel ownership and every observer exit; reject
  timing sleeps and provisional production behavior; independently run the same
  commands.
- **Commit:** `implement connection lifecycle core`

### IC-04 — Validate protocol semantics and calculate FNV-0 keys

- **Depends / peers:** `IC-02`; safe with `IC-05`.
- **Objective and scope:** add complete protocol validation and 64-bit FNV-0 for
  [README scopes and resolution](README.md#grammar) and [Procedure and Exception
  Keys](README.md#procedure-and-exception-keys), as required by [Syntax model and
  validation](SPEC.md#syntax-model-and-validation). Diagnostics retain source
  spans and validate unreachable declarations.
- **Non-goals:** documentation formatting, Go naming/projection, generation.
- **Deliverables:** validator/key code, valid/invalid fixtures, collision and
  key-vector tests, and an optional differential test harness for the Lua oracle.
- **Durable acceptance:** earlier-only type references, self/forward/unknown
  references, every global/local duplicate and reserved-word case, nested
  record scopes, key zero/collision across procedure and exception kinds, and
  README key vectors.
- **Worker evidence:** `G`, the `IC-02` bounded fuzz command, and:

  ```sh
  if command -v lua >/dev/null && lua -e 'require("lpeg")' >/dev/null 2>&1; then
      /usr/local/go/bin/go test ./internal/syntax -run '^TestLuaDifferential$' -count=1
  else
      echo 'SKIP: Lua/LPeg unavailable'
  fi
  ```

- **Reviewer:** verify modular unsigned arithmetic and validation order without
  treating Lua as normative; investigate every differential result; independently
  run the same commands.
- **Commit:** `validate InterCall interface semantics`

### IC-05 — Implement frame I/O and exclusive write ownership

- **Depends / peers:** `IC-03`; safe with `IC-04`.
- **Objective and scope:** add private frame parsing/building, owned payload
  buffering, full-read behavior, response-bit handling, native-size checks, one
  connection-wide write gate, and full-write/no-progress validation from
  [Frames](README.md#frames), [Failures and Limits](README.md#failures-and-limits),
  and [Frame writing and generated codecs](SPEC.md#frame-writing-and-generated-codecs).
- **Non-goals:** value codecs (`IC-10`), outgoing ownership (`IC-07`), receive
  routing/handlers (`IC-09`), or policy limits.
- **Deliverables:** root frame/write files, adversarial reader/writer tests, and
  `FuzzReadFrame` corpus.
- **Durable acceptance:** fragmented reads/writes, EOF and incomplete header or
  payload, oversized wire lengths before conversion/allocation, exact 24-byte
  layout, invalid writer counts, zero progress, partial-write failure,
  response-bit clear/set behavior, and concurrent frames never interleaving.
- **Worker evidence:** `R`, then
  `fuzz_gate /usr/local/go/bin/go test -run '^$' -fuzz '^FuzzReadFrame$' -fuzztime=1000x .`.
- **Reviewer:** inspect overflow ordering, buffer ownership, `io.Reader`/writer
  edge contracts, and gate release on every path; independently run the same
  commands.
- **Commit:** `implement InterCall frame I/O`

### IC-06 — Attach semantic documentation and format canonical interfaces

- **Depends / peers:** `IC-04`; safe with `IC-07` and `IC-08`.
- **Objective and scope:** implement recursive one-time comment attachment,
  normalization, documentation slots, and the byte-exact canonical formatter in
  [Semantic documentation](SPEC.md#semantic-documentation). Formatting accepts
  validated syntax and preserves source declaration order.
- **Non-goals:** Go comment directives (`IC-11`), base64 metadata consumption
  (`IC-13`) or emission (`IC-16`), artifact ownership markers (`IC-15`).
- **Deliverables:** `internal/syntax` documentation/formatter code, exhaustive
  golden fixtures, round-trip/property tests, and `FuzzParseFormat`.
- **Durable acceptance:** every eligible nested anchor, comments after names and
  `list`, trailing/unattached comments, blank-line grouping, CRLF/bare-CR and
  indentation normalization, Unicode, empty documents/interfaces/records,
  documented type line breaks, idempotent parse-validate-format, and final LF.
- **Worker evidence:** `G`, then
  `fuzz_gate /usr/local/go/bin/go test -run '^$' -fuzz '^FuzzParseFormat$' -fuzztime=1000x ./internal/syntax`.
- **Reviewer:** derive representative goldens directly from the cited algorithm,
  check that each comment attaches at most once, and independently run the same
  commands.
- **Commit:** `format semantic InterCall interfaces`

### IC-07 — Implement outgoing calls, pending ownership, IDs, and cancellation

- **Depends / peers:** `IC-05`; safe with `IC-06` and `IC-08`.
- **Objective and scope:** implement `Call` validation and ordering, monotonic
  63-bit allocation, pending-map ownership transfer, write admission, response
  claim hooks, local cancellation, and terminal interaction exactly as [Calls,
  pending ownership, and IDs](SPEC.md#calls-pending-ownership-and-ids) defines.
  Tests may drive private claim hooks until `IC-09` supplies the reader.
- **Non-goals:** receive-loop routing and handler execution (`IC-09`), generated
  request/response codecs (`IC-16`/`IC-17`), cancellation frames.
- **Deliverables:** root call/pending code and state-machine/race tests.
- **Durable acceptance:** argument/binding/terminal/context precedence; encoder
  at-most-once and no-call cases; encoder exact errors; no allocation before
  write admission; final-ID exhaustion and no reuse; full-duplex response during
  write; cancellation retirement; and exclusive response/cancel/terminal
  outcomes with closure visibility.
- **Worker evidence:** `R`, plus
  `/usr/local/go/bin/go test -race . -run '^(TestCall|TestPending)' -count=50`.
- **Reviewer:** trace each of the eight specified call steps and prove one map
  removal owns each outcome; inspect lock ordering with the write gate;
  independently run the same commands.
- **Commit:** `implement outgoing InterCall calls`

### IC-08 — Implement exact Go and wire naming projection

- **Depends / peers:** `IC-04`; safe with `IC-06` and `IC-07`.
- **Objective and scope:** create `internal/tool` naming conversion, fixed
  initialism handling, import override selector parsing/resolution, identifier
  visibility rules, deterministic private mangling, and scope collision checks
  from [Names and native overrides](SPEC.md#names-and-native-overrides).
- **Non-goals:** package discovery/type mapping, source directives, generation.
- **Deliverables:** naming/selector code and table/property tests covering every
  specification example and nested selector shape.
- **Durable acceptance:** longest-initialism behavior, ASCII and underscore
  rejection, checked inverse round trips, canonical/noncanonical wire names,
  Pascal/camel visibility, named-reference traversal boundary, unresolved and
  duplicate overrides, keywords, and all declaration/field/parameter collision
  scopes.
- **Worker evidence:** `G`, plus
  `/usr/local/go/bin/go test ./internal/tool -run '^(TestNaming|TestGoNameOverrides)$' -count=1`.
- **Reviewer:** compare all accepted/rejected examples to the exact algorithm
  and inspect deterministic collision handling; independently run the same
  commands.
- **Commit:** `implement Go name projection`

### IC-09 — Integrate receive dispatch, incoming IDs, handlers, and shutdown

- **Depends / peers:** `IC-07`; safe with `IC-10`.
- **Objective and scope:** complete the public connection API and sole receive
  loop under [Reading, dispatch, and response
  validation](SPEC.md#reading-dispatch-and-response-validation), integrating
  `IC-03`/`IC-05`/`IC-07`: matched response decode, opaque unmatched responses,
  incoming-ID reservation, unbounded handler goroutines, bound/canceled handler
  contexts, dispatch panic recovery, response writes, teardown, `Close`, and
  `Wait`.
- **Non-goals:** generated static procedure switch and typed codecs (`IC-17`),
  transport setup/limits/handshake, remote cancellation.
- **Deliverables:** completed root connection/receive/handler code and
  full-duplex lifecycle/protocol tests.
- **Durable acceptance:** constructor validation and receive-loop start before
  return; only one reader; out-of-order responses; decoder error/panic terminal
  handling; unknown-ID payload opacity; incoming duplicate before/after write;
  handler completion/terminal races; exact permanent cause; observer/receive
  teardown waited by `Wait`; `Close` nonwaiting; ignored-cancellation handlers
  cannot write after terminal.
- **Worker evidence:** `R`, plus
  `/usr/local/go/bin/go test -race . -run '^(TestConnection|TestReceive|TestHandler|TestWait)' -count=30`.
- **Reviewer:** trace goroutine, stream, channel, map, and lock ownership end to
  end; verify malformed matched and opaque unmatched distinctions against
  README; independently run the same commands.
- **Commit:** `implement connection receive dispatch`

### IC-10 — Build the direct code-generation model and wire codec emitter

- **Depends / peers:** `IC-06`, `IC-08`; safe with `IC-09`.
- **Objective and scope:** implement the small command-specific generation
  records and emitted append encoders/bounded decoders required by [Interface
  Processing](SPEC.md#interface-processing) and [Frame writing and generated
  codecs](SPEC.md#frame-writing-and-generated-codecs). Use syntax order and
  names directly; do not introduce a second general IR, plugin framework, or
  public runtime codec API.
- **Non-goals:** Go source discovery/type mapping, complete import/export
  bindings, or runtime framing.
- **Deliverables:** `internal/tool` source-emission primitives, a compiled codec
  fixture, wire-vector/round-trip tests, malformed corpus, and
  `FuzzGeneratedDecoder`.
- **Durable acceptance:** all primitives, canonical NaN emit/reject, UTF-8,
  bytes/lists, named/list/anonymous nested records, zero-width records and lists,
  checked arithmetic/conversion/allocation, nonnil empty decoded slices, field
  order, payload exhaustion, owned byte retention, and deterministic valid Go.
- **Worker evidence:** `G`, then:

  ```sh
  /usr/local/go/bin/go test ./internal/tool -run '^(TestGeneratedCodecFixtureCompiles|TestGeneratedCodecVectors|TestCodecGenerationDeterminism)$' -count=1
  fuzz_gate /usr/local/go/bin/go test -run '^$' -fuzz '^FuzzGeneratedDecoder$' -fuzztime=1000x ./internal/tool
  ```

- **Reviewer:** inspect generated fixture source as code, independently compile
  it, audit all hostile length paths and zero-width fast paths, and run the same
  commands.
- **Commit:** `generate InterCall wire codecs`

### IC-11 — Parse Go documentation and InterCall directives

- **Depends / peers:** `IC-10`; no safe peer in `internal/tool`.
- **Objective and scope:** implement generated-marker recognition, physical Go
  source positions, complete logical-line directive grammar, placement and
  contradiction checks, Go documentation extraction/normalization, and `*/`
  rejection from [Source directives and Go
  documentation](SPEC.md#source-directives-and-go-documentation) and [Safe
  import and re-export metadata](SPEC.md#safe-import-and-re-export-metadata).
- **Non-goals:** package pattern loading/signature checks (`IC-12`), type graph
  or generated metadata structural recovery (`IC-13`), source emission.
- **Deliverables:** `internal/tool` source-document model and parser tests over
  handwritten and generated Go fixtures.
- **Durable acceptance:** all directive forms and optional operands,
  param/return context exclusions, sentinel single-variable rule,
  malformed/unknown/bare/misplaced/duplicate directives, retained non-InterCall
  tags/prose, `//line` immunity, generated-file trust boundary, Unicode docs,
  and terminator rejection.
- **Worker evidence:** `G`, plus
  `/usr/local/go/bin/go test ./internal/tool -run '^(TestDirectives|TestGoDocumentation|TestGeneratedMarker)$' -count=1`.
- **Reviewer:** distinguish handwritten machine-looking prose from trusted
  marked metadata and verify physical positions; independently run the same
  commands.
- **Commit:** `parse InterCall Go directives`

### IC-12 — Discover packages and enforce procedure selection and signatures

- **Depends / peers:** `IC-11`; no safe peer.
- **Objective and scope:** add the pinned `golang.org/x/tools/go/packages`
  dependency and implement active module/workspace package-pattern loading,
  explicit package identity, output/provider importability checks, eligible
  functions, include/exclude filters, and exact signatures from [Package
  discovery and selection](SPEC.md#package-discovery-and-selection) and
  [Procedure signatures and wire values](SPEC.md#procedure-signatures-and-wire-values).
- **Non-goals:** reachable value mapping and type imports (`IC-13`), exception
  interface assembly (`IC-14`), CLI flag parsing (`IC-18`).
- **Deliverables:** `go.mod`, `go.sum`, `internal/tool` discovery/selection code,
  and temporary-module/workspace fixtures.
- **Durable acceptance:** overlap deduplication, unmatched patterns, explicit
  versus dependency packages, exclusion of tests/variants/main/generated
  selectors, import cycles/internal visibility, no implicit scan/file operand,
  exact filter grammar/errors/exclusion precedence, methods/generics/variadics,
  aliases versus defined lookalikes for `context.Context`/`error`, named wire
  parameters, and result arity.
- **Worker evidence:** `G`, then:

  ```sh
  /usr/local/go/bin/go mod tidy -diff
  /usr/local/go/bin/go test ./internal/tool -run '^(TestPackageDiscovery|TestProcedureSelection|TestProcedureSignatures)$' -count=1
  ```

- **Reviewer:** inspect `go/packages` mode/configuration and canonical path
  handling across modules/workspaces; verify dependency changes are minimal;
  independently run the same commands.
- **Commit:** `discover Go procedure packages`

### IC-13 — Map Go values, named types, and generated semantic metadata

- **Depends / peers:** `IC-12`; no safe peer.
- **Objective and scope:** implement the exact source-form-sensitive Go value
  mapping, aliases, anonymous records, field tags, reachable named-type graph,
  importability, recursion rejection, stable topological order, import override
  application, and trusted generated-type semantic recovery from [Procedure
  signatures and wire values](SPEC.md#procedure-signatures-and-wire-values),
  [Deterministic export order](SPEC.md#deterministic-export-order), and [Safe
  import and re-export metadata](SPEC.md#safe-import-and-re-export-metadata).
- **Non-goals:** application exception collection/interface assembly (`IC-14`)
  or complete binding emission.
- **Deliverables:** `internal/tool` type graph/mapping/metadata code and
  cross-package source fixtures.
- **Durable acceptance:** `[]byte` versus `[]uint8` at every source/alias node,
  all unsupported forms, nil/empty semantics in codec facts, exported required
  fields/types, embedding and tag rejection, named preservation/alias flattening,
  recursion through slices/records, deterministic wire-name topology, generated
  machine line plus field tags, exactly one canonical base64url constant,
  canonical reparse/structural match, nested-doc recovery without directive
  rescanning, and inaccessible/colliding imports.
- **Worker evidence:** `G`, `IC-10`'s bounded decoder fuzz command, and
  `/usr/local/go/bin/go test ./internal/tool -run '^(TestTypeMapping|TestNamedTypeOrder|TestSemanticMetadata)$' -count=1`.
- **Reviewer:** correlate `go/ast` source spellings with `go/types`, audit graph
  termination and metadata trust checks, and independently run the same
  commands.
- **Commit:** `map Go values to InterCall types`

### IC-14 — Model exported interfaces and application exceptions

- **Depends / peers:** `IC-13`; no safe peer.
- **Objective and scope:** collect and validate all tagged application
  exceptions, reserve/insert fixed runtime exceptions, apply docs/wire names,
  build the small export generation records and canonical interface AST, and
  record direct matching facts under [Application
  exceptions](SPEC.md#application-exceptions), [Fixed Go Runtime
  Exceptions](SPEC.md#fixed-go-runtime-exceptions), and [Deterministic export
  order](SPEC.md#deterministic-export-order).
- **Non-goals:** emitting export wrappers/static switch/codecs (`IC-17`) or CLI.
- **Deliverables:** `internal/tool` export model/interface assembly and
  exception fixtures/tests.
- **Durable acceptance:** sentinel and payload exception forms, `*T` error
  implementation, exception/type role conflicts, all explicit-package
  exceptions regardless of procedure filters, direct equality/assertion facts,
  wrapped/typed-nil/zero-or-multiple/panic fallback facts, exact fixed exception
  shapes and keys, global collisions, docs at supported slots, and stable
  type/exception/procedure order.
- **Worker evidence:** `G`, plus
  `/usr/local/go/bin/go test ./internal/tool -run '^(TestApplicationExceptions|TestExportInterfaceModel|TestExportOrder)$' -count=1`.
- **Reviewer:** verify no `errors.Is`/`errors.As` semantics enter the model and
  compare canonical output with formatter rules; independently run the same
  commands.
- **Commit:** `model exported InterCall interfaces`

### IC-15 — Write stamped generated artifacts safely and deterministically

- **Depends / peers:** `IC-14`; no safe peer because generators share artifact
  contracts.
- **Objective and scope:** implement in-memory validation, exact stamps/markers,
  package-name resolution, one-file ownership, non-following collision checks,
  same-filesystem temp staging/rename replacement, interrupted two-target export
  repair, deterministic bytes, and sorted diagnostics from [One-file ownership
  and safe replacement](SPEC.md#one-file-ownership-and-safe-replacement) and
  [Diagnostics](SPEC.md#diagnostics).
- **Non-goals:** command parsing (`IC-18`) or import/export source emitters.
- **Deliverables:** `internal/tool` artifact/stamp/writer code and host-filesystem
  safety tests; tests operate only in temporary directories.
- **Durable acceptance:** exact ownership lines and body hash, canonical
  interface marker separation, output creation ordering, output package rules,
  Go-named collisions, symlink/directory/device rejection, host filename
  equivalence, mode/package/stamp ownership, preservation of unrelated non-Go
  files and hard links, unchanged inode on no-op, no deletion/truncation,
  differing/missing paired stamps recovery, replacement failure safety, and no
  timestamps/absolute/temp paths/map order.
- **Worker evidence:** `G`, then
  `/usr/local/go/bin/go test ./internal/tool -run '^(TestArtifactDeterminism|TestArtifactFilesystemSafety|TestArtifactInterruptedExportRepair|TestDiagnosticsSort)$' -count=1`.
- **Reviewer:** attempt hostile leaf and interrupted-state cases, inspect every
  mutation boundary and validation-before-creation guarantee, and independently
  run the same commands.
- **Commit:** `write generated artifacts safely`

### IC-16 — Generate complete import bindings

- **Depends / peers:** `IC-09`, `IC-15`; no safe peer in `internal/tool`.
- **Objective and scope:** implement [Go Import Model](SPEC.md#go-import-model),
  including exact-input parse/validation, all mapped named and anonymous inline
  record declarations, callers, application/fixed exceptions, recursive docs
  metadata, codecs, and the immutable import binding singleton. This binding
  handle is the simplified architecture's descriptor replacement; do not add a
  descriptor schema, client object, registry, or runtime digest.
- **Non-goals:** export wrappers (`IC-17`) and CLI parsing (`IC-18`).
- **Deliverables:** import emitter/entry point, checked generated fixture package,
  generation goldens, compile/runtime tests, and `FuzzImportResponseDecoder`.
- **Durable acceptance:** empty interface; all types and nested anonymous
  records; declaration/field/parameter overrides; exact tags and type machine
  lines; no-payload/record/other-payload exceptions; fixed sentinel mapping;
  context connection lookup and no-closure construction on absence; caller
  result storage; one canonical chunked base64url constant including empty and
  >4096-byte cases; valid formatting/stamp; deterministic byte output.
- **Worker evidence:** `R`, then:

  ```sh
  /usr/local/go/bin/go test ./internal/tool -run '^(TestImportGeneratedFixtureCompiles|TestImportGeneration|TestImportGenerationDeterminism|TestArtifactFilesystemSafety)$' -count=1
  fuzz_gate /usr/local/go/bin/go test -run '^$' -fuzz '^FuzzImportResponseDecoder$' -fuzztime=1000x ./internal/tool
  ```

- **Reviewer:** read and compile the complete generated fixture, compare every
  declaration with its input AST, inspect closure ownership/decoder exhaustion
  and metadata chunks, and independently run the same commands.
- **Commit:** `generate InterCall import bindings`

### IC-17 — Generate complete export bindings

- **Depends / peers:** `IC-16`; no safe peer in `internal/tool`.
- **Objective and scope:** emit the canonical owned interface and complete
  export binding from [Go Export Model](SPEC.md#go-export-model): immutable
  export handle, provider wrappers, static key switch, request decoders,
  response encoders, runtime exceptions, direct application-exception matching,
  panic/encoding fallback, and deterministic imports/codecs. Do not add
  reflection, registration, or callback layers.
- **Non-goals:** CLI argument handling and cross-peer integration scenarios.
- **Deliverables:** export emitter/entry point, provider and checked generated
  fixture packages, exact interface golden, compile/runtime tests, and
  `FuzzExportRequestDecoder`.
- **Durable acceptance:** selected wrapper signatures/context; malformed or
  trailing args skip providers and select `invalid_arguments`; unknown static
  key selects `procedure_not_found`; provider/matching/encoding panics and
  invalid errors select `internal_exception`; success and each direct exception
  payload; typed nil/multiple match; fixed exception insertion; stable imports
  and switch; all generated codecs; deterministic interface and Go bytes.
- **Worker evidence:** `R`, then:

  ```sh
  /usr/local/go/bin/go test ./internal/tool -run '^(TestExportGeneratedFixtureCompiles|TestExportGeneration|TestExportGenerationDeterminism|TestArtifactFilesystemSafety)$' -count=1
  fuzz_gate /usr/local/go/bin/go test -run '^$' -fuzz '^FuzzExportRequestDecoder$' -fuzztime=1000x ./internal/tool
  ```

- **Reviewer:** inspect generated wrapper code and direct matching, compile/run
  the fixture independently, compare body/stamp/order with the syntax formatter,
  and run the same commands.
- **Commit:** `generate InterCall export bindings`

### IC-18 — Add CLI commands, options, and diagnostics

- **Depends / peers:** `IC-17`; no safe peer.
- **Objective and scope:** implement `cmd/intercall-go` with the exact import and
  export grammar, repeatable flags, package defaults, diagnostic rendering and
  ordering, validation/mutation ordering, and artifact writer integration from
  [Commands](SPEC.md#commands), [One-file ownership and safe
  replacement](SPEC.md#one-file-ownership-and-safe-replacement), and
  [Diagnostics](SPEC.md#diagnostics).
- **Non-goals:** stdin, implicit scans, manifests, transport commands, or any
  deferred feature.
- **Deliverables:** command package, black-box command tests, help text, and
  temporary workspace/output fixtures.
- **Durable acceptance:** exact operand counts; required/distinct targets;
  repeatable include/exclude/go-name flags; package validation/default/owned
  consistency; exact input bytes; physical byte-column diagnostics and logical
  path normalization; sorted multi-errors; no staging path; validation failure
  creates nothing; ownership failure creates/replaces nothing; success and
  interrupted export repair; deterministic repeated invocation.
- **Worker evidence:** `G`, plus:

  ```sh
  /usr/local/go/bin/go test ./cmd/intercall-go ./internal/tool -run '^(TestCLI|TestCommandDiagnostics|TestCommandDeterminism|TestArtifactFilesystemSafety)$' -count=1
  tmp=$(mktemp -d); trap 'rm -rf "$tmp"' EXIT; /usr/local/go/bin/go build -o "$tmp/intercall-go" ./cmd/intercall-go
  ```

- **Reviewer:** invoke the binary against success, validation-failure, and
  unowned-collision fixtures; verify no filesystem mutation precedes its allowed
  point; independently run the same commands.
- **Commit:** `add intercall-go commands`

### IC-19 — Exercise generated peers end to end

- **Depends / peers:** `IC-18`; no safe peer.
- **Objective and scope:** add durable generated fixture modules and black-box
  tests joining generated import/export packages over established byte streams.
  Reconcile [Requests and Responses](README.md#requests-and-responses) and the
  complete [Generated Binding SPI and Runtime](SPEC.md#generated-binding-spi-and-runtime)
  across real generated code.
- **Non-goals:** new public APIs, transport setup/adapters, policy limits,
  handshake, or specification changes.
- **Deliverables:** `internal/integration` harness, source and checked generated
  fixtures/modules, malformed-stream helpers, and deterministic regeneration
  comparison. Tests generate into temporary copies; validation never edits the
  checked fixtures.
- **Durable acceptance:** all primitive/value shapes and deeply nested anonymous
  records; named types and every exception shape; bidirectional and nested calls;
  concurrent/out-of-order calls and equal opposing IDs; provider and decoder
  failures; unknown procedure/malformed arguments; malformed matched response
  versus opaque late/unmatched response; duplicate incoming ID; partial I/O;
  call cancellation with late response; connection context/Close/EOF/write
  races; exact terminal causes and shutdown without leaks.
- **Worker evidence:** `R`, then:

  ```sh
  /usr/local/go/bin/go test ./internal/integration -run '^(TestGeneratedFixtureModules|TestCheckedInGeneratedFixturesAreCurrent|TestGeneratedOutputDeterminism)$' -count=1
  /usr/local/go/bin/go test -race ./internal/integration -run '^(TestBidirectional|TestNested|TestConcurrent|TestCancellation|TestMalformed|TestShutdown)$' -count=20
  ```

- **Reviewer:** independently regenerate/byte-compare in a temporary directory,
  inspect both generated sides, and stress protocol/lifecycle distinctions with
  the same commands. Verify fixture paths remain unchanged after tests.
- **Commit:** `test generated peers end to end`

### IC-20 — Document and harden the complete Go proof of concept

- **Depends / peers:** `IC-19`; no peer; this is the final integration loop.
- **Objective and scope:** add concise Go/CLI usage documentation and examples,
  finish package/exported API docs and generated directives, audit dependencies
  and generated artifacts, and reconcile every implemented path with
  `README.md` and `SPEC.md`. Correct only final integration defects; a required
  architecture change uses the SPEC escalation loop.
- **Non-goals:** redesign, compatibility promises, performance features, or any
  deferred feature. Do not alter normative protocol meaning.
- **Deliverables:** `GO.md`, package/CLI examples and doc tests as appropriate,
  completed Go comments, and any small tests/fixes required by the final audit.
- **Durable acceptance:** a new user can generate both binding directions and
  construct/bind/wait/close a connection from docs; every exported symbol and
  flag is documented accurately; no stale placeholder/TODO/dead API; only the
  standard library plus the pinned `go/packages` dependency path; checked
  generated fixtures are current and deterministic; no test leaves repository
  files dirty.
- **Worker evidence:** run every command in [Final definition of
  done](#final-definition-of-done).
- **Reviewer:** read the entire public API, docs, generated representative files,
  and relevant README/SPEC sections; independently run every final command and
  return exact `APPROVED` only with zero actionable findings.
- **Commit:** `document and harden the Go proof of concept`

## Final definition of done

All 20 task commits are separately approved, globally green, and integrated in
DAG order. The final `IC-20` Worker and Reviewer initialize their role-specific
`fuzz_gate` from [Validation suites](#validation-suites). The Reviewer owns this
complete gate and records exact results:

```bash
test "$(/usr/local/go/bin/go env GOVERSION)" = go1.26.5
/usr/local/go/bin/go test -count=1 ./...
/usr/local/go/bin/go test -race -count=1 ./...
/usr/local/go/bin/go vet ./...
/usr/local/go/bin/go mod tidy -diff
test -z "$(find . -type f -name '*.go' -not -path './.git/*' -print0 | xargs -0 -r /usr/local/go/bin/gofmt -l)"
fuzz_gate /usr/local/go/bin/go test -run '^$' -fuzz '^FuzzParse$' -fuzztime=1000x ./internal/syntax
fuzz_gate /usr/local/go/bin/go test -run '^$' -fuzz '^FuzzParseFormat$' -fuzztime=1000x ./internal/syntax
fuzz_gate /usr/local/go/bin/go test -run '^$' -fuzz '^FuzzReadFrame$' -fuzztime=1000x .
fuzz_gate /usr/local/go/bin/go test -run '^$' -fuzz '^FuzzGeneratedDecoder$' -fuzztime=1000x ./internal/tool
fuzz_gate /usr/local/go/bin/go test -run '^$' -fuzz '^FuzzImportResponseDecoder$' -fuzztime=1000x ./internal/tool
fuzz_gate /usr/local/go/bin/go test -run '^$' -fuzz '^FuzzExportRequestDecoder$' -fuzztime=1000x ./internal/tool
/usr/local/go/bin/go test ./internal/tool -run '^(TestArtifactDeterminism|TestArtifactFilesystemSafety|TestImportGenerationDeterminism|TestExportGenerationDeterminism|TestCommandDeterminism)$' -count=1
/usr/local/go/bin/go test ./internal/integration -run '^(TestGeneratedFixtureModules|TestCheckedInGeneratedFixturesAreCurrent|TestGeneratedOutputDeterminism)$' -count=1
/usr/local/go/bin/go test -race ./internal/integration -run '^(TestBidirectional|TestNested|TestConcurrent|TestCancellation|TestMalformed|TestShutdown)$' -count=20
task_snapshot_check
```

The Reviewer also confirms that temporary regeneration leaves every checked
fixture byte-identical and that `task_status` contains only the reviewed
`IC-20` diff. After the orchestrator administratively commits that unchanged
approved diff and integrates it, the integration checkout must report empty
`task_status`.

Completion additionally requires an explicit reconciliation statement in the
final Reviewer evidence:

- interface grammar, keys, value/frame encodings, malformed-input handling, and
  unmatched-response behavior agree with `README.md`;
- Go mapping, generation, runtime lifecycle/concurrency, CLI diagnostics, and
  artifact ownership agree with every nondeferred `SPEC.md` section;
- generated Go/interface bytes are deterministic and compile in fixture
  modules; and
- no WebSocket/TypeScript/resource-limit/handshake or other deferred mechanism
  entered the implementation or public API.
