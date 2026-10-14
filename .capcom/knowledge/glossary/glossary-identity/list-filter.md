---
title: "List filter: the type-to-filter every searchable list carries"
refs: [internal/tui/listsurface.go]
---

**List filter**:
The **type-to-filter** every *searchable* list in the TUI carries — `/settings`, `/sessions` and the
picker kinds that list things (models, servers, launch profiles, cycles, schedules, efforts, modes,
fork points). Printable keys and pastes type into it with no activation key; backspace trims it;
`esc` closes the pane, filter and all. It narrows the list to rows whose **matched text** — what the
row *says it is* (a key path and its section, a name and its gloss, a session's title), never its
live state (values, marks, stats, ages, spend) — contains the filter, case-insensitively, without
reordering. It shows on the pane's title row as `filter: …▌` while it holds text, keeps the
highlight on its row while that row still matches, and states a zero-match result as one row
(`no setting matches "zzz"`). A **question** pane — a short list of fixed answers (the workflow
boundary, the migration and adoption prompts, the approval and ask_user menus) — has no list filter.
See ADR 0053 and its 2026-10-14 amendment.
_Avoid_: "search" (it neither ranks nor jumps; it narrows), "fuzzy find" (it is substring), "filter
mode" (there is no mode to enter).
