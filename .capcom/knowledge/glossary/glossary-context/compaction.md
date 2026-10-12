---
title: Compaction
---

**Compaction**:
The **default** conversation-level reducer: *generatively* summarising older Turns into a
summary via the model, when the conversation exceeds a threshold. Meaning-preserving but
costs an extra model call. One fold, **two automatic triggers**: the *estimate-driven* one
fires at an Exchange boundary when the history outgrows its Budget allocation, and the
*overflow-driven* **emergency fold** fires when a request will not fit the window — the only
fold allowed to run **mid-Exchange** on the main agent, closing with a user-role bridge so the
retried request stays template-legal. A **child agent** also folds mid-Exchange, at any quiescent
Turn boundary under budget pressure, because its whole life is one Exchange and the boundary the
main agent waits for never comes. A fold that **faults** leaves the history untouched, so it stands
the estimate-driven trigger down for the rest of that Exchange rather than re-running the identical
failing call at the next boundary — the main agent re-arms when its next Exchange opens, a child
stands down for the delegation, and the emergency fold and `/compact` keep their own shot.
`auto-compact: false` opts out of all of them; the on-demand
`/compact` stays boundary-only. **Pruning** runs first: the cheap structural collapse of stale tool
results clears what it can at a Turn boundary before the estimate-driven trigger is ever reached, so
a model call is spent only on history that pruning could not relieve. Every trigger asks for the
same **structured summary**, written from scratch under fixed markdown headings — Goal, Constraints &
preferences, Progress (Done / In progress), Key decisions, Relevant files, Next steps — with an empty
section reading `None`; nothing parses it. See
ADR 0018.
_Avoid_: "compression", "truncation" (Compaction is generative and summarises).
