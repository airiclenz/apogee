---
title: The panel's Usage header can read over 100% while the gauge clamps
status: done
type: bug
priority: 2
labels: [tui]
plan: 2026-11-09 - 04 - side-panel-visual-alignment-plan.md
created: 2026-11-09T18:56:44Z
closed: 2026-11-09T22:59:19Z
reason: resolved by plan '2026-11-09 - 04 - side-panel-visual-alignment-plan' (archived at 21eb9fbf), item(s) 1
---

- `UsageView.WithFill` builds the header summary from `fill.Percent()`, which has no clamp
  (`internal/sidepanel/sidepanel.go:636`, `Fill.Percent` at `:77`). A fill over the window
  therefore reads `Usage 137%`.
- The status-line gauge, `/usage` and the panel's own sub-agent rows all clamp at 100
  (`internal/tui/model.go:5252`, `internal/tui/usage.go:431`, `internal/tui/sidepanel.go:462`).
- The spec says the panel's usage fill "reads exactly what the status-line gauge reads"
  (`docs/layout/side-panel-layout.md` §Run view).

## Acceptance

- A fill above the window renders the same percentage in the panel header, the panel's usage row and the status-line gauge; a test pins it.
- One shared clamped percent helper serves all of them.
