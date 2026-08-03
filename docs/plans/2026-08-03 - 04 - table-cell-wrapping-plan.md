# Markdown table cells wrap instead of truncating

**Goal.** A markdown table cell whose content does not fit its column wraps onto further lines
inside that cell instead of being cut with a `…`. Columns are separated by a vertical rule so a
multi-line row still reads as columns, and a table too narrow to be readable falls back to plain
paragraphs as it does today. Closes `ISSUES.md` line 5.

**Date:** 2026-08-03
**Status:** planned — not started

## Authoritative sources

Where an item's text disagrees with one of these, the source wins:

- `layout.md` § **"Markdown tables in assistant text"** (currently lines 443–518) — the visual
  contract this plan rewrites. It is the spec; the code mirrors it, never the other way round.
- `docs/adr/0030-the-tui-has-one-width-authority-and-it-mirrors-the-painter.md` — every width in
  `internal/tui` is `th.measure`; `lipgloss.Width`, `ansi.StringWidth` and `runewidth.RuneWidth`
  do not belong in this package. §7 (the cap is enforced, not assumed) is why cell wrapping goes
  through `wrapText` rather than calling `ansi.Wrap` directly.
- `internal/tui/mdtable.go` and `internal/tui/mdtable_test.go` at commit `9b746f2` — the parser
  half of the file (detection, `splitTableRow`, `fitRow`, alignment) is **not** in scope and is
  not to be touched; only the layout half below the divider comment changes.
- `docs/adr/0011-tui-is-a-thin-renderer-over-a-worker-goroutine-engine.md` — the `Model` is copied
  by value on every `Update`; nothing added here may hold a `strings.Builder` (or any no-copy
  type) anywhere the `Model` reaches. Local builders inside a render function are fine.

## Design decisions (owner, 2026-08-03)

These are settled. Do not re-open them; implement them.

1. **Frame: verticals only, no box.** Columns are separated by a vertical rule; there is no outer
   frame, no corners, and no separator line between body rows. The single header rule stays, and
   crosses the verticals. Chosen over a full boxed grid because it saves both horizontal space (no
   outer border, no edge padding) and vertical space (no rule per row) while still making column
   boundaries readable when a cell wraps.
2. **Column fitting is unchanged:** when the natural widths overflow, space comes off the widest
   column, the leftmost where two are equally wide (`fitColumns`, already implemented and tested).
   Only the endpoint changes — the cell wraps rather than being cut.
3. **Row height is unbounded.** A cell wraps to as many lines as it needs; nothing is ever dropped.
   A cap would simply reintroduce truncation at a different threshold.
4. **A readable floor, then paragraphs.** Each column gets at least `minTableColumnWidth` cells of
   content; where the width cannot pay for that, the block is not drawn as a table at all and
   falls back to plain paragraphs, exactly as an unfittable table does today.

## Standing requirements

- Invoke with `with skills: coding-standards` — Go base + `coding-standards.go.md`, and
  `testing.md` + `testing.go.md` for the test items.
- `make check` passes before every commit (repo convention, `AGENTS.md`).
- Commit directly to `main`; no branches, no PRs (pre-production policy, `AGENTS.md`).
- No AI attribution trailers in commit messages.
- **No version bump.** `VERSION` and the `CHANGELOG` release headings are untouched by every item
  here; see the closing note.
- Comment style: this package narrates *why* in full sentences above each function. Match the
  surrounding density — the existing `mdtable.go` comments are the model.

## Out of scope

- The table **parser** (detection, `matchTableBlock`, `splitTableRow`, `parseDelimiterRow`,
  `fitRow`, `hasUnescapedPipe`). Untouched.
- Pop-up panes, the `/` menu and every other column-aligned surface. Their Column contract
  (`layout.md`, "One overlay for 'which one?'") is a separate rule set; item 1 touches exactly one
  stale cross-reference in it and nothing else.
- Tool-output rendering, presented documents, and any markdown surface other than assistant text —
  `renderMarkdownBody` has one production caller (`internal/tui/render.go:256`).
- Horizontal separator lines between body rows, an outer frame, corners: explicitly rejected by
  decision 1.
- Cell-level colour or per-column styling beyond what `renderInline` already produces.
- A new ADR. `layout.md` is the ratified home of this contract — it is where "It is borderless"
  and "One row is one line" are stated — and rewriting those paragraphs *is* the record. ADR 0030
  is cited, not amended.

---

## 1. Rule the columns: a vertical divider and a crossing header rule — ✅ DONE (2026-08-03)

NOTES (2026-08-03): checkpoint — the code half is done (the two glyphs, `tableDivider` /
`tableDividerWidth = 3`, the styled divider in `layoutTableRow`, the `─┼─` joint in `tableRuleRow`,
the budget arithmetic, the layout-half header comment, and every affected test re-baselined plus the
new `TestTableDividerHoldsOneColumn`). Remaining: the two `layout.md` rewrites — the
"It is borderless." / "The header and its rule." / "Before and after." paragraphs, and the pop-up
Column contract's stale "the same minimum gap a markdown table keeps" comparison (~line 810).
NOTES (2026-08-03): deviation — `internal/tui/popup.go`'s `popupGutter` comment named the deleted
`tableGutter` constant, so it was restated on the pop-up's own terms in the same move as the
layout.md cross-reference the item calls out; that is one file beyond the item's list.
NOTES (2026-08-03): deviation — `TestTranscriptRendersMarkdownTable`
(`internal/tui/transcript_test.go`) is also baselined on the rendered table and was updated; the
item's test list does not name it. `TestTableRowsShareOneWidth`, `TestTableCentreOddRemainder`,
`TestTableTruncatesToWidth` and `TestTableFitColumns` needed no edit — their assertions are
width-relative and hold unchanged against the 3-cell divider.
NOTES (2026-08-03): checkpoint cleared — the `layout.md` half is done: the paragraph is rewritten
and its bold lead-in renamed "It is ruled, not boxed." (the word *borderless* had to go with the
contract, and item 4's acceptance grep forbids it in `layout.md`), "The header and its rule." now
states the `─┼─` crossings, the "Before and after." sketch is copied from
`TestTableRendersLayoutExample`'s asserted output and the block is now 33 columns wide, and the
pop-up Column contract states its two-space gutter on its own terms.
NOTES (2026-08-03): out of scope, left to item 2 — "The width cap is absolute." still says "the
natural column widths plus their gutters"; that paragraph is item 2's to amend.

**What.** Replace the two-space gutter with a vertical rule, keeping one line per row and the
existing truncation for now — this item lands the frame and its arithmetic, item 2 lands the
wrapping on top of it.

- `internal/tui/theme.go` — add two glyphs to the marker-glyph block beside `glyphTableRule`:
  `glyphTableColumn = "│"` (U+2502 LIGHT VERTICAL) and `glyphTableCross = "┼"` (U+253C LIGHT
  VERTICAL AND HORIZONTAL). Comment each as belonging to `mdtable.go`, and state — as the
  scroll-bar glyph block already does for `glyphScrollTrack` — that the shape is deliberately
  **not** shared with `glyphSubRail` or `glyphScrollTrack`: those are different elements that move
  independently.
- `internal/tui/mdtable.go` — replace `tableGutter = "  "` with a divider of three cells,
  `" " + glyphTableColumn + " "`, and a `tableDividerWidth = 3` constant used by the arithmetic
  (the glyph is one cell in both width methods, which the test below pins). Keep the constant and
  its comment where `tableGutter` sat.
- `layoutTableRow` writes `th.mdRule.Render(tableDivider)` between columns — the frame is faint,
  not content, the same reasoning `theme.go`'s `mdRule` comment already gives for the rule.
- `tableRuleRow` emits `─`×w per column joined by `─┼─`, so the rule is still exactly as wide as
  every other line of the block and each crossing sits under the divider above and below it.
- `renderTable`'s budget becomes `width - tableDividerWidth*(len(widths)-1)`.
- Update `mdtable.go`'s layout-half header comment: "drawn borderless — columns of text and two
  spaces of gutter … no verticals anywhere" is now false. State the verticals contract.
- `layout.md` — rewrite the **"It is borderless."** paragraph as the new contract: no outer frame,
  no corners, no rule between body rows, a `│` between columns with one space either side; the
  table still sits in the body column rather than reading as a boxed object; the last column is
  still padded, so every line still ends in the same column (that clause is load-bearing and must
  survive verbatim in substance). Amend **"The header and its rule."** for the `┼` crossings.
  Regenerate the **"Before and after."** sketch — take it from the test's actual output, not by
  hand; the test is the authority for what the sketch shows, including the stated total width.
- `layout.md` — one stale cross-reference elsewhere: the pop-up **Column contract** (currently
  line ~810) says adjacent columns are separated by a two-space gutter, "the same minimum gap a
  markdown table keeps". A markdown table no longer keeps that gap. Restate the pop-up's gutter on
  its own terms and drop the comparison. Nothing else about pop-ups changes.

**Tests.** In `internal/tui/mdtable_test.go` unless stated:

- Update for the 3-cell divider: `TestTableRendersLayoutExample`, `TestTableRowsShareOneWidth`,
  `TestTableConsumesItsSyntax`, `TestTableRuleIsContinuous`, `TestTableAlignsColumns`,
  `TestTableCentreOddRemainder`, `TestTableInlineMarkupInCells`,
  `TestTableShrinksToTheWidthItIsGiven`, `TestTableFitColumns`, `TestTableTruncatesToWidth`,
  `TestTableUnfittableFallsBack`. Expectations move; the contracts they assert do not.
- `TestTableRuleIsContinuous` gains the crossing: the rule row is the table's full width and
  carries a `┼` at exactly the columns the body rows carry a `│`.
- New `TestTableDividerHoldsOneColumn` in `internal/tui/paint_test.go`, modelled on
  `TestPaintedScrollbarHoldsOneColumn`: `│` and `┼` each measure one cell under **both**
  `ansi.WcWidth` and `ansi.GraphemeWidth`, and every painted line of a rendered table — header,
  rule and body rows — is the same painted width.

**Acceptance.**

- `go test ./internal/tui/ -run 'Table' -count=1`
- `go test ./internal/tui/ -count=1`
- `make check`
- The **"Before and after."** block in `layout.md` is byte-identical to what
  `TestTableRendersLayoutExample` asserts `renderTable` produces at 34 columns (ANSI stripped).
- `grep -n "two spaces of gutter\|no verticals" internal/tui/mdtable.go` returns nothing.

**Commit.** `feat(tui): rule markdown table columns with a vertical divider`

---

## 2. Cells wrap instead of truncating — ✅ DONE (2026-08-03)

Depends on item 1.

NOTES (2026-08-03): deviation — three existing tests are baselined on "a row is one physical line"
and had to be re-baselined for the wrap; the item's test list names none of them.
`TestTableRowsShareOneWidth` and `TestTableShrinksToTheWidthItIsGiven` (`mdtable_test.go`) and
`TestTranscriptTableFillsTheBodyColumn` (`internal/tui/transcript_test.go`) asserted an exact line
count that a wrapped row now exceeds; each keeps the contract it was written for (one straight right
edge, the width cap, the full body column) and only its line count moved.
NOTES (2026-08-03): per the run's DECISION, this item also fixed the stale "plus their gutters"
clause item 1 left in `layout.md`'s "The width cap is absolute." paragraph — it now reads "plus the
dividers between them".

**What.** A row becomes as many physical lines as its tallest cell needs.

- `internal/tui/mdtable.go` — new `wrapTableCell(th theme, cell string, width int) []string`. Fast
  path: when `th.measure.Width(cell) <= width` return the cell as a single line without wrapping —
  the markdown walk re-runs over the whole transcript on every streamed token (`model.go`), so the
  common case must stay allocation-cheap. Otherwise delegate to `wrapText` (`render.go:1112`),
  which is SGR-aware across a break and enforces the painted cap by hard-breaking anything the
  wrapper still returned over the limit (ADR 0030 §7). **Do not** call `ansi.Wrap` or
  `th.measure.Wrap` directly here; the cap enforcement is the reason `wrapText` exists.
- Turn `layoutTableRow` into `layoutTableRows(th theme, cells []string, widths []int, align []mdAlign) []string`:
  wrap every cell to its column, take the row height as the tallest result, and emit one string
  per line — each column contributing its own line at that index, or, past its own height, a run
  of `width` spaces. Cells are **top-aligned**: a short cell's blank lines go below its content.
  Join with the same styled divider item 1 introduced.
- Every line of a row is padded to the full table width, continuation and filler lines included.
  That straight right edge is the contract `layoutTableRow`'s existing comment already explains
  (the scroll-bar gutter and the mouse's selectable span both depend on it) — carry that comment
  across and extend it to continuation lines.
- `padTableCell` keeps its alignment and padding and loses its truncation branch: after wrapping,
  the only line that can still exceed its column is a single grapheme wider than the whole column,
  which `wrapText` gives a line to itself and which `layout.md` already exempts from the cap.
- `renderTable` appends each row's lines instead of one line per row; header cells wrap by the
  same path and stay bold across a break (`wrapText` re-emits the SGR run).
- Update `mdtable.go`'s layout-half header comment: "One row is one physical line … an over-wide
  cell is cut with a … rather than wrapped" is now false. State the wrap contract and why the
  line-oriented renderer above is untroubled by it (a row simply contributes more lines).
- `layout.md` — rewrite **"One row is one line."** as the wrap contract: a row is as many lines as
  its tallest cell; cells are top-aligned; height is unbounded and nothing is dropped; every line
  of the block is still exactly the table's width. Amend **"The width cap is absolute."**: the
  widest column still gives up space first, leftmost on a tie, deterministically per repaint — but
  a cell too wide for its column now **wraps** instead of being cut with a `…`. The `…` disappears
  from the table contract entirely; the single indivisible grapheme remains the one exemption.

**Tests.** New in `internal/tui/mdtable_test.go`:

- `TestTableWrapsInsteadOfTruncating` — a cell longer than its column produces further lines and
  no `…`; the full text is present across them.
- `TestTableRowHeightIsItsTallestCell` — a ragged row: the tallest cell sets the height, shorter
  cells are blank-filled below their content (top-aligned), and the filler lines are full-width.
- `TestTableWrappedLinesKeepAlignment` — right- and centre-aligned columns align every one of
  their wrapped lines, not just the first.
- `TestTableEveryLineIsTheTableWidth` — header, rule, body and continuation lines all measure the
  same width under `th.measure`.
- `TestTableWrapKeepsInlineStyle` — a `**bold**` or `` `code` `` span broken across a wrap
  boundary re-emits its SGR on the second line and resets at its end.
- `TestTableWrapIsUnbounded` — a cell of several hundred characters in a narrow column yields the
  full line count its content needs, with nothing dropped.
- Rename `TestTableTruncatesToWidth` → `TestTableWrapsToWidth` and rewrite it for the new
  endpoint; its shrink-the-widest-column assertions stay.
- `BenchmarkRenderTable` — a table whose cells all fit, at a width that needs no shrinking, to pin
  the fast path's cost on the per-token repaint path.

**Acceptance.**

- `go test ./internal/tui/ -run 'TestTableWrap|TestTableRowHeight|TestTableEveryLine' -v -count=1`
- `go test ./internal/tui/ -count=1`
- `go test ./internal/tui/ -run XXX -bench BenchmarkRenderTable -benchmem`
- `make check`
- `grep -n "Truncate" internal/tui/mdtable.go` returns nothing — no cell is cut any more, so the
  truncation call is gone from the file rather than merely unreachable.

**Commit.** `feat(tui): wrap markdown table cells instead of truncating them`

---

## 3. A readable floor, and the paragraph fallback below it — ✅ DONE (2026-08-03)

Depends on item 2.

NOTES (2026-08-03): deviation — `TestTableShrinksToTheWidthItIsGiven` (`mdtable_test.go`) is
baselined on "nine cells is the narrowest these three columns can be drawn in", which the floor
moves to eighteen; its widths are now {18, 20, 24} and its comment names `minTableColumnWidth`. The
item's test list does not name it; the contract it asserts — still a table below its natural width,
every line inside the cap — is unchanged.
NOTES (2026-08-03): the width sweep is the sibling the item allows rather than an extension of
`TestWidthNeverExceeds`: `TestTableWidthNeverExceedsAcrossWidths` (`markdown_test.go`). Per the
run's DECISION it sweeps widths 1…120 under BOTH `ansi.WcWidth` and `ansi.GraphemeWidth`, and its
fixture carries the two-cell VS16 grapheme that crossed the cap before the floor (reproduced at
widths 9 and 10 here, 5/9/10 in item 2's verifier fixture). The only line it still exempts is
`layout.md`'s single indivisible grapheme.
NOTES (2026-08-03): deviation — `renderTable`'s doc comment said it reports false when "the width is
too narrow even with every column down to a single cell", which the floor falsifies; it now names
`minTableColumnWidth`. Its behaviour is otherwise unchanged, as the item says.

**What.** Stop a squeezed column from shredding words one letter per line.

- `internal/tui/mdtable.go` — add `minTableColumnWidth = 4`, commented with its reason: below four
  cells a wrapped column reads as vertical text, and plain paragraphs are more readable than a
  table that narrow.
- Give `fitColumns` a floor parameter. Two rules, and the second is the subtle one:
  - the shrink loop never takes a column below the floor;
  - the width a table **requires** is `sum(min(natural_i, floor))`, not `len(widths) * floor`. A
    column whose natural width is already below the floor is never widened and must not be charged
    the floor — otherwise a table of naturally narrow columns that fits perfectly well would be
    rejected. `fitColumns` returns false when that required width exceeds the budget, which
    subsumes today's `len(widths) > budget` guard; remove the old guard rather than leaving two.
- `renderTable` is otherwise unchanged: a false from `fitColumns` still means the block falls
  through to the paragraph path in `markdown.go`, which is always readable and never overflows.
- `layout.md` — amend the fallback sentence in **"The width cap is absolute."**: the table is not
  drawn as a table when the width cannot give every column its readable minimum (state the number
  and that a naturally narrower column is not charged it), rather than only when a single cell per
  column overflows.

**Tests.**

- `TestTableNarrowerThanTheFloorFallsBack` — at a width that cannot pay the floor, `renderTable`
  reports false and `renderMarkdownBody` renders the block as paragraphs (source text visible,
  no divider glyph in the output).
- `TestTableOfNarrowColumnsIsNotRejected` — three columns of natural width 2 render as a table at
  a width that `len(widths) * floor` would have rejected.
- Extend `TestTableFitColumns` with the floor: a column at the floor is not shrunk further, and
  the required-width rule is asserted directly.
- Update `TestTableUnfittableFallsBack` for the new threshold.
- `internal/tui/markdown_test.go` — extend `TestWidthNeverExceeds` (or add a sibling) to sweep a
  wrapping, multi-column table across widths 1…120 and assert no rendered line ever exceeds the
  width in `th.measure`, under both width methods.

**Acceptance.**

- `go test ./internal/tui/ -run 'TestTableNarrower|TestTableOfNarrow|TestTableFitColumns|TestTableUnfittable|TestWidthNeverExceeds' -v -count=1`
- `go test ./internal/tui/ -count=1`
- `make check`

**Commit.** `fix(tui): give table columns a readable floor before falling back to paragraphs`

---

## 4. Narrate the new table, and mark the issue executed — ✅ DONE (2026-08-03)

Depends on items 1–3. Documentation only — no behaviour changes here.

**What.**

- `internal/tui/doc.go` (currently line ~155) — the package narration calls tables "borderless
  aligned columns". Update it to the ruled, wrapping, floored contract, keeping the existing
  sentence about measuring the RENDERED cell.
- `internal/tui/markdown.go` (line ~14) — the same stale word in the file header ("draws them as
  borderless aligned columns under the same absolute width cap"). Update, keeping the width-cap
  clause.
- `CHANGELOG.md` — one entry under `## [Unreleased]` → `### Changed`, in the house voice: what a
  reader saw before (a long cell cut with a `…`), what they see now (the cell wraps inside its
  column; columns are separated by a vertical rule crossed by the header rule; a table too narrow
  for a readable column still falls back to paragraphs). The shipped v1.1.0 entry describing the
  borderless table is **history and is not rewritten** — the repo does not rewrite shipped
  changelog entries.
- `ISSUES.md` — mark line 5 `- [X]` (the file's own key: X = Executed). Leave the text in place;
  dropping executed issues is a separate owner chore, as in `8e4394a`.

**Tests.** None — documentation only. The verification is the acceptance greps plus `make check`.

**Acceptance.**

- `make check`
- `grep -rn "borderless" layout.md internal/tui/` returns no hits (only `CHANGELOG.md`'s shipped
  history may still carry the word).
- `grep -n "^- \[X\] when printing markdown tables" ISSUES.md` returns line 5.
- `CHANGELOG.md`'s `## [Unreleased]` section carries the entry; no release heading and no
  `VERSION` line changed anywhere in the diff.

**Commit.** `docs(tui): record the wrapped, column-ruled markdown table`

---

## Suggested version bump

Not performed by any item above, and the decision is the owner's. When this wave is released, the
natural level is a **patch** bump from `v0.10.13` to `v0.10.14`: user-visible rendering changes,
no Go API change, no Event or hook-point addition. If the vertical rule is judged a large enough
shift in the transcript's look to be worth announcing, a minor bump to `v0.11.0` is equally
defensible — but nothing here forces either, and no item touches `VERSION` or a `CHANGELOG`
release heading.
