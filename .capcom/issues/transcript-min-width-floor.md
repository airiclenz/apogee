---
title: "Side panel cannot be dragged wide: transcript minimum width too large"
status: open
type: feature
priority: 2
labels: [tui]
plan: 2026-11-10 - 01 - panel-and-tool-shape-fixes-plan
created: 2026-11-10T10:18:08Z
---

Dragging the **Side panel** separator stops long before the owner wants it to: the transcript
(the main session view) keeps too large a minimum width, so the panel cannot be made wide enough.

## Today

`clampPanelWidth` (`internal/tui/sidepanel.go`) holds the panel to at most the window less the
show mode's transcript floor less the scroll-bar column. Both the layout and the separator drag
(`dragPanelSeparator`) clamp through it (adr-0110-amend-1108 (a)). The floors are:

- `sidePanelAutoFloor = 100` (`ui.side-panel.show: auto`)
- `sidePanelOnFloor = 80` (`ui.side-panel.show: on`)

So under `auto` the transcript never goes below 100 columns. In a 160-column window the panel
cannot grow past about 59 columns, however far it is dragged.

The same floor does two jobs:

1. **The gate:** whether the column docks at all (`sidePanelLeastWindow`: floor + gate width 36
   + scroll bar, so it docks from a 137 / 117-column window).
2. **The drag/layout ceiling:** how wide the user may make the panel once it is docked.

Job 1 wants a generous floor: an *automatic* choice should not squeeze the transcript. Job 2 is an
explicit user choice and should not be held to the same number.

## Wanted (owner, 2026-11-10)

The user can drag the panel much wider, leaving the transcript a much smaller minimum width.
Direction: split the two jobs. Keep the gate floors, and give the clamp a separate, much lower
transcript minimum: the narrowest width at which the transcript, prompt box and tool blocks still
render without breaking. Pick the number in design from the layout specs (`layout.md`,
`docs/layout/side-panel-layout.md`, `docs/layout/prompt-box-layout.md`) and the narrow-window
behaviour the TUI already has.

Touches ADR 0110 (and adr-0110-amend-1108 (a)), which needs a dated amendment.
`layout.md` §side panel and `docs/layout/side-panel-layout.md` state "100 under `auto`, 80 under
`on`" as the clamp, so they need updating, along with any drift test that pins those numbers.

## Why

The drag-resize feature (side-panel-resize) exists so the user can trade transcript width for panel width, but the 100/80-column floor caps the trade far short of what the owner wants.

## Acceptance

- With the panel docked, a separator drag can narrow the transcript to the new, lower minimum (named in ADR 0110 amendment), pinned by a drag test in internal/tui.
- The dock gate (the window width from which the column shows under auto / on) is unchanged unless the design says otherwise.
- At the new minimum the transcript, prompt box and tool blocks render without breakage (golden or frame test).
- A persisted ui.side-panel.width wider than a smaller window allows is still clamped by layout, never written back.
- layout.md, docs/layout/side-panel-layout.md and ADR 0110 state the new rule.
