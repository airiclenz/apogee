---
title: ADR 0011 decision
---

**The TUI is a thin event-driven renderer over a single worker goroutine that owns the
Agent.** The Bubble Tea program goroutine renders and reads keys; one worker `tea.Cmd` at a
time drives the Agent; the two communicate only through Bubble Tea's goroutine-safe
`p.Send` and a buffered reply channel. Concretely, five facets (C1–C5):

**C1 — One worker; the `Update` goroutine never touches the Agent.** Submitting input
launches a `tea.Cmd` that captures the engine and a fresh cancellable `ctx`, `Submit`s, then
runs the **Step loop** to the Exchange boundary (mirroring the canonical drive loop —
`Agent.Run` / the bench's `coreagent.Run`), returning a single terminal `Msg`
(`exchangeDoneMsg` / `cancelledMsg` / `errMsg`). Only **one** worker runs at a time (the
model refuses input while running), so the Agent is only ever driven from the current
worker — the single-goroutine contract holds by construction. Driving via `Step` (not `Run`)
keeps a clean per-Turn boundary for the status line and snapshots.

**Note (2026-09-30) — the worker is one hold among several, and they are one set.** The
invariant stands: one worker at a time drives the Agent. But the worker is no longer the only
thing that holds the engine away from the `Update` loop — a launcher verb restarting the server
it dials (the actuation latch, ADR 0029), a `/bg` launch reading the Agent off the loop, a
`/sessions` load, a record write, a deferred quit and a not-yet-bound session do too, and they
overlap rather than exclude one another. `internal/tui/engineholds.go` records them as one set of
`engineHold` bits — the `/bg` launch and session-load holds stored in `Model.holds`, the rest
derived from the fields that already carry them — and `Model.engineHolds` snapshots that set
with the non-hold facts the gates read. Every engine-availability gate asks a named question of
the snapshot (`commandRunnable`, `canRunDeferred`, `canRebind`, `beatMayCount`,
`canEditConfigExternally`, `canLaunchBg`, `quiescent`, `canResumeWorkflows`, `canWake`) rather
than ANDing flags, and every fold that hands the engine back runs the one release transition
`Model.releaseEngine` (ADR 0024 decision 4). `quiescent` deliberately ignores the `/bg` launch and
session-load holds: a Firing builds its own Agent (ADR 0033 decision 5). (Review 2026-09-30 #5;
plan `2026-09-30 - 02`.)

**C2 — Event→Msg bridge (`teaSink`).** `Config.Events` is a tiny adapter holding a handle to
the running program; `Emit(e)` wraps the Event in an `eventMsg` and calls `p.Send` —
async-to-`Update`, which satisfies "Emit must not block the loop for long" (a deadlock is
structurally impossible because `Send` is async). Delivery is **lossless** by default: every
Event becomes one Msg, never dropped. If `TokenEvent` flooding later shows queue pressure,
**coalesce adjacent `TokenEvent`s** (concatenate text in a window) behind the same interface
— *coalescing, never dropping*; do not pre-optimise.

> **Note (2026-09-23).** The coalescing is built (`internal/tui/sink.go`, a 30 ms window flushed
> at every Step boundary) and now covers `ReasoningEvent` as well as `TokenEvent`: adjacent
> deltas of the same kind and the same `EventBase` merge, a change of kind or `EventBase` — or
> any other Event — flushes first, so order is kept and the two streams of one Turn never merge
> into one another. Coalescing, never dropping, still holds; the one visible edge is that an
> escape sequence a provider split across two deltas is stripped whole at the receiving seam.

**C3 — Approval is a cross-goroutine rendezvous (`uiApprover`).** `Approve(ctx, req)` sends
an `approvalReqMsg{req, reply}` to the `Update` loop and blocks on a **buffered** (cap 1)
reply channel: `select { case d := <-reply: return d, nil; case <-ctx.Done(): return
ApprovalDeny, ctx.Err() }`. The buffer means the UI never blocks replying, and a reply that
arrives *after* a user stop is absorbed rather than parking a goroutine — **no leak**. A
cancelled ctx (user stop) unblocks the human gate; the Step then rolls the Turn back to a
quiescent boundary with `StatusCancelled` (ADR 0007).
This is the most race-prone piece of the seam and carries the heaviest test.

> **Amended 2026-09-27 (ADR 0088 D1).** The Step no longer rolls the cancelled
> Turn back: it settles it — finished calls keep their results, the running and unstarted ones get
> cancelled and not-run results — and the Exchange closes without an answer. The rendezvous above and the
> worker ctx mechanics of C4 are unchanged.

**C4 — Cancellation is the worker ctx's `CancelFunc`.** When the worker launches, the model
stores the ctx's `CancelFunc`; the stop key calls it; the in-flight `Step` honours it at the
next boundary and the worker returns `cancelledMsg`, leaving a resumable Session. This is the
Phase-0-promoted primitive (ADR 0007) — no
retrofit.

**C5 — Late binding resolves the construction chicken-and-egg.** `apogee.New` requires
`Config.Events` (and, for Ask-Before, an `Approver`), but the program those delegates push
to does not exist until the UI starts. A **`Bridge`** holds the `teaSink` and `uiApprover`
over one late-bound, atomic program handle: the composition root (`cmd/apogee`) builds the
Bridge, installs its `Sink`/`Approver` into `Config`, constructs the Agent, then `Run` binds
the live `*tea.Program` once it exists. The bind/send hand-off is an `atomic.Pointer`, so it
is race-free no matter how Bubble Tea schedules the worker `Cmd`.

**Package placement holds the ADR 0010
invariant.** `internal/tui` imports `internal/domain` (the public types) and drives the
engine through a **narrow local `Engine` interface** (satisfied by `*agent.Agent` =
`*apogee.Agent`); it never imports the root module path. The interface is the seam that also
makes the worker unit-testable with a fake engine. `cmd/apogee` (package `main`) is the
composition root and the only place that speaks the public `apogee.*` surface.
