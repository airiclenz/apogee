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

**2 — The cap is pin-else-discover-else-1, named `parallel-agents`.** A `servers:` entry
may carry `parallel-agents: N` (N ≥ 1); set, it is a **pin** discovery never overrides —
the same idiom as `context-window`. Absent, the cap is discovered from the **live** server:
`total_slots` in the `/props` response Apogee already fetches (one new field on the existing
read; re-resolved by the same beats that rebind the window, ADR 0024/0028). No signal — a
cloud server, a server without `/props` — means **1**: strictly today's serial behavior.
Nothing changes for anyone until a server advertises slots or the owner opts in. The
launcher's `ProfileParams.Parallel` is deliberately **not** a discovery source: fan-out
only happens against a live server, and the live server's own `/props` is authoritative;
a pre-launch number Apogee never needs would be a second source to keep consistent.

**3 — Concurrency is depth-0-only.** Only the top-level agent fans out; a child's own
delegations run serially inline, exactly as before. This makes the deadlock structurally
impossible without slot accounting, and bounds total concurrent LLM streams at the cap (a
delegating parent is blocked and consumes no slot). Relaxing to deeper fan-out with a
release-while-delegating budget is additive, if evidence ever wants it.

**4 — Execution semantics preserve every existing per-child rule.** In a mixed reply,
**leaf tools run first, in emitted order** — a write a child depends on lands before
children start — then the `sub_agent` group fans out through a pool bounded by the cap
(more calls than cap queue for free slots). Each child keeps ADR 0013's whole contract:
per-call disposition one level down, isolated guard state, tighten-only live mode, panic
recovery at its own boundary. **Failures are independent**: a child's error, breaker trip,
or denied approval becomes that child's tool result; siblings run to completion and the
parent's next primary call sees all N results (errors-are-results, ADR 0007). **Cancel is
unchanged** (ADR 0013 §5): Esc signals every in-flight child, waits for them to stop, and
rolls the whole parent Turn back — no worse than today's serial N-in-one-Turn, where a
cancel during child 2 already discards child 1.

**5 — Child streams are identified by the spawning call-ID.** `EventBase` gains the ID of
the `sub_agent` tool call that spawned the emitting agent, stamped at child construction
exactly as `Depth` is today; top-level events carry none. Every consumer keys off it: the
TUI groups interleaved events back into per-child blocks, the transcript codec persists it,
and per-child usage attribution (`SubAgentUsage`) closes the `run.go` gap. Emission through
the parent's single `EventSink` is serialized at the boundary; the sink contract stays
one-sink-per-driver (per-child sinks were rejected — every Driver would grow multiplexing,
reshaping the ADR 0011/0031 contract for no gain).

**6 — The TUI renders one live block per child.** At fan-out, one block per child appears
in call order; each accretes its own child's events via the call-ID and shows a live tail
under the existing collapsed cap, expandable like any tool block. Approvals from concurrent
children **queue** through the wait-tolerant Approver (ADR 0031 invariant) one prompt at a
time — the asking child blocks, siblings keep running — and the prompt names the asking
child's task.

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
