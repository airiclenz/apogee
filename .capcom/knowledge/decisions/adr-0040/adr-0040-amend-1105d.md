---
title: "ADR 0040 amendment of 2026-11-05 (d): painted row edges are never erased"
---

`adr-0040-amend-1105c`'s bottom chrome — and every other field apogee paints to the full width —
was right in the cell buffer but not on screen in Terminal.app (`TERM_PROGRAM=Apple_Terminal`,
`TERM=xterm-256color`): the idle status line, footer row 2 past the git segment and footer row 1's
margin after the mode marker showed the terminal's own (translucent) background
(`.capcom/issues/bottom-chrome-erased-cells.md`). The cause is the renderer, not the paint.
ultraviolet's `TerminalRenderer` does not write a row's trailing blank run when that run's cell is
clearable (`canClearWith`: a plain space with no underline, link or attribute beyond bold, faint,
italic or blink): `clearToEnd` sends `EL` 0 with the run's pen set, and `transformLine`'s
leading-run path sends `EL` 1 the same way. That holds only on a terminal that erases in the
current background (bce); Terminal.app erases in its own. Mid-row runs the renderer sends as `ECH`
render correctly there.

From here:

- **A painted field's cells at a row's edge are written as characters, never left to a terminal
  erase.** A row's last cell, and separately its first, that is a width-1 space with a background
  set carries the conceal attribute (SGR 8), which `canClearWith` refuses, so the renderer writes
  the run out instead of erasing it. The cell's content stays a space, so copied text is unchanged,
  and its style is otherwise its own; the sealed cell is reset-bracketed so the conceal does not
  carry into the rest of the row.
- **Sealed once, in `joinFrame`.** One frame-level pass (`sealRowEdges` in
  `internal/tui/boxdraw.go`) runs on every squared line; no field renderer seals itself. It decides
  on ultraviolet's own parse of the line, the one the renderer runs, and leaves a line with no
  background SGR unparsed and unchanged.
- **Scope: every painted field** (owner, 2026-11-05) — the bottom chrome, sent prompt blocks, the
  breadcrumb, popups, the staged band, and any future field that reaches a row's edge in painted
  blanks. Unpainted rows (the transcript, the gap row above the `▔` rule) erase in the default pen
  and are untouched, and mid-row `ECH` runs are left alone.
- ultraviolet is not patched, forked or given overridden terminal capabilities.

Pinned by `TestPaintedRowEdgesAreSealed` (no row edge in a `View` frame is an unsealed painted
blank, and every other cell matches the unsealed parse) and `TestPaintedRowsAreNeverErased` (the
painted-to-the-edge rows rendered through ultraviolet's `TerminalRenderer` emit no erase-in-line
and no erase-in-display beyond the initial screen clear, on a first paint and on a repaint), with
`TestBottomChromePaintsOneBackground` and `TestViewKeepsTheTerminalBackground`, in
`internal/tui/background_test.go`.
