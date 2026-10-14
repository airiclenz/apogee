# List filter contract — /settings filters, every searchable list behaves one way

**Goal:** `/settings` gains type-to-filter, and the picker and `/sessions` are aligned to the same List filter contract (ADR 0053, 2026-10-14 amendment). Question panes stop filtering.
**Date:** 2026-10-14
**Status:** unexecuted
**sized for:** ~200k-context host
**base:** be66707e
**Closes:** settings-typing-filter

**Regression check (2026-10-14, be66707e):**
- 1: guard folded (mouse.go's `sessionBrowser.view` call, non-nil projection per kind); launch-profiles test case kept as written
- 3: recast (gate in `pickerKey`, paste drop left to item 6); Acceptance regex widened
- 3 (re-check): guard folded (probe key pinned to `q`, absent from every question-kind row; asserts `pickerCount()` and an empty filter)
- 4: guard folded (title tail only, shared title-line ladder kept, test and comment sweep); supersedes `popupTitleLine`'s "never the number" for filtering panes only (ADR 0053 amend-1014 rule 5)
- 5: guard folded (`TestSessionBrowserPaintsTheFilterLine` legend update)
- 6: guard folded (claimer gated on each pane's keyOpen, field built when unbuilt); question-picker paste drop kept
- 7: guard folded (re-seat inside `settingsSelection`, `TestModelFitsTheStack`); supersedes the in-code "/settings does not filter" prose (ADR 0053 amend-1014)
- 8: guard folded (t16 golden regenerated, e2e run, height test scoped)
- 9: guard folded (Acceptance regex fixed, re-runs t16 without regenerating, `closePane` waits on a shown marker)
- 11: guard folded (`docs/layout/user-questions-layout.md` added, rule widened to `docs/layout/*.md`)

**Sources:**
- `.capcom/knowledge/decisions/adr-0053/adr-0053-amend-1014.md` (the contract, rules 1–10)
- `.capcom/knowledge/glossary/glossary-identity/list-filter.md`, `.capcom/knowledge/glossary/glossary-safety/settings-surface.md`
- `.capcom/issues/settings-typing-filter.md`
- `docs/layout/settings-screen-layout.md`, `layout.md`

**Ratified design calls** (owner, 2026-10-14 grilling):
- **Backspace:** trims a set filter; on an empty filter it keeps its old meaning (armed reset in /settings, nothing elsewhere).
- **Delete (⌦) in /settings:** arms the reset (or rule removal) with or without a filter.
- **esc:** closes the pane, filter and all.
- **Matched text:** what the row says it is, never live state — /settings key path + section heading (heading match keeps its section); picker name + gloss per kind (item 1 table); fork the prompt text; /sessions the title cell with its qualifiers.
- **Placement:** filter on the title row (`<title> · filter: mo▌`), only while non-empty; caret only where typing reaches it; never in the prompt box.
- **Narrow title row:** title tail gives way first (`…`), then the hidden-rows count; the filter stays whole, else shows its caret end behind a leading `…`.
- **Selection:** stays on its row while it matches, else first match; stays put when the filter empties; ↑/↓ wrap over matches.
- **Zero matches:** one faint unselectable row `no <noun> matches "<filter>"`; a pane's own empty-state note wins.
- **Legend:** `type to filter · …` while empty; set → `type to filter` drops, `⌫ trim` appears; zero matches keep only `⌫ trim`, row-set keys (`^a`) and `esc`.
- **Paste:** lands in the filter folded to one line; /sessions rename → rename field; delete-confirm → dropped; never the prompt draft.
- **Sub-steps:** the filter survives /settings sub-steps; shown (no caret) while the list is on screen, hidden while a sub-list or the text editor replaces it.
- **Question panes:** workflow boundary, key migration, sub-agents migration, adoption pickers do not filter.
- **No match highlighting.**

**Standing requirements:**
- skills: coding-standards
- Raspberry Pi 5 box: never run `go test` over a whole heavy package or a pattern (`./internal/tui/`, `./cmd/apogee/`, `./...`) without `-run`; always `GOMEMLIMIT=2GiB go test -race -count=1 -parallel 1 -run '<regex>' <one package>`; never two test runs at once; full suite only as `APOGEE_TEST_SLOW=1 GOMEMLIMIT=2GiB make check`. Pass this to every sub-agent.
- Bubble Tea v2: `tea.KeyPressMsg` matched on `msg.String()` (`"backspace"`, `"delete"`, `"esc"`); paste is `tea.PasteMsg`.
- `tui.Model` stays under its 32 KiB pin (`internal/tui/model_test.go`): a new field is a `copyOnWriteField`, never a held `lineEditor`.

**Out of scope:**
- `settings-swallows-quit-keys` (ctrl+c / ctrl+l in /settings).
- The `/`|`@` autocomplete dropdown (narrowed by the chat token, not a List filter).
- Approval and ask_user menus (already non-filtering).

## 1. Match a row's matched text, not every cell — ✅ DONE (2026-10-14)

NOTES (2026-10-14): mouse.go needed no edit — `sessionBrowser` gained its own `view(rows)` (shadowing the embedded `listSurface.view`) that passes `sessionMatchedText`, so `handleBrowserClick`'s `m.sessionBrowser.view(rows)` call builds unchanged; `picker` gained the same per-kind wrapper, so `pickerFilteredView` is unchanged.
NOTES (2026-10-14): `TestPickerFilterMatchesTheJoinedRowCells` renamed to `TestPickerFilterMatchesTheRowsMatchedText` (the old name states the superseded rule); the fork kind's projection is `matchCells(1)` inline in the `pickerOfferings` table rather than a named value in fork.go, which the item's Files did not list.

**What:**
**Goal:** the picker and `/sessions` filter over each row's matched text only; values, marks, stats, ages, counts and spend never decide a match.
**Approach (assumed at the header base):** replace `rowMatchesFilter`'s joined-cells predicate in `internal/tui/listsurface.go` with one matcher over a matched-text projection (case-insensitive substring, no ranking). `filterPopupRows` / `listSurface.view` take the projection from the pane. One deep function, panes supply only which cells count: a per-kind field on `pickerOffering` (`internal/tui/picker.go`) and the browser's own in `internal/tui/sessions.go`. Binding per kind:
- model: id. server: name. load (launch profiles): name + backend gloss. cycle, scheduleMode, effort, mode: label + gloss. scheduleStop: name + `— every …` gloss. subAgentsServer: name (and the `auto` row's label). fork: the prompt text (not the ordinal).
- `/sessions`: the title cell including its `· <workspace>`, `· ⟳ sched`, `· ⑂ parent` qualifiers.
- Question kinds are handled by item 3; give them no projection work here beyond compiling.
Update the comments that describe "every cell counts" in `listsurface.go`, `picker.go`, `sessions.go` (rule: every comment stating the filter reads all cells / marker cells; `grep -n "cell" internal/tui/listsurface.go internal/tui/sessions.go internal/tui/picker.go`).
**Regression guard.** `handleBrowserClick` (`mouse.go:1457`) also calls `m.sessionBrowser.view(rows)`: give `sessionBrowser` its own `view(rows)` that passes the browser's projection, or update that call site — the package must build. Every kind gets a non-nil projection (the four question kinds: label + gloss, cells 0–1), so no typed key reaches a nil func before item 3; `TestEveryPickerKindHasAnOffering`'s every-answer-filled check covers the new field. The launch-profiles case behind `TestPickerFilteredViewAgreesOnRowsCountAndAccept` stays as written (filter "ollama" still matches beta's backend gloss) and must keep passing.
**Files:** internal/tui/listsurface.go, internal/tui/picker.go, internal/tui/sessions.go, internal/tui/mouse.go, internal/tui/listsurface_test.go, internal/tui/picker_test.go, internal/tui/sessions_test.go
**Read first:** internal/tui/listsurface.go — rowMatchesFilter, filterPopupRows, listSurface.view, Model.listKey; internal/tui/picker.go — pickerOffering, pickerOfferings, launchProfileRows;
internal/tui/sessions.go — sessionRowCells, sessionBrowser.filteredView; internal/tui/mouse.go — handleBrowserClick;
internal/tui/picker_test.go — TestPickerFilterMatchesTheJoinedRowCells, TestEveryPickerKindHasAnOffering
**Tests:** rewrite `TestPickerFilterMatchesTheJoinedRowCells` as a per-kind matched-text table (`run` no longer finds `· running`; `32k` no longer finds a model; `hour` finds `1h`); extend `TestEveryPickerKindHasAnOffering` to require the projection; add a /sessions case that `5m` (age) and a spend figure match nothing while a workspace name does.
**Acceptance:**
- `go build ./... && go vet ./internal/tui/`
- `GOMEMLIMIT=2GiB go test -race -count=1 -parallel 1 -run 'TestPicker|TestListSurface|TestSessionBrowser' ./internal/tui/`
**Commit:** `feat(tui): filter lists on what a row is named, not its live state`

## 2. Keep the highlight on its row as the filter changes — ✅ DONE (2026-10-14)

NOTES (2026-10-14): the shared helper is `pickerView.reseatPosition(underlying, held)` — stated over the view, returning a filtered position — so item 7's /settings list, whose selection indexes the full slice, maps it back through `offering`; `typeIntoOverlayFilter` reads the highlighted row via `offeringIndex` before the edit and re-seats after it.
NOTES (2026-10-14): added `TestPickerFilterKeepsTheHighlightOnItsRow` (mode picker) and `TestSessionBrowserFilterKeepsTheHighlightOnItsSession` beyond the three named tests, to drive the re-seat through both panes' real key paths; all five new tests fail against the old clamp.

**What:**
**Goal:** in every `listSurface` pane, typing or trimming the filter leaves the highlight on the same underlying row while it still matches, else on the first match; an emptied filter leaves it on that row; ↑/↓ wrap over matches only.
**Approach (assumed at the header base):** in `typeIntoOverlayFilter` (`listsurface.go`) record the highlighted underlying row (via `offeringIndex`) before the edit and re-seat on it after, else index 0. Expose this as one helper taking (old underlying index, new view) so item 7's /settings filter calls the same code — one rule, one home. `/sessions`' `^a` scope toggle keeps resetting to 0 (a scope change, not a filter change).
**Files:** internal/tui/listsurface.go, internal/tui/listsurface_test.go, internal/tui/picker_test.go, internal/tui/sessions_test.go
**Read first:** internal/tui/listsurface.go — typeIntoOverlayFilter, pickerView.offeringIndex, listCursor.clampSelection, Model.listKey; internal/tui/sessions.go — sessionBrowserVerb (ctrl+a resets selected to 0);
internal/tui/listsurface_test.go — TestListSurfaceTypingNarrowsTheListAndReclampsTheHighlight; internal/tui/picker_test.go — TestPickerFilterNarrowingClampsTheSelection
**Tests:** new `TestListSurfaceHighlightStaysOnItsRowWhileItMatches`, `TestListSurfaceHighlightFallsToFirstMatch`, `TestListSurfaceEmptiedFilterKeepsTheRow`; update the comments in `TestListSurfaceTypingNarrowsTheListAndReclampsTheHighlight`.
**Acceptance:**
- `go build ./... && go vet ./internal/tui/`
- `GOMEMLIMIT=2GiB go test -race -count=1 -parallel 1 -run 'TestListSurface|TestPicker|TestSessionBrowser' ./internal/tui/`
**Commit:** `feat(tui): keep a filtered list's highlight on its row`
Depends on item 1.

## 3. Question pickers stop filtering — ✅ DONE (2026-10-14)

NOTES (2026-10-14): `pickerOffering.filters` is set `true` on the ten filtering kinds and left at its zero value on the four question kinds; `pickerKey` sends a non-filtering kind's keys straight to `listCursor.key`. The picker's filter view needed no change, because nothing can type into a question kind's filter now (item 6 owns the paste drop).
NOTES (2026-10-14): internal/tui/commandrun.go is unchanged: the boundary legend already had no `type to filter`, and the file has no filter prose. The stale "ahead of the filter" comment on the boundary pre-check sits in picker.go's `pickerKey` and was fixed there.
NOTES (2026-10-14): consequential edit — internal/tui/adoption.go and internal/tui/keymigration.go: the "the filter … survives into the next question" comments in `acceptAdoption`/`acceptKeyMigration` now say "the highlight", because these panes no longer have a filter.
NOTES (2026-10-14): boundary `y`/`n` are still covered by the existing `TestClearWithAWorkflowRunningAsksToStopIt` and `TestForkWithAWorkflowRunningAsksToStopIt`, which pass. The new test fails on all four kinds once the source change is stashed.

**What:**
Recast at the regression check (2026-10-14).
**Goal:** the workflow-boundary, key-migration, sub-agents-migration and adoption pickers have no List filter: printable keys other than a kind's own bound keys (boundary `y`/`n`) do nothing, backspace does nothing, and their legends carry no `type to filter`.
**Approach (assumed at the header base):** add a per-kind "filters" flag on `pickerOffering` (`picker.go`), false for `pickerWorkflowBoundary`, `pickerKeyMigration`, `pickerSubAgentsMigration`, `pickerAdoption`; `pickerKey` and the picker's filter view consult it. Legends: `fork.go` is unaffected (fork filters); drop the `type to filter ·` lead from `keymigration.go`'s shared hint and adoption's; `commandrun.go`'s boundary legend already lacks it.
**Regression guard.** The question-kind gate lives in the picker's own key path (pickerKey hands a question kind's keys to listCursor.key directly and never reaches Model.listKey's typing branch) — no new Model.listKey parameter. "paste is dropped" is removed from this item's Goal: item 6 owns the question-picker paste drop and its test `TestQuestionPickerPasteIsDropped`. Acceptance runs the boundary confirm's and the sub-agents migration's tests too (`WithAWorkflowRunning`, `TestSubAgentsMigration`); `TestWorkflowBoundary` matches no test. The probe key in `TestQuestionPickersDoNotFilter` is pinned to one absent from every row of the four kinds and not `y`/`n` (`"q"` — `"e"` is in every row, so it cannot bite); after it the test asserts `pickerCount()` equals the full offering and `m.picker.filter.value() == ""`, then sends backspace; the bite comes from the letter and legend asserts, since backspace on an empty filter is inert at base too.
**Files:** internal/tui/picker.go, internal/tui/listsurface.go, internal/tui/keymigration.go, internal/tui/adoption.go, internal/tui/commandrun.go, internal/tui/picker_test.go
**Read first:** internal/tui/picker.go — pickerKey, pickerOfferings; internal/tui/listsurface.go — Model.listKey, listCursor.key, typeIntoOverlayFilter;
internal/tui/keymigration.go — keyMigrationHint; internal/tui/commandrun.go — boundaryKey;
internal/tui/picker_test.go — TestPickerHintsLeadWithTypeToFilter (lists only filtering kinds, stays green)
**Tests:** new `TestQuestionPickersDoNotFilter` (each of the four kinds: the letter `q` leaves `pickerCount()` at the full offering and the filter empty, backspace inert, legend has no `type to filter`); boundary `y`/`n` still answer.
**Acceptance:**
- `go build ./... && go vet ./internal/tui/`
- `GOMEMLIMIT=2GiB go test -race -count=1 -parallel 1 -run 'TestQuestionPickers|TestPicker|TestKeyMigration|TestSubAgentsMigration|TestAdoption|WithAWorkflowRunning' ./internal/tui/`
**Commit:** `feat(tui): question pickers answer, they do not filter`
Depends on item 1.

## 4. Show the filter on the title row — ✅ DONE (2026-10-14)

NOTES (2026-10-14): retired popupSpec.bodyPadAbove/bodyPadBelow with popupBodyPad and popupBodyPadLines, and listContent.bodyLead — the filter fold was their only setter, so the item's retirement left them dead.
NOTES (2026-10-14): the filter producer is overlayFilterTail(filter, typing) and each pane sets listContent.titleTail itself (pickerListContent, browserListContent), so paint and height query read one composition; renderPickerPlaced/renderSessionBrowserPlaced now call renderListPlaced.
NOTES (2026-10-14): on the title row the hidden-rows count sits after the filter (`<title> · filter: x▌  … +3`); the plan names its priority but not its position.
NOTES (2026-10-14): TestPickerPaintsTheFilterLineWithBreathingRoom is renamed TestPickerPaintsTheFilterOnItsTitleRow (it pins no hint, so item 5's conditional guard does not apply); TestPickerShortWindowGivesUpRowsBeforeTheFilterLine is recast as TestPickerShortWindowFilterCostsNoRow; TestPickerRoomyWindowKeepsTheRowTasteAndBothSpacers and its longOfferingBeat helper are deleted (no spacers left to pin). TestSessionBrowserPaintsTheFilterLine keeps its name for item 5's reference.
NOTES (2026-10-14): no mouse_test.go change was needed — TestPickerClickTakesTheFilteredRow, TestBrowserWheelWalksTheFilteredList and TestPickerWheelWalksTheFilteredOffering pass unchanged on the painter's placement.
NOTES (2026-10-14): consequential edit — internal/tui/model_test.go: made necessary by retiring the filter line (TestPaneHeightChangeReachesLayout comment named its added lines)
NOTES (2026-10-14): consequential edit — internal/tui/lineeditor_test.go: made necessary by retiring the filter line (a case label named it)
NOTES (2026-10-14): layout.md and docs/manual still describe the filter line; items 10/11 own those docs.

**What:**
**Goal:** a filtering pane's filter text renders on its title row as `<title> · filter: <text>▌` only while non-empty, never as a body line; the list keeps every row it had (nothing moves on the first key); the caret shows only while typing reaches the filter (hidden during `/sessions` rename and delete-confirm); a too-narrow title row cuts the title tail first, then drops the hidden-rows count, then shows the filter's caret end behind a leading `…`.
**Approach (assumed at the header base):** add a title-tail field to `popupSpec` (`internal/tui/popup.go`) and fit it in `popupTitleLine` by the priority above. Retire the filter-as-body fold: `filteredListContent`, the filter half of `renderFilterListPlaced`, the filter `bodyPad`, and `listSpec`'s `padAbove` special case (`listsurface.go`); keep the height query (`internal/tui/panes.go`) agreeing with the painter. `overlayFilterLine` becomes the title-tail producer (one place states the filter, ADR 0053 decision 6).
**Regression guard.** `popupTitleLine` is shared by every pane: drop the hidden-rows count only when the spec carries a non-empty title tail; a tail-less spec (approval, ask) keeps today's ladder and `TestRenderPopupNarrowTitleKeepsTheElisionCount` stays green. This supersedes `popupTitleLine`'s doc ("never the number", `popup.go:1653`) for filtering panes only, per ADR 0053 amend-1014 rule 5 — say so there. `renderPickerPlaced` (`picker.go`) calls the retired `renderFilterListPlaced`. Rule for prose: every comment naming the filter line, its blanks or its claim — `grep -rn 'filter line\|renderFilterListPlaced\|filteredListContent\|bodyPad' internal/tui`.
**Files:** internal/tui/popup.go, internal/tui/listsurface.go, internal/tui/sessions.go, internal/tui/picker.go, internal/tui/panes.go, internal/tui/doc.go, internal/tui/picker_test.go, internal/tui/sessions_test.go, internal/tui/mouse_test.go, internal/tui/popup_test.go, internal/tui/listsurface_test.go
**Read first:** internal/tui/listsurface.go — filteredListContent, renderFilterListPlaced, overlayFilterLine; internal/tui/popup.go — popupTitleLine, popupSpec;
internal/tui/panes.go — pickerHeight, sessionBrowserHeight; internal/tui/picker.go — renderPickerPlaced; internal/tui/listsurface_test.go — TestRenderListBreathes, breathingList;
internal/tui/popup_test.go — TestRenderPopupNarrowTitleKeepsTheElisionCount
**Tests:** rewrite `TestPickerPaintsTheFilterLineWithBreathingRoom`, `TestSessionBrowserPaintsTheFilterLine`; recast the filter-line check in `TestPickerZeroMatchesPaintsTheFilterOverNoRows` and `TestSessionBrowserZeroMatchPaintsTheFilterOverNoRows` to the title row (`<title> · filter: zzz`), leaving their legend and note-row assertions to item 5; drop or recast `TestRenderListBreathes`' "a padded body" case; delete or recast `TestPickerShortWindowGivesUpRowsBeforeTheFilterLine`, `TestPickerRoomyWindowKeepsTheRowTasteAndBothSpacers`; new title-row fit tests (long fork title, narrow width, caret-end elision) and a caret-hidden-during-rename test; re-check geometry in `TestPickerClickTakesTheFilteredRow`, `TestBrowserWheelWalksTheFilteredList`, `TestPickerWheelWalksTheFilteredOffering`, `TestOverlayHeightQueryMatchesItsRender*`.
**Acceptance:**
- `go build ./... && go vet ./internal/tui/`
- `GOMEMLIMIT=2GiB go test -race -count=1 -parallel 1 -run 'TestPicker|TestSessionBrowser|TestBrowser|TestPopup|TestRenderPopup|TestRenderList|TestOverlayHeight' ./internal/tui/`
**Commit:** `feat(tui): state a list's filter on its title row`
Depends on item 2.

## 5. Zero-match row and filter-state legend — ✅ DONE (2026-10-14)

NOTES (2026-10-14): the surface composes both states — filterLegend(hint, filter, matches, rowSetEntries...) and listContent.statingFilter(filter, noun, matches, rowSetEntries...) in listsurface.go; each pane calls statingFilter last, so the paint and the height query read one composition. The pane states `matches` itself so the /sessions empty-workspace note wins over the zero-match row while the legend still goes to its zero-match form.
NOTES (2026-10-14): the picker's per-kind noun is a new pickerOffering.noun field (filtering kinds only); /sessions uses sessionBrowserNoun, and its zero-match legend keeps sessionBrowserScopeEntry ("^a this/all"), now a named const that sessionBrowserHint is built from. fork.go needed no change — forkPickerHint is consumed unchanged through the offering table.
NOTES (2026-10-14): consequential edit — internal/tui/mouse.go: made necessary by the zero-match row (two click-handler comments said a zero-match pane offers no row; the code already claims the click with no row taken).
NOTES (2026-10-14): TestPickerPaintsTheFilterOnItsTitleRow and TestPickerShortWindowFilterCostsNoRow (item 4's) compared every line under the title, the legend included; they now compare the lines between title and legend and the first asserts the filtered legend — item 4's note that the rewrite "pins no hint" was wrong, so item 5's conditional regression guard applied.
NOTES (2026-10-14): the click-on-note-row test is named TestPickerAndBrowserZeroMatchRowTakesNoClick so the item's Acceptance regex (TestPicker|…) runs it.
NOTES (2026-10-14): during a /sessions rename with a filter set the legend still reads the filtered legend ("⌫ trim"), though backspace then edits the rename field — the browser has never changed its legend for rename/confirm, and the plan does not ask for it.

**What:**
**Goal:** a filtering picker or `/sessions` with no matches paints one faint, unselectable row `no <noun> matches "<filter>"` (filter escape-stripped); clicking it does nothing; the legend reads `type to filter · …` while the filter is empty, drops `type to filter` and inserts `⌫ trim` before the `esc` entry once set, and at zero matches keeps only `⌫ trim`, `/sessions`' `^a` entry and the `esc` entry. A workspace with no sessions keeps its own note.
**Approach (assumed at the header base):** a per-kind noun on `pickerOffering` — model, server, profile, cycle, mode (scheduleMode and mode), schedule, level, server (subAgentsServer), prompt (fork); `session` for the browser. Compose the filtered and zero-match legends in the surface from each pane's empty-filter legend (`picker.go` hints, `fork.go`, `sessionBrowserHint` in `sessions.go`) rather than keeping per-kind pairs. The note row uses `selected=-1` and keeps the pane's row count at 0.
**Regression guard.** `TestSessionBrowserPaintsTheFilterLine` asserts the hint equals `sessionBrowserHint` with "parser" typed; the filtered legend drops `type to filter`, so update it to the filtered legend here (and `TestPickerPaintsTheFilterLineWithBreathingRoom`, if item 4's rewrite pins the hint).
**Files:** internal/tui/listsurface.go, internal/tui/picker.go, internal/tui/sessions.go, internal/tui/fork.go, internal/tui/picker_test.go, internal/tui/sessions_test.go, internal/tui/schedule_test.go, internal/tui/mouse_test.go
**Read first:** internal/tui/picker.go — pickerOffering, pickerListContent, pickerHintFor; internal/tui/sessions.go — browserListContent, sessionBrowserHint;
internal/tui/mouse.go — handlePickerClick, handleBrowserClick; internal/tui/fork.go — forkPickerHint;
internal/tui/sessions_test.go — TestSessionBrowserPaintsTheFilterLine; internal/tui/schedule_test.go — TestPickerHintFollowsTheKind
**Tests:** update `TestSessionBrowserPaintsTheFilterLine` (the filtered legend), `TestPickerZeroMatchesPaintsTheFilterOverNoRows`, `TestSessionBrowserZeroMatchPaintsTheFilterOverNoRows`, `TestPickerFilterWithNoMatchesTakesNothing`, `TestSessionBrowserZeroMatchesTakeNothing`, `TestPickerHintsLeadWithTypeToFilter`, `TestPickerHintFollowsTheKind`; new legend-state table test per kind and a click-on-note-row test.
**Acceptance:**
- `go build ./... && go vet ./internal/tui/`
- `GOMEMLIMIT=2GiB go test -race -count=1 -parallel 1 -run 'TestPicker|TestSessionBrowser|TestBrowser' ./internal/tui/`
**Commit:** `feat(tui): say when a filter matches nothing and legend the filter state`
Depends on item 4.

## 6. A paste is typing in a filtering list — ✅ DONE (2026-10-14)

NOTES (2026-10-14): the shared build-and-re-seat helper is `typeIntoOverlayFilter` itself, now taking the edit as a `change func(*lineEditor) tea.Cmd` (a keystroke's `editKey` from `listKey`, a paste's `editMsg` from the new `listPaste`), rather than a second helper beside it.
NOTES (2026-10-14): the picker half of the claimer lives in `listPaste` (listsurface.go), not picker.go, which the item's Files line does not name; the browser half is `sessionBrowserPaste` in sessions.go.
NOTES (2026-10-14): consequential edit — internal/tui/lineeditor.go: made necessary by typeIntoOverlayFilter now building the field on a paste as well as a keystroke (isBuilt doc comment).

**What:**
**Goal:** with a filtering picker or `/sessions` open, a bracketed paste lands in the filter folded onto one line and re-seats the highlight by item 2's rule; during a `/sessions` rename it lands in the rename field folded to one line; during delete-confirm and in a question picker it is dropped; it never reaches the prompt draft or a staged interjection while such a pane is open.
**Approach (assumed at the header base):** in `foldPaste` (`internal/tui/prompteditor.go`), after `settingsPaste`, ask a list-surface paste claimer that routes to the open pane's filter via `filter.edit(...)` (`newPopupField` already folds newlines through `flattenLine`), the browser's `renameBuf`, or drops it.
**Regression guard.** The Goal keeps the question-picker paste drop as written. Gate the claimer on each pane's own key gate (`paneSpecs` keyOpen: browser `state==stateIdle && open`, picker `state.live() && open`), so a paste over an approval or ask pane raised above a mid-run picker never lands in the picker. Build the filter field when unbuilt, as `typeIntoOverlayFilter` does — a zero `copyOnWriteField` drops input — with one helper shared by that build and item 2's re-seat.
**Files:** internal/tui/prompteditor.go, internal/tui/listsurface.go, internal/tui/sessions.go, internal/tui/picker_test.go, internal/tui/sessions_test.go
**Read first:** internal/tui/prompteditor.go — foldPaste; internal/tui/listsurface.go — typeIntoOverlayFilter; internal/tui/panes.go — paneSpecs (paneBrowser, panePicker keyOpen);
internal/tui/approval.go — foldApprovalRequest; internal/tui/settings.go — settingsPaste; internal/tui/sessions.go — sessionBrowser.renameBuf;
internal/tui/lineeditor.go — copyOnWriteField.edit; internal/tui/mouse_test.go — TestPasteIgnoredWhereInputIsInert
**Tests:** new `TestPickerPasteLandsInTheFilter` (pastes before any key), `TestSessionBrowserPasteLandsInTheFilter`, `TestSessionBrowserPasteLandsInTheRename`, `TestSessionBrowserPasteDuringConfirmIsDropped`, `TestQuestionPickerPasteIsDropped`; each asserts the prompt draft is unchanged. Keep `TestPasteInsertsAndRefreshes`, `TestPasteIgnoredWhereInputIsInert` green.
**Acceptance:**
- `go build ./... && go vet ./internal/tui/`
- `GOMEMLIMIT=2GiB go test -race -count=1 -parallel 1 -run 'TestPicker|TestSessionBrowser|TestQuestionPicker|TestPaste' ./internal/tui/`
**Commit:** `fix(tui): a paste over a list lands in its filter, not the prompt draft`
Depends on item 3. Fixes the defect that a paste over the picker or `/sessions` lands in the prompt draft (contradicting `layout.md`'s "Nothing is typed into the prompt box while a pop-up is up").

## 7. /settings: the filtered key list and its keys — ✅ DONE (2026-10-14)

NOTES (2026-10-14): settingsSelection now takes the rows (was a count) and re-seats the stored full-slice index into the filtered view on every read (the regression guard); settingsFilteredCursor/settingsSeatFiltered walk ↑/↓ and the wheel over that view with the shared listCursor, and settingsTypeFilter mirrors typeIntoOverlayFilter over full rows via pickerView.reseatPosition.
NOTES (2026-10-14): a filter that matches nothing leaves the highlight at the top (row 0), so trimming back lands on the first match — the picker's behaviour after a zero match.
NOTES (2026-10-14): recast TestSettingsPaneNavigationWrapsAndSwallowsEveryOtherKey and renamed it TestSettingsPaneNavigationWrapsAndSwallowsWhatDoesNotType, because the old name is no longer true (letters now filter). The new tests are in settings_test.go and mouse_test.go; settingsapply_test.go needed no change.
NOTES (2026-10-14): consequential edit — internal/tui/doc.go: made necessary by the /settings key list now filtering (the package map named only two filtering panes and listed the popup fields).
NOTES (2026-10-14): consequential edit — internal/tui/settingsapply.go: made necessary by delete (⌦) now arming the reset (the file-head and settingsArmReset comments said only backspace arms it).

**What:**
**Goal:** with `/settings` browsing, printable keys type into a filter over each row's key path and section heading (case-insensitive substring; a heading match keeps its whole section; a section heading and its spacer show while any of its rows show); backspace trims a set filter and arms the reset on an empty one; delete (⌦) arms the reset or rule removal with or without a filter; ↑/↓, ⏎, click and wheel act on the matching rows only; the highlight follows item 2's rule; the filter survives every sub-step and keys inside a sub-step never reach it; `esc` closes and clears it.
**Approach (assumed at the header base):** `settingsPane` (`internal/tui/settings.go`) gains a `filter copyOnWriteField`; derive one filtered view of `[]SettingRow` (with an index back to the full slice) and route every site that indexes rows by `selected` through it — `settingsKey`'s clamp, `settingsEnter`, `settingsEnumTarget`, `settingsSelectedRow`, `settingsBody`, `settingsKeyListSpec`, `settingsDisplayRows` — and `internal/tui/mouse.go`'s settings click and wheel. Keep `selected` indexing the full slice so steps' `target` re-derivation is unchanged. Use item 1's matcher and item 2's re-seat helper. Delete routes to `settingsArmReset` (`internal/tui/settingsapply.go`).
**Regression guard.** Re-seat into the filtered view inside `settingsSelection` itself — the one read every step target, legend, body, click and `settingsArmReset` resolve through — not only at the listed sites, so no frame (e.g. after ⏎ confirms a rule removal under a filter) reads a hidden row. Acceptance pins the 32 KiB size with `TestModelFitsTheStack` (`internal/tui/model_test.go`). This supersedes the in-code prose that /settings does not filter and swallows letters and pastes (`listsurface.go`, `settings.go`; ADR 0053 amend-1014): rewrite it — `grep -n 'swallows them\|its two panes\|two filtering\|two that filter\|does not filter' internal/tui/*.go`.
**Files:** internal/tui/settings.go, internal/tui/settingsapply.go, internal/tui/mouse.go, internal/tui/listsurface.go, internal/tui/settings_test.go, internal/tui/settingsapply_test.go, internal/tui/mouse_test.go
**Read first:** internal/tui/settings.go — settingsKey, settingsSelection, settingsDisplayRows, settingsKeyListSpec, settingsPaneHint; internal/tui/settingsapply.go — settingsArmReset, settingsRemoveRule;
internal/tui/mouse.go — handleSettingsClick, settingsWheel; internal/tui/settings_test.go — TestSettingsPaneNavigationWrapsAndSwallowsEveryOtherKey;
internal/tui/model_test.go — TestModelFitsTheStack
**Tests:** recast `TestSettingsPaneNavigationWrapsAndSwallowsEveryOtherKey` (`x` now filters; tab, ctrl chords still swallowed); new tests: key-path match, section-heading match keeps the section, ⌫ trims then arms reset, ⌦ arms reset filtered and unfiltered, ⌦ removes a rule, filter survives enum/value/text/reset/save-target steps, click and wheel over a filtered list, `esc` closes with a filter set.
**Acceptance:**
- `go build ./... && go vet ./internal/tui/`
- `GOMEMLIMIT=2GiB go test -race -count=1 -parallel 1 -run 'TestSettings|TestModelFitsTheStack' ./internal/tui/`
**Commit:** `feat(tui): type to filter the /settings key list`
Depends on item 2.

## 8. /settings: filter display, zero matches, legends, paste

**What:**
**Goal:** `/settings` shows its filter as `Settings · filter: mo▌` on the title row (caret only while browsing; text without caret during the value field, armed reset and `Save to:`; hidden during the enum sub-list and the text editor); zero matches paint one faint row `no setting matches "zzz"` with a blank description header and the legend `⌫ trim · esc close`; a paste while browsing lands in the filter folded to one line. Legends, exactly:
- key row: empty `type to filter · ↑/↓ select · ⏎ edit · ⌫ reset · esc close`; set `↑/↓ select · ⏎ edit · ⌫ trim · ⌦ reset · esc close`
- `server` row: empty `type to filter · ↑/↓ select · ⏎ edit · esc close`; set `↑/↓ select · ⏎ edit · ⌫ trim · esc close`
- rule row: empty `type to filter · ↑/↓ select · ⏎ adopt · ⌫ remove · esc close`; set `↑/↓ select · ⏎ adopt · ⌫ trim · ⌦ remove · esc close` (`⏎ adopt` only on a rule not in force)
**Approach (assumed at the header base):** feed the filter through item 4's `popupSpec` title tail from `settingsKeyListSpec`; the hint constants and `settingsPaneHint` in `settings.go`; `settingsPaste` routes a browsing paste to the filter.
**Regression guard.** Item 8's legend change (`type to filter · ` lead) moves the one /settings frame golden — add cmd/apogee/testdata/frames/t16-settings-rows.txt to Files, regenerate it with the tuitest -update flag, and add Acceptance `GOMEMLIMIT=2GiB go test -race -count=1 -parallel 1 -run '^TestE2ELiveStateFollowsTheRunningSession$' ./cmd/apogee/`. The key list's row claim follows the filtered display count (`settingsKeyListSpec`, `popupBudget`), so the height test is scoped: the title row and description header stay put, and the height holds only while the filtered list still overflows its window.
**Files:** internal/tui/settings.go, internal/tui/settings_test.go, internal/tui/panes_test.go, internal/tui/mouse_test.go, cmd/apogee/testdata/frames/t16-settings-rows.txt
**Read first:** internal/tui/settings.go — settingsKeyListSpec, settingsPaneHint, settingsHint/settingsNoResetHint/settingsRuleHint consts, settingsPaste; internal/tui/model.go — popupBudget;
internal/tui/prompteditor.go — foldPaste (route the paste in settingsPaste, not a key-list editorMsg arm); internal/tui/panes_test.go — TestOverlayHeightQueryMatchesItsRenderSettings;
cmd/apogee/e2e_livestate_test.go — TestE2ELiveStateFollowsTheRunningSession; cmd/apogee/e2e_smoke_test.go — settingsHint
**Tests:** update every assertion on `settingsHint`, `settingsNoResetHint`, `settingsRuleHint`; recast `TestSettingsPasteIsSwallowedWithNoFieldOpen`; new tests for each legend state, the title-row filter and caret per sub-step, the zero-match row, and that the title row and description header do not move on the first key (the height holds while the filtered list still overflows its window); regenerate the t16 golden.
**Acceptance:**
- `go build ./... && go vet ./internal/tui/`
- `GOMEMLIMIT=2GiB go test -race -count=1 -parallel 1 -run 'TestSettings|TestOverlayHeight|TestPaste' ./internal/tui/`
- `GOMEMLIMIT=2GiB go test -race -count=1 -parallel 1 -run '^TestE2ELiveStateFollowsTheRunningSession$' ./cmd/apogee/`
**Commit:** `feat(tui): show the /settings filter, its empty result and its legends`
**Closes:** settings-typing-filter
Depends on items 5 and 7.

## 9. Drive the filter end to end

**What:**
**Goal:** e2e tests drive `/settings` and one picker through the real binary: type a filter, see the title-row filter and the narrowed rows, edit a filtered row, return with the filter intact, trim to empty; a golden frame pins a filtered `/settings` pane.
**Approach (assumed at the header base):** extend `cmd/apogee/e2e_livestate_test.go` with the helpers `openSettings`, `settingsRow`, `settingsCursor`, `closePane` (`e2e_smoke_test.go`); wait on crumbs with `WaitText`/`waitIdle`, `WaitQuiet` only right before the frame read (`docs/design/test-drivers.md` §Waiting). Add `cmd/apogee/testdata/frames/t16b-settings-filtered.txt`. The existing e2e `settingsHint` substring still matches.
**Regression guard.** Item 9 re-runs TestE2ELiveStateFollowsTheRunningSession against the golden item 8 regenerated and does not regenerate it again. `Popups.*` matches no test (the popup tests are `TestE2EPopup*`), so Acceptance names them and the live-state test. `closePane` waits on `WaitGone(marker)`: close only after trimming back to a state that paints the marker, or wait on a string the closing state actually shows (the title-row `· filter:`) — the zero-match legend lacks `settingsHint` and a set picker legend lacks `modelPaneMarker`'s lead.
**Files:** cmd/apogee/e2e_livestate_test.go, cmd/apogee/e2e_popups_test.go, cmd/apogee/testdata/frames/t16b-settings-filtered.txt
**Read first:** cmd/apogee/e2e_livestate_test.go — TestE2ELiveStateFollowsTheRunningSession, openSettings, settingsRow, modelPaneMarker;
cmd/apogee/e2e_smoke_test.go — settingsHint, settingsCursor, closePane; cmd/apogee/e2e_popups_test.go — TestE2EPopupFramesLists;
internal/tuitest/driver.go — Driver.WaitGone
**Tests:** new `TestE2ESettingsTypeToFilter`, `TestE2EPickerTypeToFilter`.
**Acceptance:**
- `go build ./... && go vet ./cmd/apogee/`
- `GOMEMLIMIT=2GiB go test -race -count=1 -parallel 1 -run '^TestE2E(SettingsTypeToFilter|PickerTypeToFilter|Popup.*|LiveStateFollowsTheRunningSession)$' ./cmd/apogee/`
**Commit:** `test(e2e): drive the list filter through /settings and a picker`
Depends on item 8.

## 10. Docs: the /settings layout spec and manual

**What:**
**Goal:** `docs/layout/settings-screen-layout.md` and the `/settings` section of `docs/manual/commands.md` describe the filter as built (typing, matched text, title-row filter, ⌫ trim / ⌦ reset, zero-match row, legends, paste, sub-step display, mockups), and the layout spec's drift is gone: the enum mockup carries the blank under its question; the `mode` and `servers` descriptions match `internal/config/registry.go`; ⌫'s no-op rows are stated as non-editable, default-valued and `server`; `Save to:` lists ←/→, ↑/↓, tab, shift+tab; scrolling, the scrollbar and headers scrolling away are stated; the abridged sections are said to be abridged. `layout.md`'s `/settings` legend list matches item 8.
**Files:** docs/layout/settings-screen-layout.md, docs/manual/commands.md, layout.md
**Read first:** docs/layout/settings-screen-layout.md — "The key list", "The selection popup", owner's verbatim requirements (code cites them by number: settings.go, mouse.go); internal/tui/settings.go — settingsPaneHint, settingsResetKind, settings hint constants;
internal/config/registry.go — KeyRegistry servers/mode Desc; layout.md — "Each step names its own keys on the hint row"; docs/manual/commands.md — "The settings screen — /settings";
cmd/apogee/docs_settings_test.go — TestManualDocumentsEverySettingsKey
**Tests:** none (docs); keep `cmd/apogee/docs_settings_test.go` green.
**Acceptance:**
- `GOMEMLIMIT=2GiB go test -count=1 -run 'TestDocs|TestManual' ./cmd/apogee/`
- `grep -n "type to filter" docs/layout/settings-screen-layout.md`
**Commit:** `docs(settings): document the /settings filter and fix the layout spec's drift`
Depends on item 8.

## 11. Docs: the list filter in layout.md and the manual

**What:**
**Goal:** `layout.md`'s pop-up list sections (the typed-filter blank exemption, the "every cell counts" match rule, the `⌫` wording, the filter line with its blanks and budget trade, the zero-match statement, "nothing is typed into the prompt box", the `/sessions` section) and `docs/manual/sessions.md` state the List filter contract of ADR 0053's 2026-10-14 amendment, including that the four question pickers do not filter. Rule for finding every site: every passage describing the `filter:` line, filter matching, or a filtered list's legend — `grep -n "filter" layout.md docs/manual/*.md`.
**Regression guard.** The rule widens to `grep -n "filter" layout.md docs/layout/*.md docs/manual/*.md`: `docs/layout/user-questions-layout.md`'s 2026-10-11 amendment says the adoption pane and the two start-up offers keep "the filter and `esc` … live at once", which item 3 ends. Add a dated note there rather than rewriting that amendment.
**Files:** layout.md, docs/manual/sessions.md, docs/manual/commands.md, docs/layout/user-questions-layout.md
**Read first:** layout.md — "Typing into that pane narrows it", the breathing-row paragraph (`filter:` line pads), key-migration/adoption "the filter and esc are live at once", "The /sessions browser types too";
docs/manual/sessions.md — /sessions bullet ("every selector pop-up filters as you type"); docs/layout/user-questions-layout.md — 2026-10-11 amendment;
.capcom/knowledge/decisions/adr-0053/adr-0053-amend-1014.md — rules 1–10
**Tests:** none (docs).
**Acceptance:**
- `grep -n "every cell counts\|marker cells" layout.md` prints nothing
- `GOMEMLIMIT=2GiB go test -count=1 -run 'TestManual|TestReadme' ./internal/config/`, then the same over `./internal/tools/` (one package per run)
**Commit:** `docs(layout): state one list-filter contract for every searchable list`
Depends on items 6 and 10.
