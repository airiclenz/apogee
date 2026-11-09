---
title: The side-panel spec's sketch shows chrome that no longer exists
status: open
type: chore
priority: 3
labels: [tui]
plan: 2026-11-09 - 04 - side-panel-visual-alignment-plan.md
created: 2026-11-09T18:56:44Z
---

The "Where it sits" sketch in `docs/layout/side-panel-layout.md` (lines 17-35) disagrees with
`layout.md` and the rendered frame:

| Element | Sketch | `layout.md` and the code |
|---|---|---|
| Session-name rule | `── session name ──` | `▔` hairline with the name inset |
| Prompt box | titled `╭─ prompt ─╮` | untitled |
| Gauge | `ctx 62% ▕████▍▏` | `20k/32k 62% ██████▎` |

Redraw the sketch from a rendered frame once the sibling restyle issues land, so it shows the new
header, fold and spacing vocabulary.

## Acceptance

- The sketch matches a current rendered frame's rule, prompt box, gauge and panel rows.
