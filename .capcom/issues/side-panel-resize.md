---
title: Resize the side panel by dragging its separator
status: open
type: feature
priority: 2
plan: 2026-11-08 - 02 - side-panel-drag-resize-plan
created: 2026-11-08T16:28:51Z
---

The **Side panel** has a fixed width of 36 columns (`sidePanelWidth`, ADR 0110 Decisions 6–7).
The user cannot make it wider or narrower. `capcom ui` lets the user drag the divider between its
two panes with the mouse; apogee should behave the same.

## Wanted (owner, 2026-11-08)

- **Drag the separator.** A left-button press on the panel's separator column (or its gutter
  column) starts a resize drag; motion moves the separator and the panel and transcript re-wrap
  live, like `capcom ui`; release ends it. The separator keeps its grab offset from the pointer.
- **Limits.** Panel ≥ 24 columns; panel ≤ window − transcript floor (100 for `auto`, 80 for `on`)
  − scroll bar. The show/hide gate (137 / 117) is unchanged. A stored width is clamped when the
  layout is computed, never rewritten, so a terminal resize keeps it.
- **Persist.** A new global key `ui.side-panel.width` (columns, default 36), written once on
  release when the width changed, through the settings host like the pin glyph. Editable in
  `/settings`.
- **Double-click** on the separator resets the width to the default.
- **`/panel width <n>|reset`** sets or resets it from the keyboard.
- **Accent separator** (accent tone, bold) while a drag is active.

## Constraints

- ADR 0110 rejects "proportional or user-set width": an amendment must supersede that.
- ADR 0106 §5 and `TestMouseMotionNeverRepaints` say mouse motion never repaints: the live drag
  is a narrow, ratified exception (only while a separator drag is active).
- Widths through the width authority (ADR 0030); the `Model` copied by value and under 32 KiB
  (ADR 0011); the cached frame re-keys on the panel width.
- Text selection, the panel's click targets and wheel behaviour stay as they are.

Plan: `docs/plans/2026-11-08 - 02 - side-panel-drag-resize-plan.md`.

## Why

A fixed 36-column panel is too narrow for long file paths on a wide terminal and too wide when the transcript needs room; capcom ui already lets the user drag its divider.

## Acceptance

- Dragging the separator resizes the panel live within the limits and persists ui.side-panel.width on release
- Double-click on the separator and /panel width reset restore 36 columns
- Tests pin the drag, limits, persistence, reset and the motion-repaint exception; ADR 0110 and 0106 amended; layout and manual docs updated
