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

## 2. Choose and introduce the width authority

Depends on item 1.

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

## 3. Route the render path through the authority

Depends on item 2.

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

## 4. Fix the mouse column mapping

Depends on item 3.

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

## 5. Correct the input-box caret mirrors

Depends on item 2.

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

## 6. Hold the absolute width cap at tiny widths

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

## 7. ADR, docs and ticket closeout

Depends on items 1–6.

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
