# Go Review Remediation Plan

## Purpose and authority

This plan schedules the remediation of the static review performed against
commit `bcb3a7a119e1090e188aff4ca21de8adb17f6097`. It replaces the completed
proof-of-concept construction plan; it does not reopen that implementation or
turn review notes into protocol authority.

Authorities, in order:

1. [`README.md`](../README.md) defines the InterCall language and wire protocol.
2. [`SPEC.md`](SPEC.md) defines the Go architecture, mapping, generator, CLI,
   runtime, and artifact contracts.
3. This file defines remediation scope, dependencies, acceptance, validation,
   and commit subjects.
4. [`AGENTS.md`](AGENTS.md) defines repository-wide roles and engineering rules.
5. [`ORCHESTRADOR.md`](ORCHESTRADOR.md) is the operational runbook.

The review finding IDs are traceability labels, not a new source of authority.
Every fix must be justified by `README.md` and the current, integrated
`SPEC.md`. `RM-00` resolves the few specification ambiguities before dependent
implementation work begins. If its approved wording differs from the intent
summarized here, the integrated `SPEC.md` wording controls and this plan must be
amended before implementation.

The module is `github.com/cerasos/intercall/go`. The required toolchain is Go
1.26.5 at `/usr/local/go/bin/go`; never rely on `go` from `PATH`. The Lua
validator remains optional and non-normative.

## Scope and finding disposition

### Scheduled findings

| Task | Review findings | Remediation theme |
| --- | --- | --- |
| `RM-01` | FA3-01 / FA2-02, FA2-03 | bounded frame buffering and accurate write failures |
| `RM-02` | FA3-02 / FB-01, FA1-02, FA1-04, FA1-06 / FA2-04 / FA3-05 | live write admission and prompt terminal publication |
| `RM-03` | FA1-03 / FA2-01 / FA3-03, FA3-04, FB-02 | ordered incoming-ID reuse and no post-terminal admission |
| `RM-04` | FA4-01 | stack-safe syntax processing without narrowing the grammar |
| `RM-05` | FA4-02, FA4-04, FB-09 | one physical-line model and correct type-document anchors |
| `RM-06` | FA6-01, FA6-07, FA6-12 | source identity, physical positions, and logical paths |
| `RM-07` | FA6-03, FA6-04, FA6-06, FA6-11 | source directives, declaration groups, and Go exportness |
| `RM-08` | FA6-05, FA6-10, FA6-11 | selector and procedure-signature correctness |
| `RM-09` | FA6-02, FB-04 | complete package-pattern and output-package validation |
| `RM-10` | FB-07 | eager validation of trusted generated metadata tables |
| `RM-11` | FA4-01 downstream hardening | safe native Go projection-depth enforcement |
| `RM-12` | FB-03 (namespace layer) | complete generated package and local namespaces |
| `RM-13` | FB-03 (validation layer) | staged generated-Go type checking before mutation |
| `RM-14` | FB-05 | canonical interface ownership validation |

`RM-00` is the specification prerequisite for the scheduled work. `RM-15` is
the join, documentation, generated-fixture reconciliation, and final audit.

### Findings deliberately not scheduled

These dispositions are intentional scope decisions, not hidden residual risks:

| Findings | Disposition |
| --- | --- |
| FA1-01 | A context whose `Done` is closed while `Err` is nil violates the standard `context.Context` contract; the standard library itself may panic on it. |
| FA1-05 | Encoder panics indicate defective generated code, occur before call admission, and leave runtime state unchanged. No recovery behavior is promised. |
| FA1-07 | Panic containment for a transport implementation that panics from `Close` is not part of the `ByteStream` contract. |
| FA2-05 | The private frame builder receives production IDs and materialized payloads that already satisfy its internal preconditions; the reported values are unreachable. |
| FB-06 | The constructor promises rejection of a nil interface, not reflective detection of every interface containing a typed-nil implementation. Concrete methods remain responsible for their interface contracts. |
| FB-08 | The API returns and documents `*Connection`; copying the mutex-bearing value after use is unsupported Go misuse and is already visible to copylock analysis. |
| FA4-03 | A second grammar implementation would duplicate the parser. Durable grammar tables, invariant fuzzing, fixtures, and the optional Lua differential remain the chosen strategy. |
| FA6-08 | The diagnostics contract sorts a phase that produces several diagnostics; it does not require every phase to recover and aggregate independent errors. This UX enhancement is not correctness remediation. |
| FA6-09 | The exact first-line InterCall generated marker is intentionally the trust boundary. A BOM or leading blank line means that exact marker is absent. |

A Worker must not implement a skipped item opportunistically. New evidence that
one is reachable under a valid contract requires a separately approved plan or
specification amendment.

## Mandatory loop protocol

### Roles and isolation

- **Orchestrator:** schedules and administers only. It creates branches and
  worktrees, starts and resumes sessions, relays complete handoffs, commits an
  unchanged approved snapshot, integrates in DAG order, and records factual
  progress. It does not edit implementation, resolve conflicts, assess code,
  or run technical validation. Its only content-edit exception is the Progress
  checklist after integration.
- **Worker:** one persistent read/write session per task. It implements only the
  exact task, adds durable tests/fixtures/docs, runs all Worker evidence, and
  never commits or edits Progress.
- **Reviewer:** one separate persistent read-only session per task. It reviews
  the complete snapshot and governing contracts, independently runs every gate,
  and alone grants technical approval.

An initial task uses branch `task/<slug>` and worktree
`../InterCall-worktrees/<slug>`, with the exact slug from the DAG. A conflict or
stale-assumption replay uses `task/<slug>-replay-N` and the matching worktree as
specified by `ORCHESTRADOR.md`. Every task generation starts from an integrated
commit containing every dependency. Safe peers may run concurrently only in
separate worktrees. All `internal/tool` implementation tasks are serial because
they share package invariants and fixtures.

Keep the same Worker and Reviewer sessions through every revision. A plain
`git diff` is not a review snapshot: both roles must inventory and render
tracked, staged, deleted, and nonignored untracked paths with the helpers below.
Any content change after approval makes that approval stale.

### Worker handoff

```text
TASK: RM-NN <exact title>
BASE: <dependency-complete integrated commit>
BRANCH/WORKTREE: <actual initial-or-replay branch> | <absolute path>
SUMMARY: <implemented behavior; no claims beyond evidence>
FILES: <all added/modified/deleted paths; no unrelated paths>
DURABLE TESTS: <names and the contract each test proves>
EVIDENCE:
- <exact command>: PASS/FAIL (<exit status and salient result>)
- <exact command>: PASS/FAIL/SKIP (<SKIP only where this plan permits it>)
ORACLE: <not applicable, result, or allowed Lua/LPeg unavailability>
STATUS/DIFF: <task_status and complete task_snapshot_diff summary>
SNAPSHOT CHECK: <task_snapshot_check result>
BLOCKERS/QUESTIONS: <none, or an escalation>
```

A Worker does not hand off a known task-scope failure as residual risk and does
not add evidence reports, logs, binaries, coverage, fuzz cache, or temporary
output to the repository.

### Reviewer handoff

With findings:

```text
TASK: RM-NN <exact title>
BASE/DIFF REVIEWED: <base; every path including untracked files>
FINDINGS:
1. [BLOCKER|MAJOR|MINOR] <path:line> — <actionable defect>
   Evidence: <trigger and violated README/SPEC/task contract>
   Required change: <smallest correction>
VALIDATION:
- <independently run exact command>: PASS/FAIL (<salient result>)
SNAPSHOT: <task_status; complete task_snapshot_diff reviewed;
           task_snapshot_check; unchanged status around reviewer_fuzz>
RESIDUAL RISKS: <only an explicit out-of-scope constraint, otherwise none>
```

With no actionable finding, the first line is exactly:

```text
APPROVED
```

It is followed by the task, base, complete diff, independent command evidence,
snapshot facts, and `RESIDUAL RISKS: none` unless this plan names an explicit
constraint. A findings handoff must not contain the word `APPROVED`.

### Revision, commit, and integration

1. Relay findings verbatim to the same Worker.
2. The Worker revises and returns a complete new handoff after rerunning all
   Worker commands.
3. The same Reviewer rereads the complete current snapshot and reruns all gates.
4. After exact approval, freeze file content. The Orchestrator stages and
   commits exactly that snapshot with the task's exact subject.
5. Integrate only after dependencies, using ascending task ID for ready ties.
   A conflict or invalidated assumption returns to the same Worker and Reviewer;
   the Orchestrator never chooses a technical resolution.
6. Only an integrated task receives `[x]` and its integrated hash in Progress.
   Batch progress updates for parallel siblings into one administrative commit.

### Escalation

Stop and escalate an authority conflict, impossible acceptance criterion,
unrelated dirty path, required scope expansion, pre-existing blocker, or
specification defect. A specification defect uses a dedicated amendment and
review loop before implementation. A required validation tool failure blocks
approval; only the explicitly optional Lua/LPeg oracle may be skipped. A lost
session is resumed if possible; a replacement Reviewer must start the complete
review and all gates from the beginning.

## Validation helpers and suites

Run Go commands from `go/` in the task worktree. Snapshot helpers are Git-based
and may run anywhere in the worktree.

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
    prefix=$(git rev-parse --show-prefix) || exit
    before=$(GIT_OPTIONAL_LOCKS=0 git -C "$top" status --short --untracked-files=all) || exit
    tmp=$(mktemp -d) || exit
    trap 'rm -rf "$tmp"' EXIT HUP INT TERM
    tar -C "$top" --exclude='./.git' -cf - . | tar -C "$tmp" -xf - || exit
    (cd "$tmp/$prefix" && "$@")
    rc=$?
    after=$(GIT_OPTIONAL_LOCKS=0 git -C "$top" status --short --untracked-files=all) || exit
    if [ "$before" != "$after" ]; then
        printf '%s\n' 'reviewer fuzz changed the original worktree' >&2
        exit 1
    fi
    exit "$rc"
)
```

Before snapshotting, compare `task_status` with the declared task paths.
`task_snapshot` uses a temporary index and object directory, so it includes the
complete worktree without mutating the real index or repository objects.

For each fuzz command the Worker defines `fuzz_gate() { "$@"; }`. The Reviewer
defines `fuzz_gate() { reviewer_fuzz "$@"; }`; the helper preserves the caller's
worktree-relative directory inside its disposable repository copy. The Reviewer
records original status before and after. Reviewer test commands always use the exact positive `-count` below;
Worker evidence never substitutes for independent execution.

**`G` — global baseline**

```bash
test "$(/usr/local/go/bin/go env GOVERSION)" = go1.26.5
/usr/local/go/bin/go test -count=1 ./...
/usr/local/go/bin/go vet ./...
task_snapshot_check
test -z "$(find . -type f -name '*.go' -not -path './.git/*' -print0 | xargs -0 -r /usr/local/go/bin/gofmt -l)"
```

**`R` — runtime/concurrency baseline:** run `G`, then:

```bash
/usr/local/go/bin/go test -race -count=1 ./...
```

**`T` — tooling baseline:** run `G`, then:

```bash
/usr/local/go/bin/go mod tidy -diff
```

A focused command supplements rather than replaces its baseline. Tests must use
channel/barrier synchronization rather than sleeps for correctness. Timeout
flags are failure bounds, not synchronization.

## Dependency DAG and scheduling

```text
RM-00
  ├─ RM-01 → RM-02 → RM-03 ───────────────────────────────┐
  ├─ RM-04 → RM-05 ───────────────────────────────────────┼─ RM-15
  └─ RM-06 → RM-07 → RM-08 → RM-09 → RM-10 → RM-11 → RM-12 → RM-13 → RM-14 ─┘
```

In addition to the drawn serial edges, `RM-11` has the cross-track dependency
`RM-04`; the dependency table below is the complete DAG.

| ID | Branch/worktree slug | Dependencies | Safe parallel peers |
| --- | --- | --- | --- |
| `RM-00` | `rm-00-spec-contracts` | reviewed baseline and integrated planning documents | none |
| `RM-01` | `rm-01-frame-bounds` | `RM-00` | `RM-04`, `RM-06` |
| `RM-02` | `rm-02-write-liveness` | `RM-01` | active syntax/tool track task |
| `RM-03` | `rm-03-receive-ordering` | `RM-02` | active syntax/tool track task |
| `RM-04` | `rm-04-syntax-stack` | `RM-00` | `RM-01`, `RM-06` |
| `RM-05` | `rm-05-syntax-docs` | `RM-04` | active runtime/tool track task |
| `RM-06` | `rm-06-source-positions` | `RM-00` | `RM-01`, `RM-04` |
| `RM-07` | `rm-07-directives` | `RM-06` | active runtime/syntax track task |
| `RM-08` | `rm-08-selection` | `RM-07` | active runtime/syntax track task |
| `RM-09` | `rm-09-discovery` | `RM-08` | active runtime/syntax track task |
| `RM-10` | `rm-10-metadata` | `RM-09` | active runtime/syntax track task |
| `RM-11` | `rm-11-projection-depth` | `RM-04`, `RM-10` | active runtime task; `RM-05` may remain active |
| `RM-12` | `rm-12-generated-names` | `RM-11` | active runtime/syntax track task |
| `RM-13` | `rm-13-generated-typecheck` | `RM-12` | active runtime/syntax track task |
| `RM-14` | `rm-14-artifact-canonicality` | `RM-13` | active runtime/syntax track task |
| `RM-15` | `rm-15-final-audit` | `RM-03`, `RM-05`, `RM-14` | none |

Only listed peers are predeclared safe. Tasks in a track remain serial even if
individual files appear disjoint.

## Progress

`[ ]` means pending; `[x]` means integrated. Append the integrated commit hash.
Approval without integration is not completion. Workers never edit this list.
The integration commit containing this plan and `ORCHESTRADOR.md` is an
administrative prerequisite and must be cleanly available before `RM-00` starts.

Baseline:

- [x] reviewed implementation baseline — `bcb3a7a119e1090e188aff4ca21de8adb17f6097`

Remediation tasks:

- [x] [RM-00 — Clarify remediation contracts in the Go specification](#rm-00--clarify-remediation-contracts-in-the-go-specification) — `c7669e7422c083213aa3e7ad8e56b08241c7c1a3`
- [x] [RM-01 — Bound frame buffering and correct write diagnostics](#rm-01--bound-frame-buffering-and-correct-write-diagnostics) — `c21659fb28fd2bb8728a554a1b8826b1f8bd7038`
- [ ] [RM-02 — Make write admission and terminal teardown live](#rm-02--make-write-admission-and-terminal-teardown-live)
- [ ] [RM-03 — Order incoming request admission and ID reuse](#rm-03--order-incoming-request-admission-and-id-reuse)
- [x] [RM-04 — Make syntax processing stack-safe](#rm-04--make-syntax-processing-stack-safe) — `de9f7aee84831c44059302374b77969b0a08b892`
- [ ] [RM-05 — Correct semantic-document line and anchor handling](#rm-05--correct-semantic-document-line-and-anchor-handling)
- [x] [RM-06 — Preserve exact Go source identity and positions](#rm-06--preserve-exact-go-source-identity-and-positions) — `b3775bb96862c848c5eaaee304020eb7b941cd35`
- [ ] [RM-07 — Correct source directives and declaration handling](#rm-07--correct-source-directives-and-declaration-handling)
- [ ] [RM-08 — Correct selectors and procedure signatures](#rm-08--correct-selectors-and-procedure-signatures)
- [ ] [RM-09 — Validate every package operand and output package](#rm-09--validate-every-package-operand-and-output-package)
- [ ] [RM-10 — Validate complete generated metadata tables](#rm-10--validate-complete-generated-metadata-tables)
- [ ] [RM-11 — Enforce safe native Go projection depth](#rm-11--enforce-safe-native-go-projection-depth)
- [ ] [RM-12 — Reserve complete generated namespaces](#rm-12--reserve-complete-generated-namespaces)
- [ ] [RM-13 — Type-check generated bindings before mutation](#rm-13--type-check-generated-bindings-before-mutation)
- [ ] [RM-14 — Require canonical owned interface bodies](#rm-14--require-canonical-owned-interface-bodies)
- [ ] [RM-15 — Reconcile documentation, fixtures, and final remediation](#rm-15--reconcile-documentation-fixtures-and-final-remediation)

## Executable tasks

### RM-00 — Clarify remediation contracts in the Go specification

- **Depends / peers:** planning documents integrated; no peer.
- **Scope:** amend `SPEC.md` only where the scheduled findings expose ambiguity
  or an impossible promise. Preserve `README.md`; this task must not edit Go
  implementation.
- **Required decisions:**
  1. Set the Go runtime's maximum accepted frame payload to exactly 64 MiB
     (`67,108,864` bytes), checked after the header and before conversion or
     allocation. Larger frames are terminal `ErrProtocol`; the runtime need not
     consume their payload. Define this as a mandatory implementation-safety
     bound, not configurable policy, and reconcile the current “no limits” and
     deferred-feature wording.
  2. Require write-gate waits before admission to observe terminal selection
     and, for calls, `ctx.Done`; prohibit holding the state lock while waiting
     for the gate or calling stream `Write`/`Close`. Terminal publication under
     the state lock fixes the cause and transfers every existing pending entry
     away from later response/cancellation claims. Delivery of those terminal
     completions and transport cleanup may be asynchronous. `Close` returns
     after publication while `Wait` waits for complete cleanup.
  3. Order request admission against terminal publication and prohibit provider
     launch after terminal wins. Preserve README legality of request-ID reuse as
     soon as the peer has received the complete prior response, while recognizing
     that `ByteStream` exposes no peer-delivery acknowledgement. A duplicate
     observed before the prior response enters write admission is terminal. A
     duplicate observed during that write is fully buffered and reserved as one
     deferred next generation without parking the sole receive loop; another
     same-ID request cannot also queue. Write success admits the deferred request
     even if it arrived before the final local `Write` return; write failure or
     terminal selection discards it. The runtime is not required to detect a
     peer's early reuse once the prior response write is active.
  4. Require the interface parser, validator, documentation attachment, and
     formatter to use Go call-stack space independent of unrestricted type
     nesting; do not add a language grammar limit. Separately define the strict
     Go projection's maximum resolved type depth as exactly 4,096 occurrences.
     A root is a type underlying, exception payload, procedure parameter, or
     return and has depth 1. Each list-element, record-field,
     named-reference-to-underlying, defined-type-to-underlying, or alias-expansion
     edge adds 1. The preflight is iterative and cycle-safe; existing recursive
     graph diagnostics still own cycles. Import and export reject the first
     source occurrence exceeding 4,096 with a normal physical diagnostic before
     recursive mapping or emission. This is a native representability boundary,
     not a protocol grammar or configurable resource policy.
  5. Define physical source lines for semantic attachment by LF bytes: CRLF has
     one LF terminator; bare CR is whitespace, not a physical terminator.
     Documentation-body normalization still converts CRLF and bare CR to LF.
     State that a candidate type prefix (`type` name, exception name,
     parameter/field name, `list`, or procedure `}`) makes an intervening
     comment eligible for that type even after an earlier same-line node.
  6. Define declaration-group directives as applying to exactly one declared
     object; a group-level InterCall declaration directive on a group containing
     multiple specs or one spec declaring multiple objects is an error.
     Parentheses around a legal named struct do not change exception eligibility.
     Go source identifiers, selector symbols, directive references, field
     eligibility, and exportness follow Go's Unicode-aware lexical rules. The
     existing default Go/wire projection remains ASCII-only, so an explicit wire
     directive or field tag is required where projection cannot represent the
     source name.
  7. Make ordinary third-party files recognized by Go's standard generated
     marker inert for source directive selection. The exact InterCall marker
     remains the metadata trust boundary. On first use of a table-backed type,
     validate the complete marked file. A row is a top-level type spec carrying
     exactly one valid `@intercall type <wire-name>` machine line; rows must be
     in bijection with decoded semantic type declarations, while generated
     helper and exception types without a machine line are permitted. Reject
     every malformed, unknown, misplaced, duplicate, missing, extra, or
     structurally conflicting row, including an otherwise unreached row.
     Directive-like decoded documentation remains inert.
  8. Define Go diagnostic logical paths. A physical source under the package
     directory uses the slash-normalized package-relative path. An external
     compiler-generated source uses
     `<import-path>/.intercall-generated/<base-name>`; duplicate external base
     names are a package-load invariant error rather than silently conflated.
     Position parsing accepts both `file:line` and `file:line:column`, scans
     numeric suffixes from the right so colons in filenames are preserved, and
     defaults a missing column to 1.
  9. Require generated Go to have complete collision-free package/local scopes
     and to be parsed and type-checked in memory before filesystem mutation.
     Export checking reuses the exact `*types.Package` identities from the one
     combined discovery load. Import checking may use one synthetic runtime SPI
     package only when a durable parity test compares every modeled exported
     generated-code bridge object and signature with the actual root package.
     Preserve the rule that only discovery uses `go/packages`.
- **Durable acceptance:** no wording weakens wire framing, exact ID reuse,
  deterministic artifacts, validation-before-mutation, static generation, or
  the no-reflection/no-registration architecture; every changed statement is
  reconciled with all other SPEC occurrences and Deferred Features.
- **Worker evidence:** `G`, plus a repository-wide `rg` audit for every amended
  term (`payload`, `limit`, `write gate`, `Close`, `incoming`, `nesting`,
  `generated`, `type-check`, `line`).
- **Reviewer:** read `README.md` completely and `SPEC.md` completely; reject a
  protocol change, contradictory duplicate wording, unspecified constant, or
  implementation design accidentally elevated beyond the needed invariant;
  independently run `G`.
- **Expected paths:** `SPEC.md` only.
- **Commit:** `clarify Go remediation contracts`

### RM-01 — Bound frame buffering and correct write diagnostics

- **Depends / peers:** `RM-00`; safe with `RM-04` and `RM-06`.
- **Scope:** implement the exact frame payload ceiling before native conversion
  or allocation; preserve full-read semantics for accepted frames. Correct
  `writeFull` byte accounting and impossible-count classification.
- **Durable acceptance:** a 24-byte header declaring 64 MiB is accepted for
  allocation/read; one byte more returns an error wrapping `ErrProtocol` before
  a payload read or allocation; `uint64` extremes cannot panic or OOM; fuzz no
  longer skips the safety boundary. Partial-write diagnostics report cumulative
  progress against original frame size, and an invalid count is classified as
  invalid even when the writer also returns an error.
- **Required tests:** add deterministic `TestReadFramePayloadLimit` cases at
  limit-1/limit/limit+1/`MaxUint64`, a no-payload-read spy, and cumulative
  `writeFull` diagnostics/classification tests; add bounded fuzz seeds around
  the exact limit without allocating a limit-sized fuzz input.
- **Worker evidence:** `R`, then:

  ```bash
  /usr/local/go/bin/go test -race . -run '^(TestReadFramePayloadLimit|TestReadFrameOversizedLengthNoRead|TestWriteFullPartialDiagnostics|TestWriteFullInvalidCount)$' -count=20
  fuzz_gate /usr/local/go/bin/go test -run '^$' -fuzz '^FuzzReadFrame$' -fuzztime=1000x .
  ```

- **Reviewer:** inspect arithmetic and allocation ordering and independently run
  the same commands. The test itself must not materialize attacker-sized data.
- **Expected paths:** `frame.go`, `write.go`, focused root tests/fuzz corpus.
- **Commit:** `bound Go frame payload buffering`

### RM-02 — Make write admission and terminal teardown live

- **Depends / peers:** `RM-01`; safe with the active syntax/tool task.
- **Scope:** replace blocking mutex-only write admission with one interruptible,
  connection-wide gate; never hold connection state while waiting on that gate,
  writing, or closing the stream. Separate permanent terminal publication from
  one asynchronous cleanup owner. Consolidate request and response writing on
  one production gate abstraction and remove the dead divergent write path.
- **Invariants:** no ID or pending entry before call admission; after admission,
  per-call cancellation still cannot interrupt a write; only one writer owns the
  stream. Terminal publication under `mu` fixes the permanent cause and removes
  every existing pending entry from response/cancellation eligibility before
  `Close` returns; completion delivery may follow asynchronously. Stream close
  and teardown happen exactly once; `Close` does not wait for a blocked gate,
  `Write`, or `ByteStream.Close`; `Wait` retains its complete wait contract.
- **Durable acceptance:** deterministic barriers reproduce writer A blocked in
  `Write`, writer B waiting for admission, and concurrent `Close`; terminal is
  published and `Close` returns, stream close releases A, B abandons, and all
  operations settle with the exact cause. While stream `Close` is deliberately
  blocked, cancellation and response race an already pending call; neither can
  claim it after terminal publication, and it receives the permanent cause. A
  call canceled while waiting gets its exact context error without an ID. A
  handler waiting at the gate abandons on terminal. The blocked-close case also
  proves `Close` remains prompt and `Wait` waits. No test uses sleep as
  synchronization.
- **Worker evidence:** `R`, then:

  ```bash
  /usr/local/go/bin/go test -race . -run '^(TestConnectionCloseStalledWriter|TestConnectionCloseBlockedGateWaiter|TestConnectionCloseBlockedStreamClose|TestTerminalPublicationClaimsPendingBeforeCleanup|TestCallGateWaitCancellation|TestHandlerGateWaitTerminal|TestCallGateSerializes|TestPendingAdmissionPointTeardown|TestConnectionCloseNonwaiting)$' -count=50 -timeout=120s
  ```

- **Reviewer:** draw the lock/channel ownership graph, inspect every gate release
  and terminal path, and independently run the same commands. A timeout-only
  test or transport that violates the specified unblock contract is inadequate.
- **Expected paths:** root lifecycle/call/receive/write implementation and tests.
- **Commit:** `make connection writes and teardown live`

### RM-03 — Order incoming request admission and ID reuse

- **Depends / peers:** `RM-02`; safe with the active syntax/tool task.
- **Scope:** make terminal selection and incoming request admission one ordered
  decision. Stop processing a buffered frame when terminal already won. A
  duplicate observed before the prior response enters write admission is
  terminal. A duplicate observed while that response write is active is fully
  buffered and reserved as one deferred next generation without blocking the
  sole reader; a further same-ID request cannot queue. Successful completion
  admits the deferred request regardless of whether it arrived before the final
  local `Write` return; failure discards it under terminal state. Release/drain
  incoming and deferred state on every terminal or handler exit path.
- **Durable acceptance:** a controllable stream exposes complete response bytes
  to the simulated peer while delaying the writer's return; the peer immediately
  sends the reused ID, which is admitted after successful write completion.
  Reuse arriving before response-write admission is terminal. A duplicate
  waiting on a response write that then fails is discarded under the exact
  terminal cause and never dispatches. A matched response placed after the
  deferred duplicate is processed before the prior response writer may return,
  proving the sole receive loop did not park. A third same-ID request while one
  generation is deferred is terminal. `Close` racing with a buffered request
  cannot launch dispatch or a provider after terminal wins. Teardown leaves no
  incoming/deferred entries even when a handler ignores cancellation. Tests are
  deterministic and race-clean.
- **Worker evidence:** `R`, then:

  ```bash
  /usr/local/go/bin/go test -race . -run '^(TestReceiveIncomingReuseAtDelivery|TestReceiveDeferredReuseDoesNotBlockResponses|TestReceiveThirdIncomingGenerationRejected|TestReceiveIncomingDuplicateBeforeWrite|TestReceiveIncomingWriteFailureWithDuplicate|TestReceiveIncomingReuseAfterWrite|TestReceiveStopsAfterTerminal|TestReceiveTerminalDrainsIncoming|TestHandlerTerminalRaces)$' -count=100 -timeout=120s
  /usr/local/go/bin/go test -race ./internal/integration -run '^(TestConcurrent|TestShutdown)$' -count=20 -timeout=120s
  ```

- **Reviewer:** verify the observable linearization points against README's
  receipt-based reuse rule and the lack of a delivery acknowledgement; reject
  merely shrinking the deletion window, parking the sole reader, accepting two
  deferred generations, rejecting an in-write duplicate after eventual success,
  or dispatching it after write failure. Independently run the commands.
- **Expected paths:** root receive/lifecycle/write state and tests; integration
  tests only when needed for the peer-visible regression.
- **Commit:** `order incoming request admission`

### RM-04 — Make syntax processing stack-safe

- **Depends / peers:** `RM-00`; safe with `RM-01` and `RM-06`.
- **Scope:** remove Go-call-stack growth proportional to type nesting from parse,
  validation, documentation attachment, and canonical formatting. Preserve the
  exact unrestricted grammar, AST, positions, diagnostics, attachment, and
  bytes; do not add a nesting limit or giant checked-in fixture.
- **Durable acceptance:** each syntax-owned walk uses an explicit stack or other
  bounded-call-stack algorithm. A subprocess test lowers its maximum Go stack,
  constructs deeply nested lists and records in memory, and successfully runs
  parse, validate, attach, format, and reparse. Malformed deep input returns the
  same class and source position of normal error instead of panic/fatal exit.
  Existing 5,000-level behavior and all goldens remain exact.
- **Worker evidence:** `G`, then:

  ```bash
  /usr/local/go/bin/go test ./internal/syntax -run '^(TestDeepTypeProcessingUsesBoundedStack|TestParseDeepNesting|TestFormatRoundTripProperty)$' -count=1 -timeout=120s
  fuzz_gate /usr/local/go/bin/go test -run '^$' -fuzz '^FuzzParse$' -fuzztime=1000x ./internal/syntax
  fuzz_gate /usr/local/go/bin/go test -run '^$' -fuzz '^FuzzParseFormat$' -fuzztime=1000x ./internal/syntax
  ```

- **Reviewer:** inspect every recursive syntax walk, ensure the subprocess has a
  meaningful low stack and bounded memory, and independently run the commands.
- **Expected paths:** `internal/syntax` implementation and tests only.
- **Commit:** `make InterCall syntax processing stack safe`

### RM-05 — Correct semantic-document line and anchor handling

- **Depends / peers:** `RM-04`; safe with active runtime/tool tasks.
- **Scope:** implement the integrated `RM-00` physical-line and candidate-anchor
  rules consistently in attachment, trailing detection, blank-line grouping,
  and positions. Keep documentation-body normalization separate.
- **Durable acceptance:** LF, CRLF, bare CR, and mixed endings use the specified
  physical lines for positions and attachment; a comment after a prior same-line
  node still attaches when it follows the later declaration/parameter/field/
  exception/list/return prefix. A genuinely trailing comment after a completed
  node remains unattached. Each comment attaches at most once and formatting is
  idempotent.
- **Required regression shape:** include and preserve the type document in
  `type a uint8; type b /* doc */ a;`, plus analogous parameter, field, list
  element, exception payload, and procedure return cases.
- **Worker evidence:** `G`, then:

  ```bash
  /usr/local/go/bin/go test ./internal/syntax -run '^(TestAttachDocsSharedLineTypeAnchors|TestAttachDocsTrailing|TestAttachDocsBlankLines|TestPositionLinesAndColumns|TestFormatGolden|TestFormatRoundTripProperty)$' -count=1
  fuzz_gate /usr/local/go/bin/go test -run '^$' -fuzz '^FuzzParseFormat$' -fuzztime=1000x ./internal/syntax
  ```

- **Reviewer:** derive expected attachments directly from SPEC and independently
  run the same commands.
- **Expected paths:** `internal/syntax` docs/position/format tests and code.
- **Commit:** `correct InterCall documentation anchors`

### RM-06 — Preserve exact Go source identity and positions

- **Depends / peers:** `RM-00`; safe with `RM-01` and `RM-04`.
- **Scope:** eliminate conversion of `token.Pos` values through an unrelated
  `token.FileSet`. Resolve physical positions with the load FileSet while
  retaining canonical import-path-plus-package-relative logical paths. Pair
  syntax/documents by filename or file identity and fail on an invariant break,
  rather than trusting positional slice correspondence. Correct demonstrated
  cgo/generated logical-path fallbacks and colon-bearing path parsing.
- **Durable acceptance:** temporary packages with dependencies and multiple
  files force high FileSet bases; mapping, metadata, named-type, and retained-doc
  diagnostics point to exact physical line and byte column despite `//line`.
  Syntax nodes cannot be attributed to an adjacent file. Ordinary files render
  as `<import-path>/<slash-relative-path>`; external generated files render as
  `<import-path>/.intercall-generated/<base-name>`, and duplicate external base
  names fail rather than conflate. Both `file:line` and
  `file:line:column` preserve colons in filenames and use column 1 when omitted.
  Tests assert these literal logical paths. Recorded
  `GoDecl.Pos`/`NamedType.Pos` are correct.
- **Worker evidence:** `T`, then:

  ```bash
  /usr/local/go/bin/go test ./internal/tool ./cmd/intercall-go -run '^(TestMappingPhysicalPositionsAcrossFileSets|TestNamedTypeRecordedPositionAcrossFileSets|TestSemanticMetadataPositionAcrossFileSets|TestMappedDocumentationPositionAcrossFileSets|TestPackageSyntaxDocumentPairingByFilename|TestPackageErrorLogicalPath|TestPackageErrorExternalGeneratedPath|TestSplitFilePositionForms|TestCommandDiagnostics)$' -count=1
  ```

- **Reviewer:** inspect every `token.Pos`/offset conversion and logical-path
  constructor, then independently run the same commands.
- **Expected paths:** tool source/document/mapping/metadata/discovery diagnostics
  and focused command tests.
- **Commit:** `fix Go source position mapping`

### RM-07 — Correct source directives and declaration handling

- **Depends / peers:** `RM-06`; safe with active runtime/syntax tasks.
- **Scope:** enforce exactly-one-object declaration directives and stop group
  docs from multiplying across specs. Transparently unwrap parenthesized named
  struct exception declarations. Apply full Go identifier and exportness rules
  to source declarations, fields, and directive references while retaining the
  specified ASCII default projection. Make ordinary third-party generated files
  inert without weakening the exact InterCall metadata boundary.
- **Durable acceptance:** one-spec/one-object groups work; multi-spec and
  multi-object groups produce one precise contradiction. Nested parentheses
  around a valid payload exception work end to end. Unicode-exported sentinel
  and type declarations, tagged fields, and parameter names referenced by
  `@intercall param`/`@param` are recognized with explicit valid wire names or
  tags. A standard generated third-party file supplies nothing and ignores
  source-like prose; an exact InterCall-marked file remains metadata input.
- **Worker evidence:** `T`, then:

  ```bash
  /usr/local/go/bin/go test ./internal/tool -run '^(TestDirectiveDeclarationGroups|TestParenthesizedPayloadException|TestUnicodeSourceDeclarations|TestUnicodeTaggedFieldsAndParameters|TestGeneratedDirectiveBoundary|TestDirectives|TestGoDocumentation|TestApplicationExceptionsIntegration|TestTypeMappingIntegration)$' -count=1
  ```

- **Reviewer:** compare declaration ownership, Unicode source semantics, and the
  two generated-file boundaries with SPEC; independently run the commands.
- **Expected paths:** source/document/directive/export/mapping code and tests.
- **Commit:** `fix Go source directive handling`

### RM-08 — Correct selectors and procedure signatures

- **Depends / peers:** `RM-07`; safe with active runtime/syntax tasks.
- **Scope:** use Go's full identifier grammar for source selector symbols.
  Reject a data result whose type is the predeclared `error` interface during
  signature selection, including aliases. Reject a generic function named by
  either include or exclude; retain exclusion precedence only for eligible
  nongeneric functions.
- **Durable acceptance:** include/exclude selectors can name Unicode-exported
  functions that carry explicit wire names. Malformed or unknown Unicode
  selectors still diagnose precisely. `(error, error)`, aliases to error, and
  named data-result forms fail in the signature phase. Generic includes and
  excludes both fail as generic selectors; valid exclusion precedence remains.
- **Worker evidence:** `T`, then:

  ```bash
  /usr/local/go/bin/go test ./internal/tool -run '^(TestUnicodeProcedureSelectors|TestProcedureDataResultCannotBeError|TestGenericFilterSelectorsAreErrors|TestProcedureSelection|TestProcedureSignatures)$' -count=1
  ```

- **Reviewer:** trace filter grammar, Go object lookup, and signature identity;
  independently run the commands.
- **Expected paths:** selector/selection/name code and tests.
- **Commit:** `fix Go selector and signature validation`

### RM-09 — Validate every package operand and output package

- **Depends / peers:** `RM-08`; safe with active runtime/syntax tasks.
- **Scope:** account for every export package operand while retaining one
  authoritative combined type universe. Reject each unmatched pattern even when
  another matches. Load enough syntax/type information to reject an existing
  output package with syntax or type errors; preserve valid existing and fresh
  output behavior.
- **Durable acceptance:** valid plus unmatched wildcard fails and names the
  unmatched operand; overlaps still deduplicate; module and workspace behavior
  agree. Existing output with undefined identifiers or syntax errors is not
  importable and causes no output mutation. A valid existing output package and
  a fresh output directory still succeed. Pattern accounting must not mix
  `go/types` objects from independent loads.
- **Worker evidence:** `T`, then:

  ```bash
  /usr/local/go/bin/go test ./internal/tool ./cmd/intercall-go -run '^(TestPackagePatternAccounting|TestEveryPatternMustMatch|TestOutputPackageTypeChecking|TestPackageDiscovery|TestPackageDiscoveryWorkspace|TestOutputFreshDirectories|TestCLIExportOutputPackageTypeError)$' -count=1
  ```

- **Reviewer:** inspect `go/packages` modes and per-pattern accounting, verify
  validation precedes mutation, and independently run the same commands.
- **Expected paths:** discovery code/tests and focused CLI mutation tests.
- **Commit:** `validate Go package discovery completely`

### RM-10 — Validate complete generated metadata tables

- **Depends / peers:** `RM-09`; safe with active runtime/syntax tasks.
- **Scope:** on first use of a table-backed type from an exact InterCall-marked
  file, validate every machine directive and table row in that file. Rows are
  top-level type specs with exactly one valid type machine line and must be in
  bijection with decoded semantic type declarations. Generated helper and
  exception types without machine lines are allowed. Preserve decoded docs as
  inert text.
- **Durable acceptance:** malformed/unknown/misplaced/missing/duplicate machine
  lines, duplicate or unknown wire names, missing/extra rows, and an unreached
  row whose structure conflicts with semantic metadata all fail at the exact
  physical position. Valid helpers, exceptions, and unrelated rows recover.
  Directive-like text inside decoded docs remains inert. Complete validation
  terminates on adversarial tables and preserves canonical metadata.
- **Worker evidence:** `T`, then:

  ```bash
  /usr/local/go/bin/go test ./internal/tool -run '^(TestSemanticMachineTableValidation|TestSemanticMetadata|TestSemanticMetadataIntegration|TestTypeMapping)$' -count=1
  ```

- **Reviewer:** audit marker entry, row classification, bijection, eager
  structure checks, and `RM-06` positions; independently run the commands.
- **Expected paths:** metadata/mapping code and tests.
- **Commit:** `validate generated metadata tables`

### RM-11 — Enforce safe native Go projection depth

- **Depends / peers:** `RM-04`, `RM-10`; safe with the active runtime task and
  with `RM-05` if syntax documentation work remains active.
- **Scope:** before any recursive mapping, metadata projection/comparison,
  override/naming, model, codec, or import/export emission, iteratively check the
  integrated SPEC's exact 4,096-occurrence native Go projection ceiling. Import
  reports the first over-limit interface occurrence; export reports the first
  over-limit physical Go or trusted semantic occurrence. Preserve unrestricted
  syntax-only parse/validate/attach/format behavior from `RM-04`.
- **Durable acceptance:** complete import and export generation succeed at depth
  4,096 under a deliberately low but adequate subprocess stack, including lists,
  records, import named-type chains, export defined-type and alias chains,
  codec/model construction, dispatch/signature emission, and trusted metadata
  cross-row named chains. Each shape at depth 4,097 is rejected before recursive
  processing or Go formatting/parsing with a stable physical source diagnostic.
  Cycles retain their existing recursive-graph error. Malformed deep input
  returns an ordinary error. The boundary does not alter shallower bytes or
  generation determinism.
- **Worker evidence:** `T`, then:

  ```bash
  /usr/local/go/bin/go test ./internal/tool -run '^(TestGoProjectionDepthLimit|TestDeepImportProjectionBoundary|TestDeepImportNamedChainBoundary|TestDeepExportProjectionBoundary|TestDeepExportNamedAliasChainBoundary|TestDeepMetadataNamedChainBoundary|TestImportGenerationDeterminism|TestExportGenerationDeterminism|TestSemanticMetadataIntegration|TestTypeMapping)$' -count=1 -timeout=180s
  fuzz_gate /usr/local/go/bin/go test -run '^$' -fuzz '^FuzzGeneratedDecoder$' -fuzztime=1000x ./internal/tool
  ```

- **Reviewer:** verify depth counting and preflight coverage before every
  recursive tool path, inspect both low-stack subprocesses and source positions,
  and independently run the commands.
- **Expected paths:** type-depth preflight and type-directed `internal/tool`
  tests/code; fixtures only through their owner if bytes necessarily change.
- **Commit:** `bound native Go type projection depth`

### RM-12 — Reserve complete generated namespaces

- **Depends / peers:** `RM-11`; safe with active runtime/syntax tasks.
- **Scope:** reserve the complete generated package, type-member, import, alias,
  parameter, and local namespaces before emission. Cover mandatory accessors,
  exception `Error` methods/fields, all private generated helpers and locals,
  and provider package names. Public/parameter collisions are errors; imports
  and private helpers use deterministic mangling only where SPEC permits it.
- **Required regressions:** reject `procedure import_binding {};`; reject a
  `--go-name` parameter equal to every generated private parameter/local;
  reject an exception field colliding with generated `Error`; deterministically
  alias a provider package named `ExportBinding` and provider package names that
  collide with helpers. Validate both import and export model scopes.
- **Durable acceptance:** every user-triggerable namespace failure is diagnosed
  before emission or mutation; valid names and aliases remain deterministic.
  CLI tests cover fresh outputs and existing owned import/export targets,
  asserting no directory creation for fresh failures and unchanged bytes,
  inodes, and temp-file inventory for existing targets.
- **Worker evidence:** `T`, then:

  ```bash
  /usr/local/go/bin/go test ./internal/tool ./cmd/intercall-go -run '^(TestGeneratedNamespaceValidation|TestGeneratedImportAliases|TestGeneratedExportAliases|TestCLIGeneratedNamespaceFailureNoMutation|TestImportGenerationDeterminism|TestExportGenerationDeterminism)$' -count=1
  ```

- **Reviewer:** enumerate every emitted scope against the reservation model and
  independently run the no-mutation/determinism commands.
- **Expected paths:** generation model/override/name/import/export code and tests.
- **Commit:** `reserve generated Go namespaces`

### RM-13 — Type-check generated bindings before mutation

- **Depends / peers:** `RM-12`; safe with active runtime/syntax tasks.
- **Scope:** after the integrated 4,096-depth projection preflight, parse and
  `go/types`-check complete generated import and export source in memory before
  artifact mutation. Export checking reuses exact provider/type package
  identities from the combined discovery load. Import
  checking uses one synthetic root-runtime SPI model with a durable parity test
  against every actual exported bridge object/signature. Standard-library
  imports retain correct identity. Do not use subprocess compilation in
  production or `go/packages` outside discovery.
- **Required regressions:** checker probes reject duplicate declarations and
  parameters, undefined names, import/declaration collisions, wrong runtime SPI
  calls, and body-level type errors. A loaded provider whose signature uses
  `context.Context` and reachable cross-package named types proves identity is
  preserved. Diagnostics expose no absolute or staging path.
- **Durable acceptance:** valid import/export fixtures type-check, compile, run,
  and remain deterministic. Under a bounded subprocess stack, actual production
  checking accepts depth-4,096 import and export output for structural,
  named/alias, and trusted-metadata chains; depth 4,097 is rejected by the
  preflight before checker entry. Import/export CLI tests cover fresh and
  existing owned targets for checker failures, asserting no fresh directory and
  unchanged bytes, inodes, and temp inventory. No generated content reaches
  artifact staging without a successful checker result.
- **Worker evidence:** `T`, then:

  ```bash
  /usr/local/go/bin/go test ./internal/tool ./cmd/intercall-go -run '^(TestGeneratedGoTypeChecking|TestRuntimeSPIModelParity|TestGeneratedCheckerLoadedTypeIdentity|TestGeneratedImportCheckerProjectionBoundary|TestGeneratedExportCheckerProjectionBoundary|TestImportGeneratedFixtureCompiles|TestExportGeneratedFixtureCompiles|TestCLIGeneratedTypeFailureNoMutation|TestImportGenerationDeterminism|TestExportGenerationDeterminism|TestArtifactFilesystemSafety)$' -count=1 -timeout=180s
  /usr/local/go/bin/go test ./internal/integration -run '^(TestGeneratedFixtureModules|TestCheckedInGeneratedFixturesAreCurrent|TestGeneratedOutputDeterminism)$' -count=1
  ```

- **Reviewer:** inspect importer/package identity and complete function-body
  checking, compare the synthetic SPI object-by-object with the root package,
  compile fixtures independently, and verify validation precedes all mutation.
- **Expected paths:** generated checker/importer and generator/artifact handoff,
  tests, and generator-owned fixtures only through regeneration.
- **Commit:** `type-check generated Go bindings`

### RM-14 — Require canonical owned interface bodies

- **Depends / peers:** `RM-13`; safe with active runtime/syntax tasks.
- **Scope:** an existing interface is replaceable as owned only when marker and
  lowercase digest are valid, digest matches body, body parses and validates,
  and canonical formatting reproduces the body byte for byte. Keep ownership
  structural: a different canonical body with its correct public digest remains
  another valid owned artifact and is accepted.
- **Durable acceptance:** matching recomputed digests over noncanonical spacing
  or docs, syntax-invalid bodies, and semantically invalid bodies are rejected
  before staging or replacement. Empty and nonempty canonical bodies pass.
  Rejection preserves both target bytes and inodes and leaves no temp files.
  Interrupted repair with canonical older targets still succeeds.
- **Worker evidence:** `T`, then:

  ```bash
  /usr/local/go/bin/go test ./internal/tool ./cmd/intercall-go -run '^(TestParseInterfaceOwnershipRequiresCanonicalBody|TestArtifactRejectsNoncanonicalOwnedInterface|TestParseInterfaceOwnership|TestArtifactDeterminism|TestArtifactFilesystemSafety|TestArtifactInterruptedExportRepair|TestCLI)$' -count=1
  ```

- **Reviewer:** distinguish canonicality from unauthenticated provenance, inspect
  every mutation boundary, and independently run the commands.
- **Expected paths:** artifact ownership/writer code and filesystem/CLI tests.
- **Commit:** `validate canonical interface ownership`

### RM-15 — Reconcile documentation, fixtures, and final remediation

- **Depends / peers:** `RM-03`, `RM-05`, `RM-14`; no peer.
- **Scope:** audit the complete integrated remediation against `README.md`,
  `SPEC.md`, scheduled/skipped dispositions, public Go docs, CLI help, and
  generated fixtures. Document the exact 64 MiB frame ceiling and prompt
  `Close`/complete `Wait` behavior. Regenerate and byte-compare owned fixtures
  through their generators; never hand-edit them. Correct only small integration
  defects within approved contracts; architecture or scope changes escalate.
- **Durable acceptance:** every scheduled finding has a durable regression or
  specification-enforced structural proof; no skipped finding entered by scope
  creep; generated output compiles and is current; docs and behavior agree; no
  dead alternative write path or stale claim remains; validation leaves no
  repository output.
- **Worker evidence:** every command in Final Definition of Done.
- **Reviewer:** read the public runtime, changed SPEC/GO docs, representative
  generated output, and every remediation diff since baseline. Independently
  run the complete final gate and reconcile every scheduled finding family.
- **Expected paths:** `GO.md`, package/API docs, generator-owned fixtures through
  regeneration, and small audit-proven integration fixes/tests only.
- **Commit:** `document and audit review remediation`

## Final definition of done

All tasks are separately approved and integrated in DAG order. For `RM-15`, the
Worker and Reviewer initialize their role-specific `fuzz_gate` and run from
`go/`:

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
/usr/local/go/bin/go test -race . -run '^(TestConnectionCloseStalledWriter|TestConnectionCloseBlockedGateWaiter|TestConnectionCloseBlockedStreamClose|TestTerminalPublicationClaimsPendingBeforeCleanup|TestReceiveIncomingReuseAtDelivery|TestReceiveDeferredReuseDoesNotBlockResponses|TestReceiveIncomingWriteFailureWithDuplicate|TestReceiveStopsAfterTerminal)$' -count=100 -timeout=180s
/usr/local/go/bin/go test ./internal/syntax -run '^(TestDeepTypeProcessingUsesBoundedStack|TestAttachDocsSharedLineTypeAnchors)$' -count=1 -timeout=120s
/usr/local/go/bin/go test ./internal/tool ./cmd/intercall-go -run '^(TestMappingPhysicalPositionsAcrossFileSets|TestDirectiveDeclarationGroups|TestUnicodeProcedureSelectors|TestPackagePatternAccounting|TestOutputPackageTypeChecking|TestSemanticMachineTableValidation|TestGoProjectionDepthLimit|TestDeepImportProjectionBoundary|TestDeepImportNamedChainBoundary|TestDeepExportProjectionBoundary|TestDeepExportNamedAliasChainBoundary|TestDeepMetadataNamedChainBoundary|TestGeneratedNamespaceValidation|TestGeneratedGoTypeChecking|TestGeneratedImportCheckerProjectionBoundary|TestGeneratedExportCheckerProjectionBoundary|TestRuntimeSPIModelParity|TestParseInterfaceOwnershipRequiresCanonicalBody)$' -count=1 -timeout=180s
/usr/local/go/bin/go test ./internal/tool -run '^(TestArtifactDeterminism|TestArtifactFilesystemSafety|TestArtifactInterruptedExportRepair|TestImportGenerationDeterminism|TestExportGenerationDeterminism|TestCommandDeterminism)$' -count=1
/usr/local/go/bin/go test ./internal/integration -run '^(TestGeneratedFixtureModules|TestCheckedInGeneratedFixturesAreCurrent|TestGeneratedOutputDeterminism)$' -count=1
/usr/local/go/bin/go test -race ./internal/integration -run '^(TestBidirectional|TestNested|TestConcurrent|TestCancellation|TestMalformed|TestShutdown)$' -count=20 -timeout=180s
task_snapshot_check
```

The Reviewer also confirms that temporary regeneration leaves checked fixtures
byte-identical and `task_status` contains only the reviewed `RM-15` snapshot.
Its approval statement explicitly confirms:

- oversized frame headers terminate without attacker-sized allocation;
- terminal selection cannot deadlock behind a writer and `Close` is prompt;
- legal peer-visible ID reuse works and terminal state admits no provider;
- unrestricted syntax processing is stack-safe, native Go projection enforces
  its exact safe depth before recursion, and documentation anchors follow one
  line model;
- Go positions, Unicode source names, package operands, directives, metadata,
  generated namespaces, type checking, and ownership canonicality satisfy
  `SPEC.md`;
- generated artifacts remain static, deterministic, and compilable; and
- skipped findings and deferred product features were not implemented by scope
  creep.

After unchanged approval, integration, and the factual Progress update, the
integration checkout must contain no unaccounted task output.
