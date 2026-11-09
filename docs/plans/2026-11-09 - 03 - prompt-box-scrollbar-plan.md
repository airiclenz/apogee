# Prompt box scrollbar — implementation plan

**Goal:** When the draft's wrapped rows outgrow the prompt box, the box shows a vertical scrollbar column under the same overflow-only rule as the boxed pop-ups, the mouse wheel over the box scrolls the draft, and `layout.md` states the rule.
**Date:** 2026-11-09
**Status:** unexecuted
**sized for:** ~200k-context host
**base:** 70ce2f60
**Closes:** prompt-box-scrollbar

**Sources:**
- `.capcom/issues/prompt-box-scrollbar.md` (issue + Acceptance)
- `layout.md` — "## What "height" means" ("A capped box is a window onto the draft…"), "## The scroll bar and the column it hangs in"
- ADR 0030 (one width authority mirroring the painter); ADR 0037 (`/settings` applies `ui.show-scrollbar` live)
- glossary `prompt-recall` (↑/↓ belong to recall only on an empty box or untouched recalled entry)

**Ratified design calls (owner, 2026-11-09):**
- **Bar column:** an overflowing box lays out `border | pad | text | blank | bar | pad | border` — the text column shrinks by 2 (blank gutter + bar); the existing right pad stays between bar and border.
- **Setting gate:** the box bar honours `ui.show-scrollbar` like the transcript and pop-up bars; off → no bar, no reserved columns.
- **Overflow only:** while every wrapped row fits, no bar and no reserved gap — the box is byte-identical to today.
- **Wheel:** while the box overflows, a wheel notch over the box moves the caret up/down by the transcript's notch rows and the window follows; while the draft fits, a notch over the box scrolls the transcript as today.
- **`+N` count:** the top-border hidden-rows count stays, bar or no bar.

**Standing requirements:**
- skills: coding-standards
- This box is a Raspberry Pi: never `go test -race` over a whole package; run single tests: `GOMEMLIMIT=2GiB go test -race -count=1 -run 'TestX' ./internal/tui/`. Never two test runs at once.
- Bubble Tea v2 names only (`tea.KeyPressMsg`, `tea.MouseWheelMsg`, `msg.String()`).

**Out of scope:**
- Clicking or dragging the bar; scrolling the draft without moving the caret (the bubbles textarea viewport is private).
- Changing the transcript or pop-up bars, PgUp/PgDn routing, or `prompt-recall`.

**Regression check (2026-11-09, 70ce2f60):**
- 1: guard folded (two-width row memo, width-aware freshenTranscriptClamp, reseat on a width change, border-safe absence assertions, comment sweep; writer decision: re-read `inputInnerWidth()` after typing)
- 2: guard folded; recast (writer decision: a notch seats the caret on the window's edge row first, so every notch moves the window); yields to ADR 0025 decision 11
- 2 (re-check): guard folded (edge seat by `CursorUp`/`CursorDown` from the caret's own visual row, never `reseatCaret`/`caretTo` or a per-notch reseat walk; first notch asserted to move exactly `MouseWheelDelta`; widened comment-sweep grep; picker.go, reportpane.go, sessions.go added to Files); yields to ADR 0025 decision 11
- 3: guard folded (wheel-sentence sweep, "the width never moves" scoped to the transcript; writer decision: registry Desc joins the sweep)

## 1. Prompt box draws a scrollbar column while its draft overflows — ✅ DONE (2026-11-09)

NOTES (2026-11-09): consequential edit — internal/tui/lineeditor.go: made necessary by layout() now re-seating the caret on a width change as well as a height change (reseatInput's comment said "only on a height change")

NOTES (2026-11-09): consequential edit — internal/tui/settingsapply.go: made necessary by ui.show-scrollbar now also moving the prompt box's text width (the apply arm's comment named only the transcript width)

NOTES (2026-11-09): internal/domain/uiprefs.go's ShowScrollbar comment also dropped the false "process-constant, wrap width never changes mid-run" sentence (ADR 0037 applies it live), since the rewritten comment names which bars the key governs

NOTES (2026-11-09): the bar's cells are painted by a new shared scrollbarCells (boxdraw.go), which renderScrollbar now calls too; popupRowScrollbar keeps its own loop (out of scope). The join squares each row on the box's surface field (squareOnField) rather than joinColumn's bare pad, so no terminal background shows inside the box

NOTES (2026-11-09): TestInputBoxScrollbarShowsWhileTheDraftOverflows pastes its draft rather than using withDraft: the textarea only scrolls its window inside its own Update, so an assigned value leaves ScrollYOffset at 0

NOTES (2026-11-09): retry fix — rowCountMemo evicts an empty slot, then one holding another draft, else the slot not hit last (a new last field), so a resize while the draft overflows no longer leaves the two new widths evicting each other; TestInputContentRowsMeasuredOncePerKeypressAndView gained a resized-window case pinning 0 recounts over three repaints (fails with 12 under the old victim choice)

**What:**
**Goal:** With `ui.show-scrollbar` on and the draft's wrapped rows at the full text width exceeding the box height (`maxInputRows` or the frame's `draftRowsCeiling`), `Model.inputView()` renders each content row as text, one blank column, one bar column (thumb `glyphScrollThumb`/`th.scrollThumb`, track `glyphScrollTrack`/`th.scrollTrack`), then the right pad and border; the thumb's position and size come from `scrollbarThumb` over the textarea's first visible row, its height and the draft's row count at the narrowed width. A fitting draft, or the setting off, renders exactly as before.
**Regression guard.** TestPromptScrollClampAtMaxHeight and assertInputBoxWindowFollowsTheCaret (and any other test that reads inputInnerWidth() before typing an overflowing draft and uses it afterwards) must re-read inputInnerWidth() after the draft is typed, so they measure at the bar-narrowed width; add them to item 1's Files/Tests and its Acceptance -run pattern.
The row-count memo (`rowCountMemo.rows`, `prompteditor.go`) keeps one count per width (two slots, or the full-width count cached beside the narrow one), and `TestInputContentRowsMeasuredOncePerKeypressAndView` is amended to one count per measured width (2 while the bar shows). `freshenTranscriptClamp`'s no-op test also requires `m.input.Width() == m.inputInnerWidth()` (a recall ↑ via `showRecall` can toggle the bar at an unchanged height); its comment and `doc.go`'s "an arm mutates" paragraph name the width too. `layout()` reseats when the width changed as well as the height (record `Width()` before `SetWidth`).
Absence assertions check each content row's interior (first and last cell stripped) or that the would-be bar cell is blank: `glyphScrollTrack` "│" is the RoundedBorder side rune. Comment sweep: every code comment stating the box's text width or which bars `ui.show-scrollbar` governs — `grep -n 'ShowScrollbar\|show-scrollbar\|both bars\|only on a height change' internal/tui internal/domain` (`internal/config`'s Desc and field comment are item 3's sweep).
**Approach (assumed at the header base):**
- Add `inputScrollbarGutter = scrollbarWidth + bodyRightGutter` (mirrors `popupScrollbarGutter`) and a `Model.inputScrollbarShown()` predicate: setting on (the same reader `popupScrollbarOn` uses) and `contentRows` at the full width `max(1, m.width-borderFrame-inputPadding)` exceeds `min(maxInputRows, draftRowsCeiling())`. It must not call `inputInnerWidth()` (recursion). Narrowing only adds rows, so overflow at full width implies overflow at the narrow width — the popup's measure-wide-then-narrow rule.
- `Model.inputInnerWidth()` subtracts `inputScrollbarGutter` while the bar shows. It is the sole width producer; its consumers follow it unchanged: `layout()` (`SetWidth`/`SetHeight`/`reseatInput`), `inputRows`, `hiddenDraftRows`, `inputContentRect` (→ `pointInputRow`, `promptCursor`, pointershape), `accentTokens`, `highlightInput`. `inputElisionEdge` reads `m.width` and is unaffected.
- The bar can toggle on an edit (a keystroke crosses the cap) and on a live `ui.show-scrollbar` change: every path that re-applies the textarea height after an edit or a settings apply must also re-apply the width (`SetWidth(m.inputInnerWidth())`) before the caret is reseated.
- Build the bar column in `inputView` from `scrollbarThumb(m.input.ScrollYOffset(), m.input.Height(), <contentRows at inputInnerWidth>, m.input.Height())` and attach it with the painter-measured join (`joinColumn` / the `joinScrollbar` pattern in `boxdraw.go`), never `lipgloss.JoinHorizontal`, before the border style is applied. One helper builds the column; do not copy `renderScrollbar`'s body if it can take the box's numbers.
**Files:** internal/tui/model.go; internal/tui/boxdraw.go; internal/tui/prompteditor.go; internal/tui/doc.go; internal/domain/uiprefs.go; internal/tui/model_test.go; internal/tui/inputaccent_test.go
**Read first:** internal/tui/model.go — inputInnerWidth, layout, freshenTranscriptClamp, inputView; internal/tui/prompteditor.go — rowCountMemo.rows;
internal/tui/boxdraw.go — scrollbarThumb; internal/tui/lineeditor.go — reseatInput; internal/tui/inputaccent_test.go — TestInputContentRowsMeasuredOncePerKeypressAndView
**Tests:** in `model_test.go`, mirroring `TestInputBoxCountsTheDraftRowsItCannotShow` and `TestRenderPopupScrollbar*`:
- `TestInputBoxScrollbarShowsWhileTheDraftOverflows` — draft of `maxInputRows+5` rows: every content row of `ansi.Strip(m.inputView())` ends `<bar glyph> │`, the column before the bar is blank, at least one thumb glyph; the caret at the end puts the thumb on the last content row.
- `TestInputBoxScrollbarAbsentWhileTheDraftFits` — one-line draft at `minInputRows` and a draft of exactly `maxInputRows` rows: no `┃`/track glyph in any content row's interior (first and last cell stripped — the border side is "│" too), `inputInnerWidth()` equals `m.width-borderFrame-inputPadding`.
- `TestInputBoxScrollbarFollowsTheSetting` — overflowing draft with `ui.show-scrollbar` off: no bar, full width, `+N` count still on the top border.
- `TestInputBoxScrollbarAtTheFrameCeiling` — a short frame where `draftRowsCeiling()` < `maxInputRows` caps the box: bar shows.
- `TestInputBoxScrollbarAppearsWhenTypingCrossesTheCap` — type into a draft of `maxInputRows` rows until it overflows: textarea `Width()` equals the narrowed `inputInnerWidth()`, `hiddenDraftRows()` agrees with the rendered `+N`, and `ScrollYOffset()` equals the narrow-width row count less `Height()`; deleting back under the cap returns offset 0 at the full width.
- `TestInputBoxBarTogglesOnARecallAtTheSameHeight` — ↑ recalls a longer entry at an unchanged box height: `m.input.Width()` equals `inputInnerWidth()` and no content row overflows the border.
- `TestPromptScrollClampAtMaxHeight`, `assertInputBoxWindowFollowsTheCaret` (`TestInputBoxWindowFollowsTheCaretAt*`) — re-read `inputInnerWidth()` after the draft is typed.
- `TestInputContentRowsMeasuredOncePerKeypressAndView` (`inputaccent_test.go`) — one count per measured width (2 while the bar shows).
**Acceptance:**
- `go build ./internal/tui/ && go vet ./internal/tui/`
- `GOMEMLIMIT=2GiB go test -race -count=1 -run 'TestInputBox|TestPromptScrollClampAtMaxHeight|TestInputContentRows|TestRenderPopupScrollbar|TestScrollbar|TestDocMap' ./internal/tui/`
**Commit:** `feat(tui): show a scrollbar on the prompt box while its draft overflows`

## 2. Wheel over an overflowing prompt box scrolls the draft

**What:**
Recast at the regression check (2026-11-09).
**Goal:** While the prompt box overflows, a wheel notch whose pointer is inside the box's rectangle (border rows included) first seats the caret on the window's edge row in the notch's direction (top visible row for up, bottom for down), then moves it up/down by the transcript's notch row count, so every notch moves the box's window and its thumb until the draft's end is reached, the transcript offset unchanged; while the draft fits, the same notch scrolls the transcript exactly as before. ↑/↓ from the draft's last row reach its first row (`ScrollYOffset()` 0) and back without `prompt-recall` taking the key.
**Regression guard.** a wheel notch first seats the caret on the window's edge row in the notch's direction (top visible row for up, bottom for down) by stepping `CursorUp`/`CursorDown` from the caret's own visual row (the wrap mirror's rows above the caret line, as `inputCellSpans` counts them, plus `LineInfo().RowOffset`), so the caret stays inside the window and the seat scrolls nothing; then it moves the notch rows. Never use `reseatCaret`/`caretTo`: no per-notch `MoveToBegin`/reseat walk is allowed (it lands the target on the window's BOTTOM row and costs O(lines²), `seatCaret`'s doc); a notch costs at most `Height()+MouseWheelDelta` cursor steps, like that many ↑ keypresses. The wheel test asserts the FIRST notch up from the draft's end lowers `ScrollYOffset()` by exactly `m.viewport.MouseWheelDelta`.
The box branch is gated on `m.inputEditable()` exactly as `handleMouseClick`'s caret seat is (an inert box's notch falls through to `scrollViewport`), and calls `m.dropRecall()` as the box click does. The step is `m.viewport.MouseWheelDelta` (no constant exists), on `MouseWheelUp`/`MouseWheelDown` without `ModShift` only. The upkeep is a `lineEditor` method running `fitWrapMemo` before `CursorUp`/`CursorDown`, then `recomputeAutocomplete` (live states) and `layout()`.
Comment sweep: every comment saying an unclaimed notch scrolls the transcript — `grep -n -i -A1 -B1 'notch\|wheel' internal/tui/*.go | grep -i transcript`, which also catches two-line comments (Update's `MouseWheelMsg` case, `dropdownWheel`, `foldMouseWheel`, `handleKey`'s PgUp/PgDn "the wheel does the same" comment, and the "leaves the notch to the transcript" docs in picker.go, reportpane.go, sessions.go and mouse.go). Yields to ADR 0025 decision 11 (".capcom/knowledge/decisions/adr-0025/adr-0025-decision.md", "the mouse wheel keep[s] scrolling in every state"): off an overflowing editable box the wheel scrolls the transcript in every state; the box claims only notches inside its own rectangle, as the panes do.
**Approach (assumed at the header base):**
- In `foldMouseWheel` (`mouse.go`), after the side-panel and `pointerPanes` checks and before `scrollViewport`, add a box branch: hit-test the pointer against the box rows (the `inputContentRect()` rows widened to the border rows), require `hiddenDraftRows() > 0`, and move the caret with the textarea's `CursorUp`/`CursorDown` through `lineEditor` the number of rows the transcript wheel step uses (reuse that constant; do not introduce a second). Run the same post-move upkeep a ↑/↓ keypress runs (accent/caret reseat) — find it in the key path, do not restate it. Update `foldMouseWheel`'s doc comment: the box owns the notch only while it overflows.
**Depends on item 1.**
**Files:** internal/tui/mouse.go; internal/tui/lineeditor.go; internal/tui/model.go; internal/tui/autocomplete.go; internal/tui/picker.go; internal/tui/reportpane.go; internal/tui/sessions.go; internal/tui/mouse_test.go
**Read first:** internal/tui/mouse.go — foldMouseWheel, inputContentRect, inputEditable; internal/tui/lineeditor.go — reseatCaret, fitWrapMemo;
internal/tui/model.go — hiddenDraftRows; internal/tui/inputaccent.go — inputCellSpans; bubbles/v2@v2.1.0/textarea — repositionView
**Tests:**
- `TestWheelOverAnOverflowingPromptBoxScrollsTheDraft` — caret at the end of a `maxInputRows+8` draft, wheel-up over the box: the FIRST notch lowers `ScrollYOffset()` by exactly `m.viewport.MouseWheelDelta` and moves the thumb in `inputView()` up, transcript offset unchanged; every further notch lowers it again; enough notches reach offset 0.
- `TestWheelOverAFittingPromptBoxScrollsTheTranscript` — one-line draft, long transcript: wheel over the box changes the transcript offset, caret unmoved.
- `TestWheelOverAnInertOverflowingPromptBoxScrollsTheTranscript` — errored/approval state with an overflowing draft: a notch over the box scrolls the transcript, caret unmoved.
- `TestWheelOverARecalledPromptBoxEndsRecall` — notch over an overflowing recalled entry: `recall.active` is false, the next ↑ moves the caret and the draft is unchanged.
- `TestArrowKeysReachEveryRowOfAnOverflowingDraft` — ↑ from the end to `ScrollYOffset()==0` and row 0, ↓ back to the last row; draft text unchanged (recall did not fire).
**Acceptance:**
- `go build ./internal/tui/ && go vet ./internal/tui/`
- `GOMEMLIMIT=2GiB go test -race -count=1 -run 'TestWheel|TestArrowKeysReach|TestInputBox|TestHiddenScrollbarYieldsTheColumn' ./internal/tui/`
**Commit:** `feat(tui): scroll an overflowing prompt box with the mouse wheel`

## 3. layout.md states the prompt box scrollbar rule

**What:**
**Goal:** `layout.md` states, in the "A capped box is a window onto the draft" paragraph and in "## The scroll bar and the column it hangs in", that an overflowing prompt box carries the same bar (text, blank, bar, pad, border; overflow only; `ui.show-scrollbar` gates it; the `+N` count stays) and that a wheel notch over it scrolls the draft only while it overflows; `docs/layout/prompt-box-layout.md` points to that rule; every user doc that says what `ui.show-scrollbar` governs names the prompt box.
**Regression guard.** the show-scrollbar setting's user-facing description in internal/config/registry.go is part of the prose sweep (add it to the grep, to Files, and add `go build ./internal/config/` plus any test pinning that description to Acceptance). The sweep's grep is `grep -rn 'show-scrollbar\|scroll bar\|scrollbar\|ShowScrollbar' layout.md docs/layout docs/manual README.md internal/config` (the registry Desc and `UIConfig.ShowScrollbar`'s field comment in `config.go`).
A second rule: every sentence saying where a wheel notch goes — `grep -rn -i 'notch\|wheel' layout.md docs/layout docs/manual` — states the overflowing box's exception (layout.md's "And the wheel follows the pointer" paragraph). layout.md's "the width never moves because a bar appeared or vanished on its own" is scoped to the transcript, naming the prompt box and the pop-ups as the overflow-only exception.
**Approach (assumed at the header base):** Rule for the prose sweep: every doc sentence naming the surfaces `ui.show-scrollbar` / `show-scrollbar` controls — find them with `grep -rn 'show-scrollbar\|scroll bar\|scrollbar' layout.md docs/layout docs/manual README.md`. Add a one-line pointer under the sketch in `prompt-box-layout.md`.
**Depends on item 2.**
**Files:** layout.md; docs/layout/prompt-box-layout.md; docs/manual/configuration.md; internal/config/registry.go; internal/config/config.go
**Read first:** layout.md — "## The scroll bar and the column it hangs in" ("Every overflowing popup carries the same bar", "It re-wraps once"), "A capped box is a window onto the draft", "And the wheel follows the pointer" paragraph; docs/layout/prompt-box-layout.md — the sketch;
docs/manual/configuration.md — `ui.show-scrollbar` paragraph; internal/config/registry.go — ui.show-scrollbar Desc; internal/config/config.go — UIConfig.ShowScrollbar comment
**Tests:** docs, the registry Desc and a field comment; the drift guards are the two greps. No test pins the Desc text; `TestRegistryRowInvariants` keeps it non-empty.
**Acceptance:**
- `grep -n 'prompt box' layout.md | grep -i 'bar'` prints the new rule.
- `grep -rn 'show-scrollbar' docs/manual layout.md` — every hit naming governed surfaces includes the prompt box.
- `grep -n -A3 '"ui.show-scrollbar"' internal/config/registry.go` — the Desc names the prompt box.
- `go build ./internal/config/ && GOMEMLIMIT=2GiB go test -count=1 -run 'TestRegistryRowInvariants' ./internal/config/`
**Commit:** `docs(layout): state the prompt box scrollbar rule`
