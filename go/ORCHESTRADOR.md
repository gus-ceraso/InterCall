# Review-Remediation Orchestrator Runbook

> **Hard boundary:** orchestrate only. Never assess code, edit implementation or
> specification, run technical validation, resolve a conflict, or waive a gate.
> The only permitted content edit is a factual [`PLAN.md` Progress](PLAN.md#progress)
> update after successful integration of an unchanged approved task.

This runbook operationalizes [`PLAN.md`](PLAN.md) and [`AGENTS.md`](AGENTS.md)
for the `RM-00` through `RM-15` remediation program. It is self-contained for a
future Orchestrator but does not override either authority.

## Durable facts

- Subagents do not inherit parent messages. Every prompt must carry the complete
  task contract, authority order, absolute worktree, base, commands, helpers,
  and latest handoff.
- The repository module is `github.com/cerasos/intercall/go`; use Go 1.26.5 only
  through `/usr/local/go/bin/go`. Go commands run from `go/` in the task
  worktree.
- Each Worker has one isolated writable worktree and one persistent session.
  Each task has a separate persistent read-only Reviewer session. Never reuse a
  session for another task.
- `task_snapshot_diff`, not plain `git diff`, is the review target. It includes
  tracked, staged, deleted, and nonignored untracked paths. Reviewer fuzzing
  runs only through `reviewer_fuzz` in a disposable copy.
- Worker evidence is not approval. Reviewer Go tests are independent and use
  every explicit positive `-count` from `PLAN.md`.
- `README.md` is protocol authority. `SPEC.md` is Go authority after an
  amendment is integrated. Review finding labels are traceability only.
- The standalone review report is planning input, not task evidence or an
  authority. Do not copy it into worktrees, snapshots, prompts, or commits when
  the complete `PLAN.md` task contract suffices.
- Evidence remains in session transcripts. Do not create evidence reports,
  logs, coverage files, binaries, fuzz cache, or temporary output in the repo.
- After approval, no file content in that task worktree may change. Any content
  change invalidates approval and returns the complete snapshot to the same
  Reviewer.

## Administrative readiness

Before scheduling work, derive paths rather than trusting the current directory:

```bash
repo=/absolute/path/to/InterCall
repo=$(git -C "$repo" rev-parse --show-toplevel)
go_dir=$repo/go
worktree_root=$(dirname "$repo")/InterCall-worktrees
integration_branch=$(git -C "$repo" symbolic-ref --quiet --short HEAD)
```

Administratively establish all of the following without running tests:

1. Ask the user to confirm `integration_branch`; record that exact ref and its
   expected `HEAD` in the orchestration transcript. A detached `HEAD` or later
   branch switch is an escalation.
2. `bcb3a7a119e1090e188aff4ca21de8adb17f6097` is an ancestor of the confirmed
   integration branch.
3. The integration `HEAD` contains the approved remediation `go/PLAN.md`,
   `go/ORCHESTRADOR.md`, and `go/AGENTS.md`.
4. The integration checkout has no unrelated staged, unstaged, deleted, or
   untracked path. Do not delete, stash, absorb, or ignore unexpected dirt;
   stop and ask the user.
5. Repository-local author identity already exists and is exactly
   `Luiz Gustavo Ceraso Filho <gus@ceraso.dev>`:

   ```bash
   git -C "$repo" config --local --get user.name
   git -C "$repo" config --local --get user.email
   ```

   Never set, copy, or invent identity. Missing or different values are an
   escalation.
6. Read `README.md`, `SPEC.md`, `PLAN.md` (authority, loop protocol, helpers,
   DAG, Progress, selected task, final gate), and `AGENTS.md`. A conflict is an
   escalation, not an invitation to interpret intent.

Administrative commands such as `git status`, `git log`, `git branch`,
`git worktree list`, `git rev-parse`, and ancestry checks are allowed. Technical
commands such as `go test`, `go vet`, fuzzing, formatting, generation, or source
analysis are never Orchestrator work.

## Task lifecycle

### 1. Select only dependency-ready work

Read Progress. A dependency is complete only when its checkbox has `[x]` and an
integrated hash. Approval, a task commit, or an existing branch is insufficient.
Choose pending work according to the DAG and only parallelize peers explicitly
listed safe there. All `internal/tool` tasks (`RM-06` through `RM-14`) remain
serial.

Use ascending task ID for ready integration ties. Immediately before selecting
any base, verify that `$repo` is still on the confirmed `integration_branch` at
the recorded expected `HEAD`; update the expected hash only after an integration
or Progress commit performed by this runbook. Start each worktree from the
latest integration commit that contains every listed dependency. Record:

```text
INTEGRATION BRANCH | EXPECTED HEAD
TASK | SLUG | BASE | BRANCH | WORKTREE | WORKER SESSION | REVIEWER SESSION
```

### 2. Create an isolated branch and worktree

```bash
id=RM-NN
slug=<exact-slug-from-PLAN>
base=<latest-integrated-dependency-complete-hash>
branch=task/$slug
worktree=$worktree_root/$slug
mkdir -p "$worktree_root"
git -C "$repo" worktree add -b "$branch" "$worktree" "$base"
```

Verify administratively that `base` contains the dependency hashes. If the path
or branch already exists, inspect and escalate; never reset, delete, force, or
reuse it speculatively.

### 3. Launch and preserve the Worker

Start one read/write subagent with the Worker prompt below. Record its exact
session ID immediately. Resume that same session for every finding, conflict
replay, or revised handoff. A Worker never commits and never edits Progress.

The Worker prompt must include:

- absolute repository/worktree paths and `go/` command directory;
- branch, base, toolchain, and authority order;
- the complete selected task section copied verbatim from `PLAN.md`;
- dependencies and integrated hashes;
- expected paths and explicit non-goals;
- complete definitions of `task_status`, `task_snapshot*`, and, where needed,
  Worker `fuzz_gate`;
- exact baseline and focused commands; and
- the complete Worker handoff template and escalation rules.

Do not summarize away acceptance details.

### 4. Require a complete Worker handoff

A valid handoff inventories every path, names durable tests, reports each exact
command and exit status, includes snapshot/check facts, and states blockers.
A failure or missing gate returns to the same Worker. The Orchestrator does not
judge whether a partial result is “close enough.”

### 5. Launch and preserve the Reviewer

Start a distinct read-only subagent only after a complete Worker handoff. Record
its session ID. Supply:

- the complete current Worker handoff;
- absolute worktree, branch, and base;
- authority order and the complete task contract;
- all helper definitions, with Reviewer `fuzz_gate` using `reviewer_fuzz`;
- every exact Reviewer command;
- required relevant local/global reading; and
- the exact findings/approval handoff format.

The Reviewer independently runs all gates and reviews the complete snapshot.
For `RM-00`, require complete `README.md` and `SPEC.md` reading. For `RM-15`,
require review of all remediation changes from the reviewed baseline through the
current snapshot and the full Final Definition of Done.

Only a response whose first line is exactly `APPROVED`, with complete evidence,
is approval. A findings handoff must not contain that word.

### 6. Relay revisions verbatim

Send every finding verbatim to the same Worker session. Do not interpret,
prioritize, propose a fix, or drop a minor finding. The Worker revises, reruns
all evidence, and returns a complete replacement handoff. Send that handoff to
the same Reviewer, who rereads the entire current snapshot and reruns every
required gate. Repeat until exact approval.

### 7. Freeze and commit the approved task

After approval, permit no content-changing command in the task worktree.
Administrative status and staging are allowed. Reconfirm the pre-existing local
author configuration; do not alter it.

Stage exactly the approved complete snapshot and commit with the exact subject
from the task section:

```bash
git -C "$worktree" add -A
git -C "$worktree" commit -m '<exact PLAN commit subject>'
task_commit=$(git -C "$worktree" rev-parse HEAD)
```

Do not amend. Record `task_commit`. If staging reveals an undeclared path or
content changed after approval, stop and return the entire state to the same
Reviewer.

### 8. Integrate in DAG order

Immediately verify that `$repo` is on the confirmed `integration_branch` at the
recorded expected `HEAD`. If not, stop. If the task commit descends directly
from that `HEAD`, a fast-forward is administrative. Otherwise integrate the
exact approved commit in DAG order with a clean cherry-pick. Record the resulting
integrated hash and update the recorded expected `HEAD`; the integrated hash may
differ from the task hash.

A conflict is not administrative resolution. Abort the in-progress integration
without choosing file content and preserve the original branch, worktree,
commit, and sessions. Create a replay branch and fresh worktree from current
integration `HEAD`:

```bash
n=<next-positive-replay-number>
replay_branch=task/$slug-replay-$n
replay_worktree=$worktree_root/$slug-replay-$n
git -C "$repo" worktree add -b "$replay_branch" "$replay_worktree" \
    "$integration_branch"
```

Resume the same Worker session in `replay_worktree`. Give it the original base,
approved task commit, and latest integrated base. The Worker, not the
Orchestrator, renders and reapplies the old task diff, resolves it against the
new base, runs the complete task evidence, and returns a new handoff without
committing. Resume the same Reviewer session against the replay worktree for a
complete snapshot review and every independent gate. After approval, freeze and
commit the replay snapshot, then retry integration. Record both generations in
the transcript. A safe peer that invalidates an assumption uses the same replay
procedure even without a Git conflict; never self-certify disjointness beyond
the DAG.

### 9. Record factual Progress

Verify again that `$repo` is on the confirmed `integration_branch` at the
recorded integrated `HEAD`. Only after successful integration, change the
matching checkbox from `[ ]` to
`[x]` and append the integrated hash. This is the Orchestrator's sole content
edit. Do not alter task prose, finding disposition, commands, or dependencies.

For concurrently developed peers, integrate in ascending ID and batch their
checkbox/hash edits in one administrative commit after that wave. For a serial
task, record progress immediately. Use the established identity and an
administrative subject such as:

```text
record RM-NN progress
```

or, for a batch:

```text
record remediation wave progress
```

After the Progress commit, record its hash as the new expected integration
`HEAD`. A Progress commit provides no technical approval.

### 10. Retain state, then clean up safely

Keep task worktrees, branches, and both sessions until task and integrated hashes
and Progress are recorded and no replay can be needed. Remove a worktree only
when administrative inspection proves it has no unique uncommitted content.
Delete a branch only when Git proves no unique work would be lost. Never force
cleanup to make status look clean.

Then select the next dependency-ready task. After `RM-15` integration and its
Progress update, confirm administratively that every remediation task is `[x]`
and no unaccounted task output remains. The Orchestrator does not rerun the final
suite; the `RM-15` Reviewer evidence is the gate.

## Prompt templates

### Worker prompt

```text
You are the persistent Worker for <RM-NN — exact title>.

WORKTREE (absolute; use for every command): <absolute path>
GO COMMAND DIRECTORY: <absolute path>/go
BRANCH: <actual initial-or-replay branch>
BASE: <dependency-complete integrated hash>
MODULE/TOOLCHAIN: github.com/cerasos/intercall/go; Go 1.26.5; use only /usr/local/go/bin/go.

AUTHORITIES:
1. README.md defines the InterCall language and wire protocol.
2. Integrated SPEC.md defines the Go implementation; README wins on protocol conflicts.
3. PLAN.md defines this task's scope, dependencies, acceptance, commands, and commit subject.
4. AGENTS.md defines shared role and engineering rules.
Stop on unresolved conflict, pre-existing blocker, unrelated dirt, or required scope expansion.
The review IDs are traceability only. Lua/LPeg is optional and non-normative.

EXACT TASK CONTRACT (verbatim):
<paste complete PLAN RM-NN section>

INTEGRATED DEPENDENCIES:
<paste IDs and integrated hashes>

EXPECTED PATHS / NON-GOALS:
<paste verbatim from task and disposition constraints>

HELPERS AND COMMANDS:
<paste complete required helper definitions, initialize Worker fuzz_gate if applicable, and paste all exact Worker commands>

Implement only this task. Add durable deterministic tests/fixtures/docs. Never
hand-edit generated files; change the owning generator and regenerate. Do not
commit, edit PLAN Progress, or add evidence output. Use channel/barrier
synchronization rather than sleeps. Before handoff, run task_status, inspect the
complete task_snapshot_diff, and run task_snapshot_check. Return the exact PLAN
Worker handoff with all command statuses and blockers. Do not hide a known
failure as residual risk.
```

### Reviewer prompt

```text
You are the separate persistent read-only Reviewer for <RM-NN — exact title>.

WORKTREE TO REVIEW (absolute): <absolute path>
GO COMMAND DIRECTORY: <absolute path>/go
BRANCH: <actual initial-or-replay branch>
BASE: <base hash>
LATEST WORKER HANDOFF:
<paste complete handoff>

AUTHORITIES AND EXACT TASK CONTRACT:
<paste authority order, complete RM-NN section, dependencies, finding disposition, and relevant integrated SPEC facts>

HELPERS AND COMMANDS:
<paste task_status, task_snapshot, task_snapshot_diff, task_snapshot_check,
reviewer_fuzz, Reviewer fuzz_gate initialization if applicable, and every exact
Reviewer gate>

Remain read-only: do not edit, stage, commit, regenerate in place, or run fuzz
outside reviewer_fuzz. Independently inventory every path, render and inspect
the complete snapshot including untracked files, read governing README/SPEC and
enough repository context to prove behavior, run task_snapshot_check, and run
every gate with PLAN's positive -count. Worker evidence is not your evidence.
Record original status before and after reviewer_fuzz.

Return PLAN's exact findings format if any actionable issue exists; do not use
the word reserved for approval. With zero actionable findings, first line must
be exactly APPROVED and include complete independent evidence, snapshot facts,
and residual risks.
```

### Finding relay

```text
Resume Worker session <worker-session-id> for <RM-NN> in <absolute worktree>.
The separate Reviewer returned the findings below. They are relayed verbatim.
Address every finding within the exact task contract, update durable tests,
rerun all Worker evidence, and return a complete replacement handoff. Do not
commit or edit Progress.

<verbatim Reviewer handoff>
```

Resume Reviewer session `<reviewer-session-id>` with the Worker's complete new
handoff and require a full reread and all independent gates. Never replace a
handoff with an Orchestrator summary.

## Failure and escalation matrix

- **Task-introduced defect:** relay to the same Worker and same Reviewer loop.
- **Pre-existing blocker:** pause affected tasks; create a separately approved,
  dependency-correct prerequisite task only through a plan amendment.
- **Specification ambiguity/defect:** pause implementation; run a dedicated
  specification amendment and review. Rebase and reapprove affected snapshots.
- **Scope expansion or skipped finding becomes reachable:** stop and ask the
  user for an approved plan change. Do not create a generic backlog.
- **Integration conflict:** do not resolve. Abort administratively, then return
  latest integrated context to the same Worker and same Reviewer.
- **Optional Lua/LPeg unavailable:** record the allowed skip only where a task
  invokes it; do not install ad hoc dependencies.
- **Required command/tool unavailable:** no approval is possible; report the
  blocker.
- **Worker session failure:** preserve worktree and ID and resume. If impossible,
  escalate; a replacement receives complete context and restarts Worker duties.
- **Reviewer session failure:** preserve state and resume. If replacement is
  unavoidable, it starts the complete independent review and all gates anew.
- **Approval stale for any reason:** return the whole current snapshot to the
  same Reviewer. The Orchestrator never decides a changed byte is harmless.
