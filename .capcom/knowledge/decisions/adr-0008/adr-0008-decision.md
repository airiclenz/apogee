---
title: ADR 0008 decision
---

**Tools are stateless across Turns** — a clause of the public `Tool` interface:

- a tool's **only durable side effect is filesystem writes** (which the sandbox copy
  captures);
- **nothing live is held across the quiescent boundary** — no open process, REPL, socket, or
  cursor survives a Turn;
- `terminal` and `python-exec` stay **one-shot** (fresh process per call), matching
  apogee-code.

This makes the copyable-state hygiene of ADR 0001 actually hold: a forked or resumed run
inherits files and serialized conversation state, never a live handle.

*(Amended 2026-08-25 by ADR 0059:
a **Console** is the one deliberate exception — live across Turns, but never across a snapshot,
fork, or resume, under the "production resume reconnects fresh" clause below. `terminal` and
`python_exec` stay one-shot.)*

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
