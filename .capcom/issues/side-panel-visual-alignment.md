---
title: "[epic] Align the side panel's look with the rest of the TUI"
status: done
type: epic
priority: 2
labels: [tui]
plan: 2026-11-09 - 04 - side-panel-visual-alignment-plan.md
created: 2026-11-09T18:56:44Z
closed: 2026-11-09T22:59:20Z
reason: resolved by plan '2026-11-09 - 04 - side-panel-visual-alignment-plan' (archived at 21eb9fbf); all children closed and item 16's rendered dark/light/safe frames show the transcript vocabulary
---

The side panel (`docs/layout/side-panel-layout.md`, glossary `Side panel`) was built with its own
visual vocabulary. It does not read as part of the same UI as the transcript, status line, footer
and `/settings`. Analysis on 2026-11-09 combined rendered frames, painted in-process at 170x45 with
the dark, light and safe glyph sets, with a code-and-spec audit. Misalignments showed up in four
areas the owner named: section headers, colours and tones, density and spacing, and expand/collapse.
The analysis also found two bugs and some spec drift. Each child issue covers one strand.

Owner decisions (2026-11-09):
- The restyle may change headers, glyphs, spacing and tones freely. Behaviour stays: sections,
  modes, stops, the width rules and the overflow order. ADR 0110/0111 and the spec are amended
  where a visual rule changes.
- Sections fold like transcript blocks, with `▶`/`▼` at the right edge. A header click or `⏎`
  toggles the fold.
- The pin moves to the header's lead glyph: `●` when pinned, `○` when auto. It replaces `◆`.
- A user's fold persists in config.
- A section folded by overflow wears `▶` like a user fold.

Framing (borders around the panel) is out of scope.

## Why

apogee's TUI is one visual system (`layout.md`). A column beside the transcript that speaks a
different glyph, tone and spacing language reads as bolted on, and teaches the reader two meanings
for one slot. For example, the right-edge cell means "fold" in the transcript and "pin" in the panel.

## Acceptance

- Every child issue is closed.
- A rendered wide frame (dark, light, `ui.glyphs: safe`) shows panel headers, facts, folds and spacing in the transcript's vocabulary.
