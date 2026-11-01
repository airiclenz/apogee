---
title: A drag-select stops at the transcript edge instead of scrolling on
status: open
type: task
priority: 3
plan: 2026-11-01 - 00 - tui-row-detail-gauge-and-notify-plan
created: 2026-11-01T10:43:53Z
---

Dragging a selection to the top row (while scrolled up) or below the transcript does nothing: the head keeps its last cell and the viewport never scrolls, so a selection longer than one screen needs wheel scrolling mid-drag. Deferred as polish in docs/plans/archived/tui-input-deepening-plan.md (item 3).

Owner call 2026-11-01: ~50 ms tick, 1 line per tick plus 1 per row the pointer sits past the bottom edge, capped at 5; stops on release, on re-entry, or at the transcript top/bottom; reaching the bottom resumes following.

## Acceptance

- Holding a drag on row 0 while scrolled up scrolls up and extends the selection; holding it below the transcript scrolls down and extends it.
- Release stops the scroll and copies the whole selection.
- layout.md states the edge rule; a mouse test pins both directions.
