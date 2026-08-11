# Repository Agent Guide

## Scope and authority

This file applies to the entire repository. Consult the authorities in this
order:

1. [`README.md`](README.md) defines the InterCall language and wire protocol.
2. [`SPEC.md`](SPEC.md) defines the Go architecture, mapping, generator, CLI, and
   runtime.
3. [`PLAN.md`](PLAN.md) defines task scope, dependencies, execution order,
   review gates, and commit subjects.
4. `AGENTS.md` defines shared role and engineering guidance.
5. [`ORCHESTRADOR.md`](ORCHESTRADOR.md) is the operational runbook for the
   Orchestrator.

Do not silently choose between conflicting instructions. Stop and escalate the
conflict. `README.md` wins over Go implementation documents on protocol matters,
`SPEC.md` wins over implementation assumptions, and `PLAN.md` controls execution
order. The lower documents summarize or operationalize the higher ones; they do
not override them.

[`intercall-validate.lua`](intercall-validate.lua) is an optional independent,
non-normative oracle. Lua or LPeg may be unavailable. Investigate differences
against `README.md`; never treat the Lua result as protocol authority.

## Repository and toolchain

- Root module: `github.com/cerasos/intercall`
- Required toolchain: Go 1.26.5
- Go executable: `/usr/local/go/bin/go`

Do not rely on `go` from `PATH`, which may resolve to an older release. Run
commands from the assigned worktree root unless `PLAN.md` says otherwise.

## Role boundaries

- **Worker:** edits only the assigned worktree and task scope, implements durable
  tests or fixtures, runs the required validation, and hands off exact evidence.
  A Worker never commits and never edits the [`PLAN.md` progress
  checklist](PLAN.md#progress).
- **Reviewer:** uses a separate, persistent, read-only session. The Reviewer
  examines the complete diff/snapshot, relevant local code, and repository-wide
  contracts; independently runs every required gate; and returns actionable
  findings or a handoff whose first line is exactly `APPROVED`. A Reviewer never
  edits or commits.
- **Orchestrator:** creates and schedules isolated worktrees and sessions, relays
  handoffs, commits unchanged approved diffs, integrates them, records hashes,
  and updates progress. The Orchestrator does not perform technical validation,
  edit implementation, assess code, rerun tests, or resolve conflicts. Its only
  content-edit exception is a factual `PLAN.md` progress update after
  integration.

See [`ORCHESTRADOR.md`](ORCHESTRADOR.md) for the complete orchestration
lifecycle.

## Required loop behavior

Use the exact helpers, handoff formats, gates, revision protocol, and escalation
rules in [`PLAN.md`'s mandatory loop
protocol](PLAN.md#mandatory-loop-protocol) and [validation
suites](PLAN.md#validation-suites). Use the [dependency
DAG](PLAN.md#dependency-dag-and-scheduling) and the relevant [task
contract](PLAN.md#executable-tasks) rather than copying or improvising scope.

The following details are easy to miss:

- Keep the same Worker session and the same Reviewer session through revisions.
- Include tracked, staged, deleted, and nonignored untracked paths in review by
  using `task_snapshot_diff`; a plain `git diff` is incomplete.
- Run `task_snapshot_check` through the temporary-index helper so whitespace
  checks include the complete snapshot without changing the real index.
- Reviewer Go tests are independent and uncached: use the exact positive
  `-count` from `PLAN.md`, including `-count=1` in the baseline.
- Reviewer fuzzing runs only through `reviewer_fuzz` in a disposable copy because
  Go fuzzing may write corpus files.
- Evidence belongs in session transcripts. Do not add evidence reports, logs,
  coverage files, binaries, fuzz cache, or temporary output to the repository.
- After approval, no file in the approved worktree may change. Any change makes
  approval stale and requires the same Reviewer to approve the complete new
  snapshot.

## Engineering standards

Follow `SPEC.md` rather than introducing a competing design:

- Runtime and interface parsing use the standard library. Only discovery code
  may use `golang.org/x/tools/go/packages`, as specified.
- Generated dispatch and codecs are static. Do not add reflection, runtime
  registration, plugin layers, or other deferred mechanisms.
- Preserve public API compatibility, the specified exported surface, and the
  native mapping. Ownership, concurrency, lifecycle, buffer, and error contracts
  are governed by `SPEC.md`.
- Never hand-edit generated files. Change the generator and regenerate through
  the owning task and tests.
- Implement all nondeferred features. Do not move work into the [deferred
  feature boundary](SPEC.md#deferred-features) or implement a deferred feature
  without an approved specification change.

## Validation baseline

Task-specific suites and commands are authoritative in `PLAN.md`. The global
baseline uses the explicit toolchain and complete snapshot helper:

```bash
test "$(/usr/local/go/bin/go env GOVERSION)" = go1.26.5
/usr/local/go/bin/go test -count=1 ./...
/usr/local/go/bin/go vet ./...
task_snapshot_check
test -z "$(find . -type f -name '*.go' -not -path './.git/*' -print0 | xargs -0 -r /usr/local/go/bin/gofmt -l)"
```

Run the race, fuzz, fixture, determinism, filesystem, and narrow stress commands
when the assigned `PLAN.md` task requires them. Worker evidence does not replace
independent Reviewer validation.

## Scope, escalation, and durable guidance

Stay within the assigned task and paths. Escalate unclear authority, an
impossible contract, unrelated worktree changes, a pre-existing blocker, a
`SPEC.md` defect, or a required scope expansion. Do not redesign silently, hide
failures as residual risk, resolve unrelated issues opportunistically, or add a
generic backlog.

Update `AGENTS.md` or `ORCHESTRADOR.md` only for a verified lesson that applies
across tasks. Such a change requires an approved documentation or specification
loop. Do not record transient evidence, personal preferences, session details,
or one-off task notes in either file.
