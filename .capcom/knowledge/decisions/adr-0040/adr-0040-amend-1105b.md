---
title: "ADR 0040 amendment of 2026-11-05 (b): the terminal background is the scheme's surface"
---

Decision 9's first sentence — "No full-screen background painting: the terminal's background stays
the terminal's" — is superseded by the owner's ratified Background call of 2026-11-05 (plan
`docs/plans/2026-11-05 - 04 - bottom-chrome-background-and-watch-footer-plan.md`, item 1). It left
the bottom chrome banded: the status line, input box and footer painted `surface` explicitly while
the gap row above them and every cell a row's erase-to-end-of-line left blank showed the terminal's
own default, so the bottom of the screen read as stripes rather than one band
(`.capcom/issues/prompt-box-uneven-background.md`).

From here:

- **The terminal's default background is the scheme's `surface` while apogee runs.** Every frame
  sets it (`tea.View.BackgroundColor`, an OSC 11 set the renderer resets when apogee exits), read off
  the live theme, so a scheme switch moves it with every other colour. The whole screen takes it,
  transcript included.
- **A colourless terminal is sent none.** Under an Ascii or non-TTY colour profile (`NO_COLOR`
  included) the background stays unset — the same colour test the prompt block's half rows take
  (`padsPrompts`, `internal/tui/theme.go`), carried across a scheme switch beside it.
- **The bottom chrome still paints itself.** The gap row above the `▔` hairline and the gauge's
  filled cells are painted `surface` explicitly, joining the hairlines, status line, input box and
  footer rows that already were, so a terminal that ignores OSC 11 still shows one band at the
  bottom. The gauge's empty track keeps its `chrome` groove.

Pinned by `TestViewSetsSurfaceAsTerminalBackground` and `TestBottomChromePaintsOneBackground` in
`internal/tui/background_test.go`.

The rest of decision 9 stands: no glyph, marker, width or row budget is themed, and the cross-scheme
distinctness tests are unchanged. Setting the background is not detecting it — the rejection of
OSC 11 *auto-detection* (`adr-0040-rejected`) is untouched, and `ui.color-scheme` stays the user's
explicit choice. What changes is what `light` asks of the terminal: its `surface` now becomes the
screen's background, so it no longer depends on the terminal already being light.
