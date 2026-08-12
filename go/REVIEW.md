# InterCall Go Implementation — In-Depth Review

- **Date:** 2026-08-12
- **Reviewed commit:** `bcb3a7a` (record Go restructure progress; clean tree)
- **Scope:** `go/` module `github.com/cerasos/intercall/go`, ~44.5K lines over 129 files
- **Review type:** static, findings-only (no code changes made)
- **Review waves:** wave 1 — specialist reviewer fleet (findings FA1-01…FA6-12, §3–§7); wave 2 — follow-up review of the same snapshot, compiled from `~/REVIEW.md` (findings FB-01…FB-09, integrated into §4–§5). Wave 2 uses Critical/High/Medium severities, which map to P0/P1/P2.
- **Authorities:** `README.md` (protocol), `go/SPEC.md` (Go mapping/architecture), `go/GO.md` (usage), `go/PLAN.md` (task plan)

---

## 1. Executive summary

The Go proof of concept is unusually high-quality: the ownership invariants at the
heart of the runtime (pending-call exactly-once outcomes, single-winner terminal
selection, `mu`→`writeMu` lock ordering, full-read frame semantics, byte-exact
wire layout) hold under every interleaving we traced, and the grammar, scanner,
positions, and FNV-0 key machinery in `internal/syntax` were verified sound
production-by-production. The defects found are concentrated where passing
test suites structurally cannot look: **untested interleaving windows**,
**allocation paths the fuzzers deliberately cap**, and **adversarial or
contract-violating inputs the stdlib assumes away**.

The headline findings:

1. **P0 — Remote process crash via one 24-byte frame** (FA3-01/FA2-02). The frame
   reader allocates `make([]byte, int(wireLength))` with no upper bound beyond
   native `int`. A hostile peer needs a single header to either panic the
   process (unrecovered `makeslice` panic on the receive path) or trigger a
   fatal, unrecoverable `runtime: out of memory`. The project's own fuzz target
   and integration harness cap this exact allocation, so the crash path is
   untested by construction.
2. **P1 — Stalled writer deadlocks the whole connection** (FA3-02). `Call` and
   `handleRequest` hold the connection lock `mu` while blocking on the write
   gate. A transport write that stalls pins `mu`, which blocks
   `selectTerminal` — the only path that closes the stream, which is the only
   thing that unblocks a stalled write per the `ByteStream` contract. `Close`,
   `Wait`, and the receive loop all freeze.
3. **P1 — Legal request-ID reuse can kill a healthy connection** (FA1-03 /
   FA2-01 / FA3-03, three independent confirmations). The incoming-ID release
   runs in the handler goroutine *after* the response write returns, with no
   ordering edge against the receive loop's next reservation. A fully compliant
   peer that reuses an ID the instant it receives the response can be falsely
   flagged with a terminal protocol error.

Also notable: a second P1 deadlock-relevant finding family in `internal/tool`
was empirically confirmed by the reviewer (cross-FileSet position corruption
FA6-01, untype-checked output packages FA6-02, var-group directive
multiplication FA6-03), and the parser's unbounded type-nesting recursion
(FA4-01) can produce a fatal, unrecoverable stack overflow from a ~30–50 MB
`.intercall` file.

A second review wave (compiled from `~/REVIEW.md`, findings FB-01…FB-09)
re-scrutinized the same snapshot, concentrating on the areas wave 1 flagged
as gaps — the tool mapping/generator/artifact internals, syntax
documentation attachment, and the runtime's shutdown windows. It
independently re-confirmed the write-gate deadlock (FB-01) and **elevated it
to Critical**: the reproduction (block writer A on `stream.Write`, start
writer B, then `Close`) leaves all three operations blocked, violating the
specified non-waiting close and terminal-teardown contracts. It also
surfaced four High findings the first wave missed: post-terminal frame
processing can still launch providers and side effects (FB-02); the
generator accepts inputs that produce uncompilable Go via collisions with
mandatory generated symbols (FB-03); unmatched export patterns can be
silently ignored (FB-04); and ownership validation does not require an
existing interface body to be canonical (FB-05). Four Medium findings
complete the wave (FB-06…FB-09): typed-nil constructor arguments,
malformed metadata in marked generated files, accidental `Connection`
copyability, and a lost type-position documentation comment on shared
lines.

**Finding counts:** wave 1: 1 P0, 5 P1 (one of which has three confirming
IDs), 6 P2, 16 P3 — 28 distinct findings across 33 originally reported IDs.
Wave 2: 1 Critical (FB-01, an independent re-confirmation and severity
elevation of FA3-02), 4 High, 4 Medium — 9 findings, 8 of them new distinct
root causes. Combined: **36 distinct root causes**; three of wave 1's root
causes account for 8 of the 28 rows (several reviewers converged on the
same genuinely hot spots).

---

## 2. Methodology

- A fleet of six specialist reviewers was launched, each assigned one package or
  concern with a strict findings template (severity, `file:line`, evidence,
  why-the-test-suite-misses-it, recommendation).
- **Fleet attrition:** one reviewer (syntax validation / FNV-0 keys / semantic
  docs / canonical formatting) crashed before writing its report. The remaining
  five reports were salvaged in full. See [§8 Coverage gaps](#8-coverage-gaps).
- Reviewers read every file in scope completely and were instructed to verify
  hypotheses by reading more code or via targeted read-only commands
  (`go test -run` of a specific test, standalone `/tmp` harnesses). No fuzzing,
  no full-suite runs, no repository modifications were performed — the
  instruction was to find what `go test`/`vet`/`race`/`fuzz` cannot.
- **Synthesizer verification:** every P0/P1 claim in this document was
  re-verified against the source at the cited lines; both empirical
  reproductions (the OOM/allocation harness and the broken-context panic
  harness) were inspected. Findings marked *"empirically confirmed"* were
  reproduced by the reviewing agent outside the repo.
- Report files: `/tmp/review/A1.md` … `/tmp/review/A6.md`; reproduction harnesses
  at `/tmp/review/probe` (allocation fatal-crash probe) and `/tmp/nilcause`
  (contract-violating-context panic probe).
- **Wave 2:** a follow-up review of the same snapshot (source file
  `~/REVIEW.md`) concentrated on the areas wave 1 left open — tool
  mapping/generator/artifact internals, syntax documentation attachment, and
  runtime shutdown windows — and produced FB-01…FB-09, integrated into §4–§5.

Severity definitions:

| Level | Meaning |
| --- | --- |
| P0 | Contract-breaking or wrong behavior on a reachable path; process-fatal from untrusted input |
| P1 | Significant defect or robustness gap (liveness, false teardown, silent wrong output) |
| P2 | Minor bug, edge-case robustness gap, or doc/API drift |
| P3 | Nit, elegance, or latent issue unreachable from production call sites today |

---

## 3. Findings index

| ID | Sev | Area | Title | `file:line` |
| --- | --- | --- | --- | --- |
| FA3-01 / FA2-02 | **P0** | Runtime receive | Unbounded wire-controlled payload allocation fatally crashes the process | `frame.go:88-94` |
| FA3-02 | **P1** | Runtime concurrency | `mu` held while blocking on the write gate; stalled writer freezes `Close`/`Wait`/receive loop | `receive.go:97-98`, `call.go:129-130` |
| FA1-03 / FA2-01 / FA3-03 | **P1** | Runtime concurrency | Incoming-ID release not atomic with response-write completion; legal ID reuse can spuriously terminate the connection | `receive.go:105-111` |
| FA6-01 | **P1** | Tool mapping | Cross-FileSet token positions corrupt mapping diagnostics (and recorded `GoDecl.Pos`) | `mapping.go:387,772`, `metadata.go:192`, `gosource.go:348` |
| FA6-02 | **P1** | Tool discovery | Existing output package that does not type-check is accepted as "importable" | `discover.go:45,454-500` |
| FA6-03 | **P1** | Tool directives | Var-group doc inheritance tags every spec: one directive silently declares several sentinels | `gosource.go:415-431` |
| FA1-01 | P2 | Runtime lifecycle | `cause != nil` guard conflates selection state with a non-nil cause; contract-violating context panics the process | `connection.go:170-177,222` |
| FA1-02 | P2 | Runtime lifecycle | `Close()` runs full teardown synchronously; "returns immediately" only for prompt transports | `connection.go:136-141,195-200` |
| FA2-03 | P2 | Runtime write | Partial-write diagnostics misreport the frame remainder as the total; invalid-count classification bypassed when `err != nil` | `write.go:29-38` |
| FA4-01 | P2 | Syntax parser | Unbounded type-nesting recursion → fatal unrecoverable stack overflow from ~30–50 MB input | `parse.go:247-280`, `validate.go:130-147`, `docs.go:57-140`, `format.go:127-167` |
| FA4-02 | P2 | Syntax docs | Two inconsistent line models: bare `\r` terminates lines for attachment but not for positions | `docs.go:207-232`, `position.go:27-28` |
| FA6-04 | P2 | Tool directives | Parenthesized named struct types wrongly rejected for `@intercall exception` (legal Go) | `gosource.go:493-496`, `directives.go:386-410` |
| FA1-04 | P3 | Runtime concurrency | Callers/handlers blocked on the write gate are blind to per-call cancellation and terminal state; a stalled writer strands every waiter until an external `Close` | `call.go:129-130`, `receive.go:97-98` |
| FA1-05 | P3 | Runtime design | Encoder panic propagates to the application; decoder/dispatch panics are contained — three failure modes for one defect class | `call.go:106-110` |
| FA1-06 / FA2-04 / FA3-05 | P3 | Runtime design | `writeFrame` is dead production code and a second, admission-free write path | `write.go:48-58` |
| FA1-07 | P3 | Runtime lifecycle | Panicking `stream.Close()` skips `cancelHandlers`/`close(teardown)`; `Wait` hangs forever | `connection.go:195-200` |
| FA2-05 | P3 | Runtime frame | `buildFrame` silently masks out-of-range IDs and does unchecked header-size arithmetic | `frame.go:127-129` |
| FA3-04 | P3 | Runtime receive | `incoming` set never drained at teardown; non-success handler paths leak entries for the connection lifetime | `receive.go:99-103,107-110` |
| FA4-03 | P3 | Syntax tests | Fuzz asserts invariants only; the one grammar oracle (Lua differential) silently skips without lua+LPeg | `fuzz_test.go:16-84`, `lua_test.go:33` |
| FA4-04 | P3 | Syntax docs | Bare-CR corner case splits the trailing-comment and blank-line group rules | `docs.go:196-204,234-271` |
| FA6-05 | P3 | Tool selection | Data result of type `error` passes the signature check; rejected later with a wrong-phase message | `select.go:380-395` |
| FA6-06 | P3 | Tool discovery | Directive grammar enforced on generated files; malformed directive in third-party generated code aborts export | `discover.go:404-424` |
| FA6-07 | P3 | Tool discovery | `packageError` builds logical paths from basenames; breaks cgo files and colon-containing dirs | `discover.go:362-375` |
| FA6-08 | P3 | Tool diagnostics | Only one diagnostic per phase ever surfaces; SPEC's multi-diagnostic sorting is dead for tool phases | `diagnostics.go:36-42` |
| FA6-09 | P3 | Tool discovery | `IntercallGenerated` marker detection byte-fragile (BOM, leading blank line) | `gosource.go:306-318` |
| FA6-10 | P3 | Tool selection | Excluding a tagged generic function silently succeeds, contradicting SPEC's "generic selectors are errors" | `select.go:249-252` |
| FA6-11 | P3 | Tool directives | `isExported` is ASCII-only; Unicode-exported Go names misreported | `directives.go:560-564`, `select.go:161-166` |
| FA6-12 | P3 | Tool mapping | `Syntax`/`CompiledGoFiles` positional correspondence is an unguarded go/packages invariant | `mapping.go:277-289` |
| FB-01 | **P0** | Runtime concurrency | Terminal shutdown can deadlock behind the write gate (independent re-confirmation; severity elevation of FA3-02) | `call.go:129-130`, `receive.go:97-99`, `connection.go:170-200` |
| FB-02 | **P1** | Runtime receive | Requests read after terminal selection can still invoke providers | `receive.go:31-47,57-65,87-105`, `connection.go:176-200` |
| FB-03 | **P1** | Tool generation | Generator accepts inputs that produce uncompilable Go (generated-name collisions, no type-check of staged output) | `override.go:182-225`, `import.go:338-344,502-584`, `export_emit.go:193-209,556-560`, `artifact.go:480-526` |
| FB-04 | **P1** | Tool discovery | An unmatched export operand can be silently ignored | `discover.go:141-153` |
| FB-05 | **P1** | Tool artifacts | Ownership validation does not require an existing interface body to be canonical | `artifact.go:159-189,774-782` |
| FB-06 | P2 | Runtime lifecycle | Typed-nil constructor arguments bypass validation and may panic | `connection.go:78-92` |
| FB-07 | P2 | Tool metadata | Marked generated metadata can be malformed without rejection | `metadata.go:195-230`, `mapping.go:807-810` |
| FB-08 | P2 | Runtime lifecycle | `Connection` is accidentally copyable; a copy can corrupt lifecycle state | `connection.go:19-68,170-200` |
| FB-09 | P2 | Syntax docs | Documentation attachment discards a valid type-position comment on a shared line | `docs.go:180-203` |

---

## 4. Cross-cutting themes

Three root causes dominate. They share a property: each sits in a window or
input class that the test suite cannot reach by construction.

### 4.1 Unbounded wire-controlled allocation (P0) — FA3-01 / FA2-02

`readFramePayload` (`go/frame.go:88-94`) validates the wire payload length only
against `uint64(maxInt)` and then executes `make([]byte, int(length))` with no
recovery anywhere on the receive path (`receiveLoop`, `readFrame`,
`readFramePayload`, `parseFrameHeader` have no `recover`). One 24-byte header
from the peer therefore produces, depending on the length:

- lengths above Go's `maxAlloc` (~2^47–2^48): an unrecovered `makeslice: len
  out of range` panic in the receive goroutine — the whole process dies;
- lengths between `maxAlloc` and physical memory: a fatal `runtime: out of
  memory` `throw`, which is **not recoverable even in principle**;
- far smaller lengths on memory-constrained hosts: the same fatal OOM.
  (`/tmp/review/probe` confirmed `make([]byte, 1<<45)` is fatal on the review
  host.)

This contradicts the runtime's own contract — SPEC.md:796 *"A frame that cannot
be safely buffered is terminal"* — because no such category exists in the code:
every length ≤ `maxInt` is "safely buffered" until the allocator proves
otherwise. README "Failures and Limits" explicitly permits implementations to
"limit frame payload length" and to "close the connection" above the limit.

The crash path is untested by construction: `FuzzReadFrame` hard-caps payloads
at 4096 bytes with an explicit admission that natively representable but
enormous lengths are skipped (`frame_internal_test.go:498-501,568`), and the
integration harness bounds raw payload allocation at 1 MiB
(`internal/integration/frame.go:20-22`).

**Recommendation (mandatory):** enforce a local payload cap (fixed constant or
config, e.g. 64 MiB) in `parseFrameHeader`/`readFramePayload` before
allocation; a wire length above it is a terminal `ErrProtocol` — exactly the
"cannot be safely buffered" category SPEC promises. Optionally add a
`recover`-based defense-in-depth in `receiveLoop` that selects terminal, but
note that a limit is not optional: `recover` cannot contain an OOM.

### 4.2 Stalled writer pins `mu` and deadlocks the connection (P1) — FA3-02

Both production writers acquire the connection lock and then block on the write
gate while still holding `mu`:

- `call.go:129-130` — `c.mu.Lock(); c.writeMu.Lock()`
- `receive.go:97-98` — `c.mu.Lock(); c.writeMu.Lock()`

`selectTerminal` (`connection.go:170-171`) requires `mu`, and the teardown it
runs is the only place the runtime closes the stream (`connection.go:198`) —
which is the `ByteStream`-contract escape hatch that unblocks a stalled write
(`binding.go:11`: "make Close unblock both read and write"). Chain: writer A
stalls inside `writeFull` (realistic: TCP peer stopped draining); writer B
blocks on `writeMu` holding `mu`; now `Close()` → `selectTerminal` → `mu`
blocked → stream never closed → A's write never unblocked. Meanwhile
`claimResponse`/`reserveIncoming`/`Wait` all need `mu`. The connection is
frozen until the transport eventually errors on its own (minutes, with TCP
retransmit timers). This also breaks the SPEC "Close … returns nil without
waiting" contract (SPEC.md:608-609).

**Why tests miss it:** every gate test releases the gate from the test goroutine
or never calls `Close()` while a waiter holds `mu`; no test combines a stalled
gate holder with `Close()`. `TestConnectionCloseNonwaiting` parks the receive
loop in a read, which holds no `mu`.

**Recommendation:** do not hold `mu` while waiting for `writeMu`. Acquire the
gate first, then `mu`, re-check `cause`/`ctx.Err()`, and make the gate wait
interruptible (`select` on `c.terminal` and `ctx.Done()`) so a cancellation
that fires while waiting wins without an ID — preserving the current admission
contract without pinning `mu` behind a foreign write.

**Wave 2 confirmation (FB-01, Critical):** the follow-up review reproduced
the cycle with a compliant blocking stream — writer A blocked inside
`stream.Write`, writer B holding `mu` while waiting on the gate, then
`Close`; all three remain blocked because terminal selection cannot reach
`stream.Close`, the only operation that unblocks writer A. It confirms the
same mechanism and the same recommendation, and elevates the severity: this
is the one defect that violates the non-waiting close and terminal-teardown
contracts on a reachable path with a fully compliant peer.

### 4.3 Incoming-ID release race (P1) — FA1-03 / FA2-01 / FA3-03

Three independent reviewers converged on this one. README ("Frames") states the
peer may reuse a request ID "once it receives the response", and is "not
required to remember IDs across completed requests". SPEC.md:752-754 fixes the
runtime's release point: *"A handler's incoming ID remains active until its
complete response write succeeds; reuse before the earlier response write
completes is a terminal protocol error; reuse afterward is allowed."*

The implementation reserves the ID in the receive loop (`receive.go:40-46`) and
releases it in the handler goroutine only after `writeFull` returns,
`writeMu.Unlock()`, and the error branch (`receive.go:105-111`). There is no
synchronization edge between the handler's release and the receive loop's next
reservation. The window between the transport accepting the response bytes and
`releaseIncoming` executing includes a `mu.Lock` and any goroutine preemption —
arbitrarily large if the handler is descheduled. A fully compliant peer that
round-trips a reuse request inside that window is falsely flagged:
`selectTerminal(… ErrProtocol)` (`receive.go:43-45`) tears down a healthy
connection.

**Why tests miss it:** reuse tests synchronize on the map drain
(`waitIncoming(t, c, 0)`) before feeding the reused-ID frame, eliminating the
very interleaving under test. It is a logic race, not a data race, so `-race`
cannot see it.

**Recommendation:** order release against reservation — release the ID while
still holding `writeMu` immediately after the successful write (shrinking the
window to the last-Write-return→delete gap), or better, carry a per-ID
write-completion flag in the map entry and treat only an *incomplete* write as
a terminal reuse; add a regression test that reuses the ID from the instant the
response bytes are observed, without waiting for the map to drain.

---

## 5. Findings by area

### 5.1 Runtime — connection lifecycle, binding, context, errors (A1)

**FA1-01 [P2] — `cause != nil` conflates selection state with a non-nil cause; a
contract-violating construction context crashes the process.**
`connection.go:170-177` guards the selection winner with `if c.cause != nil`,
so `selectTerminal(nil)` is indistinguishable from "not yet selected". The one
user-controlled value reaching it is the observer's `c.selectTerminal(c.ctx.Err())`
(`connection.go:222`). The runtime's only precheck is `ctx.Err()` at
construction (`connection.go:91`). A hand-rolled `context.Context` whose
`Done()` closes but whose `Err()` returns nil passes the precheck; then
`context.WithCancel(ctx)` (`connection.go:95`) panics with the stdlib-internal
`"context: internal error: missing cancel error"` — either in the caller's
goroutine (already-done context) or in a runtime-spawned propagation goroutine
(becomes done later), killing the process. If selection ever ran with a nil
cause, `Wait` would return nil, violating its "never returns nil" contract.
*Empirically confirmed* (harness at `/tmp/nilcause`).
**Recommendation:** make selection state the `terminal` channel itself (or
guard by non-blocking receive), and defensively map a nil `err` to
`context.Canceled` in `selectTerminal`.

**FA1-02 [P2] — `Close()` runs the complete teardown synchronously in the
caller.** `Close()` → `selectTerminal(ErrClosed)` (`connection.go:140`), and
the winner executes every pending completion, `stream.Close()`, handler
cancellation, and `close(teardown)` in its own goroutine
(`connection.go:195-200`). GO.md:194 promises "Close terminates the connection
and returns immediately", but a transport whose `Close()` flushes or waits
stalls the caller indefinitely (and combined with 4.2, deadlocks). All test
streams have instant `Close()`.
**Recommendation:** document the synchronous-teardown caveat; consider running
teardown on a dedicated goroutine — `Wait` already waits on `teardown`, so
semantics are preserved.

**FA1-07 [P3] — Panicking `stream.Close()` skips `cancelHandlers()` and
`close(c.teardown)`**, hanging `Wait` forever and leaking handler contexts. The
teardown sequence (`connection.go:195-200`) has no recovery. No test stream
panics from `Close`.
**Recommendation:** defer-guard the teardown body so `close(c.teardown)`
happens on every exit path.

**FA1-05 [P3] — Encoder-panic asymmetry.** `Call` invokes the generated
encoder with no recovery (`call.go:107`); a panic escapes to the application.
Decoder panics are recovered and made terminal (`call.go:257-263`); dispatch
panics are recovered into `internal_exception` (`receive.go:122-130`). The same
defect class (a generated-code bug) has three different failure modes.
State-wise a propagating encoder panic is safe (no ID, frame, or pending
entry), so this is a design/API-consistency gap, not a correctness bug; SPEC is
silent on encoder panics.
**Recommendation:** decide and document — recover encoder panics into the
call's error, or state in SPEC that they propagate.

**FB-06 [P2] — Typed-nil constructor arguments bypass validation and may
panic.** `connection.go:78-92`: the checks test only whether an interface
itself is nil. A typed-nil `ByteStream` passes, starts the receive loop, and
can panic when `Read` or `Close` dereferences the receiver; a typed-nil
implementation of `context.Context` likewise passes and can panic at
`ctx.Err()`. This contradicts the documented nil-argument rejection contract
and is the sibling of FA1-01 — FA1-01 needs a contract-violating context to
panic; FB-06 needs no contract violation beyond passing a typed nil.
**Recommendation:** reject nil underlying interface values (or document that
typed nil is unsupported, though rejection is safer) before taking stream
ownership or starting goroutines.

**FB-08 [P2] — `Connection` is accidentally copyable; a copy can corrupt
lifecycle state.** `connection.go:19-68,170-200`: although described as
opaque, callers can use `clone := *conn`. The copy has distinct mutexes and
`cause`, but aliases the channels, maps, stream, and cancellation function.
`clone.Close()` closes the shared `terminal` while the original still has no
cause; a later original terminal selection can panic with `close of closed
channel` (or leave `Wait` inconsistent). **Recommendation:** make
`Connection` a copy-safe wrapper around one private heap state, or
explicitly prevent/catch copies; at minimum, add a no-copy guard and a test
that prevents lifecycle channels from being independently closed.

**Verified sound (A1):** pending-call ownership is exactly-once under all
traced interleavings; single-winner `close(terminal)`/`close(teardown)`;
`mu`→`writeMu` order respected everywhere with no inversion; binding identity
is sound (dispatch func lives behind a pointer — no func-comparability trap);
error taxonomy matches SPEC "Fixed Go Runtime Exceptions" exactly (six local
sentinels, three wire sentinels, `errors.Is`/`%w` correct); every terminal path
wraps `ErrProtocol` or the transport error with an operation prefix; cleanup
error suppressed per contract; `WithConnection`/`ConnectionFromContext` match
their contracts including the forged-nil-`*Connection` case.

### 5.2 Runtime — frame I/O and write path (A2)

**FA2-03 [P2] — `writeFull` partial-write diagnostics misreport.** The message
formats the *remainder* `len(b)` at the failing call, not the frame total
(`write.go:32-34`), so a frame that accepted 24 bytes and then 3 of 30 reports
"partial write after 3 of 30 bytes". Separately, when a writer returns an
impossible count *with* an error, the error branch (`write.go:30`) fires before
the invalid-count branch (`write.go:35-39`), so
`errors.Is(err, errInvalidWriteCount)` is false for the same contract violation
that is classified when the error is nil. Both paths are terminal, so behavior
is safe; diagnostics/classification are inconsistent.
**Recommendation:** carry the frame total into the loop; check `n < 0 ||
n > len(b)` before the error branch.

**FA1-06 / FA2-04 / FA3-05 [P3] — `writeFrame` is dead production code and a
second, admission-free write path.** `Connection.writeFrame` (`write.go:48-58`)
— the only function that documents and centralizes "hold the connection-wide
gate, release on every path" — is invoked exclusively by tests
(`write_internal_test.go:211,248,251`). Both production writers hand-roll the
gate inline (`call.go:128-156`, `receive.go:97-111`) and have already drifted
apart: `Call` rechecks `ctx.Err()`, the handler rechecks `cause`; the handler
additionally leaks its incoming ID on the write-error and abandon paths
(FA3-04). The helper acquires `writeMu` without `mu`, safe today but
incompatible with any future caller holding `mu` (lock-order inversion with
`call.go:130`). Three reviewers independently flagged this as the runtime's
clearest maintenance trap. **Recommendation:** route both production writers
through `writeFrame` (with an admission callback), or delete it and keep one
place that owns the "write error ⇒ selectTerminal + gate release on every
path" invariant.

**FA2-05 [P3] — `buildFrame` silently masks out-of-range IDs and does unchecked
header-size arithmetic.** `requestID & idMask` (`frame.go:129`) truncates an
out-of-range ID to a *different valid ID* instead of rejecting it, and
`frameHeaderSize + len(payload)` (`frame.go:128`) is unchecked. Both are
unreachable from production call sites today (IDs are capped by `nextID`;
payloads cannot materialize above `maxInt-24`), but this is the one spot
README's "checked before … arithmetic" rule is not observed, and the decode
side masks in `parseFrameHeader` where masking is *required* — an asymmetry
future edits can trip over.
**Recommendation:** reject `requestID > idMask` and overflowing payload lengths
in `buildFrame`; keep masking in `parseFrameHeader` alone.

**Verified sound (A2):** header layout is byte-exact vs the README wire example
(offsets 0/8/16, little-endian, response bit set/cleared, ID masked on parse,
payload length bounded on decode before allocation); the write gate is acquired
by every production writer with a consistent `mu`→`writeMu` order and no user
code ever runs while the gate is held; `writeFull` handles short writes,
error-after-partial, invalid counts, and zero progress, with every failure
routed to `selectTerminal`; `buildFrame` copies the payload (no caller-buffer
retention); teardown closes the stream exactly once and unblocks a concurrent
in-flight write per the `ByteStream` contract; selection losers never
double-close.

### 5.3 Runtime — receive, dispatch, calls, pending (A3)

See §4 for FA3-01 (P0), FA3-02 (P1), FA3-03 (P1, merged as FA1-03/FA2-01/FA3-03).

**FA1-04 [P3] — Gate waiters are blind to per-call cancellation and terminal
state.** Both `Call` step 6 and `handleRequest` admission block on `writeMu`
with a plain `Lock()` (`call.go:130`, `receive.go:98`); the SPEC's "acquires
the write gate while allowing either to win" is realized only as a recheck
*after* acquisition. If the gate holder is stalled inside `writeFull`, every
queued caller blocks indefinitely: a per-call context that fired long ago
cannot claim anything (no pending entry exists yet), and terminal selection
cannot wake the waiters either — only an external `Close` resolves the stall.
Per-call `ctx` cancellation is therefore honored only up to the gate, not at
the gate. This is the caller-side half of FA3-02's freeze; its fix (a
select-based gate wait watching `terminal` and `ctx.Done()`) is the same one.
**Why tests miss it:** gate tests use writers that always return; no suite
builds a transport whose `Write` blocks indefinitely.

**FA3-04 [P3] — `incoming` set is never drained at teardown.** `selectTerminal`
drains `c.pending` but not `c.incoming` (`connection.go:177-186`). Every
non-success handler path leaks its entry: the abandon path at admission
(`receive.go:101-104` — the *common* path for every in-flight handler when the
connection closes) and the write-failure path (`receive.go:107-110`) both
return without `releaseIncoming`. A handler that ignores cancellation retains
the whole `Connection` (its context carries it via `WithConnection`), so the
leaked set lives as long as the handler — possibly forever, unbounded by the
number of in-flight handlers at close.
**Recommendation:** drain `c.incoming` in `selectTerminal`'s teardown so
"terminal ⇒ both maps empty" holds.

**FB-02 [P1] — Requests read after terminal selection can still invoke
providers.** `receive.go:31-47,57-65,87-105`; `connection.go:176-200`:
terminal selection sets `cause`, but the receive loop does not check it
after `readFrame`, before reserving an incoming ID, or before dispatch. A
valid stream may unblock a read after `Close` by returning already buffered
frames, so `Close` can be followed by a newly launched handler and provider
side effects (with an already-cancelled handler context); a buffered backlog
also permits unbounded post-shutdown handler creation. This is the
admission-side complement of FA3-04 (which leaks the map entry; FB-02
creates the entry after terminal). **Recommendation:** stop processing
frames once terminal state is selected, and make incoming-ID reservation
reject terminal state before scheduling a handler.

**Verified sound (A3):** error taxonomy on receive/call paths matches SPEC
(transport errors prefixed and wrapping the stream error; framing and
matched-response failures wrap `ErrProtocol`; per-call cancellation returns the
exact `ctx.Err()` and never terminates the connection; terminal cause wins over
canceled context at every pre-admission check); lock order `mu`→`writeMu`
respected with no inversion (FA3-02 is a liveness issue, not an inversion); the
pending-map ownership model is implemented exactly as SPEC describes and the
exclusive-outcome invariant holds on every traced interleaving including
response-claims-during-write and admission-point teardown; request-ID
allocation is correct (monotonic 0..idMask, never reused,
`ErrRequestIDsExhausted` after the final ID, no allocation while waiting for
the gate, independent incoming/outgoing spaces, ID 0 legal); MSB-set IDs are
always classified as responses, so the incoming set never holds IDs ≥ 2^63;
handler panic containment is complete for dispatch and decoders — the only
unrecovered panic on the receive path is the allocation panic of FA3-01.

### 5.4 Syntax — scanner and parser (A4)

**FA4-01 [P2] — Unbounded type-nesting recursion → fatal unrecoverable stack
overflow.** The grammar has no depth limit (deliberate; `parse_test.go:501-505`
tests only 5000 levels), but every phase recurses: Parse
(`parse.go:247-280`, two frames per `list`), Validate (`validate.go:130-147`),
AttachDocs (`docs.go:57-140`), Format (`format.go:127-167`). At ~150–300 bytes
of stack per `list` level, Go's hard 1 GB goroutine-stack cap is reached near
4–10 million levels — about 25–50 MB of input. Exceeding it is
`runtime.throw("goroutine stack exceeds 1000000000-byte limit")`, a fatal error
no `recover` can catch; the process dies without a diagnostic, contradicting
"Parse never panics on input bytes" (`parse.go:38`). Fuzz cannot reach the
threshold by construction (input-size caps).
**Recommendation:** impose a bounded nesting depth at parse time that yields a
normal `*Error` (e.g. "type nesting exceeds limit"), and/or convert the
recursive walks to iterative form. Reconcile with SPEC's "no policy resource
limits" note.

**FA4-02 [P2] — Two inconsistent line models.** The trailing-comment rule uses
`lineStarts`, which treats a bare `\r` as a line terminator (`docs.go:207-232`);
diagnostic positions use go/token semantics where only `\n` breaks a line and
`\r` is an ordinary byte (`position.go:27-28`). On a CR-only file, comment
attachment and diagnostics disagree about which line a comment is on — the same
file can attach a comment as leading (per the attachment model) while
diagnostics place it on a line where SPEC's trailing rule would apply. Every
fixture uses LF/CRLF, so no test exercises the divergence, which silently
changes `_intercallSemantic` canonical bodies depending on line-ending style.
**Recommendation:** use one line model package-wide (preferably go/token's,
which SPEC mandates for diagnostics), with bare-CR and mixed-ending fixtures.
(FA4-04 is the same root cause in the blank-line group rule.)

**FA4-03 [P3] — Fuzz asserts invariants only; the grammar oracle silently
skips.** `FuzzParse` checks only "no panic, offsets in range, spans in range";
`FuzzParseFormat` checks round-trip idempotence. Neither checks accept/reject
against the README grammar — a parser that accepted `@` as a token or rejected
`procedure ping {};` would pass both. The only accept/reject oracle is the Lua
differential test, which `t.Skip`s without lua+LPeg (`lua_test.go:33`) and is
non-normative anyway. The reviewer hand-walked the grammar and found it
complete today; the risk is regression.
**Recommendation:** add a property test deriving accept/reject from the grammar
(independently of the parser), and consider making the Lua differential a
required CI step where installable.

**FB-09 [P2] — Documentation attachment discards a valid type-position
comment on a shared line.** `docs.go:180-203`; regression expectation in
`docs_test.go:187-189` (pins `type doc ""`): `isTrailing` classifies a
comment as trailing if *any* earlier completed node ends on its line, so
this valid input loses the documentation slot required for the second
declaration's type:

```intercall
type a uint8; type b /* doc */ a;
```

The comment sits directly between a type-declaration name and its type — an
explicit type-documentation anchor in `SPEC.md` — but canonical formatting
removes it. The same issue affects parameter/field type positions and
list-element documentation after a prior same-line node. This is a distinct
mechanism from FA4-02/FA4-04 (line-model divergence): the line model is
consistent here; the *trailing classification* is too coarse.
**Recommendation:** determine trailing status relative to the current
syntactic construct/anchor rather than any earlier completed node.

**Verified sound (A4):** grammar completeness — every README production is
implemented exactly, every traced non-form rejected at the correct offset;
reserved words are exactly the README list; scanner is panic-safe byte-for-byte
(every path advances or records a sticky error); invalid UTF-8 always reported
at the first bad byte before the unterminated-comment error; truncated BOM
falls into the invalid-UTF-8 path; BOM inside a comment correctly allowed;
positions match go/token semantics for LF, CRLF, bare CR, tab, multibyte runs,
and EOF at `len(src)`; scanner errors always win over grammar errors; `bail()`'s
internal panic is unreachable; AST fidelity confirmed (source-order decls,
half-open spans, idempotent AttachDocs, Format matches SPEC goldens); FNV-0
keys over `kind + " " + name` match README vectors and the independent
`foldString` oracle, with zero-key and cross-kind collisions rejected.

### 5.5 Tool — discovery, directives, selection (A6)

*All findings in this section were empirically confirmed by the reviewer with
`go test -overlay` and standalone /tmp programs.*

**FA6-01 [P1] — Cross-FileSet token positions corrupt mapping diagnostics.**
Explicit-package `Document`s are parsed into a fresh per-file `token.FileSet`
(base 1) while `pkg.Syntax` nodes carry positions from the go/packages load
FileSet (one shared fset whose bases accumulate over every dependency, reaching
millions). `errAt`/`mErr`/`mapNamedType` compute
`doc.Position(doc.offset(loadPos))`, subtracting the doc base from a load pos;
`token.File.Offset` clamps out-of-range results to `[0, size]` silently.
Result: mapping-phase diagnostics report the wrong line — for load bases above
file size, always *last line, column 1* (verified: a real error at `p.go:6:36`
reported as `p.go:7:1`). The fallback branch (`pkg.Fset.PositionFor(pos, false)`,
`mapping.go:391`) is correct but only reached for dependency packages.
`NamedType.Pos` (`mapping.go:772`) and `GoDecl.Pos` (`export.go:313`) record the
same corrupted positions; the synthesizer verified positions are
diagnostics-only today (no emit path writes them into generated artifacts), so
severity stays P1 — but any future persistence of recorded positions becomes
silently wrong.
**Recommendation:** for explicit packages, always use the load fset
(`PositionFor`) for line/column while keeping the doc's logical path, or parse
documents into the load's fset so bases coincide.

**FA6-02 [P1] — An existing output package that does not type-check is accepted
as "importable".** `outputMode` is `NeedName | NeedFiles` (`discover.go:45`) —
no type-checking bits. `go list` alone reports only build-level errors, so an
existing output directory containing a type error or syntax error loads with an
empty `Errors` list, passes the checks, and the generator overwrites
`binding_gen.go` inside a package that cannot compile. The failure surfaces
only at a later `go build`. SPEC requires "The export output directory must
resolve to an importable package". Tests cover only the "no Go files" case.
**Recommendation:** give `outputMode` the type-checking bits (`NeedTypes |
NeedTypesInfo | NeedSyntax`).

**FA6-03 [P1] — Var-group doc inheritance tags every spec: one directive
silently declares several sentinels.** `buildSpec` (`gosource.go:415-431`)
falls back from `spec.Doc` to `decl.Doc` for *every* `ValueSpec` of a
parenthesized var group, claiming "following go/doc's rule". go/doc's actual
rule (verified with Go 1.26.5) attaches a var-group doc to the group as one
entry; individual specs get nothing. Consequence: `// @intercall exception`
above `var (A error; B error)` yields two `GoDecl`s, each carrying the
directive, and `collectExceptions` registers **two application exceptions from
one directive** — with an explicit wire name, surfacing only as a baffling late
"wire name collision" instead of a clear contradiction. (Type groups are fine:
go/doc does attach group docs to every type.)
**Recommendation:** inherit var-group docs only for single-spec groups, or
reject the directive when the group contains more than one variable — i.e.,
match go/doc exactly.

**FA6-04 [P2] — Parenthesized named struct types are wrongly rejected for
`@intercall exception`.** `type A (struct{ ... })` is legal Go, but
`isStructType` (`gosource.go:493-496`) matches only `*ast.StructType`; the
parser preserves parentheses as `*ast.ParenExpr`, so the directive fails with
"contradictory … applies only to a named struct type" — a wrong rejection with
a misleading message. (`mapValue`'s `ParenExpr` case at `mapping.go:469` does
unwrap, so the pipeline is internally inconsistent; `export.go:372`'s
`spec.Type.(*ast.StructType)` assertion would produce an "internal error" if
the check were ever bypassed.)
**Recommendation:** unwrap `*ast.ParenExpr` in `isStructType` and defensively in
`collectExceptions`/`buildFields`.

**FA6-05 [P3] — A data result of type `error` passes the signature check.**
`buildProvider` (`select.go:380-395`) accepts `func(ctx) (error, error)`
without checking the data result's type; the rejection happens later in the
mapping phase with the generic "interface types are not wire values" message,
so the signature-validation phase fails to enforce its own contract (SPEC: the
predeclared `error` is legal only as the mandatory final result) and the
near-miss is diagnosed in the wrong phase — at a corrupted position (FA6-01).
**Recommendation:** reject in `buildProvider` ("the data result must not be the
predeclared error interface").

**FA6-06 [P3] — Directive grammar is enforced on generated files.**
`parseDocuments` (`discover.go:404-424`) runs full directive parsing over every
compiled file, including files recognized by the standard generated-file
marker — which SPEC says "do not supply selectable procedures or application
exceptions". A generated file whose doc comment contains a line like
`// @intercall frobnicate` (third-party generated code mentioning the protocol)
aborts the whole export. Arguably per-SPEC ("unknown directives are errors" is
unconditional), but the generated-file carve-out suggests generated content
should be inert. **Recommendation:** decide and document; pin with a test.

**FA6-07 [P3] — `packageError` builds logical paths from the basename only**
(`discover.go:362-375`). For cgo packages, `CompiledGoFiles` includes
cache-dir files (`_cgo_gotypes.go`), so the reported logical path doesn't
exist; `splitFilePos`'s last-two-colons split mis-parses colon-containing
directory names. **Recommendation:** derive the package-relative name from the
load fset and package dir.

**FA6-08 [P3] — Only one diagnostic per phase is ever surfaced.** `Discover`,
`parseDocuments`, `MapExport`, and `GenerateImport` all collapse their
collections through `firstError` (`diagnostics.go:36-42`); the SPEC's
multi-diagnostic sort is exercised only by CLI grammar diagnostics. Users fix
errors one at a time. **Recommendation:** print the full sorted collection, or
amend SPEC to first-error-per-phase.

**FA6-09 [P3] — `IntercallGenerated` marker detection is byte-fragile.**
Exact first-line string comparison (`gosource.go:306-318`): a UTF-8 BOM (which
the scanner skips, so `ast.IsGenerated` still fires) or a leading blank line
makes the marker false while `Generated` is true — the valid generated artifact
then fails export with "must have exactly one @intercall type directive".
**Recommendation:** strip a leading BOM (and optionally blank lines) before
comparison.

**FA6-10 [P3] — Excluding a tagged generic function silently succeeds**,
contradicting SPEC's "generic selectors are errors"; `ExcludesSkipSignatureChecks`
blesses the deviation. Behaviorally benign. **Recommendation:** reconcile SPEC
or reject generic excludes too.

**FA6-11 [P3] — `isExported` is ASCII-only.** Go exportness is Unicode-aware
(`func École(...)` is exported), but `directives.go:560-564`/`select.go:161-166`
check only `A-Z`, producing a wrong "applies only to an exported function"
contradiction. (The conversion pipeline is ASCII-only per SPEC, so such a
function couldn't be exported without a wire-name directive — but the message
is wrong about exportness.) **Recommendation:** use `token.IsExported` for the
contradiction check, ASCII rules only for name conversion.

**FA6-12 [P3] — `Syntax`/`CompiledGoFiles` positional correspondence is an
unguarded invariant** (`mapping.go:277-289`); a future go/packages driver could
break it, silently misattributing documents and positions to the wrong files.
**Recommendation:** pair by file name or `token.File` identity and error on
mismatch.

**FB-03 [P1] — Import/export generation accepts inputs that produce
uncompilable Go.** `override.go:182-225`; `import.go:338-344,502-584`;
`export_emit.go:193-209,556-560`; `artifact.go:480-526`: the generator
validates syntax but does not reserve all generated names or type-check its
output. `procedure import_binding {};` projects to `ImportBinding`,
colliding with the mandatory generated accessor; a `--go-name` parameter
override can equal a private generated parameter, e.g.
`procedure:ping/param:value=_intercallctx_01508600011dfaaf`, producing
duplicate parameters; a provider package named `ExportBinding` can be
imported without an alias, colliding with generated `func ExportBinding`.
Each case can make `intercall-go` exit successfully while the generated
package fails `go test` — the generator-side sibling of FA6-02 (which
accepts an *existing* untype-checkable output package). **Recommendation:**
reserve the complete package/local generated namespace during projection and
type-check the staged generated source before writing it.

**FB-04 [P1] — An unmatched export operand can be silently ignored.**
`discover.go:141-153`: `Discover` rejects only when **all** patterns yield
no packages. With one valid package pattern and one unmatched wildcard
pattern, `go/packages` may return only the valid root and the command
succeeds, although `SPEC.md` requires every pattern to match at least one
package. **Recommendation:** load/validate patterns individually, or retain
per-pattern match accounting and reject every unmatched operand.

**FB-05 [P1] — Ownership validation does not require an existing interface
body to be canonical.** `artifact.go:159-189,774-782`:
`parseInterfaceOwnership` verifies only that the marker's SHA-256 matches
the bytes after the blank line; it never parses, validates, or reformats
those bytes to establish that they are the canonical interface body required
by `SPEC.md`. Editing an owned interface's formatting/body and recomputing
the public marker digest makes it "owned" and lets export overwrite it,
rather than rejecting the altered artifact. **Recommendation:** parse /
validate / format the existing body and require byte equality with its
canonical rendering before treating it as replaceable.

**FB-07 [P2] — Marked generated metadata can be malformed without
rejection.** `metadata.go:195-230`; `mapping.go:807-810`: for a marked
generated dependency, `buildMachineTable` deliberately ignores malformed
`@intercall type` lines on an unreached type; the mapper only discovers the
problem if it subsequently reaches that type. `SPEC.md` defines the
generated-file marker as the trust boundary and requires malformed machine
metadata in a marked file to be an error, not ordinary prose (contrast
FA6-06, which errs on the opposite side for generated files).
**Recommendation:** validate every machine directive and table invariant
when recovering marked-file metadata, not only those needed by the current
traversal.

---

## 6. Elegance and UX

The user asked for elegance and UX to be part of the final assessment. The
surviving reviewers covered this only indirectly (the dedicated elegance/UX
agent was lost), so this section synthesizes their findings plus the
synthesizer's first-hand reading of the runtime. It is deliberately more
subjective than the rest of this document.

### What is genuinely elegant

- **The pending-map ownership model** (presence = eligible; removal = exclusive
  ownership transfer) is a beautiful simplification: no registered/writing/
  waiting/claimed enum states, no tombstones, and the exactly-once invariant
  falls out of one lock-protected map operation. The same design DNA shows in
  the `cause`-guarded single-winner teardown and the "no ID allocated while
  waiting for the gate" admission contract (call.go's step-by-step doc comment
  is a model of contract prose — SPEC section numbers, ordering, and failure
  modes all in one place).
- **Doc-comment discipline** across the runtime is exceptional: every exported
  symbol carries a contract-level comment, and the receiver/step numbering in
  `Call` makes the SPEC ordering contract auditable at a glance.
- **Error taxonomy discipline**: fixed sentinel texts, `errors.Is`/`%w`
  everywhere, terminal causes always wrapped with an operation prefix, and the
  deliberate suppression of stream-cleanup errors — small decisions, applied
  consistently.

### What is awkward (findings, in elegance terms)

- **Dead `writeFrame` helper with divergent inline twins** (FA1-06/FA2-04/
  FA3-05): the only function that documents the gate discipline is used by no
  production path; the two real writers hand-roll the gate inline and have
  already drifted apart (one rechecks `ctx.Err()`, the other `cause`; one
  releases the incoming ID on success, the other has the FA3-04 leak). Three
  reviewers independently flagged it — a maintenance trap that is exactly as
  dangerous as it looks.
- **Encoder/decoder/dispatch panic asymmetry** (FA1-05): one defect class,
  three failure modes, none of them documented. The runtime's otherwise
  meticulous failure-modeling stops at the encoder boundary.
- **`writeFull`'s partial-write message** (FA2-03) misreports the byte counts —
  a small UX lie in the one place a user goes to diagnose a half-delivered
  frame.
- **CLI diagnostics**: one error per run (FA6-08) makes fixing a broken
  provider package a whack-a-mole session; combined with FA6-01's wrong
  positions, the *first* error a user sees can point at the wrong line of the
  wrong file.
- **Generated-file marker fragility** (FA6-09) and ASCII-only exportness
  (FA6-11) are small sharp edges on otherwise smooth tooling.
- **`Close()`'s synchronous teardown** (FA1-02) is an API-surprise risk:
  "returns immediately" is only true for prompt transports, and GO.md promises
  exactly what the API may not deliver.

### Net assessment

For a proof of concept, the runtime API is unusually well-shaped: small,
obvious, hard-to-misuse surface (`NewConnection`, `Call`, `Close`, `Wait`,
immutable binding pair), with the complexity correctly hidden behind the
generated-code SPI. The elegance gaps above are mostly *internal* (dead code,
drift between twin write paths) or *diagnostic* (messages, positions, one-error
reporting) rather than architectural. The one UX area with a real contract
issue is `Close()`'s blocking behavior.

---

## 7. Verified-sound areas (what the suites *and* this review agree is right)

These were checked production-by-production or interleaving-by-interleaving and
found correct; they are recorded so future work does not re-litigate them:

- **Wire format:** frame layout byte-exact vs README (24-byte header, LE
  uint64s at 0/8/16, MSB kind bit, 63-bit IDs, ID 0 valid, independent
  direction ID spaces).
- **Write path:** gate serialization, no frame interleaving, short-write
  loops, no caller-buffer retention, exactly-once stream close by the
  selection winner.
- **Ownership invariants:** pending exactly-once outcomes under all traced
  interleavings (response / cancellation / teardown mutual exclusion by map
  removal); single-winner terminal selection; `mu`→`writeMu` lock order with
  no inversion anywhere.
- **Error taxonomy:** six local + three wire sentinels match SPEC texts and
  `errors.Is` behavior; terminal wrapping discipline consistent.
- **Grammar/scanner/positions:** full README grammar accepted exactly;
  scanner panic-safe; UTF-8/BOM/CRLF handling verified incl. first-bad-byte
  reporting; positions match go/token semantics; parser errors point at the
  offending token.
- **Keys:** FNV-0 over `kind + " " + name` matches README vectors and an
  independent oracle; zero-key and cross-kind collisions rejected.
- **ID allocation:** monotonic, never-reused outgoing IDs; exhaustion handling;
  no allocation while waiting for the gate; MSB classification prevents
  incoming-set contamination.

---

## 8. Coverage gaps

The fleet lost one reviewer before it could write (syntax validation / FNV-0
keys / semantic docs / canonical formatting — partially compensated: A4's
verification notes cover keys and Format goldens at a high level, but
`validate.go` semantics, `docs.go` normalization, and `format.go`
round-trip edge cases were not deep-reviewed), and the planned second wave
(mapping/naming, import generator, export generator + artifacts, CLI,
test-quality audits ×2, spec-compliance cross-cut, robustness cross-cut,
docs accuracy, elegance/UX cross-cut) never launched as a fleet. A
follow-up review (wave 2, FB-01…FB-09) subsequently covered the mapping /
generator / artifact internals and syntax documentation attachment; CLI,
test-quality, and cross-cut sweeps remain open. Areas with **no or partial
coverage**:

1. `internal/syntax` `validate.go` / `docs.go` / `format.go` deep review —
   **partially covered by wave 2**: FB-09 (documentation attachment) joins
   wave 1's FA4-01/FA4-02/FA4-04; `validate.go` semantics and `format.go`
   round-trip edge cases remain uncovered.
2. `internal/tool` mapping/naming (`mapping.go`, `name.go`, `mangle.go`,
   `initialisms.go`, `model.go`, `metadata.go`) — **partially covered by
   wave 2**: FB-07 (marked-file metadata validation) beyond
   FA6-01/FA6-05/FA6-12; naming/mangle/initialisms/model remain uncovered.
3. Import/export generator emission (`import.go`, `emit.go`, `export*.go`,
   `codec.go`) and artifact safety (`artifact.go`: atomicity, permissions,
   interrupted writes) — **partially covered by wave 2**: FB-03
   (generated-name reservation / type-checking of staged output), FB-04
   (per-pattern export matching), FB-05 (canonical ownership validation);
   atomicity, permissions, and interrupted-write behavior were not
   independently reviewed.
4. `cmd/intercall-go` CLI review (help text accuracy, flags, exit codes) —
   only FA6-08 touches it.
5. Test-quality audits (what the suites fail to assert, weak assertions,
   flakiness) — beyond FA4-03.
6. Cross-cutting spec-compliance sweep of README normative statements vs code
   (partially covered by A2/A3/A4 verification notes).
7. Docs accuracy (GO.md/SPEC.md claims vs behavior; GO.md:194 was caught by
   FA1-02).
8. Dedicated robustness sweep (parser, codecs, wire decode) beyond FA3-01/
   FA4-01.

The coverage-gap list doubles as a prioritized follow-up queue; the strongest
candidates are now (4) and (5), plus the remaining halves of (1)–(3).

---

## 9. Open questions

1. **FA6-01 persistence:** no emit path currently writes recorded positions
   into generated artifacts (verified), so severity is P1. If a future
   feature persists `GoDecl.Pos`/`NamedType.Pos` (re-export metadata, source
   maps), the cross-FileSet corruption becomes P0 — the fix should land before
   any such feature.
2. **FA6-03 policy:** should var-group doc inheritance be removed entirely
   (exact go/doc semantics) or should the sentinel check count variables
   across the whole group? The same ambiguity applies to `@intercall type` on
   multi-type groups with an explicit wire name.
3. **FA6-06 / FA6-08 policy:** is "directives are errors even in generated
   files" and "first-error-per-phase" the intended contract, or should SPEC be
   amended? Current tests pin both as incidental behavior.
4. **Encoder panics (FA1-05):** SPEC is silent; the runtime recovers decoder
   and dispatch panics. Which behavior is intended?
5. **FA4-01 policy:** SPEC says "no policy resource limits" — a nesting-depth
   cap is a stack-safety measure, not a resource policy, but the distinction
   should be stated in SPEC so it doesn't read as a contradiction.
6. **Write-gate interruptibility (FA3-02):** the SPEC acknowledges the
   uninterruptible gate wait ("No ID is allocated while waiting for the write
   gate"). Should the gate wait become select-based (terminal + ctx.Done()) in
   the spec'd contract, or is the stall documented as a PoC limitation?

---

## 10. Appendix — verification evidence

- **FA3-01/FA2-02 (P0):** source-verified `frame.go:88-94` (only `maxInt`
  guard before `make([]byte, int(length))`) and absence of `recover` on the
  receive path; allocation harness `/tmp/review/probe` reproduced the fatal
  `1<<45` OOM on the review host and the recoverable-but-unrecovered
  `makeslice` panic at higher sizes.
- **FA3-02 (P1):** source-verified `call.go:129-130` / `receive.go:97-98`
  (`mu` held across `writeMu.Lock()`) and `connection.go:170-171,198`
  (`selectTerminal` needs `mu`; it is the only stream close).
- **FA1-03/FA2-01/FA3-03 (P1):** source-verified `receive.go:105-111`
  (release after write return, no ordering edge) and `receive.go:43-45`
  (false terminal selection); README "Frames" reuse language quoted at
  README.md:469-470.
- **FA1-01 (P2):** reproduction harness `/tmp/nilcause` (custom context with
  closed `Done`/nil `Err`) demonstrated the stdlib
  `"context: internal error: missing cancel error"` panic in the
  runtime-spawned propagation goroutine.
- **FA6-01 … FA6-12:** empirically confirmed by the reviewing agent via
  `go test -overlay` and standalone programs outside the repo; synthesizer
  re-verified `mapping.go:382-391` (fallback branch correctness), the
  `outputMode` flags at `discover.go:45`, and the absence of position writes
  in emit paths.
- **All other findings:** source-verified at the cited lines by the
  synthesizer; test-blindness arguments checked against the named tests.
- **Wave 2 (FB-01…FB-09):** source-verified at the cited lines
  (`call.go:129-130`, `receive.go:97-99,31-47,57-65,87-105`,
  `connection.go:78-92,170-200`,
  `internal/tool/discover.go:141-153`,
  `internal/tool/artifact.go:159-189,480-526,774-782`,
  `internal/tool/override.go:182-225`,
  `internal/tool/import.go:338-344,502-584`,
  `internal/tool/export_emit.go:193-209,556-560`,
  `internal/tool/metadata.go:195-230`,
  `internal/tool/mapping.go:807-810`,
  `internal/syntax/docs.go:180-203`, regression pin at
  `internal/syntax/docs_test.go:187-189`); the FB-01 deadlock reproduction is
  specified (block writer A, start writer B, call `Close`). Wave 2
  validation: `/usr/local/go/bin/go test -count=1 ./...`,
  `go test -race -count=1 ./...`, `go vet ./...`, and `gofmt -l` all
  passed. The existing suites still do not cover the write-gate liveness
  cycle, complete generated-name reservation/type-checking, per-operand
  wildcard matching, or canonical verification of existing owned
  interfaces.
