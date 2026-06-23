---
Status: accepted
---

# Tools are stateless across Turns; external effects are non-forkable

## Context

The bench forks a run by deep-copying conversation state and copying the sandbox directory,
then driving two branches from byte-identical state
([ADR 0001](0001-agent-loop-is-an-embeddable-library-driven-by-an-external-bench.md)). That
only works if a tool holds no live state across the boundary the fork copies — otherwise a
branch would inherit a dangling handle, an open REPL, or a half-open connection. And some
effects (a call to an MCP server, a network request) cannot be copied at all: forking the
conversation does not fork the remote.

apogee-code's `terminal` and `python-exec` tools are already one-shot/stateless (a fresh
process per call, process-group kill, no persistent shell or REPL), so this is a contract to
*port and make explicit*, not a change.

## Decision

**Tools are stateless across Turns** — a clause of the public `Tool` interface:

- a tool's **only durable side effect is filesystem writes** (which the sandbox copy
  captures);
- **nothing live is held across the quiescent boundary** — no open process, REPL, socket, or
  cursor survives a Turn;
- `terminal` and `python-exec` stay **one-shot** (fresh process per call), matching
  apogee-code.

This makes the copyable-state hygiene of ADR 0001 actually hold: a forked or resumed run
inherits files and serialized conversation state, never a live handle.

**MCP and network are non-forkable external effects.** They reach state Apogee does not own
and cannot snapshot:

- the **bench disables them with deterministic stubs for v1** — the web/MCP tools stay in the
  model's menu (faithful to production; `toolfilter` and menu-reasoning Mechanisms see the
  real set) but return a fixed canned result, exactly as `RealSandbox` already does for
  network commands. Record/replay is **deferred** behind the same injectable seam: it does
  *not* enable fork-counterfactuals (a counterfactual diverges by construction ⇒ cache-miss
  exactly when it does something new); its only value is variance reduction in whole-task A/B,
  to be built when an external-tool task is actually worth validating.
- **production resume reconnects fresh** and makes **no server-side-state promise**: a resumed
  session re-establishes MCP/network connections from scratch; any remote state from before
  the snapshot is gone.

## Consequences

- The public `Tool` interface documents statelessness-across-Turns as a contract; tool authors
  (including third parties — [ADR 0002](0002-tools-are-an-open-extension-point-mechanisms-are-curated.md))
  must honour it. A tool that needs persistence must serialize it into conversation/session
  state, not hold it live.
- External-effect tools route through a **single injectable boundary** so the bench can swap in
  deterministic stubs (and, later, record/replay) without touching tool code.
- A bench task that *requires* live external content becomes always-fail under stubs and falls
  out of the frozen discriminating suite
  ([ADR 0009](0009-the-ab-decision-rule.md)) — v1 validates Mechanisms on the
  network-independent core; external-dependent task validation is out of scope and flagged.
- Disabling external effects keeps the bench's non-determinism confined to LLM sampling, which
  keeps the A/A noise floor — and therefore δ — clean (ADR 0009).
