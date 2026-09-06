# Pop-up breathing rows and click-to-select on every boxed pop-up (bead `apogee-p94`)

**Goal:** every boxed pane painted by `renderPopup` keeps one blank row between what precedes its row block and the block, and one blank row above its hint row — the
eleven hand-edited goldens under `cmd/apogee/testdata/frames/` become held goldens without `-update`. The five wheel-only pop-ups (ask, approval, picker, `/sessions` browser, `/` dropdown) gain a pointer through one shared row hit-test.

**Date:** 2026-09-06 · **Status:** unexecuted · **Base:** `6e895e77` · **sized for:** ~200k-context host

**Sources**
- `docs/handoffs/2026-09-06 - 00 - popup-redesign-plan-handoff.md` (untracked; delta table, calls) · `docs/adr/0053-popup-surfaces-embed-one-list-surface.md` (verdicts, accept-through-filter, `bodyPad*` as painter contract), `0062` (cells, no test hooks), `0011`, `0043`, `0025` D10 · `internal/tui/popup.go` (`popupSpec` :302-321, pads :767-770, `popupPlacement` :404), `mouse.go` (:390-466 click chain, :1362-1392 wheel doctrine, `settingsPaint` :890-927), `layout.md` `## What "height" means`
- `cmd/apogee/testdata/frames/popup-*.txt`, `t10-forced-pane.txt`, `t12-pane-60.txt`, `t16-settings-rows.txt` — THE spec; diff: `go test ./cmd/apogee -run 'TestE2EPopupFrames|TestE2EApprovalForcesALook|TestE2EHostileWrapsUnderItsOwnIndent|TestE2ELiveStateFollowsTheRunningSession' -popup-design -count=1 -v`

**Ratified design calls** (owner, 2026-09-06; 1-4 on the bead's `--design`, A-H via AskUserQuestion, I and J at the regression check)
- **Sequencing / frame scope / click scope / click meaning:** as on `bd show apogee-p94` — one shared hit-test over `popupPlacement` + `frameSpans.pane`; first click highlights (multi-select: toggles), click on the highlighted row acts as `⏎`; never select-and-send in one click.
- **A. Scope:** uniform — `/settings` (t16) and the three report panes (`/usage`, `/inspect`, `/thinking`) breathe too.
- **B. Short window:** the blanks are booked by each builder and are the FIRST lines dropped, as a pair, before any row; `popupChrome` stays 4, `frameRowPlan`'s grant and the twelve-row floor are untouched.
- **C (amended):** on the `/` dropdown an outside click dismisses and continues down the chain; picker and `/sessions` stay dismiss-and-consume. Ask/approval swallow it as before (esc there stops the run / arms the stop gesture). **D. Timing:** no double-click window; the approval pane's 100 ms arming latch gates the activating click exactly as it gates `⏎`; the highlight-moving click is outside the latch like `↑/↓`.
- **E. Tests:** reducer tests in `internal/tui/mouse_test.go` plus one e2e click per pane family through SGR bytes (`\x1b[<0;x;yM` / `…m`, 1-based) over `Driver.Press` — verified to decode to `tea.MouseClickMsg` in-process with no new hook.
- **F. Approval body:** every part (sub-agent line, `Reason:`, `Fix:`, `Scope:`, args block, notes) is a paragraph — one blank row between consecutive parts. **G. Ask with text typed:** a click on a choice row is swallowed while the box holds text (mirrors `askChoiceKey`'s empty-box guard).
- **H. Hints:** unchanged — a hint is a key legend (`/settings` precedent). **I. Frame heights:** the edited frames gained rows without losing any; plan item 0 trims them by rule.
- **J. Click arming (all five panes):** the pointer activates only a row IT highlighted — a click-armed row latched on the Model — so the first click on any pane never activates, whatever the keyboard or the default left highlighted; two clicks always.

**Derived calls** (writer, 2026-09-06)
- The rule rides the EXISTING pad fields — `popupSpec.rowPadAbove` and `popupRowStyle.padBelow`, booked through `popupRowPadLines` — set by every builder (ADR 0053 pins `bodyPad*` the same way: painter's contract, builders set it). No new chrome constant, no painter-side default. `rowPadAbove` is NOT set where the line above the row block is already blank: `/settings` (its `settingsBody` closes with a blank — t16 keeps both) and a list with a typed filter (`bodyPadBelow`). `padBelow` is set only with a hint (approval has none and gains no trailing blank).
- Multi-select ask: a click on a non-highlighted row highlights it AND toggles its box; a click on the highlighted row sends (`submitAnswer`'s existing rules). The dropdown's activating click is the `tab` spelling (`acceptAutocomplete`, unconditional), never `⏎`'s exact-match decline. Click handlers slot into `handleMouseClick` after the three report panes and before `handleFooterModeClick`, in `foldMouseWheel`'s order: browser → picker → prompt (ask/approval) → dropdown. Geometry from `pre`, mutation on the live model, as the doc block at `mouse.go:390-399` demands. No ADR: the breathing rule is `layout.md`'s, the click rule extends the wheel doctrine already in `mouse.go`.

**Standing requirements:** `skills: coding-standards`. Never run `-update` against the eleven design frames before item 6 passes them clean. Any authorized deviation lands as a dated NOTES line under its item.

**Regression check (2026-09-06, `6e895e77`):**
- 0: new item — the design frames are trimmed to their screen height (ratified call I).
- 1: guard folded — the empty-block pad is gated on `len(spec.rows) == 0 && capLines > 0`, the painter reserves the pads out of `capLines`, and `popupRowBlockLines` keeps its `pad int` signature.
- 2, 3: rejected — "the frame can only match after the owner deletes one transcript row from `popup-ask-free.txt`" and "pane-shape acceptance until the owner trims the design files"; item 0 deletes that row and trims `popup-approval`, `t10-forced-pane` and `t12-pane-60` by rule, so both frame acceptances stand.
- 3: guard folded — the adjacency rule is stated with its grep rather than as a closed list of sites; `approval.go:258-266` is superseded by call F.
- 4: guard folded (decision) — the demand and the cap book the pads, the painter owns the window; the frame acceptance holds after item 0.
- 5: guard folded (decision) — same booking rule; `reportpane.go:217` follows the painter's window; `TestReportPaneBreathes` uses an overflowing list.
- 6: guard folded — the flag grep excludes `docs/plans`, and CHANGELOG.md is named as the one place `-popup-design` survives.
- 7: guard folded (decision) — the click chain gains a `tea.Cmd` currency and `listCursor.seat`; `rowAt` keeps its `sub`, and the pane parameter is `framePane`.
- 8: guard folded — the key pins live in `driver_test.go`; the ask test model hands back its reply channel.
- 9: guard folded — the pointer activates only a row it highlighted itself, so no single click ever grants a call.
- 10: guard folded — the schedule e2e clicks through the mode question too, and `acceptBrowser` keeps its Cmd.
- 11: recast (decision) — the dropdown's outside click dismisses AND continues down the chain; the e2e clicks `/confine`.
- 12: guard folded — `-run 'DocMap|Click'`, the `layout.md` grep is scoped to the two sections, and the five doc files the prose rule also names stay read-only.
- **Round 2 (2026-09-06, `6e895e77`):** 7 — call J folded as the shared machinery items 8-11 inherit: one `clickArmed` value on the Model, the open-pane gate inside `popupPaneHit`, one shared assertion for the five `tea.Model` accepts (`model.go` joins **Files:**).
- 8: tests amended — the activating click is the second click on the SAME row, and a click on the default-highlighted first choice highlights and arms only.
- 9: the guard kept, now named as call J, plus the `panePrompt` open gate and the shared assertion.
- 10: both handlers and the outside-click dismiss gated on `panePicker`/`paneBrowser` being open, activation on the second click, and the picker/browser renders gain the placement (`picker.go`, `listsurface.go` join **Files:**).
- 11: all four findings folded — call J, the `paneDropdown` gate, `renderAutocomplete` split so the click reads the placement (`autocomplete.go` joins **Files:**), and in-rect non-row → claimed, no-op.

**Out of scope:** hint wording (H); a double-click window (D); `t04`/`t12-skills`/`t15`/`t17`/`t18` frames (no boxed pane); column hit-testing inside a row; `popupChrome` growth; CONTEXT.md entries (it carries no pop-up geometry terms); the `-popup-design` flag surviving in any form.

## 0. The design frames are trimmed to their screen height — ✅ DONE (2026-09-06)

NOTES (2026-09-06): every frame is now exactly its own screen height — 30 lines for the ten full frames (`popup-*` and `t10`/`t16` at 100×30, 60×30 and 140×30), 24 for `t12-pane-60` (60×24, `narrowHostileSize`). `popup-ask-free` now PASSES `-popup-design` outright; the other seven differ from today's render by exactly the breathing rows the painter has yet to book, and their frames are the same height as the render.
NOTES (2026-09-06): deviation — the item's mechanical rule ("delete as many BLANK filler rows immediately above the pane's top border `╭` as the pane gained; content rows are never deleted") holds for only three of the eight frames. Rather than guess, each frame was trimmed to what the renderer ITSELF produces when the transcript loses the rows the taller pane will take: the same scenario was re-rendered with the terminal shortened by exactly the number of rows the design's pane gains (a faithful stand-in — the row budget the transcript and the pane divide is identical), the resulting frame was captured from the `-popup-design` diff, and the design's own pane rows were spliced back in. The five frames where the rule does not describe what the renderer does: **popup-approval** — the transcript SCROLLS rather than dropping the blank above `╭`, so the top header row goes and two scrollbar cells turn `┃`→`│`; **popup-ask-free** — its pane never grew at all, so the trim is the stray blank transcript row at line 2 plus one scrollbar cell; **popup-dropdown** — its pane sits BELOW the composer rule, so the blank above `╭` is the structural gap and the transcript instead scrolls, clipping the header box's top border, dropping one trailing blank and gaining a scrollbar column on eight rows; **t10-forced-pane** — the transcript has no two blank rows to give, so `✦ That is what the command had to say.` and its blank scroll out and one scrollbar cell changes; **t12-pane-60** — the pane absorbs the growth itself, eliding one more argument row (`│   aaaa…│` goes, `… (+4 more lines)` becomes `… (+5 more lines)`). `popup-picker`, `popup-sessions` and `t16-settings-rows` trimmed exactly as the item describes (two blank rows above `╭`; the last visible settings row `use-default-prompt`) — t16 also loses one scrollbar cell on the `server` row as the thumb shrinks. Every design pane's own content is preserved intact in all eight.
NOTES (2026-09-06): deviation — the item's second acceptance clause ("the `-popup-design` diff for each still shows ONLY the inserted blank rows as `-` lines and no `+` lines") is not satisfiable once the frames are at screen height, by any trim: a golden the same height as the render cannot differ from it by deletions alone, so each design row shows as a `-` and the row it displaces shows as a matching `+`. After the trim the seven still-failing frames diff with equal counts (`popup-picker`/`popup-sessions`/`t12`/`t16` -2/+2, `popup-approval`/`t10` -3/+3, `popup-dropdown` -10/+10) and `popup-ask-free` is clean; the first acceptance clause (`wc -l`) holds exactly.
NOTES (2026-09-06): `internal/tui/popup.go` and `internal/tui/popup_test.go` were already dirty in the working tree when this item ran (another item's work, left untouched). Every probe and every capture used here was taken after those edits were on disk, so the frames are consistent with that tree; item 1's guard fires only on an EMPTY row block, which none of these eleven frames has.

**What.** Test data only. Eight of the eleven design frames are taller than the screen they were recorded at (`e2eSize` 100×30, `cmd/apogee/e2e_support_test.go:33`): `popup-dropdown`, `popup-picker`, `popup-sessions`, `t10-forced-pane` are 32 lines; `popup-approval`, `popup-ask-free`, `t16-settings-rows` are 31; the three untouched ask frames are exactly 30. For each grown pop-up delete as many BLANK filler rows immediately above the pane's top border `╭` as the pane gained (the pane grows upward into the transcript's empty rows; content rows are never deleted). For `t16-settings-rows` the full-height `/settings` pane cannot grow: delete its last visible settings row (`use-default-prompt`) — the row window shrinks by one. `t12-pane-60` (25 lines) is checked against the height the pane crop had before the edit and trimmed the same way only if it grew.

**Files:** `cmd/apogee/testdata/frames/popup-approval.txt`, `popup-ask-free.txt`, `popup-dropdown.txt`, `popup-picker.txt`, `popup-sessions.txt`, `t10-forced-pane.txt`, `t12-pane-60.txt`, `t16-settings-rows.txt`

**Tests.** None (data).

**Acceptance.** `wc -l cmd/apogee/testdata/frames/popup-*.txt cmd/apogee/testdata/frames/t10-forced-pane.txt cmd/apogee/testdata/frames/t16-settings-rows.txt` shows 30 for every full-frame golden; the `-popup-design` diff for each still shows ONLY the inserted blank rows as `-` lines and no `+` lines.

**Commit:** `test(cmd/apogee): the pop-up design frames are trimmed to their screen height`

## 1. The painter keeps the pad above the hint when the row block is empty — ✅ DONE (2026-09-06)

NOTES (2026-09-06): the item's regression guard states "Pad lines never carry a scrollbar cell (popup.go:726-736 paints it on row lines only)" — the code did the opposite, painting a track cell on every block line including the pads, which the reservation change makes visible on any overflowing padded list. Implemented the guard as written and as the design goldens draw it: `popupRowBlock` gained a `trail` counterpart to `lead`, and `popupRowScrollbar` sizes and paints the bar over the ROW slice only.
NOTES (2026-09-06): `popupRowBlockLines` keeps its `pad int` signature as the plan directs, so "counts pad.below for an empty list" is honoured by returning `pad` for empty heights and by a new `popupRowPads(spec, rows)` helper that drops `rowPadAbove` for an empty block and `padBelow` without a hint — one place both the painter and the arithmetic read the two conditions from.
NOTES (2026-09-06): three subtests of `TestRenderPopupRowPadSurroundsTheBlock` changed verdict, as the binding reservation addition requires — an overflowing window now keeps both pads instead of dropping them — and were renamed to say so; a floor case, the empty-block cases and the base spec's hint were added alongside.

**What.** `internal/tui/popup.go`: (a) `popupRowLinesAt` (:742-811) paints `padBelow` for an EMPTY row block when `spec.hint != ""` — today the early return at :762-765 skips both pads, which is why `popup-ask-free` sits flush; (b) `popupRowBlockLines` (:1037-1039) counts `pad.below` for an empty list so booking and painting agree; (c) `padBelow` is painted only when `spec.hint != ""` — a hintless pane never ends on a blank; (d) the `popupSpec` doc block (:187-301) states the house rule in one paragraph: every boxed pane sets `rowPadAbove` unless the line above its row block is already blank, and `rowStyle.padBelow` whenever it has a hint; the give-way at :767-770 (both pads dropped before any row) is the short-window rule. `popupChrome`, `popupTitleBorderChrome`, `popupBorderChrome`, `popupPlacement.rowsAt` unchanged. `rowPadAbove` with an empty block paints nothing.

**Regression guard.** The empty-row-block pad (a) is gated on `len(spec.rows) == 0 && capLines > 0` — the ask-free case — never on `start == end`, so a pane at its four-row floor paints no pad and `TestPaintedSettingsPaneAtItsFourRowFloor` stays green. ADD to item 1's What (binding, settles both reviewers' overflow finding): `popupRowLinesAt` reserves `popupRowPadLines(padAbove, padBelow)` OUT OF `capLines` before windowing the rows — the window is built against `capLines - padLines` — and hands the lines back to the rows (both pads dropped) only when the anchor/selected row cannot seat in what remains; an overflowing list therefore breathes (popup-dropdown and t16 overflow with a scrollbar AND show both blanks — that is the spec) and only a window at the floor loses the pads. Pad lines never carry a scrollbar cell (popup.go:726-736 paints it on row lines only; t16 line 22 and popup-dropdown lines 16/25 have none). `popupRowBlockLines` mirrors the reservation. Two corrections ride with it: `popupRowBlockLines` KEEPS its `pad int` signature (popup.go:1037) — six call sites pass it (approval.go:384, settings.go:1804, ask.go:357-358, model_test.go:1892, popup_test.go:393, :1896) and only ask.go books a below-only pad, through `popupRowPadLines(false, padBelow)`; and (c) reddens `TestRenderPopupRowPadSurroundsTheBlock`, whose base spec sets no hint, so that spec gains one.

**Files:** `internal/tui/popup.go`, `internal/tui/popup_test.go`

**Tests.** `popup_test.go`: `TestRenderPopupEmptyRowBlockPadsAboveTheHint` (title, body, no rows, hint, `padBelow` → exactly one blank between body and hint; with `maxRows: 0` the blank is dropped); `TestRenderPopupPadBelowNeedsAHint` (rows + `padBelow`, no hint → no trailing blank); `TestRenderPopupRowPadSurroundsTheBlock` (:1701) gains the empty-block row, an overflowing-list case (rows > `capLines`: `capLines`-2 row lines plus both pads), a floor case (`capLines` equal to the anchor row's height: rows, no pads) and a scrollbar case asserting no bar cell on either pad line, and its base spec gains a hint so the three `padBelow` subtests keep their trailing blank. Inline goldens (`popupTitleSpec` sets no pads) unchanged.

**Acceptance.** `go build ./... && go test ./internal/tui -run 'TestRenderPopup' -count=1`

**Commit:** `feat(tui): the pop-up painter keeps the pad above the hint when a pane has no rows`

## 2. The free-text ask pane breathes — ✅ DONE (2026-09-06)

NOTES (2026-09-06): the item's named test passes both before and after the arithmetic change — at 80×24 and at `smallestOverlayWindow` the free-text pane renders identically either way, because a roomy window pays for the unpainted line out of its surplus and a floored one affords neither line. Added a third subtest ("an overflowing question spends every granted row", 80×20 with an overflowing question) that pins the BOOKING: the pane must paint every row `frameRowPlan` granted it. It fails on the old `popupRowPadLines(true, …)` (8 of 9 rows) and passes on the new one.

Depends on items 0 and 1.

**What.** `internal/tui/ask.go` `askPrompt` (:314-385): with no choices, the pane still books `popupRowPadLines(false, askRowStyle.padBelow)` in its `wanted`/`capped` arithmetic (:356-363) and passes the resulting row demand to `popupBudget`, so the blank between the question and the hint is painted at any height that affords it and dropped at the floor. The choice-bearing variants change nothing (already `rowPadAbove` + `padBelow`). Binding: one arithmetic for all four variants — no `if len(rows) == 0` branch that restates the pad count.

**Files:** `internal/tui/ask.go`, `internal/tui/model_test.go`

**Tests.** `model_test.go`: `TestModelAskFreeTextBreathesAboveTheHint` — via `newAskModel` with no choices at 80×24, the row after the question is blank and the hint follows (`paneRowIndex`); at `smallestOverlayWindow` the blank is gone and the pane is `popupTitleBorderChrome`+question lines. `TestModelAskPromptMenuChrome` (:2125) and the question-floor subtests (~:2340-2370) stay green unchanged.

**Acceptance.** `go test ./internal/tui -run 'Ask|Popup' -count=1`; `go test ./cmd/apogee -run TestE2EPopupFramesPrompts -popup-design -count=1 -v 2>&1 | grep 'does not match'` names only `popup-approval`.

**Commit:** `feat(tui): the free-text ask pane keeps a blank row above its hint`

## 3. Every part of the approval body is a paragraph — ✅ DONE (2026-09-06)

NOTES (2026-09-06): consequential edit — cmd/apogee/e2e_approval_test.go: made necessary by the paragraph join; `assertFixFollowsReason` asserted `Fix:` on the row directly under `Reason:`, and the file-top doc block said the same. The item's own Acceptance names `TestE2EApprovalForcesALook`, so both are rewritten to the call-F shape (one blank between the two parts) rather than dropped; the frame the test holds (`t10-forced-pane`) now matches without `-update`.

NOTES (2026-09-06): consequential edit — layout.md: made necessary by the paragraph join; the sketch under `**What the approval prompt's body says…**` and the sentence beneath it stated "the reason and the arguments adjacent", which call F makes false. The sketch gains the blank, the sentence states the paragraph rule, and the paragraph was re-wrapped to the file's ~100-column measure. No other layout.md section touched — item 6 owns the breathing rule's own statement.

NOTES (2026-09-06): `internal/tui/model_test.go:2212` ("unlike the approval box's adjacent decisions") and `internal/tui/popup.go:351` ("the approval's four adjacent rows") were left as written — both name the MENU rows' adjacency, which call F does not change. The regression guard's grep ran clean afterwards.

NOTES (2026-09-06): `TestModelApprovalPartsAreParagraphs` builds its model at 100×30 rather than through `newApprovalModel`, whose default window elides the multi-part bodies the table needs whole; the blank-row predicate `paneRowIsBlank` was lifted out of `TestModelApprovalMenuSpacing`'s local closure so the three tests judge a blank the same way.

NOTES (2026-09-06): pre-existing and NOT this item's — `TestDocsEnvRootsMoveTheHomeAndTheFence` fails ("timed out waiting for apogee's first frame") whenever `./cmd/apogee` is run with `-popup-design`, on the clean tree at the plan's base as well as with this item's changes (verified by stashing). It spawns a child apogee that does not know the flag. Item 6 deletes `-popup-design` and closes it. `TestE2ELiveStateFollowsTheRunningSession` (t16) and `TestE2EPopupFramesLists` still fail under `-popup-design`: those are items 4 and 5's design frames, unaffected by this item.

Depends on items 0 and 1.

**What.** `internal/tui/approval.go` `approvalPrompt` body (:321-365, join at :398): the parts — sub-agent line, `Reason:`, `Fix:`, `Scope:`, `approvalArgsBlock`, `resolvedPathNote`, `mcpServerGrantNote` — are joined with one blank row between consecutive parts (call F); a part that wraps (t10's four-line `Fix:`) keeps the blank after its last line because it is one part. The menu's `rowPadAbove` stays; no hint, no `padBelow`. Body budgeting is untouched: the blanks are body lines counted by `popupBodyLineCount` and elided with the body at short windows (t12's `… (+4 more lines)` is design). Update the mockup at `docs/layout/user-questions-layout.md:20-29` to the t10 shape.

**Regression guard.** The adjacency this item reverses is written in three places the What does not name: `approval.go:258-266` ("It is the ONLY blank the pane spends"), the inline comment at `approval.go:398`, and the test doc at `model_test.go:1734-1743`. Binding as a RULE, not a closed list of sites: every comment naming the `Reason:`/`command:` adjacency or the pane's one blank is rewritten to call F — `grep -n 'adjacent\|ONLY blank' internal/tui/approval.go internal/tui/model_test.go` (six hits at `6e895e77`). `approval.go:258-266` records today's spacing as the mockup's deliberate choice; call F supersedes it, and the rewritten comment says so.

**Files:** `internal/tui/approval.go`, `internal/tui/model_test.go`, `internal/tui/approval_test.go`, `docs/layout/user-questions-layout.md`

**Tests.** `model_test.go`: `TestModelApprovalMenuSpacing` (:1744, doc :1734-1743) and `TestModelApprovalDrawsRemedyUnderReason` (:1587) rewritten to assert `Reason:`, blank, `Fix:`…, blank, `command:`; `approval_test.go:71` (`Scope:` after `Reason:`) asserts the blank between; new `TestModelApprovalPartsAreParagraphs` table over {reason only, reason+fix, reason+scope+note} asserting exactly one blank between parts and none before the first or after the last.

**Acceptance.** `go test ./internal/tui -run 'Approval' -count=1`; `go test ./cmd/apogee -run 'TestE2EPopupFramesPrompts|TestE2EApprovalForcesALook|TestE2EHostileWrapsUnderItsOwnIndent' -popup-design -count=1` passes.

**Commit:** `feat(tui): the approval body sets every part off as a paragraph`

## 4. The list panes breathe — picker, /sessions browser, / dropdown, /settings sub-lists — ✅ DONE (2026-09-06)

NOTES (2026-09-06): the pads ride `popupSpec.rowPadAbove` / `popupRowStyle.padBelow` set in `renderList`, booked into the demand and cap handed to `popupBudget` through `popupRowPadLines`; the painter's own reservation (item 1) owns the window, so `renderList` subtracts nothing.

NOTES (2026-09-06): `internal/tui/autocomplete_test.go` is on the item's **Files:** list but needed no amendment — `autocomplete_test.go:160,227` and `picker_test.go:2427,2480` index rows by content, not by an offset from the title, and stayed green. `picker_test.go` and `sessions_test.go` were amended: four physical-line counts gained the two blanks, and the two "an unfiltered pane spends no spacer" loops now exempt the row block's own pads.

NOTES (2026-09-06): new tests are `TestRenderListBreathes` (three exact pane shapes: no body at 26 rows = full `rowCap` plus both blanks; a body at 26 rows = the body's own pads and no double; the floor at 18 rows = rows and no blanks) and `TestRenderListBreathingRowsCarryNoScrollbarCell`.

NOTES (2026-09-06): the `/settings` enum sub-list (`renderSettingsSubList`, settings.go:1844) carries a body with `bodyPad` false, so under the item's literal `c.body == ""` gate it gains only the lower blank — its question still sits flush on the rows. Implemented as the item's text states; see DEFER.

Depends on items 0 and 1.

**What.** `internal/tui/listsurface.go` `renderList` (:465-491): set `rowPadAbove` when `c.body == ""` (a typed filter's `bodyPad` already closes with a blank — ADR 0053 D6, `renderFilterList` :499-504) and `rowStyle.padBelow` when `c.hint != ""`; book both through `popupRowPadLines` in the same arithmetic that books the filter pads (`popupBodyPadLines`, :468) — the row demand and the row cap handed to `popupBudget` are lines and include the pads, as `ask.go:356-358` does. Binding: at a comfortable height every list still shows its full `max*Rows` rows (8 for picker, sessions, dropdown) plus the two blanks; at the floor rows and no blanks. Every `renderList` caller inherits it (picker.go:1104, sessions.go:630, autocomplete.go:1051, settings.go:1844); no per-caller flag.

**Regression guard.** The demand and cap handed to `popupBudget` include the pad lines so the grant grows by two at comfortable heights; the WINDOW is the painter's (item 1's reservation), so `renderList` does not subtract the pads itself.

**Files:** `internal/tui/listsurface.go`, `internal/tui/listsurface_test.go`, `internal/tui/picker_test.go`, `internal/tui/sessions_test.go`, `internal/tui/autocomplete_test.go`

**Tests.** `listsurface_test.go`: `TestRenderListBreathes` — empty filter: title, blank, rows, blank, hint; typed filter: title, blank, filter, blank, rows, blank, hint (no double); at `maxRows` = rows exactly, no blanks. `sessions_test.go` frame-height property (:1725-1780) and `:2003` window assertions stay green; `autocomplete_test.go:160,227`, `picker_test.go:2427,2480` amended where they index rows from the title.

**Acceptance.** `go test ./internal/tui -run 'List|Picker|Session|Autocomplete|Dropdown' -count=1`; after item 0, `go test ./cmd/apogee -run TestE2EPopupFramesLists -popup-design -count=1` passes all three frames.

**Commit:** `feat(tui): every list pop-up keeps a blank row under its title and above its hint`

## 5. The report panes and /settings breathe

Depends on items 0 and 1.

**What.** `internal/tui/reportpane.go` `reportSpec` (:202-234): `rowPadAbove` and `padBelow` set, booked at :203 alongside `popupChrome` (chrome unchanged; row demand +2 lines) — `/usage`, `/inspect`, `/thinking` scroll windows shrink by two at every height, `reportWheel` arithmetic follows the window. `internal/tui/settings.go` `settingsKeyListSpec` (:1715-1743) and `settingsTextSpec` (:1810-1832): `padBelow` only — `rowPadAbove` stays off because `settingsBody` (:1649-1670) closes with its own blank; rewrite the comment at :1292-1293 to say the description's blank IS the pane's breathing row and t16 holds it. `settingsPaint`/`rowAt` (`mouse.go:890-927`) need no change (`rowsAt` is above the block). Update `docs/layout/settings-screen-layout.md` mockups (:43-69 gains the blank above the hint; :99-107 and :126-132 gain both, they are `renderList` panes).

**Regression guard.** Same booking rule as item 4 (demand and cap include the pad lines; the painter reserves the window). For the reports, `reportpane.go:217`'s hidden-row arithmetic follows the painter's returned window (`place.start`/`place.end`), not its own `shown - pad`. `TestReportPaneBreathes` uses a list LONGER than the window at 80×24, asserting the window shrank by two and both blanks show without scrollbar cells.

**Files:** `internal/tui/reportpane.go`, `internal/tui/settings.go`, `internal/tui/usage_test.go`, `internal/tui/inspector_test.go`, `internal/tui/thinkingpane_test.go`, `internal/tui/settings_test.go`, `internal/tui/paint_test.go`, `docs/layout/settings-screen-layout.md`

**Tests.** `TestReportPaneBreathes` (usage_test.go): over a list LONGER than the window at 80×24 — title, blank, rows, blank, hint, the window two rows shorter than before this item and neither blank carrying a scrollbar cell; at `smallestOverlayWindow` no blanks and the same row count as before this item. `settings_test.go`: `TestSettingsPaneBreathesAboveItsHint` — one blank above the hint, and still exactly the two blanks under `Description:`; `TestPaintedSettingsPaneAtItsFourRowFloor` (`paint_test.go:1258`) unchanged and green; `TestSettingsClickSelectsTheRowUnderThePointer` (`mouse_test.go:2741`) green.

**Acceptance.** `go test ./internal/tui -run 'Report|Usage|Inspector|Thinking|Settings|Painted' -count=1`; `go test ./cmd/apogee -run TestE2ELiveStateFollowsTheRunningSession -popup-design -count=1` passes.

**Commit:** `feat(tui): the report panes and /settings keep a blank row above their hint`

## 6. The designs become held goldens; layout.md states the rule

Depends on items 0, 2, 3, 4, 5.

**What.** Delete the `-popup-design` surface: the flag var and `skipUnlessDesign` (`cmd/apogee/e2e_popups_test.go:31-39`), its two calls (:75, :122), and the three `if *popupDesign {` gates with their two-line comments (`e2e_approval_test.go:87-91`, `e2e_hostile_test.go:245-249`, `e2e_livestate_test.go:155-159`), leaving bare `tuitest.Golden(...)` calls; rewrite the file comment at `e2e_popups_test.go:12-18` — the frames are held. All eleven frames must pass WITHOUT `-update`; if one differs, the layout item that owns it is wrong, not the frame. `layout.md` `## What "height" means`: add one paragraph stating the breathing rule and that the blanks give way first as a pair (cite the filter-line precedent at :2004-2006); restate :227-230 (the four rows are the floor shape, one row above it the hint gets its blank) and :236-239 (the blanks are not a fifth row); redraw the approval example :451-462; clarify :116-124 (the border still seats on the hairline); note the two blanks at :1883-1886 and :1945 and the shrunken `/usage` window at :1712-1717. Rule for the prose: every `layout.md` sentence that states a pane's row spend or shows a pane — `grep -n 'four rows\|hint row\|title row\|╭' layout.md`.

**Regression guard.** The Tests grep cannot "return only CHANGELOG history" as written: CHANGELOG.md is not under `cmd/ internal/ docs/ layout.md`, and this plan document — a saved repo doc that stays — carries the flag name eight times. Grep `cmd/ internal/ layout.md docs/adr docs/design` (or add `--exclude-dir=plans`), and state CHANGELOG.md (:21) separately as the one place the name survives.

**Files:** `cmd/apogee/e2e_popups_test.go`, `cmd/apogee/e2e_approval_test.go`, `cmd/apogee/e2e_hostile_test.go`, `cmd/apogee/e2e_livestate_test.go`, `layout.md`

**Tests.** The eleven goldens themselves; `grep -rn 'popupDesign\|popup-design' cmd/ internal/ layout.md docs/adr docs/design` returns nothing — CHANGELOG.md:21 and this plan are the only places the name survives.

**Acceptance.** `go test ./cmd/apogee -count=1` green; `git diff --stat -- cmd/apogee/testdata/frames/` empty (no frame re-recorded); `go vet ./cmd/apogee`.

**Commit:** `test(cmd/apogee): the pop-up frames are held goldens; layout.md states the breathing rule`

## 7. One shared row hit-test over popupPlacement

**What.** `internal/tui/popup.go`: `popupPlacement` gains `gap int` (set in `renderPopupPlaced` from `spec.rowStyle.gapLines()`) and a method `rowAt(line int) (row int, ok bool)` — `line` is the painted line index (top border = 0); walk `blocks[start:end]` subtracting `len(blocks[i])` then `gap`, as `settingsTextPaint.lineAt` (`mouse.go:1051-1063`) does today; a title, blank, gap, hint or border line answers `ok=false`; an empty window answers `ok=false`. `internal/tui/mouse.go`: `settingsPaint.rowAt` (:921-927) and `settingsTextPaint.lineAt` become callers of `popupPlacement.rowAt` — one module, one reason to change (binding: delete their private arithmetic). Add `popupPaneHit(pre Model, pane paneID, render func() (string, popupPlacement), y int) (row int, inRect bool, ok bool)` — the `settingsPaneRect` + `settingsPaint` shape generalised: rect from `pre.frameSpans().pane(pane)`, placement by re-rendering the spec, `row` via `rowAt(y - paneTop)`. `settingsPaint` uses it.

**Regression guard.** Plan-wide, inherited by items 8-11: (a) the click chain gains a `tea.Cmd` currency — each pane click handler returns `(Model, tea.Cmd, claimed bool)` and `handleMouseClick` returns the Cmd of the claiming handler (today every claim site returns nil, mouse.go:410-440), because `submitAnswer`, `resolveApproval`, `acceptPicker`, `acceptBrowser` and `acceptAutocomplete` all return Cmds; (b) `listCursor.highlight(n int) int` (listsurface.go:182) is a count-taking GETTER — add one setter `listCursor.seat(row, count int)` (clamped) in listsurface.go, and items 8-11 move the highlight through it (`askSel.seat`, `approvalSel.seat`, the list surfaces' cursor, `m.autocomplete`'s embedded cursor); no handler assigns the field directly. Two spellings in the What are corrected with it: `rowAt` answers `(row, sub int, ok bool)` — `settingsTextPaint.lineAt`'s callers need `sub` (`span(line, sub)` at mouse.go:1071, `settingsTextCaretAt` at :1082), and the key list and `popupPaneHit` ignore it — and `popupPaneHit`'s pane parameter is `pane framePane` (`frameSpans.pane`, model.go:2425; `type framePane`, model.go:3645), never `paneID`, which names nothing in the package. (c) **Call J** (owner, 2026-09-06) is this item's machinery, inherited by every handler in items 8-11: ONE value field on the Model — `clickArmed struct{ pane framePane; row int; ok bool }`, a plain value (ADR 0011) — set by a pane click handler when it highlights a row and consulted by the same handler on the next click (`ok && pane == this && row == hit` → activate), so the pointer activates only a row IT highlighted and a first click never activates, whatever the keyboard or the pane's default left highlighted; it is cleared when the highlight moves by keyboard or wheel (in ONE place — `handleKey`, model.go:1403, and `foldMouseWheel`, mouse.go:1393 — never per pane), when the pane closes, and after an activation. (d) The five accepts return `tea.Model`, not `Model` (`submitAnswer` ask.go:98, `resolveApproval` approval.go:210, `acceptPicker` picker.go:901, the browser accept sessions.go:294-301, `acceptAutocomplete` autocomplete.go:882), so the handlers take the Model back through ONE shared helper carrying the assertion the key path already makes (`modalClaim`, model.go:1219-1223, whose doc calls it this package's contract) — never five assertions of their own. (e) `popupPaneHit` asks `pre.openPanes().has(pane)` BEFORE the rect test and answers `ok=false` where the pane is not on the frame — the gate `dropdownWheel` makes for exactly this reason (autocomplete.go:736-745) — and every handler answers `return m, nil, false` there, as `handleReportClick` does (reportpane.go:412-416), so no handler can claim or dismiss on a frame carrying no such pane; ask and approval share `panePrompt` (model.go:3648) and their live predicates are `openPanes`' own (model.go:3673-3677), so the state check picks which of the two is up.

**Files:** `internal/tui/popup.go`, `internal/tui/popup_test.go`, `internal/tui/mouse.go`, `internal/tui/mouse_test.go`, `internal/tui/listsurface.go`, `internal/tui/listsurface_test.go`, `internal/tui/model.go`

**Tests.** `popup_test.go` `TestPopupPlacementRowAt`: table over single-line rows; wrapped rows with `askRowStyle` (gap 1, hanging indent); a scrolled window (`start` > 0, selected deep); an empty block; title/blank/hint/border lines → `ok=false`; `rowPadAbove` shifting `rowsAt`. `mouse_test.go`: `TestSettingsClickSelectsTheRowUnderThePointer`, `TestSettingsClickSeatsTheCaretInTheEditField`, `TestTheClickChainKeepsItsFrameToItself` green; new `TestPopupPaneHitAnswersNothingWithThePaneShut` (the pane off `openPanes` → `ok=false`, nothing claimed, before any rect arithmetic) and `TestClickArmClearsOnKeyAndWheel` (a latched row drops on any key press and any wheel notch).

**Acceptance.** `go build ./... && go test ./internal/tui -run 'PopupPlacement|PopupPaneHit|SettingsClick|ClickChain|ClickArm' -count=1`

**Commit:** `refactor(tui): one row hit-test over popupPlacement, shared by every boxed pane`

## 8. Click on the ask pane

Depends on item 7.

**What.** `internal/tui/mouse.go`: `handleAskClick(pre, msg)` in `handleMouseClick` after the report panes (:430) and before `handleFooterModeClick` (:439), live only while `m.state == stateAwaitingAsk && m.pendingAsk != nil`, geometry via `popupPaneHit` over `askPrompt`. Inside the rect: box holds text → claimed, no-op (call G); no choices → claimed, no-op; row hit ≠ `askSel` → `askSel.highlight(row)`, and on multi-select also `askChecked[row] = !askChecked[row]`; row hit == `askSel` → `m.submitAnswer()`; a non-row line → claimed, no-op. Outside the rect → claimed, no-op (call C). No selection is armed, so `handleMouseRelease` falls through unchanged. `internal/tuitest/keys.go`: `Click(x, y int) Key` and `Release(x, y int) Key` emitting SGR press/release (1-based, from 0-based cell coordinates), pinned in `TestKeysDecodeAsIntended`. `mouse_test.go`: generalise `settingsFrameCell` (:2724-2733) into `frameCell(t, m, want)` used by every pane click test (no second copy).

**Regression guard.** The key pins go in `internal/tuitest/driver_test.go` — `TestKeysDecodeAsIntended` lives there (:112) and `internal/tuitest/keys_test.go` does not exist. `askPaneModel` (mouse_test.go:4073) builds its `Reply` channel inline and returns only the Model, so no test over it can observe the worker receiving the choice: use `newAskModel` (model_test.go:1908, which returns the channel) or have `askPaneModel` return it too. The highlight moves through `listCursor.seat` (item 7), never `highlight(row)`, and `handleAskClick` returns `(Model, tea.Cmd, bool)` so `submitAnswer`'s re-armed spinner tick survives a click. The activating click is the SECOND pointer click on the same row (call J, item 7): a first click on ANY row — the default-highlighted first choice included — only highlights (multi-select: toggles) and arms, so the pane's own default never becomes an answer the pointer gave.

**Files:** `internal/tui/mouse.go`, `internal/tui/mouse_test.go`, `internal/tuitest/keys.go`, `internal/tuitest/driver_test.go`, `cmd/apogee/e2e_popups_test.go`

**Tests.** `mouse_test.go` over `newAskModel` (the reply channel is what the sent choice is read from): click a non-highlighted row → highlight moves and arms, nothing sent; a second click on THAT row → `submitAnswer` (worker receives the choice); a first click on the default-highlighted first choice → highlight and arm only, nothing sent (call J); multi-select: first click toggles `[✔]` and highlights, the second on the same row sends the ticked set; typed text → click swallowed, box text intact; outside click → nothing, transcript selection untouched; `TestMouseClickOnOverlayRowsArmsNoSelection` (:2584) amended to keep asserting no selection while the pane now claims. `e2e_popups_test.go` `TestE2EPopupClickAsk` over `popups.yaml`: raise the multi-select question, `Press(tuitest.Click(...))` + `Release` on the second finding's text (`Frame.Find`), wait for `[✔]` on that row; click again, wait for `Noted, thank you.`; close per the e2e checklist.

**Acceptance.** `go test ./internal/tui -run 'Ask|Click' -count=1 && go test ./internal/tuitest -count=1 && go test ./cmd/apogee -run TestE2EPopupClickAsk -count=1`

**Commit:** `feat(tui): the ask pane takes a click — highlight, toggle, second click sends`

## 9. Click on the approval pane

Depends on item 8.

**What.** `mouse.go`: `handleApprovalClick(pre, msg)` in the prompt slot beside `handleAskClick` (one `handlePromptClick` dispatching on state is acceptable; binding: the ask and approval semantics stay in their own functions), live while `m.pending != nil`, geometry via `popupPaneHit` over `approvalPrompt`. Row hit ≠ `approvalSel` → `approvalSel.highlight(row)` (outside the latch, like `↑/↓`); row hit == `approvalSel` → `if !m.approvalArmed { claimed, no-op }` else `m.resolveApproval()` (call D — the same gate `model.go:1515-1529` puts on `⏎`; the Cancel row stops the worker as `resolveApproval` already does). Non-row line and outside the rect → claimed, no-op (call C).

**Regression guard.** The pane opens with `Allow` highlighted (`approval.go:87`, `approvalMenu[0]`) and `awaitApprovalPane` already settles past the 100 ms latch (`approvalArmDelay`, approval.go:52; `settled` is 150 ms, e2e_smoke_test.go:26), so "row hit == `approvalSel` → `resolveApproval`" would grant a call on ONE click, on the security surface. Binding: the pointer activates only a row IT highlighted — a click-armed row latched on the Model — so the pointer's first click on the pane never activates, whatever the keyboard left highlighted; the arming latch (`approval.go:132-141`, the pane's answer to a gesture aimed at the previous frame) still gates the activating click on top of that. The highlight moves through `listCursor.seat` (item 7), and `handleApprovalClick` returns `(Model, tea.Cmd, bool)` so `sendApproval`'s `m.spin.arm()` survives a click. That latch is call J (item 7) — the shared `clickArmed` field, not a rule of this pane's own — and this handler carries item 7's other two: the open-pane gate (`pre.openPanes().has(panePrompt)`, asked before the rect test) and the one shared helper that takes `resolveApproval`'s `tea.Model` back.

**Files:** `internal/tui/mouse.go`, `internal/tui/mouse_test.go`, `cmd/apogee/e2e_popups_test.go`

**Tests.** `mouse_test.go` over `approvalPaneModel`: highlight moves before `approvalArmedMsg`; activating click before arming is swallowed (nothing resolved); after arming a click on the highlighted `Deny` row resolves deny; click on `Cancel` twice stops the worker; outside click → no-op, pane stands. e2e `TestE2EPopupClickApproval`: raise the approval through `awaitApprovalPane` (which already settles past the latch — no latch-clearing step), click `Deny`'s text, assert the highlight moved off `Allow`, click it again and wait for the denial to land (read `e2e_approval_test.go` for the exact text); a first click on the highlighted `Allow` resolves nothing.

**Acceptance.** `go test ./internal/tui -run 'Approval|Click' -count=1 && go test ./cmd/apogee -run TestE2EPopupClickApproval -count=1`

**Commit:** `feat(tui): the approval pane takes a click behind its arming latch`

## 10. Click on the picker and the /sessions browser

Depends on item 7.

**What.** `mouse.go`: `handleBrowserClick` then `handlePickerClick` at the head of the new slot (before the prompt), geometry via `popupPaneHit` over the pane's `renderList` spec. Row hit ≠ cursor → `listCursor.highlight(row)`; row hit == cursor → the pane's accept: picker `m.acceptPicker()`; browser: extract the inline accept at `sessions.go:294-301` into `acceptBrowser()` so `⏎` and the click share one path (binding). ADR 0053 D5: the hit-test row is the DISPLAYED (filtered) index — resolve through the same filter mapping `⏎` uses, never the unfiltered items. Browser `renaming`/`confirming` sub-modes swallow every click (as `browserWheel` :452-454). Outside the rect → dismiss exactly as `listCloses` does (`picker.go:847-850`, `sessions.go:290-293`) and claim the click (call C).

**Regression guard.** `acceptBrowser` keeps the `loadSession` Cmd in its signature (`sessions.go:301`) and `acceptPicker` already returns one, so both handlers return `(Model, tea.Cmd, bool)` and `handleMouseClick` returns that Cmd (item 7) — otherwise a clicked `/sessions` row closes the pane and never loads the record. The highlight moves through `listCursor.seat` (item 7), never `highlight(row)`. `/schedule <prompt>` opens the CYCLE picker and `acceptCycle` only swaps the overlay to the mode question (`schedule.go:214-219`), so the e2e needs two further clicks on a mode row (`acceptScheduleMode` → `createSchedule`, :226-233) before any confirmation line exists to wait for. Item 7's gate binds both handlers — `pre.openPanes().has(paneBrowser)` / `.has(panePicker)` BEFORE the rect test — so the outside-click dismiss, which CLAIMS, fires only while the pane is open and never swallows a click on a frame with no list. The activating click is the second click on the same row (call J, item 7); the first only moves the highlight. And `renderList` returns a string alone (listsurface.go:465-495), dropping `renderPopupPlaced`'s placement for both panes (`renderPicker` picker.go:1104, `renderSessionBrowser` sessions.go:647): add the placed sibling there once — `renderListPlaced` answering `(string, popupPlacement, bool)`, which `renderList` wraps — and split each render into its `listContent` builder plus the placed render (the `settingsKeyListSpec` + `renderPopupPlaced` shape, mouse.go:889-916), so the click reads the placement the painter spent; item 11 reuses it for the dropdown.

**Files:** `internal/tui/mouse.go`, `internal/tui/mouse_test.go`, `internal/tui/sessions.go`, `internal/tui/sessions_test.go`, `internal/tui/picker.go`, `internal/tui/listsurface.go`, `cmd/apogee/e2e_popups_test.go`

**Tests.** `mouse_test.go` over `pickerPaneModel`/`browserPaneModel`: highlight, then a second click on that row accepts; with a typed filter the clicked row accepts the filtered item, not the unfiltered one at that index; browser confirming → click swallowed; outside click closes the pane and reaches nothing underneath; with the pane shut, a click in the band it would have filled claims nothing and dismisses nothing. `sessions_test.go`: `⏎` still resumes through `acceptBrowser`. e2e `TestE2EPopupClickLists`: `/schedule tidy the logs` → click `15m` twice (the overlay moves to the mode question) → click a mode row twice → wait for the schedule's confirmation line (read `schedule.go` for the exact text); relaunch → `/sessions` → click the row twice → the session resumes (its first user line is back in the transcript).

**Acceptance.** `go test ./internal/tui -run 'Picker|Browser|Session|Click' -count=1 && go test ./cmd/apogee -run TestE2EPopupClickLists -count=1`

**Commit:** `feat(tui): the picker and the /sessions browser take a click`

## 11. Click on the / dropdown

Depends on item 10.

**What.** Recast at the regression check (2026-09-06). `mouse.go`: `handleDropdownClick` last in the slot (the dropdown is the input-slot tenant — `foldMouseWheel:1436-1440`'s reason), geometry via `popupPaneHit` over the autocomplete spec. Row hit ≠ `selected` → highlight; row hit == `selected` → `m.acceptAutocomplete()` unconditionally (the `tab` spelling, never `⏎`'s exact-match decline at `autocomplete.go:684-716`). Outside the rect → `m.dismissAutocomplete(); m.layout()` and let the click CONTINUE down the chain (call C amended, the `handleUsageClick` currency) — it still reaches the box and the transcript.

**Regression guard.** Outside click on the dropdown = dismiss AND let the click CONTINUE down the chain (the `handleUsageClick` currency, mouse.go:412-416), so a click in the input box still seats the caret and a transcript click still lands — owner-ratified 2026-09-06 (header call C, amended). Name the reversed narration: `autocomplete.go:683-686` and `foldMouseWheel`'s dropdown paragraph (`mouse.go:1436-1438`) stay true — the click rule matches them, item 12 quotes them. The e2e test clicks a `takesArgs && !runsBareAtAccept` row (`/confine`, command.go:254) whose accept splices `/confine ` into the box, never `/compact` (which RUNS on accept); the reducer test covers the same. The highlight moves through the item-7 setter on `m.autocomplete`'s cursor, never `highlight(row)`. Four more, from the same check: (1) call J (item 7) governs the activation — the menu opens with row 0 already highlighted (`computeAutocomplete`, autocomplete.go:157) and row 0 is `/clear` (the alphabetical table, command.go:251), so an unconditional accept would run `startNewSession` on the pointer's FIRST press; the first click only highlights and arms, and the "exact match typed" reducer case clicks twice. This is the plan's own ratified click meaning — never select-and-send in one click (header, Sequencing) — which the recast keeps. (2) The handler is gated on `m.openPanes().has(paneDropdown)` BEFORE the rect test, as `dropdownWheel` is (autocomplete.go:736-745): ungated, `inRect == false` cannot tell "no menu on the frame" from "click outside the menu", and every click in the app would pay a `layout()` and clear `m.skillRegion` (autocomplete.go:769-772). (3) `renderAutocomplete` (autocomplete.go:1042) returns a string only, through `renderList` → `renderPopup`, which drops the placement: split it into its `listContent` builder plus item 10's placed render, so `popupPaneHit` reads the numbers the painter spent instead of re-deriving title, hint, `rowCap`, rows and `selected` in mouse.go — `internal/tui/autocomplete.go` joins **Files:**. (4) Inside the rect but on no row (title, blanks, hint, borders — `cmd/apogee/testdata/frames/popup-dropdown.txt`) → claimed, no-op, exactly as items 8-10 say; the dismiss-and-continue answer is reserved for `inRect == false`.

**Files:** `internal/tui/mouse.go`, `internal/tui/mouse_test.go`, `internal/tui/autocomplete.go`, `cmd/apogee/e2e_popups_test.go`

**Tests.** `mouse_test.go` over `dropdownPaneModel`: the first click highlights and arms, a second click on that row accepts and fills the box as `tab` does; with an exact match typed the two clicks still accept; a click on the menu's own hint row → claimed, no-op; an outside click in the box dismisses the menu AND seats the caret, arming no transcript selection; with no menu up, a click where it would have stood claims nothing, dismisses nothing and pays no `layout()`. e2e `TestE2EPopupClickDropdown`: type `/`, click `/confine` twice, wait for the box to read `/confine ` (its accept splices the verb, `command.go:254`).

**Acceptance.** `go test ./internal/tui -run 'Autocomplete|Dropdown|Click' -count=1 && go test ./cmd/apogee -run TestE2EPopupClickDropdown -count=1`

**Commit:** `feat(tui): the / dropdown takes a click`

## 12. The click doctrine in the code narration and the layout docs

Depends on items 9, 11.

**What.** `internal/tui/mouse.go`: the `handleMouseClick` doc block (:390-399) and the wheel doctrine (:1362-1392) state that a click follows the notch — the pane under the pointer owns it — and the two outside-click policies with their reason (esc on ask/approval is not a dismiss). `internal/tui/doc.go` file narration for `mouse.go` and `popup.go` names the shared hit-test. `layout.md`: the wheel paragraph (:111-114) and `## The /usage popup` (:1708-1712) gain the click rule and the outside-click table; `docs/layout/user-questions-layout.md` states click semantics for both prompts. Rule for the prose: every sentence that says the five panes are keyboard-only or wheel-only — `grep -rn 'wheel\|click\|pointer' layout.md docs/layout docs/manual internal/tui/doc.go`. Hints untouched (call H).

**Regression guard.** The doc-map guard is `TestDocMapNamesEveryFile` (`internal/tui/docmap_test.go:12`) and Go's `-run` is case-sensitive, so the acceptance runs `-run 'DocMap|Click'`. `grep -n 'click' layout.md` is already non-zero at `6e895e77` (:108-109, :585, :809-834, :1432-1443, :1708-1711 …), so the acceptance asserts the click wording INSIDE the wheel paragraph and under `## The /usage popup` — a ranged grep, not a file-wide count. The prose rule's grep also names `docs/manual/probe.md`, `sessions.md`, `configuration.md`, `docs/layout/tool-layout.md` and `settings-screen-layout.md`: verified to carry no keyboard-only claim, so they are READ-only here and **Files:** stays closed; one that turns out to carry such a claim lands as a dated NOTES line under this item.

**Files:** `internal/tui/mouse.go`, `internal/tui/doc.go`, `layout.md`, `docs/layout/user-questions-layout.md`, `docs/manual/commands.md`

**Tests.** None beyond `go vet` (comments) and `go test ./internal/tui -run DocMap` (doc.go coverage).

**Acceptance.** `go vet ./internal/tui && go test ./internal/tui -run 'DocMap|Click' -count=1`; the click wording is present in `layout.md`'s wheel paragraph (:111-114) and under `## The /usage popup` (:1708-1712) — a grep of those two ranges, not of the file.

**Commit:** `docs(tui): clicks follow the wheel doctrine — the pane under the pointer owns them`
