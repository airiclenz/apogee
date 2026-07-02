---
Status: accepted
---

# The TUI is a thin event-driven renderer over a worker-goroutine engine

## Context

Phase 2 makes the agent a product: an interactive terminal UI you hold a coding
conversation in ([phase-2 detail plan](../plans/archived/phase-2-detail-plan.md)). The TUI is the
first *interactive* consumer of the Phase-1 public surface; the bench (P1.7) was the first
programmatic one, and they consume the **same** surface ([ADR 0001](0001-agent-loop-is-an-embeddable-library-driven-by-an-external-bench.md)).
Five forces collide, and wiring them without a deadlock or a data race is the real work of
the phase:

- the **Agent is single-goroutine** and not safe for concurrent use (`agent.go`: "drive
  one Agent from one goroutine; observe it only via its EventSink");
- **`Step` blocks** — network I/O, streaming, and a synchronous `Approver` that may block
  on a human ([ADR 0007](0007-step-turn-and-the-quiescent-boundary.md));
- **`EventSink.Emit` is called synchronously on the Step goroutine**, in Turn order, and
  "must not block the loop for long";
- **Bubble Tea owns a single-threaded `Update` loop** — all model mutation happens there,
  one `Msg` at a time;
- **the human's approval and stop live in that `Update` loop**, but the `Approver` that
  needs them runs on the Step goroutine.

The constraint the broad plan sets is absolute: *"No agent logic in the TUI — it only
renders events and sends user input"* (and supplies the Approval delegate + cancellation).
So the threading model has to be settled **before** any pane is drawn, or every view fights
it. This ADR records that model — the same way [ADR 0007](0007-step-turn-and-the-quiescent-boundary.md)
recorded the loop's control-flow calls — so each pane (P2.2–P2.4) is mechanical.

## Decision

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

**C2 — Event→Msg bridge (`teaSink`).** `Config.Events` is a tiny adapter holding a handle to
the running program; `Emit(e)` wraps the Event in an `eventMsg` and calls `p.Send` —
async-to-`Update`, which satisfies "Emit must not block the loop for long" (a deadlock is
structurally impossible because `Send` is async). Delivery is **lossless** by default: every
Event becomes one Msg, never dropped. If `TokenEvent` flooding later shows queue pressure,
**coalesce adjacent `TokenEvent`s** (concatenate text in a window) behind the same interface
— *coalescing, never dropping*; do not pre-optimise.

**C3 — Approval is a cross-goroutine rendezvous (`uiApprover`).** `Approve(ctx, req)` sends
an `approvalReqMsg{req, reply}` to the `Update` loop and blocks on a **buffered** (cap 1)
reply channel: `select { case d := <-reply: return d, nil; case <-ctx.Done(): return
ApprovalDeny, ctx.Err() }`. The buffer means the UI never blocks replying, and a reply that
arrives *after* a user stop is absorbed rather than parking a goroutine — **no leak**. A
cancelled ctx (user stop) unblocks the human gate; the Step then rolls the Turn back to a
quiescent boundary with `StatusCancelled` ([ADR 0007](0007-step-turn-and-the-quiescent-boundary.md)).
This is the most race-prone piece of the seam and carries the heaviest test.

**C4 — Cancellation is the worker ctx's `CancelFunc`.** When the worker launches, the model
stores the ctx's `CancelFunc`; the stop key calls it; the in-flight `Step` honours it at the
next boundary and the worker returns `cancelledMsg`, leaving a resumable Session. This is the
Phase-0-promoted primitive ([ADR 0007](0007-step-turn-and-the-quiescent-boundary.md)) — no
retrofit.

**C5 — Late binding resolves the construction chicken-and-egg.** `apogee.New` requires
`Config.Events` (and, for Ask-Before, an `Approver`), but the program those delegates push
to does not exist until the UI starts. A **`Bridge`** holds the `teaSink` and `uiApprover`
over one late-bound, atomic program handle: the composition root (`cmd/apogee`) builds the
Bridge, installs its `Sink`/`Approver` into `Config`, constructs the Agent, then `Run` binds
the live `*tea.Program` once it exists. The bind/send hand-off is an `atomic.Pointer`, so it
is race-free no matter how Bubble Tea schedules the worker `Cmd`.

**Package placement holds the [ADR 0010](0010-package-layout-domain-core-and-thin-root-facade.md)
invariant.** `internal/tui` imports `internal/domain` (the public types) and drives the
engine through a **narrow local `Engine` interface** (satisfied by `*agent.Agent` =
`*apogee.Agent`); it never imports the root module path. The interface is the seam that also
makes the worker unit-testable with a fake engine. `cmd/apogee` (package `main`) is the
composition root and the only place that speaks the public `apogee.*` surface.

## Consequences

- **The seam is provable without a terminal.** The bridge depends on a narrow
  `programSender interface { Send(tea.Msg) }`, so a stub program stands in for
  `*tea.Program`; the worker drives a fake `Engine`. The whole seam — sink ordering, the
  approval rendezvous and its cancel-no-leak guarantee, the worker's terminal Msg, and
  concurrent Emit + Approve + cancel — is tested under `-race` before any pane exists (P2.1),
  which is the point of converging the threading model first.
- **Each pane is mechanical.** P2.2–P2.4 only fold the C1–C4 messages
  (`eventMsg`/`approvalReqMsg`/`exchangeDoneMsg`/`cancelledMsg`/`errMsg`) and keypresses into
  the model — no new threading decisions, no agent logic.
- **This is the template every future interactive consumer copies.** A worker-goroutine
  engine + a `p.Send` event bridge + a buffered approval rendezvous + a ctx cancel is the
  load-bearing structure of the product binary; sub-agent (`Depth > 0`) rendering in Phase 3
  and any later interactive surface inherit it.
- **`tea.Msg` is not `any`.** In Bubble Tea v2 (`charm.land/bubbletea/v2`), `Msg` is an alias
  for a method-less `ultraviolet.Event`, so the seam references `tea.Msg` (not `any`) for the
  `programSender` interface; any struct is still a valid Msg, so the seam message types stay
  plain values. (See the build note below — the module path moved at v2.0.7.)

## Phase-2 realisation (P2.1)

- **Files.** `internal/tui/messages.go` (the five worker→Update Msgs + the `programSender`
  seam assertion), `bridge.go` (`Bridge` + the atomic late-bound `programRef`), `sink.go`
  (`teaSink`, C2), `approver.go` (`uiApprover`, C3), `worker.go` (`startExchange` /
  `driveExchange`, C1/C4). `cmd/apogee/wire.go` builds the `Bridge` and installs its
  `Sink`/`Approver` (retiring the P2.0 `nopSink` placeholder); the `launcher` seam carries
  the `Bridge` so `Run` can bind it.
- **Dependency / build note.** The plan pinned `github.com/charmbracelet/bubbletea/v2
  v2.0.7`, but Bubble Tea moved its module path to **`charm.land/bubbletea/v2`** exactly at
  v2.0.7 (the `github.com/...` path resolves only through v2.0.6). We took the new canonical
  path `charm.land/bubbletea/v2 v2.0.7` — the best-for-the-future choice (that is where the
  project now publishes). The v1 fallback, if a Bubbles v2 widget lags in P2.2, is
  `github.com/charmbracelet/bubbletea v1.3.10`. The v2-vs-v1 call is still made at the first
  widget need (P2.2), per the plan.
- **What is deliberately not here yet.** `Run` binds nothing and stays a no-op until P2.2
  builds the `Model` and the real `*tea.Program`; the worker is never launched in production
  before then, so the unbound delegates are harmless. Token coalescing (C2) is left as a
  documented hook, not built.

## Phase-2 realisation (P2.2 — the skeleton plugs in)

- **The model folds exactly the five C1–C4 Msgs.** `internal/tui/model.go` adds the `Model`
  (a value type with value-receiver `Init`/`Update`/`View`), its four-state machine
  `{idle, running, awaitingApproval, errored}`, the Bubbles `textarea` input + `viewport`
  transcript + `spinner`, and the status line. `Update` folds `eventMsg` /
  `approvalReqMsg` / `exchangeDoneMsg` / `cancelledMsg` / `errMsg` plus keypresses, the window
  size, and the spinner tick — nothing else, and no agent logic (C5). A keypress→submit
  launches the worker via `startExchange` and stores its `CancelFunc` (C4); **submit while
  running is a no-op** (the single-worker invariant). `transcript.go` is the C6 entry model
  (typed entries + an in-progress assistant token buffer) with an exhaustive switch over all
  eight Events — P2.2 folds the streaming-text and error paths; the tool/approval/mechanism/
  reset bodies are marked P2.3 stubs on the now-stable structure.
- **`Run` does the one wiring step P2.1 left.** It builds
  `tea.NewProgram(newModel(ctx, eng, opts), tea.WithContext(ctx))`, then calls
  `br.Bind(program)` **before** `program.Run()` — so the late-bound sink/approver reach the
  live program the instant the first worker emits, and the program context cancels an
  in-flight Exchange on shutdown (C4). `*tea.Program` satisfies the `programSender` seam
  (`Send(tea.Msg)`), exactly as P2.1 verified.
- **Charm v2-vs-v1: kept v2.** textarea/viewport/spinner all exist in `bubbles/v2 v2.1.0`, so
  no widget lagged and the v1 fallback was not triggered. **lipgloss and bubbles also live on
  the `charm.land/…` path** (`charm.land/lipgloss/v2 v2.0.4`, `charm.land/bubbles/v2 v2.1.0`)
  — both moved there at these versions, exactly as Bubble Tea did. The v2 `View()` returns a
  `tea.View` struct (alt-screen is a view field, not a program option) and key presses arrive
  as `tea.KeyPressMsg` — the seam message types stay plain values, unaffected.
