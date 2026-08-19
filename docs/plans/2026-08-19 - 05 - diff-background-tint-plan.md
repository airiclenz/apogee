# Diff lines carry their color as a background tint — implementation plan

**Goal:** Added/removed diff lines mark themselves with a background tint (turquoise-family
for added, red-family for removed) instead of coloring the text. The text itself wears the
plain detail tone; the tint runs the full row/pane width; the line-number gutter stays
chrome. A deliberate visual change — golden expectations update with it.

- **Date:** 2026-08-19
- **Status:** not started
- **Sized for:** ~200k-context host

## Authoritative sources

- This plan's Ratified design calls (user decisions, 2026-08-19) — the binding visual
  contract.
- ADR 0052 (split diffs) — this plan amends its rendering contract (item 3 records the
  amendment). The split-diff plan's ratified calls 6/7 ("the marker travels with the
  TEXT's colour") are superseded: the marker becomes a glyph signal riding the tinted
  band.
- ADR 0040 (scheme roles: one vocabulary, one seam), ADR 0030 (width authority),
  ADR 0011 (value-type Model), ADR 0043 (flat package, doc.go map).
- Evidence verified in the working tree on 2026-08-19: `detailStyle` (toolbranch.go) is
  the single style seam for diff-kind detail lines; its six call sites style through
  `gutteredWrap`/`hangingWrap`/`clipWrap` (wrap.go) plus split view's own
  `splitCell.paint`/`splitPad` (splitdiff.go); the styles are built at theme.go's
  constructor from scheme roles `DiffAdd`/`DiffDel`. Line numbers will drift — anchor on
  these names.

## Ratified design calls

1. **The background carries the added/removed signal; the text does not.** Diff-line
   text wears the plain detail tone of its block state (`detailTone`: collapsed muted /
   expanded muted-bright), identical to ordinary detail text. (User, 2026-08-19.)
2. **Full-width tint.** The band runs from the marker column to the pane edge (split
   view) or the wrap rail (stacked/flat views), including under short lines' trailing
   space and under wrapped continuation rows' text. (User, 2026-08-19.)
3. **The gutter stays chrome.** Line numbers and the gutter columns keep their current
   muted look, untinted — including the gutter-width leading spaces on continuation
   rows. (User, 2026-08-19.)
4. **Two new scheme roles, defaults bound here.** `diff-add-bg` / `diff-del-bg` join the
   scheme vocabulary (ADR 0040's single seam; scheme parsing already treats omitted keys
   as "keep default", so existing user scheme files stay valid). Defaults: dark
   `#0e3b34` / `#42181d`; light `#d9f2ec` / `#fbe4e6` — quiet bands in the same
   turquoise/red families as `diff-add`/`diff-del`, so the pairing still survives
   red-green-weak vision; the user may tune the hex values by eye afterward via scheme
   files. (Plan author, 2026-08-19.)
5. **The tint is constant across block states.** Collapsed and expanded diff lines carry
   the same background; only the text tone follows the state, like every other detail
   line. (Plan author, from call 1, 2026-08-19.)
6. **Where the padding lives:** the wrap rails (wrap.go) pad wrapped lines to their rail
   inside the style whenever the style carries a background — one rule all frames
   inherit, no per-call-site changes; split view pads inside `paint` where it owns the
   pane width. (Plan author, 2026-08-19.)

## Standing requirements

- skills: coding-standards
- This is a deliberate visual change: golden/layout expectations are UPDATED to the new
  contract, never worked around. Everything not named here renders byte-identical.
- ADR 0030: all padding is measured through `theme.measure`; no banned width calls.
- ADR 0043: no new files expected; if one is created it costs a doc.go line in the same
  commit.
- Any authorized deviation from item text lands as a dated NOTES line under the item.
- No version identifier changes (see the closing note).

## Sequencing against the deepening plan (04)

Run this plan BEFORE `2026-08-19 - 04 - tui-architecture-deepening-plan.md`
(recommended): plan 04's items are behaviour-preserving moves and will carry the tint
along. If 04 (or part of it) has already run, several files named below have moved —
toolpresent.go's stacked-diff code into diffbody.go, the frame renderers behind
toolbody.go — anchor on the function names and record a dated NOTES line naming the
actual files touched.

## Out of scope

- Stat/summary strings ("+8 −3", "12 lines") and every other use of the
  `diff-add`/`diff-del` foreground roles outside diff body lines — unchanged.
- The `⋯` region rule, context lines (`detailPlain` rows in diff bodies), markers'
  glyphs, gutter arithmetic, wrap behavior — unchanged except for the tint itself.
- Renaming the existing `diff-add`/`diff-del` roles or removing them.

---

## 1. Verify the split-diff plan is archived — ✅ DONE (2026-08-19)

NOTES (2026-08-19): gate passed — `docs/plans/archived/2026-08-19 - 03 - split-diff-display-plan.md`
present (archived by commit a1c0e57d); `ls docs/plans/ | grep -c split-diff` prints 0. `docs/plans/`
holds only the 04 deepening plan and this plan.

**What:** Confirm `docs/plans/archived/` contains
`2026-08-19 - 03 - split-diff-display-plan.md` and it is gone from `docs/plans/`. This
plan restyles the rendering that plan built; running both at once would collide on
splitdiff.go and the frame files. If it is not archived, report BLOCKED — do not
proceed.

**Files:** none (read-only gate; the verifier's commit carries only this plan file's
done-mark).

**Tests:** none.

**Acceptance:** `ls "docs/plans/archived/" | grep "split-diff-display-plan"` succeeds
and `ls docs/plans/ | grep -c split-diff` prints 0.

**Commit:** `chore(plans): gate the diff background tint on the landed split-diff plan`

## 2. Add the diff-add-bg / diff-del-bg scheme roles — ✅ DONE (2026-08-19)

NOTES (2026-08-19): the role count is stated in prose in three places the role-table test pins by
name (README.md, layout.md, newTheme's comment in internal/tui/theme.go); adding two roles moves it
29 → 31, so those three lines are updated here even though the item's Files list names only the
four scheme-package files. Comment text only — no code or style change in theme.go, which item 3
owns.

**Source:** ratified call 4.

**What:** Add two fields to `Scheme` in `internal/scheme/scheme.go` — `DiffAddBg`
(yaml `diff-add-bg`) and `DiffDelBg` (yaml `diff-del-bg`) — with doc comments naming
their meaning: the background band a diff body line carries; the paired foreground roles
keep the markers/summaries. `roleKeys`/`fieldIndex` derive automatically from the struct.
Add the bound default values to both built-in scheme files: dark.yaml `#0e3b34` /
`#42181d`, light.yaml `#d9f2ec` / `#fbe4e6`, each with a comment in the files' existing
voice (quiet band under the matching foreground hue; the turquoise-vs-red pairing
survives red-green-weak vision). Extend scheme_test.go's role table with the two rows.
Omission stays silent for user scheme files (existing Parse behavior — verify, don't
change).

**Files:** internal/scheme/scheme.go, internal/scheme/schemes/dark.yaml,
internal/scheme/schemes/light.yaml, internal/scheme/scheme_test.go

**Tests:** the role-table test covers both new keys; existing Parse degrade tests pass
unchanged.

**Acceptance:** `go build ./... && go test ./internal/scheme`

**Commit:** `feat(scheme): add diff-add-bg and diff-del-bg background roles`

## 3. Diff styles become background tints; text follows the state tone — ✅ DONE (2026-08-19)

NOTES (2026-08-19): four files beyond the item's Files list were touched, each because this change made a
sentence in them false: `internal/tui/doc.go` ("the diff colours stay where they were" — the band does,
the text no longer), `layout.md`'s "A change is coloured the one way" paragraph, and
`docs/layout/split-diff-layout.md`'s pane/colour/example bullets. Prose only, no code or behaviour beyond
the item.
NOTES (2026-08-19): `theme_test.go` was not in the enumerated set either, but its role-sampling table pinned
`diffAdded`/`diffRemoved` as FOREGROUNDS; it now samples them as backgrounds and its distinct-value scheme
fixture gains `DiffAddBg`/`DiffDelBg`.
NOTES (2026-08-19): `gofmt` re-aligned the five theme-struct field lines above `diffAdded` (successMark …
selection) because the new doc comment splits their alignment group. Whitespace only, gofmt's call.
NOTES (2026-08-19): `TestDiffLinesKeepTheirColourInBothBlockStates` and
`TestSplitDiffRowsColourTheMarkerWithItsLine` keep their names — both still describe the new contract (the
BAND is what is kept in both states; the marker is still coloured with its line, now inside the band) — and
their bodies and comments were rewritten to it. The item's requested unit test landed as
`TestDetailStyleBandsTheDiffKindsUnderTheStateTone`.
NOTES (2026-08-19): the `## [Unreleased]` entry item 2 landed claims the `diff-add` / `diff-del` foreground
pair "keeps everything it already colours, markers and `+8 −3` summaries included". That is no longer true
of any of it — see the DEFER line — so the clause is worth dropping when this item's entry is applied.

**Source:** ratified calls 1, 5. Depends on item 2.

**What:** In theme.go's constructor, rebuild the two diff styles from the new roles as
background-only tints (no foreground). In `detailStyle` (toolbranch.go), the diff kinds
return `detailTone(th, expanded)` composed with the tint's background — text wears the
plain per-state tone, the band says added/removed, constant across states. Rewrite the
now-false comments in the same commit: theme.go's "a diff line keeps diffAdded /
diffRemoved in both states" paragraph and the diff-style field comments;
`detailStyle`'s doc ("red/green in both …"). Append a dated amendment note to ADR 0052
recording the new contract (background tint, plain text tone, full-width band, chrome
gutter) and that its ratified calls 6/7's "marker travels with the text's colour"
rationale is superseded — the marker is a glyph signal on the band. Golden expectations
that pin the old foreground styling update to the new contract in this commit; the
full-width padding itself lands in items 4 and 5.

**Files:** internal/tui/theme.go, internal/tui/toolbranch.go,
docs/adr/0052-diff-bodies-render-as-split-diffs-fed-by-tool-recorded-edit-regions.md,
plus the test files whose expectations pin the old styling (implementer enumerates via
the failing tests)

**Tests:** a unit test that `detailStyle` for diff kinds carries the background in both
states and the state's plain tone as foreground.

**Acceptance:** `go build ./... && go test ./internal/tui`

**Commit:** `feat(tui): diff lines carry a background tint with plain-tone text`

## 4. Full-width tint in the stacked and flat frames — ✅ DONE (2026-08-19)

NOTES (2026-08-19): `gutteredWrap` is in `internal/tui/toolblock.go`, not in wrap.go as the item's text has
it; it was edited there and takes the shared rule from the wrap.go helper (`renderToRail`). No move — the
plan's own sequencing note asks for the anchor to be the function name.
NOTES (2026-08-19): the hanging rails (`hangingWrap`, `clipWrap`) split their prefix off exactly as
`gutteredWrap` does — `renderHangingRow` renders the marker / blank indent with the band's background
cleared and bands only the text, to the rail left of the prefix. Without the split the ┝/┕ branch glyph
and the hanging indent were painted inside the band, which ratified call 3 rules out.
NOTES (2026-08-19): no golden expectation needed updating. `renderPlain` (transcript_test.go) strips ANSI
AND trims trailing spaces, so every stacked/flat plain golden is blind to the pad; the two styled
expectations that pinned the old un-split, un-filled band — `TestRenderDiffDetailStandalone` and
`TestDiffLinesKeepTheirColourInBothBlockStates` — were rewritten to the full-width contract.
NOTES (2026-08-19): `layout.md` was touched beyond the item's Files list — its "A change is coloured the one
way" paragraph described the band without stating its width or naming the chrome it stops at, which the
full-width rule makes an omission rather than a description. Four sentences, prose only.
NOTES (2026-08-19): in the stacked frame the line NUMBER rides inside the band, not beside it: it is
composed into `detailLine.Text` by `stackedRow.line` (toolpresent.go) long before any wrap rail sees the
row, so no change in wrap.go can hold it out. See the DEFER line.

**Source:** ratified calls 2, 3, 6. Depends on item 3.

**What:** The wrap rails in wrap.go (`gutteredWrap`, `hangingWrap`, `clipWrap`) pad each
produced line to their rail width INSIDE the style whenever the style carries a
background — measured through `theme.measure` (ADR 0030), so escapes cost nothing and
wide glyphs count correctly. Styles without a background render exactly as today
(byte-identical — the suite's untouched goldens prove it). Gutter columns and
continuation-row leading spaces stay outside the styled region (chrome). All six
`detailStyle` call sites inherit the rule with zero call-site changes. Update the
stacked/flat golden expectations to the tinted full-width bands.

**Files:** internal/tui/wrap.go, plus the golden/test files for the stacked and flat
diff frames (implementer enumerates via the failing tests)

**Tests:** unit test on a wrap rail: a background-carrying style pads to the rail inside
the style; a plain style's output is unchanged; a wide-glyph line pads correctly.

**Acceptance:** `go build ./... && go test ./internal/tui`

**Commit:** `feat(tui): tint stacked diff rows to the full wrap width`

## 5. Full-width tint in the split view — ✅ DONE (2026-08-19)

NOTES (2026-08-19): the item's Files list names splitdiff.go and splitdiff_test.go; two prose files were
touched beyond it because this change makes a sentence in each incomplete. `docs/layout/split-diff-layout.md`'s
**Panes** bullet named the band but not the columns it covers, and `layout.md`'s "A change is coloured the one
way" paragraph sent the band to "the block's own wrap rail", which is not where it stops in the split reading —
one clause each, prose only.
NOTES (2026-08-19): the item's third requested test ("the divider column lands exactly where it does today")
landed as two added assertions inside the existing `TestSplitDiffRowsStayAlignedWhenEitherSideWraps` rather than
as a new test: that test already pins the divider column at every row of a body that wraps on both sides, so a
second one would have restated it. What was genuinely new — every row now ends either at the divider or at the
full composed width, because a filled right pane squares up too — was added there.
NOTES (2026-08-19): the item's requested short-line and continuation-row tests landed as
`TestSplitCellPaintBandsAShortLineToThePaneEdge` and `TestSplitCellPaintKeepsContinuationGuttersChrome`, both
against `splitCell.paint` directly. `TestSplitDiffRowsColourTheMarkerWithItsLine` keeps its name and its subject
(the marker inside the band, the number outside it) with its expectation widened to the filled band.

**Source:** ratified calls 2, 3. Depends on item 3.

**What:** In splitdiff.go, `splitCell.paint` pads each painted row to the pane's code
width inside the style — the band runs from the marker column to the pane edge on first
and continuation rows alike; the number gutter (and continuation rows' gutter-width
spaces) stays chrome, outside the style. Fold the pane-width padding `splitPad`
currently does for filled cells into `paint` (pad cells keep their current untinted
fill); keep every measurement on `theme.measure`. Rewrite `paint`'s comment — "the
marker travels with the TEXT's colour" becomes the band rationale per the ADR 0052
amendment. Update splitdiff golden expectations.

**Files:** internal/tui/splitdiff.go, internal/tui/splitdiff_test.go

**Tests:** unit tests: a short line's band reaches the pane edge; a wrapped line's
continuation row is tinted but its gutter spaces are not; the divider column lands
exactly where it does today (width unchanged).

**Acceptance:** `go build ./... && go test ./internal/tui`

**Commit:** `feat(tui): tint split-diff panes edge to edge`

---

## Suggested version bump

No item changes a version identifier. When this lands, a **minor** bump is suggested —
a visible rendering change plus two new scheme roles (a user-facing config vocabulary
addition). Whether and when to bump is the user's call.
