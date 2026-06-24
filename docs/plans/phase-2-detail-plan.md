# Apogee — Phase-2 Detail Plan (P2): the minimal modular TUI shell

**Date:** 2026-06-23 · **Status:** ✅ **COMPLETE** (P2.0–P2.6 all landed; the deliverable
holds end-to-end against a hermetic and a live model) — **P2.2 landed** (the Bubble Tea
`Model`/`Update`/`View` skeleton: the four-state machine, the input box, the transcript
viewport, and the status line; `Run` now builds the `tea.Program` and binds the `Bridge`;
the **Charm v2** stack is taken over the v1 fallback; **P2.3** has since filled the C6 event
fold — tool/result/approval/mechanism entries, the StreamReset discard, and pre-tool narration
finalisation). P2.1 landed before it (HEAD `5e574c5`:
the concurrency seam — `teaSink` + `uiApprover` + the worker driver, late-bound through the
`Bridge`, `-race`-proven against a stub program; **ADR 0011** records the model). P2.0 landed
before that (HEAD `a210c4f`: the Cobra binary, the composition-root wiring, and state-root
resolution). **P2.3** (the C6 event fold) and **P2.4** (the Approval UI — the a/d/s decision keys
over C3's reply channel) have since landed; **P2.5** (config & session glue — flag>env>file>default precedence,
`~/.apogee/config.yaml`, snapshot-on-clean-quit) has since landed too; and **P2.6** (the
hermetic e2e + the live-model run) has now landed, so **Phase 2 is complete** — the broad-plan
deliverable holds (a real coding conversation with a local model in the terminal: tokens stream,
a tool call appears, the write is approved, the result renders). Phase 1 is complete (the embeddable
agent core + the bench are built; the live-model eval is the one open Phase-1 runtime step,
which Phase 2 also exercises in passing). **All Phase-2 entry-state pre-checks were
re-verified against source (2026-06-23) and passed** (see the **Readiness** note in §0). This
document refines the broad plan's **Phase 2**
into numbered, acceptance-tested tasks and fixes the **concurrency model** the TUI lands into
(the hard part — §3). It is authoritative for the *order and acceptance* of Phase-2 work.
**Parent:** [`implementation-plan-apogee-merge.md`](./implementation-plan-apogee-merge.md) §4
(Phase 2 is intentionally coarse there). **Design of record:**
[`../design/technical-design.md`](../design/technical-design.md) §5 (TUI / CLI rows) + §6 #3
(Event delivery & backpressure, the channel-adapter note) and
[ADR 0007](../adr/0007-step-turn-and-the-quiescent-boundary.md) (Step / quiescent boundary /
cancellation — the seam the TUI drives). **Predecessor:**
[`phase-1-detail-plan.md`](./phase-1-detail-plan.md) (the public API the TUI consumes).
**Standing Requirements** (plan "⚠️ Standing requirements") apply to every task below — chiefly
**`/coding-standards` is mandatory for all new Go** (`coding-standards.go.md` +
`testing.go.md`), and **the module graph stays lean** (§3a: a pin is a decision; the dep is
added by the task that first needs it).

> **Why a detail plan now.** The broad plan calls Phase 2 "a thin Bubble Tea app over the
> Phase-1 Events." The *rendering* is thin; the **concurrency seam underneath it is not.** The
> Agent is single-goroutine and `Step` blocks (network + streaming + a synchronous, possibly
> human-blocking `Approver`); Bubble Tea owns its own single-threaded `Update` loop. Wiring a
> blocking, event-pushing, cancellable engine to an event-driven UI without a deadlock or a
> data race is the real work — and it must be settled **before** any pane is drawn or every
> view fights the threading model. This doc makes those calls (§3) so each pane is mechanical.
> The TUI is the **first interactive consumer** of the Phase-1 Events; the bench (P1.7) was the
> first programmatic one. They share the exact same surface — that is the point (ADR 0001).

---

## 0. Phase-2 entry state (where the repo stands)

| Backlog | Deliverable | State |
|---|---|---|
| P0.1–P0.6 | Phase 0 — facade, skeleton, detail plan + CI, `platform` seam, capstone harness | ✅ complete |
| P1.0–P1.7 | Phase 1 — ADR-0010 layout, real provider, full Turn/Step state machine, processing (one format), minimal tools, hook-mutation bodies, concrete Session schema, bench re-armed | ✅ complete |
| — | the public API is **body-complete for an embedder**: `New`/`Resume`, `Submit`/`Step`/`Run`, the 8 typed Events through `EventSink`, the synchronous `Approver`, `Snapshot`/`Resume` at the quiescent boundary, `Close` | ✅ proven by the bench (apogee-sim `internal/coreagent`) under `-race` |
| — | verify green: `gofmt -l .` · `go vet ./...` · `go build ./...` · `go test -race ./...` · 6-target cross-build · `grep -rl '"github.com/airiclenz/apogee"' internal/` empty (ADR-0010) · `apogee --help` exit 0 (hand-rolled stub) | ✅ |

**Readiness (re-verified against source, 2026-06-23 — all gates pass; work can start immediately at
P2.0).** Every gate above was re-run from a clean tree, not taken on trust: `gofmt -l .` empty ·
`go vet ./...` clean · `go build ./...` ok · `go test -race -count=1 ./...` green on every package ·
the 6-target cross-build green · the ADR-0010 grep empty · `apogee --help` exit 0. The consumer
surface below was checked field-by-field against `apogee.go` / `internal/domain` / `internal/agent`,
and the §3 concurrency seam (C1–C5) was confirmed to map onto the real engine: the Step `ctx` is
threaded into `Approver.Approve` (`dispatch.go:102`), an Approve error under a cancelled ctx becomes
a clean cancellation (`dispatch.go:104`), and the `allow-for-session` cache lives at `dispatch.go:113`.
`internal/tui` + `internal/mcp` are bare `doc.go` stubs (the TUI is greenfield as assumed) and
`go.mod` carries zero deps (the lean-graph invariant holds; cobra/charm enter at P2.0/P2.1).
**No engine change is required to begin.** One plan defect surfaced in that pass and is fixed in this
revision: the Event set is **8** variants, not 7 — `StreamResetEvent` was missing from the §0 list,
the C6 rule, and P2.3; all three now account for it.

**What Phase 2 inherits to build on (the consumer surface — verified against the source):**

- **Construction** — `apogee.New(Config)` / `apogee.Resume(Config, Session)`. `Config`
  carries the Upstream (`Endpoint`, `Model`), autonomy (`Mode`, `Bypass`), the host delegates
  (`Approver`, `Confiner`, **`Events EventSink`** — required), the registries (`Tools`,
  `Mechanisms`, nil ⇒ defaults), and the **injected state roots** (`LibraryDir`,
  `SessionsDir`, `ConfigDir`, `WorkspaceDir`). **There is no implicit `~/.apogee`** (ADR 0001):
  *the binary* must resolve and inject those roots — a Phase-2 responsibility (C7).
- **Driving** — `Agent.Submit(UserInput{Text, FileRefs})`, then `Agent.Step(ctx) →
  (StepResult, error)` returning at a quiescent boundary, or `Agent.Run(ctx)` to step to
  Exchange-end. `StepResult.Status ∈ {StatusTurnComplete, StatusExchangeComplete,
  StatusCancelled}`. The canonical drive loop is in `coreagent.Run` (P1.7) — the TUI's worker
  is the interactive twin of it.
- **Observing** — `EventSink.Emit(Event)` is **push, synchronous, in Turn order, on the Step
  goroutine; the loop neither buffers nor drops** ([ADR 0007 §Phase-1 realisation] / TDD §6 #3).
  *"Emit must not block the loop for long — fan out if needed"* is the **host's** contract to
  honour. The 8 variants (each embeds `EventBase{Depth, Turn}`):
  `TokenEvent{Text}` · `StreamResetEvent{}` · `MessageEvent{Text}` · `ToolCallEvent{Call ToolCall}` ·
  `ToolResultEvent{Result ToolResult}` · `ApprovalEvent{Request, Decision}` ·
  `MechanismFiredEvent{Mechanism, Hook, Action}` · `ErrorEvent{Source, Err}`.
  **`StreamResetEvent` carries no payload** — it signals an `ActionRetry` re-stream: the tokens
  streamed for the current Turn are superseded, and a streaming observer (the TUI) must **discard
  its in-progress token buffer for that Turn** before the re-stream's tokens arrive (events.go
  contract; emitted at `loop.go:232`).
- **Approving** — `Approver.Approve(ctx, ApprovalRequest{Tool, Arguments, Reason}) →
  (ApprovalDecision, error)` is called **synchronously inside a Step, may block on the human,
  and a cancelled ctx must unblock it.** Decisions: `ApprovalAllow` / `ApprovalDeny` /
  `ApprovalAllowForSession` (the loop caches *allow-for-session* per tool name for the rest of
  the Session — `dispatch.go`).
- **Cancellation** — promised by `Step`/`Run`: cancel ctx ⇒ abandon the in-flight stream/tool,
  return at the next boundary with `StatusCancelled`, state serializable (ADR 0007; promoted to
  a Phase-0 primitive **precisely so the TUI does not retrofit it** — plan §6 #24a).
- **The binary** — `cmd/apogee/main.go` is still the **Phase-0 stdlib stub** (hand-rolled
  `--help`). Phase 2 replaces it with the Cobra tree and makes it a real product (P2.0).

**The exact event sequence the renderer must handle (verified in `loop.go`/`dispatch.go`):**

- Within a Turn, `TokenEvent`s stream **live** as content arrives (`loop.go:262`).
- An `ActionRetry` post-response decision re-streams the Turn: the loop emits a **`StreamResetEvent`**
  first (`loop.go:232`), and the observer discards the tokens accumulated for the Turn so far before
  the re-stream's tokens arrive. (No default Mechanism emits `ActionRetry` in Phase 2 — the
  catalogue is empty — but the renderer handles it so a Phase-4 repair Mechanism, or a P2.6 scripted
  retry, needs no retrofit.)
- A **final no-tool** Turn then emits **one `MessageEvent`** with the full text (`loop.go:177`)
  and ends the Exchange (`StatusExchangeComplete`).
- A **tool** Turn emits **no `MessageEvent`** — it commits the assistant message and, per call,
  emits `ToolCallEvent` → (`ApprovalEvent`, around the synchronous `Approve`) → `ToolResultEvent`
  (`dispatch.go:31/111/190`), then returns `StatusTurnComplete` (the loop continues next Step).
- ⇒ **Renderer rule (C6):** finalise the streamed-token buffer into a committed assistant
  message when *either* a `MessageEvent` arrives (exchange end) *or* the first `ToolCallEvent`
  of the Turn arrives (the streamed text was pre-tool narration). `MessageEvent.Text` is the
  canonical full text — reconcile it against the accumulated tokens (they should match). On a
  `StreamResetEvent`, **discard** the in-progress token buffer for the Turn (the re-stream
  replaces it) — never commit superseded tokens.
- `MechanismFiredEvent` / `ErrorEvent` interleave anywhere; `ErrorEvent` is a *recovered* fault
  (a tool/Mechanism panic or a tool error), **not** a loop stop (ADR 0007). Every Phase-1 event
  is `Depth == 0`; sub-agent nesting (`Depth > 0`) is Phase 3 — the TUI must **tolerate**
  `Depth > 0` (indent or ignore) without crashing, but need not render it richly yet.

---

## 1. Phase-2 deliverable & exit definition

Broad plan §4 Phase-2 deliverable, verbatim: *"you can hold a real coding conversation with a
local model in the terminal, watch tools run, and approve writes."* With the constraint, also
verbatim: *"No agent logic in the TUI — it only renders events and sends user input"* (and
supplies the **Approval** delegate + **cancellation**). Phase 2 is **done** when all hold:

1. **Real binary.** `cmd/apogee` is a Cobra command tree; the root command launches the TUI;
   `apogee --help` still exits 0; the 6-target cross-build stays green. The binary resolves and
   injects the state roots (`~/.apogee` + workspace `.apogee` — C7); the library keeps no
   implicit roots.
2. **Thin renderer, clean split.** A Bubble Tea app with a disciplined model/update/view split
   under `internal/tui`, holding **no agent logic** — it renders Events, sends `UserInput`,
   supplies the `Approver`, and owns the cancel control. It consumes the **same** Phase-1
   surface the bench does.
3. **Conversation works end-to-end.** Submit text → watch assistant tokens stream → watch a
   tool call, approve/deny it inline → see the tool result → see the final message → submit
   again. Proven against a **hermetic httptest model** under `-race` (the same proof shape as
   P1.7 `coreagent`), then confirmed against a **live local model** from the host.
4. **Approval is a real human gate.** In Ask-Before mode every write prompts; `allow` / `deny`
   / `allow-for-session` all work; `allow-for-session` suppresses the next prompt for that tool;
   a user-stop while a prompt is pending cancels cleanly (`StatusCancelled`).
5. **Cancellation works.** A stop key (e.g. `esc` / `ctrl+c`) cancels the in-flight Exchange at
   the next quiescent boundary, the UI returns to idle, and the Session is still resumable.
6. **The ADR-0010 invariant holds.** `grep -rl '"github.com/airiclenz/apogee"' internal/` stays
   **empty**: `internal/tui` imports `internal/domain` (the public types, via their canonical
   path) and accepts the engine through a **narrow local interface** (satisfied by
   `*agent.Agent`) — it never imports the root module path. `cmd/apogee` (package `main`, not
   under `internal/`) is the composition root (C5).

The public API stays **v0.x, no stability promise** (ADR 0001 §18); semver begins at the end of
Phase 3. Phase 2 ships **Plan + Ask-Before** only — **Auto is out of scope** (it needs a
`Confiner`, which is Phase 3); selecting Auto is refused gracefully at startup (`--mode auto` ⇒
a clear message + `ErrAutoUnavailable`, not a crash — C8).

---

## 2. Dependency additions (pins already decided — phase-0 detail plan §1)

A pin is a decision; the dependency is added by the *task that first needs it*. Phase 2 is the
phase the TUI + CLI stack lands, so it adds the deps that were pinned-and-deferred in Phase 0/1:

| Module | Pin | Added by | Note |
|---|---|---|---|
| `github.com/spf13/cobra` | `v1.10.2` | **P2.0** (the command tree) | Replaces the hand-rolled `--help`. Mature, ubiquitous. |
| `charm.land/bubbletea/v2` | `v2.0.7` | **P2.1** ✅ (the program + Msg loop) | **v2** chosen for a greenfield TUI (phase-0 §1.1). **Path moved**: Bubble Tea renamed its module to `charm.land/bubbletea/v2` exactly at v2.0.7 (the `github.com/charmbracelet/...` path resolves only through v2.0.6); took the new canonical path (ADR 0011). Fallback: v1 `github.com/charmbracelet/bubbletea v1.3.10`. |
| `charm.land/lipgloss/v2` | `v2.0.4` | **P2.2** ✅ (layout/style) | **Path moved** to `charm.land/…` like Bubble Tea (the `github.com/charmbracelet/…` path lags). Matches the Bubble Tea v2 line. Fallback: v1 `v1.1.0`. |
| `charm.land/bubbles/v2` | `v2.1.0` | **P2.2** ✅ (textarea/viewport/spinner) | **Path moved** to `charm.land/…`. Matches the Bubble Tea v2 line. Fallback: v1 `v1.0.0`. |
| `gopkg.in/yaml.v3` | `v3.0.1` | **P2.5** (config file) — *only if* a config file lands in v1; flags+env+defaults may suffice | Same pin apogee-sim carries. Keep config thin (§6). |

**Charm v2 risk + fallback (phase-0 §1.1, re-confirmed here):** if a needed Bubbles v2 widget
or a community component lags the v2 API during P2.2, fall back to the **v1 trio** (`bubbletea
v1.3.10` + `lipgloss v1.1.0` + `bubbles v1.0.0`) — API-stable and battle-tested. Decide at the
first real widget need; record the call in the P2.2 commit. `ulid v2.1.1` is **not** pulled in
(session filenames use a sortable timestamp; revisit only if collision-free IDs are needed).
Net: Phase 2 adds **cobra + the Charm trio** (and yaml only if a config file is built). `net/http`
stays stdlib (the provider already owns the Upstream client). Each addition is re-justified when
its `go get` lands.

---

## 3. The architecture Phase 2 lands into — the concurrency model (the hard part)

This is the section that must be right before panes are drawn. Five forces collide:

- the **Agent is single-goroutine** and *not* safe for concurrent use (`agent.go`: "drive one
  Agent from one goroutine; observe it only via its EventSink");
- **`Step` blocks** — network I/O, streaming, and a synchronous `Approver` that may block on a
  human;
- **`EventSink.Emit` is called synchronously on the Step goroutine**, in Turn order, and *"must
  not block the loop for long"*;
- **Bubble Tea owns a single-threaded `Update` loop** — all model mutation happens there, one
  `Msg` at a time;
- **the human's approval + stop live in that `Update` loop**, but the `Approver` that needs them
  runs on the Step goroutine.

The resolution is one decision with five facets (C1–C5 below). **Recommendation: record it as a
new ADR (0011 — "the TUI is a thin event-driven renderer over a worker-goroutine engine")** when
P2.1 lands, the same way Phase 1 recorded its control-flow calls into ADR 0007. The shape:

```
 ┌────────────────────────── Bubble Tea program goroutine ──────────────────────────┐
 │  model { transcript, input, status, pendingApproval, cancel CancelFunc, ... }     │
 │  Update(msg):  keypress → submit ⇒ launch worker Cmd (holds Agent+ctx)            │
 │                eventMsg{Event} ⇒ fold into transcript / status (RENDER ONLY)      │
 │                approvalReqMsg{req, reply} ⇒ show prompt; key ⇒ reply<-decision    │
 │                exchangeDoneMsg / cancelledMsg / errMsg ⇒ back to idle             │
 │                stopKey ⇒ cancel()                                                 │
 └───────────▲───────────────────────────────────────────────────────▲──────────────┘
       p.Send │ (goroutine-safe; async to Update)         reply <- d  │ (buffered chan)
 ┌───────────┴───────────── worker goroutine (one tea.Cmd at a time) ─┴──────────────┐
 │  Submit(input); for { Step(ctx) } until !TurnComplete    ← the ONLY caller of the │
 │    Agent.* methods (preserves the single-goroutine contract)                      │
 │  Config.Events = teaSink{p}        → Emit(e) = p.Send(eventMsg{e})                │
 │  Config.Approver = uiApprover{p}   → Approve = p.Send(approvalReqMsg) ; <-reply   │
 └────────────────────────────────────────────────────────────────────────────────┘
```

**C1 — One worker, run the Agent inside a `tea.Cmd`; the `Update` goroutine never touches the
Agent.** Submitting input launches a `tea.Cmd` whose closure captures the `*agent.Agent` and a
fresh cancellable ctx; the Cmd `Submit`s and then runs the **Step loop** (mirroring
`coreagent.Run`) to the Exchange boundary, returning a single terminal `Msg`
(`exchangeDoneMsg` / `cancelledMsg` / `errMsg`). Only **one** such Cmd runs at a time (the model
refuses input while running), so the Agent is only ever driven from the currently-running Cmd —
the single-goroutine contract holds by construction. **Drive via `Step`, not `Run`** — the
per-Turn boundary lets the status line show Turn progress and leaves a clean snapshot point
between Turns (Run hides that). All intermediate output reaches the UI as Events (C2), not as the
Cmd's return value.

**C2 — Event→Msg bridge: a `teaSink` whose `Emit` calls `p.Send`.** `Config.Events` is a tiny
adapter holding the `*tea.Program`; `Emit(e)` wraps the Event in an `eventMsg` and calls
`p.Send` — Bubble Tea's goroutine-safe, async-to-`Update` enqueue, which is exactly the intended
mechanism and satisfies *"Emit must not block the loop for long."* **Backpressure/drop policy
(TDD §6 #3):** for a single local model at human-reading rates, direct `p.Send` is sufficient and
**lossless** (the correctness default — never silently drop, since the bench-side requires
in-order delivery and the TUI wants the same). If profiling later shows `TokenEvent` flooding the
program queue, add **coalescing of adjacent `TokenEvent`s** in the sink (concatenate text within
a short window) — *coalescing, not dropping* — behind the same interface; do not pre-optimise.
The sink must **never block forever**: `p.Send` is async, so a deadlock is structurally
impossible here, but document the invariant.

**C3 — Approval is a cross-goroutine rendezvous.** `Config.Approver` is a `uiApprover` holding
the `*tea.Program`. `Approve(ctx, req)`:

```
reply := make(chan domain.ApprovalDecision, 1)        // buffered: the UI never blocks replying
p.Send(approvalReqMsg{Request: req, Reply: reply})     // hand the prompt to the Update loop
select {
case d := <-reply:   return d, nil
case <-ctx.Done():   return domain.ApprovalDeny, ctx.Err()   // user-stop unblocks the human gate
}
```

`Update` stores the pending `approvalReqMsg`, renders the prompt, and on the human's keypress
(`a`/`d`/`s` → allow/deny/allow-for-session) sends `msg.Reply <- decision` (non-blocking, buffered)
and clears the prompt. A user-stop while pending cancels the worker ctx ⇒ `Approve` returns
`ctx.Err()` ⇒ the Step rolls back to a boundary with `StatusCancelled`; `Update` also clears the
prompt on the resulting `cancelledMsg`. This whole path is exercised under `-race` (C3 is the
single most race-prone piece; it gets a dedicated concurrency test in P2.1).

**C4 — Cancellation is the model's `CancelFunc`.** When the worker Cmd launches, the model stores
the ctx's `CancelFunc`; the stop key calls it; the in-flight `Step` honours it at the next
boundary (ADR 0007). The Cmd returns `cancelledMsg`; the model returns to idle with a resumable
Session. (This is the Phase-0-promoted primitive — no retrofit.)

**C5 — Package placement & the ADR-0010 invariant.** `internal/tui` holds the Bubble Tea
model/update/view and imports **`internal/domain`** (the public types — `domain.Event`,
`domain.UserInput`, `domain.ApprovalRequest`, …) and **accepts the engine through a narrow local
interface** it defines (e.g. `type engine interface { Submit(domain.UserInput) error;
Step(context.Context) (domain.StepResult, error); Snapshot() (domain.Session, error); Mode()
domain.Mode; Close() error }`), satisfied by `*agent.Agent`. It must **not** import the root
`apogee` module path (that breaks the ADR-0010 invariant *and* would cycle root→cmd→tui→root).
The narrow interface also makes the model **unit-testable with a fake engine**. **`cmd/apogee`**
(package `main`) is the composition root: it may dogfood the **public `apogee` package**
(`apogee.New`, `apogee.Config`) — desirable, it proves the shipped surface builds a real product
— and wire the resulting `*apogee.Agent` (= `*agent.Agent` by alias) into `internal/tui`. Because
the root types are aliases of the domain types, `cmd` speaking `apogee.*` and `internal/tui`
speaking `domain.*` are the *same types* — no friction, no violation.

**C6 — The rendering model** (the event-sequence rule derived in §0): a `transcript` of typed
*entries* (user msg / assistant msg / tool call+result / error / note), an in-progress assistant
buffer fed by `TokenEvent`s, finalised on `MessageEvent` **or** the first `ToolCallEvent` of the
Turn, and **discarded on a `StreamResetEvent`** (an `ActionRetry` re-stream — the accumulated tokens
for the Turn are superseded). The renderer must switch over **all 8** event variants so the set
stays exhaustive. `MessageEvent.Text` is canonical. `ApprovalEvent` is observational (the decision
already came back through C3's reply channel) — use it for the transcript record, not as the gate.

**C7 — State-root resolution moves into the binary.** ADR 0001 forbids an implicit `~/.apogee` in
the *library*; therefore `cmd/apogee` resolves: `ConfigDir`/`LibraryDir`/`SessionsDir` under
`~/.apogee` (XDG-respecting where sensible), and `WorkspaceDir` = the cwd (or `--workspace`).
These are injected via `Config`. This is the home of the "Config: `~/.apogee/` + workspace
`.apogee/`" seam from the broad-plan VS Code→Go map.

**C8 — Mode handling in Phase 2.** Expose `--mode plan|ask-before` (default `ask-before`).
`--mode auto` is **refused at startup** with a clear message ("Auto mode requires Confinement,
landing in Phase 3") — `New` already returns `ErrAutoUnavailable` when `Mode==Auto` and
`Confiner==nil`; the binary turns that into a friendly error, never a panic. `--bypass` is a
cheap passthrough flag (mostly inert until Phase-4 Mechanisms exist; wire it so it is ready).

---

## 4. Phase-2 task list

IDs use the `P2.x` scheme. **P2.0 (the binary + wiring) blocks all** (nothing renders without a
program to run it in). **P2.1 is the convergence point** — it builds and *proves under `-race`*
the C1–C5 concurrency seam as a standalone, fake-engine-testable package, before any pane
depends on it. Then the panes (P2.2–P2.4) fan out. **P2.6 is last** (it needs the slice working
end-to-end), and it doubles as the Phase-1 live-model confirmation.

| ID | Task | Depends | New deps | Resolves |
|---|---|---|---|---|
| **P2.0** ✅ | Cobra command tree + binary wiring + state-root resolution + `Config` construction (C5/C7/C8) | — | `cobra` | broad §4; TDD §5 CLI row |
| **P2.1** ✅ | The concurrency seam: `teaSink` bridge + `uiApprover` rendezvous + worker `tea.Cmd` + cancel (C1–C4), as a fake-engine-testable package | P2.0 | `bubbletea/v2` | ADR 0007; TDD §6 #3; **ADR 0011** |
| **P2.2** ✅ | Bubble Tea `Model`/`Update`/`View` skeleton: states (idle/running/awaiting-approval/error), input box, transcript viewport, status line | P2.1 | `lipgloss/v2`, `bubbles/v2` | TDD §5 TUI row |
| **P2.3** ✅ | Event rendering: token-stream assembly, tool-call/result entries, message finalisation, error/mechanism display (C6) | P2.2 | none | §0 event-sequence rule |
| **P2.4** ✅ | The Approval UI: inline prompt, `allow`/`deny`/`allow-for-session` keys, cancel-clears-prompt (C3) | P2.2 | none | CONTEXT: Approval; ADR 0004 |
| **P2.5** ✅ | Config & session glue: flag > env > file > default precedence (`~/.apogee/config.yaml` landed); snapshot-on-quit + `--resume` | P2.0 | `yaml.v3` | ADR 0001 (roots); §6.1 (sessions) |
| **P2.6** ✅ | End-to-end acceptance: drive the **real** Agent through the TUI against a hermetic httptest model under `-race`; then the **live-model** run from the host | P2.1–P2.4 | none | broad §4 deliverable; Phase-1 live eval |

### P2.0 — the Cobra command tree + binary wiring
Replace `cmd/apogee/main.go`'s stdlib stub with a Cobra root command that **launches the TUI**.
Flags (minimal, reviewable): `--endpoint`, `--model`, `--mode` (`plan`|`ask-before`, default
ask-before — C8), `--workspace` (default cwd), `--bypass`, `--resume <session-file>`,
`--config <dir>`. Resolve the state roots (C7) and build a `Config`; construct the Agent via the
**public** `apogee.New` (dogfood — C5); hand it to `internal/tui`. Keep `apogee --help` exit 0
and the 6-target cross-build green. No subcommands beyond the root are required this phase
(`headless`/`probe` are later — §6); add the tree shape so they slot in.
**Acceptance:** `apogee --help` exits 0 and lists the flags (cobra-generated); `apogee
--endpoint … --model … --workspace <tmp>` constructs an Agent and enters the TUI (smoke-tested by
launching with a fake/empty endpoint and asserting clean construction + a clean quit); an
`--mode auto` invocation exits non-zero with the Phase-3 message (not a panic); cross-build green.
**✅ Done (HEAD `a210c4f`).** `cmd/apogee` is now the Cobra composition root: `root.go` carries the
flag set with an injectable `launcher` seam (so construction is provable without a terminal);
`wire.go` resolves the state roots (C7 — `~/.apogee` home for config/library/sessions + cwd /
`--workspace` for the tool sandbox, **paths only — no dir creation**), parses the mode (C8),
dogfoods `apogee.New` / `apogee.Resume` (C5), and maps `ErrAutoUnavailable` to the Phase-3 message;
a temporary `nopSink` satisfies the required `Config.Events` until P2.1 wires the real bridge.
`internal/tui` holds **only the seam boundary** — the narrow `Engine` interface (satisfied by
`*apogee.Agent`, with a compile-time assertion in `wire.go`), `Options`, and a placeholder `Run` —
so the ADR-0010 grep stays empty. `cobra v1.10.2` pinned. All acceptance gates green (11 new tests
under `-race`; 6-target cross-build). **`--resume` was wired now** (it rides the stable Session API)
with round-trip + future-version (`ErrSessionVersion`) tests; **snapshot-on-quit and the optional
config file remain P2.5.** Next: **P2.1** — the concurrency seam (`teaSink` + `uiApprover` + worker
`tea.Cmd` + cancel, under `-race`) and **new ADR 0011**.

### P2.1 — the concurrency seam (the convergence)
Build C1–C5 as a cohesive, **rendering-free** unit so the threading is proven before the views
exist: the `teaSink` (`Emit` → `p.Send(eventMsg)`, C2 — with the lossless default + a documented
coalescing hook), the `uiApprover` (the request/reply rendezvous with ctx-cancel, C3), and the
worker driver (`Submit` + Step-loop to the boundary, returning the terminal `Msg`, holding the
`CancelFunc`, C1/C4). Define the narrow `engine` interface (C5) so the driver is testable with a
fake Agent. **This is the most race-prone code in the phase — it carries the heaviest test.**
**Acceptance (all under `-race`):** a fake engine + a scripted event sequence drives the sink and
the terminal Msg in order; the approver rendezvous returns the UI's decision and, on a cancelled
ctx, returns `ApprovalDeny`+`ctx.Err()` **without** the UI ever replying (no goroutine leak, no
deadlock); a cancel mid-run yields the `cancelledMsg`; concurrent Emit + Approve + cancel pass
the race detector. (Bubble Tea's `teatest` may drive a thin harness, or a stub `programSender`
interface stands in for `*tea.Program` so the seam tests need no real terminal.)
**✅ Done (HEAD `5e574c5`).** `internal/tui` now holds the seam: `messages.go` (the five
worker→Update Msgs + the `programSender` assertion), `bridge.go` (`Bridge` + the atomic
late-bound `programRef`), `sink.go` (`teaSink`, C2), `approver.go` (`uiApprover`, C3),
`worker.go` (`startExchange`/`driveExchange`, C1/C4). `cmd/apogee` retires the P2.0 `nopSink`
and installs `Bridge.Sink()`/`Approver()` into `Config`; the `launcher` seam now carries the
`Bridge` so `Run` can bind the live program (its body stays a placeholder until P2.2 builds
the `Model` + `*tea.Program`). Tests are all under `-race`: scripted sink ordering
(lossless/in-order), the approver returning each decision, the **cancel-no-leak** proof
(cancelled ctx ⇒ `ApprovalDeny`+`ctx.Err()`, buffered reply absorbs a late UI reply), the
worker terminal-Msg paths, and concurrent Emit+Approve+cancel+rebind (stress-passed 20×).
**Two handoff premises corrected** (both in **ADR 0011**): the Bubble Tea module path moved to
`charm.land/bubbletea/v2` at v2.0.7, and `tea.Msg` is an alias for a method-less
`ultraviolet.Event` (not `any`), so the `programSender` seam references `tea.Msg`. All verify
gates green. Next: **P2.2** — the `Model`/`Update`/`View` skeleton + the Charm v2-vs-v1 call.

### P2.2 — the Model/Update/View skeleton
The disciplined Bubble Tea split under `internal/tui`: a `Model` with explicit `state ∈
{idle, running, awaitingApproval, errored}`, an input box (Bubbles `textarea`), a scrollback
(Bubbles `viewport`), a status line (model · endpoint · mode · bypass · turn counter · spinner
when running), and `Init`/`Update`/`View`. `Update` folds the C1–C4 messages
(`eventMsg`/`approvalReqMsg`/`exchangeDoneMsg`/`cancelledMsg`/`errMsg`) and keypresses; **it holds
no agent logic** (C5). Layout/styling via Lipgloss; the decision to keep v2 or fall back to v1
(phase-0 §1.1) is made and recorded here at the first widget need.
**Acceptance:** `Update` is unit-tested by feeding synthetic `Msg`s and asserting `Model` state
transitions + `View` substrings (golden snapshots per `testing.go.md`); resizing (`WindowSizeMsg`)
reflows without panic; submitting while `running` is a no-op (single-worker invariant); the
package has **no** import of the root module path (the ADR-0010 grep stays empty).
**✅ Done.** `internal/tui` now holds the skeleton: `model.go` (the `Model`, its four-state
machine `{idle, running, awaitingApproval, errored}`, `Init`/`Update`/`View`, the Bubbles
`textarea` input + `viewport` transcript + `spinner`, the status line, and the layout) and
`transcript.go` (the C6 entry model — typed entries + an in-progress assistant token buffer —
with an **exhaustive switch over all 8 events**; P2.2 folds the streaming-text + error paths and
records the Turn index, leaving the tool/approval/mechanism/reset bodies as marked `P2.3` stubs on
the stable structure). `Run` now builds `tea.NewProgram(newModel(…), tea.WithContext(ctx))` and
calls `br.Bind(program)` **before** `program.Run()` (the wiring P2.1 deferred). `Update` folds
exactly the five seam Msgs + keypresses + `WindowSizeMsg` + the spinner tick — **no agent logic**
(C5): a keypress→submit launches the worker via `startExchange` and stores its `CancelFunc`;
**submit while `running` is a no-op**; `esc`/`ctrl+c` cancel an in-flight worker or quit at idle.
Tests (`model_test.go`, all under `-race`, 23 subtests) drive the lifecycle (submit → stream →
message → done), each state's seam-Msg transition, token reconciliation (the `MessageEvent` text
is canonical, the streamed preview is superseded), the single-worker no-op, the stop/quit keys,
`WindowSizeMsg` reflow at six sizes incl. 1×1 without panic, the status-line substrings, and
`Depth > 0` tolerance. **Charm v2-vs-v1 call: kept v2** (`bubbletea/v2 v2.0.7` + `bubbles/v2
v2.1.0` + `lipgloss/v2 v2.0.4`) — every Bubbles widget needed (textarea/viewport/spinner) exists
in v2.1.0, so the fallback was not triggered; **all three deps are on the `charm.land/…` path**
(lipgloss and bubbles moved there at these versions, exactly as Bubble Tea did — the handoff's
warning was correct). All verify gates green; the ADR-0010 grep stays empty. Next: **P2.3** (the
C6 event fold) + **P2.4** (the Approval UI keys over C3's reply channel).

### P2.3 — rendering the event stream
Fold Events into the transcript per the C6 rule: append `TokenEvent.Text` to the in-progress
assistant entry (live); **discard the in-progress token buffer on a `StreamResetEvent`** (an
`ActionRetry` re-stream supersedes the Turn's tokens — events.go contract); finalise that entry on
`MessageEvent` (canonical text) **or** the first `ToolCallEvent` of the Turn; render `ToolCallEvent`
(tool + pretty-printed `Arguments`) and its paired `ToolResultEvent`; render `ErrorEvent` as a
recoverable notice (not a stop); render `MechanismFiredEvent` only in a debug view (off by default —
there is no catalogue until Phase 4). Switch over **all 8** event variants (no default Mechanism
emits `ActionRetry` in Phase 2, but handle `StreamResetEvent` now so no retrofit is needed when
Phase-4 repair Mechanisms land). Tolerate `Depth > 0` (indent or skip) without crashing.
**Acceptance:** feeding a **recorded** event sequence (the shape `coreagent` produces: tokens →
tool call → tool result → tokens → message) yields a correct transcript (golden); a tool-Turn
with **no** `MessageEvent` still finalises the pre-tool narration; a sequence containing a
`StreamResetEvent` (tokens → reset → tokens → message) **discards the superseded tokens** and
renders only the final accepted text; an `ErrorEvent` mid-stream renders inline and the transcript
keeps going; the streamed tokens and the final `MessageEvent` reconcile to the same text.
**✅ Done (HEAD `27ceb63`).** `transcript.apply` now folds the full event stream (the five P2.2 stubs filled): a
`StreamResetEvent` discards the in-progress buffer (`discardPending`); the **first `ToolCallEvent`
of a Turn** finalises the pre-tool narration (`finalizeNarration` — the streamed tokens are
canonical when no `MessageEvent` follows; a second call in the same Turn does not re-finalise, and a
Turn with no narration commits no empty entry) before appending the call entry (tool name +
pretty-printed `Arguments` via `prettyJSON`/`json.Indent`, malformed args shown **verbatim** rather
than dropped); `ToolResultEvent` appends the paired result (an `IsError` result is marked but stays
a result — it is in-band, not a recovered fault); `ApprovalEvent` is recorded **observationally** as
a note (the gate itself is the C3 rendezvous); `MechanismFiredEvent` is gated behind a default-off
`debug` view (no catalogue until Phase 4 — §6; the field is the seam, the product toggle is later);
`ErrorEvent` renders inline without stopping the stream. `renderEntry` now indents the continuation
lines of a multi-line body when `Depth > 0`. The switch stays **exhaustive over all 8** variants.
`transcript_test.go` (new, in-package, ANSI-stripped) covers the **golden** tool-Turn sequence,
narration-without-`MessageEvent` plus the no-narration / two-calls-one-Turn edges, the StreamReset
discard (and the idle-reset no-op), canonical-vs-streamed reconciliation (incl. the empty-canonical
fallback), the inline `ErrorEvent`, the error tool result, the observational approval, the
debug-gated mechanism, `Depth > 0` indentation, and the argument formatter. **No new deps** (P2.3
added none); the only non-test change outside `transcript.go` was threading a `depth` arg through
`addError`'s one model call site. All verify gates green; the ADR-0010 grep stays empty. Next:
**P2.4** (the Approval UI keys over C3's reply channel).

### P2.4 — the Approval UI
The interactive face of C3: when `awaitingApproval`, render the pending `ApprovalRequest` (tool,
arguments, `Reason`) and a key legend; `a` → `ApprovalAllow`, `d` → `ApprovalDeny`, `s` →
`ApprovalAllowForSession`, each sending the decision back over the reply channel and returning to
`running`. A stop key while pending cancels (clears the prompt on `cancelledMsg`). Only the
top-level (`Depth == 0`) prompt is handled this phase.
**Acceptance:** an `approvalReqMsg` puts the model in `awaitingApproval` and renders the request;
each key produces the right `ApprovalDecision` on the reply channel (table test, `-race`);
`allow-for-session` is observably distinct (the engine then auto-allows that tool — verified in
the P2.6 e2e, where the loop's `approved[...]` cache suppresses the second prompt); a cancel
while pending clears the prompt and returns to idle.
**✅ Done (HEAD `02ae4d3`).** The wiring P2.2 left in place made this small — all in `model.go`,
the ADR-0010 grep stays empty, no new deps. `handleKey` gained an `awaitingApproval` branch placed
**before** the scroll fall-through (which otherwise swallowed `a`/`d`/`s` as viewport scroll keys —
the bug handoff 15 flagged); `handleApprovalKey` + the `approvalKeys` map send the verdict
(`a`→`ApprovalAllow`, `d`→`ApprovalDeny`, `s`→`ApprovalAllowForSession`) **non-blocking** on the
cap-1 reply chan, clear `m.pending`, and return to `running` — **re-arming the spinner tick**, which
had died when the prompt went up. A non-decision key still scrolls the transcript so the human can
review context before ruling. `View` + `approvalPrompt` stack a prompt block (bold `approve
<tool>?`, faint `(<reason>)`, then `prettyJSON(Arguments)` — **reusing the P2.3 helper**, no second
formatter) between the viewport and the status line, **shrinking the viewport on View's local value
copy** by the prompt height so it never pushes the status/input/hint past the bottom of the window;
only `Depth == 0`. **The cancel path needed no new code** (`esc` while pending is already `busy()` →
`stopWorker` → `cancelledMsg` → `finishWorker` clears the prompt) — only a test. `model_test.go`
(now **61** tui subtests, +7, all `-race`): a table test for each decision key (verdict on the
reply chan, return to `running`, `pending` cleared, spinner re-armed), the prompt render
(tool/reason/args present), a non-decision-key no-op, and the cancel-while-pending → idle path.
`allow-for-session` being **observably distinct** (the loop's `approved[...]` cache suppresses the
second prompt) is left to the **P2.6** e2e, per the plan. All verify gates green. Next: **P2.5**
(config & session glue) and **P2.6** (the hermetic e2e + the live-model run).

### P2.5 — config & session glue
Resolve `Config` from flags → env → file → defaults (C7). Keep it **thin**: flags+env+defaults
are enough for the deliverable; a `.apogee/config.yaml` (workspace) / `~/.apogee/config.yaml`
(user) loader is added **only if** it earns its place this phase (else deferred, with `yaml.v3`
left as a pinned-but-unused decision). Wire **basic** sessions on the already-built
`Snapshot`/`Resume`: `--resume <file>` reconstructs via `apogee.Resume`, and the TUI offers a
save (snapshot to `SessionsDir`) — at minimum on clean quit. No session **browser** UI (§6).
**Acceptance:** precedence (flag > env > file > default) is table-tested; a snapshot written by
the TUI round-trips through `--resume` and **continues** the conversation at the right Turn
(reusing P1.6's `turnIndex` continuation); a `--resume` of a future-version file surfaces
`ErrSessionVersion` as a friendly error, not a panic.
**✅ Done (HEAD `4f93505`).** Both pieces landed, all in the composition root + a new
`internal/session` writer (the ADR-0010 grep stays empty). **(1) Config precedence** (`cmd/apogee/config.go`): a pure
`resolveSettings(file, env, flag layer) settings` overlays optional layers in increasing
priority, fed by `flagLayer` (gated on cobra's `Changed` so an unset flag's default never
shadows a lower layer), `envLayer` (`APOGEE_{ENDPOINT,MODEL,MODE,BYPASS}`; a bad bool is a hard
error), and `fileConfig.layer()`. `applyConfig` orchestrates in `RunE` before `runRoot` — it also
overlays `APOGEE_CONFIG`/`APOGEE_WORKSPACE` onto the dirs (the file can't set the home it lives
in) and reads `<home>/config.yaml`. **The config file earned its place** (owner decision: a
uniform `~/.apogee` with `config.yaml`/`library`/`sessions`), so `gopkg.in/yaml.v3 v3.0.1` landed
(no longer a pinned-but-unused decision); `resolveRoots` now shares an `apogeeHome` helper with
the file path. A malformed file errors rather than silently dropping a typo'd setting; a bad
`mode` from any source still flows through `parseMode`'s friendly error. **The config file is
auto-seeded on first run** (owner directive): `cmd/apogee/defaults/config.yaml` is `//go:embed`-ed
into the binary (every build re-bakes the latest), and `seedDefaultConfig` writes it to
`<home>/config.yaml` when absent (best-effort, owner-only perms, honouring `--config`/
`APOGEE_CONFIG`) with a one-time stderr notice — **never overwriting** an existing file. The
shipped template is fully commented, so it is behaviour-neutral (a guarded test asserts it parses
to an empty layer); it lives under `cmd/apogee/` because `//go:embed` cannot reach above its source
dir and config is the composition root's concern (ADR 0001), not the library's. **(2) Snapshot-on-quit**
via the **saver seam** (handoff 16's recommended shape): `internal/session.Store` (`NewStore`/
`Save(domain.Session) (path, error)` — sortable UTC-timestamp filenames, lazy `MkdirAll`, owner-
only perms) owns the on-disk format in the binary; `cmd/apogee` wraps it in a `sessionSaver` and
installs `tui.Options.Save func(domain.Session) error`, then prints a `--resume` hint once the
alt-screen tears down. The model snapshots **only on a clean quit** (idle/errored, `!busy()`,
non-empty transcript) — calling `eng.Snapshot()` while a worker owns the single-goroutine Agent
would race, so a `ctrl+c` mid-run cancels and quits **without** saving (snapshot-the-last-boundary
mid-run is deferred — §6.1). Tests (all `-race`): the precedence table + `applyConfig` end-to-end
(file+env+flag, env-resolved dirs, flag-beats-env-dir, malformed/bad-bypass errors, absent-is-empty);
the `Store` round-trip + UTC/sortable filename; the saver→`--resume` round-trip through `buildAgent`;
and the model's save-on-clean-quit / skip-empty / skip-while-busy / nil-saver paths (66 tui subtests).
All verify gates green; `go mod tidy` recategorised `yaml.v3` as direct. Next: **P2.6** (the
hermetic e2e + the live-model run).

### P2.6 — end-to-end acceptance (+ the Phase-1 live confirmation)
The deliverable proof, mirroring P1.7's hermetic pattern: a **scripted OpenAI-compatible
`httptest` model** drives the **real** Agent **through the TUI's seam** (the `teaSink`,
`uiApprover`, worker) — the model streams assistant text, requests `write_file`, the approval
path approves (scripted key or an auto-approver in the test), the file lands in a `t.TempDir()`
workspace, and the transcript shows tokens → tool call → tool result → final message. Then the
**live-model run** from the host: point `--endpoint http://192.168.64.1:1111` at a tool-capable
local model (load it via the llama-launcher MCP control `http://192.168.64.1:7331/mcp`) and hold
a real file-edit conversation, watching tools run and approving the write — which **also closes
the one open Phase-1 runtime step** (the live file-edit eval), now through the product UI.
**Acceptance:** the hermetic e2e passes under `-race` (no terminal required — drive via `teatest`
or the `programSender` stub); the live run completes a file-edit task interactively with a real
model, writes approved through the UI, transcript correct.
**✅ Done (HEAD `6f763fc`).** Both parts landed in `internal/tui` test code; production `internal/tui`
still imports only `internal/domain` + the narrow `Engine` (the ADR-0010 grep stays empty — the
e2e's `internal/agent`/`provider`/`session`/`tools` imports are **test-only** and none is the bare
root path). **`teatest` was not used** (it is not in the module cache and targets the v1 Bubble Tea
path; the v2 module moved to `charm.land/…`), so the no-terminal driver is a small white-box
`uiHarness` — a `programSender` that drains the Msgs the seam Sends into a **real `Model` through the
real `Update`**, auto-approving a prompt exactly as a human pressing `a` would (the real keypress →
`handleApprovalKey` → C3 reply rendezvous). The seam Sends from the one worker goroutine and the
harness reads on the test goroutine, so only one goroutine touches the Model — race-clean, no lock,
and it launches the worker via `startExchange` directly (not the input-key path, whose `Batch` would
drag the cosmetic spinner tick into the loop). **(1) Hermetic e2e** (`e2e_test.go`, `-race`): a
**stateless** scripted OpenAI-compatible `httptest` model that decides each reply from the request's
own message history (fresh task → narrate + `write_file`; history ends in a tool result → final
message; a later user turn → a plain closing reply) — driving, in order, a tool Turn, a final Turn,
and the continuation Turn, the way a real model does. `TestE2EConversationThroughTUI`: narration →
`write_file` → approve → the write lands in a `t.TempDir()` workspace → the transcript folds tokens →
call → result → final message (the real event stream through the real fold). `TestE2ESnapshotResume`​`Continues`: snapshot on a clean quit through the **real saver seam** (`session.Store`) → `agent.Resume`
from the written file → continue, proving the resumed Exchange picks up at the snapshot's `turnIndex`
(turn 2, after exchange 1's turns 0+1) — a reset would surface as turn 0. **(2) Live-model run**
(`live_test.go`, **opt-in** — skipped unless `APOGEE_LIVE_ENDPOINT` is set, so `make check` is
unaffected): the same harness + real Model against a live local model, the open Phase-1 live
file-edit eval now over the product surface. Run against **`gemma-4-E4B-it-Q8_0`** (the already-loaded
model — a deliberate no-swap; the launcher MCP is reachable at `192.168.64.1:7331` and the model is
tool-capable despite `/v1/models` advertising only `completion`): it streamed, called `write_file`,
the write was approved through the real gate and wrote `greeting.txt` (14 bytes), and the final
message rendered — `StatusExchangeComplete`, transcript correct. **Re-confirmed against
`gpt-oss-20b-MXFP4`** (swapped in via the launcher MCP) — identical end-to-end result, ~0.9s/Turn —
so the deliverable holds across two model families/scales. The **only** unautomated remainder
is a human pressing `a` in a live alt-screen terminal (no TTY in the dev env); the hermetic e2e
proves the Model handles that real keypress, and the owner can run the interactive TUI directly. All
verify gates green; `go mod tidy` no drift (no new deps). **Phase 2 is complete.**

---

## 5. Open design calls to resolve *within* Phase 2

Record each as a short note / ADR amendment when it lands (don't pre-decide in the abstract):

- **The concurrency contract → ADR 0011** (settled by **P2.1**): the worker-goroutine engine +
  `p.Send` event bridge + approval rendezvous + cancel, exactly as C1–C5. Worth an ADR because
  it is the product binary's load-bearing structure and the template every future interactive
  consumer copies. (Phase 1 set the precedent: control-flow calls → ADR 0007.)
- **Charm v2 vs v1** (settled by **P2.2** — **kept v2**): textarea/viewport/spinner all exist in
  `bubbles/v2 v2.1.0`, so no widget lagged and the v1 fallback was not triggered. All three deps
  resolve on the **`charm.land/…`** path (lipgloss + bubbles moved there at these versions, like
  Bubble Tea). Recorded in the P2.2 commit and ADR 0011's build note.
- **Config file or not** (settled by **P2.5** — **file landed**): the owner's uniform `~/.apogee`
  layout names `config.yaml` alongside `library/` and `sessions/`, so a user-level config file
  earned its place (set endpoint/model/mode once, not every invocation). `yaml.v3` is now a direct
  dep. A single user-level file at `<home>/config.yaml`; a workspace-local `.apogee/config.yaml`
  (a second file tier) is deferred unless it earns a precedence layer of its own. The file is
  **auto-seeded on first run** from a `//go:embed`-ed, behaviour-neutral template
  (`cmd/apogee/defaults/config.yaml`), never overwriting an existing one.
- **TokenEvent backpressure** (touched by **P2.1/P2.3**): start lossless via `p.Send`; add
  *coalescing* (never dropping) only if profiling shows queue pressure — behind the sink seam.
- **Session UX depth** (settled by **P2.5**): minimal `--resume` + snapshot-on-clean-quit, with a
  printed resume hint; no browser/picker (§6). Snapshot-mid-run is deferred — a `ctrl+c` while a
  worker runs cancels and quits without saving (snapshotting from the Update goroutine would race
  the single-goroutine Agent the worker owns), so only a quiescent-boundary quit persists state.

---

## 6. Out of scope for Phase 2 (explicit non-goals — keep the shell thin, the engine untouched)

- **Auto mode + Confinement** — Phase 3 (needs the `Confiner` backends). Phase 2 = Plan +
  Ask-Before; Auto is refused gracefully (C8). **No agent-logic changes in this phase at all** —
  if the TUI seems to need a new engine behaviour, that is a Phase-1-surface gap to fix in the
  library, not logic to add in `internal/tui`.
- **Sub-agents / `Depth > 0` rendering** — Phase 3. The TUI must *tolerate* nested events
  without crashing; rich nested rendering waits.
- **MCP / web / network tools** and the rest of the **30-tool suite** — Phase 3. Phase 2 renders
  whatever tools the Phase-1 registry exposes (the local file set).
- **Mechanisms catalogue, self-regulation, the `MechanismFiredEvent` debug UI as a real feature**
  — Phase 4. A hidden debug pane is the most Phase 2 should show.
- **`apogee headless`** (serialized events to stdout) — an *optional* scripting surface, **not**
  the bench contract (ADR 0001); defer unless cheap. **`apogee probe`** — Phase 5.
- **Model discovery picker** — the provider has `/v1/models` (P1.1); a TUI model picker is a
  nice stretch, not the deliverable. Keep `--model` explicit for v1.
- **Theming / config-driven keybindings / mouse** — later polish. Pick sane defaults now.
- **Windows-specific terminal handling** — Phase 5. Bubble Tea is cross-platform; nothing
  Windows-specific is needed for the deliverable, and the cross-build must stay green.

---

## 7. Acceptance-criteria summary (quick gate)

A reviewer can check Phase 2 with:

```
gofmt -l .                          # empty
go vet ./...                        # clean
go build ./...                      # ok
go test -race ./...                 # tui model/update + the C1–C5 seam + the hermetic e2e
grep -rl '"github.com/airiclenz/apogee"' internal/   # empty (ADR-0010 invariant; incl. internal/tui)
GOOS=windows GOARCH=arm64 CGO_ENABLED=0 go build ./...   # + the other 5 cross targets
./apogee --help                     # cobra usage, exit 0
```

…plus the **deliverable**: a real coding conversation with a **live local model** in the
terminal — assistant text streams, a tool call appears, the human approves the write, the result
renders, the Exchange completes — driven entirely over the Phase-1 public API, with `internal/tui`
holding no agent logic (P2.6). The hermetic httptest e2e is the reproducible proof; the live run
is the final confirmation (and closes the open Phase-1 live-eval step through the product UI).

---

## 8. Suggested skills

- **`/coding-standards`** — **mandatory** for every Go task here (`coding-standards.go.md` +
  `testing.go.md`); load before writing each body. Note §9 of the TDD: where a standard fights
  the plan or official Go/Bubble Tea idiom, the plan/idiom wins (e.g. the `Update`/`View` shape).
- **`manage-llm-server`** / the llama-launcher MCP control endpoint — to load a tool-capable
  model before the P2.6 live run.
- **`run` / `verify`** — to drive the TUI against the hermetic httptest model and then the live
  local model, and confirm the streamed-tokens → tool-call → approval → result → message loop
  end-to-end on a real model.
- **`/handoff`** at session end; **`archive-handoffs`** to retire the consumed Phase-1 handoff
  once P2.0 lands.
```
