---
Status: accepted
---

# Delegations fan out concurrently, bounded by the server's parallel-agents cap

## Context

Every sub-agent today runs strictly serially: dispatch executes a reply's tool calls in
order ([ADR 0013](0013-the-sub-agent-orchestrator-is-the-recursion-point-with-isolated-live-guard-state.md)
runs the nested `Agent` inline on the caller's goroutine), and guided decomposition
deliberately dispatches **one child per Turn**
([ADR 0014](0014-guided-decomposition-steers-the-primary-call-and-serializes-delegation.md) §3).
Meanwhile a llama.cpp server started with `--parallel N` holds N idle slots Apogee never
uses, and a cloud endpoint can take many concurrent requests. A model that emits three
`sub_agent` calls in one reply has already chosen N-in-one-Turn; running them one after
another is pure wall-clock waste the model did not ask for.

The grill (2026-08-07) surfaced the constraints that force the shape:

- **The server's parallelism is discoverable but ignored.** llama.cpp's `/props` reports
  `total_slots`; Apogee already fetches `/props` every discovery pass and heartbeat
  ([ADR 0024](0024-the-heartbeat-observes-upstream-and-rebind-applies-at-the-boundary.md))
  and reads only `n_ctx`. Cloud servers report nothing.
- **`/props` `n_ctx` is the PER-SLOT share** (ADR 0024): raising `--parallel` shrinks every
  agent's window. Concurrency and window size trade off at the server, not in Apogee.
- **An unbounded shared slot budget deadlocks.** With one budget across depths, N children
  can hold all N slots while each blocks waiting for a slot for its own children — a
  textbook semaphore deadlock; escaping it needs release-while-delegating accounting.
- **Interleaved child events are indistinguishable.** Events carry only `Depth`
  (`internal/domain/events.go`); serialized execution made depth sufficient. The code
  already flags the gap: `internal/run/run.go` notes concurrent fan-out would need a run
  identity on `UsageEvent`.
- **The hard invariant binds** ([ADR 0006](0006-bypass-mode-is-the-mechanisms-off-floor.md),
  [ADR 0009](0009-the-ab-decision-rule.md)): any change to guided decomposition's dispatch
  shape must re-pass the bench gate before defaulting on.

## Decision

**Model-emitted delegations in one Turn execute concurrently at depth 0, bounded by a
per-server `parallel-agents` cap that is a config pin when set and a `/props` discovery
otherwise; guided decomposition dispatches batches of the same cap.** Concretely:

**1 — Two triggers, one width.** (a) *Engine, structural:* when the model's own reply
carries several `sub_agent` calls, dispatch runs them concurrently up to the cap. This is
loop mechanics, not a Mechanism — it steers nothing, injects nothing, and executes only
calls the model already made — so it is **on under Bypass** and needs no bench gate of its
own. (b) *Mechanism:* guided decomposition dispatches `min(cap, remaining)` children per
Turn instead of one, with a quiescent boundary between batches (ADR 0014 amendment,
2026-08-07). There is **no separate batch knob**: the server's cap is the single source of
width everywhere, and cap 1 reproduces today's behavior exactly — the serialized floor
still exists.

> **Noted 2026-09-24.** Trigger (b) has no referent any more: guided decomposition retired (ADR
> 0071 decision 5, ADR 0014 superseded), and the lab layer with it (ADR 0076 decision 1). What
> survives is the width itself, stated to Reactions as `LoopView.ParallelAgents()` — the width
> dispatch uses, which an engine-origin Reaction synthesizing delegations would batch by; no
> shipped Reaction does. Trigger (a) stands unchanged.

**2 — The cap is pin-else-discover-else-floor, named `parallel-agents`; the floor is 4 for a
keyed server and 1 otherwise.** A `servers:` entry
may carry `parallel-agents: N` (N ≥ 1); set, it is a **pin** discovery never overrides —
the same idiom as `context-window`. Absent, the cap is discovered from the **live** server:
`total_slots` in the `/props` response Apogee already fetches (one new field on the existing
read; re-resolved by the same beats that rebind the window, ADR 0024/0028). No signal — a
server without `/props`, a hosted endpoint that advertises no slot count — means the
**floor**, and the floor is read from the entry's shape: a **keyed** entry (`api-key`,
`api-key-cmd` or `api-key-env`, or one that speaks `wire: anthropic`) is a hosted server that
serves parallel requests as a matter of course and falls to **4**; any other entry — an
unkeyed LAN or loopback llama.cpp, the ephemeral `--endpoint` entry — falls to **1**, strictly
serial. Nothing changes for an unkeyed server until it advertises slots or the owner opts in,
and a keyed server is held serial by spelling it: `parallel-agents: 1`. The
launcher's `ProfileParams.Parallel` is deliberately **not** a discovery source: fan-out
only happens against a live server, and the live server's own `/props` is authoritative;
a pre-launch number Apogee never needs would be a second source to keep consistent.

> **Amended 2026-09-19 — the "else" rank is a keyed floor of 4, not 1 (apogee-9se; plan
> `2026-09-19 - 01`, items 4–5).** As first written this decision read "no signal … means 1:
> strictly today's serial behavior", and a hosted server — which advertises no `/props` — ran a
> fan-out serial for hours because the absence of a signal was read as a width. The rank order is
> unchanged: a pin is never overruled, discovery answers when nothing is pinned, and only the last
> rank changed — `ResolveParallelAgents(pinned, discovered, floor)` takes the floor as an argument
> and `DefaultParallelAgents(entry)` resolves it from the entry's key source and wire alone. The
> endpoint's address plays no part: a keyed loopback entry (the one a `/model` profile load builds
> when the launcher's config carries an api-key) reads 4 too, and since a send is refused until the
> first heartbeat binds a model, the `total_slots` that beat reports outranks the 4 before any
> delegation can run. Decision 1's "cap 1 reproduces today's behavior" still holds — it is now the
> unkeyed default and the keyed pin, not the universal one.

**3 — Concurrency is depth-0-only.** Only the top-level agent fans out; a child's own
delegations run serially inline, exactly as before. This makes the deadlock structurally
impossible without slot accounting, and bounds total concurrent LLM streams at the cap (a
delegating parent is blocked and consumes no slot). Relaxing to deeper fan-out with a
release-while-delegating budget is additive, if evidence ever wants it.

> **Amended 2026-09-01 by [ADR 0069](0069-the-top-level-model-picks-the-delegation-seat.md) —
> mixed-seat width.** A depth-0 reply may now put some of its `sub_agent` calls on the session's
> own server and others on the **Sub-agent server**, which this record never had to size: its cap
> was the cap of the one server every child was going to. The rule now: a reply whose children all
> share a **Delegation seat** is bounded by THAT seat's cap, exactly as above; a reply that spans
> both seats is bounded by `min(session cap, target cap)`. Decision 1's one-width-per-reply rule is
> what picks the smaller of the two rather than running a pool per seat — two widths in one Turn
> would need the slot accounting this decision exists to avoid — and the depth-0 bound itself is
> untouched, since a child's own tool carries no seat parameter.

**4 — Execution semantics preserve every existing per-child rule.** In a mixed reply,
**leaf tools run first, in emitted order** — a write a child depends on lands before
children start — then the `sub_agent` group fans out through a pool bounded by the cap
(more calls than cap queue for free slots, up to the fan-out ceiling of the 2026-09-20 note below —
the calls past that ceiling are refused, never queued). Each child keeps ADR 0013's whole contract:
per-call disposition one level down, isolated guard state, tighten-only live mode, panic
recovery at its own boundary. **Failures are independent**: a child's error, breaker trip,
or denied approval becomes that child's tool result; siblings run to completion and the
parent's next primary call sees all N results (errors-are-results, ADR 0007). **Cancel is
unchanged** (ADR 0013 §5): Esc signals every in-flight child, waits for them to stop, and
rolls the whole parent Turn back — no worse than today's serial N-in-one-Turn, where a
cancel during child 2 already discards child 1.

> **Amended 2026-09-19 (`apogee-2un`, Stage A).** The pool's cancel rule is untouched — Esc still
> signals every in-flight child, waits, and rolls the whole parent Turn back. What Stage A changes
> is the Exchange close *after* that rollback: the host settles it (`Agent.SettleExchange`) instead
> of aborting it, so the Turns that finished before the delegating one are kept and the saved
> record holds them (ADR 0013 §5's 2026-09-19 note; ADR 0022's). A finer cut inside the pool —
> keeping the children that finished before the Esc — is Stage B and stays on `apogee-2un`.
> Since 2026-09-20 the fan-out ceiling (the note after next) bounds what Stage B would recover to
> at most one round — `delegate-fanout-rounds × width` — so the loss it parks is no longer unbounded.

> **Amended 2026-09-14 — the pool yields its queued slots to a pending message.** A slot
> waiting behind the cap is not owed a run. When a user message is staged for the top-level
> agent (`Config.InterjectionPending`, the host's mailbox read as a predicate — [ADR 0025](0025-interjections-commit-at-the-between-steps-boundary.md),
> amended the same day), a pool worker that dequeues a delegation **skips it instead of
> starting it**: the slot commits in call order like a refused one, with the error-shaped tool
> result `sub-agent not started: the user sent a message while this group was running;
> delegate again if the task is still needed` and a finished phase carrying it (no started
> phase, no audit entry), so the parent's next primary call sees the skip beside its
> siblings' real results and may delegate again. The decision is read once, at dequeue, and
> never re-read. A child does the same one level down: a message in its own mailbox
> ([ADR 0063](0063-sub-agent-runs-are-user-addressable-views.md)) makes its serial inline
> dispatch skip the grandchildren it has not started. **Running children are never cancelled
> by a message** — this decision's cancel rule stands untouched: Esc is still the only cancel,
> and it still rolls the whole parent Turn back. Firings keep waiting for a quiescent host
> (ADR 0033 D7). Implemented by `docs/plans/2026-09-14 - 01`.

> **Amended 2026-09-20 — a reply's fan-out is bounded by a ceiling.** A reply is no longer free to
> fan out every `sub_agent` call it emits, the calls past the width queuing until a worker frees:
> the first `delegate-fanout-rounds × width` `sub_agent` calls in emitted order run as before (a
> file-only key, default **2** rounds; `0` switches the ceiling off), and every later `sub_agent`
> call in the reply is **refused** at dispatch, in the 2026-09-14 skip's shape — a finished phase
> alone, no started phase, no audit entry, committed in call order — with the constant-format
> error-shaped result `sub-agent not started: this reply fanned out %d delegations and the ceiling
> is %d (%d rounds × width %d) — the first %d ran; delegate the rest again once their results are
> in`. Leaf tools in the same reply are never counted or refused, and pre-emption still decides at
> dequeue among the calls that run. A refused call takes a `refused` row in the delegate ledger
> (`refusePastCeiling`'s own write, its cause the result's head line, numbered behind the slots
> that ran), and a group with a refused slot states no width line — the refusal already names the
> width. The `width` is the one the engine STATES to the model (`statedDelegationWidth`): the
> Sub-agent server's cap once it has stated one since the seat last moved, else the session
> server's, and 1 on a delegate — so the ceiling applies at **every depth** (a delegate's ceiling is
> `rounds` outright) and there is deliberately **no floor**: an unkeyed, unpinned local server has
> width 1, so at the default rounds a reply of three delegations there refuses the third (owner,
> 2026-09-20: "no floor, apply at every depth"). Decisions 1 and 2 are superseded in wording for
> the ceiling: "cap 1 reproduces today's behavior exactly" and "nothing changes for an unkeyed
> server until it advertises slots or the owner opts in" now hold for the **width** only. The
> overflow is refused rather than held back and re-issued by the engine: the session this closes
> (2026-09-20, a 56-dispatch reply at width 4 — 35 never started, the coordinator blind for 1h41m
> and the whole Turn rolled back on Esc) is a coordinator committing itself to more than it can read
> a single result of before the group returns, and an engine that quietly re-issued the overflow
> would preserve exactly that; refused, the coordinator is told in its own results, in the same
> words every time, and decides what to delegate again once the round it did get has reported. The
> bounds are announced before the first call: the orientation block gains `- Delegation bounds: up
> to W run at once; a reply may fan out at most C — calls past that are refused and must be
> delegated again; a reply's whole group returns together; each delegate is capped at S Turns (a
> max_steps above that is clamped).` whenever `sub_agent` is on the roster (the ceiling and cap
> clauses omitted when their key is `0`), and the `max_steps` schema text points at it. That width
> clause supersedes [ADR 0069](0069-the-top-level-model-picks-the-delegation-seat.md) decision 6's
> "no beat-driven text of any kind" for ONE latched-per-seat number: it is moved only by the human's
> doors (`/server`, `/sub-agents-server`) and a cap's first statement (a heartbeat's slot discovery,
> the far server's first stated cap), never by a target-down beat — and `fanOutWidthNoteFormat`'s
> width line remains the account of what a group actually RAN at, which may differ per reply.
> Implemented by `docs/plans/2026-09-20 - 00`.

> **Amended 2026-09-20 (`apogee-60x`, [ADR 0082](0082-a-silent-stream-is-cut-and-a-transient-fault-is-ridden-out-under-a-budget.md)).**
> "Failures are independent: a child's error, breaker trip, or denied approval becomes that
> child's tool result" — the independence stands (siblings run to completion, the parent's next
> primary call sees all N results), but a fault is no longer that result and nothing more. A child
> whose Run ends `Faulted` with the parent's ctx still live is folded at the fault
> (`finishAtFault`) and **retained** exactly as a capped one is, and its error result — the head
> still names the fault — carries, in the slots the cap path already ships, a draft note naming a
> spawn-named `output_path` the child wrote before the fault and the continue line
> `[to continue this delegate: sub_agent with continue: "<name>"]`. A cancel still unwinds the
> whole delegation and retains nothing (the cancel rule above, ADR 0013 §5). The breaker trip and
> the denied approval in that sentence are tool results inside the child's own Turn, never a
> fault of its Run, and are untouched; a refused call never spawned a child and is retained by
> nothing.

**5 — Child streams are identified by the spawning call-ID.** `EventBase` gains the ID of
the `sub_agent` tool call that spawned the emitting agent, stamped at child construction
exactly as `Depth` is today; top-level events carry none. Every consumer keys off it: the
TUI groups interleaved events back into per-child blocks, the transcript codec persists it,
and per-child usage attribution (`SubAgentUsage`) closes the `run.go` gap. Emission through
the parent's single `EventSink` is serialized at the boundary; the sink contract stays
one-sink-per-driver (per-child sinks were rejected — every Driver would grow multiplexing,
reshaping the ADR 0011/0031 contract for no gain).

> **Amended 2026-09-24 — the run id, not the call-ID, identifies a child's stream (plan
> `2026-09-24 - 01 - subagent-premature-done-plan`; owner call 2026-09-24).** The spawning call-ID
> is not unique. It is the model's or the server's to choose: a text-format parser numbering calls
> per Turn hands two siblings of one reply the same id, and a nested delegation can reuse its
> parent's. Keyed on it, the TUI paired one child's result with a sibling's run and ticked that
> sibling's row done while its own child was still working. So `EventBase` gains a second additive
> field, `RunID`: the engine mints it once per delegation (`<prefix>.<n>`, a random prefix per
> top-level Agent and a counter shared by its whole delegation tree) and stamps it at child
> construction exactly as the call-ID is stamped. The parent's `ToolCallEvent` and
> `ToolResultEvent` for that delegation carry it as `SpawnRunID`. The TUI now groups and pairs
> per-child blocks by the run id, the transcript codec persists it beside the call-ID (`runID`,
> `spawnRunID`, additive members as the call-ID's was), and the headless Event lines carry it
> (ADR 0075, amended the same day). Per-child usage attribution (`SubAgentUsage`) still brackets by
> the call-ID; this amendment does not move it. The call-ID stays on every event
> and keeps naming the tool-call block a run answers. A record or event written before the run id
> existed falls back to (depth, call-ID). The run id is identity only: it is never sent to a model,
> and the ids on the wire are never rewritten. Addressing a running child (`InterjectChild`,
> ADR 0063 D1) still goes by the spawning call-ID.

**6 — The TUI renders one live block per child.** At fan-out, one block per child appears
in call order; each accretes its own child's events via the call-ID and shows a live tail
under the existing collapsed cap, expandable like any tool block. Approvals from concurrent
children **queue** through the wait-tolerant Approver (ADR 0031 invariant) one prompt at a
time — the asking child blocks, siblings keep running — and the prompt names the asking
child's task.

> **Superseded in part 2026-09-15 (plan 2026-09-14 - 03, item 5; owner call 2026-09-14).** The
> queue above was extended the same way to `ask_user` questions (decision 12's kind-blind prompt
> slot, `AskRequest.SubAgentTask`/`SubAgentName`/`Depth` naming the asking child). No child asks
> any more: `ask_user` and `present_document` are withheld from every sub-agent's roster, at every
> depth and under any `tools` ask, because a delegation has no seat at the human's prompt — a
> child reports the question, or its deliverable's path, in its result and the parent asks or
> presents. Approvals from concurrent children still queue exactly as written; the prompt slot
> stays kind-blind, now serialising children's approvals against the top-level agent's own
> question. The identity carriers on the call context survive as the run's identity.

## Considered options

- **Discovery overrides the config key ("fallback" shape)** — *rejected*: the repo's idiom
  is the opposite (`context-window`: set = a pin the heartbeat never overrides), and a
  wrong server-advertised number would be uncorrectable from config.
- **All depths share one slot budget** — *rejected for v1*: requires release-while-
  delegating accounting to dodge the self-deadlock; real concurrency machinery for a case
  (nested fan-out) small models barely exercise. Depth-0-only needs none of it.
- **Fail-fast sibling cancellation** — *rejected*: destroys completed work, hides surviving
  reports the model could have used, and adds cancellation paths; errors-are-results
  already composes.
- **Per-child event sinks** — *rejected*: every Driver (TUI, bench, `run.Once`, the future
  daemon) would need multiplexing; one additive `EventBase` field serves them all.
- **A separate mechanism batch knob** — *rejected*: two width numbers to explain and keep
  consistent; the bench can still tune by pinning `parallel-agents` on the bench server's
  entry.
- **Launcher-profile `Parallel` as a discovery source** — *rejected*: redundant with the
  live server's `/props`, and a second source can disagree with the first.

## Consequences

- **ADR 0013 §5 and ADR 0014 §3 are amended** (dated 2026-08-07): per-child atomicity and
  cancel-rolls-back-the-Turn survive verbatim; "the driver runs the nested Agent in one
  shot" becomes per-child, not per-Turn; "one delegation per Turn" becomes "one batch of up
  to the cap per Turn". ADR 0014's rejection of *unbounded* all-N-in-one-Turn stands — a
  batch is bounded and keeps the quiescent boundary between batches.
- **The changed guided-decomposition stack must re-pass the ADR 0009 gate** before it can
  default on; until then it ships default-off as ever. Cancel granularity coarsens from one
  child to one batch — recorded, accepted.
- **`serverEntry` gains `parallel-agents`** (per-entry, like the per-server `llama-launcher`
  key of the 2026-08-07 plan); validation refuses negative values, and `0` reads as unset
  (the `context-window` idiom — yaml cannot distinguish an explicit `0` from an absent
  key); the settings surface live-applies it per
  [ADR 0037](0037-every-settings-edit-applies-to-the-running-session.md).
- **`EventBase` grows one additive field** (spawning call-ID), persisted as an **additive
  transcript-blob member** — *not* a blob version bump (corrected at implementation,
  2026-08-08; this bullet first read "the transcript blob version bumps additively"): the
  codec's own additive rule makes an `omitempty` member invisible to an older build, while
  a bump would make every blob this build writes unreadable to one, on ADR 0022's
  reject-forward rule, for no gain. Bench and headless consumers read the field or ignore it.
- **Concurrent children share one workspace.** Two children editing the same file can race;
  that is the model's (or the enumeration's) choice, as in other agent tools. Worktree
  isolation is explicitly out of scope for v1 — parked.
- **The window trade is the server operator's.** `--parallel N` shrinks the per-slot
  window; Apogee's numbers were already per-slot-honest (ADR 0024) and stay so. The docs
  say it plainly: more parallel agents = smaller window each.
- **CONTEXT.md** gains **Parallel agents** and updates **Sub-agent** and **Guided
  decomposition**; the `sub_agent` events' call-ID join the transcript-blob description.
