---
title: Side panel sub-agents section outlives its 30 s linger on an idle frame
status: open
type: bug
priority: 2
created: 2026-11-08T13:24:46Z
---

The side panel's sub-agents section stays live for 30 s after the last run ends (`subAgentLinger`, internal/sidepanel/sidepanel.go:25), but that linger is checked only at paint time (internal/sidepanel/sidepanel.go:416, `now.Sub(lastEnd) < subAgentLinger`). An idle frame — no spinner tick, no other repaint — therefore keeps showing the section past 30 s until the next repaint happens for some other reason.

Deferred out of the side-panel run (plan `2026-11-08 - 00 - tui-side-panel-plan`).

## Acceptance

- With no other activity, the sub-agents section leaves the panel once 30 s have passed since the last run ended, without waiting for an unrelated repaint
- A test pins that the panel repaints at the linger deadline on an otherwise idle model
