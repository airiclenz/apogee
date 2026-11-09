---
title: The panel's usage rows differ from the status-line gauge and /usage
status: done
type: task
priority: 3
labels: [tui]
plan: 2026-11-09 - 04 - side-panel-visual-alignment-plan.md
created: 2026-11-09T18:56:44Z
closed: 2026-11-09T22:59:20Z
reason: resolved by plan '2026-11-09 - 04 - side-panel-visual-alignment-plan' (archived at 21eb9fbf), item(s) 13
---

**Gauge background**
- The panel reuses `renderGaugeBar` (`internal/tui/sidepanel.go:605` → `internal/tui/model.go:5267`).
- That paints the bar on the status line's surface background (`#000000` dark, `#ffffff` light) on
  top of the panel's transparent background.
- On a terminal whose background is not exactly the theme's, the bar shows as a slab.

**Order**
- The status line reads `20k/32k 62% ██████▎`.
- The panel reads `██████▎    20k/32k`, with the bar first.

**Vocabulary**
- The panel prints `in 4k · out 300` and a literal `0` (`sidepanel.go:597-635`).
- `/usage` says `prompt`/`completion` and leaves a zero cell blank (`layout.md` §/usage).

**Wanted**
- A panel gauge style with no background.
- Figures before the bar, as on the status line.
- The same words and zero rule as `/usage`.

## Acceptance

- Rendered dark and light frames show the panel's bar cells with no background colour.
- The usage rows order figures then bar, and use `/usage`'s words and blank-zero rule.
