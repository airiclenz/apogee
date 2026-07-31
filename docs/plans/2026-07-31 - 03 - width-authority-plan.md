# One width authority for the TUI — implementation plan

- **Goal:** every display-width measurement in the TUI agrees with the width the terminal
  actually paints, so the scroll bar, the mouse selection, the input-box caret mirror and
  the absolute width cap stay correct on lines containing emoji and other graphemes where
  the two prevailing measures disagree.
- **Date:** 2026-07-31 · **Status:** not started
- **Authoritative sources:**
  - Verified survey of the problem: the **Findings** section below (pinned 2026-07-31
    against the versions in `go.mod` at commit `16bc94e`). It is ground truth for this
    plan: where an item's prose disagrees with Findings, Findings wins, and the
    divergence is recorded as a dated NOTES line under that item.
  - Layout ground truth: `layout.md:159-160` — "**The width cap is absolute.** No
    rendered line ever exceeds the width the block was given" — and `layout.md:139` —
    "it is that **rendered** width that sets the column — never the source width."
    Neither line today says *which* rendered width; item 7 lands that amendment.
  - Copy-safety constraint: ADR 0011 and `internal/tui/doc.go` — the Bubble Tea `Model`
    is copied by value, so any width authority must be a copy-safe value.
  - Upstream behaviour is pinned to `github.com/charmbracelet/x/ansi v0.11.7`,
    `charm.land/lipgloss/v2 v2.0.4`, `charm.land/bubbletea/v2 v2.0.7`,
    `charm.land/bubbles/v2 v2.1.0`,
    `github.com/charmbracelet/ultraviolet v0.0.0-20260525132238-948f4557a654`. If a
    dependency bump lands before this plan runs, re-verify Findings before implementing.
- **Standing requirements:** forward the `coding-standards` skill to implementer and
  verifier sub-agents at invocation; run `make check` before every commit; never touch
  `VERSION` or any CHANGELOG release heading (version bumps are the owner's call — see
  the closing note); no AI-attribution trailers in commits; the Bubble Tea `Model` is
  copied by value — never let a `strings.Builder` or other no-copy type reach it
  (ADR 0011, `internal/tui/doc.go`); any authorized deviation from an item's text must
  land as a dated NOTES line under that item.
- **Out of scope:** popup *column* alignment and the popup row-cell model (separate plan,
  `2026-07-31 - 01`, still live — see the coordination note under item 3); session
  auto-titling (`2026-07-31 - 02`); any change to what content the TUI renders, to
  colours or styles, to the markdown table layout algorithm, or to the popup/picker row
  grammar; upgrading or replacing any charm dependency; teaching apogee to negotiate
  terminal capabilities beyond the single width question in item 2.

## Findings (verified 2026-07-31 — ground truth for every item)

**Two measures are in play, and they disagree.**

- *Layout side — GraphemeWidth.* `lipgloss.Width` is a per-line `max` over
  `ansi.StringWidth` (`lipgloss/v2@v2.0.4/size.go:15-25`), and `ansi.StringWidth` resolves
  to `stringWidth(GraphemeWidth, s)` (`x/ansi@v0.11.7/width.go:65-66`), which measures
  grapheme clusters through `clipperhouse/displaywidth`. displaywidth promotes any cluster
  followed by VS16 (U+FE0F) to two cells (`displaywidth@v0.11.0/width.go:173-181`). Every
  apogee-side helper is on this side: `ansi.Cut`, `ansi.Truncate`, `ansi.Wrap`, and
  `lipgloss.JoinHorizontal`'s padding (`lipgloss/v2@v2.0.4/join.go:88`).
- *Paint side — WcWidth by default.* `tea.NewView(...)` at `internal/tui/model.go:2147`
  hands one string to bubbletea, which does `uv.NewStyledString(view.Content)` and
  `content.Draw(s.cellbuf, ...)` (`bubbletea/v2@v2.0.7/cursed_renderer.go:268, 311`).
  `printString` picks its decoder by the buffer's method (`ultraviolet/styled.go:117-120`),
  and `NewScreenBuffer` sets `Method: ansi.WcWidth` (`ultraviolet/buffer.go:612-617`) —
  `ansi.WcWidth` is `iota`, the zero value. WcWidth takes the first non-zero-width rune of
  the cluster (`go-runewidth@v0.0.23/runewidth.go:225-235`); VS16 is zero-width and U+26A0
  is neutral, so **WcWidth("⚠️") = 1 while GraphemeWidth("⚠️") = 2**.
- *The escape hatch exists but apogee never touches it.* bubbletea requests mode 2027 only
  when `shouldQuerySynchronizedOutput` allows (`bubbletea/v2@v2.0.7/tea.go:1111-1114`,
  false on Apple Terminal and on SSH with a known `TERM_PROGRAM`), and on the reply calls
  `p.renderer.setWidthMethod(ansi.GraphemeWidth)` (`tea.go:794-797`). No `2027`,
  `GraphemeWidth`, `WcWidth`, `SetWidthMethod` or `UnicodeCore` string appears anywhere in
  apogee's source. **apogee's layout code never learns which method the painter chose.**

**Three symptoms follow.**

1. *Scroll bar drift.* `renderScrollbar` (`internal/tui/model.go:2254-2276`) measures
   nothing; its column is set by `lipgloss.JoinHorizontal` at `internal/tui/model.go:2119`,
   which pads rows in GraphemeWidth. The painter then walks the row in WcWidth, advances
   one column short per VS16 grapheme, and drops that row's `│`/`█` a column left.
2. *Mouse selection drift.* `pointTranscriptRow` (`internal/tui/mouse.go:159-172`) takes
   the terminal's reported column — a **painted** (WcWidth) cell index — as-is, then
   `transcriptSelectionText` cuts with `ansi.Cut` (`mouse.go:471`) and measures with
   `lipgloss.Width` (`mouse.go:460`), both **GraphemeWidth**. Highlight paint-back
   (`highlightTranscript` `mouse.go:483-513`, `shadeCells` `mouse.go:432-441`) drifts the
   same way, so the highlight and the clipboard agree with each other and disagree with
   the pointer.
3. *Input-box caret mirror — a third, independent mismatch.* `bubbles/v2@v2.1.0/textarea`
   wraps and positions its caret with `uniseg.StringWidth` (VS16 → 2), while apogee's two
   mirrors of that widget use **per-rune** `runewidth.RuneWidth`: `wrapRowStarts`
   (`internal/tui/inputaccent.go:198, 209, 231`) and `cellToRuneOffset`
   (`internal/tui/mouse.go:196`). Both mirrors carry tests claiming to pin the widget
   (`inputaccent.go:171-183`, `mouse.go:186-190`) but **neither fixture contains a VS16
   sequence** — `inputaccent_test.go:78` uses `"日本語のテキスト 絵文字"` and
   `mouse_test.go:327` uses `"aあb🙂c"`, on which all three libraries agree. `CHANGELOG.md:3118-3124`
   records the `caretTo` fix as using "the same width the widget's own cursor math uses",
   which is not accurate.

**Measurement is scattered; there is no shared helper.** 36 production measurement sites
across six files and three libraries, with two interchangeable spellings of the same
measure and no rule about which to use:

| Group | Helper today | Sites |
|---|---|---|
| Transcript wrapping / block layout | `lipgloss.Width` | `render.go:130, 196, 199, 285, 312, 339, 347, 358, 385, 417, 422, 556, 685`; `markdown.go:193` |
| Markdown table columns | `lipgloss.Width` | `mdtable.go:257, 264, 352, 355` |
| Popups / overlays | `ansi.StringWidth` | `popup.go:185, 212, 293` |
| Status / chrome | both | `model.go:2339, 2343, 2528`; `interject.go:393`; `render.go:649` |
| Mouse mapping + selection paint | `lipgloss.Width`, `runewidth.RuneWidth`, `utf8.RuneCountInString` | `mouse.go:414, 435, 460, 497, 196, 249` |
| Input-box accent mirror | `lipgloss.Width` + `runewidth.RuneWidth` | `inputaccent.go:116, 198, 209, 231` |

Benign `len()`-as-width on ASCII constants: `markdown.go:144`, `mdtable.go:238`. Rune-count
*content* clips that are not layout widths and must not be converted:
`toolpresent.go:513`, `activity.go:121`.

**No prior decision binds.** `docs/adr/` (0001–0029) has zero hits for width, emoji,
grapheme, wcwidth, 2027 or terminal capability; `CONTEXT.md` and `TODO.md` do not contain
the word "width"; neither symptom is listed in `ISSUES.md`. ADR 0011 binds only in that the
authority must be copy-safe.

**Nothing is tested at the painted layer.** `newTestModel(t)` (`model_test.go:39-43`),
`step` (`:47`) and `plain(v tea.View)` (`:77`) all assert the *pre-paint* string. Every
width-pinning test in the package — `markdown_test.go:270`, `mdtable_test.go:280, 370, 441,
467`, `popup_test.go:30, 280, 489, 579, 598`, `model_test.go:1681, 1809, 1836`,
`render_test.go:23, 47, 62, 73`, `inputaccent_test.go:65`, `mouse_test.go:326` — asserts in
GraphemeWidth, which is the measure under suspicion. **No test in the repo renders through
ultraviolet.** That is why item 1 comes first: without a painted-cell harness, a fix to any
of the three symptoms cannot be shown to work, and a green suite proves nothing.

## 1. A frame-level paint harness — assert what the terminal shows — ✅ DONE (2026-07-31)

NOTES (2026-07-31): `paintedWidth` takes the width method as a second argument —
`paintedWidth(row string, method ansi.Method) int`, not the item's one-argument form. A painted
width is only meaningful against the measure it was painted with, so a helper that fixed one
method would re-introduce the silent assumption this plan exists to remove. Two additional
package-local helpers came with it, both used by the item's own tests and by items 3–4:
`paintedColumn(row, glyph string, method ansi.Method) int` (the screen coordinate a glyph is
painted in — the space a mouse click is reported in) and `paintTestModel`/`transcriptPaintRows`
fixtures. `paintFrame` is exactly as specified.

NOTES (2026-07-31): RECORDED DRIFT for item 3 to check against — at window 80×24 with a
40-paragraph transcript, an `⚠️` (U+26A0 U+FE0F) row on screen and the scroll bar showing a thumb:
painted under `ansi.WcWidth` every ASCII transcript row paints its `│`/`█` in **column 79**, while
the `⚠️` row paints its bar in **column 78** — exactly one column left. Painted under
`ansi.GraphemeWidth` all rows agree on column 79. Item 3's inverted test must show column 79 on
every row under BOTH methods.

NOTES (2026-07-31): `go.mod` — `github.com/charmbracelet/ultraviolet` moved from the indirect
block to the direct one (`go mod tidy`), because the harness imports it. No version changed.

**What:** Add a test-only harness that renders a `Model` the way bubbletea does and returns
the painted cell grid, so tests can assert painted columns instead of measured ones. New
file `internal/tui/paint_test.go`. Follow the traced path exactly: build the view string as
`model.go:2147` does, wrap it with `uv.NewStyledString`, `Draw` it into a screen buffer
created the way `ultraviolet/buffer.go:612-617` creates one, and read the cells back. The
harness takes the width method as a parameter so a test can paint the same frame in both
`ansi.WcWidth` and `ansi.GraphemeWidth`. Export (package-locally) `paintFrame(t *testing.T,
m Model, method ansi.Method) []string` returning one string per painted row, and
`paintedWidth(row string) int`.

Add two tests. A self-test: for an ASCII-only transcript the harness's rows equal
`plain(m.View())` line for line under both methods — proving the harness reproduces the
real path rather than inventing one. And a **characterization** test that pins today's
buggy behaviour explicitly: a transcript line containing `⚠️`, painted in `ansi.WcWidth`
with the scroll bar visible, puts that row's scroll-bar glyph one column left of every
other row's. Name it so its intent is unmistakable (e.g.
`TestPaintedScrollbarDriftsOnVS16_CharacterizesBug`) and comment that item 3 inverts it.
The suite stays green; the bug is now pinned by a test rather than by prose.

**Tests:** the two described above, plus a table-driven width comparison asserting
`ansi.StringWidth("⚠️") == 2` and the WcWidth measure of the same string `== 1`, so a
dependency bump that changes either measure fails loudly here.

**Acceptance:** `go test -race -count=1 ./internal/tui/ -run 'Paint'` passes;
`make check` passes; the characterization test's recorded drift is quoted in the item's
NOTES line so item 3 can be checked against it.

**Commit:** `test(tui): add a painted-cell harness and pin the VS16 width drift`

## 2. Choose and introduce the width authority — ✅ DONE (2026-07-31)

Depends on item 1.

NOTES (2026-07-31): OWNER'S DECISION — **(a) mirror the painter via the mode-2027 capability**. The
authority follows whatever measurement method the painter actually uses: mode 2027 confirmed →
`ansi.GraphemeWidth`, otherwise → `ansi.WcWidth`. Measurement must always match what gets painted.
(b) normalization is therefore NOT needed. Carry this into item 7's ADR.

NOTES (2026-07-31): OBSERVABILITY ANSWERED — the signal IS observable. `bubbletea/v2@v2.0.7`
`tea.go:786-798` handles `ModeReportMsg` in the eventLoop's internal switch WITHOUT a `continue`
(unlike `BatchMsg`/`sequenceMsg`), so the same message falls through to `model.Update(msg)` at
`tea.go:871` — after the renderer's method was set and before `p.render(model)`, so there is no
frame of mismatch. No `tea.With…` option and no renderer read-back is needed.

NOTES (2026-07-31): DEVIATION — the item says this one "converts no call sites" and that the diff
"shows only additions plus the `theme` field", but landing (a) also required the four-line
`case tea.ModeReportMsg:` in `Model.Update` (`model.go`) that folds the capability into the
authority and re-lays out when it moves. Without it (a) is inert — the authority would never
mirror anything and would degenerate to "always WcWidth", which the item calls wrong. No
measurement call site was converted; items 3–5 still own the conversion. The message previously
fell through to `default:` and into the textarea, which has no case for it (`bubbles/v2@v2.1.0`
has zero `ModeReportMsg` hits), so nothing was taken away from another consumer.

NOTES (2026-07-31): DEVIATION — the operations mirror `ansi.Method`'s signatures exactly rather
than the item's `Wrap(string, int) string`: `Wrap`/`Wordwrap` keep the `breakpoints` argument and
`Hardwrap` keeps `preserveSpace`. That makes each conversion in items 3–6 a rename rather than a
rewrite and keeps the seam a faithful stand-in for the library. Beyond the item's "at minimum" set
(`Width`, `Truncate`, `Cut`, `Wrap`) the authority also carries `Wordwrap` and `Hardwrap` — both
are live production measurement sites (`render.go:646`, `markdown.go:147`) — plus `Method()`, which
items 3–4 need to paint a test frame in the same measure the layout used.

NOTES (2026-07-31): the `theme` field is named `measure` (`th.measure.Width(s)`), not `width`,
which would have read `th.width.Width(s)` at ~35 call sites. `internal/tui/doc.go`'s package
narration gained a clause for `width.go` because that paragraph states it "names every file" in
the package.

**Design call (stop and ask the owner before implementing this item).** Findings establish
that apogee measures in GraphemeWidth and, on any terminal that does not answer mode 2027,
paints in WcWidth. Three strategies close the gap; they differ in what the user sees, not
merely in code shape:

- **(a) Mirror the painter.** One authority whose method starts at `ansi.WcWidth` and flips
  to `ansi.GraphemeWidth` if and when the terminal confirms mode 2027. Always agrees with
  the painter, so no drift on any terminal. Cost: apogee must observe the capability, and
  Findings do not establish that bubbletea forwards the `ModeReportMsg` to the model's
  `Update` — the first task of this item is to determine whether that signal is observable
  at all (check whether the message reaches `Update`, whether a `tea.With…` option exposes
  the method, and whether the renderer's choice can be read back). If it is not observable,
  (a) degenerates to "always WcWidth", which is wrong on modern terminals in the opposite
  direction.
- **(b) Normalize the content.** Keep GraphemeWidth as the single authority and strip or
  fold VS16 out of everything the TUI renders, so the two measures cannot disagree on the
  dominant case. Cheap, needs no capability negotiation, and is verifiable with item 1's
  harness. Cost: it is a narrowing — ZWJ sequences and regional indicators can still
  diverge — and it changes rendered glyphs (`⚠️` paints as `⚠`).
- **(c) Always WcWidth.** Simplest and correct on legacy terminals; wrong by one cell per
  VS16 grapheme on any terminal that does answer 2027, which is the growing majority.

Recommendation to put to the owner: **(a), with (b) as the fallback** if the capability
signal turns out not to be observable — and, if (a) lands, (b)'s normalization is not
needed. Record the owner's choice as a dated NOTES line under this item before writing
code, and carry it into the ADR in item 7.

**What:** Create `internal/tui/width.go` holding the decision: a copy-safe value type (a
plain struct or a named `ansi.Method`, per ADR 0011 — no pointers, no `sync` types, nothing
that panics when the `Model` is copied) with the measurement operations the codebase needs
— at minimum `Width(string) int`, `Truncate(string, int, string) string`, `Cut(string, int,
int) string`, `Wrap(string, int) string` — each dispatching on the chosen method. Hang it
where `theme` already hangs: `theme` (`internal/tui/theme.go:93+`) is a plain value struct
already threaded as the first parameter through every free renderer function
(`renderMarkdownBody(th, …)`, `renderTable(th, …)`, `popupBodyLines(th, …)`), so adding the
authority there reaches most sites without signature churn. Findings names the exceptions
that take no `theme` today and will need one threaded or an explicit parameter: `wrapText`,
`wrappedOffset`, `truncateToWidth`, `popupColumnWidths`, `cellToRuneOffset`, `wrapRowStarts`,
`shadeCells`, `transcriptSelectionText`.

This item introduces the seam and its tests **only** — it converts no call sites, so the
diff is additive and the suite stays green. Items 3–5 do the conversion.

**Tests:** new `internal/tui/width_test.go` — each operation agrees with the corresponding
`ansi` function under `GraphemeWidth`; under `WcWidth` the VS16 case measures 1 and cuts
accordingly; the type survives a value copy (mirror the guard style of
`TestModelNoBuilderByValue`).

**Acceptance:** `go test -race -count=1 ./internal/tui/ -run 'Width'` passes; `make check`
passes; `git diff --stat` for this item shows only additions plus the `theme` field.

**Commit:** `feat(tui): add a single display-width authority`

## 3. Route the render path through the authority — ✅ DONE (2026-07-31)

Depends on item 2.

NOTES (2026-07-31): COORDINATION — plan `2026-07-31 - 01` is **not archived** (its items 3–5 carry no
✅), so its four files are left alone per this item's own instruction: `popup.go:185, 212, 293` and
`interject.go:393` (gated on the archived branch) are NOT converted here and need a follow-up item.
`popup.go` is also why `wrapText` (`render.go:577`, `ansi.Wrap`) keeps its signature and its
hard-wired GraphemeWidth: giving it the authority means a parameter, and `popup.go:245` is its other
caller. Leaving it is safe for the cap — a GraphemeWidth wrap never paints wider than its limit under
WcWidth — and the follow-up item should take it together with `truncateToWidth`.

NOTES (2026-07-31): DEVIATION — two of the listed sites are deliberately NOT converted, and the
acceptance grep's "sites this item's NOTES line explicitly justifies leaving" is exactly them.
`render.go:649` (`inputContentRows`) and `render.go:685` (`wrappedOffset`) are **widget mirrors**, and
item 5's rule governs them: a mirror's oracle is the widget, never the painter. The textarea wraps
with `uniseg.StringWidth` (`bubbles/v2@v2.1.0/textarea/textarea.go:1805-1852`) and the viewport
soft-wraps with `ansi.StringWidth` (`viewport/viewport.go:284, 415`) — both grapheme-clustered, and
neither moves when the painter does. Measuring them in the authority would size the input box and the
sticky-header pad to something no widget ever draws. Both sites now carry a WIDGET MIRROR comment
naming their oracle. `inputContentRows` is a third mirror alongside item 5's two; item 5 should adopt
it when it makes the mirrors measure exactly the way the widget does.

NOTES (2026-07-31): DEVIATION (scope) — closing symptom 1 took a second join, not just the one the
item names. Squaring the transcript rows in the authority's measure makes their *GraphemeWidth* vary,
and the frame's outer `lipgloss.JoinVertical` left-aligns by padding every row to the widest row it
was given **in GraphemeWidth** — so the fix to the bar column pushed all 24 rows to 105 columns on an
80-column window, and the item's own painted cap test failed. `View` now composes through
`Model.joinFrame`, the vertical counterpart of `joinScrollbar`; both go through one `squareLine`
primitive (pad, or ANSI-aware cut, to exactly N columns in the authority's measure). This is what
makes `layout.md:159-160`'s absolute cap hold at the painted layer.

NOTES (2026-07-31): the ANSI operations that CONSUME a converted measurement were converted with it —
`ansi.Truncate` at `render.go:197`, `mdtable.go:353`, `model.go` footer/status, and the `ansi.Hardwrap`
at `markdown.go:147` that item 2's NOTES already names as a live site. A width measured in one method
and cut in another is the same defect one step later. Signatures threaded to reach the authority:
`hangingWrap`, `hangingPrefixes`, `withMarker`, `startupLabelWidth`, `startupInfoWidth`,
`tableColumnWidths`, `layoutTableRow`, `padTableCell` all now take `th theme` first (the exceptions
Findings predicted). `markdown.go` and `mdtable.go` no longer import `lipgloss` or `ansi` at all.

NOTES (2026-07-31): the inverted test is `TestPaintedScrollbarHoldsOneColumn`, and it paints each
frame in the measure the model's own authority is on (new `paintedAs` harness helper, which feeds the
model the `tea.ModeReportMsg` bubbletea feeds it). Painting a WcWidth-composed frame with a
GraphemeWidth painter is the mismatch the authority exists to prevent, not a case it must survive.
Checked against item 1's recorded drift: at 80×24 the bar now paints in **column 79 on every row,
the `⚠️` row included, under both methods** (it was 78 on that row under WcWidth). No existing
width-pinning test needed its expectation changed — every glyph the package uses as a marker
(`✦ ❯ ┝ ┕ │ ⤷ • ▤ ⧖ ─ ▔ █`) measures 1 in both methods, so nothing but the VS16 case moves.

**Coordination note (check before starting).** Plan `2026-07-31 - 01 - popup-column-alignment-plan.md`
is live and owns `popup.go`, `autocomplete.go`, `picker.go` and `sessions.go`; at the time
this plan was written its items 3–5 were outstanding and item 3's work sat uncommitted in
the working tree. If that plan is not yet archived, **leave its four files alone** and
convert them in a follow-up item rather than colliding with it; record that decision as a
dated NOTES line here. If it is archived, include `popup.go:185, 212, 293` and
`interject.go:393` in this item's conversion.

**What:** Convert the layout-side measurement sites named in Findings to the item-2
authority: `render.go:130, 196, 199, 285, 312, 339, 347, 358, 385, 417, 422, 556, 649, 685`;
`markdown.go:193`; `mdtable.go:257, 264, 352, 355`; `model.go:2339, 2343, 2528`. Leave the
benign ASCII-constant `len()` uses at `markdown.go:144` and `mdtable.go:238` alone, and do
**not** convert the rune-count content clips at `toolpresent.go:513` and `activity.go:121` —
they cap content, not layout.

Then close symptom 1: the scroll bar's column is set by the `lipgloss.JoinHorizontal` at
`model.go:2119`, which pads in GraphemeWidth regardless of the authority. Compose the
transcript body and `renderScrollbar` (`model.go:2254-2276`) so each row is padded to the
viewport width **in the authority's measure** before the bar is appended, rather than
relying on `JoinHorizontal`'s own padding.

Invert item 1's characterization test: the scroll-bar glyph now lands in the same painted
column on every row, including the `⚠️` row, under both width methods. Rename it to state
the invariant rather than the bug.

**Tests:** the inverted scroll-bar test from item 1; a painted-frame assertion that no
painted row exceeds the terminal width for a transcript mixing ASCII, CJK and VS16 content
(the `layout.md:159-160` absolute cap, now asserted at the painted layer); the existing
width-pinning tests listed in Findings must keep passing unmodified except where the
authority's measure legitimately changes an expectation — any such change is justified in
the item's NOTES line.

**Acceptance:** `go test -race -count=1 ./internal/tui/` passes; `make check` passes;
`grep -n 'lipgloss.Width\|ansi.StringWidth' internal/tui/render.go internal/tui/markdown.go internal/tui/mdtable.go internal/tui/model.go` returns only sites this item's NOTES line
explicitly justifies leaving.

**Commit:** `fix(tui): measure the render path with one width authority`

## 4. Fix the mouse column mapping — ✅ DONE (2026-07-31)

Depends on item 3.

NOTES (2026-07-31): DEVIATION — `pointTranscriptRow` converts NOTHING, because under item 2's decision
(a) the conversion is the IDENTITY. The authority measures the way the painter paints, so the terminal's
painted column already indexes the rendered line in the authority's space, and the transcript body starts
at screen column 0. What actually closes symptom 2 is routing the CUTS through the authority — measure a
line in one method and slice it in another and the pointer names one glyph while the clipboard takes its
neighbour. The reasoning is now stated in `pointTranscriptRow`'s doc comment, so a later move to strategy
(b) or (c) sees that it depends on the authority mirroring the painter.

NOTES (2026-07-31): DEFECT IN THE AUTHORITY, fixed here because item 4 cannot land on top of it (item 2 is
already ✅, so this is its territory). `x/ansi@v0.11.7`'s `cut` binds its LEFT truncation to `TruncateWc` —
a *right*-truncation — on the WcWidth branch (`truncate.go:35-39`; the grapheme branch correctly uses
`TruncateLeft`), so `ansi.Method.Cut(s, left, right)` under WcWidth spends `left` as a *width* rather than
as an offset and returns the first `left` columns: `Cut("abcdef", 2, 5)` hands back `"ab"` where the span is
`"cde"`. WcWidth is the painter's DEFAULT and a mouse selection is exactly a cut with a
non-zero left, so every terminal that does not answer mode 2027 would have copied from the wrong end of the
line. `widthAuthority.Cut` now composes `Truncate` + `TruncateLeft` itself (`width.go`), with
`TestWidthAuthorityCutsFromTheLeft` pinning it and a comment naming the upstream defect so it can go back to
delegating after a dependency bump. Item 2's parity test could not see it: every case there cuts from
column 0.

NOTES (2026-07-31): DEVIATION — `mouse.go:249` (`runeOffsetOf`) is deliberately NOT converted. It is
`utf8.RuneCountInString` over a byte prefix — the byte↔rune bridge between the chat mini-language and the
textarea's rune-counted cursor — and there is no display column at either end of it. Findings groups it with
the measurement sites by library, not by role. It now says so in a comment.

NOTES (2026-07-31): SCOPE — `shadeCells` is shared with `accentTokens` (`inputaccent.go:122`), which now
passes the authority too: the cells being re-styled are cells the terminal already painted, so the slice must
be the painter's whoever the caller is. That call's COLUMNS still come from the per-rune widget mirror
(`runesWidth`) that item 5 owns, and `inputaccent.go:116` stays on `lipgloss.Width` for the same reason
(Findings assigns it to the mirror group). The two agree on the VS16 case under WcWidth and disagree under
GraphemeWidth exactly as they did before this item, so nothing regressed — but item 5 should reconcile them:
a mirror's wrap ROWS are the widget's, while its columns WITHIN a row are the painter's.

NOTES (2026-07-31): both new tests fail against the pre-change code. At 80×24 the ⚠️ row paints as
`❯ danger ⚠️ zebra`; a drag from painted column 11 — the `z` under WcWidth — copied `" zebr"`, the
neighbouring glyph, and the highlight measured 16 columns against a 15-column painted span.

**What:** Close symptom 2. `pointTranscriptRow` (`mouse.go:159-172`) takes the terminal's
reported column as a content cell index; the terminal reports **painted** cells. Convert
that coordinate into the authority's space before it reaches any cut, and route
`transcriptSelectionText` (`mouse.go:449-475`, cutting at `:471`, measuring at `:460`),
`highlightTranscript` (`:483-513`) and `shadeCells` (`:432-441`) through the item-2
authority so the pointer, the highlight and the clipboard all agree. Sites named in
Findings: `mouse.go:414, 435, 460, 497, 249`.

**Tests:** using item 1's harness, a click at painted column *n* on a row containing `⚠️`
selects the glyph painted at column *n* — asserted under both width methods; the existing
`mouse_test.go` selection tests keep passing; a regression test that the highlight's painted
extent equals the selected text's painted width.

**Acceptance:** `go test -race -count=1 ./internal/tui/ -run 'Mouse|Selection|Transcript'`
passes; `make check` passes.

**Commit:** `fix(tui): map mouse columns through the width authority`

## 5. Correct the input-box caret mirrors — ✅ DONE (2026-07-31)

Depends on item 2.

NOTES (2026-07-31): the mirrors now measure with `uniseg.StringWidth` — the widget's own function, not a
grapheme-clustered stand-in for it. `runesWidth` is that one measure and both mirrors route through it
(`cellToRuneOffset` inverts it over rune prefixes, exactly as `textarea.LineInfo` builds `CharOffset` and
`textarea.Cursor` its x). `github.com/rivo/uniseg` therefore moves from the indirect block of `go.mod` to
the direct one; no version changed. Reaching for `ansi.GraphemeWidth` instead would have been a second
guess at the widget rather than a mirror of it.

NOTES (2026-07-31): DEVIATION — one of the three named `inputaccent.go` sites deliberately KEEPS
`runewidth.RuneWidth`: the last-rune term of the hard-word-break test, because the widget itself weighs
that rune with go-runewidth there (`bubbles/v2@v2.1.0/textarea/textarea.go:1838-1839`, `lastCharLen`)
while measuring the word with uniseg. "Measure the way the widget does" is the rule, and at that one term
the widget does not use uniseg. It is also load-bearing: `rw.RuneWidth(U+FE0F) == 0` is what lets a
VS16-filled word reach the full row width without ever tripping the break, which is how the widget comes
to draw an EMPTY leading row (see the test note below).

NOTES (2026-07-31): DEVIATION (scope) — this item also lands the accent-pass reconciliation item 4's
NOTES flagged, since item 4 is ✅ and its note assigned it here. `inputCellSpans` now takes the width
authority and measures its COLUMNS with it, and `accentTokens`' clamp moved off `lipgloss.Width` (the
`inputaccent.go:116` Findings assigns to the mirror group). The rule is now stated at both sites: a
mirror's ROWS are the widget's — only it decides which runes it put on which line — while the COLUMNS
address cells the painter has already drawn, so they are the authority's. `accentTokens`' doc no longer
claims `shadeCells` cuts with `ansi.Cut`; it has cut through the authority since item 4.

NOTES (2026-07-31): `TestWrapRowStartsMirrorsTheWidget` now keys its expectation by `LineInfo.RowOffset`
instead of reading `StartColumn` in order. The `⚠️⚠️⚠️ end` fixture at width 6 makes the widget draw an
empty FIRST row (the group overflows without the hard break firing), and an empty row is addressed by no
cursor column at all — the old dedup-in-order oracle could not see it, and would have demanded the mirror
drop a row that is on screen, which is exactly a row an accent would then paint one line too high.

NOTES (2026-07-31): both fixtures fail against the pre-change per-rune measure, as the item requires.
`TestWrapRowStartsMirrorsTheWidget`: `an emoji carrying VS16` and `a VS16 run filling the row` report
"widget draws 3 rows, wrapRowStarts says 2", and `VS16 inside a word too wide for the row` reports
"col 4: runesWidth from the row start = 3, widget's CharOffset = 4". `TestCellToRuneOffset`:
`cellToRuneOffset("a⚠️b", 3) = 4, want 3` — the caret one glyph past the click. And
`TestCellToRuneOffsetInvertsWidth` breaks at every boundary after the first VS16 (5 boundaries of
`"a⚠️b ⚠️"`).

NOTES (2026-07-31): FOLLOW-UP for item 7 or a new item — `inputContentRows` (`render.go:648`), the third
widget mirror item 3's NOTES asked this item to adopt, was NOT adopted: it is not a measurement fix but a
box-height change, and the two mirrors disagree far too widely to fold in under this item's tests. Checked
against a real textarea's `LineInfo.Height`: `wrapRowStarts` matches the widget on every non-tab case
tried while `inputContentRows` does not — `"hello world"` at width 5 is 4 widget rows and it says 3,
`"a b  c"` at 3 is 3 and it says 2, `"a-b-c-d"` at 3 is 3 and it says 4 — and over 4000 random
prompt-shaped inputs the two differ on 41%. It under-counts, which is the ISSUES #2 failure mode its own
docstring describes. `Σ len(wrapRowStarts(line, w))` is the drop-in replacement; both mirrors are still
wrong on tabs (the widget expands them).

**What:** Close symptom 3, which is independent of the painter question: the `textarea`
widget measures with `uniseg.StringWidth` (grapheme-clustered), while apogee's two mirrors
of its cursor math measure per rune with `runewidth.RuneWidth` — `wrapRowStarts`
(`inputaccent.go:198, 209, 231`) and `cellToRuneOffset` (`mouse.go:196`). Both must measure
the way the widget does, whatever the authority chooses for the rest of the TUI: these
functions mirror a third-party widget's internal math, so the widget — not apogee's
painter-facing authority — is their oracle. Say so in a comment at both sites, since the
distinction is exactly what the current code gets wrong. Also correct the inaccurate
`CHANGELOG.md:3118-3124` claim in item 7, not here.

**Tests:** extend the two mirror fixtures with a VS16 sequence — `inputaccent_test.go:78`
and `mouse_test.go:327` — so `TestWrapRowStartsMirrorsTheWidget` and
`TestCellToRuneOffsetInvertsWidth` actually exercise the disagreement they claim to pin.
Both must fail against the pre-change code; state that in the item's NOTES line with the
observed failure.

**Acceptance:** `go test -race -count=1 ./internal/tui/ -run 'WrapRowStarts|CellToRuneOffset|InputAccent'`
passes; `make check` passes.

**Commit:** `fix(tui): mirror the textarea's own width measure in the caret math`

## 6. Hold the absolute width cap at tiny widths — ✅ DONE (2026-07-31)

NOTES (2026-07-31): DEVIATION — `wrapText` now takes `th theme` first, so it can measure with the
authority as this item requires; that touches `popup.go:245`, a file plan `2026-07-31 - 01` still owns
(its items 3–5 carry no ✅). It is a call-site rename, not one of the popup MEASUREMENT conversions
item 3 deferred — `popupBodyLines` already had `th` — and `popupBodyLines` is in none of that plan's
outstanding items. Item 3's NOTES gave the parameter as its reason for leaving `wrapText` on a
hard-wired GraphemeWidth; that reason is spent, but the WRAP itself still calls `ansi.Wrap`, because
this item's text converts only the cap enforcement ("any returned line **still** wider than the
limit"). The follow-up item item 3 named — `wrapText`'s wrap together with `truncateToWidth` — stands.

NOTES (2026-07-31): the defect is wider than "at limits ≤ 3". The breakpoint branch has no
already-full-line check at ALL, so it grows a word onto a full line at any limit:
`ansi.Wrap("| --- | --- | --- |", 8, "")` returns an eleven-cell first line, and the single hyphen in
"sub-agent" trips it at width 12 in `TestSubAgentReflowAtSmallWidths`. The fix is limit-agnostic for
the same reason.

NOTES (2026-07-31): the one thing the cap cannot hold is a single grapheme wider than the limit — a
CJK glyph at limit 1 — which no break can divide; it gets a line to itself, and
`TestWrapTextHoldsTheWidthCap` allows exactly that case (`ansi.FirstGraphemeCluster(ln) == ln`) and
nothing else. The enforcement pass hard-wraps with `preserveSpace: true` so it only INSERTS breaks —
a line's own indentation survives — and the test pins that by comparing its non-space content against
the plain wrap's. The blank row the hard wrap opens ahead of an over-wide leading grapheme is dropped.

NOTES (2026-07-31): all three assertions fail against the pre-change code, as the item requires.
`TestTableUnfittableFallsBack` (its 486-487 wording rewritten, and its `---` match now made across the
breaks the wrapper takes at these widths, plus a real width bound): `width 1: fallback line "| --- |"
is 7 cells wide`. `TestSubAgentReflowAtSmallWidths`: `width 6: line 14 "│ │   -agent" is 12 cells
wide, over the 7 cap` — its bound is `max(width, floor)`, floor = two rail gutters + the ✦ marker +
one column, because below 7 columns it is the MARKER, not the wrapper, that cannot fit (widths 12 and
40 were added to exercise the un-floored bound). `TestWrapTextHoldsTheWidthCap`: `line 0 "| --- |" is
7 cells wide, over the 1 cap`.

**What:** `wrapText` (`internal/tui/render.go:570-578`) returns lines wider than its limit
for pipe/hyphen token lines at limits ≤ 3, violating `layout.md:159-160`. Findings locates
the defect **upstream**, in `x/ansi@v0.11.7/wrap.go:408-419`: the breakpoint branch (which
treats `-` as a breakpoint unconditionally, `wrap.go:406-407`) lacks both the leading
`curWidth == limit` newline check and the trailing overflow check that the `default:` branch
at `wrap.go:420-435` has, so a hyphen run keeps growing a word onto an already-full line.
`ansi.Wrap("| --- | --- | --- |", 3, "")` yields a 5-cell first line; `ansi.Wrap("----", 3,
"")` yields 4 cells. The grapheme path has the same gap at `wrap.go:352-361`.

Fix it in apogee, not upstream: have `wrapText` enforce its own contract by hard-breaking
any returned line still wider than the limit, measured with the item-2 authority. The
docstring at `render.go:570-573` already promises hard-breaking of over-long words — this
makes the promise true. Reachable in production via `railedWidth` (`render.go:590-595`,
floors at 1) for deeply nested sub-agent blocks and on very narrow terminals; `popup.go:245`
is already protected by its own re-clip at `popup.go:257`, and that re-clip stays.

`mdtable_test.go:486-487` currently documents the over-wide behaviour as "pre-existing …
not the table path's to change" — this item owns that wording; update it. Likewise
`render_test.go:73` (`TestSubAgentReflowAtSmallWidths`) asserts only non-panic and
non-empty at widths 0/1/2/3/6 — give it a real width bound.

**Tests:** a table-driven `wrapText` test over limits 1–8 and inputs including `|`, `-`,
`---`, mixed pipe/hyphen runs, CJK and VS16 — no returned line exceeds the limit;
`render_test.go:73` gains its width assertion; `mdtable_test.go:467-490`'s fallback
expectation updated to the now-bounded behaviour.

**Acceptance:** `go test -race -count=1 ./internal/tui/ -run 'Wrap|Reflow|Table'` passes;
`make check` passes.

**Commit:** `fix(tui): keep wrapped lines within the width cap at tiny widths`

## 7. ADR, docs and ticket closeout — ✅ DONE (2026-07-31)

Depends on items 1–6.

NOTES (2026-07-31): the ADR is `docs/adr/0030-the-tui-has-one-width-authority-and-it-mirrors-the-painter.md`
(0030 was free). It records owner decision (a) and why, the two-measure background, the one-authority
rule, and item 5's widget-mirror exception — plus the four decisions that are only visible in the code
and would otherwise be re-litigated: the `Cut` and `wrapText` workarounds for the two `x/ansi@v0.11.7`
defects (each revertable after a bump), the squared frame replacing lipgloss's joins, and painted cells
as the standard of proof.

NOTES (2026-07-31): DEVIATION — **no term was added to `CONTEXT.md`**; the concept map does not warrant
it. Its 90 glossary terms are the agent domain (Mechanisms, Steps, Turns, Sessions, tools, confinement)
and it carries **no** TUI presentation vocabulary at all — no Transcript, Scrollback, Footer or Block
entry to sit beside. The TUI's own language lives in `layout.md` and `internal/tui/doc.go`, and both now
carry the width authority.

NOTES (2026-07-31): DEVIATION — `layout.md` gained a short **new section** ("What 'width' means everywhere
below", after the sketch) as well as the two amendments the item names. Both `:139` and `:159-160` had to
name the same measure, and stating it once — with the mode-2027 mechanism and the prompt-box exception —
lets each line point at it instead of carrying its own half-explanation. The amendment stays on the width
question; the popup row grammar is untouched.

NOTES (2026-07-31): DEVIATION (scope) — a `TODO.md` section ("The TUI width authority — what it did not
convert") was added, which the item does not name. It is where this plan's accumulated residues become
findable rather than living only in NOTES lines a reader would have to know to look for: the four sites
item 3 deferred to plan `2026-07-31 - 01` plus `wrapText`'s own `ansi.Wrap` and `truncateToWidth`
(item 6's NOTES), `inputContentRows`' 41% divergence from the widget (item 5's NOTES), and the
pre-existing `hangingPrefixes` floor. ADR 0030's Consequences names the first group; TODO.md holds all
three with the fix each one wants.

NOTES (2026-07-31): OPEN QUESTION FOR THE OWNER — **the two defects were NOT filed in `ISSUES.md`**, and
the item allows either. Two things argued against filing: the convention commit the item names
(`bf527ed`) closes an entry by **deleting** it, and the file today holds only open, owner-voiced rows —
no `[x]` row survives in it — so filing-then-deleting is a no-op and filing a new `[x]` row would
reintroduce a form the file was cleaned of (the older in-place style is `8084ec4`). The record lands in
the CHANGELOG's Unreleased → Fixed and in ADR 0030 instead. Say the word and both entries go in.

NOTES (2026-07-31): the stale `ISSUES #2` pointer at `internal/tui/render.go` (now in
`inputContentRows`' docstring) named a number that no longer resolves: `ISSUES.md`'s entries have never
carried numbers — `#2` was positional — and the entry it meant was closed in `8084ec4` and later dropped
from the file, so today the second row is a different issue entirely. It now names the fix commit
(`a7afbf1`) and the regression test that pins it. The same docstring gained a KNOWN DIVERGENCE
paragraph, because item 5's NOTES proved the function under-counts in exactly the way the sentence above
it warns about; it points at the TODO entry. Other `ISSUES #N` pointers in the package (`model.go`,
`prompteditor.go`, `autocomplete.go` and several tests) are stale the same way and were left — this item
names only `render.go`.

**What:** Write the ADR that settles the question — next free number in `docs/adr/`
(0030 at the time of writing; check). It records the strategy chosen in item 2 and why,
the two-measure background from Findings, the rule that the TUI has exactly one
painter-facing width authority, and the deliberate exception from item 5 (widget mirrors
measure the way the widget does). Add the term to `CONTEXT.md` if the concept map warrants
it.

Amend `layout.md`: `layout.md:159-160`'s absolute width cap and `layout.md:139`'s "rendered
width" both need to name *which* rendered width now that the answer exists. Keep the
amendment to the width question — the popup row grammar is item 5 of plan
`2026-07-31 - 01` and stays that plan's to write.

Add both defects to `ISSUES.md` as closed entries in the same edit that closes them (follow
the convention commit `bf527ed` used), or omit them if the owner would rather they were
never filed — ask if unclear. Correct the inaccurate claim at `CHANGELOG.md:3118-3124`
with a dated note rather than by rewriting shipped history, and add the unreleased
CHANGELOG entries for this plan's user-visible fixes. Fix the stale "ISSUES #2" pointer at
`internal/tui/render.go:637` while in the area.

**Tests:** none (docs only).

**Acceptance:** `grep -rn "width authority" docs/adr/ layout.md` hits the new ADR and the
amended `layout.md`; `make check` passes.

**Commit:** `docs: record the TUI width authority and close the width defects`

## Suggested version bump

Three user-visible correctness fixes to the TUI (scroll-bar placement, mouse selection,
caret mirror) plus a width cap that now actually holds. A patch-level bump on the current
`0.10.x` line looks right at the owner's next release cut; a minor bump would also be
defensible given the new ADR and the cross-cutting refactor. No version identifier is
changed by this plan — whether and when to bump is the owner's decision. Note `VERSION`
moved to `v0.10.5` in commit `62f920a` after this plan's Findings were gathered.
