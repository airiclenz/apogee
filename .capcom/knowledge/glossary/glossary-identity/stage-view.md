---
title: Stage view
---

**Stage view**:
The level of the **Run view** stack that lists one **Workflow** stage's items. A foreground
Workflow's block shows one row per stage; opening a started stage of more than one item (or more
than one round, or a retried item) pushes a stage view, which lists one delegation-style row per
item — the item's receipt summary on its row, `round N` sub-headers for a repeated stage, dim
`attempt N` sub-rows for an item's earlier attempts — each opening that item's run view. A stage of
one item, run once, skips the level and opens its item's run view directly. The breadcrumb reads
`← main › <workflow> › <stage>[ › <item>]`, and `esc` goes up one level. A stage view is **not a
run**: its prompt box is read-only, it states no context gauge, and `^x` stops only an item's run
from that item's row — stopping the whole Workflow stays `/workflows`. Like every view level it is
**Driver state** and is never restored, but the stage rows it opens from are kept in the **Session
record**, so a resumed session's rows reopen read-only. Ratified 2026-09-28
(ADR 0090, amending ADR 0063 D4).
_Avoid_: "stage run" (a stage is not a run — its items are), "sub-view", "workflow view" (the
`/workflows` pane is a different surface).
