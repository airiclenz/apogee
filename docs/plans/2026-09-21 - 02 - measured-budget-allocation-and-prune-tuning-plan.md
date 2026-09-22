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
- Round 3 (2026-09-22, 64e1c68c): 1: guard folded — `internal/agent/loop.go` (`(*Agent).budget`, the
  ONE production `Allocate` caller) joins Files and the caller list, and "unmeasured" is spelled
  `Measured{-1, -1}` because the struct zero value is measured-zero, so no call site compiles
  unchanged. 2: guard folded — the cap is gated on and computed from the ADVERTISED window
  (`a.cfg.Context.MaxContextTokens`), never `ContextLimit` (a `working-window:`-only session would
  collapse History to 256); `seedFillFixtures` sizes from the CAPPED History through an exported
  helper, since `cmd/apogee` is package main and the fill regexp pins the fifties; the item
  supersedes `internal/domain/hooks.go`'s allocation-block sum sentence and strikes the matching sum
  assertion; the prose rule widens to `3\.9k` with `autocompact_guard_test.go` and `prune_test.go`
  joining Files (owner decision). 3: guard folded — the grep reaches `allocated to it\|window
  share\|own share of the window`, `internal/notice/contextfiles.go` joins Files (doc comments
  only), the exact-wording case moves to `internal/notice/contextfiles_test.go` because
  `internal/agent` imports `internal/notice` nowhere, and ADR 0026's "the Budget already allocates a
  system-prompt share" rejection line takes the addendum too. 4: guard folded — both owned comments'
  figures are re-derived from `a.budget().History` (3584 at 8192, ~10k-char trigger) and
  `PruneKeepTurns`; `fillnotice_test.go`'s ladder rungs and `autocompact_guard_test.go`'s compaction
  fixture join the named non-targets. 5: guard folded — the acceptance greps are repointed to
  patterns that bite (`protects the four`, `under **40%**`, `four most`, `40%`), and ADR 0026 §8's
  "15% of working room" line joins the named non-targets as item 3's (owner decision).

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

## 1. `Allocate` measures the standing parts and floors History — ✅ DONE (2026-09-22)

NOTES (2026-09-22): the headroom and the 2% floor are applied as integer percentages (`standingHeadroomPercent = 110`, `standingFloorPercent = 2`, `ceilPercent`) rather than float factors — `12000 × 1.10` in float64 lands a hair above 13200, and rounding that up reserved a phantom token. The result is exactly the item's "measured × 1.10, rounded up".
NOTES (2026-09-22): the cross-package call sites spell the unmeasured value `apogeectx.Measured{SystemPrompt: -1, FileContext: -1}` rather than the item's positional `Measured{-1, -1}`, because `go vet` (in this item's acceptance) refuses an unkeyed composite literal of an imported struct type. The in-package `internal/context/budget_test.go` cases keep the positional `Measured{-1, -1}` spelling the item names.
NOTES (2026-09-22): consequential edit — internal/agent/budget_test.go: made necessary by Allocate's new parameter (two call sites pass the unmeasured value; assertions unchanged)
NOTES (2026-09-22): consequential edit — internal/agent/turn_test.go: made necessary by Allocate's new parameter (one call site passes the unmeasured value)
NOTES (2026-09-22): consequential edit — internal/agent/contextfiles_test.go: made necessary by Allocate's new parameter (one call site passes the unmeasured value)
NOTES (2026-09-22): consequential edit — cmd/apogee/e2e_fillnotice_test.go: made necessary by Allocate's new parameter (`seedFillFixtures` passes the unmeasured value; its t.Fatalf text follows the new signature)

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
(`budget()` always measures) — stated in the What above. (round 3) "Unmeasured" is spelled with a
NEGATIVE field (`Measured{-1, -1}`): the struct zero value `Measured{}` is MEASURED-zero, which
item 2 guard (c) rests on, so no call site "compiles unchanged" — every site
`grep -rn 'Allocate(' --include='*.go'` finds is edited to pass it explicitly, the ONE production
caller included: `(*Agent).budget` in `internal/agent/loop.go`, which joins this item's Files and
its caller list and passes the unmeasured value here (item 2 replaces it).
**Files:** internal/context/budget.go, internal/context/budget_test.go, internal/agent/loop.go
**Read first:** internal/context/budget.go — Allocate, Allocation, systemPromptFraction, fileContextFraction, defaultReserveFraction; internal/context/budget_test.go — TestAllocate_ReserveHonouredAndPartsSum, TestAllocate_UnknownWindowIsZero (`got != (Allocation{})`, so StandingAdvisory must stay comparable), TestAllocate_OversizeReserveClamped;
internal/agent/loop.go — budget; internal/agent/budget_test.go — TestBudgetIsHonestBeforeCalibration, TestBudgetSplitsTheAdvertisedWindowFromTheWorkingRoom; internal/agent/turn_test.go — TestGrowthBounds_WorkingWindowKeepsReaderNumbers;
internal/agent/contextfiles_test.go — TestContextFilesReportMeasuresStandingContent; cmd/apogee/e2e_fillnotice_test.go — seedFillFixtures
**Tests.** `budget_test.go`: (a) measured parts reserve `measured × 1.10` and History takes the
rest, every field non-negative, parts sum to the window; (b) a zero measurement floors at 2% of
working; (c) an unmeasured part (spelled `Measured{-1, -1}`) reserves its fraction exactly as before (existing
`TestAllocate_ReserveHonouredAndPartsSum` keeps passing); (d) a standing content larger than the
working room leaves History at exactly 50% of working and the two reservations scaled
proportionally, sum intact; (e) `StandingAdvisory` equals the 15% share regardless of measurement;
(f) unknown window still yields the zero Allocation.
**Acceptance.** `go build ./... && go vet ./internal/context && go test ./internal/context -run 'TestAllocate' -count=1`
**Commit:** `feat(context): Allocate reserves the measured standing parts and floors History`

## 2. `(*Agent).budget()` feeds the measurement — ✅ DONE (2026-09-22)

NOTES (2026-09-22): the row name the measurement splits on is now the named constant `standingContextFilesRow` in `internal/agent/standingblocks.go` (the table row uses it), so the table's spelling and the Budget's split have one author rather than a magic string in each; `standingblocks.go` joins FILES for it.
NOTES (2026-09-22): `HistoryCap` also replaces the inline `max(b.Window-compactMaxTokens-compactPromptOverheadTokens, compactMinTranscriptTokens)` in `deriveGrowthBounds`, so the cap and the fold's transcript budget are literally one expression — that identity is the item's survivability argument, and two copies could drift.
NOTES (2026-09-22): `internal/agent/floorguards_test.go` joins FILES: `TestFloorGuard_ToolResultCapTrimsAnOlderResultUnderBypass` shares `numberedLines(200)` with `toolresultfloor_test.go` as the payload that must sit BETWEEN the guard's cap and the structural floor, and the smaller History pushed that pinned count past the floor. Both sites now take it from the new `betweenTheCeilings(t)` helper, which derives the largest such payload under 90% of `a.budget().History` instead of pinning a line count.
NOTES (2026-09-22): the item's `60%\|48%\|working room\|3\.9k` sweep is a floor, so two files outside its Files list carry the same retired figure and were folded in (comment text only): `internal/agent/autocompact_test.go` and `internal/agent/subagent_test.go`, each stating the "~3.9k-token History allocation" of the 8k window, now say ~3.6k.
NOTES (2026-09-22): `internal/agent/filerefs_test.go` `TestResolveFileRefs_BoundsExtractionByTheClampBudget` needed its size tolerance widened from `2*floorChars + 200` to `+ 600`: at its 1024-token window the cap makes History the fold's minimum transcript (256 tokens), so the extraction budget is ~2k characters and the fixed 200 no longer covers the annotated header, the not-extracted marker and the ~315-character page the walk finishes past the budget. The comment now names what the slack covers; the test's own claims (short page count, marker present) are unchanged.
NOTES (2026-09-22): `internal/agent/fillnotice_test.go`'s fixtures now come from a new `fillCharsAt(t, pct)` helper (pct of `a.budget().History`, plus one token so truncation cannot report a percent one short) rather than pinned byte counts, and the file header restates the new 3,584-token line — the item allowed either, and this takes both.
NOTES (2026-09-22): `internal/agent/prune_test.go`'s `pruneConfig` comment is re-derived to the CURRENT band (History 3,584 tokens ⇒ the 60% trigger ≈ 8.6k chars); item 4 owns the move to 70% and the ~10k figure.
NOTES (2026-09-22): `TestE2EStreamPTY` (cmd/apogee) failed once under the full `go test ./...` run and passes both in isolation and on a full `go test ./cmd/apogee` — a PTY timing flake under parallel load, on a path this item does not touch.

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
they hand-pin the 3.9k line and are named in Tests, not listed as "keep passing".
(i) (round 3) the cap is gated on and computed from the ADVERTISED window
(`a.cfg.Context.MaxContextTokens > 0`, mirroring `deriveGrowthBounds`'s `if b.Window > 0`), NEVER
from `limit`/`ContextLimit`: a session with `working-window: 200000` and no `context-window:` has
`Window == 0` and `ContextLimit == 200000`, where `max(0 − 4608, 256)` would collapse History from
~155k to 256 for every reader; with no advertised window History stays `alloc.History` uncapped,
exactly as today. (j) (round 3, supersedes (c)'s SIZING, not its `Measured{0, 0}` claim)
`seedFillFixtures` sizes from the CAPPED History, because the binary's `budget()` History at 8192 is
`max(8192 − 4096 − 512, 256) = 3584` while the uncapped 6292 lands the fill at ~95% and fails
`fillNoticeLine`'s "percent in the fifties" regexp; `cmd/apogee` is package main and cannot name the
unexported `compactMaxTokens` / `compactPromptOverheadTokens` / `compactMinTranscriptTokens`, so
export the cap as a helper (e.g. `agent.HistoryCap(window)`) for the e2e test to call — never a
re-pinned literal without the arithmetic stated in a comment. (k) (round 3) the cap reverses the
allocation-block sum sentence in `internal/domain/hooks.go` ("the rest is split across SystemPrompt,
FileContext, and History (they sum to ContextLimit - ResponseReserve)"): this item supersedes that
sentence and names it for rewording (History is the allocation capped at the fold's transcript
budget, so the three no longer sum), and strikes the matching sum assertion in
`TestBudgetSplitsTheAdvertisedWindowFromTheWorkingRoom`. (l) (round 3, owner decision) item 2's
prose rule is widened to reach the two comments its `60%\|48%\|working room` grep misses —
`internal/agent/autocompact_guard_test.go` and `internal/agent/prune_test.go`, each pinning the
8192 window's "~3.9k-token History allocation". The rule becomes: every comment under
`internal/agent` stating the History allocation as a fixed fraction OR as a pinned token figure for
a named window — grep `60%\|48%\|working room\|3\.9k` — is re-derived from the measured allocation;
at the 8192 window the cap makes History 3584 (a DECREASE from today's 3933), so both comments
state that number or cite `a.budget().History`. `internal/agent/autocompact_guard_test.go` and
`internal/agent/prune_test.go` join item 2's Files; item 4 keeps `internal/agent/prune_test.go` in
its own Files for the band re-sizing, and the deliberate overlap makes items 2 and 4 run serial.
Documented
decisions: this item yields to ADR 0018 §8
(`docs/adr/0018-context-overflow-recovers-structurally-the-emergency-fold-and-one-retry.md`, the
§8 crossover arithmetic; restated in code by `internal/agent/dispatch.go` `structuralFloor`) — the
cap in (a) keeps §8's ordering at every window; the §8 arithmetic gets a dated addendum line naming
the cap's new home (`budget()`) in item 5. It also yields to ADR 0077 §3
(`docs/adr/0077-the-context-fill-notice-is-the-first-engine-advise-reaction.md`; restated by
`internal/domain/budget.go` `HistoryFill` and `internal/agent/fillnotice.go`) — with the cap in
`budget()` the notice and the fold read one History, so the "never disagree" rule stands and gets
no addendum.
**Files:** internal/agent/loop.go, internal/domain/hooks.go, internal/agent/compact.go, internal/agent/dispatch.go, internal/agent/contextfiles.go, internal/agent/budget_test.go, internal/agent/turn_test.go, internal/agent/contextfiles_test.go, internal/agent/fillnotice_test.go, internal/agent/toolresultfloor_test.go, internal/agent/filerefs_test.go, internal/agent/autocompact_guard_test.go, internal/agent/prune_test.go, cmd/apogee/e2e_fillnotice_test.go
**Read first:** internal/agent/loop.go — budget, standingSystem, requestExceedsWindow; internal/agent/standingblocks.go — standingBlocks, standingRenders (the "context files" row is the file part; the ride-along rule returns nil when nothing seeds); internal/agent/compact.go — deriveGrowthBounds, compactMaxTokens, compactPromptOverheadTokens, compactMinTranscriptTokens;
internal/domain/hooks.go — Budget (Window vs ContextLimit, the allocation-block sum sentence); internal/agent/dispatch.go — structuralFloor, clampToolResult, clampToBound; internal/agent/contextfiles.go — ContextFilesReport, contextBlocks (no recursion: no standing render reads budget());
internal/agent/budget_test.go — TestBudgetIsHonestBeforeCalibration, TestBudgetSplitsTheAdvertisedWindowFromTheWorkingRoom; cmd/apogee/e2e_fillnotice_test.go — seedFillFixtures, fillFixtureShare, fillNoticeLine
**Tests.** `internal/agent/budget_test.go`: `TestBudgetIsHonestBeforeCalibration` and
`TestBudgetSplitsTheAdvertisedWindowFromTheWorkingRoom` now compare against `Allocate` fed the same
measurement the Agent takes (a helper on the test side renders the standing blocks and estimates
them), repinned to `min(alloc.History, max(Window − (compactMaxTokens + compactPromptOverheadTokens), compactMinTranscriptTokens))`,
with the allocation-sum assertion struck (the three parts no longer sum to
`ContextLimit − ResponseReserve`) and the `window: 0, working: 200000` row proving History stays
UNCAPPED when no window is advertised;
and a new test shows a session with a large `AGENTS.md` gets a larger `FileContext` and a
smaller History than the same session without it, with History never below 50% of working.
`turn_test.go` `TestGrowthBounds_WorkingWindowKeepsReaderNumbers` sizes its fixtures from the
Agent's own `budget()` (or the measured `Allocate`), never the unmeasured split; `deriveGrowthBounds`
stays uncapped, so no 16k `structuralFloor() <= transcriptBudget` case. `e2e_fillnotice_test.go`
`seedFillFixtures` sizes from the CAPPED History at `fillNoticeWindow` — the exported helper of
guard (j), never the uncapped `Allocate(fillNoticeWindow, 0, 0, Measured{0, 0}).History` — and
`fillNoticeConfig` pins `use-default-prompt: false` (comment: with no context file nothing renders
under the ride-along rule, both parts measure 0 and floor at 2%, so `Measured{0, 0}` is exact);
`fillNoticeLine`'s "percent in the fifties" regexp still matches.
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

## 3. The oversize notice reads the advisory ceiling — ✅ DONE (2026-09-22)

NOTES (2026-09-22): `internal/agent/contextfiles_test.go` needed no edit — item 2 already repinned `TestContextFilesReportMeasuresStandingContent` to `a.budget().StandingAdvisory` and its `Oversize()` assertion, which is exactly what this item's Tests ask it to keep; it is therefore not in FILES.
NOTES (2026-09-22): the item's grep is a floor, so two comments outside its Files list that call the ceiling an ALLOCATION were folded in (comment text only): `internal/agent/contextfiles.go`'s `ContextFilesReport` method doc ("the Budget share that content is allocated" / "no allocation to compare against") and `internal/tui/model.go`'s `noteContextFiles` doc ("the window share allocated to it").
NOTES (2026-09-22): consequential edit — internal/tui/model.go: made necessary by the ceiling no longer being an allocation (doc comment on noteContextFiles only; no behaviour change)
NOTES (2026-09-22): the rendered wording is untouched, so the phrase "its Budget share" survives in `internal/notice/contextfiles.go`'s emitted string and in the `ContextNotice` doc that quotes it; only the comments that called that share an ALLOCATION were reworded.
NOTES (2026-09-22): the ADR 0026 addendum is one dated `## Addendum (2026-09-22)` section covering both named lines (§8's `Budget.SystemPrompt` and the Considered-options rejection); §8 and the option line themselves are left as written, per the item's "addendum, not a rewrite".

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
(round 3) The grep is widened again to
`SystemShare\|system[- ]share\|system-prompt share\|Budget\.SystemPrompt\|allocated to it\|window share\|own share of the window`
over `internal/` — it reaches the two comments that call the ceiling an ALLOCATION,
`internal/notice/contextfiles.go` ("outgrown the window share allocated to it", which joins Files:
DOC COMMENTS only, the rendered wording stays) and `internal/domain/contextfile.go` ("the Budget's
own share of the window" / "a zero share, so nothing was allocated"). The new exact-wording case
goes in `internal/notice/contextfiles_test.go`, NOT `internal/agent/contextfiles_test.go`:
`internal/agent` imports `internal/notice` nowhere today (only `internal/tui` and `cmd/apogee` do),
and the case as drafted would silently open an engine→notice edge; `internal/agent`'s test keeps
`SystemShare == a.budget().StandingAdvisory` plus `Oversize()`. The ADR 0026 addendum also covers
the Considered-options line that rejects a configurable size limit "because the Budget already
allocates a system-prompt share" — after item 1 it no longer does, so the §8 addendum alone would
leave that reasoning stating a retired fact.
**Files:** internal/agent/contextfiles.go, internal/agent/contextfiles_test.go, internal/domain/contextfile.go, internal/notice/contextfiles.go, internal/notice/contextfiles_test.go, internal/notice/window.go, docs/adr/0026-workspace-context-files-are-session-scoped-prompt-data.md
**Read first:** internal/domain/contextfile.go — ContextFilesReport, Oversize, SystemShare; internal/notice/contextfiles.go — ContextFileNotices, ContextNotice; internal/agent/contextfiles.go — ContextFilesReport, readContextFile;
internal/agent/contextfiles_test.go — TestContextFilesReportMeasuresStandingContent, TestContextFilesReportWithoutWindowOrFiles; internal/notice/contextfiles_test.go; internal/notice/window.go — WindowUnknown;
docs/adr/0026-workspace-context-files-are-session-scoped-prompt-data.md — §8, Considered options; internal/eventjson/writer.go — ContextFiles
**Tests.** `contextfiles_test.go` `TestContextFilesReportMeasuresStandingContent` asserts
`SystemShare == a.budget().StandingAdvisory` (repinned in item 2, kept here) plus `Oversize()`; the
new exact-wording case lives in `internal/notice/contextfiles_test.go` — a report whose standing
content fits its measured reservation but exceeds 15% of working STILL renders the notice with the
exact wording above (drive `ContextFileNotices`, compare the emitted string), so no engine→notice
import is opened; the surfaces `tui/model.go`
`noteContextFiles`, `headless.go` `contextFilesFrame`, `schedule.go` `contextAnomalies` need no
change and their existing tests keep passing.
**Acceptance.** `go build ./... && go test ./internal/agent -run 'TestContextFiles' -count=1 && go test ./internal/notice ./internal/domain -count=1`
**Commit:** `fix(agent): the oversize notice keeps its fixed advisory ceiling`

## 4. Prune band 70% / 50% and six protected Turns — ✅ DONE (2026-09-22)

NOTES (2026-09-22): the fixtures in `internal/context/prune_test.go` are re-derived rather than re-tuned: two new helpers do it — `fillerTurns(name, n, size)` builds n filler Turns and `pruneConvPadded` appends `PruneKeepTurns` of them, so the protected window's size is written down only in the constant; `bandHistory(t, conv, reclaim)` returns a History allocation between `after/pruneLowFraction` and `full/pruneHighFraction` (measured with `domain.PromptChars`), replacing the hand-picked `History: 3000` / `2000` in the two ordering tests.
NOTES (2026-09-22): `TestPruneDoesNothing`'s first two cases ("unknown window", "under the high fraction") were also padded through `pruneConvPadded` — at keep=6 their five hand-written Turns would have made the protected-index gate, not the fraction, the reason the pass declines.
NOTES (2026-09-22): `TestPruneBandIsSeventyToFifty` (new, per the item's Tests) also pins the ratified constants directly (`pruneHighFraction != 0.7 || pruneLowFraction != 0.5 || PruneKeepTurns != 6`), since nothing else in the tree pinned the six.
NOTES (2026-09-22): the item's comment sweep is a floor, so `TestAutoPruneStubsOldTurnsAndKeepsTheRecentWindow`'s doc line ("a history of six tool-calling Turns") was folded in — it named the fixture by the old window; it now says "two tool-calling Turns more than the protected window". The re-derived figures: `pruneConfig` 70% trigger ≈ 10k chars (3,584 × 0.7 × 4), `prune_test.go` ~32k chars seeded (PruneKeepTurns+2 Turns), `stepnotice_test.go` ~24k chars seeded (PruneKeepTurns Turns).
NOTES (2026-09-22): `cmd/apogee/e2e_reactionidentity_test.go` (~line 153) still describes the History allocation as "60% of the same working window, ~62.9k characters" — item 2's prose rule was scoped to `internal/agent`, so this copy outside it kept the retired fixed-share figure. Left alone (not this item's band comment); reported on FOLLOW-UP. The test itself still passes: the measured History at that window is larger than the figure the comment quotes, so the fixture stays under the clamp.

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
sites are not this item's (owners named in the What). (round 3) Rewording only the band leaves the
two owned comments' FIGURES wrong: every figure in `internal/agent/prune_test.go` `pruneConfig`
("History allocation ≈ 3.9k tokens; the 60% trigger ≈ 9.4k chars") and in
`internal/agent/stepnotice_test.go` ("~16k chars, past the ~9.4k-char trigger") is re-derived from
`a.budget().History` at the 8192 window — item 2's cap makes it 3584, so the 70% trigger is
~10k chars — and from `PruneKeepTurns` (`seedToolTurns(a, PruneKeepTurns, 4000)` now seeds ~24k
chars, not ~16k); none of 3.9k / 9.4k / 16k is carried over. The named non-targets also gain
`internal/agent/fillnotice_test.go`'s fill-ladder rungs ("from 40% to 80%", "= 40%" — item 2
repins those) and `internal/agent/autocompact_guard_test.go`'s compaction fixture ("The four
Turns"), neither of which is the prune band.
**Files:** internal/context/prune.go, internal/context/doc.go, internal/context/prune_test.go, internal/agent/prune.go, internal/agent/prune_test.go, internal/agent/stepnotice_test.go
**Read first:** internal/context/prune.go — pruneHighFraction, pruneLowFraction, PruneKeepTurns, Prune, pruneProtectedIndex; internal/context/prune_test.go — pruneConv, readCall, TestPruneProtectsTheRecentToolCallingTurns, TestPruneOrdersOldestTurnFirst, TestPruneDoesNothing;
internal/agent/prune_test.go — pruneConfig, seedToolTurns, TestAutoPruneStubsOldTurnsAndKeepsTheRecentWindow; internal/agent/stepnotice_test.go — TestStepNoticeIsToldAgainAfterAPruneStubbedIt, TestTokenNoticeIsToldAgainAfterAPruneStubbedIt; internal/domain/budget.go — HistoryExceedsFraction, PromptChars;
internal/agent/compact.go — compactMaxTokens, compactPromptOverheadTokens, compactMinTranscriptTokens; internal/agent/prune.go — autoPrune; internal/context/doc.go — package comment (Pruning paragraph)
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

## 5. Docs: CONTEXT.md, ADR, config template, manual — ✅ DONE (2026-09-22)

NOTES (2026-09-22): the new ADR is numbered **0084** (0083 was the highest in the tree) and its front matter names what it amends: ADR 0018 §7/§8, ADR 0026 §8 and ADR 0077.
NOTES (2026-09-22): the two addenda are a dated `##` section each rather than a literal single line — each file's existing addenda/amendments are sectioned that way, and the content the item mandates (which figures are retired, what History is now, the cap's new home in `budget()`, and that ADR 0077 §3's "never disagree" rule stands) does not fit one line. Neither ADR body is rewritten, and neither addendum calls the retired split "the floor case".
NOTES (2026-09-22): the item's sweep grep (`60%|40%|four most recent|15%|25%|48%` across CONTEXT.md, docs/adr, docs/manual, internal/config/defaults) was run after the edits; every surviving hit is either a named non-target (ADR 0018 §9's and ADR 0071's tool-result-cap 40%, ADR 0026 §8's 15%, ADR 0022's "25% of runs"), a line the new addenda cover by quotation (ADR 0018 105/132/305-306, ADR 0077 24/25/81), or the new correct text in CONTEXT.md. `docs/manual/` has no remaining hit.

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
ADR 0022's "25% of runs" are named non-targets. (round 3, owner decision) item 5's named
non-target list gains ADR 0026 §8's "15% of working room" line (the `15%` grep hits
docs/adr/0026-*.md around lines 128 and 171) — item 3 owns that line through
`Budget.SystemPrompt`, so item 5 leaves it alone. (round 3) The acceptance greps are repointed to
patterns that actually bite the tree: `four most recent` matches CONTEXT.md nowhere (it wraps —
"it protects the four" / "most recent tool-calling Turns") and `four turns` matches neither
`config.yaml` nor `configuration.md` ("The four most" / "# recent tool-calling turns"), so all
three files could keep "four" with the check green; and NO pattern reached the band's LOW bound
("under **40%**" in CONTEXT.md and configuration.md, "back under 40%" in config.yaml), so the
40% → 50% edit could be skipped anywhere. Use `protects the four` and `under \*\*40%\*\*` for
CONTEXT.md and `four most` and `40%` for config.yaml / configuration.md — all match today.
**Files:** CONTEXT.md, docs/adr/0018-context-overflow-recovers-structurally-the-emergency-fold-and-one-retry.md, docs/adr/0077-the-context-fill-notice-is-the-first-engine-advise-reaction.md, docs/adr/<next>-the-budget-reserves-what-the-standing-content-measures.md, internal/config/defaults/config.yaml, docs/manual/configuration.md
**Read first:** CONTEXT.md — **Budget** entry, **Pruning** entry ("quiescent **Turn boundary**", "above **60%**", "under **40%**", "protects the four"), **Context files** entry ("Budget's system-prompt share"); docs/adr/0018-context-overflow-recovers-structurally-the-emergency-fold-and-one-retry.md — §8 structural floor, "History is ~60% of the working room (~48% of the window)", Amendment (2026-08-26);
docs/adr/0077-the-context-fill-notice-is-the-first-engine-advise-reaction.md — "Two facts fixed the design", Addendum (2026-09-19) heading form, Rejected "~48%" line; internal/config/defaults/config.yaml — `prune-tool-results:` comment block; docs/manual/configuration.md — the **Pruning** paragraph after Compaction;
internal/agent/prune.go — autoPrune ("runs at EVERY Turn boundary, mid-Exchange included" — the source that makes the CONTEXT.md correction true); internal/context/budget.go — systemPromptFraction, fileContextFraction, defaultReserveFraction, Allocate; docs/adr/0026-workspace-context-files-are-session-scoped-prompt-data.md — §8 (item 3's site, not this one's)
**Tests.** None (docs only); `go test ./internal/config -count=1` proves the embedded template still parses.
**Acceptance.** `go build ./... && go test ./internal/config -count=1 && ! grep -n 'protects the four\|above \*\*60%\*\*\|under \*\*40%\*\*\|quiescent \*\*Turn boundary\*\*' CONTEXT.md && ! grep -n '60%\|40%\|four most' internal/config/defaults/config.yaml docs/manual/configuration.md`
**Commit:** `docs(context): the Budget measures its standing parts; prune band and window documented`
