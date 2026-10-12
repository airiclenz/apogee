---
title: Pruning
---

**Pruning**:
The **structural** conversation-level reducer that collapses *stale tool results* — and nothing
else — to one-line stubs when history outgrows its share of the Budget. Like Compaction and unlike
Tool-result capping it is **not a Reaction**: it stays on under Bypass and is gated only by the
file-only `prune-tool-results:` key (default on). It runs at **every Turn boundary,
mid-Exchange included**, rewriting committed history: above **70%** of the History allocation it
stubs the oldest results first, largest first within a Turn, until the fill is back under **50%**,
and it protects the **six** most recent tool-calling Turns entirely. Each stub reads
`[pruned: N lines from <tool> <argument> — re-run the call if you need it]` — the recovery is the
model's own re-run — and a pass reports itself to every Driver as one **`PruneEvent`** carrying the
results stubbed and the tokens freed. It skips whenever the context window is unknown, since a
fraction of an unknown allocation means nothing. Because it rewrites committed history it
invalidates the upstream prefix cache
(ADR 0023 §6), which is why the
band is wide rather than a single threshold.
_Avoid_: "compaction" (Pruning is mechanical and drops nothing but tool output; Compaction is
generative and summarises everything), "truncation" (the tool-result cap shortens text in place;
a prune replaces a whole result with a stub naming the call that produced it).
