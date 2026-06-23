---
Status: accepted
---

# Step, Turn, and the quiescent boundary

## Context

The loop is an embeddable, steppable library ([ADR 0001](0001-agent-loop-is-an-embeddable-library-driven-by-an-external-bench.md)):
the bench steps it one unit at a time, snapshots, and resumes; the TUI streams it and lets a
user interrupt. For any of that to be well-defined, the unit of advance and the points at
which state is safe to capture must be precise — and the loop hosts third-party tools and
Mechanisms ([ADR 0002](0002-tools-are-an-open-extension-point-mechanisms-are-curated.md)),
which can fail in ways that must not take down the embedding host.

## Decision

**Turn** = one iteration of the loop: a single *primary* Upstream call and the work that
follows it (parse → dispatch tools → apply Mechanisms). Compaction's summarisation call is
*internal* to a Turn, not a Turn of its own. **Exchange** = one user input through to the
final no-tool response (usually several Turns).

**`Step()` advances the loop exactly one Turn and returns at a *quiescent boundary*** — no
in-flight stream, no in-flight tool call, conversation state fully serializable. Streaming
and Approval happen *inside* a Step; **snapshot, resume, and the bench's fork are valid only
at the quiescent boundary.** This is the single invariant that makes snapshot/resume and
forking coherent: there is never a half-streamed token or half-run tool to capture.

**Cancellation / interrupt is a Phase-0 primitive**, not a Phase-2 TUI add-on. A cancel
signal (Go `context` cancellation) delivered through `Step()` **takes effect cleanly at the
next quiescent boundary** — the in-flight Upstream call or tool is abandoned, state is left
serializable, and the Step returns with a cancellation outcome. The bench needs this for
hard-cap/timeout; the TUI needs it for user-stop. Both consume the same primitive, so it
must exist before either consumer is built — designing it in Phase 2 would mean retrofitting
the core loop.

**`Step()` recovers at the extension boundary.** A `panic` in a tool or a Mechanism is
**caught at that boundary**, converted to a typed `error` Event (a failed tool-result or a
skipped Mechanism), and the loop degrades to the quiescent boundary — it never unwinds into
the embedding host. This mirrors `net/http`'s per-request recover: a library that hosts
third-party extensions must isolate their faults, or one bad extension crashes every
embedder (TUI session, bench sweep, third-party app alike).

**Sub-agent stepping is top-level-only for v1.** Only the top-level agent is externally
stepped; a sub-agent runs to completion within its parent's Step. The driver is designed
**swappable** so nested stepping can drop in later, and the snapshot schema leaves room for a
suspended sub-agent.

## Consequences

- The loop must be written so that *every* `Step()` return is at a serializable boundary —
  no partial state may leak across it. This is a structural constraint on the loop's
  implementation, locked in Phase 0.
- The recover boundary is **per-tool and per-Mechanism**, not Step-wide: a panic in one tool
  fails that tool, not the whole Turn, so the loop can still make progress.
- Cancellation semantics are part of the public API contract from v0.1 (additively refinable,
  but present): embedders rely on "cancel takes effect at the next boundary."
- The bench's "a panic aborts a long counterfactual sweep" fragility dissolves as a free
  consequence of the recover-at-boundary contract — it was a symptom of a missing *product*
  property, not a bench-only concern.

## Phase-1 realisation (P1.2)

Building the full Turn/Step state machine (P1.2) forced two control-flow sub-decisions this
ADR's Decision left abstract. They refine — they do not change — the boundary contract above.
(They were the two "decide within Phase 1" calls the
[Phase-1 detail plan](../plans/phase-1-detail-plan.md) §5 flagged against this ADR; recorded
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
