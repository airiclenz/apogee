---
title: The panel separator and the scroll track form two touching rules
status: open
type: task
priority: 4
labels: [tui]
plan: 2026-11-09 - 04 - side-panel-visual-alignment-plan.md
created: 2026-11-09T18:56:44Z
---

- When the transcript's scroll bar shows, its track (`│` in `chrome`) and the panel separator (`│` in
  `statusFaint`/`muted`, `internal/tui/sidepanel.go:218`) sit in adjacent columns. The result is
  two parallel strokes in two tones (frame `06-overflow-h20`).
- The separator is also louder than the track and the prompt-box border.

**Wanted**
- A recessive role for the separator (`chrome` or a divider role), or distinct shapes, so the
  transcript's bar and the panel's edge read as different things.
- The drag state's accent-bold separator stays.

## Acceptance

- A rendered frame with the scroll bar showing has no two same-shaped verticals in adjacent columns of different tones, or the separator matches the track's role.
