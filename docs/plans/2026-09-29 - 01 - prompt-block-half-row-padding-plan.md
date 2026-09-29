# Prompt block half-row padding and workflow live star — implementation plan

**Goal:** Every sent prompt block in the transcript (submitted `❯` prompt, delivered `⧖`
interjection, run view's task row) reads as an optically taller dark-gray field: a row of `▄`
above and a row of `▀` below, drawn in the block's `chrome` gray on the terminal's own
background. The half rows stand in for the blank separator rows around the block wherever one
exists, and add a row where none does — the transcript's top, its bottom, under the run view's
header spacer, and the seam between two adjacent padded blocks (e.g. two `⧖` interjections from
one delivery), where the upper block's `▀` stands in for the one separator and the lower block's
`▄` is an added row. A running workflow block's header `✦` blinks like a running sub-agent's.

**Date:** 2026-09-29
**Status:** unexecuted
**sized for:** ~200k-context host
**base:** 7d474950

**Sources:**
- `layout.md` — top section (prompt block, one blank line between blocks)
- `internal/tui/userblock.go` — `renderUserBlock`, `promptMarkerRow`
- `internal/tui/render.go` — `renderView`'s `appendJoined`, `blockPaint`, `renderedTranscript`
- `internal/tui/mouse.go` — `transcriptSelectionText`, `highlightTranscript`
- ADR 0030 (one authority per measurement; paint and marks are one act)

**Ratified design calls** (user, 2026-09-29):
- **Placement:** the half rows stand in for the blank separator above and below a padded block wherever one exists; where none does (transcript top/bottom, under the run-view header, the second pad at a seam between two padded blocks) the pad row is added (user, 2026-09-29; edge cases settled at regression check).
- **Copy:** a drag-copy drops the half rows entirely (clipboard text as before the change); they get no selection shading.
- **No colour:** under a colour profile carrying no colour (`colorprofile.Ascii`, `colorprofile.NoTTY`) no half rows are painted and the blank separators return.
- **Scope:** all three `renderUserBlock` callers are padded; the sticky header's range includes the half rows.
- **Workflow star:** a workflow block's header `✦` blinks while its workflow has not ended (`waiting` on an ask included), on the live star's phase.
- **Idle blink:** dropped — background workflows paint no transcript block and a paused turn's star holds still; no blink clock outside a running turn (user, 2026-09-29).

**Standing requirements:**
- skills: coding-standards
- Tests run narrowed to the named `-run` patterns and one package; never `-race` or `-cover` over a whole package or pattern.

**Out of scope:**
- The input box, popups' selected-row bar (`th.userBlock` reuse in `popup.go`), the breadcrumb band.
- Any change to prompt collapse (`promptCollapsedRows`, see more / see less) behaviour.
- A user setting to toggle the padding.
- Blinking the fan_out card's star, or any static `✦` besides the workflow block's header.
- A blink clock while idle or while a turn is paused.

**Regression check (2026-09-29, 7d474950):**
- 1: recast — pad rows stand in for a separator where one exists and add a row at the top, bottom and under the run-view header; block cursor steps past pad rows; mouse_test helpers and three shape tests updated; rail-closer test dropped.
- 2: recast — a pad row standing in for a dropped separator copies as that separator's text; a pad row item 1 added is skipped; pads storage moves to item 1.
- 3: guard folded.
- 4: guard folded (decision) — supersedes layout.md's collapsed "exactly three rows" and sticky "three-row shape" passages.
- 5: guard folded (decision) — one shared live-star predicate, no new clock; supersedes doc.go:102-106's hasOpenToolCall-only repaint gate.
- 6: dropped by the user (2026-09-29) — item deleted.
- 1 (second pass): guard folded (decision) — the seam between two adjacent padded blocks (upper `▀` stands in, lower `▄` added) named in the goals, with a test; guards folded — four more tests moved to the padded shape, the sticky-row drag retarget moved here from item 2, a short screen sticks the prompt without its pads, `runview_test.go` `cursorStops(` callers, widened comment grep.
- 2 (second pass): guard folded (decision) — the lower `▄` at a padded-block seam is an added pad, skipped on copy; guard folded — pads at either end of the selected span are skipped, the sticky copy test pins its exact text.
- 4 (second pass): guard folded (decision) — layout.md prose states the stand-in/added rule, the adjacent-blocks seam included.

## 1. Paint half-row padding around the prompt block and drop the adjacent separators — ✅ DONE (2026-09-29)

NOTES (2026-09-29): the per-line pad fact is a `pad bool` field on `lineMark` (blocktarget.go) rather than a separate slice on `blockPaint`. That way join/railed/retargeted and the paint cache carry it without being changed; `blockPaint.addPad` sets it. blocktarget.go was not in **Files:**.
NOTES (2026-09-29): `cursorStops`, `surfaceStop` and `blockCursor.clamp` gained a `pads []bool` parameter (guard g), and every call was updated. Short-screen sticky: when `b.count >= viewport height`, `stickyHeaderSpan` trims the pad rows through the new helper `Model.withoutPads`, which mouse_test.go's `promptRow`/`promptBlockLine` also use.
NOTES (2026-09-29): consequential edit — internal/tui/doc.go: made necessary by the half rows standing in for the one blank row between blocks, which the rail-continuity paragraph describes.
NOTES (2026-09-29): tests outside the listed files that pinned prompt-block rows or separators, moved to the padded shape: transcript_test.go (two goldens, the trim test, the preview test), paint_test.go (tab-bearing block row count), subagentblock_test.go (the ▄ row between the breadcrumb and the task is allowed), runview_test.go `TestRunViewEscGoesOneLevelUp` (parks at header-3, so the 3-row sticky prompt no longer covers the clicked header; the "taller view" case drops its trailing prompts from 12 to 6), userblock_test.go `promptRows` now checks and drops the half rows, cmd/apogee e2e: 12 frame goldens re-recorded with -update, `assertFirstBodyRow` skips the ▄ row.
NOTES (2026-09-29): cmd/apogee/e2e_stream_test.go `assertScrollbackIsWhole` now allows up to 3 missing lines in a row instead of 1. A one-line sticky prompt now covers 3 rows (the ratified "sticky range includes the half rows"), so each full-window PgUp now skips 3 answer lines under the overlay instead of 1. That is a visible side effect of the ratified design, not a lost line.

**What:**
Recast at the regression check (2026-09-29).
**Goal:** a padded prompt block's paint opens with one row of `▄` and closes with one row of `▀`,
each exactly the block's width in the theme's width authority, in the `chrome` foreground with no
background; the pad rows stand in for the blank separator wherever one exists (`renderView` emits
no blank separator between a padded block and its neighbours) and add a row where none does — the
transcript's top, its bottom, under the run view's header spacer (a lone prompt paints 3 rows),
and the seam between two adjacent padded blocks (e.g. two `⧖` interjections from one delivery:
the upper block's `▀` stands in for the one separator, the lower block's `▄` is added); the half rows are part of the block's `userBlock` range and carry the block's own click
mark.

**Regression guard.** (a) Pad rows stand in for the blank separator wherever one exists, and add a
row where none does — the transcript's top, its bottom, under the run view's header spacer, and
the lower block's `▄` at the seam between two adjacent padded blocks (the upper `▀` stands in for
the one separator); tests pin a lone prompt (3 rows) and two adjacent padded blocks (row count =
unpadded + 1 at that seam). (b) This item also stores `renderedTranscript.pads` on `Model`
beside `userBlocks`/`lineTargets` (`refreshViewport`), and `surfaceStop`/`cursorStops`
(`blockcursor.go`) step past leading pad rows the way they step past the breadcrumb, so the block
cursor highlights the `❯` row; a BlockCursor test covers a collapsible prompt. (c) `mouse_test.go`
helpers `promptRow` and `promptBlockLine` aim at the block's first non-pad line (their callers
unchanged); update `TestSentBlockAccentsItsSkillTokens`, `TestCollapsedBlockAccentsOnlyWhatItShows`,
`TestStickyHeaderShowsTheCollapsedPromptShape` to the padded shape (count+2). (d) No
`TestRenderViewPaddedPromptKeepsRailCloser` (`closes` is never set on an `entryUser` block); keep
the closer arm of the separator-skip predicate as a guard. Supersedes `layout.md`'s collapsed
"exactly three rows" (:1015-1020) and sticky header "three-row shape" (:1045-1047) — item 4
rewrites them. (e) Every test that reads a prompt's first/last row or counts its marked rows moves
to the padded shape: `TestFollowsTailOfLongStreamedReply` and `TestStickyHeaderHandoffOnScroll`
(model_test.go, `firstViewLine` is now the `▄` — expect the `❯` on view row 1),
`TestRenderMarksTheWholeBlock` (blocktarget_test.go, lines shift +1),
`TestRunViewTaskFoldOpensWhatItAdvertises` (runview_test.go, pads carry `targetTask`: count+2, the
last marked row is `▀`), and `TestTranscriptSelectionOnStickyHeaderRow` (mouse_test.go) retargets
its drag to header row 1 here, not in item 2. (f) When a user block's `count >= ` the viewport
height, `stickyHeaderSpan` (model.go) sticks it without its pad rows, mirroring the trail-only
rule; a short-screen sticky test covers it. (g) If `cursorStops`/`clamp` gain a pads parameter,
update every `cursorStops(` call in `blockcursor_test.go` and `runview_test.go`. (h) The comment
sweep's grep is `grep -n "blank line\|separat\|white-on-dark-gray" internal/tui/*.go` (catches
render.go's `renderView` doc and wrap.go's `railSpacer`/`railJoin`).

**Approach (assumed at the header base):**
- `theme.go`: add glyph constants `glyphPadAbove = "▄"`, `glyphPadBelow = "▀"` beside the other
  glyphs; add a `promptPad lipgloss.Style` (`Foreground(chrome)`, no background) and a
  `padPrompts bool` field set `true` by the theme constructor (item 3 clears it).
- `userblock.go`: `renderUserBlock` wraps its rows (including the see-less trailer) in the two
  half rows when `th.padPrompts`. Fill each row to `width` cells in `th.measure` (a glyph that
  measures 2 cells fills `width/2` glyphs plus one space when odd). Mark the half rows with the
  block's own `kind` (`targetHeader` when collapsible, else `targetNone`), so the whole block
  stays one click surface. `.railed(th, depth)` rails them like every other row.
- `render.go`: `blockPaint` gains a per-line `pad` fact (set by `renderUserBlock`, carried through
  `add`/`join`/`railed`/`retargeted`); `renderedTranscript` gains a parallel `pads []bool` built
  in `appendJoined`'s one loop and remapped by `reserveWidgetCells` exactly as `targets` is.
  `appendJoined` skips the separator when the previous block ended on a pad row or this block
  opens on one — EXCEPT a `railJoin` closer (`closes && prevBlockDepth > depth`), which stays.
- Update every comment that states "one blank line separates every block" or describes the user
  block as full-width rows only: `grep -n "blank line\|separat\|white-on-dark-gray" internal/tui/*.go`.

**Files:** internal/tui/theme.go; internal/tui/userblock.go; internal/tui/render.go; internal/tui/model.go; internal/tui/blockcursor.go; internal/tui/wrap.go; internal/tui/userblock_test.go; internal/tui/render_test.go; internal/tui/mouse_test.go; internal/tui/model_test.go; internal/tui/blockcursor_test.go; internal/tui/blocktarget_test.go; internal/tui/runview_test.go
**Read first:** internal/tui/render.go — renderView appendJoined, blockPaint add/join/railed/retargeted, reserveWidgetCells; internal/tui/userblock.go — renderUserBlock;
internal/tui/blockcursor.go — cursorStops, surfaceStop, blockCursor.clamp; internal/tui/model.go — refreshViewport, stickyHeaderSpan; internal/tui/wrap.go — railJoin, railSpacer;
internal/tui/mouse_test.go — promptRow, TestTranscriptSelectionOnStickyHeaderRow; internal/tui/model_test.go — firstViewLine, TestStickyHeaderHandoffOnScroll

**Tests:**
- `TestUserBlockPaintsHalfRowPadding` — first/last rows are `▄`/`▀` runs of `width` cells, `chrome` fg, no bg; the body rows are unchanged.
- `TestUserBlockPaddingCarriesTheBlockMark` — collapsible block: pad rows `targetHeader`; short block: `targetNone`.
- `TestRenderViewPaddedPromptReplacesSeparators` — answer / prompt / answer transcript has the same line count as with `padPrompts=false`, with no blank row adjacent to the pad rows; `userBlocks[i]` spans the pad rows.
- `TestRenderViewLonePromptPaintsThreeRows` — a transcript holding one prompt paints `▄`, the `❯` row, `▀`: 3 rows.
- `TestRenderViewAdjacentPaddedBlocksAddOneRow` — two adjacent padded blocks (two `⧖` from one delivery): `▀` then `▄` at the seam, row count = unpadded + 1 there.
- `TestStickyHeaderShortScreenDropsPadRows` — a viewport no taller than the prompt block sticks it without its pad rows.
- Update `TestFollowsTailOfLongStreamedReply`, `TestStickyHeaderHandoffOnScroll` (the `❯` on view row 1), `TestRenderMarksTheWholeBlock` (lines +1), `TestRunViewTaskFoldOpensWhatItAdvertises` (count+2, last marked row `▀`); retarget `TestTranscriptSelectionOnStickyHeaderRow`'s drag to header row 1.
- `TestBlockCursorHighlightsPaddedPromptRow` — the cursor landing on a collapsible prompt shades its `❯` row, not the `▄` row.
- Update `TestSentBlockAccentsItsSkillTokens`, `TestCollapsedBlockAccentsOnlyWhatItShows`, `TestStickyHeaderShowsTheCollapsedPromptShape` to the padded shape (count+2); repoint `promptRow`/`promptBlockLine` (mouse_test.go) at the block's first non-pad line.
- Update existing tests pinning prompt-block rows or separators.

**Acceptance:**
- `go build ./internal/tui/`
- `go vet ./internal/tui/`
- `go test -count=1 -run 'UserBlock|RenderView|Prompt|Sticky|Transcript|SentBlock|CollapsedBlock|BlockCursor|FollowsTail|RenderMarksTheWholeBlock|RunViewTask' ./internal/tui/`

**Commit:** `feat(tui): pad sent prompt blocks with half-block rows`

## 2. Drop the pad rows from a transcript copy and its selection shading — ✅ DONE (2026-09-29)

NOTES (2026-09-29): `transcriptSelectionText` takes a new `padRows` value (the pad map `Model.pads` plus `floor`, the end of the run-view header), built by `Model.padRows()` in mouse.go. It replaces a bare `[]bool` because an added `▄` under the run-view header can only be told apart from a stand-in by that header bound. The zero `padRows{}` is the "nil pads" the plan names for fake lines. model.go needed no change, since item 1 already stores `m.pads`.
NOTES (2026-09-29): stand-in vs added is read back from the paint. A `▄` stands in when it sits below the header floor and the row above is not a pad. A `▀` stands in when a row follows it. A `┊` closer next to either one means the pad was added. At depth > 0 the stand-in copies the rail at min(pad depth, neighbour's leading `│ ` count), so a depth-0 neighbour whose text itself starts with `│ ` would copy one rail too deep there (a rare case; depth 0 is unaffected).
NOTES (2026-09-29): added one test the plan did not list, `TestTranscriptCopySkipsPadRowsAtTheSpanEnds`, which pins the span-end trimming (start on `▀`, end on `▄`, `▄`..`▀`, pads only).

**What:**
Recast at the regression check (2026-09-29).
**Goal:** a drag-selection over the transcript that crosses a padded prompt block copies exactly
the text it copied without padding — no `▄`/`▀` rows; a pad standing in for a separator copies as
that separator, an added pad copies as nothing, so the clipboard text is identical to the
unpadded transcript's — and the selection highlight paints nothing on a pad row.

**Regression guard.** A pad row that stands in for a dropped separator copies as that separator's
text (`""` or the stripped rail); a pad row item 1 ADDED (transcript top, under the run-view
header, trailing, and the lower block's `▄` at a seam between two padded blocks — the upper `▀`
copies as the separator) is skipped — so the clipboard text is exactly what it was before the change;
`TestTranscriptCopySkipsPromptPadRows` expects `answer\n\n❯ prompt\n\nanswer`, plus an
adjacent-padded-blocks case (`⧖ a\n\n⧖ b`). Pads at either END of the selected span are skipped;
only interior stand-ins copy as the separator. `Model`'s pads
storage now lives in item 1 (item 2 only consumes it). Update every `transcriptSelectionText(` call
in `mouse_test.go` (nil pads for fake lines; pads read bounds-checked so nil/short copies as
before). `TestTranscriptSelectionOnStickyHeaderRow`'s drag is retargeted to header row 1 by item 1. Supersedes
`mouse.go`'s D4 "copied verbatim" comment (:976-983) and the `contentLineAt` comment (:46-54) —
update both to name the pad-row exception.

**Approach (assumed at the header base):** this item consumes the `Model`'s pads that item 1
stores (`renderedTranscript.pads` beside `m.lines` and `rendered.userBlocks`); `transcriptSelectionText` takes the parallel
flags and skips a flagged row (a selection starting or ending ON a pad row copies from/to the
adjacent content row); `highlightTranscript` leaves a flagged row unshaded. Rows are addressed
through `contentLineAt`/`drawnLineAt`, so the sticky-header overlay keeps copy equal to sight.

Depends on item 1.

**Files:** internal/tui/model.go; internal/tui/mouse.go; internal/tui/mouse_test.go
**Read first:** internal/tui/mouse.go — transcriptSelectionText, highlightTranscript, handleMouseRelease; internal/tui/model.go — drawnLineAt, stickyHeaderSpan;
internal/tui/render.go — renderView appendJoined (rooted header); internal/tui/interject.go — foldInterjected; internal/tui/wrap.go — railJoin, railSpacer;
internal/tui/mouse_test.go — TestTranscriptSelectionText, armTranscriptSelection

**Tests:**
- `TestTranscriptCopySkipsPromptPadRows` — answer → prompt → answer selection copies `answer\n\n❯ prompt\n\nanswer` (no pad glyphs; the stood-in separators copy as before); an adjacent-padded-blocks case (two `⧖` from one delivery) copies `⧖ a\n\n⧖ b` (the seam's lower `▄` skipped).
- `TestTranscriptSelectionLeavesPadRowsUnshaded`.
- `TestTranscriptCopyFromStickyHeaderSkipsPadRows` — selection starting on the overlaid header (its `▄`) pins the exact copied text, starting at the `❯` row with no leading `\n`.

**Acceptance:**
- `go build ./internal/tui/`
- `go test -count=1 -run 'TranscriptCopy|TranscriptSelection|SelectionText' ./internal/tui/`

**Commit:** `feat(tui): leave prompt pad rows out of a transcript copy`

## 3. Suppress the pad rows on a colourless profile — ✅ DONE (2026-09-29)

NOTES (2026-09-29): the new tea.ColorProfileMsg case hands the message on to foldWidgetMsg after setting th.padPrompts, so the widgets still receive it exactly as they did through the default arm; the repaint rides a padPrompts term added to frameKey (settle's compare), and the paint cache is kept apart by a padPrompts field in paintKey (set by blockKey) rather than a clear() in the new arm.

**What:**
**Goal:** when the program's colour profile is `colorprofile.Ascii` or `colorprofile.NoTTY`,
`renderView` paints no pad rows and restores the blank separators; any other profile paints
them; the choice survives a colour-scheme switch and invalidates memoised block paints.

**Regression guard.** No "mark for repaint" exists — the repaint is `settle`'s `frameKey` compare:
add `padPrompts` to `frameKey`/`Model.frameKey()` (or `m.layout()` when `m.ready`, as
`foldModeReport` does) and fix paintcache.go's "nothing else about a theme changes mid-session".
`blockKey` names only `th.measure` and `entryUser`/`entryInterjected` are cacheable, so the cache
step is unconditional: `m.transcript.paints.clear()` in the new arm (or the flag in `paintKey` plus
its "one part of [theme] this key NAMES" comment). Test `p != colorprofile.Ascii && p !=
colorprofile.NoTTY` — `> Ascii` also rejects `colorprofile.Unknown` (0). Supersedes model.go's
"the colour profile has no case of its own" comment (Update, `m.diag.observe`) — update it.

**Approach (assumed at the header base):** `Model.Update` handles `tea.ColorProfileMsg` (today
only `diagnostics.go` records it) by setting `th.padPrompts = profile > colorprofile.Ascii`
(no colour ⇒ false) and marking the transcript for repaint. `applyColorScheme`
(`settingsapply.go`) carries `padPrompts` from the outgoing theme onto the rebuilt one, the way
it carries `measure`. `blockKey` (paint cache) must distinguish the two states — include the flag
in the key if `th` is not already compared by value.

Depends on item 1.

**Files:** internal/tui/model.go; internal/tui/theme.go; internal/tui/settingsapply.go; internal/tui/paintcache.go; internal/tui/colorprofile_test.go
**Read first:** internal/tui/model.go — Update (tea.ModeReportMsg arm, m.diag.observe), settle; internal/tui/paintcache.go — frameKey, Model.frameKey, blockKey, paintKey;
internal/tui/settingsapply.go — applyColorScheme; internal/tui/width.go — foldModeReport

**Tests:**
- `TestAsciiProfileDropsPromptPadding` — `tea.ColorProfileMsg{Profile: colorprofile.Ascii}` → rendered lines carry no `▄`/`▀`, blank separators back.
- `TestTrueColorProfileKeepsPromptPadding`.
- `TestLateAsciiProfileDropsPromptPadding` — Ascii arriving after the first sized frame repaints unpadded with no transcript write.
- `TestColorSchemeSwitchKeepsPadChoice` — Ascii profile, then a scheme switch: still unpadded.
- `TestPaintCacheSeparatesPadStates` — same block, both states, two different paints.

**Acceptance:**
- `go build ./internal/tui/`
- `go test -count=1 -run 'PromptPadding|PadChoice|PadStates' ./internal/tui/`

**Commit:** `feat(tui): paint no prompt pad rows on a colourless terminal`

## 4. Document the padded prompt block in layout.md

**What:**
**Goal:** `layout.md`'s opening prompt-block description states the half-row padding (`▄` above,
`▀` below, in the block's gray, on the terminal's background), that the pad rows stand where the
blank separator would, that a copy leaves them out, and that a colourless terminal gets the flat
block with blank separators; the "exactly one empty line" rule names the prompt seam as its
exception.

**Regression guard.** Explicitly supersede `layout.md`'s "the collapsed shape exactly three rows"
and the sticky header "sticks as its three-row shape" passages — both become the padded five-row
shape. The prose states the same stand-in/added rule as item 1: a pad row stands in for the blank
separator wherever one exists and is added where none does (transcript top/bottom, under the
run-view header, and the lower block's `▄` at the seam between two adjacent padded blocks, whose
upper `▀` stands in for the one separator).

**Approach (assumed at the header base):** edit the first two paragraphs of `layout.md` (the `❯`
and first `✦` sketches) and redraw the `❯` sketch with its pad rows. Any other `layout.md` passage
claiming one blank line around a prompt: `grep -n "empty line\|blank line" layout.md`.

Depends on items 1–3.

**Files:** layout.md
**Read first:** layout.md — opening ❯ paragraph and ✦ answer paragraph, "**Blank lines.**" rule, "**A huge prompt collapses to three rows.**", sticky-header sentence of "Collapsed and expanded blocks",
run view "The header is four rows" paragraph; docs/layout/tool-layout.md — Keyboard block cursor bullet

**Tests:** none (docs only).

**Acceptance:**
- `grep -n "▄" layout.md`
- `grep -n "▀" layout.md`

**Commit:** `docs(layout): describe the prompt block's half-row padding`

## 5. Blink a running workflow block's header star

**What:**
**Goal:** a workflow block whose view is live and has not ended (`workflowView.live()` and
`end == ""`, the waiting-on-an-ask state included) paints its header star through
`blockState.star()` with `live: true`, so it alternates `✦` / space on the frame's blink phase
exactly as a running sub-agent row does; an ended or replayed workflow block paints a steady `✦`;
while a turn runs, a blink-phase flip repaints the transcript whenever a running workflow block is
on it, even with no open tool call.

**Regression guard.** The live-star predicate is ONE transcript function shared by
`foldSpinnerTick` (and `frameKey` if it keys on open calls); no new clock — while a turn is paused
(ask/approval) the star holds still, and layout.md's "the transcript carries no timer of its own"
stays true. Supersedes doc.go:102-106's repaint gate "only while [transcript.hasOpenToolCall]
holds" — update it to name the shared predicate.

**Approach (assumed at the header base):**
- `workflowblock.go`: `renderWorkflowBlock` / `renderWorkflowStages` / `renderWorkflowText` take a
  `blockState` (or the blink phase) and lead the header with `state.star()` instead of
  `glyphAssistant`; add `workflowView.running() bool` (`live() && end == ""`).
- `render.go`: `renderEntryLines`' `entryWorkflow` case passes `blockState{live:
  in.workflowView.running(), blink: blink}`; `resolveBlock`'s single-entry branch sets `live` for
  that kind the same way (the kind is not cacheable, so the key is unaffected).
- `spinner.go` `foldSpinnerTick`: repaint on a phase flip when `hasOpenToolCall()` OR the
  transcript holds a running workflow block — one transcript predicate (e.g.
  `transcript.hasLiveStar()`) that both conditions sit behind.
- `layout.md`, "The live star": name the running workflow block among the headers that blink.

Depends on item 1 (both touch `render.go`).

**Files:** internal/tui/workflowblock.go; internal/tui/render.go; internal/tui/spinner.go; internal/tui/transcript.go; internal/tui/paintcache.go; internal/tui/doc.go; internal/tui/workflowblock_test.go; internal/tui/spinner_test.go; layout.md
**Read first:** internal/tui/workflowblock.go — renderWorkflowBlock, workflowView.live; internal/tui/render.go — resolveBlock (single-entry branch), renderEntryLines (entryWorkflow case);
internal/tui/blockstate.go — blockState.star; internal/tui/spinner.go — foldSpinnerTick; internal/tui/paintcache.go — Model.frameKey; internal/tui/entrykind.go — entryKind.hasLiveStar

**Tests:**
- `TestWorkflowStarBlinksWhileRunning` — running view, blink true → header cell is a space; blink false → `✦`.
- `TestWorkflowStarSteadyOnceEnded` — `end` set (finished / stopped / failed) and a replayed view: `✦` at both phases.
- `TestWorkflowStarBlinksWhileWaitingOnAsk`.
- `TestSpinnerFlipRepaintsForRunningWorkflow` — no open tool call, a running workflow block: a phase-flip tick repaints.

**Acceptance:**
- `go build ./internal/tui/`
- `go test -count=1 -run 'WorkflowStar|SpinnerFlip|Workflow' ./internal/tui/`

**Commit:** `feat(tui): blink a running workflow's header star`
