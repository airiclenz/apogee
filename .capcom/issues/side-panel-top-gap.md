---
title: Side panel content starts on row 1, below a blank top row
status: open
type: feature
priority: 3
labels: [tui]
plan: 2026-11-10 - 01 - panel-and-tool-shape-fixes-plan
created: 2026-11-10T10:30:32Z
---

The **Side panel**'s first section heading is painted on the window's top row (row 0), flush
against the top edge. The owner wants one empty row above it, so the content starts at row 1.

## Where

- `sidePanelColumn` / `sidePanelContent` (`internal/tui/sidepanel.go`) build the column's rows
  for a given height. The blank row goes first and comes out of that height, so the column stays
  exactly as tall as the window.
- Every row-indexed reader of the panel must move with it, or clicks and keyboard stops land one
  row off: pointer hit-testing (`internal/tui/mouse.go`), panel stops / block cursor
  (`internal/tui/panelstops.go`, `internal/tui/blockcursor.go`), and the overflow budget
  (`docs/layout/side-panel-layout.md` §Overflow).
- The narrow-window **overlay** (ADR 0111 D7) and the run view's panel should get the same
  top row, unless design decides otherwise.

The separator column stays full height: only the panel's content moves down.

## Docs

`docs/layout/side-panel-layout.md` §Where it sits (the sketch starts the panel's content on
row 0) and the matching `layout.md` section need redrawing. If ADR 0110 states where the
column's content starts, it needs a dated amendment.

## Why

Owner call 2026-11-10: content flush against the window top edge looks cramped next to the transcript.

## Acceptance

- With the panel docked, row 0 of the column is blank and the first section heading paints on row 1 (golden/frame test in internal/tui).
- Clicks on a section heading or row, and keyboard panel stops, hit the row painted under them (mouse/panel-stop test).
- The column never paints taller than the window; overflow accounts for the lost row.
- side-panel-layout.md and layout.md sketches show the blank top row.
