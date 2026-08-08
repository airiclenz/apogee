# Parallel sub-agents — implementation plan

- **Goal:** run a reply's `sub_agent` calls concurrently at depth 0, bounded by a
  per-server **Parallel agents** cap (`parallel-agents:` pin, else `/props` `total_slots`
  discovery, else 1 = today's serial behavior), with per-child event attribution via the
  spawning call-ID, live per-child TUI blocks, queued approvals, and guided decomposition
  dispatching batches of the same cap.
- **Date:** 2026-08-07
- **Status:** not started
- **Authoritative sources:** ADR 0039 (the decision this plan implements), ADR 0013
  amendment 2026-08-07 (per-child atomicity, cancel), ADR 0014 amendment 2026-08-07
  (batch = cap), ADR 0024 (per-slot window honesty; beat rebind), ADR 0037 (settings
  live-apply). CONTEXT.md **Parallel agents** entry. Code line numbers below are as of
  commit `cd7b9c8`; `internal/tui` carries uncommitted edits, so TUI sites are cited by
  symbol, not line.
- **Skills:** coding-standards
- **Out of scope:** worktree/workspace isolation for racing sibling writers (parked, ADR
  0039 consequences), an all-depth shared slot budget (rejected for v1), flipping
  `guided_decomposition` on by default or running the ADR 0009 bench gate (the bench's
  call, after this lands), reading the launcher's `ProfileParams.Parallel` (rejected
  discovery source), any version identifier change (see closing note), the
  `2026-08-07 - 00` per-server llama-launcher plan (independent; this plan only copies its
  per-entry key idiom).

## Ratified design calls

Decided by the owner, 2026-08-07, via AskUserQuestion during the grill that produced ADR
0039 (calls 1–9); calls 10–12 bound at plan write time from existing documented postures:

1. **Trigger = engine + batched mechanism.** Concurrency fires when the model's own reply
   carries several `sub_agent` calls (structural, on under Bypass), AND guided
   decomposition dispatches `min(cap, remaining)` per Turn. Rejected: engine-only.
2. **Cap = pin, else discover, else 1.** `parallel-agents:` on the server entry is a pin
   discovery never overrides (the `context-window` idiom); absent → the live server's
   `/props` `total_slots`; no signal → 1. Rejected: discovery-over-pin ("fallback" shape).
3. **Depth 0 only.** A child's own delegations stay serial inline; no slot accounting, no
   deadlock. Rejected: all-depth shared budget.
4. **Siblings finish.** A child's failure (error, breaker, denied approval) becomes that
   child's tool result; no sibling cancellation. Rejected: fail-fast.
5. **Call-ID stamp.** `EventBase` gains the spawning `sub_agent` call's ID; per-child
   sinks rejected.
6. **TUI = live per-child blocks** grouped by call-ID, collapsed-cap live tail. Rejected:
   buffer-silently-until-done.
7. **Batch = the cap**, no separate mechanism knob; cap 1 = ratified ADR 0014 behavior.
8. **Name = `parallel-agents`** everywhere (key, glossary, ADR). Rejected:
   `parallel-delegations`.
9. **Docs written at decision time** (ADR 0039 + amendments + CONTEXT.md exist as of this
   plan's date); this plan implements them.
10. **Cancel is unchanged ADR 0013 §5:** Esc signals all in-flight children, waits, rolls
    the whole parent Turn back.
11. **Mixed replies: leaf tools first, in emitted order, then the fan-out** — a write a
    child depends on lands before children start; results map back by call ID.
12. **Approvals queue** through the wait-tolerant Approver, one prompt at a time, prompt
    names the asking child's task. `0` in `parallel-agents` reads as unset (yaml cannot
    see an explicit 0; negative values are refused).

## 1. Config: the per-entry `parallel-agents` key — ✅ DONE (2026-08-08)

**What:** Additive schema only — nothing reads the value yet.

- `cmd/apogee/config.go`: `serverEntry` (lines 928-933) gains `ParallelAgents int` with
  yaml tag `parallel-agents,omitempty`. Doc comment: the three states (absent/0 =
  discover, N ≥ 1 = pin), the per-slot-window trade in one line, and a cite of ADR 0039
  decision 2.
- `cmd/apogee/config.go`: `validateServers` (lines 936-964) refuses negative values with a
  message naming the entry index and name, matching the neighbouring checks' style.
- `cmd/apogee/defaults/config.yaml`: the `servers:` teaching block (lines 28-72) gains the
  key's prose: pin-else-discover-else-1, the "more parallel agents = smaller window each"
  warning, and an example entry line `# parallel-agents: 4`.

**Tests:** `cmd/apogee/config_test.go` — validateServers accepts absent, `0`, `1`, `4`;
refuses `-1` with a message naming the entry. Template↔schema guard suites
(`defaults_test.go`) stay green with the new prose.

**Acceptance:** `go build ./... && go test ./cmd/apogee/` (and `make check` before
commit).

**Commit:** `feat(config): add per-server parallel-agents key`

## 2. Discovery and resolution: the cap reaches the engine — ✅ DONE (2026-08-08)

Depends on item 1.

NOTES (2026-08-08): four deviations from this item's literal text, all recorded rather than assumed.
(a) The `/props` helper `discoverRuntimeContextWindow` is renamed `discoverProps` and returns both
facts — the same single request now answers two questions, and the old name would have lied about
the second. (b) The cap reaches the engine through a NEW anytime-safe mutator,
`Agent.SetParallelAgents` (plus the unexported `parallelAgentsCap()` read seam item 4 consumes), not
through `RebindSpec`: a `/server` switch installs the cap without any rebind, and this item's own
test asks for exactly that. (c) `resolveParallelAgents` sits beside `validateServers` in
`cmd/apogee/config.go` rather than at the cited `config.go:475`, which is inside the source-layer
struct's field docs and cannot hold a function; the composition-root holder that owns the pin and the
observed slot count is `parallelAgentsCap` in `cmd/apogee/upstream.go`, beside `sessionMover`.
(d) Discovery reaches that holder through a wrapper around the `tui.Options.Heartbeat` seam in
`wire.go` — the `Rebind` seam carries only model and window, and widening it is an `internal/tui`
change this item does not own. Startup and a first bind install through `serverBinder.bind` (which
seeds `Config.ParallelAgents` before the Agent exists); the `servers:` row's live apply re-resolves
through `parallelAgentsCap.relist`.

**What:** One new field read from a response Apogee already fetches, resolved
pin-else-discover-else-1, delivered to the engine the same way the runtime context window
is.

- `internal/provider/discovery.go`: the `/props` read (lines 98-137) also decodes
  `total_slots` (the struct at 130-137 gains the field); the discovery result carries it
  beside the runtime window. The existing test fixture at
  `internal/provider/discovery_test.go:215` already contains `total_slots` — assert it is
  now parsed.
- `internal/heartbeat/heartbeat.go`: the beat's observation carries `total_slots` through
  exactly like the runtime window, so a model/server rebind re-resolves the cap at the
  boundary (ADR 0024/0028 — no new beat machinery, one more field on the existing one).
- `cmd/apogee`: a resolver beside the window plumbing (the `context-window` pin path,
  `config.go:475` comment area): pinned `ParallelAgents` ≥ 1 wins; else discovered
  `total_slots` ≥ 1; else 1. The resolved cap is installed where the bound server's window
  is installed — on switch (`switchServer`), bind (`bindServer`), and startup — and
  live-applies with the `servers:` row (ADR 0037; the row's live-apply already re-reads
  entries, verify the cap re-resolves with it).
- `internal/domain/config.go`: `Config` gains `ParallelAgents int` (doc comment: "< 2
  means serial; the engine treats it as a width, never a promise"). The engine reads it in
  item 4.

**Tests:** provider — `/props` with and without `total_slots` (absent → 0). Heartbeat —
the field rides a beat. cmd — resolver table: pin beats discovery, discovery beats
default, absent-absent → 1; a `/server` switch to a pinned entry installs the pin.

**Acceptance:** `go build ./... && go test ./internal/provider/ ./internal/heartbeat/
./cmd/apogee/` (and `make check` before commit).

**Commit:** `feat(provider): discover total_slots and resolve the parallel-agents cap`

## 3. Event identity: the spawning call-ID on every child event — ✅ DONE (2026-08-08)

Independent of items 1–2 (pure engine/persistence plumbing; lands before item 4 so
concurrency is born attributable).

NOTES (2026-08-08): five deviations from this item's literal text, all recorded rather than assumed.
(a) The TUI member is named `spawnCallID` (entry + wire, `"spawnCallID,omitempty"`), not `callID`:
`entry.callID` already exists and means the entry's OWN tool call (a result pairs to it), so
re-using the name would conflate two different facts and a second `callID` is impossible. (b) NO
transcript blob version bump — "per its existing versioning rules" resolves to no bump: the file
states that rule three times over for its earlier additive members (`SkillSpans`, `CtxUsed`/
`CtxLimit`, `Solo`, `Quoted`), an omitempty member is invisible to an older build, and a bump would
make every blob this build writes UNREADABLE to any older one (the reject-forward rule) for zero
gain. Item 8's CHANGELOG line should therefore read "additive transcript member", not "version
bump". (c) Fold-time stamping of `entry.spawnCallID` is NOT done here: the transcript model still
appends entries without it, because threading the id through the six fold helpers IS item 6's
"groups Depth > 0 events by CallID" — item 3 lands the identity, the persistence and the round
trip. (d) `internal/domain/events.go`: `AuditEvent.CallID` (the audited call) now SHADOWS the
promoted `EventBase.CallID` (the spawning call). Both are documented on each other and pinned by
`TestAuditEventCallIDShadowsTheSpawningCall` rather than renaming an existing exported field.
(e) The planned "domain — base() stamps CallID" test lives in `internal/agent` (`base()` is an
Agent method); `internal/domain` got the shadowing and sibling-separation tests instead. Also:
`internal/tui/sink.go`'s token-coalescing doc was corrected — its buffer key is the whole
`EventBase`, so concurrent siblings' tokens now split buffers for free.

**What:**

- `internal/domain/events.go`: `EventBase` (lines 21-34) gains `CallID string` — the ID of
  the `sub_agent` tool call that spawned the emitting agent; empty at depth 0. Stamped in
  `base()` exactly as `Depth` is: the `Agent` carries it as a construction-time field.
- `internal/agent/subagent.go`: `newChildAgent` (lines 129-169) threads the spawning
  call's ID into the child; `runSubAgent` (lines 59-112) passes it from the dispatch-site
  tool call.
- `internal/run/run.go`: `eventTap` (lines 289-336) keys `SubAgentUsage` (lines 88-104) by
  `CallID` instead of the serial-assumption it documents at lines 310-313; delete the
  stale comment. Headless per-run stderr lines (`cmd/apogee/headless.go:424-450`) keep
  their shape — the key change is internal.
- `internal/tui/transcriptcodec.go`: persist `CallID` on sub-agent entries additively
  (transcript blob version bump per its existing versioning rules); decoder tolerates its
  absence in old blobs.

**Tests:** domain — `base()` stamps `CallID` at depth > 0, empty at 0. agent — a child's
events all carry the spawning call's ID; two sequential children carry different IDs. run —
usage attributes to the right `CallID` with two children in one Turn. tui codec —
round-trip with and without the field.

**Acceptance:** `go build ./... && go test ./internal/domain/ ./internal/agent/
./internal/run/ ./internal/tui/` (and `make check` before commit).

**Commit:** `feat(events): stamp the spawning sub_agent call-ID on child events`

## 4. Engine: concurrent depth-0 fan-out — ✅ DONE (2026-08-08)

Depends on items 2 and 3. The heart of the plan; if a SPLIT is needed, split the work at a
checkpoint, not this item's scope.

NOTES (2026-08-08): four deviations from this item's literal text, all recorded rather than assumed.
(a) The partition is applied UNCONDITIONALLY — a mixed reply's leaf tools run before its
delegations at EVERY width, not only when the group then fans out. "Cap 1 must not change
behavior" is honoured for the execution PATH (each call takes byte-for-byte the old route, and a
delegation group at width 1 runs through the same serial loop), but a mixed reply's dispatch ORDER
now follows design call 11 at every cap: making the order depend on the bound server's slot count
would mean the same reply produces a different history on different servers, which no test could
pin and no user could predict. (b) Only the CHILD RUN is concurrent. The pre-tool-exec hooks, the
guardrail probe and the Resolution, the audit record, the self-regulation signal, the
post-tool-result hooks and the append into history all stay on the dispatching goroutine, in
emitted-call order, on either side of the pool — this is what keeps the parent's registry, guards,
tracker and conversation single-goroutine, and it satisfies the item's "each driving `runSubAgent`
for one call" literally. One consequence is named in the code: siblings are resolved against the
same guardrail state, so a delegation cannot observe a breaker its sibling tripped. (c) The
"one mutex-guarded emit seam" is installed at CONSTRUCTION — `newAgent` wraps `Config.Events` in an
idempotent `serialEventSink` — rather than at the ~20 emit sites; `newChildAgent`'s Config copy
means the parent, every child and every descendant then share ONE mutex. (d) Per-child panic
recovery is ADDED, in the pool worker only (`runDelegation`): the delegate path had no recover of
its own before (the existing ones are `executeTool`'s and `recoverHook`'s), and it needs one now
because a panic crossing a goroutine's top frame kills the process. The serial path is left exactly
as it was, so cap 1 keeps its old behavior there too.

HAZARD for item 7 / later work (not this item's scope): `newChildAgent` shares the parent's
`MechanismRegistry` — and therefore the same Mechanism INSTANCES — with every child (ADR 0013's
default). Concurrent siblings running a stateful Mechanism would touch one instance from two
goroutines. Nothing is armed by default today, so no live path races; arming `guided_decomposition`
(item 7) alongside a fan-out needs per-child instances or a lock.

**What:**

- `internal/agent/dispatch.go`: the sequential loop (lines 41-66) partitions a reply's
  calls per design call 11 — leaf tools first, in emitted order, exactly today's path;
  then the `sub_agent` group. At `Depth == 0` with `Config.ParallelAgents ≥ 2` and ≥ 2
  sub-agent calls, the group runs through a bounded worker pool of `min(cap, len(group))`
  goroutines, each driving `runSubAgent` for one call; otherwise the group runs serially
  inline (the existing path, byte-for-byte — cap 1, any depth > 0, or a single call must
  not change behavior).
- Results land in per-call slots and are appended to history in emitted-call order
  (deterministic history regardless of completion order; results map by call ID on the
  wire anyway).
- **Failures are independent** (design call 4): a child's error/breaker/denial becomes its
  tool result; the pool never cancels siblings.
- **Cancel** (design call 10): the parent's cancellation signal reaches every in-flight
  child's context; the pool waits for all children to reach a boundary, then the
  orchestrator surfaces `dispatchCancelled` and the parent Turn rolls back — the ADR 0013
  §5 path, now N-wide.
- **Sink serialization:** child emission into the parent's `EventSink` goes through one
  mutex-guarded emit seam (document on `EventSink` in `internal/domain/events.go` that the
  engine serializes concurrent emission; drivers still receive a linear stream).
- Per-child panic recovery stays at each child's own boundary (ADR 0007) — verify the
  recover sits inside the goroutine.
- `internal/agent/agent.go:27`'s "not safe for concurrent use" comment gains the
  clarification: the *parent* loop is single-goroutine; the fan-out pool is internal to
  one dispatch.

**Tests:** all under `-race`. Two fake-upstream children run concurrently (observe
overlapping starts) and their results land in call order; cap 1 forces the serial path;
depth-1 fan-out stays serial; one child erroring leaves the sibling's result intact;
cancel mid-fan-out stops both children and rolls the Turn back with no partial history;
sink receives a linear, correctly-stamped stream under concurrent emission; a child panic
recovers without killing the sibling or the parent Exchange.

**Acceptance:** `go build ./... && go test -race ./internal/agent/ ./internal/...` (and
`make check` before commit).

**Commit:** `feat(agent): concurrent depth-0 sub-agent fan-out bounded by parallel-agents`

## 5. Approvals: queued, child-named prompts — ✅ DONE (2026-08-08)

Depends on item 4.

NOTES (2026-08-08): four deviations from this item's literal text, all recorded rather than assumed.
(a) The serializing seam is installed in the ENGINE, not at the TUI adapter: `newAgent` wraps
`Config.Approver` in an idempotent `queuedApprovals` seam (`internal/agent/construct.go`), the exact
counterpart of item 4's `serializedEvents`, so the parent and every descendant queue against ONE slot
and every Driver — TUI, bench, a future daemon — gets one-prompt-at-a-time without building a queue
of its own (ADR 0031). `domain.Approver` now documents that guarantee. `internal/tui` therefore needs
no concurrency change at all, which is stronger than the item's "not a TUI change". The wait is a
channel rather than a mutex because it must be ctx-AWARE: a sibling queued behind the visible prompt
has to give up when the Turn is cancelled instead of handing the driver a request for a rolled-back
Turn. (b) The task reaches the prompt by being threaded into the child at CONSTRUCTION —
`newChildAgent(spawnCallID, task)` → `Agent.task` → `ApprovalRequest.SubAgentTask` — not by a
CallID → spawning-arguments lookup as the item's parenthetical suggests: it is the same seam the
call-ID already uses, and it needs no live registry of in-flight call arguments. (c) The item names
no render site for "the prompt carries the asking child's task", so one was added: the approval pane
leads its body with `Sub-agent: <task>` when the field is non-empty (`internal/tui/model.go`), and
`layout.md` gained the prose — a depth-0 prompt is unchanged to the byte. (d) That line is CLIPPED
(`approvalTaskClipRunes`) rather than wrapped in full like the Reason, a deliberate departure from
the pane's wrap-everything rule: the task says who is asking, and who is asking must not be able to
push what is being decided off the screen.

**What:** The Approver contract is wait-tolerant (ADR 0031); what concurrency adds is
simultaneous *requests* and an anonymous prompt.

- Verify (and where missing, enforce with a serializing seam) that concurrent approval
  requests from sibling children queue: one prompt visible at a time, the asking child
  blocked, siblings running. The TUI approver seam (`internal/tui`, the approval prompt
  path) must be safe for concurrent callers — likely a channel/mutex at the engine-side
  approver adapter, not a TUI change.
- The approval prompt carries the asking child's task (from the spawning `sub_agent` call
  arguments, reachable via the `CallID` stamped in item 3), so "which agent is asking" is
  answerable at a glance.

**Tests:** `-race`: two children raising approvals concurrently produce two sequential
prompts, each naming its child's task; answers route to the right child (approve one, deny
the other, assert each child saw its own verdict).

**Acceptance:** `go build ./... && go test -race ./internal/agent/ ./internal/tui/` (and
`make check` before commit).

**Commit:** `feat(approval): serialize concurrent child approvals with child-named prompts`

## 6. TUI: live per-child blocks

Depends on items 3 and 4. Cited by symbol — `internal/tui` carries uncommitted edits at
plan time.

**What:**

- The transcript model groups `Depth > 0` events by `CallID` into one block per child,
  ordered by emitted-call order (first `sub_agent` call = first block), instead of
  assuming a contiguous serial stream. Serial sessions (one call, cap 1) must render
  byte-identically to today.
- While a child runs, its block renders under the existing collapsed cap (header + three
  content rows, commit `cd7b9c8`'s machinery in `render.go` / `toolpresent.go`) showing
  the live tail of that child's activity; on completion the block becomes the normal
  collapsed sub-agent call block (`N tool calls · 12k/32k · <gist>`). Expand/collapse
  works as for any tool block (mouse and keyboard paths — `mouse_test.go` patterns).
- `layout.md` gains the per-child-block prose (the TUI spec owns rendering rules).

**Tests:** `internal/tui` — interleaved two-child event streams group into two blocks in
call order; a running child shows a capped live tail; the serial single-child case renders
as before (golden/snapshot equality); collapse/expand on a per-child block; codec
round-trip renders groups after resume.

**Acceptance:** `go build ./... && go test ./internal/tui/` (and `make check` before
commit).

**Commit:** `feat(tui): live per-child blocks for concurrent sub-agent fan-out`

## 7. Guided decomposition: batch = min(cap, remaining)

Depends on item 4.

**What:** ADR 0014 amendment 2026-08-07, mechanically.

- `internal/mechanisms/guided_decomposition.go`: `PostResponse` (lines 349-385)
  synthesizes the first `min(cap, remaining)` `sub_agent` calls (today: exactly one, lines
  361-366); the deferred directive carries the remainder as before. The follow-through's
  re-derivation consumes up to a batch per Turn; its existing exact-match, consume-once
  cursor rules (ADR 0014 addendum 2026-07-06) apply per item unchanged.
- The cap reaches the mechanism the same way `Depth` does (the `Request.SetDepth` seam
  precedent — a `SetParallelAgents` sibling or a widened seam, implementer's call; no new
  Upstream call, no second spawn path).
- Cap 1 must reproduce today's behavior byte-for-byte (the serialized floor); the
  bounds constants (`guidedDecompositionMaxSubtasks` etc.) are untouched.
- CHANGELOG note (item 8) states the stack must re-pass the ADR 0009 gate — this item
  changes no default.

**Tests:** existing guided-decomposition suite stays green with cap 1 (unchanged
behavior); cap 3 with a 7-item enumeration dispatches 3+3+1 across three Turns with a
quiescent boundary between batches; suppression mid-queue abandons cleanly as today;
off-script-Turn re-defer still carries the intact remainder.

**Acceptance:** `go build ./... && go test ./internal/mechanisms/` (and `make check`
before commit).

**Commit:** `feat(mechanisms): guided decomposition dispatches parallel-agents-sized batches`

## 8. Docs: CHANGELOG and drift sweep

Depends on items 1–7. ADR 0039, the ADR 0013/0014 amendments, and the CONTEXT.md entries
were written at decision time (2026-08-07) — this item closes the loop, it does not
re-write them.

**What:**

- `CHANGELOG.md`: entry under the unreleased heading — the per-server `parallel-agents`
  key, `/props` `total_slots` discovery, concurrent depth-0 fan-out, per-child TUI blocks,
  call-ID event attribution (transcript blob version bump), batched guided decomposition
  with the re-gate note. Do NOT touch `VERSION` or any release heading.
- Drift sweep: `README.md` (if it describes sub-agent execution), `layout.md` (item 6
  should have landed its prose — verify), `docs/design/mechanism-catalogue` (if it states
  one-per-Turn), CONTEXT.md transcript-blob line (should mention the call-ID — added at
  decision time, verify).

**Tests:** none (docs). **Acceptance:** `make check` green;
`grep -rn "one per Turn" CONTEXT.md docs/design/` shows no stale serialized-only claims;
CHANGELOG diff touches only the unreleased section.

**Commit:** `docs: record parallel sub-agents across CHANGELOG and drifted prose`

## Suggested version bump

Minor (0.x line): a new capability with an additive config key and an additive transcript
blob version bump; no schema break. Suggestion only — whether and when to bump is the
owner's call; no item touches a version identifier.
