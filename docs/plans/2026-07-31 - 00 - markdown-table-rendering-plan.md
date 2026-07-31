# Markdown tables in the transcript — implementation plan

**Date:** 2026-07-31. **Status:** ready to execute. Origin: `ISSUES.md` line 9 — "markdown
tables emitted by the model are not properly rendered as a table in the apogee chat."
**Ground truth:** this plan's Decisions section, plus `layout.md` as amended by item 1 (the
look spec of record — after item 1 lands, `layout.md` is the visual authority wherever an
item's prose is ambiguous). The GFM table syntax reference
(https://github.github.com/gfm/#tables-extension-) is the authority for *what counts as a
table*; the Decisions section states where we deliberately deviate. Executed by
`/implement-plan` in a fresh session; forward the `coding-standards` skill at invocation.

**Out of scope:** every other `ISSUES.md` entry; markdown constructs not currently handled
(links, italics, blockquotes, strikethrough, thematic breaks); adopting glamour/goldmark or
any other markdown library; in-cell text wrapping (v1 truncates — see Decision 7); the doc
server's markdown rung (ADR 0019 defers it — different surface); any change to
`render.go`'s marker/rail framing.

## The problem in one paragraph

`internal/tui/markdown.go` is a hand-rolled markdown-subset renderer (fences, headings,
lists, bold, inline code) with no table support: a pipe table falls through to the
`default:` branch of `renderMarkdownBody` (`markdown.go:40-66`) and each row is rendered as
an independent word-wrapped paragraph. Column alignment depends on the model's own source
padding (measured in characters, not display cells, and destroyed by inline styling), the
`|---|:---:|` delimiter row renders literally as visible garbage, and rows wider than the
content width soft-wrap mid-row. Worse, a delimiter row written without a leading pipe
(`--- | ---`) is swallowed by `matchListItem` (`markdown.go:227`) as a bullet item. The fix
is a spare, dependency-free table renderer in the same posture as the rest of
`markdown.go`: pure functions of `(theme, lines, width)`, dispatched from
`renderMarkdownBody`'s walk.

## Decisions this plan implements (ratified by owner review of this plan — no open design calls)

1. **Hand-rolled, no new dependency.** No `charm.land/lipgloss/v2/table`, no glamour. That
   component targets bordered standalone tables and fights three house constraints
   (per-token re-render cost, mid-stream degradation, the hard width cap); the stated
   posture of `markdown.go:14-25` — spare, lipgloss-only, pure, table-testable — stands.
2. **Detection is a two-line lookahead, GFM-shaped.** A table begins where a line
   containing at least one unescaped `|` (the header row) is immediately followed by a
   valid delimiter row: only `|`, `-`, `:`, and spaces, at least one `-`, cell specs
   matching `-+`, `:-+`, `-+:`, or `:-+:`, and the same cell count as the header. The
   table-block matcher runs **before** `matchListItem` in `renderMarkdownBody`'s switch.
   A delimiter-shaped line with no header row above it keeps today's behavior (the
   `--- | ---` line falls to the list matcher — accepted, GFM-compatible). Matching is
   cheap prefix/scan work — **no regex** (the walk runs over the whole transcript on every
   streamed token, `model.go:391`).
3. **Rows, cells, alignment per GFM.** Leading/trailing pipes optional; `\|` is a literal
   pipe inside a cell; cells are trimmed; rows with fewer cells than the header are padded
   with empty cells, excess cells are dropped; alignment comes from the delimiter row
   (`:--` left, `:-:` center, `--:` right, plain left). The table ends at the first blank
   line or first line with no unescaped `|`.
4. **The rendered shape is borderless aligned columns** (specced precisely in `layout.md`
   by item 1): cells inline-rendered (`renderInline` per **cell**, so `**bold**` and
   `` `code` `` work inside cells), columns padded to the widest cell and separated by a
   two-space gutter, no vertical borders. Header cells additionally styled with
   `th.mdBold`; the delimiter row renders as a `─` rule per column (a muted existing theme
   style; add a theme field only if none fits — no colors outside `theme.go`).
5. **All width math is display-cell math.** Column measurement uses `lipgloss.Width` /
   `ansi.StringWidth` on the inline-rendered cell — never `len()` (`markdown.go` bakes
   ANSI into text before wrapping; see also `doc.go`).
6. **The width cap is absolute.** If natural column widths + gutters exceed the available
   width, repeatedly shrink the currently-widest column (floor 1 cell) until the table
   fits, truncating over-wide cells ANSI-aware with `ansi.Truncate` and `…` (the house
   idiom, `render.go:197`). If even all-columns-at-1 cannot fit, the block renders as
   plain paragraphs (today's behavior). No rendered line ever exceeds the given width
   (`TestWidthNeverExceeds` is the enforcing test and gains table cases).
7. **No in-cell wrapping in v1.** Every table row renders as exactly one physical line
   (no embedded newlines — `render.go:14-20`); over-wide cells truncate. Cell wrapping is
   a possible later enhancement, not this plan.
8. **Streaming degrades to plain text.** Until the delimiter row has arrived, the lines
   are ordinary paragraphs (the house partial-markup contract, `markdown.go:23-25`); once
   header + delimiter are present the block renders as a table, and columns may re-widen
   as rows stream in — the reflow (and its interaction with drag-selection's
   keep-if-unchanged rule) is accepted.
9. **Purity and placement.** New code lives in `internal/tui/mdtable.go` + tests in
   `internal/tui/mdtable_test.go`, pure functions only — no state on `Model` or
   `transcript`, nothing that trips ADR 0011's value-copy rule. `renderMarkdownBody`
   receives an already-railed, already-marker-reduced width; the table renderer never
   re-applies indent.

## 1. Amend `layout.md` — the table block spec — ✅ DONE (2026-07-31)

**NOTES (2026-07-31):** `layout.md` has no "assistant block" section to nest under (every part is a
top-level `##`), so the spec landed as its own `## Markdown tables in assistant text` section placed
after the tool-call-sketch rules and before the chrome sections — i.e. in the transcript-body part of
the doc. Two details Decisions 4–6 left open were pinned so item 3 has an unambiguous authority: a
centred cell with an odd remainder takes the extra space on its **right**, and where two columns are
equally wide the **leftmost** shrinks first under the width cap.

**What:** Add a "Markdown tables" subsection to the assistant-block part of `layout.md`
stating the visual contract of Decisions 4–8 in the spec's own prose style: borderless
aligned columns, two-space gutters, bold header, `─` rule row honoring per-column widths,
GFM alignment markers respected, absolute width cap with `…` truncation, one physical line
per row, plain-paragraph fallback for partial (streaming) and unfittable tables. Include
one small before/after example (source table → rendered shape at a stated width).
`layout.md` is the spec of record and is amended FIRST so no implementation item is
written against a sketch that contradicts it.

**Tests:** none (docs-only).

**Acceptance:** `grep -n "Markdown tables" layout.md` finds the new section;
`grep -c "─" layout.md` shows the rule glyph appears in the example; `make check` passes
(unchanged code).

**Commit:** `docs(layout): spec markdown table rendering in the transcript`

## 2. Table detection and parsing — pure parser in `mdtable.go` — ✅ DONE (2026-07-31)

Depends on item 1.

**NOTES (2026-07-31):** the item names the block scanner and the parser separately; they landed as one
entry point, `matchTableBlock(lines, start) (mdTable, int, bool)`, because detection *is* parsing the
header and delimiter — splitting them would re-split both rows on every candidate line of every
streamed token (Decision 2's cost constraint). The pieces are still separate testable functions
underneath (`parseDelimiterRow`, `delimiterCellAlign`, `splitTableRow`, `fitRow`, `hasUnescapedPipe`),
and the parsed table is the `mdTable` value `matchTableBlock` returns (header, align, rows).

**What:** Create `internal/tui/mdtable.go` with unexported pure functions implementing
Decisions 2–3: a delimiter-row validator; a block scanner that, given the line slice and
an index, reports whether a table starts there and how many lines it spans; and a parser
producing header cells, per-column alignment, and body rows as `[][]string` (escaped-pipe
handling, trim, pad-short/drop-excess). No rendering, no theme, no width math in this
item; nothing is wired into `renderMarkdownBody` yet (`make check` runs no unused-symbol
linter, and the tests exercise every function).

**Tests:** New `internal/tui/mdtable_test.go`, same conventions as `markdown_test.go`
(visible-text assertions don't apply yet — this is pure data): leading/trailing-pipe
variants; `\|` in cells; alignment parsing for all four delimiter forms; cell-count
mismatch (pad/drop); delimiter row with wrong cell count → not a table; header with no
delimiter following → not a table; standalone delimiter-shaped line → not a table; table
terminated by blank line and by a pipe-free line; single-column table (`| a |`).

**Acceptance:** `go test -race -count=1 ./internal/tui/ -run 'Table'` passes;
`make check` passes.

**Commit:** `feat(tui): add pure GFM table block detection and parsing`

## 3. Table layout, rendering, and dispatch from `renderMarkdownBody`

Depends on item 2.

**What:** In `mdtable.go`, add the renderer implementing Decisions 4–8: per-cell
`renderInline`, `th.mdBold` header, column measurement via `lipgloss.Width` on rendered
cells, alignment padding, two-space gutters, the `─` rule line, the shrink-widest /
`ansi.Truncate`+`…` overflow loop with its all-at-1 plain-paragraph bailout. Wire it into
`renderMarkdownBody` (`markdown.go:40-66`): the table matcher runs before `matchListItem`,
consumes the block's lines, and appends the rendered rows; everything else in the walk is
untouched. Streaming needs no extra code — a header row without its delimiter simply fails
detection and falls through to the paragraph path.

**Tests:** In `mdtable_test.go` / `markdown_test.go`, following `markdown_test.go`'s
conventions (`strip()`, `colorActive(th)`): a 3-column table renders with aligned columns
and no literal `|` or `---` visible; right/center alignment position cells correctly;
`**bold**` and `` `code` `` inside cells style without breaking alignment (assert
stripped-text column positions); delimiter row renders as `─` runs; over-wide table
truncates with `…` and no line exceeds width; width 3 and width 1 neither panic nor
overflow; header-without-delimiter renders as plain paragraphs (streaming degradation);
table immediately followed by a list/heading/fence renders both blocks correctly. Extend
`TestWidthNeverExceeds` and the `TestPlainTextUnchanged` corpus (plain text and non-table
pipe usage stay byte-identical). Add one assistant-message-with-table case at the
transcript level via `renderPlain` (`transcript_test.go:19`) to cover marker + hanging
indent framing.

**Acceptance:** `go test -race -count=1 ./internal/tui/` passes (all existing invariants
green, including `TestWidthNeverExceeds`, `TestPlainTextUnchanged`,
`TestTranscriptLayoutGolden`, `TestWrappedOffsetMatchesViewport`); `make check` passes.

**Commit:** `feat(tui): render markdown tables as aligned columns in the transcript`

## 4. Docs closeout

Depends on item 3.

**What:** (a) Update the `markdown.go` file-header narration (`markdown.go:14-25`) and the
`doc.go` markdown paragraph (`doc.go:136-139`) to name tables among the handled
constructs. (b) Add a `CHANGELOG.md` `## [Unreleased]` entry for the user-visible change.
(c) Tick the `ISSUES.md` line 9 checkbox (mark `[x]`; do not delete the line). No VERSION
change (see closing note).

**Tests:** none (docs-only).

**Acceptance:** `grep -n "table" internal/tui/markdown.go | head -5` shows the narration
mentions tables; `grep -n -A3 "Unreleased" CHANGELOG.md` shows the entry;
`grep -n "markdown tables" ISSUES.md` shows the ticked box; `make check` passes.

**Commit:** `docs: record markdown table rendering in changelog, issues, and code narration`

## Suggested version bump (not performed by this plan)

Minor (`0.8.0` → `0.9.0`): a user-visible transcript rendering feature. Whether and when
to bump is the owner's call.
