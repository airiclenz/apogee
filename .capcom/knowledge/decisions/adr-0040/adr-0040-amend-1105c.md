---
title: "ADR 0040 amendment of 2026-11-05 (c): the transcript keeps the terminal's own background"
---

`adr-0040-amend-1105b`'s first bullet — the terminal's default background set to the scheme's
`surface` while apogee runs (`tea.View.BackgroundColor`, OSC 11) — is withdrawn by the owner on
2026-11-05: it turned the whole screen, transcript included, the scheme's `surface` (black under
`dark`), which was not wanted. The chat area keeps the terminal's own default background.

From here:

- **apogee never sets the terminal's default background.** `Model.View` leaves
  `tea.View.BackgroundColor` unset under every scheme and colour profile. Decision 9's first
  sentence — "No full-screen background painting: the terminal's background stays the
  terminal's" — stands again as written.
- **The bottom chrome still paints itself, from the `▔` rule down.** `adr-0040-amend-1105b`'s third
  bullet stands for the hairlines, the status line (the gauge's filled cells included), the input
  box and both footer rows: each paints `surface` explicitly to the full width, so the bottom of the
  screen is one band (`.capcom/issues/prompt-box-uneven-background.md`). The gauge's empty track
  keeps its `chrome` groove. The gap row above the `▔` rule is withdrawn from it: it stays on the
  terminal's own background with the transcript, owner's call of 2026-11-05.
- `adr-0040-amend-1105b`'s second bullet (no OSC 11 on a colourless terminal) falls away with the
  first: nothing is sent on any profile.

Pinned by `TestViewKeepsTheTerminalBackground` and `TestBottomChromePaintsOneBackground` in
`internal/tui/background_test.go`.
