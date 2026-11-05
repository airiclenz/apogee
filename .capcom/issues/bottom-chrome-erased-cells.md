---
title: Painted fields erased with EL show the terminal's background in Terminal.app
status: open
type: bug
priority: 2
labels: [tui]
plan: 2026-11-05 - 05 - bottom-chrome-erase-fill-plan
created: 2026-11-05T17:35:56Z
---

In Terminal.app (`TERM_PROGRAM=Apple_Terminal`, `TERM=xterm-256color`) parts of the bottom chrome still show the terminal's own (translucent) background instead of the scheme's `surface`, after `prompt-box-uneven-background` was closed (screenshots 2026-11-05 16.02.44 and 18.20.20). Three spots: the whole idle status line between the `▔` rule and the input box, footer row 2 past the git segment (`~/repos/apogee ✦ main 0Δ 0↓ 0↑`), and footer row 1's margin after the mode marker (`◐ ask before`).

Root cause: the frame paints those cells `surface` correctly (the cell buffer is right, so `TestBottomChromePaintsOneBackground` passes), but the ultraviolet renderer sends a row's trailing blank run as `EL` (`\x1b[K`, `TerminalRenderer.transformLine` → `clearToEnd`) whenever the run's cell is clearable (`canClearWith`: a plain space with no reverse/conceal/underline). It relies on the terminal filling erased cells with the current background (BCE), and Terminal.app does not draw those cells as an opaque `surface` field. Runs written as characters, and mid-row runs sent as `ECH`, render correctly there. The test never looks at the bytes the renderer emits.

Specs: `layout.md` (colour scheme paragraph), ADR 0040 with `adr-0040-amend-1105c`. Code: `internal/tui/boxdraw.go` (`squareOnField`), `internal/tui/model.go` (`statusLine`, `footerRowContent`, `footerLeft`), `internal/tui/background_test.go`.

## Why

The bottom of the screen still reads as patchy bands on a translucent terminal, which the closed prompt-box-uneven-background fix was meant to end.

## Acceptance

- Rendering every painted-to-the-edge row (bottom chrome, sent prompt blocks, breadcrumb, staged band) through the real ultraviolet TerminalRenderer under TERM=xterm-256color emits no erase-in-line or erase-in-display sequence for it, on a first paint and on a repaint, idle status line and two-row footer with a git segment included
- Every cell of every painted field renders its field colour in Terminal.app (owner's check, no translucent band)

Scope (owner, 2026-11-05): the same defect shows wherever a painted row ends in blanks with no character before the right border, so the fix covers every painted field (sent prompt blocks, popups, breadcrumb, suggest band, bottom chrome), not only the bottom chrome.

## Log

- 2026-11-05 Airic Lenz: Automated half landed via 2026-11-05 - 05 - bottom-chrome-erase-fill-plan (archived at 3b64a3ce): joinFrame seals painted row edges with conceal so ultraviolet writes them instead of sending EL; TestPaintedRowEdgesAreSealed and TestPaintedRowsAreNeverErased pin no EL/ED on first paint and repaint. Left open for the owner's Terminal.app check (acceptance bullet 2).
