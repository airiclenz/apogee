---
title: "ADR 0022 addendum of 2026-08-25: a delegating Turn is progress-saved; the engine half keeps the boundary"
---

**The gap: one Turn is not one bounded unit of loss.** A `sub_agent` call runs synchronously inside
the Turn that issued it (`internal/tools/sub_agent.go`, dispatched from `internal/agent/loop.go`),
so a Turn holding a delegation stays open for as long as the child runs — 15+ minutes in the
session that surfaced this (`20260825T164640Z-ca180f38`), and a whole fan-out batch under
ADR 0039.
For that entire time the record on disk ended at the *previous* tool call: the assistant message
that delegated, the prompt it carried and every child event were nowhere on disk while the TUI
painted the run. A reader of the record mid-run — a second session, a reviewer, `apogee headless`
tooling — concluded that no sub-agent had been dispatched at all. Decision 1's "a crash loses at
most one Turn" held as written and still bounded nothing useful: for such a Turn it is unbounded in
wall-clock and in tokens.

**The rule: the TUI re-persists mid-Turn, pairing the last boundary snapshot with the live
transcript.** A **progress save** fires on the depth-0 `sub_agent` `ToolCallEvent` (the delegation
being issued), on every `ToolResultEvent` at depth ≥ 1 (a child crossing a tool boundary), and on
every `SubAgentPhaseEvent` reporting `SubAgentFinished`. Its engine half is not freshly taken: the
Model caches the last **quiescent-boundary** snapshot — the idle `Snapshot()` taken immediately
before each worker launch (after any `SettleExchange`/`AbortExchange` close, before `Submit`, so
it can never carry `pendingInput` the TUI cannot resume), refreshed by each Turn's own `turnSnapshotMsg` and by a
restored record's payload, dropped when `/clear` rotates the session. Its transcript half is
**live**. Bursts collapse in the existing single-flight latest-wins write queue, so the cadence
costs at most one in-flight write plus one pending, exactly as decision 1's saves do. Delegations
only: a long *leaf* tool (`terminal`, `console_read`) keeps the per-Turn behaviour — generalising is
a later one-predicate change, not a decision taken here.

**What does not move.** ADR 0007's boundary rule is
untouched: no snapshot is ever taken inside a Step, and the engine half of a progress-saved record
is by construction one that was taken at a quiescent boundary, so the record's engine half never
changes meaning. Decision 1's cadence for that engine half is unchanged — it still advances only
per-Turn; only *when the transcript half may be written* moves. The record shape and all three
schema versions stand (`RecordVersion`, the transcript blob's version, `domain.SessionVersion`):
an open `sub_agent` head in the blob **is** the in-flight marker, so no `Meta` flag and no browser
column were added. Decision 8's non-goal stands: the child's own `Session` is still never a record.

**Resume semantics: a progress-saved Turn re-attempts, exactly as a cancelled one does.** A record
written mid-delegation holds an open Turn, and restoring it is the same act as restoring a
cancelled Turn — the engine rolls back to the pre-request boundary and `/continue` re-runs the Step
that started the delegation, while sending a new message discards it. Because the transcript half
may now hold calls that were live at write time and are dead at read time, replay closes **every**
tool-call entry still open as *interrupted* and adds one note saying the unfinished work was not
kept. That rule also repairs the records cancelled Turns were already leaving behind, and it is a
replay-time rule only: the live paint path is unchanged, so a running delegation still paints as
running while it runs.

> **Amended 2026-09-19 (`apogee-2un`, Stage A).** Two readings of "discards it" above are now
> narrower. **A live cancel no longer empties the record.** `Esc` twice used to close the Exchange
> with `AbortExchange`, dropping every finished Turn along with the cancelled one — the idle save
> after the stop then wrote `messages: null` (48 sessions). The cancel and loop-error folds now
> close it with `Agent.SettleExchange`: the record written at idle holds a **closed** Exchange —
> the opening message and every Turn that finished before the stop — and only the cancelled Turn's
> in-flight work is gone, exactly what `endCancelled` rolled back. The cut is marked in the live
> conversation as an ephemeral `[engine — cancelled]` engine note on the last tool result, and
> that marker is **live-session only**: `Advice` is `json:"-"` and `recordContent` strips it (ADR
> 0076 D6), so the record and a resumed conversation carry the kept results and no marker.
> **A new message on a restored open Exchange settles it.** A record that still holds an open
> Turn — a session killed mid-task, or progress-saved mid-delegation — resumes as before
> (rollback to the pre-request boundary, `/continue` re-runs the Step), but sending a new message
> now settles the Exchange instead of aborting it: the Turns that finished before the interruption
> stand and the new message continues from them; "discards it" is true of the unfinished Step
> alone. `/clear` on such a session still aborts — the explicit throw-away. An Exchange with no
> finished Turn has no tool result to carry the note and is still rolled back whole; a
> `session.Aborted` mark lands only there. Plan `2026-09-19 - 01`.

**The bound, as now stated.** A crash loses at most one Turn of **engine** state; of the
**scrollback** it loses at most the work since the last child tool boundary.

### Considered options

- **Progress-save from a cached boundary snapshot (chosen).** Costs no engine change and no schema
  change, and the record's two halves keep their existing meanings — the engine half is a boundary
  snapshot, the transcript half is what the human saw.
- **Accept the bound and document it only.** Rejected: it leaves a reader of the record mid-run
  concluding that no delegation was issued, which is the actual harm — the wall-clock bound was
  only how we found it.
- **An engine Step boundary at the delegation (nested stepping).** Rejected *here*, not denied: it
  would supersede part of ADR 0007, reach into ADR 0039's fan-out and change what a bench arm
  compares, so it needs its own grill rather than a clause in this addendum. ADR 0007's
  "the snapshot schema leaves room for a suspended sub-agent" is the door it would come through.
