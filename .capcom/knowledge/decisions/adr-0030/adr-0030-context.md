---
title: ADR 0030 context
---

Every rule in `layout.md` is stated in *width*: word wrap "breaks short of the right edge",
a table's columns are set by "that **rendered** width", and "**the width cap is absolute** —
no rendered line ever exceeds the width the block was given". None of those sentences said
*which* measure of width, because until this decision the codebase did not have one answer —
it had two, and they disagree.

**The layout side measures grapheme clusters.** `lipgloss.Width` is a per-line `max` over
`ansi.StringWidth` (`lipgloss/v2@v2.0.4/size.go:15-25`), and `ansi.StringWidth` is
`stringWidth(GraphemeWidth, s)` (`x/ansi@v0.11.7/width.go:65-66`), which measures whole
clusters through `clipperhouse/displaywidth` — and displaywidth promotes any cluster followed
by VARIATION SELECTOR-16 (U+FE0F) to two cells (`displaywidth@v0.11.0/width.go:173-181`).
Every helper the TUI reached for sat on that side: `ansi.Cut`, `ansi.Truncate`, `ansi.Wrap`,
`ansi.Wordwrap`, `ansi.Hardwrap`, and the padding `lipgloss.JoinHorizontal` does
(`lipgloss/v2@v2.0.4/join.go:88`).

**The paint side starts at wcwidth.** The composed view goes to bubbletea as one string
(`internal/tui/model.go`, `tea.NewView`), which wraps it with `uv.NewStyledString` and draws it
into a screen buffer (`bubbletea/v2@v2.0.7/cursed_renderer.go:268, 311`). That buffer is created
with `Method: ansi.WcWidth` (`ultraviolet/buffer.go:612-617`) — `ansi.WcWidth` is `iota`, the
zero value — and wcwidth takes the first non-zero-width rune of the cluster
(`go-runewidth@v0.0.23/runewidth.go:225-235`). U+FE0F is zero-width and U+26A0 is neutral, so
**`⚠️` is two cells to the layout and one cell to the painter**. bubbletea moves the painter to
`ansi.GraphemeWidth` only when a terminal answers its start-up **mode 2027** (Unicode core)
query (`bubbletea/v2@v2.0.7/tea.go:786-798`), and it does not even ask on Apple Terminal or over
SSH with a known `TERM_PROGRAM` (`tea.go:1111-1114`). Nothing in apogee read that answer.

**Three symptoms followed, and they were not cosmetic.**

1. *The scroll bar drifted.* Its column came from `lipgloss.JoinHorizontal`, padding in
   grapheme width; the painter then walked the row in wcwidth and dropped that row's `│`/`█`
   one column left of every other row's. Measured at 80×24 with the painted harness: column 78
   on the `⚠️` row against column 79 everywhere else.
2. *A drag selected the wrong glyph.* The terminal reports a **painted** cell index; the
   selection then cut with `ansi.Cut` and measured with `lipgloss.Width` — both grapheme width.
   Pointer, highlight and clipboard could name three different things: a drag from painted
   column 11 of `❯ danger ⚠️ zebra` copied `" zebr"`, the neighbouring glyph, and the highlight
   measured 16 columns against a 15-column painted span.
3. *The prompt caret landed one glyph off* — an independent third mismatch. `bubbles/v2@v2.1.0`'s
   textarea wraps and positions with `uniseg.StringWidth`, while apogee's two mirrors of that
   math (`wrapRowStarts`, `cellToRuneOffset`) accumulated **per-rune** `runewidth.RuneWidth`.
   Both carried tests claiming to pin the widget, and neither fixture contained a VS16 sequence.

Measurement was scattered to match: 36 production sites across six files and three libraries,
two interchangeable spellings of the same measure (`lipgloss.Width` / `ansi.StringWidth`), and
no rule about which to use. Nothing was tested at the painted layer — every width-pinning test
in the package asserted the *pre-paint* string, in the measure that was under suspicion.

No prior ADR binds: `docs/adr/` had zero hits for width, emoji, grapheme, wcwidth or terminal
capability. ADR 0011 binds only
in that the `Model` is copied by value on every `Update`, so whatever holds the answer must be a
copy-safe value.
