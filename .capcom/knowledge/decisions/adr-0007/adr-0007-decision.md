---
title: ADR 0007 decision
---

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

> **Amended 2026-09-27 (ADR 0089 D3).** An Exchange may open on an engine note as well as a user
> message: when a Background workflow ends while the agent is idle, the Driver opens a new Exchange
> on its one-line finish note (the wake).
