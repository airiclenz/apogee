---
title: ADR 0007 phase-1 realisation (p1.2)
---

Building the full Turn/Step state machine (P1.2) forced two control-flow sub-decisions this
ADR's Decision left abstract. They refine — they do not change — the boundary contract above.
(They were the two "decide within Phase 1" calls the
[Phase-1 detail plan](../../../../docs/plans/archived/phase-1-detail-plan.md) §5 flagged against this ADR; recorded
here as the canonical home, mirrored in the TDD §6 status notes.)

- **Streaming + Approval interleave — *stream fully, then gate*.** When a streamed reply
  contains tool calls, the loop consumes the stream to its terminal `Delta` and **closes the
  SSE body** before any tool is dispatched; the synchronous `Approver` is then consulted at a
  sub-step boundary *after* the connection is closed. So a blocking human-in-the-loop Approval
  never holds an open Upstream connection, and there is never a half-streamed token *and* a
  pending Approval to capture. The `EventSink` sees, per Turn: `TokenEvent`s (live) →
  [stream ends] → `ToolCallEvent` → `ApprovalEvent` (around the blocking `Approve`) →
  `ToolResultEvent`. This is the natural reading of "streaming and Approval happen *inside* a
  Step" — they are sequenced within it, not concurrent.

- **Event delivery — synchronous, in-order, no buffer/drop in the loop.** `EventSink.Emit` is
  called synchronously in Turn order; the loop neither buffers nor drops. The "Emit must not
  block the loop" contract is the **host's** to honour (a buffered channel adapter with a drop
  policy for the TUI sits behind the same interface). The bench consuming Events as ordered Go
  values is exactly what reproducibility needs.

- **Cancellation rolls the whole Turn back.** A cancel delivered mid-stream *or* mid-tool drops
  this Turn's appended assistant/tool messages back to the boundary the Turn began at (and
  re-queues any deferred corrections it drained), returns `StatusCancelled` **without**
  advancing the Turn counter, and keeps the user input — so the snapshot taken there resumes
  and **re-attempts** the Turn from serializable state rather than continuing from a partial
  one. A re-run write tool overwrites idempotently (ADR 0008).

> **Amended 2026-09-27 (ADR 0088 D1).** This consequence is superseded: a
> cancelled Turn that issued tool calls is now settled, not rolled back — a finished call keeps its
> real result, a running call gets a cancelled result and an unstarted one a not-run result, and the
> Turn stays in the conversation. A Turn cancelled before its reply finished streaming issued nothing
> that ran, so it is still dropped. The boundary guarantee stands: a cancel takes effect at the next
> boundary, and the state left there is serializable.
