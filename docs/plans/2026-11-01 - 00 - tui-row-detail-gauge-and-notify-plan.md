# TUI row detail, live context estimate, pop-up gutter and input notify — plan

**Goal:** Close the three TUI tickets filed 2026-11-01 (failed Terminal rows hide their detail, the pop-up scrollbar sits flush, fetch rows and the gauge are blind to ingest size) plus the owner-chosen extras: a notify on needed input, drag-select edge auto-scroll, and the release-gate deadlock on a prose-only push.
**Date:** 2026-11-01
**Status:** unexecuted
**sized for:** ~200k-context host
**base:** 6385f944

**Sources:**
- `.capcom/issues/terminal-error-detail-hidden.md`, `popup-scrollbar-gutter.md`, `fetch-ingest-size-blind.md`, `prompt-wait-unannounced.md`, `drag-select-edge-stuck.md`, `release-gate-prose-deadlock.md`
- `docs/layout/tool-layout.md`, `layout.md`, `docs/layout/settings-screen-layout.md`
- ADR 0031 (wire-silent engine), ADR 0075 (Event lines protocol), ADR 0098 (release process)

**Ratified design calls** (owner, 2026-11-01):
- **Empty failed body:** a hooked failure whose output is blank expands to the text the model received, cwd line stripped, verbatim (e.g. `[exit code 2 — fail-fast: …]`).
- **Empty clean run:** the slot keeps `exit 0`; the row gets a caret that reveals `(no output)`.
- **Estimate transport:** a new `context_estimate` Event `{Tokens, Calibrated}` after each tool-result append, plus `ToolResultEvent.EstimatedTokens` (that result's own estimate).
- **Notify key:** `ui.notify: off | bell | osc9 | both`, default `bell`; fires when an approval or ask prompt opens.
- **Turn-end notify:** a finished or errored Turn notifies only while the window is unfocused (focus reporting on).
- **Auto-scroll:** ~50 ms tick; 1 line per tick, +1 per row past the bottom edge, capped at 5.
- **Extras:** notify, release-gate fix and drag auto-scroll join this plan.

**Standing requirements:**
- skills: coding-standards
- Tests run narrowed to one package and named tests (`go test -race -count=1 -run '<re>' ./<pkg>/`); `make test` only at closeout.
- Release-skill step 1a ("Push, then wait for green CI") was uncommitted at write time (`.apogee/skills/release-apogee/SKILL.md`, its `.claude/skills/` pointer and `docs/manual/building.md` sit modified in the working tree); the owner must commit it before item 8 runs.

**Out of scope:**
- An estimate on child run views (`runview`); notifications in `apogee watch` or headless.
- `workflow_dispatch` on `ci.yml`.
- Request-side sampling params (`code-affordance-parity`).

**Regression check (2026-11-01, 6385f944):**
- 1: guard folded
- 2: guard folded
- 3: recast (writer's decision; supersedes the `internal/tools/doc.go` paragraph that fetch results deliberately carry no summary)
- 4: guard folded
- 5: guard folded
- 6: guard folded
- 7: guard folded; yields to layout.md's gauge paragraph and the CHANGELOG entry (the estimate never takes the slot from `enter dismiss`)
- 8: guard folded (writer's decision on step 1a); supersedes ADR 0098 decision item 5's "the commit that was gated is, in effect, the commit that ships"
- 9: guard folded (writer's decision on the empty value)
- 10: guard folded (writer's decision on the empty value)
- 11: guard folded; supersedes the `handleMouseMotion` comment in `internal/tui/mouse.go` ("Motion outside the owning rectangle is ignored …") for the transcript
- 3 (round 2): guard folded; supersedes design call 14 of `docs/plans/archived/2026-08-10 - 04 - tool-display-overhaul-plan.md` and the two `internal/tui/toolregistry.go` header paragraphs citing it; prose grep widened to every count site; wire kind `fetched_body` named; `network_test.go` added
- 6 (round 2): guard folded (writer's decision: item 3's TestToolStat fetch cases rewritten in place; executable `-run` Acceptance with `TestFetchRow`)
- 6 (writer): the replay estimate is persisted as an omitempty `session.ToolView` field; the ToolView field pins follow

## 1. Failed and empty subprocess rows always reveal a body — ✅ DONE (2026-11-01)

NOTES (2026-11-01): the blank-output floor lives in a new helper `hookedFailureBody` (toolview.go) that `absorbFailure` calls on the hooked route, and the phrase became the named constant `noOutputPhrase` (toolregistry.go), shared by `outputDetail` and `subprocessDetail`.
NOTES (2026-11-01): the painted lone-block test is `TestLoneBlankRunCountsItsOneLine` (toolblock_test.go). Its name joins the Acceptance `-run`: `go test -race -count=1 -run 'TestPresentToolCall|TestGroupMember|TestDecliningStat|TestToolStat|TestLoneBlankRun' ./internal/tui/`. The group-member test is `TestGroupMemberWithABlankRunStillOpens`.
NOTES (2026-11-01): the sub_agent blank-body case sits in `TestPresentToolCallFailedSubprocessNamesItsExitCode` with a `headsRun` flag. A delegation heads its own run and never groups, so the table's groupable assertion skips that case.
NOTES (2026-11-01): reworded the `outputDetail` doc example ("┕ true ⋯ (no output)" became an empty git_log's "┕ HEAD ⋯ (no output)") because terminal no longer puts that phrase in the slot.

**What:**
**Goal:** A failed `terminal` / `python_exec` row whose output is blank has a caret and expands to the failure text the model received with the cwd line stripped (the `[exit code N …]` marker verbatim). Any other hooked failure (`sub_agent`'s `delegationFailure`) with a blank body does the same. A clean empty-output run keeps `exit 0` in the slot and expands to `(no output)`. `docs/layout/tool-layout.md` states both rules. Fixes defect `terminal-error-detail-hidden` (python_exec shares it).
**Approach (assumed at the header base):** In `toolView.absorbFailure` (`internal/tui/toolview.go`), when the hook's output is blank, the body is `failureBody` of `domain.StripCwdLine(result.Content)` — one generic floor, not per-hook. `subprocessDetail` (`internal/tui/toolregistry.go`) returns `(no output)` as the body, not `summaryOnly`, so `exitCodeStat` still sets `exit 0`. `outputDetail` stays as is: `TestDecliningStatKeepsTheProseFloor` pins git_log's slot. Spec lines go next to the verdict paragraph in tool-layout.md and on the terminal/python_exec table row.
**Regression guard.** A lone (ungrouped) collapsed row now has a one-line body, so `collapsedRemainder` (`internal/tui/toolbranch.go`) joins its count onto the slot: it reads `exit 0 · +1 more line` / `error: exit 1 · +1 more line`, and the tool-layout.md spec line states that lone-block slot in the remainder wording tool-layout.md already uses. Every comment saying a blank subprocess result has no body, or that its exit code stands alone, is reworded (`grep -n 'standing alone\|exit code alone\|(no output)\|none at all' internal/tui/*.go`: the `outputBody`, `subprocessDetail` and `absorbFailure` comments and `TestPresentToolCallOutcomeSplit`'s doc).
**Files:** internal/tui/toolview.go; internal/tui/toolregistry.go; internal/tui/toolpresent_test.go; internal/tui/toolblock_test.go; docs/layout/tool-layout.md
**Read first:** internal/tui/toolview.go — absorbFailure, enrichWithResult; internal/tui/toolregistry.go — subprocessFailure, subprocessDetail, outputDetail, outputBody, failureBody, delegationFailure; internal/tui/toolbranch.go — collapsedRemainder, collapsedDetails; internal/tui/toolblock.go — renderGroupMember;
internal/tui/toolpresent_test.go — TestPresentToolCallFailedSubprocessNamesItsExitCode, TestPresentToolCallOutcomeSplit, TestDecliningStatKeepsTheProseFloor, detailsText; internal/tui/toolblock_test.go — TestGroupMemberKeepsItsSummaryAndClipsTheTarget; internal/tui/subagentblock_test.go — the "a faulted run keeps its cause and gains the steering" case
**Tests:**
- Flip the cases pinning an empty body in `TestPresentToolCallFailedSubprocessNamesItsExitCode` ("a failure that printed nothing…", "a fail-fast note inside the marker…") and `TestPresentToolCallOutcomeSplit` (the two empty-output cases) to the new bodies. Add python_exec and sub_agent blank-body cases.
- A painted group-member test (pattern: `TestGroupMemberKeepsItsSummaryAndClipsTheTarget`) asserts the caret, and asserts the expanded body for both the failed and the clean empty runs.
- A painted lone-block case beside it asserts the collapsed slot `exit 0 · +1 more line` (clean) and `error: exit 1 · +1 more line` (failed); its name joins the Acceptance `-run`.
**Acceptance:**
- `go build ./... && go vet ./internal/tui/`
- `go test -race -count=1 -run 'TestPresentToolCall|TestGroupMember|TestDecliningStat|TestToolStat' ./internal/tui/`
**Closes:** terminal-error-detail-hidden
**Commit:** `fix(tui): failed and empty subprocess rows reveal what the model was told`

## 2. Pop-up scrollbar leaves a gap column — ✅ DONE (2026-11-01)

NOTES (2026-11-01): a new helper `popupScrollbarShown` (popup.go) answers popupRowLines' overflow condition without composing the block. `settingsNoteWidth` uses it over `settingsKeyListSpec`. `settingsEnumCellWidth` uses it over `listSpec` of the sub-list content with the value cells left out, and takes the `SettingRow` as a new first parameter to build that content (its one caller, `settingsEnumContent`, passes it).
NOTES (2026-11-01): `TestRenderPopupScrollbarReservesOneColumnWhileOverflowing` is renamed `TestRenderPopupScrollbarReservesTwoColumnsWhileOverflowing`. It also asserts that a row one column short of the full width is elided, and that the selected row ends on the field-filled gap and then the thumb. The new tests are `TestThinkingPaneOverflowingLeavesTheGapAndCutsNoRow`, the "an overflowing picker leaves a gap before its bar" subtest of `TestPopupCallersPaintTheOverflowBar`, and `TestSettingsPaneOverflowingListSizesTheNoteBesideTheBar`. That last one sweeps widths 105–135 and fails at 118/119 without the settings subtraction. `settingsModeEditModel` takes optional extra rows for it.
NOTES (2026-11-01): pre-existing, outside this item: `settingsNoteWidth` measures columns over the key rows only. The painter also measures the section heading rows, so a section name wider than every key (a fixture's short keys under "Autonomy") widens the key column past what the note was sized for. Real registry keys are wider than their section names. The new test's filler keys are long to stay clear of it.
NOTES (2026-11-01): `TestE2ELiveStateFollowsTheRunningSession` passes with `t16-settings-rows.txt` unchanged, and so do the popup e2e tests (`TestE2EPopupFrames|TestE2EPopupClick|TestE2EPickerTypeToFilter`). golangci-lint on ./internal/tui/ reports 0 issues.

**What:**
**Goal:** Every boxed pop-up whose rows overflow paints one blank column between its content and the scrollbar, and the selected row's highlight stops before that column. A pop-up whose rows fit paints byte-identical to the base. /thinking and /advice always wrap two columns short of the inner width. `layout.md` §pop-ups and `docs/layout/settings-screen-layout.md` state the gap, matching the transcript's rule.
**Approach (assumed at the header base):** One constant `popupScrollbarGutter = scrollbarWidth + bodyRightGutter` beside `popupGutter` in `internal/tui/popup.go`. It is used by `popupRowLines` and `popupRowLineCount` (changed together, since the height query must match the render) and by `Model.thinkingWrapColumn` (`thinkingpane.go`). `popupRowScrollbar` squares content to `inner - popupScrollbarGutter`, paints one blank field cell, then the bar. Re-check the narrow floor (`rowInner <= 1`). The per-site widths (settings, ask glosses, picker fit) stay as they are: the painter trims them. Update the `thinkingWrapColumn` prose in `internal/tui/doc.go`.
**Regression guard.** `popupRowLinesTwoPass` (`popup_test.go`) switches to `popupScrollbarGutter` together with `popupRowLines`, so `TestPopupRowLinesReusedLayoutPaintsAsTwoLayouts` keeps comparing like with like. `settingsNoteWidth` and `settingsEnumCellWidth` (`settings.go`) subtract `popupScrollbarGutter` while the list overflows and `m.popupScrollbarOn()`, so `autoBlastRadiusNote` never picks a sentence the bar then paints with a trailing `…`; for settings this replaces "the per-site widths … stay as they are". Every prose site naming the pop-up bar's one reserved column (code comments, layout.md including the /thinking pane's wrap paragraph, docs/layout/) is reworded: `grep -rn "scroll bar's column\|scrollbarWidth\|bar's column" layout.md docs/layout internal/tui/*.go`.
**Files:** internal/tui/popup.go; internal/tui/thinkingpane.go; internal/tui/doc.go; internal/tui/settings.go; internal/tui/popup_test.go; internal/tui/thinkingpane_test.go; internal/tui/settings_test.go; layout.md; docs/layout/settings-screen-layout.md
**Read first:** internal/tui/popup.go — popupRowLines, popupRowLineCount, popupRowScrollbar, popupRowLinesAt; internal/tui/thinkingpane.go — Model.thinkingWrapColumn; internal/tui/settings.go — settingsNoteWidth, settingsEnumCellWidth; internal/tui/settingsapply.go — autoBlastRadiusNote;
internal/tui/popup_test.go — TestRenderPopupScrollbarReservesOneColumnWhileOverflowing, popupRowLinesTwoPass, TestPopupRowLinesReusedLayoutPaintsAsTwoLayouts, popupBarCell, TestPopupCallersPaintTheOverflowBar; internal/tui/thinkingpane_test.go — TestThinkingWrapColumnFollowsTheWindow; cmd/apogee/e2e_livestate_test.go — TestE2ELiveStateFollowsTheRunningSession
**Tests:**
- Recast `TestRenderPopupScrollbarReservesOneColumnWhileOverflowing` for two columns, asserting the cell before the bar is blank.
- Update the `TestThinkingWrapColumnFollowsTheWindow` formula.
- New frame tests: /thinking (base on `paintThinkingPane`) and the picker (in `TestPopupCallersPaintTheOverflowBar`).
- `popupRowLinesTwoPass` follows the gutter; `TestPopupRowLinesReusedLayoutPaintsAsTwoLayouts` and `TestPopupRowSeatFromCountsIsThePaintersWindow` stay green.
- A /settings case (pattern: `TestSettingsPaneModeEscalationToAutoStatesTheBlastRadius`) with the key list overflowing and the bar on: the blast-radius note paints with no trailing `…`.
- `TestRenderPopupScrollbarIsAbsentWhileTheRowsFit` and the panes_test height==render check stay green unchanged.
**Acceptance:**
- `go test -race -count=1 -run 'TestRenderPopup|TestThinking|TestPopupCallers|TestRenderList|TestRenderReport|TestPane|TestPopupRowLines|TestPopupRowSeat|TestSettingsPane' ./internal/tui/`
- `go test -race -count=1 -run TestE2ELiveStateFollowsTheRunningSession ./cmd/apogee/` (it owns `testdata/frames/t16-settings-rows.txt`; if that golden changes, regenerate it with `-update` and inspect the diff)
**Closes:** popup-scrollbar-gutter
**Commit:** `feat(tui): leave a gap column between pop-up content and its scrollbar`

## 3. Fetch results carry a FetchedBody tool summary — ✅ DONE (2026-11-01)

NOTES (2026-11-01): consequential edit — internal/domain/toolsummary_test.go: made necessary by the new variant (TestToolSummaryVariantsAreSealed count 8 → 9, plus the compile-time membership list)
NOTES (2026-11-01): internal/tools/doc.go's count now reads THIRTEEN, not TWELVE: the old "TEN" list left out git_show, which attaches a ReadSpan (git.go), so the rewritten list names it; the glossary Tool summary Note's list also gains git_show (its "eleven" counted it but did not name it)
NOTES (2026-11-01): the toolsummary_pin_test.go cross-package pin now runs web_fetch and http_request against its existing loopback server (9 → 11 cases, "eleven of the thirteen"), and both join TestToolSummaryPinUsesRegisteredToolNames and TestToolStatDeclinesWithoutATypedSummary
NOTES (2026-11-01): fetchedBodyStat clips "HTTP <Status>" with clipDetail, as the firstLineDetail floor does, because the reason phrase is the server's to choose
NOTES (2026-11-01): the shared fetchedBodyCases table and fetchedBodyServer helper live in web_fetch_test.go; network_test.go's TestHTTPRequest_AttachesFetchedBodySummary reuses them

**What:**
Recast at the regression check (2026-11-01).
**Goal:** `web_fetch` and `http_request` results carry `domain.FetchedBody{Status string; Truncated bool}`, where Status is the status text the result's first line names (`200 OK`) and Truncated is true when the body hit `maxNetworkResponseBytes`. The variant is sealed, mirrored by `internal/eventjson` encode and decode as the summary kind `fetched_body` with members `status` and `truncated`, aliased on the root facade, and listed in `docs/manual/headless.md`'s summary table. Its variant counts in docs read the new total.
**Approach (assumed at the header base):** Add the variant in `internal/domain/toolsummary.go` (fix the "eight variants" prose there and in `internal/domain/doc.go`). Set it via `okSummary` in `renderFetchResult` / `renderRequestResult` callers from `netResponse.truncated`. Mirror it in `toolSummaryOf` (encode.go) and in decode.go, and alias it in `apogee.go` plus the `example_test.go` list.
**Regression guard.** Item 3 also adds a minimal `stat` hook to both fetch presenters in internal/tui/toolregistry.go that reads FetchedBody and renders `HTTP <Status>`, so the slot reads exactly as at the base, plus a TestPresentToolCall case carrying the summary (add internal/tui/toolregistry.go and internal/tui/toolpresent_test.go to Files and a targeted internal/tui -run to Acceptance); item 6 then extends that same hook rather than adding one. Item 3 explicitly supersedes the internal/tools/doc.go paragraph recording that web_fetch/http_request deliberately carry no summary: truncation is a fact the tool computed that a host would otherwise re-derive from the content's marker, which is that paragraph's own criterion for attaching one; item 3 rewrites that paragraph (the attached list and its count). Item 3 likewise supersedes, for the two fetch tools, design call 14 of `docs/plans/archived/2026-08-10 - 04 - tool-display-overhaul-plan.md` (no engine or wire change for presentation; "no `size` beside an HTTP status"), and rewords the two `internal/tui/toolregistry.go` header paragraphs that cite it ("design call 14 forbids growing one", "without growing a wire (design call 14)"). Prose guard rule: every prose site that counts or lists the summary-bearing tools or the ToolSummary variants is reworded, found with `grep -rniE "\b(eight|nine|ten|eleven)\b[^.]*(variant|summary|built-ins)|variants are the eight|nine of the eleven" internal/ .capcom/knowledge/glossary/` — among them toolregistry.go's "The eleven tools that report a typed summary", internal/tui/doc.go's and toolsummary_pin_test.go's "nine of the eleven", toolpresent_test.go's "The eleven summary-bearing tools" and domain/toolsummary.go's "the eight below" (transcript.go's Event-variant count is not one); add internal/tools/doc.go, internal/tui/doc.go, internal/tui/toolsummary_pin_test.go and the glossary tool-summary Note to Files; add an Acceptance grep that docs/manual/headless.md names the FetchedBody wire summary. http_request's summary cases land beside `TestHTTPRequest_*` in internal/tools/network_test.go.
**Files:** internal/domain/toolsummary.go; internal/domain/doc.go; internal/tools/doc.go; internal/tools/web_fetch.go; internal/tools/http_request.go; internal/tools/web_fetch_test.go; internal/tools/network_test.go; apogee.go; example_test.go; internal/eventjson/encode.go; internal/eventjson/decode.go; internal/eventjson/encode_test.go; internal/eventjson/decode_test.go; internal/tui/toolregistry.go; internal/tui/doc.go; internal/tui/toolsummary_pin_test.go; internal/tui/toolpresent_test.go; docs/manual/headless.md; .capcom/knowledge/glossary/glossary-context/tool-summary.md
**Read first:** internal/tui/toolview.go — enrichWithResult, absorbProse, applyStat, typedSummary; internal/tui/toolregistry.go — toolRegistry "web_fetch"/"http_request", searchHitsStat, firstLineDetail; internal/tools/web_fetch.go — Execute, renderFetchResult;
internal/tools/http_request.go — renderRequestResult; internal/tools/network.go — netResponse; internal/eventjson/encode.go — toolSummaryOf; internal/eventjson/decode_test.go — roundTripSummaries, TestDecodeCoversEveryToolSummaryVariant;
internal/tui/toolpresent_test.go — TestPresentToolCall, TestToolStat, TestToolStatDeclinesWithoutATypedSummary
**Tests:** Summary cases (truncated and not) for both tools (web_fetch in `web_fetch_test.go`, http_request in `network_test.go`); `TestEncodeToolSummaryMirror`, `TestDecodeCoversEveryToolSummaryVariant`, `TestToolSummaryVariantsAreSealed` extended; a `TestPresentToolCall` case for each fetch tool whose result carries FetchedBody keeps the `HTTP 200 OK` slot.
**Acceptance:**
- `go test -race -count=1 ./internal/domain/`
- `go test -race -count=1 -run 'Fetch|Request|Summary' ./internal/tools/`
- `go test -race -count=1 ./internal/eventjson/`
- `go test -count=1 -run Example .`
- `go test -race -count=1 -run 'TestPresentToolCall|TestToolStat' ./internal/tui/`
- `grep -n 'fetched_body' docs/manual/headless.md` prints the FetchedBody summary row
**Commit:** `feat(tools): fetch results carry a FetchedBody summary`

## 4. The context-estimate Event contract — ✅ DONE (2026-11-01)

NOTES (2026-11-01): consequential edit — internal/eventjson/writer_test.go: made necessary by `tool_result` lines always writing `data.estimated_tokens`; TestWriterEnvelopeOrderAndNulls pins one tool_result line verbatim, which the plan's Files list did not name.
NOTES (2026-11-01): consequential edit — .capcom/knowledge/decisions/adr-0075/adr-0075-decision.md: made necessary by the new kind; decision 4's dated count chain ("twenty-four … the list is `Kinds()`") gains a one-sentence pointer to `adr-0075-amend-1101`.
NOTES (2026-11-01): `TestKindsAreTwentyFour` renamed `TestKindsAreTwentyFive`; no exhaustive `switch` over `domain.Event` outside eventjson lacks a default (Go type switches fall through silently, and each such fold documents "nothing on purpose"), so no case was added there. internal/eventjson/testdata/ needed no change (journal.jsonl carries no tool_result line).
NOTES (2026-11-01): `capcom check` exits 1 on `AGENTS.md:40: the capcom block is out of date` — pre-existing at HEAD (reproduced in a clean worktree; AGENTS.md untouched since before the plan base), caused by the installed capcom binary; nothing this item changed. `capcom agents` would refresh it, outside this item's scope.

**What:**
**Goal:** `domain.ContextEstimateEvent{EventBase; Tokens int; Calibrated bool}` exists, along with `domain.ToolResultEvent.EstimatedTokens int` (the estimated tokens that result's content adds). Both are re-exported by the facade. eventjson encodes and decodes them: the new line kind `context_estimate`, and `data.estimated_tokens` on `tool_result`, omitted when 0. `docs/manual/headless.md` documents both. ADR 0075 carries a dated amendment that records the additive kind within the current `v`. Nothing emits them yet.
**Approach (assumed at the header base):** Add the type in `internal/domain/events.go` beside `UsageEvent`. Bump `Kinds()` (encode.go) and rename `TestKindsAreTwentyFour` to match. The amendment is the child Note `adr-0075-amend-1101` (`capcom know add --id adr-0075-amend-1101 --parent adr-0075`), named on the record's Status line and in its reading-order list. Grep for exhaustive `switch` over `domain.Event` outside eventjson and add the case where one has no default.
**Regression guard.** Item 4 adds a `foldCases` row for `ContextEstimateEvent` (`internal/tui/fold_test.go`) that folds to nothing, so the source-parsing `TestFoldEventCoversEveryEventVariant` stays green; item 7 fills it in. `data.estimated_tokens` is written always (0 when unknown), after `spawn_run_id`, per encode.go's no-omitempty rule (ADR 0075 decision 3); this replaces the Goal's "omitted when 0", and `adr-0075-amend-1101` names the change to decision 8's amendment calling `spawn_run_id` "always present and last" on `tool_result`. Every prose site stating the line-kind or Event-variant count is reworded (`grep -rn -i "twenty-four\|twenty-two" internal/eventjson docs/manual/headless.md`), and the new kind goes before the two frames in `Kinds()`, since `TestEncodeMapsEveryEventVariant` slices `Kinds()[:len-2]`.
**Files:** internal/domain/events.go; apogee.go; internal/eventjson/encode.go; internal/eventjson/decode.go; internal/eventjson/doc.go; internal/eventjson/encode_test.go; internal/eventjson/decode_test.go; internal/eventjson/testdata/; internal/tui/fold_test.go; cmd/apogee/testdata/eventlines/run.jsonl; docs/manual/headless.md; .capcom/knowledge/decisions/adr-0075.md; .capcom/knowledge/decisions/adr-0075/adr-0075-amend-1101.md
**Read first:** internal/eventjson/encode.go — Kinds, Encode (ToolResultEvent arm), toolResultData; internal/eventjson/decode.go — toolResultFrom, the tool_result decode; internal/eventjson/encode_test.go — TestEncodeMapsEveryEventVariant, TestKindsAreTwentyFour, TestEncodeJSONGolden; internal/tui/fold_test.go — foldCases, TestFoldEventCoversEveryEventVariant;
apogee_alias_internal_test.go — TestEveryDomainEventVariantIsAliased; cmd/apogee/docs_eventlines_test.go — TestManualListsEveryEventLineKind; internal/domain/events.go — ToolResultEvent, UsageEvent
**Tests:** `TestEncodeMapsEveryEventVariant`, `TestEncodeJSONGolden` (+ golden), `TestDecodeRoundTripsEveryKind`, the Kinds count test; `TestManualListsEveryEventLineKind` (cmd/apogee) passes with the manual's table; the inert `foldCases` row; `TestE2EEventLinesGolden`'s `run.jsonl` re-recorded (every `tool_result` line gains `"estimated_tokens":0`).
**Acceptance:**
- `go test -race -count=1 ./internal/domain/ && go test -race -count=1 ./internal/eventjson/`
- `go test -race -count=1 -run 'TestManualListsEveryEventLineKind|TestE2EEventLinesGolden' ./cmd/apogee/`
- `go test -race -count=1 -run 'TestFoldEvent' ./internal/tui/`
- `capcom check`
**Depends on:** item 3
**Commit:** `feat(domain): a context_estimate Event and a per-result token estimate`

## 5. The engine emits the estimate at each tool-result append — ✅ DONE (2026-11-01)

NOTES (2026-11-01): the reading holds for the reply it came from only — `streamResponse` forgets it as each request starts (`contextReading.forget`) and takes it at `DeltaDone` with usage — so a reply that reports no usage falls back to `ContextEstimate` instead of stacking on a stale reading. This replaces the Goal's "before any reading" with "where the reply the result answers reported no usage"; it is identical whenever a server always or never reports usage, and also covers a clear, restore, fold or server/model switch without a reset at each site. The domain doc, the manual's `context_estimate` row and `adr-0075-amend-1101` were reworded to match.
NOTES (2026-11-01): the `contextReading` type and `Agent.contextEstimateEvent` live in internal/agent/contextcost.go beside `ContextEstimate`, the composition they fall back to, and the field sits on the Agent struct in internal/agent/agent.go (the plan's Files list named neither, though its approach keeps "a since-reading char counter on the agent"). The estimate measures the committed message (advice and engine-note fences included), so `EstimatedTokens` equals what the following estimate adds.
NOTES (2026-11-01): consequential edit — internal/agent/doc.go: made necessary by contextcost.go now owning the between-readings estimate (package-map role line).
NOTES (2026-11-01): consequential edit — internal/domain/events.go: made necessary by the per-reply reading rule (ContextEstimateEvent doc's fallback clause).
NOTES (2026-11-01): consequential edit — docs/manual/headless.md: made necessary by the per-reply reading rule (the `context_estimate` row's fallback clause).
NOTES (2026-11-01): consequential edit — .capcom/knowledge/decisions/adr-0075/adr-0075-amend-1101.md: made necessary by the per-reply reading rule (same fallback clause, in the amendment item 4 wrote today).
NOTES (2026-11-01): the new tests drive `Agent.Run` (one `Step` is one request/dispatch round) and read two different files, since a repeated identical read trips the tool-loop-breaker. `capcom check` still exits 1 on the pre-existing `AGENTS.md:40` capcom-block staleness noted under item 4.

**What:**
**Goal:** After every tool-result append, the engine emits the `ToolResultEvent` with `EstimatedTokens` set, then a `ContextEstimateEvent` at the same depth. Its Tokens are the last reported context reading at that depth (prompt + completion of the last non-Maintenance usage) plus the estimated tokens of every message appended since. Before any reading it is the Agent's own context estimate. `Calibrated` reports whether the estimator has been calibrated. No Driver logic is added (ADR 0031).
**Approach (assumed at the header base):** In `appendToolResult` (`internal/agent/dispatch.go`), after `a.conv.Append(msg)`, estimate with the same budget estimator `contextFillNotice` uses (`a.budget().EstimateTokens`) over the appended message's chars. Keep a since-reading char counter on the agent, reset where the stream loop calibrates at `DeltaDone` (`internal/agent/loop.go`). Before the first reading, anchor on the composition `ContextEstimate` uses (safe on the worker goroutine).
**Regression guard.** `eventBaseOf` (`internal/agent/subagent_test.go`) learns `domain.ContextEstimateEvent` (or reads `e.Identity()`), so `TestSubAgent_EventsCarryTheSpawningCallID` and `TestSubAgent_RunIDIdentifiesEachDelegation` keep passing once a delegation's results emit the estimate. `TestE2EEventLinesGolden`'s `run.jsonl` is re-recorded (a `context_estimate` line, a non-zero `estimated_tokens`, shifted `seq`) and gains a `"estimated_tokens":\d+` redaction beside the existing `"tokens":\d+` one, because the estimate follows the TempDir path's length.
**Files:** internal/agent/dispatch.go; internal/agent/loop.go; internal/agent/dispatch_test.go; internal/agent/subagent_test.go; cmd/apogee/e2e_eventlines_test.go; cmd/apogee/testdata/eventlines/run.jsonl
**Read first:** internal/agent/dispatch.go — appendToolResult, commitCall, commitNotRun; internal/agent/loop.go — streamResponse (DeltaDone arm, a.tokens.Calibrate, a.usage.record); internal/agent/contextcost.go — ContextEstimate, ContextCost; internal/agent/fillnotice.go — contextFillNotice;
internal/agent/dispatch_test.go — TestToolResultEventCarriesToolAndWriteTarget, lastToolResultEventOf; internal/agent/subagent_test.go — eventBaseOf; cmd/apogee/e2e_eventlines_test.go — TestE2EEventLinesGolden
**Tests:**
- Extend `TestToolResultEventCarriesToolAndWriteTarget` with EstimatedTokens > 0.
- New: the estimate event follows the result event; Tokens exceed the prior reading by the result's estimate; Calibrated is false before the first usage and true after; the counter resets on the next reading.
- `eventBaseOf` knows the new variant; `TestE2EEventLinesGolden`'s golden re-recorded with the new redaction.
**Acceptance:**
- `go test -race -count=1 -run 'TestToolResultEvent|TestContextEstimate|TestBudget|TestContextCost|TestSubAgent_EventsCarry|TestSubAgent_RunID' ./internal/agent/`
- `go test -race -count=1 -run TestE2EEventLinesGolden ./cmd/apogee/`
**Depends on:** item 4
**Commit:** `feat(agent): estimate the context after each tool-result append`

## 6. Fetch rows show the size ingested — ✅ DONE (2026-11-01)

NOTES (2026-11-01): the estimate is set on the view by a widened `addToolResult(result, estimatedTokens, spawnRunID, run)`; its four test callers (transcriptbridge_test.go ×3, transcript_test.go ×1) pass 0. The fetch presenters keep `stat: fetchedBodyStat` (the token-less slot, `HTTP 200 OK · 3.1 KiB[ · truncated]`) and gain `statTokens: fetchedBodyTokenStat`, which `enrichWithResult` prefers when set; the size is `len(res.Content)` (the same number `toolView.chars` records) and the separator reuses `slotSeparator`.
NOTES (2026-11-01): the persisted field is `session.ToolView.EstimatedTokens` (`json:"estimatedTokens,omitempty"`), mirrored as `toolView.estimatedTokens`; the session round-trip golden and the undated-record tests were extended rather than new tests added.
NOTES (2026-11-01): consequential edit — internal/tui/toolsummary_pin_test.go: made necessary by the fetch slot now carrying the content size (the cross-package pin's web_fetch/http_request rows read `HTTP 200 OK · 117 B` / `HTTP 200 OK · 667 B`; the page is a constant and the only varying echoed header, Date, is fixed-width).
NOTES (2026-11-01): consequential edit — docs/manual/sessions.md: made necessary by the new `estimatedTokens` record field (the paragraph listing `at` and `chars`, and its "Neither is shown on screen" line).

**What:**
**Goal:** A `web_fetch` / `http_request` row's slot reads `HTTP <status> · <size> · ~<n> tok`, plus ` · truncated` when its FetchedBody says so. `<size>` is the byte length of the result content the model received, in the TUI's existing byte format. `<n>` is the event's EstimatedTokens in the gauge's compact token format, and the `~<n> tok` part is left out when that is 0 (a pre-item journal). Live and replayed rows read the same. The tool-layout.md table rows for both tools are amended.
**Approach (assumed at the header base):** A `fetchStat` hook on `toolPresenter`, modelled on `searchHitsStat`, returning a plain stat. Thread `EstimatedTokens` into `toolView.enrichWithResult` so the live and replay call sites in `transcript.go` share it. Reuse the existing byte and token formatters; add none.
**Regression guard.** The estimate rides the view, not a signature: the `ToolResultEvent` case of `apply` (`transcript.go`) hands `e.EstimatedTokens` to the row, which stores it on a `toolView` field beside `chars` and persists it the way `chars` is (`toWireToolView`/`fromWireToolView`); `enrichWithResult` keeps its signature, which replaces "Thread `EstimatedTokens` into `toolView.enrichWithResult`", and a widened `addToolResult` lists its test callers. Fetch gets a token-aware hook (e.g. `statTokens func(res domain.ToolResult, tokens int)`) called from `enrichWithResult`, extending item 3's fetch `stat` hook rather than adding a `fetchStat`. `<size>` is `format.Bytes` and `<n>` is `format.Tokens`. Item 6 rewrites item 3's exact TestToolStat fetch cases to the new slot text rather than adding cases beside them, and its Acceptance names only executable -run patterns (no placeholders: use the existing test names plus a pattern the item's new tests must match, e.g. 'TestToolStat|TestPresentToolCall|TestToolRegistry|TestFetchRow'). Writer's decision (2026-11-01): the estimate is persisted as an omitempty field beside `chars` in `session.ToolView` (additive; an old record reads 0, so `~<n> tok` is left out). The pins that list ToolView's fields (`TestTranscriptCodecPersistsANamedDelegationAsItsTarget`'s `wantTool`, and any in `internal/session/transcript_test.go`) are updated to name it.
**Files:** internal/tui/toolregistry.go; internal/tui/toolview.go; internal/tui/transcript.go; internal/tui/transcriptbridge.go; internal/session/transcript.go; internal/tui/toolpresent_test.go; internal/tui/transcript_test.go; internal/tui/transcriptbridge_test.go; internal/session/transcript_test.go; docs/layout/tool-layout.md
**Read first:** internal/tui/transcript.go — apply (ToolResultEvent case), addToolResult; internal/tui/toolview.go — enrichWithResult, applyStat, toolView.chars; internal/tui/toolregistry.go — toolRegistry "web_fetch"/"http_request";
internal/tui/transcriptbridge.go — toWireToolView, fromWireToolView; internal/tui/transcriptbridge_test.go — TestTranscriptCodecPersistsANamedDelegationAsItsTarget, TestTranscriptCodecRoundTrip;
internal/tui/toolpresent_test.go — TestToolStat, TestPresentToolCall; docs/layout/tool-layout.md — http_request/web_fetch rows
**Tests:** item 3's `TestToolStat` cases for both tools rewritten in place to the new slot text (plain, truncated) through the `stat` hook, not added beside; new tests are all named `TestFetchRow…`: a dedicated table for the token-aware hook (`~<n> tok` present, left out at a zero estimate), not `TestToolStat`, which calls `p.stat(tc.result)`; one painted row through `tr.apply(ToolCallEvent/ToolResultEvent)`; a scrollback codec round-trip (`encodeTranscript`→`decodeTranscript`) of a fetch row that keeps `HTTP 200 OK · … · ~<n> tok`.
**Acceptance:**
- `go test -race -count=1 -run 'TestToolStat|TestPresentToolCall|TestToolRegistry|TestFetchRow|TestTranscriptCodec' ./internal/tui/`
- `go test -race -count=1 ./internal/session/`
**Depends on:** items 3, 4
**Commit:** `feat(tui): fetch rows show bytes and tokens ingested`

## 7. The gauge follows the tool-result estimate — ✅ DONE (2026-11-01)

NOTES (2026-11-01): internal/tui/watch.go needed no change: `foldWatchedEvent` already routes every journal Event through `foldEvent` → `foldStats`, and `clearWatchView` restores a zero `liveStats`, so the watch gauge follows `context_estimate` lines from the foldStats arm alone; only `watch_test.go` was extended (a delegate's estimate ignored, the top-level one shown as `~96k/128k 75%`, no `~` after esc).
NOTES (2026-11-01): the flag is `liveStats.isEstimateNewer`; it is set only by a depth-0 ContextEstimateEvent with Tokens > 0 and cleared where a non-Maintenance depth-0 reading sets ctxUsed (total > 0). `takeContextEstimate`, `/clear`, restore and the /compact reset are unchanged (the /compact zeroing leaves the flag, which is inert while ctxUsed and ctxEstimate are both 0); `statsFold` in fold_test.go gained `ctxEstimate` and `isEstimateNewer` so the variant table states the new arm.

**What:**
**Goal:** A depth-0 `ContextEstimateEvent` puts the top-level context gauge on `~<Tokens>`, marked as an estimate the way the Turn-start estimate already is. The next non-Maintenance depth-0 `UsageEvent` replaces it with the measured value. `apogee watch` lights its gauge the same way from the journal's `context_estimate` lines. `layout.md`'s gauge paragraph states it. Closes `fetch-ingest-size-blind` with items 3–6.
**Approach (assumed at the header base):** `liveStats` (`internal/tui/model.go`) gains an "estimate is newer than the reading" flag. `foldStats` (`fold.go`) sets `ctxEstimate` and the flag on the event and clears the flag on a counting UsageEvent. `contextGauge` renders the estimate when `ctxUsed == 0` or the flag is set. `/clear`, restore and compaction resets stay as they are. The watch path is the journal fold in `watch.go`.
**Regression guard.** The flag branch is gated like the `ctxUsed == 0` branch: `(ctxUsed == 0 || flag) && m.state != stateErrored`, so an estimate never takes the slot from `enter dismiss`. The item yields to the rule layout.md's gauge paragraph and the CHANGELOG gauge entry record ("never from `enter dismiss`"). Every comment or doc saying when the `~` estimate shows is reworded (`grep -rn 'ctxEstimate\|\`~\` estimate\|estimate.*Turn start' internal/tui/*.go layout.md docs/manual/`), including `Options.ContextWindow` in `tui.go` and the `liveStats`, `takeContextEstimate`, `statusRight` and `contextGauge` comments in `model.go`.
**Files:** internal/tui/model.go; internal/tui/fold.go; internal/tui/watch.go; internal/tui/tui.go; internal/tui/model_test.go; internal/tui/fold_test.go; internal/tui/watch_test.go; layout.md
**Read first:** internal/tui/model.go — contextGauge, statusRight, liveStats, liveStats.reset, takeContextEstimate; internal/tui/fold.go — foldStats (UsageEvent arm), foldEvent; internal/tui/watch.go — foldWatchedEvent, clearWatchView; internal/tui/commandrun.go — foldCompactDone, resetSessionView;
internal/tui/model_test.go — TestContextGaugeShowsEstimateUntilFirstReading, TestContextGaugeEstimateYieldsToEnterDismiss, estimateGauge, statusCells; internal/tui/watch_test.go — TestWatchLightsTheGaugeFromTheWatchedRun
**Tests:** New `TestContextGaugeMovesOnAToolResultEstimate` (`~` shown after the event, measured after usage); a fold case (a Maintenance reading does not clear the flag); a case beside `TestContextGaugeEstimateYieldsToEnterDismiss` that sends a `ContextEstimateEvent` before the `errMsg` and still reads `enter dismiss`; extend `TestWatchLightsTheGaugeFromTheWatchedRun`; the existing `TestContextGaugeShowsEstimate*` stay green.
**Acceptance:** `go test -race -count=1 -run 'TestContextGauge|TestFoldStats|TestUsageEvent|TestWatchLights' ./internal/tui/`
**Depends on:** item 5
**Closes:** fetch-ingest-size-blind
**Commit:** `feat(tui): the context gauge moves on each tool-result estimate`

## 8. The release gate peels commits CI never runs on — ✅ DONE (2026-11-01)

NOTES (2026-11-01): the ADR 0098 decision item 5 paragraph (adr-0098-decision.md:54) still states the old CHANGELOG/VERSION-only peel and still matches the Acceptance grep; it is deliberately left as written, with the dated amendment blockquote directly under it superseding it, since AGENTS.md says a record's Decision text is corrected by amendment, never rewritten.
NOTES (2026-11-01): `capcom check` exits 1 on this tree and on the untouched base alike, solely for "AGENTS.md:40: the capcom block is out of date; run capcom agents to refresh it" — pre-existing, unrelated to this item; it reports nothing about the edited ADR Notes.
NOTES (2026-11-01): the matcher also implements a leading `**/` as zero or more directories (GitHub's reading), a form ci.yml does not use today, pinned by TestCompileGlob; `?`, `+` and `[` stay refused by the drift test.
NOTES (2026-11-01): the release skill's step 1a now re-runs step 1's block right after the push and carries on when the gate is ok, skipping the CI wait; its `RUN=none` bullet now says a gate still blocked after that re-run means the commit below the prose has no green run either.

**What:**
**Goal:** The release gate peels a trailing first-parent commit whose every path either is CHANGELOG.md/VERSION or is skipped by `ci.yml`'s `on.push.paths` filter, so a prose-only HEAD clears on the green run below it. Take-back paths (`.capcom/knowledge/decisions/adr-0098.md`, `graphics/demo/stage/**`) are not peeled. A drift test fails when the gate's copy of the filter and `ci.yml` disagree. The gate docs, `status` help, ADR 0098 and the release skill state the widened rule. Fixes defect `release-gate-prose-deadlock`.
**Approach (assumed at the header base):** New `cmd/release/cifilter.go` holds an ordered Go copy of the patterns. Its matcher maps `**`→any and `*`→one segment, and the last match wins, as GitHub evaluates them. `touchesOnlyPeelablePaths` accepts a CI-skipped path; merges and empty commits still stop the peel. The drift test yaml-parses `../../.github/workflows/ci.yml` (`gopkg.in/yaml.v3`, already a dependency) and fails on any `?`, `+` or `[`. Docs to update:
- the gate.go doc comments;
- the `status` help text in `status.go`;
- `docs/manual/building.md` "Check the gate";
- a dated amendment blockquote under ADR 0098's decision item 5, named on its Status line;
- the `ci.yml` comment block (the gate mirrors this list);
- release skill step 1a: after the push, re-run step 1's block first and carry on if the gate is ok, and replace the `RUN=none` bullet's "blocked until a code commit" sentence.
**Regression guard.** Item 8 first checks that release-skill step 1a is committed (`git log -1 --format=%h -S'1a — Push, then wait for green CI' -- .apogee/skills/release-apogee/SKILL.md` prints a commit and `git diff --quiet HEAD -- .apogee/skills/release-apogee/SKILL.md .claude/skills/release-apogee/SKILL.md docs/manual/building.md` exits 0); if not, the item stops and never commits those hunks itself.
Every comment, help line and doc saying the peel touches only CHANGELOG.md/VERSION is reworded, `TestGatePeelsOnlyTrailingChangelogAndVersionCommits`'s doc comment included (`grep -rn 'CHANGELOG.md.*VERSION\|VERSION-only' cmd/release docs/manual .apogee .capcom/knowledge/decisions/adr-0098*`). The matcher reads `*` as `[^/]*` within one segment (path.Match semantics) and `**` as any run including `/`, as GitHub does. The ADR 0098 amendment supersedes decision item 5's "the commit that was gated is, in effect, the commit that ships" as well as its peel rule, since a peeled prose commit ships ungated.
**Files:** cmd/release/cifilter.go; cmd/release/cifilter_test.go; cmd/release/gate.go; cmd/release/gate_test.go; cmd/release/status.go; docs/manual/building.md; .capcom/knowledge/decisions/adr-0098.md; .capcom/knowledge/decisions/adr-0098/adr-0098-decision.md; .github/workflows/ci.yml; .apogee/skills/release-apogee/SKILL.md
**Read first:** cmd/release/gate.go — peelBumps, touchesOnlyPeelablePaths, peelablePaths, checkGreenRun, checkGate; cmd/release/gate_test.go — TestGatePeelsOnlyTrailingChangelogAndVersionCommits, TestGateNamesEachFailingCondition, linearHistory, fakeRepo.respond; .github/workflows/ci.yml — on.push.paths and its comment block;
cmd/release/status.go — newStatusCommand Long; cmd/release/publish.go — checkPushable; .apogee/skills/release-apogee/SKILL.md — step 1 Gate, step 1a RUN=none bullet; .capcom/knowledge/decisions/adr-0098/adr-0098-decision.md — item 5; internal/skills/repo_skills_test.go — TestRepoReleaseSkillsLoad
**Tests:** `TestGatePeelsOnlyTrailingChangelogAndVersionCommits` gains cases: a prose-only commit is peeled; prose + bump is peeled; a take-back path stops the peel; mixed prose + code stops it. `TestGateNamesEachFailingCondition` gains a case where a prose-only HEAD clears on the parent's green run; it reaches the gate because `linearHistory()` gains a `proseCommit` (child of `rollupCommit`, changed `.capcom/issues/x.md`), since that test's loop overwrites every case's commits with `linearHistory()`. The drift test. `cifilter_test` cases: `graphics/apogee-logo-dark.svg` skipped, `graphics/demo/stage/x.go` run, `.capcom/knowledge/decisions/adr-0098.md` run.
**Acceptance:**
- `go test -race -count=1 ./cmd/release/`
- `go test -count=1 -run TestRepoReleaseSkillsLoad ./internal/skills/`
- `grep -rn 'CHANGELOG.md.*VERSION\|VERSION-only' cmd/release docs/manual .apogee .capcom/knowledge/decisions/adr-0098*` prints no line stating the old CHANGELOG/VERSION-only peel
- `grep -n 'Amended 2026-11-01' .capcom/knowledge/decisions/adr-0098/adr-0098-decision.md` prints the amendment
- `capcom check`
**Closes:** release-gate-prose-deadlock
**Commit:** `fix(release): peel trailing commits ci.yml never runs on`

## 9. The `ui.notify` setting

**What:**
**Goal:** The config key `ui.notify` exists: an enum `off | bell | osc9 | both`, default `bell`. It is editable in /settings and applies live, appears in the shipped template, and is documented in `docs/manual/configuration.md`'s `ui:` section, with that section's key count corrected. Nothing reads it yet.
**Approach (assumed at the header base):** Follow the `ui.spinner` enum entry in `internal/config/registry.go`. Add the const, the ordered list entry, the default and the `Set` case in `internal/domain/uiprefs.go`; the `uiConfig` yaml field in `internal/config/config.go`; `internal/config/defaults/config.yaml`; and the live-apply case in `internal/tui/settingsapply.go`. /settings reads `config.KeyRegistry` and needs no edit.
**Regression guard.** An empty `ui.notify` value (the zero UIPrefs that the tui test harness or a hand-built tui.Options carries) reads as off; the shipped default `bell` arrives only through DefaultUIPrefs and the config template.
Every literal or sweep that spells the whole `ui` block or every `ui` key gains `ui.notify` (`grep -rn 'Glyphs: domain.GlyphsUnicode\|UIKeyGlyphs\|"ui.glyphs"' --include='*_test.go' internal cmd`): `uiKeyCases` (uiprefs_test.go), `wantUIDefault` and `everyKeyFileConfig` (an off-default notify, for `TestRegistrySetIsTheInverseOfRead`) in config_test.go, `TestSettingsApplyLocalLandsEveryUIKeyThroughSet`, `TestSettingsRowsFormatEffectiveValues`. `Validate` and the parse accept `""`, since `validateSpinnerName`/`validateGlyphSetName` and registry_test.go validate a partial `UIPrefs{}`.
**Files:** internal/config/registry.go; internal/domain/uiprefs.go; internal/config/config.go; internal/config/defaults/config.yaml; internal/tui/settingsapply.go; docs/manual/configuration.md; internal/config/registry_test.go; internal/config/config_test.go; internal/domain/uiprefs_test.go; internal/tui/settingsapply_test.go; cmd/apogee/settingsrows_test.go
**Read first:** internal/domain/uiprefs.go — UIPrefs, uiKeys, DefaultUIPrefs, UIPrefs.land, UIPrefs.Validate, ParseGlyphSet; internal/config/registry.go — KeyRegistry ui.glyphs row, glyphsValues, validateGlyphSetName, uiField; internal/config/registry_test.go — TestRegistryEnumValuesMatchParseSites, TestRegistrySetIsTheInverseOfRead, TestUIRowsLandThroughUIPrefsSet;
internal/config/config_test.go — wantUIDefault, everyKeyFileConfig; internal/domain/uiprefs_test.go — uiKeyCases; internal/tui/settingsapply.go — settingsApplyLocal; internal/tui/settingsapply_test.go — TestSettingsApplyLocalLandsEveryUIKeyThroughSet; cmd/apogee/settingsrows_test.go — fabricatedSettings, TestSettingsRowsFormatEffectiveValues
**Tests:** A registry/parse case (each value, an invalid value refused, `""` accepted as off); `TestManualDocumentsEverySettingsKey` (cmd/apogee) green; the whole-block and every-key sweeps above gain their `ui.notify` row.
**Acceptance:**
- `go test -race -count=1 ./internal/config/ && go test -race -count=1 ./internal/domain/`
- `go test -race -count=1 -run 'TestManualDocumentsEverySettingsKey|TestSettings' ./cmd/apogee/`
- `go test -race -count=1 -run TestSettingsApply ./internal/tui/`
**Commit:** `feat(config): a ui.notify setting`

## 10. Notify when apogee waits on the user

**What:**
**Goal:** Opening an approval prompt or an ask pane notifies per `ui.notify`:
- `bell` writes BEL;
- `osc9` writes OSC 9 with the text `apogee: approval needed` or `apogee: question`, wrapped for tmux passthrough while `$TMUX` is set;
- `both` writes both;
- `off` writes nothing.
It fires once on the transition into awaiting, so a queued approval that follows an open one is silent, and a re-offered dismissed prompt is silent. A Turn that ends done or errored notifies (`apogee: turn finished` / `apogee: turn failed`) only while the terminal reports the window blurred. `layout.md` states the rule, and the IDEAS.md item is ticked.
**Approach (assumed at the header base):** A new `internal/tui/notify.go` builds the Cmd: `tea.Raw`, `ansi.Notify`, `ansi.TmuxPassthrough`. `$TMUX` is read once at construction into a Model field that tests set. Hook `foldApprovalRequest` and `foldAskRequest`, not `openDecisionLatch` (its pickers stay silent), and skip the `offerWaitingPrompt` re-offer path. The View sets `ReportFocus`; `tea.FocusMsg`/`tea.BlurMsg` keep a `focused` flag that starts true. `finishWorker`'s done and error paths fire the Turn-end notify.
**Regression guard.** An empty `ui.notify` value reads as off (see item 9); tests that want a notification set the mode explicitly.
The Turn-end notify fires at the `exchangeDoneMsg` case (`msg.Result.Faulted` → `apogee: turn failed`, else `apogee: turn finished`) and in `foldLoopError`, never inside `finishWorker`, so a stop (`foldCancelled`) and a `/compact` (`foldCompactDone`) stay silent; this replaces "`finishWorker`'s done and error paths". The queued-approval clause becomes: no notify for a request that folds in the same Exchange right after the human answered one. Re-offers are silenced by prompt ID (a set of offered IDs on `backgroundWorkflows` beside `dismissed`): a prompt's first offer through `offerWaitingPrompt` notifies, its re-offer and the `^a` open (`answerShownWorkflow`) do not; this replaces "skip the `offerWaitingPrompt` re-offer path".
Tests collect the batch members on goroutines as `TestModelApprovalFoldAsksTheTerminalForItsDrainMarker` does (never waiting out `approvalArmBackstop`), or unit-test a pure (mode, tmux, text) → string builder; `fireBatch` discards the bytes.
**Files:** internal/tui/notify.go; internal/tui/notify_test.go; internal/tui/approval.go; internal/tui/ask.go; internal/tui/model.go; internal/tui/workflow.go; internal/tui/workflows.go; layout.md; IDEAS.md
**Read first:** internal/tui/approval.go — foldApprovalRequest, openDecisionLatch, decisionLatchCmds, sendApproval; internal/tui/ask.go — foldAskRequest; internal/tui/workflow.go — offerWaitingPrompt, openWorkflowPrompt; internal/tui/workflows.go — answerShownWorkflow;
internal/tui/model.go — finishWorker, foldCancelled, foldLoopError, View, the exchangeDoneMsg case; internal/tui/mousereassert.go — reassertMouse (the tea.Raw precedent); internal/tui/model_test.go — testUIPrefs, stepCmd, cmdMsg, TestModelApprovalFoldAsksTheTerminalForItsDrainMarker;
internal/agent/construct.go — queuedApprover.Approve
**Tests:** `notify_test.go` covers each mode against an approval and an ask (the expected `tea.RawMsg` bytes, read as the guard says), the tmux wrap, the answer → next `approvalReqMsg` silence, a background prompt's first offer notifying while its re-offer and the `^a` open stay silent, a Faulted Exchange reading `turn failed`, a cancel and a `/compact` while blurred staying silent, and the Turn end while blurred vs focused. `TestModelApprovalFoldAsksTheTerminalForItsDrainMarker` stays green.
**Acceptance:** `go test -race -count=1 -run 'TestNotify|TestApproval|TestAsk|TestModelApproval' ./internal/tui/`
**Depends on:** item 9
**Closes:** prompt-wait-unannounced
**Commit:** `feat(tui): notify when an approval, a question or a blurred Turn end waits on the user`

## 11. Drag-select auto-scrolls past the transcript edge

**What:**
**Goal:** While a transcript drag-select is held on row 0 with the view scrolled up, the transcript scrolls up 1 line per ~50 ms tick. Held below the transcript's last row, it scrolls down `min(5, 1 + rows past the edge)` lines per tick. Each step moves the selection head to the newly revealed edge line. Scrolling stops on release (which copies the whole selection), when the pointer re-enters the transcript, or at the top or bottom; reaching the bottom resumes following. `layout.md` states the edge rule beside its drag-select rule.
**Approach (assumed at the header base):** `handleMouseMotion` (`internal/tui/mouse.go`) starts a `dragScrollTickMsg{gen}` loop through `tea.Tick`. A generation counter drops stale ticks, as `flashClearMsg` does, and `handleMouseRelease` bumps it. Each tick scrolls through the same tail as `scrollViewport` (`floorShortScreenOffset`, then the `detached` rule), stopping when `m.mousePressed` is false. The interval is a named constant; tests send the tick messages directly and never sleep.
**Regression guard.** The scroll-up edge line is `m.viewport.YOffset()` plus the `stickyHeaderSpan` count, never `contentLineAt(0)` (that row draws the sticky owning prompt). One loop runs at a time: a motion past the edge only stores the latest pointer distance on the Model, `gen` bumps only on release or on leaving the edge zone, and a tick stops when `!m.transcriptSel.active` as well as `!m.mousePressed`. The item supersedes, for the transcript, `handleMouseMotion`'s comment that motion outside the owning rectangle is ignored; every such comment is rewritten (`grep -n 'past the edge\|outside the owning' internal/tui/*.go`).
**Files:** internal/tui/mouse.go; internal/tui/model.go; internal/tui/mouse_test.go; layout.md
**Read first:** internal/tui/mouse.go — handleMouseMotion, handleMouseRelease, pointTranscriptRow, flashClearMsg, showFlash; internal/tui/model.go — scrollViewport, floorShortScreenOffset, stickyHeaderSpan, drawnLineAt, contentLineAt, Model.mousePressed, Model.detached;
internal/tui/mouse_test.go — leftClick, leftDrag, leftRelease, TestTranscriptSelectionSurvivesWheelScroll, TestTranscriptSelectionDropsWhenSpanChanges, TestTranscriptSelectionOnStickyHeaderRow
**Tests:** In `mouse_test.go` (helpers `leftClick`, `leftDrag`, `leftRelease`; fixtures built with `addUser` + many `commitAssistant`, as `TestTranscriptSelectionSurvivesWheelScroll` does, since `modelWithTranscript`'s single prompt cannot scroll): scroll up from row 0; scroll down with the distance-scaled step and cap; stop on release, with the copy spanning both screens; a stale tick ignored; a bottom-reach resumes following; motion reports within one tick keep a single loop scrolling; a span change mid-drag stops the loop; scrolling up under a sticky header moves the head to the revealed line, not the prompt.
**Acceptance:** `go test -race -count=1 -run 'TestTranscriptDrag|TestTranscriptSelection|TestDragScroll' ./internal/tui/`
**Closes:** drag-select-edge-stuck
**Commit:** `feat(tui): auto-scroll a drag-select held past the transcript edge`

## 12. Drop the stale parity TODO tag

**What:**
**Goal:** The package comment of `internal/tui/command.go` carries no `TODO: apogee-code feature parity` tag, and no non-test file in `internal/tui` names that TODO.
**Approach (assumed at the header base):** Remove the parenthetical from the heading line; the rest of the comment stays.
**Files:** internal/tui/command.go
**Read first:** internal/tui/command.go — the package heading comment ("Chat input mini-language — the parse layer")
**Tests:** none (comment only).
**Acceptance:** `go build ./internal/tui/ && ! grep -rn 'TODO: apogee-code' internal/tui --include='*.go'`
**Commit:** `chore(tui): drop a stale parity TODO tag`
