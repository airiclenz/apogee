# Plan: TUI polish — breadcrumb field, dropped-gauge fill, umbrella over same-type runs

**Goal:** close the three open TUI entries in `IDEAS.md` and `ISSUES.md`: the run-view breadcrumb takes the prompt box's black field and gains a blank row beneath it; the status line stays one unbroken black band when the context gauge is dropped for width; and any run of 2+ same-type tool calls folds under the `✦ Tools (N calls)` umbrella exactly as a mixed batch does, retiring the `✦ Terminal (N)` shape.
**Date:** 2026-09-02
**Status:** unexecuted
**Sized for:** ~200k-context host
**Base commit:** a76a7a4f (every line number below was read at this commit; the symbol name is the locator, never the number)

**Sources:**
- `IDEAS.md` (breadcrumb entry, tool-call display entry); `ISSUES.md:30` (gauge entry)
- `layout.md` "Run view" → "The breadcrumb is the header" (~:1087–1101); "The status line's right slot" (~:1303–1320, "The black field runs past it to the edge regardless"); "The rules behind the tool-call sketch" (~:576–600)
- `docs/layout/tool-layout.md` (canon for tool blocks): Rules (:35), Vocabulary (:53), the grouped sketches (:160–220)
- `internal/tui/doc.go` ~:628–652, ~:948 (grouping and umbrella ownership prose)

**Ratified design calls** (owner, 2026-09-02, via AskUserQuestion unless marked writer):
- **Single-run umbrella keeps the row count:** `✦ Tools (4 calls)` / `┕ Terminal (4) ⋯ exit 0 ▶` — one type-row shape everywhere; `typeRowText` is unchanged.
- **A lone groupable call stays standalone:** `✦ Terminal` / `┕ <target> ⋯ <outcome>`; only runs of 2+ fold.
- **Sub-agent groups are untouched:** `✦ Sub-Agent (N)` keeps its own renderer; sub-agent calls stay `solo` and never enter an umbrella.
- **Breadcrumb style (writer):** a theme field of its own, `breadcrumb` = `Foreground(userText).Background(surface)` — the header is not a prompt block and stops borrowing `userBlock`.
- **Breadcrumb spacer (writer):** the blank row is the second line of the sticky header (`header.count == 2`), unpainted like the frame's gap row above the bottom block; it scrolls with nothing and carries no click target.
- **Dropped-gauge fill (writer):** the width-starved branch of `statusLine` squares the left slot on the `statusBar` field with the existing `squareOnField` helper — never a bare space pad.
- **Aggregate on a type row (owner, 2026-09-02, via AskUserQuestion; rule order settled at regression check round 2):** `runAggregate` (`internal/tui/toolview.go:984`) evaluates failures ⇒ `N errors`, then the sum where `sumStats` succeeds (`1 line`+`1 line` ⇒ `2 lines`, t18's two `2 lines` reads ⇒ `4 lines`), then identical summary text shown once where the stats do NOT sum (`exit 0`×4 ⇒ `exit 0`), else blank (`PASS`,`FAIL` ⇒ blank). The existing case `TestRunAggregate/"a stat with no arithmetic is blank"` (`internal/tui/toolpresent_test.go:2325`, `exit 0`×2) is rewritten to want `exit 0`; the `PASS`/`FAIL` case (:2327) stays blank as the differing-text example; `"two singulars still make a plural"` (:2306) stays `2 lines`.

**Regression check (2026-09-02, a76a7a4f):**
- 1: guard folded — t17/t18 goldens owned by the item and re-recorded if the paint moves (the "stays green unchanged" claim is dropped); `appendJoined` skips its separator while only the header's rows are laid down so the at-rest paint keeps one blank row; `TestRunViewHeaderIsDrawnByTheStickyOverlay` expects `(0, 2)`.
- 3: recast — `runAggregate` gains the identical-summary rule (ratified above; "aggregates" struck from Out of scope); t18 golden and `✦ Ask User (2)` guards folded (grep rule widened to every tool label); the member-paint rewrite rule and the `transcript_test.go` / `mouse_test.go` flips folded.
- 4: guard folded — `renderToolBlock` narrowed to one view and `paint_test.go:864` re-pointed; `shapeToolRun` locator corrected (`paintcache.go:68`, no switch arm); prose grep extended to `same tool type|\(same type\)` and the Vocabulary "group" entry rewritten.
- Round 2 (2026-09-02, a76a7a4f):
- 3: recast — aggregate rule order settled as failures → sum → identical text → blank (the ratified call above amended; `"a stat with no arithmetic is blank"` rewritten to want `exit 0`, `PASS`/`FAIL` stays blank, `"two singulars still make a plural"` stays `2 lines`); guard (d) restated as the grep rule alone (closed test list dropped), `internal/tui/runview_test.go` added to Files, `TestRunViewEscGoesOneLevelUp`, `TestRenderMarksTheWholeBlock/"a body-less group is no target at all"`, `TestBlockCursorScrollsTheViewToFollowIt`, `TestSplitDiffPaintsUnderAnOpenMembersGutter` named as flips; sub-agent test locators corrected (`TestTranscriptSubAgentGroupNeverJoinsAnUmbrella` = `transcript_test.go:2738`, `subagentblock_test.go:1050–1073` = `TestUnframedSubAgentNeverFoldsIntoASuperGroup`).

**Standing requirements:**
- skills: coding-standards
- Any authorized deviation from item text lands as a dated NOTES line under the item.
- Fixture strings in tests are the exact rendered rows (after `strip`), never a paraphrase.

**Out of scope:**
- The other `ISSUES.md` entries (tok/s values, routing log noise); the `[P]` lint/vulncheck entry (plan `2026-09-01 - 04`).
- Any change to member rows, fold interaction, or the `/inspect` and headless renderers (none read the group kind).
- Version bump (see closing note).

## 1. Breadcrumb on the surface field, with a blank row beneath — ✅ DONE (2026-09-02)

NOTES (2026-09-02): the two goldens `cmd/apogee/testdata/frames/t17-run-view.txt` and `t18-run-view-finished.txt` needed no re-record — with `appendJoined`'s separator skipped over the header's own rows the at-rest paint is byte-identical, which the item's Tests names as the expected pass. They are therefore not in FILES.
NOTES (2026-09-02): `IDEAS.md` is gitignored (`.gitignore:12`) and untracked, so its breadcrumb entry was removed in the working tree but is deliberately left OUT of FILES — `git add IDEAS.md` would need `-f` and the file is never committed.
NOTES (2026-09-02): the item names `TestRootedPaintShowsOneRunAndNothingElse` as accounting for the blank line; it asserts by `strings.Contains` over the whole paint, so it accounts for the row unchanged and was left alone — the blank row's position and ownership are pinned in `TestRootedPaintRegistersNoUserBlock` instead.
NOTES (2026-09-02): the `layout.md` run-view sketch already drew a blank row under the crumb (it was the rail separator), so its rows are unchanged — only the row's ownership and the header's field moved, neither of which ASCII can show; both are stated in the amended "The breadcrumb is the header" paragraph.
NOTES (2026-09-02): consequential edit — internal/tui/model.go: made necessary by the header growing to two rows (the `Model.header` doc comment called it "the breadcrumb row", singular).

**What:** In `internal/tui/theme.go` add the style field `breadcrumb` (declared beside `userBlock`, built as `lipgloss.NewStyle().Foreground(userText).Background(surface)`, doc comment naming its role: the run view's sticky header on the same black field the prompt box and status line share). `breadcrumbRow` (`internal/tui/subagentblock.go:205`) renders every segment — trail, gap, hint, squared tail — through `th.breadcrumb` instead of `th.userBlock`; the row's width and order rules do not change. In `internal/tui/render.go` (~:234–237, the rooted-paint header) append a second, empty line after the breadcrumb and register `header = userBlock{start: 0, count: 2}`; the spacer line gets `targetNone`, and the breadcrumb's `targetBreadcrumb` target stays on line 0 only. `applyStickyHeader` and the height math (`frameFixedRows`, `transcriptBudget`) are untouched — the spacer is a transcript line, not a frame row. Amend `layout.md` "The breadcrumb is the header" paragraph and the run-view sketch above it: the header is painted on the `surface` role (not the prompt block's gray) and is two rows, the second blank and unpainted. Remove the breadcrumb entry from `IDEAS.md` (spelled "bread-crumb" there — the removal step greps for that spelling, not "breadcrumb").

**Regression guard.** `TestE2ESubAgentView` compares `t17-run-view` / `t18-run-view-finished` byte for byte (`cmd/apogee/e2e_subagent_view_test.go:140`, `:201`; `internal/tuitest/golden.go:109–130`) and the added row shifts every row below the crumb: re-record both goldens with `go test ./cmd/apogee -run TestE2ESubAgentView -update` — the test file itself is not edited. `appendJoined` (`render.go:186–188`) lays `railJoin` → `railSpacer(th, 0) == ""` before the first block whenever `len(lines) > 0`, which would give the at-rest view TWO blank rows: skip the separator while only the header's rows are laid down (`len(lines) > header.count`) so the paint stays crumb, one blank row, `❯ task` and only the blank row's stickiness changes. `TestRunViewHeaderIsDrawnByTheStickyOverlay` (`render_test.go:606–608`) pins `stickyHeaderSpan() == (0, 1)`: it now expects `(0, 2)` and that row 1 of `applyStickyHeader`'s output is the blank spacer (`strip(...) == ""`).

**Files:** `internal/tui/theme.go`, `internal/tui/theme_test.go`, `internal/tui/subagentblock.go`, `internal/tui/subagentblock_test.go`, `internal/tui/render.go`, `internal/tui/render_test.go`, `cmd/apogee/testdata/frames/t17-run-view.txt`, `cmd/apogee/testdata/frames/t18-run-view-finished.txt`, `layout.md`, `IDEAS.md`

**Tests:**
- `theme_test.go` role table: `breadcrumb` fg = `s.UserText`, bg = `s.Surface` (both custom-scheme and default-scheme tables).
- `subagentblock_test.go`: new `TestBreadcrumbRowIsPaintedEdgeToEdgeOnTheSurfaceField` — render at widths 60 and 24 (with and without the hint), assert `ansi.StringWidth == width` and that every cell carries the surface background (reuse `firstCellWithoutBackground` from `popup_test.go:97`; it must report no bare cell), and that no cell carries the `chrome` background.
- `render_test.go`: `TestRootedPaintRegistersNoUserBlock` now expects `header == userBlock{start: 0, count: 2}`, `strip(lines[1]) == ""`, `targets[1].kind == targetNone`, and that exactly one blank row separates the breadcrumb from `❯ task` (no separator row after the spacer); `TestRootedPaintShowsOneRunAndNothingElse` accounts for the blank line; `TestRunViewHeaderIsDrawnByTheStickyOverlay` expects `stickyHeaderSpan() == (0, 2)` and `strip(row 1) == ""` after `applyStickyHeader`.
- `cmd/apogee/e2e_subagent_view_test.go::TestE2ESubAgentView`: run it plain first; with the `appendJoined` separator skipped the at-rest paint (crumb, one blank row, task) is expected byte-identical and an unchanged golden is the pass — if it is red, re-record t17/t18 with `-update` and confirm the diff is confined to the rows under the crumb; the test file is not edited.

**Acceptance:**
```
go build ./... && go vet ./internal/tui/
go test ./internal/tui/ -run 'Breadcrumb|RootedPaint|Theme|SubAgent'
go test ./cmd/apogee/ -run 'TestE2ESubAgentView'
```

**Commit:** `feat(tui): paint the run-view breadcrumb on the surface field with a blank row beneath`

## 2. Status line: dropped gauge leaves the black field unbroken — ✅ DONE (2026-09-02)

NOTES (2026-09-02): the item's test names widths "3, 10, 12 and the largest width at which contextGauge() is still dropped"; the largest such width is found by a scan helper (widestDroppedGauge) rather than by recomputing the slot arithmetic, so it tracks the real composition. The gauge-absence assertion uses a gaugeMarks glyph set ("%", the full block and every gaugeEighths partial) because at low context fill the bar carries no full block at all.
NOTES (2026-09-02): squareOnField truncates when the content is over width, so the truncated-left step is folded into the one call (squareOnField(m.th.measure, m.th.statusBar, left, max(0, m.width))) rather than truncating first and squaring after — same result, one pass.
NOTES (2026-09-02): bite check confirmed against the pre-item tree — TestStatusLineDroppedRightSlotKeepsTheField failed at widths 3 and 39, and both tightened tests (TestStatusLineIndentFitsNarrowWindow, TestStatusLineQuietSuffixGivesWayFirst) failed too.

**What:** fix — `ISSUES.md:30`: when the right slot does not fit, `statusLine` (`internal/tui/model.go:3214–3217`) returns `Truncate(left, m.width)` bare, so columns `width(left)..m.width-1` are padded later by `joinFrame`/`squareLine` with unstyled spaces and the band breaks where the gauge would sit — contradicting `layout.md` "The black field runs past it to the edge regardless". Replace the early return with `squareOnField(m.th.measure, m.th.statusBar, <truncated left>, m.width)` (`internal/tui/boxdraw.go:37`), so the row is exactly `m.width` cells, all on the `statusBar` field, in every width. The with-gauge branch is untouched. Remove the entry from `ISSUES.md`.

**Files:** `internal/tui/model.go`, `internal/tui/model_test.go`, `ISSUES.md`

**Tests:**
- `model_test.go`: new `TestStatusLineDroppedRightSlotKeepsTheField` — a model with a lit gauge (as `TestStatusLineGaugeEndsShortOfEdge` builds one) at widths 3, 10, 12 and the largest width at which `contextGauge()` is still dropped: assert `ansi.StringWidth(line) == m.width`, the gauge glyphs are absent, and `firstCellWithoutBackground(line)` (`popup_test.go:97`) reports no bare cell. This test must fail against the pre-item tree (bite check).
- `TestStatusLineIndentFitsNarrowWindow` (`:4347`) and `TestStatusLineQuietSuffixGivesWayFirst` (`:4292`) tighten from `got > width` to `got == m.width` for every width ≥ 1 (width 0 stays a `<= 0` check).

**Acceptance:**
```
go build ./... && go vet ./internal/tui/
go test ./internal/tui/ -run 'StatusLine|Paint'
```

**Commit:** `fix(tui): keep the status line's black field to the edge when the gauge is dropped`

## 3. Fold a same-type run of 2+ calls under the `✦ Tools (N calls)` umbrella

**What:** Recast at the regression check (2026-09-02). `toolSuperGroup` (`internal/tui/transcript.go:1580`) returns an umbrella whenever the runs it collects hold 2+ calls in total — one run of ≥2 same-label calls, or 2+ runs — instead of requiring 2+ runs; a single groupable call still yields nil (lone call stays standalone). Because sub-agent calls are `solo` (`toolview.go:760`) and never groupable, `✦ Sub-Agent (N)` is unaffected. Consequences the tests pin: a 3-call Terminal run now renders `✦ Tools (3 calls)` over one row `┕ Terminal (3) ⋯ <aggregate> ▶` (the aggregate follows the ratified "Aggregate on a type row" rule — `runAggregate`, `internal/tui/toolview.go:984`, gains it in this item); the umbrella forms live the moment the second groupable call is placed (same label or not) — update the formation comment (`transcript.go:~1575–1579`) and the `resolveBlock` comment (`render.go:~644–650`); a member of a former same-type group is reached by opening its type row first (two clicks, as any umbrella member). Rewrite every test whose fixture asserts a `✦ <Label> (N)` header for a groupable run driven through the transcript/`render` path to the umbrella shape (rule: grep `'✦ [A-Za-z ]+ \([0-9]+\)'` in `internal/tui/*_test.go` excluding `Sub-Agent` — every tool label, `✦ Ask User (2)` included; 15 sites at base across `blocktarget_test.go`, `toolbranch_test.go`, `toolblock_test.go`, `toolshape_test.go`, `transcriptbridge_test.go`); a test that calls `renderToolGroup` directly is left standing for item 4 to delete. `shapeToolRun`/`renderToolGroup` themselves stay in place this item (dead for groupable runs; item 4 removes them) so the tree is green at this commit. Sub-agent grouping tests (`TestTranscriptSubAgentGroupNeverJoinsAnUmbrella`, `transcript_test.go:2738`; `TestUnframedSubAgentNeverFoldsIntoASuperGroup`, `subagentblock_test.go:1050–1073`) must pass unchanged.

**Regression guard.** (a) **Aggregate rule order (owner, 2026-09-02, via AskUserQuestion):** `runAggregate` (`internal/tui/toolview.go:984`) evaluates failures ⇒ `N errors`, then the sum where `sumStats` succeeds (`1 line`+`1 line` ⇒ `2 lines`, t18's two `2 lines` reads ⇒ `4 lines`), then identical summary text shown once where the stats do NOT sum (`exit 0`×4 ⇒ `exit 0`), else blank (`PASS`,`FAIL` ⇒ blank); a failure still wins (`exit 0`, `exit 1` ⇒ `1 error`). The existing case `TestRunAggregate/"a stat with no arithmetic is blank"` (`internal/tui/toolpresent_test.go:2325`, `exit 0`×2) is rewritten to want `exit 0`; the `PASS`/`FAIL` case (:2327) stays blank as the differing-text example; `"two singulars still make a plural"` (:2306) stays `2 lines` — the aggregate test file (the `aggregated` helper, `toolpresent_test.go:2272`) gets a case per outcome; the fixture `groupMemberLine("  ┕ Terminal (3) ⋯ exit 0")` and the header example `┕ Terminal (4) ⋯ exit 0 ▶` therefore stand as written. (b) `cmd/apogee/e2e_subagent_view_test.go:201` pins `cmd/apogee/testdata/frames/t18-run-view-finished.txt:5` (`✦ Read (2)` + two member rows inside a run view): re-record it with `go test ./cmd/apogee/ -run TestE2ESubAgentView -update` (`internal/tuitest/golden.go:23`) and run `go test ./cmd/apogee/` in Acceptance. (c) The fixture grep covers every tool label — `✦ Ask User (2)` at `toolbranch_test.go:784`, `transcriptbridge_test.go:481` and `:519` are rewritten too. (d) Members paint only under an OPEN type row (`toolblock.go:209`), so the rewrite scope is the rule alone — every test building 2+ adjacent same-label groupable calls at one depth, found by `grep -nE 'readCall\(|askUserCall\(|Tool: "terminal"|single_find_and_replace' internal/tui/*_test.go` — no closed list of test names: each such test opens the type row (`setTypeExpanded(head, true)` or a click on it) before asserting members, or uses unfoldable entries. Flips the rule covers include `TestRenderMarksTheWholeBlock/"a body-less group is no target at all"` (`blocktarget_test.go:145`, the type row is now `targetType`), `TestBlockCursorScrollsTheViewToFollowIt` (`blockcursor_test.go:134`), `TestSplitDiffPaintsUnderAnOpenMembersGutter` (`toolblock_test.go:460`) and `TestRunViewEscGoesOneLevelUp` (`runview_test.go:380` — its tall fixture opens the type row or uses unfoldable entries). (e) `transcript_test.go:2496` case "one run alone is the same-label group, not an umbrella" flips to `want: superGroup{{at: 0, n: 2}}, calls: 2`; `mouse_test.go:1713` subtest "the siblings and the header stay put" is rewritten: an umbrella with an open child marks its header `targetUmbrella` and a click there closes all (`toolblock.go:224–227`, `mouse.go:687`) — expect that, or assert `targetNone` only while nothing is open.

**Files:** `internal/tui/transcript.go`, `internal/tui/render.go`, `internal/tui/toolview.go`, `internal/tui/transcript_test.go`, `internal/tui/toolpresent_test.go`, `internal/tui/toolblock_test.go`, `internal/tui/toolshape_test.go`, `internal/tui/toolbranch_test.go`, `internal/tui/blocktarget_test.go`, `internal/tui/mouse_test.go`, `internal/tui/transcriptbridge_test.go`, `internal/tui/paintcache_test.go`, `internal/tui/blockcursor_test.go`, `internal/tui/runview_test.go`, `cmd/apogee/e2e_subagent_view_test.go`, `cmd/apogee/testdata/frames/t18-run-view-finished.txt`

**Tests:**
- `transcript_test.go`: `TestTranscriptSuperGroupFormation` gains cases "a same-type run of 2 forms an umbrella", "a lone call does not", "a sub-agent run of 2 does not"; `TestTranscriptSuperGroupFormsLiveAndGrows` asserts formation at the second same-label call.
- Paint fixtures (rows are the exact `strip` output): `TestRenderGroupsBodyCarryingCalls` → `"✦ Tools (3 calls)"`, `groupMemberLine("  ┕ Terminal (3) ⋯ exit 0")`; `TestRenderGroupsOneLineOutputCalls` (`toolshape_test.go:39`) → `"✦ Tools (2 calls)"`, one `Terminal (2)` row whose aggregate is blank (two differing one-line outputs do not sum); the remaining 13 grep sites likewise (`✦ Ask User (2)` at `toolbranch_test.go:784`, `transcriptbridge_test.go:481`, `:519` included), each keeping what it tested (count tone, clip, expanded member shape) one level down under an opened type row.
- Tests with no `✦ Label (N)` fixture but 2+ adjacent same-label calls (guard (d) grep rule, no closed list): open the type row first (or use unfoldable entries), then assert what they asserted — `TestPaintCacheCoversEveryGroupMemberState` still sees opened ≠ collapsed, `TestBlockCursorEntersAtTheEndItsKeyPointsAwayFrom` still finds its stops, `TestBlockCursorScrollsTheViewToFollowIt` still fits its stops, `TestRenderMarksTheWholeBlock/"a body-less group is no target at all"` expects `targetType` on the type row, `TestSplitDiffPaintsUnderAnOpenMembersGutter` and `TestRunViewEscGoesOneLevelUp` (`runview_test.go`) keep their assertions one level down.
- `toolpresent_test.go` (the `aggregated` helper): a case per aggregate outcome in rule order — `exit 0` + `exit 1` ⇒ `1 error`; summing stats sum first (`"two singulars still make a plural"` stays `2 lines`; two `2 lines` ⇒ `4 lines`); identical non-summing text shows once (`"a stat with no arithmetic is blank"` rewritten to want `exit 0`; `exit 0` ×4 ⇒ `exit 0`); differing non-summing text stays blank (`PASS`/`FAIL` case unchanged; `exit 0` + `exit 0 · 1.2s` ⇒ blank).
- `transcript_test.go:2496`: the two-read case wants `superGroup{{at: 0, n: 2}}, calls: 2`.
- `mouse_test.go::TestGroupMemberClickTogglesOnlyThatMember`: first click on the type row, then the member; assert only that member opens; the "the siblings and the header stay put" subtest expects `targetUmbrella` on the header while a member is open and that a click there closes all.
- `cmd/apogee/e2e_subagent_view_test.go::TestE2ESubAgentView`: re-record `t18-run-view-finished` with `-update`; the diff is the `✦ Read (2)` block becoming `✦ Tools (2 calls)` over a collapsed `┕ Read (2)` row and nothing else.

**Acceptance:**
```
go build ./... && go vet ./internal/tui/
go test ./internal/tui/
go test ./cmd/apogee/
```

**Commit:** `feat(tui): fold a same-type run of tool calls under the Tools umbrella`

## 4. Retire the same-type group shape and update the canon

**What:** Depends on item 3. Delete the now-unreachable same-label group shape: the `len(run) > 1` branch in `resolveBlock` (`render.go:~686–698`), `shapeToolRun` (`paintcache.go:68`; no switch arm exists — its only other reference is the `shape: shapeToolRun` assignment inside the deleted `resolveBlock` branch, `render.go:689`), `renderToolGroup` (`toolblock.go:119`) and the `len(views) > 1` hand-off in `renderToolBlock` (`toolblock.go:75`); keep `renderGroupMember`, `memberGutter` and `branchMarker` (the umbrella's opened type rows use them). Delete the tests that called `renderToolGroup` directly (rule: grep `renderToolGroup\(` in `internal/tui/*_test.go`). Prose rule: every comment or doc line that describes a same-type run folding into `✦ <Label> (N)` or names the mixed-type condition for an umbrella is rewritten — grep `'same-label|same-type|same tool type|\(same type\)|different tool|different-label|shapeToolRun|renderToolGroup'` across `internal/tui/*.go`, `internal/tui/doc.go`, `layout.md`, `docs/layout/tool-layout.md`. In `docs/layout/tool-layout.md`: Vocabulary "group" (`:55`, "2+ consecutive calls of the same tool type" — the retired shape's own definition) is rewritten as the umbrella's type row or folded into "super-group"; "super-group" → "2+ groupable calls, one same-type run or adjacent runs of different types"; the "forms live" sentence → "the moment the second groupable call is placed"; delete the two "(same type)" sketches (:160–180) and add a single-run sketch under the collapsed super-group one. In `layout.md` "The rules behind the tool-call sketch": drop "mixed-type" from the super-group sentence; in "The label." the grouped-block count `(N)` now belongs to a type row, the header count reads `(N calls)`. Remove the tool-call display entry from `IDEAS.md`.

**Regression guard.** With the `len(views) > 1` hand-off gone, `renderToolBlock`'s loop (`toolblock.go:94`) would paint a countless `✦ Read` header over two branch rows — a shape no canon names — and `TestPaintedTabBearingToolTargetKeepsItsColumn` (`paint_test.go:864`) calls it with TWO views: narrow `renderToolBlock` to one view (a single `toolView` parameter or a `len(views) == 1` guard) and re-point that test at a single-view row or a two-run umbrella. The deletion rule (grep `renderToolGroup\(` in tests) matches nothing at base — no test calls it directly, so no test is deleted by it. `shapeToolRun` is `paintcache.go:68` (`:70` is `shapeToolSuper`) with no switch arm; the prose grep must also catch `docs/layout/tool-layout.md:55` ("same tool type") and the "(same type)" sketch headings.

**Files:** `internal/tui/render.go`, `internal/tui/paintcache.go`, `internal/tui/toolblock.go`, `internal/tui/toolblock_test.go`, `internal/tui/paint_test.go`, `internal/tui/doc.go`, `layout.md`, `docs/layout/tool-layout.md`, `IDEAS.md`

**Tests:**
- No new behaviour; `go test ./internal/tui/` green. `TestPaintedTabBearingToolTargetKeepsItsColumn` (`paint_test.go:864`) drives `renderToolBlock` with one view (or a two-run umbrella through `renderSuperGroup`) and keeps its column assertion.
- `grep -rn 'shapeToolRun\|renderToolGroup' internal/ layout.md docs/layout/` returns nothing.
- `grep -n '(same type)\|same tool type' docs/layout/tool-layout.md` returns nothing.

**Acceptance:**
```
go build ./... && go vet ./internal/tui/
go test ./internal/tui/
! grep -rn 'shapeToolRun\|renderToolGroup' internal/ layout.md docs/layout/
```

**Commit:** `refactor(tui): retire the same-type tool group shape; umbrella canon covers single runs`

---

**Suggested version bump:** patch (v0.19.13) — three user-visible TUI fixes/refinements, no config or wire change. The owner decides.
