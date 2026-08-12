# Orchestrator Runbook (`ORCHESTRADOR.md`)

> **Hard boundary:** orchestrate only. Never perform technical validation,
> review code, edit implementation, rerun even a “quick” test, or resolve a
> conflict. The Reviewer owns every technical gate. The only permitted content
> edit is a factual [`PLAN.md` progress](PLAN.md#progress) update after
> integration.

This English-language runbook is self-contained for a future Orchestrator. It
operationalizes [`PLAN.md`](PLAN.md) and [`AGENTS.md`](AGENTS.md); it does not
replace either document.

## Durable operating facts

- Subagents do not inherit parent messages. Every prompt must contain the task's
  complete context and constraints.
- Never rely on an inherited or shared current directory. Give every Worker and
  Reviewer an absolute task-worktree path. Parallel tasks use different
  worktrees; the Reviewer may inspect only its task's worktree and does so
  read-only.
- The module is `github.com/cerasos/intercall/go` on Go 1.26.5. Always specify
  `/usr/local/go/bin/go`; `PATH` may select Go 1.24 or another older release.
- Go validation suites run from `go/` within the task worktree; the git
  snapshot helpers (`task_status`, `task_snapshot*`) run from the worktree root
  or anywhere inside it.
- Reviewer Go tests use the explicit positive `-count` in `PLAN.md`; the global
  test gate uses `-count=1` to bypass the Worker's cache.
- Plain `git diff` and `git diff --check` omit parts of a complete task state.
  The Reviewer uses `task_snapshot_diff` and `task_snapshot_check`, whose
  temporary index includes staged, unstaged, deleted, and untracked files.
- Go fuzzing may write corpus files. The Reviewer invokes every fuzz command
  through `reviewer_fuzz`, which runs in a disposable copy.
- Lua and LPeg may be unavailable. The Lua validator is optional and
  non-normative; `README.md` remains authoritative.
- Evidence stays in Worker and Reviewer transcripts. Do not create evidence
  files.

## Lifecycle

### 1. Read authority and progress

From the integration checkout, read:

1. [`README.md`](../README.md), the protocol authority;
2. [`SPEC.md`](SPEC.md), the Go architecture authority;
3. [`PLAN.md`](PLAN.md), especially [Progress](PLAN.md#progress), the [mandatory
   loop protocol](PLAN.md#mandatory-loop-protocol), [validation
   helpers](PLAN.md#validation-suites), [DAG](PLAN.md#dependency-dag-and-scheduling),
   and the exact task section; and
4. [`AGENTS.md`](AGENTS.md), the shared role and engineering rules.

Stop and ask the user about unresolved conflicts. Never infer an implementation
choice from document order beyond the authority rules in `AGENTS.md`.

### 2. Select dependency-ready work

Choose only a pending task whose listed dependencies have integrated commit
hashes in `PLAN.md`. Use only the safe peers named by the DAG. Parallelize those
peers only when each can start from the correct dependency-complete base and use
an isolated worktree. Use ascending task ID for integration ties.

Approval, an existing branch, or a Worker handoff does not make a dependency
complete. Only integration and a factual progress entry do.

### 3. Create the branch and worktree

Derive paths rather than relying on the shell's current directory:

```bash
repo=/absolute/path/to/InterCall
test "${repo#/}" != "$repo"
repo=$(git -C "$repo" rev-parse --show-toplevel)
worktree_root=$(dirname "$repo")/InterCall-worktrees
slug=<exact-slug-from-PLAN>
branch=task/$slug
base=<dependency-complete-integration-hash>
worktree=$worktree_root/$slug
mkdir -p "$worktree_root"
git -C "$repo" worktree add -b "$branch" "$worktree" "$base"
```

The deterministic names are `task/<branch-slug>` and
`../InterCall-worktrees/<branch-slug>`, using the slug in the DAG table. Verify
administratively that the selected base contains every dependency. If the
branch or path already exists, inspect its administrative status and escalate;
do not delete, reset, or reuse it speculatively.

### 4. Launch the Worker

Launch one read/write Worker subagent with the [Worker prompt](#worker-prompt).
Replace every placeholder, paste the exact task contract and commands, and give
the absolute worktree path. Parent conversation context will not be copied into
the subagent.

### 5. Preserve the Worker session

Immediately record the returned Worker session ID in the orchestration
transcript with the task ID, branch, worktree, and base. Resume that exact
session for every finding, rebase, conflict, or revised handoff. Do not create a
fresh Worker to handle revisions.

### 6. Launch the Reviewer

After a complete Worker handoff, launch a separate persistent Reviewer session
with the [Reviewer prompt](#reviewer-prompt). Supply the complete Worker handoff,
absolute worktree path, base, task contract, authorities, helper definitions,
and exact commands. The Reviewer is read-only and independently renders the
complete snapshot and runs the gates. Record its session ID separately.

Only a handoff whose first line is exactly `APPROVED` and that contains the
required independent evidence is approval. A findings handoff must not contain
the word `APPROVED`.

### 7. Relay revisions without changing sessions

Send Reviewer findings verbatim to the same Worker session with the [relay
prompt](#finding-relay). Send the Worker's complete revised handoff to the same
Reviewer session. Repeat until that Reviewer returns exact `APPROVED`.

The Orchestrator neither interprets findings nor suggests fixes. If either
session becomes unavailable, follow [Failures and escalation](#failures-and-escalation).

### 8. Commit the unchanged approved state

After approval, allow no file modification in the task worktree. Any change,
including an automatic rewrite or corpus file, invalidates approval.
Administrative status and staging operations are permitted; technical checks
are not.

Before committing, verify the existing repository-local identity:

```bash
git -C "$worktree" config --local --get user.name
git -C "$worktree" config --local --get user.email
```

The established identity is `Luiz Gustavo Ceraso Filho <gus@ceraso.dev>`. Do not
set, copy, or invent identity. If either value is absent or inconsistent, stop
and ask the user. Then stage the exact approved state, commit with the exact
`PLAN.md` task subject, and record the resulting task commit hash. Do not amend
or modify the worktree after approval.

### 9. Integrate in DAG order

Integrate approved commits only after their dependencies, with ascending task ID
breaking ties. A clean integration of the exact approved commit is
administrative. Record the resulting integrated hash, which may differ from the
task-branch hash.

If integration conflicts or an integrated sibling invalidates an assumption,
stop. Do not edit conflict markers or choose a resolution. Return the work to
the same Worker session against the current integrated base, then require the
same Reviewer session to review the complete resulting snapshot and approve it
again. Only the reapproved state may be committed and integrated.

### 10. Update factual progress

Only after successful integration, edit the matching `PLAN.md` checkbox from
`[ ]` to `[x]` and append the integrated commit hash. This is factual
administration, not technical approval. Never mark approved-but-unintegrated
work complete, and never encode in-progress or review state in `PLAN.md`.

Task Workers must not edit the checklist. After integrating parallel siblings,
batch all factual checkbox/hash updates for that wave into one administrative
progress commit so sibling branches do not conflict over `PLAN.md`. For a
serial wave, make the progress commit after that integration. Verify the same
repository-local author configuration before every administrative commit.

### 11. Clean up and continue

Retain sessions and task state until the integrated hash and progress update are
recorded and no conflict replay can be needed. Administrative `git status`,
`git worktree list`, `git branch`, and ancestry checks are allowed. Remove a worktree
only when it has no unique uncommitted changes; avoid forced removal. Delete a
branch only when integration is recorded and Git can establish that no unique
work would be lost; otherwise retain it and escalate. Never guess that a dirty
or unmerged branch is disposable.

Archive or release the recorded session references when safe, then return to
step 2 and launch the next dependency-ready task.

## Prompt templates

### Worker prompt

```text
You are the Worker for <IC-NN — exact title>.

WORKTREE (absolute; use it for every command): <absolute path>
BRANCH: task/<slug>
BASE: <dependency-complete hash>
MODULE/TOOLCHAIN: github.com/cerasos/intercall/go; Go 1.26.5; use only /usr/local/go/bin/go.

AUTHORITIES:
1. README.md is authoritative for the language and wire protocol.
2. SPEC.md is authoritative for the Go architecture and mapping; README wins on protocol conflicts.
3. PLAN.md controls this task's scope, dependencies, commands, handoff, and execution order.
4. AGENTS.md supplies repository-wide role and engineering rules.
Stop and report any unresolved conflict; do not silently choose or redesign.
The Lua validator is optional and non-normative; Lua/LPeg absence is an allowed SKIP only where PLAN says so.

EXACT TASK CONTRACT (verbatim from PLAN.md):
<paste the complete IC-NN task section, including commit subject>

ALLOWED/EXPECTED PATHS:
<paste task paths or the narrow path set derived from its deliverables>

INTEGRATED DEPENDENCY CONTEXT:
<paste dependency hashes and any relevant approved handoff facts>

REQUIRED HELPERS AND COMMANDS:
<paste the complete PLAN helper definitions needed by this task, initialize the Worker fuzz_gate when applicable, and paste every exact Worker command>

Implement only this task in the specified worktree. Add durable tests, fixtures,
or documentation required by the contract. Do not commit. Do not edit PLAN.md's
progress checklist. Do not create evidence files. Before handoff, inventory all
paths with task_status, render the complete temporary-index snapshot, and run
task_snapshot_check. Return the exact PLAN Worker handoff format with command
exit status and salient results. Report blockers under PLAN's escalation rules;
do not hide known task-scope failures as residual risk.
```

### Reviewer prompt

```text
You are the separate, read-only Reviewer for <IC-NN — exact title>.

WORKTREE TO REVIEW (absolute): <absolute path>
BRANCH: task/<slug>
BASE: <base hash>
WORKER SESSION/HANDOFF: <paste the complete latest handoff; do not rely on parent context>
MODULE/TOOLCHAIN: github.com/cerasos/intercall/go; Go 1.26.5; use only /usr/local/go/bin/go.

AUTHORITIES AND TASK CONTRACT:
<paste the authority rules and complete IC-NN task section verbatim>

REQUIRED HELPERS AND COMMANDS:
<paste task_status, task_snapshot, task_snapshot_diff, task_snapshot_check,
reviewer_fuzz, the Reviewer fuzz_gate initialization when applicable, and every
exact Reviewer gate command>

Remain read-only: never edit, stage, or commit. Read the governing README.md and
SPEC.md sections, relevant local code, and enough global context to trace the
contract. Independently inventory all files, render and review the complete
snapshot including untracked files, run task_snapshot_check, and run every gate
without relying on Worker results. Go test commands must use PLAN's explicit
positive -count. Run fuzz only in reviewer_fuzz and record original status before
and after.

If there is any actionable finding, return the exact PLAN findings format and do
not use the word APPROVED. If there are zero actionable findings, the first line
must be exactly APPROVED, followed by the task/base/complete diff reviewed,
independent evidence, snapshot facts, and residual risks as PLAN requires.
```

### Finding relay

```text
Resume Worker session <worker-session-id> for <IC-NN> in <absolute worktree>.
The separate Reviewer returned the following findings. They are relayed
verbatim; address them within the task contract, update durable tests as needed,
rerun all Worker evidence, and return a complete new Worker handoff. Do not
commit or edit PLAN progress.

<verbatim Reviewer handoff>
```

After the revised Worker handoff, resume Reviewer session
`<reviewer-session-id>` with the complete handoff and instruct it to reread the
complete current snapshot and rerun every gate. Never summarize away either
handoff.

## Failures and escalation

- **Task-introduced finding:** relay it verbatim to the same Worker, then return
  the complete revision to the same Reviewer.
- **Pre-existing blocker:** pause affected tasks and schedule a separately
  scoped Worker/Reviewer prerequisite repair loop. Integrate it, rebase affected
  work, and obtain fresh approval.
- **Nonblocking pre-existing issue:** schedule a separate follow-up at the
  earliest dependency-correct point. If it is outside `README.md`/`SPEC.md`
  scope, ask the user rather than expanding work or creating a generic backlog.
- **`SPEC.md` defect or blocking ambiguity:** pause affected tasks. Run a
  dedicated specification amendment -> review -> commit loop, integrate it,
  rebase, and resume with fresh task review. Never redesign in implementation or
  in an administrative progress edit.
- **Integration conflict or invalidated assumption:** do not resolve it. Route
  the current integrated context to the same Worker and require the same
  Reviewer to approve the complete resolved state before a new commit.
- **Optional oracle unavailable:** record the allowed SKIP from the Worker and
  Reviewer. Do not install ad hoc dependencies or block work solely for missing
  Lua/LPeg.
- **Worker tool or session failure:** preserve the worktree and session ID, then
  resume the same session. If it cannot be resumed, escalate; a replacement
  Worker must receive the complete context and restart the Worker loop.
- **Reviewer tool or session failure:** do not approve. Preserve state and resume
  the same Reviewer. If replacement is unavoidable, give the new separate
  Reviewer the complete context and require a full review and all independent
  gates from the beginning.
- **Required validation tool unavailable:** the Reviewer cannot approve. Report
  the blocker; only an explicitly optional gate may be skipped.
- **Approval becomes stale:** after any file-content change, return the complete
  state to the same Reviewer and rerun the whole review gate. The Orchestrator
  must not decide that a change is too small to matter.
