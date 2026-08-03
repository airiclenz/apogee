---
Status: accepted
---

# The TUI has one width authority, and it mirrors the painter

## Context

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
capability. [ADR 0011](0011-tui-is-a-thin-renderer-over-a-worker-goroutine-engine.md) binds only
in that the `Model` is copied by value on every `Update`, so whatever holds the answer must be a
copy-safe value.

## Decision

**One display-width authority for the TUI, and it is whatever measure the painter is actually
using.** Owner's call, 2026-07-31: measurement must always match what gets painted.

1. **The authority is a plain value wrapping one `ansi.Method`** — `widthAuthority` in
   `internal/tui/width.go`. It starts at `ansi.WcWidth`, which is where the painter starts, and
   adopts `ansi.GraphemeWidth` at the same `tea.ModeReportMsg` bubbletea uses to move its own
   renderer. bubbletea handles that message *without* `continue`, so it falls through to
   `Model.Update` (`tea.go:786-798`, `:871`) after the renderer's method was set and before the
   next render — there is no frame of mismatch, and no `tea.With…` option or renderer read-back
   is needed. The switch is one-way, as the renderer's is.
2. **It rides on `theme`** (`theme.measure`). `theme` is already threaded as the first parameter
   through every free renderer function, so the authority reaches ~35 measurement sites without
   a signature change at each one, and it is one enum field — no pointer, no `sync` type — so
   ADR 0011's copy rule needs no new exception.
3. **It carries the operations, not just the measure**: `Width`, `Truncate`, `Cut`, `Wrap`,
   `Wordwrap`, `Hardwrap`, with signatures identical to `ansi.Method`'s. A width measured in one
   method and cut in another is the same defect one step later, so an operation that *consumes*
   a converted measurement was converted with it. Identical signatures also keep each conversion
   a rename rather than a rewrite, so nothing drifts away from the library at the seam.
4. **`Cut` composes its two truncations itself** rather than delegating. `x/ansi@v0.11.7`'s `cut`
   binds the LEFT truncation of the wcwidth branch to `TruncateWc` — a *right*-truncation
   (`truncate.go:35-39`; the grapheme branch correctly uses `TruncateLeft`) — so
   `ansi.Method.Cut(s, left, right)` under wcwidth spends `left` as a *width* rather than as an
   offset and returns the first `left` columns: `Cut("abcdef", 2, 5)` hands back `"ab"` where the
   span is `"cde"`. Wcwidth is the painter's default and a selection is exactly a cut with a
   non-zero left, so every terminal that does not answer mode 2027 would have copied from the
   wrong end of the line. `TestWidthAuthorityCutsFromTheLeft` pins the correct behaviour; the
   delegation can return after a dependency bump fixes it upstream.
5. **The frame is squared in the authority's measure**, not by lipgloss's joins. `squareLine`
   pads or ANSI-aware cuts a line to exactly N columns; `joinScrollbar` and `joinFrame`
   (`internal/tui/model.go`) replace the `JoinHorizontal`/`JoinVertical` that padded in grapheme
   width regardless of the authority. Both were load-bearing: the horizontal join is what put
   the bar a column left, and the vertical one — left-aligning by padding to the widest row *it*
   measured — pushed a 24-row frame to 105 columns on an 80-column window once the rows were
   squared. This is what makes `layout.md`'s absolute cap hold at the painted layer.
6. **Widget mirrors are the deliberate exception: their oracle is the widget, never the
   painter.** `wrapRowStarts`/`runesWidth` (`inputaccent.go`), `cellToRuneOffset` (`mouse.go`) and
   `inputContentRows` (`render.go`) mirror third-party widgets' internal math — the textarea wraps
   with `uniseg.StringWidth`, the viewport soft-wraps with `ansi.StringWidth`, and neither moves
   when the painter does. *(`wrappedOffset` (`render.go`), the viewport mirror this list also
   named, was deleted 2026-08-03 with the submit-time jump-to-top that was its sole consumer; the
   rule is unchanged, and every live mirror is now the textarea's.)* They measure the way their
   widget measures, down to the one term where the textarea itself weighs a rune with
   go-runewidth (`textarea.go:1838-1839`, `lastCharLen`). The dividing line, stated at each
   site: a mirror's **rows** are the widget's — only it decides which runes it put on which line
   — while the **columns** within a row address cells the painter has already drawn, so those
   are the authority's.
7. **The cap is enforced, not assumed.** `wrapText` hard-breaks any line the wrapper still
   returned over the limit. Upstream's breakpoint branch (`x/ansi@v0.11.7/wrap.go:406-419`, and
   the grapheme path at `:352-361`) lacks the already-full-line and overflow checks its own
   `default:` branch has, so a hyphen or pipe run grows a word onto a full line at any limit —
   `ansi.Wrap("| --- | --- | --- |", 8, "")` returns an eleven-cell first line. The one thing no
   break can divide is a single grapheme wider than the limit; that gets a line to itself and
   nothing else is allowed over.
8. **Painted cells are the standard of proof.** `internal/tui/paint_test.go` renders a `Model`
   the way bubbletea does and reads the cell grid back (`paintFrame`, `paintedWidth`,
   `paintedColumn`, `paintedAs`), so the width claims are asserted in the cells a terminal
   really shows — and asserted under **both** methods. A frame is always painted in the measure
   the model's own authority is on; painting a wcwidth-composed frame with a grapheme painter is
   the mismatch this ADR exists to prevent, not a case the code must survive.

## Considered options

- **Normalize the content instead** — keep grapheme width as the single measure and strip or
  fold VS16 out of everything the TUI renders. Rejected: it is a narrowing, not a fix — ZWJ
  sequences and regional indicators can still diverge — and it changes what the user sees
  (`⚠️` would paint as `⚠`). It also treats the terminal's measure as apogee's to overrule.
- **Always wcwidth** — simplest, and correct on every legacy terminal. Rejected: wrong by one
  cell per VS16 grapheme on any terminal that *does* answer mode 2027, which is the growing
  majority, and it would have made the drift worse over time rather than better.
- **Always grapheme width and force the painter** — rejected as not available: apogee cannot
  make a terminal support mode 2027, and bubbletea does not even ask on Apple Terminal or over
  SSH with a known `TERM_PROGRAM`. A program that assumes the capability is wrong exactly where
  it cannot detect that it is.
- **A package-level function or a package var holding the method** — rejected under ADR 0011
  and under the same reasoning that keeps the theme a value: a global would make the measure
  invisible at the call site and untestable in two methods within one test binary. Hanging it on
  `theme` puts the measure where the styles already are.
- **Fix `wrap.go` and `truncate.go` upstream and wait for a release** — rejected as a
  dependency on someone else's schedule for a contract `layout.md` states as absolute. Both
  workarounds are commented with the upstream defect and revert to delegation after a bump.
- **Convert the remaining popup sites now** — rejected on collision grounds only: plan
  `2026-07-31 - 01` owns those files while it is live. See Consequences.

## Consequences

- **The rule for `internal/tui`, from here on: measure with `th.measure`.** `lipgloss.Width`,
  `ansi.StringWidth` and `runewidth.RuneWidth` do not belong in this package except inside a
  widget mirror that names its oracle in a comment. `internal/tui/doc.go` narrates `width.go` as
  the one measure; `layout.md` says which width its rules are stated in.
- **`layout.md`'s "rendered width" and "absolute width cap" now name the painted measure.** The
  cap is asserted where it is claimed — in painted cells — rather than in the measure that was
  producing the overflow.
- **`go.mod` gained two direct requirements**, both already in the build:
  `github.com/charmbracelet/ultraviolet` (the paint harness imports it) and `github.com/rivo/uniseg`
  (the caret mirrors call the textarea's own measure). No version changed.
- **A dependency bump is a re-verification event, and it fails loudly.** `ansi.StringWidth("⚠️") == 2`
  with the wcwidth measure of the same string `== 1` is pinned in `paint_test.go`; the `Cut`
  workaround and the `wrapText` cap enforcement each have their own pin naming the upstream
  defect they stand in for. If any of those change, the tests say so before the layout does.
- **Four measurement sites are deliberately still unconverted**, all of them safe for the cap
  and none of them silent: `popup.go:185, 212, 293` and `interject.go:393` belong to plan
  `2026-07-31 - 01` while it is live, and `wrapText`'s own `ansi.Wrap` plus `truncateToWidth`
  wait on the same file. A wrap or clip computed in grapheme width never paints *wider* than its
  limit under wcwidth, so the absolute cap holds regardless; what remains possible is a popup
  column being a cell off on a row carrying VS16. Tracked in `TODO.md`.
  *(The `popup.go` and `interject.go` sites were converted on 2026-08-03 once plan
  `2026-07-31 - 01` was archived: `truncateToWidth` now cuts as well as measures with the
  authority, and the drift each left possible is pinned under both methods in `paint_test.go`.
  `wrapText`'s own `ansi.Wrap` is the one site still standing, and stays tracked in `TODO.md`.
  The rule above is unchanged.)*
- **`CHANGELOG.md`'s v1.1.0 `caretTo` entry claimed the fix used "the same width the widget's own
  cursor math uses", which was not true.** Shipped history is not rewritten; the entry carries a
  dated correction pointing at the real fix.
