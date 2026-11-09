---
title: Side panel rows have no section spacing, body indent or right margin
status: open
type: task
priority: 2
labels: [tui]
plan: 2026-11-09 - 04 - side-panel-visual-alignment-plan.md
created: 2026-11-09T18:56:44Z
---

Rendered at 170x45 (frame `01-wide`), compared with the transcript and the chrome below.

**Today**
- Sections stack with no blank row between them.
- Body rows start in the header's own column.
- Changed-file, usage and MCP rows have no lead cell, so the text column is ragged from section to
  section.
- The left gutter is one cell (`internal/tui/sidepanel.go:39`).
- Content runs to the window's last column: the pin glyph and the right-aligned facts sit at x=169
  (`sidepanel.go:684`).
- In the overlay, transcript text runs right up to the overlay's `│` (frame `09-overlay-110`).

**The rest of the UI**
- The transcript leaves one blank line between blocks and hangs bodies `bodyIndent` (2) columns in
  under `┝`/`┕`.
- `/settings` requires a spacer line before each section header
  (`docs/layout/settings-screen-layout.md`).
- The status-line gauge and the footer end 2 columns short of the edge (x=167;
  `internal/tui/model.go:4117-4118`).
- The transcript's fold indicator keeps `bodyRightGutter`.

**Wanted**
- A spacer row before every section header but the first. The overflow allocation counts spacers
  and drops them first under pressure.
- Body rows indented by `bodyIndent`, or hung under `┝`/`┕` branches, with every row given a
  consistent lead cell.
- A right margin that lines the panel's right edge (fold indicator, facts) up with the gauge and
  footer above and below it. The click and hit cells move with it.
- The left gutter equal to the transcript's `bodyIndent`.
- A blank column between the overlay and the transcript text under it.

## Acceptance

- Rendered frames show one spacer row between sections (dropped first on a short window), bodies indented under their header, and the panel's right-edge cells in the same column as the status line's last cell.
- Pointer hit-testing (pin, fold, body rows, grip) and navigation-mode stops follow the new geometry; the existing side-panel tests pass.
- The spec's Rows/Columns and sketch say the new geometry.
