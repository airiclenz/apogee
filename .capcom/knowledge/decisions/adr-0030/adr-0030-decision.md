---
title: ADR 0030 decision
---

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
   *(Extended 2026-08-03: `drawBox` (`model.go`) is the boxed counterpart of `squareLine`. A
   **bordered** surface cannot be squared by a `lipgloss.Style.Width` at all — past its width that
   style wraps rather than pads, folding one composed row into two painted ones — so the pop-up pane
   and the start-up card draw their own rows, border glyphs and padding included. See the last
   Consequences bullet.)*
6. **Widget mirrors are the deliberate exception: their oracle is the widget, never the
   painter.** `wrapRowStarts`/`runesWidth`, `cellToRuneOffset` and `inputContentRows`
   (`editorgeometry.go`) mirror third-party widgets' internal math — the textarea
   wraps with `uniseg.StringWidth`, and it does not move when the painter does.
   *(Amended 2026-10-02: the three used to sit in `inputaccent.go` and `mouse.go`; they moved, with
   `sanitizeInputLine` and the line editor's offset arithmetic, into `editorgeometry.go`, the
   string geometry the line editor owns. `cellToRuneOffsetIn` moved beside them as the
   painter-measured sibling, not a mirror — its ruler is the authority. The rule and the list are
   unchanged.)*
   *(Amended 2026-08-20: `inputContentRows` was written in `render.go`,
   moved to `chromelayout.go` with the render split, and now sits beside the other textarea mirrors
   this list names; the rule and the list are unchanged.)* *(`wrappedOffset` (`render.go`), the
   viewport mirror this list also named, was deleted 2026-08-03 with the submit-time jump-to-top
   that was its sole consumer; the rule is unchanged, and every live mirror is now the textarea's.)*
   *(Amended 2026-08-29: the viewport is no longer one of the widgets this rule covers. It used to
   soft-wrap with `ansi.StringWidth` — a second, GraphemeWidth opinion about lines the painter had
   already wrapped in WcWidth, which folded a full line carrying a VARIATION SELECTOR-16 glyph into
   two rows and drifted every `contentLineAt` reader by one. `SoftWrap` is now off (`newModel`,
   `model.go`): the viewport does not wrap, its rows are the painter's lines one for one, and an
   over-wide line would be clipped at the right edge. The `cellToRuneOffset` column rule below is
   unchanged — it addresses cells on a row the widget has drawn, which is still the painter's
   business.)* *(Amended 2026-08-30: with the wrap off, that CLIP became the way the same
   disagreement could still lose content — a full line ending in a VS16 glyph measures a cell over
   the viewport's own width and had that glyph cut off the frame. The painter now reserves the
   widget's extra cells: `reserveWidgetCells` (`render.go`) holds the finished paint to the
   viewport's width in the widget's measure and breaks the rare line that overruns into stored
   lines of its own, so the clip has nothing left to cut. That pass IS a mirror under this rule —
   it asks `ansi.StringWidth` and cuts with `ansi.Cut`, the viewport's own two calls, so the
   painter breaks exactly where the widget would have — and it runs after every block is composed,
   so the wrap each surface chose in the painter's measure is untouched.)*
   They measure the way their widget measures, down to the one term where the textarea itself weighs
   a rune with go-runewidth (`textarea.go:1838-1839`, `lastCharLen`). The dividing line, stated at
   each site: a mirror's **rows** are the widget's — only it decides which runes it put on which
   line — while the **columns** within a row address cells the painter has already drawn, so those
   are the authority's.
7. **The cap is enforced, not assumed.** `wrapText` hard-breaks any line the wrapper still
   returned over the limit. Upstream's breakpoint branch (`x/ansi@v0.11.7/wrap.go:406-419`, and
   the grapheme path at `:352-361`) lacks the already-full-line and overflow checks its own
   `default:` branch has, so a hyphen or pipe run grows a word onto a full line at any limit —
   `ansi.Wrap("| --- | --- | --- |", 8, "")` returns an eleven-cell first line. The one thing no
   break can divide is a single grapheme wider than the limit; that gets a line to itself and
   nothing else is allowed over.
   *(Extended 2026-08-30: the cap is the painter's own measure, and exactly one surface needs a
   second budget beside it — the transcript, whose finished lines are handed to a widget that
   measures them in `ansi.GraphemeWidth` and cuts what it calls over-wide. `reserveWidgetCells`
   (`render.go`) is that reserve, and it is applied to the finished PAINT rather than to the wrap:
   `wrapText` still breaks where the painter's measure says — a table cell, a pop-up body and
   every narrow-limit wrap are unchanged — and only a transcript line that would really have lost
   cells to the widget is re-laid. See rule 6.)*
8. **Painted cells are the standard of proof.** `internal/tui/paint_test.go` renders a `Model`
   the way bubbletea does and reads the cell grid back (`paintFrame`, `paintedWidth`,
   `paintedColumn`, `paintedAs`), so the width claims are asserted in the cells a terminal
   really shows — and asserted under **both** methods. A frame is always painted in the measure
   the model's own authority is on; painting a wcwidth-composed frame with a grapheme painter is
   the mismatch this ADR exists to prevent, not a case the code must survive.
