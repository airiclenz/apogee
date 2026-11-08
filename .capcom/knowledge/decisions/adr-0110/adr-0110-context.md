---
title: ADR 0110 context
---

Session state is scattered across one-column surfaces. Usage is in the `/usage` pane and the status
line's context gauge, advice in `/advice`, a sub-agent's progress in its framed row and Run view,
the task list in cards inline in the transcript, and a file write only in its tool block. Each one
either has to be opened or scrolls away. A wide terminal has columns to spare, and the owner's idea
(IDEAS.md "Sidepanel", 2026-09) was a panel for that state that shows itself once the window is wide
enough. Four archived plans (2026-09-14 to 09-23) scoped it out, and the IDEAS item "complete
key-based navigation — including side-ways arrow keys" waits on it.

Facts that shaped the decision (survey of 2026-11-07):

- **Width is expensive to change.** `m.width` is part of the paint cache's frame key, so a
  transcript width change re-wraps and repaints everything. `layout.md` (the scroll bar section)
  already rules that the width never moves because something appeared or vanished on its own.
- **Selection assumes column 0.** apogee captures the mouse and does its own drag-select. The cell
  mapping holds because the transcript body starts at screen column 0, so a panel on the left would
  break it and a panel on the right does not.
- **The height budget.** One row budget is spent on the transcript slot, and the transcript pays
  for every pane, band and dropdown. Run view and the report panes sit in that slot.
- **About 50 sites read `m.width`** outside tests, mostly the prompt box, status line, footer and
  panes.
- **Where the data is.** The engine already emits `UsageEvent`, `ContextEstimateEvent`, the
  `SubAgent*` events, `WorkflowPhaseEvent` and `ReactionFiredEvent`. A file write's path rides
  `ToolResultEvent.WriteTarget`, but nothing collects it per session. The task list exists only as
  `task_list`'s result text, which the TUI parses by markers. MCP state, staged interjections and
  Firings have no engine Event. `ContextEstimateEvent` carries only a token total, with no
  breakdown.
- **ADR 0031** demands only that nothing model-visible or safety-relevant lives in one Driver; a
  display-only panel would not breach it on its own.
