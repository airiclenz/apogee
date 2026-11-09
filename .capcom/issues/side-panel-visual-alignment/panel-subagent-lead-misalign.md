---
title: Running sub-agent rows misalign under the default spinner and lose its colour
status: open
type: bug
priority: 2
labels: [tui]
plan: 2026-11-09 - 04 - side-panel-visual-alignment-plan.md
created: 2026-11-09T18:56:44Z
---

**Misalignment**
- The default spinner style is `snake` (`internal/domain/uivocab.go`, `DefaultSpinnerStyle`), and
  its frames are two cells wide (pinned by `TestSnakeFrames`, `internal/tui/spinner_test.go:195`).
- The panel's other lead glyphs are one cell: `○` queued, `✓` done, `✗` failed, `◷` waiting
  (`internal/tui/sidepanel.go:445-455`).
- So on a running row the name, and every fact after it, sits one column right of the queued, done
  and failed rows.
- The spec promises facts in fixed columns so every row lines up (`docs/layout/side-panel-layout.md`
  §Sub-agent rows).

**Lost colour**
- The panel calls `m.spin.drawn` (`sidepanel.go:450`), which returns the bare frame, so the panel's
  spinner is uncoloured.
- The status line paints the same spinner through `spinnerAnim.view` with the colour loop
  (`internal/tui/spinner.go`).

## Why

The default configuration breaks the spec's alignment promise on every running sub-agent, and two
spinners on one screen should not look different.

## Acceptance

- Under every spinner style (and `ui.glyphs: safe`) the name column of running, queued, done, failed and waiting rows is the same column; a test pins it for `snake`.
- The panel's running glyph carries the same colour loop as the status line's spinner.
