---
title: ADR 0025 context
---

Apogee's prompt box was dead while the model worked. `handleKey` fed the textarea only at
`stateIdle` and `stateAwaitingAsk`; every other key while busy was routed to `scrollViewport`,
`Enter`'s running branch was an explicit no-op, and a paste was dropped. So the human waiting on a
five-Turn task could not even *begin* the next message, let alone say "also check the tests" while
there was still a test-checking Turn left to spend it on. The owner's report was two wishes in one
sentence: *"I cannot start writing the next prompt when the model is working"* and *"send off
messages to the model while it is working — scheduled messages … sent when possible even when the
model is still working"* (ISSUES items 1–2, this repo).

The engine was equally closed. `Agent.Submit` refuses mid-Exchange with `ErrInputPending`, its
single-slot `pendingInput` is consumed at the top of `step()`, and the Agent's own contract is blunt:
drive it from ONE goroutine, and the only anytime-goroutine-safe mutators are `SetMode` /
`SetConfineToWorkspace` behind sibling mutexes. ADR 0011's
closing rule names exactly two legal call classes for the host: idle-only calls guarded by the state
machine (`ClearContext`, `Compact`, `AbortExchange`, `Snapshot`, and — since
ADR 0024 — `Rebind`),
and mid-`Step` calls behind a `SetMode`-class mutex. A mid-run message fits neither.

Three existing mechanisms look like they could carry the message, and none of them can:

- **`Request.InjectContext`** already knows about user-after-tool and routes *around* it. It is
  **request-scoped**: never committed, gone after one request. A remark that vanishes the moment the
  Turn that carried it ends is not what "I told the model something" means.
- **The deferred-injection pipe** (ADR 0017 F6)
  is **Exchange-scoped by contract** — `closeExchange` clears it — and request-scoped on delivery. An
  interjection must outlive both: it is history the next compaction summarises and the next session
  restores.
- **The derived Exchange opening** is `the last RoleUser message`, and
  ADR 0017 states plainly *why* that is
  stable: nothing commits a user message mid-Exchange. Committing one naively would move the opening
  to the remark, collapsing the Exchange to the remark alone — and roughly nine Mechanism call sites
  read that boundary (guided decomposition, decompose, library, cot, empty-response, tool-use
  enforcer, filehint, toolfilter), several of them relying on it as the shared task context.

This ADR records the design the owner ratified in the 2026-07-27 grill (six decisions, plus three
more in a second grill the same day), implemented by
`docs/plans/2026-07-27 - 01 - interjection-and-terminal-cursor-plan.md`. It supersedes nothing. It
extends ADR 0011's engine-call taxonomy by naming a third class that was already in the tree
unnamed, and it amends ADR 0017's stability argument with the one exception that argument now has.
