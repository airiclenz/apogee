# The Budget measures its standing parts; the prune band and its protected window widen — plan

**Goal:** stop stale-tool-result Pruning from firing at ~29% of the context window. Today the
Budget reserves fixed 15% (system prompt) and 25% (file context) shares of the working room whether
or not that content exists — `Budget.FileContext` has no reader at all — so History gets 48% of the
window and the 60%/40% prune band with four protected Turns thrashes a model that reads files in
400-line chunks (live session `20260921T173133Z-718e0f6c`: 14 stubs in three passes at 35% window
fill, the same files re-read three and four times). After this plan History is the working room
minus what the standing content actually measures, the band is 70%/50%, and six Turns are protected.

**Date:** 2026-09-21
**Status:** unexecuted
**sized for:** ~200k-context host
**Standing requirements:** skills: coding-standards; any authorized deviation from item text lands as a dated NOTES line under the item; no version identifier changes; `CHANGELOG.md` entries travel in item sidecars and land at closeout. Items are written by symbol, never by line: other plans land first, so an implementer locates every site by the named symbol and the stated grep.

**Regression check (2026-09-21, b7fbf8c7):**
- 1: guard folded — the 15%/25% unmeasured fallback is test-only after item 2 (owner decision).
- 2: recast — History floor capped at the fold's transcript budget (yields to ADR 0018 §8, whose crossover arithmetic gets a dated addendum line in item 5); the `SystemShare` one-liner lands here; the fill-notice fixtures measure exactly; the memo clause is struck; both budget tests are repinned; the History-share prose in `loop.go` / `dispatch.go` is owned here.
- 3: guard folded — grep widened, `internal/notice/window.go` added, `CONTEXT.md` "system-prompt share" line assigned to item 5.
- 4: guard folded — the three six-Turn fixtures and the already-stubbed case are padded from `PruneKeepTurns`; the tool-result cap's 40% and the History-share ~60% sites are not this item's.
- 5: guard folded — the addenda name the retired fixed-share split, never "the floor case" (owner decision); the grep rule is scoped to Budget-share, History-allocation and prune-band lines.
- Round 2 (2026-09-21, b7fbf8c7): 2: recast — the History cap moves to where History is PRODUCED (`(*Agent).budget()`, History ≤ the fold's transcript budget); `deriveGrowthBounds` stays uncapped and its 16k `turn_test` case is struck; every reader reads ONE number, so ADR 0077 §3's "never disagree" rule holds unchanged (yields to ADR 0077 §3 / `HistoryFill`); `fillnotice_test.go`, `toolresultfloor_test.go`, `filerefs_test.go` are sized from `a.budget().History` and named in Tests; guard (c) reworded — nothing renders, `Measured{0, 0}` is exact (owner decisions). 5: guard folded — the ADR 0018 §8 addendum names the cap's new home (`budget()`); the ADR 0077 addendum is the fixed-share-retired line only, no addendum on `HistoryFill`'s comment (owner decision).

## Authoritative sources

- `CONTEXT.md` entries **Budget**, **Pruning**, **Context files**, **Context-fill notice** (section `#context-and-history`).
- `internal/context/budget.go` (`Allocate`, `Allocation`, `TokenEstimator`); `internal/context/prune.go` (`Prune`, `pruneHighFraction`, `pruneLowFraction`, `PruneKeepTurns`); `internal/domain/hooks.go` (`Budget`); `internal/domain/budget.go` (`HistoryExceedsFraction`, `HistoryFill`); `internal/agent/loop.go` (`(*Agent).budget`, `standingSystem`, `standingRenders`); `internal/agent/contextfiles.go` (`ContextFilesReport`, `contextBlocks`); `internal/agent/contextcost.go` (ADR 0079).
- ADR 0018 (History allocation and the structural floor), ADR 0026 §8 (oversize is advisory, measured against the system share), ADR 0077 (the fill notice reads the History allocation), ADR 0079 (context cost is a first-class report).
- Live evidence: `~/.apogee/sessions/20260921T173133Z-718e0f6c.json` — 160k window, `ctxUsed` 55,885, prune notes at 18:04 / 18:32 / 19:01.

## Ratified design calls

- **Both standing parts are measured** (owner, 2026-09-21): `Allocate` takes the measured token size of the system-prompt part and of the file-context part; each reservation is the measurement plus 10% headroom, floored at 2% of the working room; History is the remainder. The 15%/25% fractions survive ONLY as the fallback for an unmeasured part (a caller that passes no measurement).
- **History keeps a floor of 50% of the working room** (owner, 2026-09-21): the measured reservations are honoured only down to that floor; a standing content larger than that is the oversize notice's job to report, never a reason to switch the reducers off (a non-positive History reads as "window unknown").
- **The oversize notice keeps a fixed advisory ceiling** (owner, 2026-09-21): `ContextFilesReport.Oversize()` compares the whole standing content against the unchanged 15%-of-working-room share, carried on the Budget as its own advisory field, never against the measured reservation. Wording and trigger unchanged.
- **Prune band 70% / 50%, six protected Turns** (owner, 2026-09-21): constants, not configuration; `prune-tool-results:` stays the only key.
- **No new config keys** (owner, 2026-09-21): shares, band and protected window are code constants documented in `CONTEXT.md`.
- **One History for every reader** (owner, 2026-09-21, round 2): the History cap lives where History is PRODUCED — `(*Agent).budget()` sets `History = min(alloc.History, max(Window − (compactMaxTokens + compactPromptOverheadTokens), compactMinTranscriptTokens))` when the window is known; `deriveGrowthBounds` stays uncapped; the fill notice, Prune, `HistoryFill`, the fold trigger and `structuralFloor` all read that ONE number, so ADR 0077 §3's "the notice and the fold never disagree" holds unchanged.

## Out of scope

- Exposing the split in the TUI inspector or the `ContextCost` report; any settings row.
- The generative mid-Exchange reducer (`apogee-1bf`), the tool-result cap fractions (ADR 0071), the reply reserve (`response-reserve`) and its default.
- Sub-agent context-file injection (`apogee-vi5`).
- A live-LLM check; everything here is driven in `go test`.

## 1. `Allocate` measures the standing parts and floors History

**What.** In `internal/context/budget.go`, `Allocate` gains a measured input: the token size of the
system-prompt part and of the file-context part (a small value type, e.g. `Measured{SystemPrompt,
FileContext int}`, a negative or absent value meaning "unmeasured"). For a measured part the
reservation is `measured × 1.10`, rounded up, floored at 2% of the working room; an unmeasured part
keeps today's fraction (`systemPromptFraction`, `fileContextFraction`). History is the remainder,
floored at 50% of the working room: when the two reservations would push History below that, they
are scaled down together (proportionally) so the parts still sum to the working room exactly.
`Allocation` gains `StandingAdvisory int` — the unchanged 15%-of-working-room share the oversize
notice reads (item 3) — kept beside the reservations so a caller can tell the advisory ceiling from
the reserved room. The headroom, the 2% floor and the 50% History floor are named constants with
doc comments stating what each protects. The existing callers that pass no measurement
(`internal/context/budget_test.go`, `internal/agent/*_test.go`, `cmd/apogee/e2e_fillnotice_test.go`)
compile unchanged through the unmeasured path or a thin wrapper — the implementer picks one and
applies it at every call site found by `grep -rn 'Allocate(' --include='*.go'`. After item 2 the
unmeasured path (the 15%/25% fallback) is TEST-ONLY: `budget()` always measures.
Binding: the split stays pure (no I/O, no Agent state) — ADR 0010; one implementation of the
chars→token ratio (`Budget.EstimateTokens`) — measurement arrives in tokens, `Allocate` converts
nothing.
**Regression guard.** The 15%/25% fallback for an unmeasured part is TEST-ONLY after item 2
(`budget()` always measures) — stated in the What above.
**Files:** internal/context/budget.go, internal/context/budget_test.go
**Read first:** internal/context/budget.go — Allocate, Allocation, systemPromptFraction, fileContextFraction, defaultReserveFraction; internal/context/budget_test.go — TestAllocate_ReserveHonouredAndPartsSum, TestAllocate_UnknownWindowIsZero, TestAllocate_OversizeReserveClamped; internal/agent/loop.go — budget;
internal/agent/budget_test.go — TestBudgetIsHonestBeforeCalibration, TestBudgetSplitsTheAdvertisedWindowFromTheWorkingRoom; internal/agent/contextfiles_test.go — TestContextFilesReportMeasuresStandingContent; cmd/apogee/e2e_fillnotice_test.go — seedFillFixtures; internal/agent/turn_test.go — TestGrowthBounds_WorkingWindowKeepsReaderNumbers
**Tests.** `budget_test.go`: (a) measured parts reserve `measured × 1.10` and History takes the
rest, every field non-negative, parts sum to the window; (b) a zero measurement floors at 2% of
working; (c) an unmeasured part reserves its fraction exactly as before (existing
`TestAllocate_ReserveHonouredAndPartsSum` keeps passing); (d) a standing content larger than the
working room leaves History at exactly 50% of working and the two reservations scaled
proportionally, sum intact; (e) `StandingAdvisory` equals the 15% share regardless of measurement;
(f) unknown window still yields the zero Allocation.
**Acceptance.** `go build ./... && go vet ./internal/context && go test ./internal/context -run 'TestAllocate' -count=1`
**Commit:** `feat(context): Allocate reserves the measured standing parts and floors History`

## 2. `(*Agent).budget()` feeds the measurement

**What.** Recast at the regression check (2026-09-21). Depends on item 1. In `internal/agent`, `(*Agent).budget()` measures the standing content
it already renders — the `context files` row of `standingBlocks()` is the file-context part, every
other standing row together is the system-prompt part — converts each through the Agent's
`TokenEstimator` (the same ratio the reducers read, so the reservation and the trigger share one
scale) and passes both to `Allocate`. `Budget` (`internal/domain/hooks.go`) gains the
`StandingAdvisory` field, copied from the Allocation; its field comments say `SystemPrompt` and
`FileContext` are now MEASURED reservations. Sub-agents measure their own standing content (they
copy the parent's context files) with no special case. The measurement is taken on every `budget()`
call (one standing render per call; no memo, because the standing render moves on SetMode,
SetScratchDir, the date, delegation seats, ExtraReadRoots and every task_list call).
The History cap lives where History is PRODUCED: `budget()` sets
`History = min(alloc.History, max(Window − (compactMaxTokens + compactPromptOverheadTokens), compactMinTranscriptTokens))`
when the window is known (the 4608 is named by its existing symbols in `internal/agent/compact.go`,
never the literal), so ADR 0018 §8's survivability ordering (structural floor below the fold's
transcript budget) holds at every window and `deriveGrowthBounds` stays UNCAPPED — the fill notice,
Prune, `HistoryFill`, the fold trigger and `structuralFloor` all read that ONE number. The one-liner
`report.SystemShare = budget.StandingAdvisory` in `ContextFilesReport`
(`internal/agent/contextfiles.go`) lands here (item 3 keeps the comments, the ADR addendum and the
tests). Prose: every comment under `internal/agent` stating a History share as a fixed fraction
(grep `60%\|48%\|working room` — known sites `internal/agent/loop.go` `requestExceedsWindow`,
`internal/agent/dispatch.go` `structuralFloor`) now says History is the working room less the
measured standing reservations, floored at 50%.
Producers of the changed values: `budget()` only. Consumers reached: `deriveGrowthBounds`
(`historyFloor`), `structuralFloor`, `refBound`, `autoPrune`, `autoCompact`, `HistoryFill` (the fill
notice, ADR 0077), `ContextFilesReport.SystemShare` (the advisory field, from this item).
**Regression guard.** (a) (round 2, replaces round 1's `deriveGrowthBounds` cap) the History cap
moves to where History is PRODUCED — `(*Agent).budget()` sets
`History = min(alloc.History, max(Window − (compactMaxTokens + compactPromptOverheadTokens), compactMinTranscriptTokens))`
when the window is known, and `deriveGrowthBounds` stays UNCAPPED (the earlier `deriveGrowthBounds`
cap and its 16k `turn_test.go` case are STRUCK); every reader — the fill notice, Prune,
`HistoryFill`, the fold trigger, `structuralFloor` — reads that ONE number, so ADR 0077 §3's "the
notice and the fold never disagree" holds unchanged (ratified: **One History for every reader**);
the two budget tests are repinned to that min; the constant 4608 is named by its existing symbols
in `compact.go` rather than the literal; (b) the one-liner
`report.SystemShare = budget.StandingAdvisory` in `ContextFilesReport` lands IN item 2 (item 3
keeps the comments, ADR addendum and tests), `internal/agent/contextfiles.go` and
`contextfiles_test.go` join item 2's Files, `TestContextFilesReportMeasuresStandingContent` is
repinned there to `a.budget().StandingAdvisory`, and `-run 'TestContextFiles'` joins item 2's
acceptance; (c) `seedFillFixtures` in `cmd/apogee/e2e_fillnotice_test.go` sizes from
`Allocate(fillNoticeWindow, 0, 0, Measured{0, 0})` and `fillNoticeConfig` pins
`use-default-prompt: false`: with no context file nothing renders under the ride-along rule
(`standingRenders` returns nil), both parts measure 0 and floor at 2%, so `Measured{0, 0}` is exact
— with a comment stating that, no "orientation block ~50 tokens" claim; (d) STRIKE the
memoisation clause: the measurement is taken on every `budget()` call (one standing render per
call; no memo, because the standing render moves on SetMode, SetScratchDir, the date, delegation
seats, ExtraReadRoots and every task_list call); (e) name `TestBudgetIsHonestBeforeCalibration`
beside `TestBudgetSplitsTheAdvertisedWindowFromTheWorkingRoom` in Tests with the same repin; (f) the
prose in `internal/agent/loop.go` and `internal/agent/dispatch.go` that records History as "~60% of
the working room" is reworded in item 2 (rule: every comment under `internal/agent` stating a
History share as a fixed fraction — grep `60%\|48%\|working room` — now says History is the working
room less the measured standing reservations, floored at 50%); `internal/agent/dispatch.go` and
`internal/agent/compact.go` join item 2's Files; (g) (round 2) `internal/agent/fillnotice_test.go`
joins item 2's Files; its bodies are sized from `a.budget().History` (or its header line restates
the new History at 8192) and `TestContextFillNotice` joins the acceptance `-run`; (h) (round 2)
`toolresultfloor_test.go` and `filerefs_test.go` are re-derived from `a.budget().History` where
they hand-pin the 3.9k line and are named in Tests, not listed as "keep passing". Documented
decisions: this item yields to ADR 0018 §8
(`docs/adr/0018-context-overflow-recovers-structurally-the-emergency-fold-and-one-retry.md`, the
§8 crossover arithmetic; restated in code by `internal/agent/dispatch.go` `structuralFloor`) — the
cap in (a) keeps §8's ordering at every window; the §8 arithmetic gets a dated addendum line naming
the cap's new home (`budget()`) in item 5. It also yields to ADR 0077 §3
(`docs/adr/0077-the-context-fill-notice-is-the-first-engine-advise-reaction.md`; restated by
`internal/domain/budget.go` `HistoryFill` and `internal/agent/fillnotice.go`) — with the cap in
`budget()` the notice and the fold read one History, so the "never disagree" rule stands and gets
no addendum.
**Files:** internal/agent/loop.go, internal/domain/hooks.go, internal/agent/compact.go, internal/agent/dispatch.go, internal/agent/contextfiles.go, internal/agent/budget_test.go, internal/agent/turn_test.go, internal/agent/contextfiles_test.go, internal/agent/fillnotice_test.go, internal/agent/toolresultfloor_test.go, internal/agent/filerefs_test.go, cmd/apogee/e2e_fillnotice_test.go
**Read first:** internal/agent/loop.go — budget, standingSystem, requestExceedsWindow; internal/agent/standingblocks.go — standingBlocks, standingRenders; internal/agent/compact.go — deriveGrowthBounds, historyExceedsAllocation; internal/agent/dispatch.go — structuralFloor, clampToolResult;
internal/domain/budget.go — HistoryFill, HistoryExceedsAllocation; internal/agent/toolresultfloor_test.go — floorAgent, TestToolResultCapKeepsTheTighterCapAboveTheFloor; internal/agent/filerefs_test.go — refAgentWithWindow, TestResolveFileRefs_SplitsTheFloorAcrossReferences;
internal/agent/fillnotice_test.go — fillConfig, sizedTool, TestContextFillNoticeFirstPostFoldResultFiresItsOwnRung
**Tests.** `internal/agent/budget_test.go`: `TestBudgetIsHonestBeforeCalibration` and
`TestBudgetSplitsTheAdvertisedWindowFromTheWorkingRoom` now compare against `Allocate` fed the same
measurement the Agent takes (a helper on the test side renders the standing blocks and estimates
them), repinned to `min(alloc.History, max(Window − (compactMaxTokens + compactPromptOverheadTokens), compactMinTranscriptTokens))`,
and a new test shows a session with a large `AGENTS.md` gets a larger `FileContext` and a
smaller History than the same session without it, with History never below 50% of working.
`turn_test.go` `TestGrowthBounds_WorkingWindowKeepsReaderNumbers` sizes its fixtures from the
Agent's own `budget()` (or the measured `Allocate`), never the unmeasured split; `deriveGrowthBounds`
stays uncapped, so no 16k `structuralFloor() <= transcriptBudget` case. `e2e_fillnotice_test.go`
`seedFillFixtures` sizes from `Allocate(fillNoticeWindow, 0, 0, Measured{0, 0})` and
`fillNoticeConfig` pins `use-default-prompt: false` (comment: with no context file nothing renders
under the ride-along rule, both parts measure 0 and floor at 2%, so `Measured{0, 0}` is exact).
`contextfiles_test.go` `TestContextFilesReportMeasuresStandingContent` is repinned to
`a.budget().StandingAdvisory`. `internal/agent/fillnotice_test.go`: the bodies of every
`TestContextFillNotice*` case are sized from `a.budget().History` (or the file's header line
restates the new History at 8192). `internal/agent/toolresultfloor_test.go`
(`TestToolResultCapKeepsTheTighterCapAboveTheFloor`, `floorAgent`) and
`internal/agent/filerefs_test.go` (`TestResolveFileRefs_SplitsTheFloorAcrossReferences`,
`refAgentWithWindow`) are re-derived from `a.budget().History` where they hand-pin the 3.9k line.
Existing `unknownwindow_test.go` keeps passing.
**Acceptance.** `go build ./... && go vet ./internal/agent ./cmd/apogee && go test ./internal/agent -run 'TestBudget|TestGrowthBounds|TestResolveFileRefs|TestToolResultFloor|TestToolResultCap|TestUnknownWindow|TestContextFiles|TestContextFillNotice' -count=1 && go test ./cmd/apogee -run 'FillNotice' -count=1`
**Commit:** `feat(agent): the Budget reserves the measured standing content`

## 3. The oversize notice reads the advisory ceiling

**What.** Depends on item 2. The one-liner `report.SystemShare = budget.StandingAdvisory` in
`ContextFilesReport` (`internal/agent/contextfiles.go`) landed in item 2 (regression decision 2b);
this item keeps the comments, the ADR addendum and the tests. `Oversize()` in
`internal/domain/contextfile.go` and the rendered wording in `internal/notice/contextfiles.go`
(`standing system content ~N tokens exceeds its Budget share (~M) — trim context files, the task
list or the system prompt`) are unchanged. Every comment naming the share the notice reads — rule:
any comment or doc line that says the notice compares against "the system-prompt share" or
`Budget.SystemPrompt` (grep `SystemShare\|system[- ]share\|system-prompt share\|Budget\.SystemPrompt`
over `internal/ docs/adr/0026*`) — is reworded to the advisory ceiling; ADR 0026 gets a dated
addendum line, not a rewrite. `CONTEXT.md`'s "Budget's system-prompt share" line (**Context files**
entry) is item 5's, not this item's.
**Regression guard.** The grep is widened to `SystemShare\|system[- ]share\|system-prompt share\|Budget\.SystemPrompt`
over `internal/ docs/adr/0026*` — it reaches `internal/agent/contextfiles.go` ("against the Budget's
system share") and `internal/notice/window.go` ("gates it on a system share"), which joins Files;
`CONTEXT.md`'s "Budget's system-prompt share" line is outside this item's scope and item 5 owns it.
**Files:** internal/agent/contextfiles.go, internal/agent/contextfiles_test.go, internal/domain/contextfile.go, internal/notice/window.go, docs/adr/0026-workspace-context-files-are-session-scoped-prompt-data.md
**Read first:** internal/agent/contextfiles.go — ContextFilesReport; internal/domain/contextfile.go — ContextFilesReport, Oversize; internal/notice/contextfiles.go — ContextFileNotices; internal/agent/contextfiles_test.go — TestContextFilesReportMeasuresStandingContent, TestContextFilesReportWithoutWindowOrFiles;
internal/notice/contextfiles_test.go; internal/eventjson/writer.go — ContextFiles; internal/notice/window.go — WindowUnknown
**Tests.** `contextfiles_test.go` `TestContextFilesReportMeasuresStandingContent` asserts
`SystemShare == a.budget().StandingAdvisory` (repinned in item 2, kept here); a new case shows a standing content that fits its
measured reservation but exceeds 15% of working STILL renders the notice with the exact wording
above (drive `ContextFileNotices`, compare the emitted string); the surfaces `tui/model.go`
`noteContextFiles`, `headless.go` `contextFilesFrame`, `schedule.go` `contextAnomalies` need no
change and their existing tests keep passing.
**Acceptance.** `go build ./... && go test ./internal/agent -run 'TestContextFiles' -count=1 && go test ./internal/notice ./internal/domain -count=1`
**Commit:** `fix(agent): the oversize notice keeps its fixed advisory ceiling`

## 4. Prune band 70% / 50% and six protected Turns

**What.** `internal/context/prune.go`: `pruneHighFraction = 0.7`, `pruneLowFraction = 0.5`,
`PruneKeepTurns = 6`; the doc comments state the new numbers and the reason the band still spans
20 points (one prefix-cache invalidation per pass — ADR 0023 §6). Rule for prose: every comment in
`internal/` naming the old band or window — grep `60%\|40%\|four most recent\|four Turns\|
PruneKeepTurns` under `internal/context`, `internal/agent` and their tests — is updated (known
sites: `internal/agent/prune.go` file comment, `internal/agent/prune_test.go` `pruneConfig`
comment, `internal/context/doc.go`). Tests that pin the band implicitly through History sizes are
re-derived from the constants rather than re-tuned by hand. The tool-result cap's
40%-of-working-room sites (`internal/agent/dispatch.go`, `internal/agent/toolresultfloor_test.go`)
and the History-share ~60% sites (`internal/agent/loop.go` `requestExceedsWindow`,
`internal/agent/dispatch.go` `structuralFloor`) are NOT this item's — the band is "60% of
History" / "40%" / "four Turns" only; the History-share comments are item 2's (decision 2f).
**Regression guard.** `TestPruneOrdersOldestTurnFirst`, `TestPruneOrdersLargestWithinATurn` and
`TestPruneStubNamesTheCall` each build exactly six tool-calling Turns and go red at keep=6: pad each
fixture with `PruneKeepTurns` filler Turns (count derived from the constant, never a hand-written
c,d,e,f) and re-derive History from the band; seed `TestPruneDoesNothing`'s "already-stubbed results
are not re-pruned" case with `PruneKeepTurns` Turns after the stubbed one so the protected-index gate
passes and the stub check is what declines. The tool-result cap's 40% and the History-share ~60%
sites are not this item's (owners named in the What).
**Files:** internal/context/prune.go, internal/context/doc.go, internal/context/prune_test.go, internal/agent/prune.go, internal/agent/prune_test.go, internal/agent/stepnotice_test.go
**Read first:** internal/context/prune.go — pruneHighFraction, pruneLowFraction, PruneKeepTurns, Prune, pruneProtectedIndex; internal/context/prune_test.go — pruneConv, readCall, TestPruneDoesNothing, TestPruneProtectsTheRecentToolCallingTurns, TestPruneOrdersOldestTurnFirst, TestPruneOrdersLargestWithinATurn, TestPruneStubNamesTheCall;
internal/domain/budget.go — HistoryExceedsFraction, PromptChars; internal/agent/prune.go — autoPrune; internal/agent/prune_test.go — pruneConfig, seedToolTurns, TestAutoPruneStubsOldTurnsAndKeepsTheRecentWindow;
internal/agent/stepnotice_test.go — TestStepNoticeIsToldAgainAfterAPruneStubbedIt, TestTokenNoticeIsToldAgainAfterAPruneStubbedIt; internal/context/doc.go — package comment (Pruning paragraph)
**Tests.** `prune_test.go`: `TestPruneProtectsTheRecentToolCallingTurns` drives seven tool-calling
Turns and asserts the six most recent are untouched and the first is stubbed;
`TestPruneOrdersOldestTurnFirst`, `TestPruneOrdersLargestWithinATurn` and `TestPruneStubNamesTheCall`
pad their fixtures with `PruneKeepTurns` filler Turns and take their History from the band;
`TestPruneDoesNothing`'s "already-stubbed results are not re-pruned" case seeds `PruneKeepTurns`
Turns after the stubbed one; a new
`TestPruneBandIsSeventyToFifty` builds a history at 65% of History and asserts no pass, at 75%
asserts a pass that stops once the fill is under 50%. `internal/agent/prune_test.go`
`TestAutoPruneStubsOldTurnsAndKeepsTheRecentWindow` and `stepnotice_test.go`'s two prune cases are
re-sized to the new band and window and keep their assertions.
**Acceptance.** `go build ./... && go test ./internal/context -run 'TestPrune' -count=1 && go test ./internal/agent -run 'TestAutoPrune|TestStepNotice|TestTokenNotice' -count=1`
**Commit:** `feat(context): prune at 70%/50% of History and protect six Turns`

## 5. Docs: CONTEXT.md, ADR, config template, manual

**What.** Depends on items 1–4. (a) `CONTEXT.md` **Budget** entry states the measured allocation:
the reply reserve (20% default), the two measured reservations (+10% headroom, 2% floor), the 50%
History floor, and that the fractions are fallbacks for an unmeasured part. **Pruning** entry:
70% / 50%, six Turns, and "quiescent Turn boundary" corrected to "every Turn boundary, mid-Exchange
included" (the code's rule; the prefix-cache sentence stays). **Context files** entry: oversize is
measured against the advisory ceiling (15% of working room), which no longer equals the reserved
share, and its "Budget's system-prompt share" line names the advisory ceiling (item 3's grep
stops at `internal/` and ADR 0026; this line is owned here). (b) A new ADR in `docs/adr/` — number
read from the tree at execution time — titled "the Budget reserves what the standing content
measures": context (the dead `FileContext` slot, the 48% History, the thrash), decision (the five
ratified calls above), consequences: the ADR 0018 / ADR 0077 addenda say the old "~60% of the
working room / ~48% of the window" figures described the FIXED-SHARE split this plan retires —
History is now measured, never below 50% of the working room and never above the fold's transcript
budget (item 2 decision a) — do NOT call it "the floor case"; each gets a one-line dated addendum
pointing here, no rewrite; the ADR 0018 §8 addendum names the cap's new home (`budget()`, History
≤ the fold's transcript budget); the ADR 0077 addendum is the fixed-share-retired line only — the
"never disagree" rule stands, no addendum on `HistoryFill`'s comment. (c) `internal/config/defaults/config.yaml` `prune-tool-results:` comment:
70% / 50% / six Turns. (d) `docs/manual/configuration.md` prune paragraph: same numbers.
Rule: every doc line stating a Budget share, the History allocation or the prune band with the old
numbers — grep `60%\|40%\|four most recent\|15%\|25%\|48%` across `CONTEXT.md`, `docs/adr`,
`docs/manual`, `internal/config/defaults` — is updated or carries the addendum. Non-targets the
grep also hits: the tool-result cap's 40% (ADR 0071 D-table, ADR 0018 §9 — out of scope) and
ADR 0022's "25% of runs" (unrelated). `four most recent` is line-wrapped in `CONTEXT.md`'s
**Pruning** entry and `config.yaml`'s `prune-tool-results:` block; the 60%/40% patterns still find
both. `CHANGELOG.md` is never edited here (closeout).
**Regression guard.** The ADR 0018 / ADR 0077 addenda say the old "~60% of the working room / ~48%
of the window" figures described the FIXED-SHARE split this plan retires — History is now measured,
never below 50% of the working room and never above the fold's transcript budget (item 2 decision
a) — do NOT call it "the floor case"; (round 2) the ADR 0018 §8 addendum names the cap's new home
(`budget()`, History ≤ the fold's transcript budget) and the ADR 0077 addendum is the
fixed-share-retired line only — the "never disagree" rule stands, no addendum on `HistoryFill`'s
comment (`internal/domain/budget.go`). The prose rule is scoped to lines stating a Budget share, the History
allocation or the prune band; the tool-result cap's 40% (ADR 0071 D-table, ADR 0018 §9) and
ADR 0022's "25% of runs" are named non-targets.
**Files:** CONTEXT.md, docs/adr/0018-context-overflow-recovers-structurally-the-emergency-fold-and-one-retry.md, docs/adr/0077-the-context-fill-notice-is-the-first-engine-advise-reaction.md, docs/adr/<next>-the-budget-reserves-what-the-standing-content-measures.md, internal/config/defaults/config.yaml, docs/manual/configuration.md
**Read first:** CONTEXT.md — **Budget**, **Pruning**, **Context files** entries (section #context-and-history); docs/adr/0018-context-overflow-recovers-structurally-the-emergency-fold-and-one-retry.md — Amendment sections, the "~60% of the working room" paragraph; docs/adr/0077-the-context-fill-notice-is-the-first-engine-advise-reaction.md — "Two facts fixed the design" paragraph, Addendum (2026-09-19) heading form;
internal/config/defaults/config.yaml — `prune-tool-results:` comment block; docs/manual/configuration.md — the **Pruning** paragraph after Compaction; internal/context/budget.go — systemPromptFraction, fileContextFraction, Allocate; internal/agent/prune.go — autoPrune
**Tests.** None (docs only); `go test ./internal/config -count=1` proves the embedded template still parses.
**Acceptance.** `go build ./... && go test ./internal/config -count=1 && ! grep -n 'four most recent\|above \*\*60%\*\*\|quiescent \*\*Turn boundary\*\*' CONTEXT.md && ! grep -n '60%\|four turns' internal/config/defaults/config.yaml docs/manual/configuration.md`
**Commit:** `docs(context): the Budget measures its standing parts; prune band and window documented`
