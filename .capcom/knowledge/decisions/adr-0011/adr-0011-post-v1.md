---
title: "ADR 0011 post-v1 realisation (apogee-code track): synchronous engine calls from the `Update` goroutine"
---

C1 states the invariant as "the `Update` goroutine never touches the Agent." The apogee-code
feature-parity track (mini-language, `/clear`, `/compact`, live Shift+Tab, session-save on quit)
added four deliberate `Update`-goroutine calls into the engine, so the contract the next
interactive consumer must copy is narrower than that one-line summary. It refines — it does not
change — the Decision: the Agent's **Exchange is still driven from exactly one worker at a time**
(the single-worker invariant is untouched, `p.Send` is still the only cross-goroutine event path,
the approval rendezvous is unchanged). Beyond driving an Exchange, the `Update` goroutine may call
the engine only under one of two conditions.

- **Idle-only synchronous calls — the engine is quiescent, no worker owns it.** The context and
  snapshot seams run straight on the `Update` goroutine because each is reached only where no
  worker is in flight: `ClearContext` (`/clear`) and the `Compact` launch decision are reached
  only from `submit` at `stateIdle` (`runCommand`); `AbortExchange` runs on `cancelledMsg` /
  `errMsg` (since 2026-09-19 `SettleExchange`, which keeps the finished Turns and falls back to
  the abort's rollback only when none finished — `AbortExchange` itself is now `/clear`'s close
  of an interrupted Exchange; ADR 0017's 2026-09-19 note), i.e. *after* the worker has returned
  its terminal `Msg` and handed the engine back
  (the post-Esc / post-fault un-wedge — without it the Agent stays in-Exchange and the next
  `/clear` or message is refused with `ErrInputPending`); `Snapshot` runs from `quit` only on the
  `!busy()` path (`saveSession`; since 2026-08-02 that closing flush is `saveAtIdle`, which takes
  the same Snapshot on the same goroutine and then queues the write). The single-goroutine contract forbids *concurrent* use, not a
  synchronous call made once the sole driver has already returned — so all four are safe by
  construction, each guarded by the **state machine** rather than by a lock.

- **The one mid-`Step`-safe call — `SetMode`, mutex-guarded.** Shift+Tab cycles the autonomy
  mode live in *every* state, including while a worker is mid-`Step`. This is the single method
  the Agent documents as goroutine-safe: `SetMode`/`Mode` take an `RWMutex` (`modeMu`, `agent.go`)
  over the one field shared across goroutines, so the UI's write can race-free overlap the
  worker's dispatch-time read and takes effect at the next tool call. It is the explicit exception
  named in the Agent's own doc — nothing else may be called while a worker is in flight.

**The seam a consumer must copy, precisely stated.** Drive each Exchange from one worker; call the
context/snapshot seams (`ClearContext`, `Compact`, `AbortExchange`, `Snapshot`) from the `Update`
goroutine **only where the state machine proves no worker is running**; and add a new mid-`Step`
call **only** behind the same mutex-guarded, documented-goroutine-safe surface `SetMode` carries.
This mirrors ADR 0007's realisation notes: the
boundary contract is unchanged; these entries record the exact control-flow calls that live at it.

**Amendment (2026-07-27) — there is a THIRD class, and it was already in the tree: between-Steps
calls by the goroutine DRIVING `Step`.** The rule above enumerates two classes and, read strictly,
misdescribes a call this package has made since the session work landed: the worker calls
`eng.Snapshot()` *between* Steps for the per-Turn save
(ADR 0022) — not from the
`Update` goroutine, and not behind a mutex.
ADR 0025 names that class and adds its
second member, `Agent.Interject`, which commits the human's mid-run remark into the open Exchange at
the same boundary. Its safety is structural rather than either of the other two arguments: **between
Steps, the driving goroutine is the engine's single driver, so the boundary IS the synchronization**
— no state-machine proof is needed (a worker *is* running, it is simply the caller) and no lock is
taken. The class is deliberately narrow: it is available only to the goroutine that owns the
Exchange, only at a quiescent boundary, and a call made from anywhere else races the loop's own
writes. So a consumer copies **three** rules, not two — idle-only under the state machine,
`SetMode`-class under a documented mutex, and between-Steps under the boundary itself. Nothing above
changes; the idle-only list is unchanged (ADR 0024 added `Rebind` to it), and the single-worker
invariant is untouched — a message typed while a worker runs is *staged*, never submitted.

**Note (2026-07-25) — "thin renderer" now holds for tool outcomes too: the view reads a typed
summary instead of re-deriving one from prose.** Until this date the tool presenter — then
`internal/tui/toolpresent.go`, split along its seams and deleted on 2026-08-20 (ADR 0043), its
vocabulary now in `internal/tui/toolregistry.go` — reconstructed *what a tool had done* by
pattern-matching the free-text result string the tool wrote
for the **model** — five regexes and the five extractors around them, across seven of the
registry's 21 entries. That was agent knowledge in the renderer wearing a rendering costume: a
stringly-typed cross-package contract with no type (the TUI does not import `internal/tools`), so a
wording change in a tool silently degraded a card with no compiler nudge and no failing test in the
package that changed. It was also resting on a coincidence — `clampToolResult` and any
`PostToolResult` Mechanism may rewrite `Content` before the view ever sees it.

The fact now travels as data: a tool attaches a sealed `domain.ToolSummary`
(ADR 0002's 2026-07-25 note)
and the registry's per-tool stat hook words it. **This makes the ADR *more* true, not
less** — the renderer that stops parsing is thinner than the one that parsed — and it is the
Event-fold posture applied to a second stream: the engine hands the view typed values; the view
words them.

**The two halves are deliberately independent, and the wording is the view's.** A summary carries
only what the tool already computed for its own header, never a sentence; the view formats its own
(`Located %q on lines: …` is built in `internal/tui`, not read out of the result). That several
card lines still read like the tool's own header is what made "the rendered output does not change,
byte for byte" a checkable acceptance oracle for the change — it is not a contract, and the view
may reword without touching a tool. A result carrying **no** summary still renders from prose, so
a third-party tool, or any built-in that attaches none, is unaffected.
