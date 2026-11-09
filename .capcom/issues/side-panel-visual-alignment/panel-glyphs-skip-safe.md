---
title: The side panel's own glyphs ignore ui.glyphs safe
status: open
type: bug
priority: 3
labels: [tui]
plan: 2026-11-09 - 04 - side-panel-visual-alignment-plan.md
created: 2026-11-09T18:56:44Z
---

- The panel's glyphs `◆ ⊙ ● ▸ ○ ↳` are fixed constants outside `glyphSet`
  (`internal/tui/theme.go:84-97`, `glyphPanel*`).
- So under `ui.glyphs: safe` they never fall back (frame `12-safe`). `⊙` (U+2299) is the least
  font-safe of them.
- Under safe, the transcript's `♦` and the panel's `◆` become near-identical.
- `docs/manual/configuration.md` (around 1157-1168) says safe "swaps every mark apogee draws" and
  lists only four exceptions. The code contradicts the manual.

`panel-section-user-fold` changes which glyphs the panel uses (`◆` and `⊙` go; `●`/`○` lead, and
`▶`/`▼` come from the existing set). Whatever glyphs remain move into `glyphSet` with safe
equivalents.

## Acceptance

- Every glyph the panel paints comes from `glyphSet` (or the manual's named exceptions), and a `ui.glyphs: safe` frame shows only safe-set glyphs in the panel; a test pins it.
- The manual's list of exceptions matches the code.
