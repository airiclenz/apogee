---
title: ADR 0077 context
---

A model running in apogee learns nothing numeric about its context window. The host knows the fill
exactly — prompt tokens from every upstream response, the window from llama.cpp `/props` or the
`context-window:` pin — and shows it to the human in three places (the TUI gauge, the sub-agent run
heads, `/usage`), but the only model-facing signals are after the fact: a truncation marker, a prune
stub, the post-compaction bridge, and the step-cap wrap-up directive, which counts steps rather than
tokens. A context-limited model, and a delegate above all, cannot therefore stop in time and report
what is left to its parent; it works until the fold takes the conversation from under it.

ADR 0076 D2 gives the engine row
an **advise** cell — text the model sees, fenced, capped, fail-open, landed as a trailer on the
closing tool result (D6) — but nothing has yet taken it: the class constant and the Bypass skip
exist, the slot, the provenance-derived fence, the ledger and the resume strip do not. Stage 3
(`apogee-rxj`) owns the *user* advise cell and waits on the same slot.

Two facts fixed the design. First, the fold does not wait for the window: the Budget holds 20% back
for the reply and gives History 60% of what remains, so the automatic Compaction line sits at roughly
**48% of the working ceiling** (`internal/context/budget.go`, `internal/domain/budget.go`). A child
agent folds at the first Turn boundary past it; the main agent defers to the next Exchange opening
and can climb past it mid-Exchange. A notice in "% of window" would put every rung above 50 out of a
child's reach. Second, the hard invariant (`AGENTS.md`): a model-facing behaviour above the Floor
ships **off** until bench evidence turns it on — which decides the default, and nothing else.
