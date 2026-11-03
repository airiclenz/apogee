# One request-room module, and a delegate's bounds as one table

**Goal:** One unexported `requestRoom` in `internal/agent` owns the request's measure, the fresh-result share and the clamp, built once per commit and measuring what `buildRequest` sends, trailers included. A delegate's step, token and time bounds become one table and one `delegateLimits` value, and the time bound gains its ¾ notice.
**Date:** 2026-11-02
**Status:** unexecuted
**sized for:** ~200k-context host
**base:** 00d5bf9f
**Sources:**
- `docs/reviews/architecture-review-2026-11-02.html` — cards #3 (request room) and #7 (delegate bounds)
- ADR 0018 (D7, D8, `adr-0018-amend-1102`) · ADR 0084 D1 · ADR 0006 · ADR 0071 Decision 1 clause (c) · ADR 0077 (addendum 0919b)
- `docs/plans/archived/2026-11-02 - 03 - fresh-result-overflow-plan.md` (item 4: the baseline the module preserves)
- capcom: `fresh-result-pins-blunt`, `tool-result-trailers-overflow`, `text-tool-measure-split`, `run-fill-value`

**Ratified design calls** (owner, 2026-11-02; the verifier's recommendations bulk-approved):
- **Time notice:** a delegate gets a ¾ time-budget notice (item 9).
- **Trailers:** advice, step, token and time notes count inside the fresh-result bound.
- **Bound's measure:** the bound weighs what `buildRequest` sends, through `requestContents()` (ledger note, wrap-up note, deferred injections included).
- **Refs:** @file/skill refs keep their History share (`refBound`); the asymmetry is recorded in an ADR 0018 amendment, not changed.
- **Renders:** one Budget and one standing render per append commit (`appendToolResult`'s commit), passed down as a value (never a cache): the request room derives its Budget from its own standing rows, and within that commit every post-tool-result cascade that reads `a.budget()` through `loopView` reads the room's Budget instead; `a.budget()` stays for call sites outside an append commit. A dated note on ADR 0084 D1.
- **Module scope:** measure + share + `clampToBound`. `elideNewestMessage`, the delegate `ElideMiddle` cap and the `CapToolResults` Floor guard stay where they are (ADR 0071 clause (c), apogee-sim parity).
- **Tool-surface measure:** the guard/gauge split on text-tool profiles stays as is (issue `text-tool-measure-split`).
- **Limits:** one `delegateLimits` value replaces the six bound fields; the step-only `max_steps` clamp lives on it but outside the table.

**Standing requirements:**
- skills: coding-standards
- Test-run rules (pass to every sub-agent): run only `GOMEMLIMIT=2GiB go test -count=1 -run '<pattern>' ./<one pkg>/`. Never run `-race` or `-cover` over a whole package or a pattern. Never run two test runs at once.
- Bubble Tea v2 names only (`tea.KeyPressMsg`, `msg.String()`); the tui `Model` is copied by value (ADR 0011: no `strings.Builder` by value).
- Items 2, 3, 5, 6 and 7 keep model-facing bytes identical: tool result bodies, elision markers, notice texts, error texts, result heads, wrap-up directives, snapshot words. Items 4, 8 and 9 change them and say how.
- Durations in tests go through `internal/testtime`; the time notice is driven through the injected clock (`Agent.now`).
- Any deviation from item text lands as a dated NOTES line under the item.

**Out of scope:**
- `CapToolResults`, `elideNewestMessage`, the delegate `ElideMiddle` cap.
- The text-tool measure split (`text-tool-measure-split`); one context-fill value per run (review #9, `run-fill-value`).
- A new glossary term for the request room.

**Regression check (2026-11-02, 00d5bf9f):**
- 2: guard folded (`requestTokens` stays as the test helper over `room.weight`; overflow pins added to the run).
- 3: recast (room Budget from its own rows; `loopView` handed the commit's room; scoped to the append commit); guards folded (unset-field fallback in `contextFillNotice`, `contextPrefix`/`clampRef`, wrapper test callers migrate, test files added); ADR 0084 D1 amended for the append commit per the ratified Renders call.
- 4: guard folded (one measure per append commit shared with the cascade; pending-result stand-in tail; `requestContents` takes the room; render-once test in Acceptance).
- 5: guard folded (constant, topic, threshold and `boundErrText` names kept; two renderers per row).
- 6: guard folded (keyed-literal sweep, `seat_test.go`, hit-setting helper, named `-run` tests).
- 7: guard folded (keyed-literal sweep and `-run` widening as item 6; conditional `hostcopy_test.go` row; comment rule).
- 8: guard folded (trailers subtracted after the ceiling; `fillnotice.go` comment; bite stated on the post-item-7 tree).
- 10: guard folded (every doc or template comment listing the delegate's budget notices names the time notice; case-insensitive prose grep).
- 3 (re-check): guards folded (`newProjection` takes the room's Budget and derives the reply cap from it, `maxOutputTokens()` kept no-arg; `loopView(turn)` and `resolveSkillRefs` keep their signatures; the render-count test arms the fill notice; `-run` widened to the budget, output-cap and sub-agent context tests).
- 4 (re-check): decision applied (the render-once Acceptance depends on item 3 removing the `maxOutputTokens` second render in `newProjection`).

## 1. Loop-level pins bite on the fold elision and the room-left share — ✅ DONE (2026-11-03)

NOTES (2026-11-03): bite proven locally — `elideNewestMessage` forced to keep-whole fails TestFoldElidesAnOversizedNewestMessageEndToEnd (summary call 16722 tokens vs 4096, no marker); `freshResultBound` forced to `structuralFloor()` fails TestFreshResultTakesOnlyTheRoomLeftEndToEnd (summarizer calls = 1, want 0); the two existing Seqcom pins stay green under both reverts, as the issue records.
NOTES (2026-11-03): the elision pin reaches the fold through the predictive guard on the second Exchange (uncalibrated window) and also asserts the prompt's head and tail survive; the room-left pin calibrates (`calibrate`) so the guard measures the bare room, and checks the room-left setup (between the 1/8 floor and the History allocation) at the append point.

**What:**
**Goal:** Loop-level tests in `internal/agent` fail when the fold transcript keeps an oversized newest message whole, and fail when the fresh-result bound returns the whole History allocation instead of the room left. Test-only; no production change.
**Approach (assumed at the header base):**
- Add the pins in `freshresult_overflow_test.go` beside `TestFreshOversizedResultFitsTheNextRequest` and `TestSeqcomOverflowBodyFoldsAndRetries`, driving the loop through the scripted upstream.
- Elision pin: a newest message larger than the transcript budget reaches the emergency fold. Assert the summarizer request the upstream receives fits the transcript budget and carries the elision marker (`elideNewestMessage`, internal/context/compact.go).
- Room-left pin: a known, calibrated window whose conversation leaves less room than `structuralFloor()`, then one oversized single-call result. Assert the next request the upstream receives estimates at or under the room, and that no summarizer request precedes it.
- The verifier proves the bite by temporarily reverting each mechanism (keep-whole in `elideNewestMessage`; `freshResultBound` returning `structuralFloor()`) and seeing the matching pin fail.

**Files:** internal/agent/freshresult_overflow_test.go
**Read first:** internal/agent/freshresult_overflow_test.go — TestFreshOversizedResultFitsTheNextRequest, seqcomAgent; internal/context/compact.go — renderBudgetedTranscript, elideNewestMessage; internal/agent/toolresultfloor_test.go — freshRoom, requestTokens; internal/agent/dispatch.go — freshResultBound; internal/domain/conversation.go — PrefixEnd (a clamped tool result is ≤ historyFloor ≤ HistoryCap = transcriptBudget, so the oversized newest message must be an unclamped user prompt past the prefix, i.e. a second Exchange)
**Closes:** fresh-result-pins-blunt
**Tests:** `TestFoldElidesAnOversizedNewestMessageEndToEnd`, `TestFreshResultTakesOnlyTheRoomLeftEndToEnd`.
**Acceptance:**
- `go vet ./internal/agent/`
- `GOMEMLIMIT=2GiB go test -count=1 -run 'TestFoldElidesAnOversizedNewestMessageEndToEnd|TestFreshResultTakesOnlyTheRoomLeftEndToEnd|TestFreshOversizedResultFitsTheNextRequest|TestSeqcomOverflowBodyFoldsAndRetries' ./internal/agent/`
**Commit:** `test(agent): pin the fold elision and the room-left share at loop level`

## 2. Extract the request room

Depends on item 1.

**What:**
**Goal:** An unexported `requestRoom` type in `internal/agent/requestroom.go` owns the request measure, the room left, the fresh-result share, the window check and the clamp, and its table tests build it with no Agent. Model-facing bytes and fold timing are unchanged.
**Approach (assumed at the header base):**
- `requestRoom` holds one `domain.Budget`, its `growthBounds` (`deriveGrowthBounds`), the standing render and the tool menu. A constructor takes those values; `Agent.requestRoom()` gathers them.
- Methods take the messages they weigh: `weight(msgs)`, `left(msgs)`, `freshShare(msgs, pending)` (the `freshResultBound` arithmetic, unknown window → `historyFloor`, share clamped to [`historyFloor/freshResultFloorDivisor`, `historyFloor`]), `exceeds(req, conv)` (the `requestExceedsWindow` arithmetic, room × `uncalibratedRoomMargin` when `Used == 0`), `fit(content, bound)` (the `clampToBound` body, moved, not copied).
- `freshResultBound`, `clampToBound` and `requestExceedsWindow` stay as thin Agent wrappers over one room each; item 3 removes them.
- One deep module, one reason to change: no second copy of any arithmetic stays in `dispatch.go` or `loop.go`.
- The `requestTokens` helper in `toolresultfloor_test.go` becomes `room.weight`.

**Regression guard.** `requestTokens(a, msgs)` stays as the test helper in `toolresultfloor_test.go`, its body delegating to `room.weight` (replacing the last Approach bullet's rename): `freshresult_overflow_test.go` calls it and is not edited by this item. The overflow pins run in this item's Acceptance.

**Files:** internal/agent/requestroom.go, internal/agent/requestroom_test.go, internal/agent/dispatch.go, internal/agent/loop.go, internal/agent/toolresultfloor_test.go
**Read first:** internal/agent/dispatch.go — freshResultBound, clampToBound, structuralFloor; internal/agent/loop.go — requestExceedsWindow, predictiveFoldDue; internal/agent/compact.go — deriveGrowthBounds; internal/agent/toolresultfloor_test.go — requestTokens; internal/agent/freshresult_overflow_test.go — TestFreshOversizedResultFitsTheNextRequest
**Tests:** `requestroom_test.go` table tests: floor, ceiling, unknown window, share rolls forward across `pending`, uncalibrated margin, `fit` byte-identical to the moved clamp on a long-line and a many-line body.
**Acceptance:**
- `go build ./...` · `go vet ./internal/agent/`
- `GOMEMLIMIT=2GiB go test -count=1 -run 'TestRequestRoom|TestFreshResult|TestFreshOversized|TestSeqcomOverflow|TestOversizedToolResult|TestClampToBound|TestUnknownWindowClamp|TestPredictiveGuard|TestKnownWindowIgnores|TestFloorGuard_ToolResultCapTrimsAnOlderResultUnderBypass' ./internal/agent/`
**Commit:** `refactor(agent): one request-room module for the measure, the share and the clamp`

## 3. One room per commit

Depends on item 2.

**What:** Recast at the regression check (2026-11-02).
**Goal:** A tool-result commit renders the standing system and reads the Budget once, and every reader of the request's measure takes a `requestRoom` value instead of composing its own. A render-count test pins it; model-facing bytes, the fill notice and the estimate are unchanged.
**Approach (assumed at the header base):**
- `commitCall` builds one room and passes it down as a value: `appendToolResult` takes it (clamp, `EstimatedTokens`, `contextEstimateEvent`), and the post-tool-result cascade's `contextFillNotice` reads it from a transient turn-state field set and cleared inside `commitCall` (never configured, never serialized, like `resultsToAppend`). Every other `appendToolResult` caller builds one room at its own entry.
- `contextEstimateEvent` takes the room; `ContextCost`/`ContextEstimate` keep their public signatures and build one room at entry, sharing an unexported body with the per-commit path. The room carries the `standingRenders` rows so `ContextCost`'s rows and `standingSystem`'s join read one render.
- `requestExceedsWindow`/`predictiveFoldDue` build one room per check; `structuralFloor` and `refBound` read the room their caller built once per message.
- Remove item 2's thin wrappers once no caller is left.

**Regression guard.** The request room derives its Budget from its own standing rows (one standing render per appendToolResult commit); within that commit every post-tool-result cascade that reads a.budget() through loopView reads the room's Budget instead (loopView is handed the commit's room), while a.budget() stays for call sites outside an append commit — "one render per commit" is scoped to the append commit, stated in the item and the header Renders call. `contextFillNotice` builds its own room when the transient turn-state field is unset (`adviseOneCall` fires the cascade outside `commitCall`; `TestContextFillNoticeMeasuresTheAppliedBound` must never read a zero room). `refBound`'s second caller `contextPrefix` (`subagent.go`) reads the room too, and `clampRef` moves onto `room.fit`. The wrappers' test callers (`predictiveguard_test.go`, `unknownwindow_test.go`, `toolresultfloor_test.go`, `fillnotice_test.go`, `filerefs_test.go`, `turn_test.go`) migrate to the room, and every `appendToolResult` test caller takes the new parameter. ADR 0084 D1 (`adr-0084-decision.md`, "taken on every budget() call and never memoised") is amended for the append commit by the header's Renders call; item 10 writes the dated note. `newProjection` takes the Budget and derives the reply cap from it (a budget-taking body under `maxOutputTokens()`, which stays no-arg for `budget_test.go`, `rebind_test.go`, `switchupstream_test.go`), so `loopView`→`newProjection` never reads `a.budget()` a second time. `loopView(turn)` keeps its signature and reads the commit's room from the transient turn-state field (`fanout_test.go`, `hooksynthesis_test.go`, `writedetection_test.go` call `loopView(0)`); `resolveSkillRefs` keeps its signature (`skillmount_test.go`).

**Files:** internal/agent/requestroom.go, internal/agent/dispatch.go, internal/agent/fillnotice.go, internal/agent/contextcost.go, internal/agent/loop.go, internal/agent/reactions.go, internal/agent/subagent.go, internal/agent/turn.go, internal/agent/requestroom_test.go, internal/agent/fillnotice_test.go, internal/agent/advise_test.go, internal/agent/advise_argv_test.go, internal/agent/toolresultmarker_test.go, internal/agent/toolresultfloor_test.go, internal/agent/predictiveguard_test.go, internal/agent/unknownwindow_test.go, internal/agent/filerefs_test.go, internal/agent/turn_test.go
**Read first:** internal/agent/dispatch.go — commitCall, appendToolResult; internal/agent/loop.go — loopView, newProjection, maxOutputTokens;
internal/agent/fillnotice.go — contextFillNotice; internal/agent/standingblocks.go — standingRenders; internal/agent/subagent.go — contextPrefix
**Tests:** `TestAppendToolResultRendersStandingOnce` (counts `standingRenders` calls through an unexported seam, nil in production, across one `commitCall` — the append and its post-tool-result cascade — and asserts 1; it arms `cfg.ContextFillNotice`, leaves the max-output pin unset and has no usage reading, so `contextEstimateEvent` takes the `ContextEstimate` path); existing `TestContextFillNotice*`, `TestContextCost*`, `TestContextEstimate*`, `TestBudget*`, `TestMaxOutputTokens*`, `TestTurnRequestCarriesTheOutputCap`, `TestSubAgentContext_*` green.
**Acceptance:**
- `go build ./...` · `go vet ./internal/agent/`
- `GOMEMLIMIT=2GiB go test -count=1 -run 'TestAppendToolResultRendersStandingOnce|TestContextFillNotice|TestContextCost|TestContextEstimate|TestFreshResult|TestRequestRoom|TestPredictiveGuard|TestUnknownWindow|TestKnownWindow|TestAdvise|TestAppendTool|TestToolOutcome|TestClampTo|TestResolveFile|TestResolveRefs|TestResolveSkill|TestInterject|TestGrowthBounds|TestBudget|TestMaxOutputTokens|TestTurnRequestCarriesTheOutputCap|TestSubAgentContext' ./internal/agent/`
**Commit:** `refactor(agent): one request room per commit, read by every measure`

## 4. The bound weighs the request buildRequest sends

Depends on item 3.

**What:**
**Goal:** The fresh-result share weighs the same contents `buildRequest` sends: the standing system, the conversation, the delegation-ledger note, the wrap-up note and the pending deferred injections. Request bytes are unchanged. **Model-facing change:** a clamped result is shorter by the weight of a pending note or injection, and only when one is pending.
**Approach (assumed at the header base):**
- Split `buildRequest` into two shared halves: the message list (standing system + conversation) and one tail-stamping step (ledger `NoteOnTail`/`AppendToSystem` fallback, wrap-up directive, `InjectContext` for each deferred inject, in that order).
- `requestContents(turn)` returns an unarmed `domain.NewRequest` over those halves, with the deferred injections peeked, never drained. Add `Conversation.PeekDeferred()` (a copy) beside `TakeDeferred`. `buildRequest` stays `newProjection` + the same tail step over the drained list. No second copy of the note order.
- `requestRoom.freshShare` weighs `requestContents`' messages and tools. `buildRequest`/`armRequest` are still never called from `appendToolResult`.
- Rewrite the doc comment of every function this item changes; `freshResultBound`'s "composed here from its parts" sentence is retired.

**Regression guard.** Same scoping as item 3 — the buildRequest-contents measure is taken once per append commit and shared by the bound and the cascade. `requestContents` takes the room (its standing render, menu and Budget) and never re-renders standing. The tail step weighs the conversation plus a stand-in tool message for the pending result (empty content), so `NoteOnTail`/`InjectContext` take the branches they take once the result is committed (not the `AppendToSystem` fallback an assistant tail gives). The render-once Acceptance (`TestAppendToolResultRendersStandingOnce`) relies on item 3 removing the `maxOutputTokens` second render (in item 3's `newProjection` / room construction); this item does not chase that render, and its verifier reads it as item 3's.

**Files:** internal/agent/loop.go, internal/agent/requestroom.go, internal/agent/dispatch.go, internal/domain/conversation.go, internal/domain/conversation_test.go, internal/agent/toolresultfloor_test.go
**Read first:** internal/agent/loop.go — buildRequest, newProjection; internal/domain/request.go — NoteOnTail, InjectContext; internal/domain/advice.go — RenderEngineNote; internal/domain/conversation.go — TakeDeferred; internal/agent/children.go — delegationLedger.note; internal/agent/subagent.go — wrapUpDirective
**Tests:** `TestFreshResultCountsAPendingInjection` and `TestFreshResultCountsTheLedgerNote`: a queued deferred injection, or two delegations for the ledger note, plus one clamped result. The next request stays within the room and `predictiveFoldDue(buildRequest)` is false; both fail at the header base. `TestPeekDeferredLeavesTheQueue`. `TestDelegationLedger*` green.
**Acceptance:**
- `go build ./...` · `go vet ./internal/agent/ ./internal/domain/`
- `GOMEMLIMIT=2GiB go test -count=1 -run 'TestPeekDeferredLeavesTheQueue|TestConversationDeferred' ./internal/domain/`
- `GOMEMLIMIT=2GiB go test -count=1 -run 'TestFreshResultCounts|TestFreshResult|TestPredictiveGuard|TestDelegationLedger|TestAppendToolResultRendersStandingOnce' ./internal/agent/`
**Commit:** `feat(agent): the fresh-result bound weighs the request buildRequest sends`

## 5. A delegate's bounds as one table

**What:**
**Goal:** One table in `internal/agent/delegatelimits.go` holds a row per bound (steps, tokens, time), and every per-bound fact reads it: which bound tripped, its words on every surface, and its ¾ notice. Every rendered string is byte-identical; a golden row test pins each surface.
**Approach (assumed at the header base):**
- One `boundRow` per `delegateBound`: snapshot word, the error format (`stepCapErrFormat`, …), the report and non-report result heads, the wrap-up directive format, the workflow finish cause, the `domain.DelegationBound`, a value renderer (`boundDurationText` for time, `formatTokens` for tokens), and an optional notice (topic, share, prompt line, render). Time has no notice yet.
- Rewrite over the table: `delegateBoundHit` (order steps, tokens, time), `boundErrText`, `capHeadFormats`, `delegationBound`, `wrapUpDirective`'s switch, `wrapUpFinishDirective`, `delegateBoundWords`/`parseDelegateBound`, `stepNoticeThreshold`/`tokenNoticeThreshold`.
- `stepBudgetNotice`/`tokenBudgetNotice` become one `budgetNotices()` that returns the fired notes in row order. `appendToolResult` appends them in that order after the advice. Latch, depth-0 silence and the zero-means-off rule are unchanged.
- The format constants keep their text exactly; they move into the rows.

**Regression guard.** Every format constant (`stepCapResultFormat`, `wrapUp*DirectiveFormat`, …), `stepNoticeTopic`/`tokenNoticeTopic`, `stepNoticeThreshold`/`tokenNoticeThreshold` and `boundErrText` keep their names, as constants the rows reference or thin wrappers over them, so the test files that name them compile unchanged. Each row keeps two renderers: the marker value (the raw `%d` for steps and tokens, `boundDurationText` for time) and the notice's own render (`formatTokens`, token notice only) — the token markers stay `(20000000 tokens)`.

**Files:** internal/agent/delegatelimits.go, internal/agent/delegatelimits_test.go, internal/agent/agent.go, internal/agent/subagent.go, internal/agent/workflowspawn.go, internal/agent/state.go, internal/agent/stepnotice.go, internal/agent/dispatch.go
**Read first:** internal/agent/agent.go — delegateBoundHit, boundErrText, boundDurationText; internal/agent/subagent.go — capHeadFormats, wrapUpDirective; internal/agent/state.go — parseDelegateBound; internal/agent/stepnotice.go — tokenBudgetNotice; internal/agent/subagent_test.go — TestCapResultHead_NonReportVariantsKeepTheBoundPrefix
**Tests:** `TestBoundRowsRenderEverySurface` (golden strings, no driven child), `TestParseDelegateBoundRoundTrips`. `TestSubAgent_*Budget*`, `TestStepNotice*`, `TestTokenNotice*`, `TestWrapUpDirective*`, the snapshot round-trip and the workflow wrap-up tests green.
**Acceptance:**
- `go build ./...` · `go vet ./internal/agent/`
- `GOMEMLIMIT=2GiB go test -count=1 -run 'TestBoundRows|TestParseDelegateBound|TestSubAgent_.*Budget|TestStepNotice|TestTokenNotice|TestWrapUpDirective|TestSnapshot|TestWorkflowSpawn|TestBoundErrText|TestCapResultHead|TestSeat_|TestDelegateReport' ./internal/agent/`
**Commit:** `refactor(agent): a delegate's bounds read one table`

## 6. Tests reach the delegate bounds through one helper

Depends on item 5.

**What:**
**Goal:** No test outside `delegatelimits_test.go` names a delegate-bound field. Tests set and read the caps, start, hit and requested clamp through helpers there. Test-only.
**Approach (assumed at the header base):**
- Add helpers in `delegatelimits_test.go` that set the step, token and time caps and the start, and that read the hit and the requested clamp, over today's `Agent` fields (`stepCap`, `tokenCap`, `timeCap`, `capStart`, `capHit`, `capRequested`).
- Rewrite every test read or write of those fields to use the helpers. Item 7 then changes only the helpers.

**Regression guard.** The field sweep also covers keyed `&Agent{stepCap: …}`-style literals (grep the six field names as keyed literal keys in internal/agent and its _test files), and the item's -run pattern includes the tests that pin the seeded and the zero (top-level) bounds — `TestNewChildAgent_IsBuiltFromOneDelegationValue`, `TestNewAgentBuildsATopLevelAgent`, `TestStepCapNeverBoundsTheMainAgent`. The literals set `capHit` too (`subagent_test.go`, `seat_test.go`), so a helper also sets the hit. `stepnotice_test.go`'s `stepCap:`/`tokenCap:` table keys are local struct fields, not `Agent` fields, and stay.

**Files:** internal/agent/delegatelimits_test.go, internal/agent/stepnotice_test.go, internal/agent/subagent_test.go, internal/agent/construct_test.go, internal/agent/delegatereport_test.go, internal/agent/fanout_test.go, internal/agent/seat_test.go
**Read first:** internal/agent/stepnotice_test.go — stepNoticeChild, tokenNoticeChild; internal/agent/subagent_test.go — TestNewChildAgent_IsBuiltFromOneDelegationValue, TestBoundErrText_SaysCapped, wrapUpAgent; internal/agent/seat_test.go — TestSeat_FallbackNoteIsTheBodysLastLine; internal/agent/construct_test.go — TestNewAgentBuildsATopLevelAgent; internal/agent/fanout_test.go — TestFanOut_StartedPhaseCarriesTheAppliedCap
**Tests:** the rewritten tests, unchanged in what they assert.
**Acceptance:**
- `grep -nE '\.(stepCap|tokenCap|timeCap|capStart|capHit|capRequested)\b' internal/agent/*_test.go | grep -v delegatelimits_test.go` prints nothing
- `grep -nE 'Agent\{[^}]*\b(stepCap|tokenCap|timeCap|capStart|capHit|capRequested):' internal/agent/*_test.go | grep -v delegatelimits_test.go` prints nothing
- `go vet ./internal/agent/`
- `GOMEMLIMIT=2GiB go test -count=1 -run 'TestStepNotice|TestTokenNotice|TestSubAgent|TestNewAgentBuildsATopLevelAgent|TestNewChildAgent|TestStepCapNeverBounds|TestRunEndsTheExchangeAtTheStepCap|TestBoundErrText|TestCapResultHead|TestSeat_|TestDelegateReport|TestFanOut' ./internal/agent/`
**Commit:** `test(agent): reach the delegate bounds through one helper`

## 7. One delegateLimits value replaces the six bound fields

Depends on item 6.

**What:**
**Goal:** An `Agent` carries a delegate's bounds as one `delegateLimits` value, seeded once from the delegation and zero at depth 0. No `Agent` field named `stepCap`, `tokenCap`, `timeCap`, `capStart`, `capHit` or `capRequested` remains. Model-facing bytes are unchanged.
**Approach (assumed at the header base):**
- `delegateLimits` holds steps, tokens, time, start, hit and requested. Methods: `tripped(turns, prompt, now)` (steps, then tokens, then time; zero is off; a zero start never trips time), `noticeDue(row, …)`, `word(surface)`. It lives in `delegatelimits.go` beside the table.
- The delegation struct carries one `delegateLimits` and `delegation.seed` copies it verbatim. `Run` sets the start through `a.now` when time is bounded. `resolveStepCap`/`stepCapFor` and `delegationResult`'s clamp note read `requested` on the same value, outside the table.
- Readers move to the value: `agent.go`, `children.go`, `construct.go`, `subagent.go`, `turn.go`, `stepnotice.go`, `workflowspawn.go`. Item 6's helpers follow.

**Regression guard.** Same keyed-literal sweep and -run widening as item 6 (the production literal in `subagent.go`'s delegation seeding included); plus, cross-plan: plan `2026-11-02 - 04 - seat-and-resolved-entry-plan.md` item 14 adds a reflect field-coverage guard `internal/agent/hostcopy_test.go` that fails on any Agent field without a row — when that file exists at run time, item 7 replaces the six bound fields' rows with one `delegateLimits` row (per-delegation, classified as the six fields were) and lists the file on its **Files:** line as conditional; when it does not exist, nothing to do. Every comment naming an Agent bound field (`Agent.stepCap`, `Agent.capHit`, …) names the `delegateLimits` value instead.

**Files:** internal/agent/delegatelimits.go, internal/agent/delegatelimits_test.go, internal/agent/agent.go, internal/agent/children.go, internal/agent/construct.go, internal/agent/subagent.go, internal/agent/turn.go, internal/agent/stepnotice.go, internal/agent/workflowspawn.go, internal/agent/hostcopy_test.go (conditional: only when plan 04 item 14 has added it)
**Read first:** internal/agent/subagent.go — delegation (struct), newChildAgentOn, resolveStepCap; internal/agent/construct.go — delegation.seed; internal/agent/agent.go — Run (capStart set), delegateBoundHit; internal/agent/dispatch.go — stepCapFor; internal/agent/workflowspawn.go — wrapUpFinishDirective
**Tests:** `TestDelegateLimitsTripped` (order, zero means off, zero start); every delegate test from items 5 and 6 green.
**Acceptance:**
- `go build ./...` · `go vet ./internal/agent/`
- `grep -nE '^\s+(stepCap|tokenCap|timeCap|capStart|capHit|capRequested)\s' internal/agent/agent.go` prints nothing
- `grep -nE 'Agent\{[^}]*\b(stepCap|tokenCap|timeCap|capStart|capHit|capRequested):|^\s*(stepCap|tokenCap|timeCap|capStart|capHit|capRequested):' internal/agent/*.go` prints nothing
- `grep -nE 'Agent\.(stepCap|tokenCap|timeCap|capStart|capHit|capRequested)' internal/agent/*.go` prints nothing
- `GOMEMLIMIT=2GiB go test -count=1 -run 'TestDelegateLimits|TestBoundRows|TestStepNotice|TestTokenNotice|TestSubAgent|TestNewAgentBuildsATopLevelAgent|TestNewChildAgent|TestStepCapNeverBounds|TestRunEndsTheExchangeAtTheStepCap|TestBoundErrText|TestCapResultHead|TestSeat_|TestDelegateReport|TestFanOut|TestWorkflowSpawn|TestBackgroundHostCoversEveryAgentField' ./internal/agent/`
**Commit:** `refactor(agent): one delegateLimits value for a delegate's bounds`

## 8. Trailers count inside the fresh-result bound

Depends on items 4 and 7.

**What:**
**Goal:** A committed tool result plus its trailers (advice spans and the fired budget notes) stays within the fresh-result bound on a known and on an unknown window. This fixes `tool-result-trailers-overflow`: trailers were appended after the clamp, uncounted, so a clamped result landed the next request at room + trailer and forced a predictive fold before the model read it. **Model-facing change:** a clamped body is shorter by the trailers' weight; trailer text, order and span offsets are unchanged.
**Approach (assumed at the header base):**
- In `appendToolResult`, collect the advice and `budgetNotices()` first. Weigh them as the characters they add to the tool message (`PromptChars` with them minus without), through `requestRoom.weight`.
- Bound = clamp(share − trailer tokens, floor, `historyFloor`). With no window known: max(`historyFloor` − trailer tokens, floor). The floor (`historyFloor/freshResultFloorDivisor`) still holds, and the predictive fold makes room below it.
- Clamp, then append in the fence order: output, advice, budget notes in row order. Offsets are still measured from the clamped content. The wrap-up directive is decided after the Turn and stays outside the bound.
- `contextFillNotice` runs before the advise slot fills and keeps measuring the share without trailers.

**Regression guard.** Subtract the trailers after the ceiling: bound = max(min(share, `historyFloor`) − trailer tokens, floor), the same rule on both windows (unknown window: share = `historyFloor`), so a committed result plus trailers never exceeds `historyFloor`. Amend `fillnotice.go`'s "at its committed size" sentence to say the rung reads the result before its trailers.

**Files:** internal/agent/dispatch.go, internal/agent/requestroom.go, internal/agent/fillnotice.go, internal/agent/toolresultfloor_test.go, internal/agent/stepnotice_test.go
**Read first:** internal/agent/dispatch.go — appendToolResult, freshResultBound, commitCall; internal/agent/stepnotice.go — stepBudgetNotice; internal/domain/advice.go — WithAdvice, withLeadingAdvice; internal/agent/fillnotice.go — contextFillNotice; internal/agent/toolresultfloor_test.go — TestFreshResultIsBoundedByTheRoomLeft
**Closes:** tool-result-trailers-overflow
**Tests:** `TestClampedResultWithAdviceFitsTheRoom` (a ~2 KiB advice span on a single clamped result), `TestClampedDelegateResultWithStepNoticeFitsTheRoom` (a delegate at ¾ of its step cap, clamped read), `TestUnknownWindowClampCountsTrailers`. Each asserts the next request is ≤ room and `predictiveFoldDue(buildRequest)` is false, or the committed message is ≤ `historyFloor`. All fail at the header base, except `TestClampedDelegateResultWithStepNoticeFitsTheRoom`, which needs items 5–7: it fails with item 8's `appendToolResult` change reverted on the post-item-7 tree. Plus `TestTrailerFenceOrder`.
**Acceptance:**
- `go vet ./internal/agent/`
- `GOMEMLIMIT=2GiB go test -count=1 -run 'TestClampedResultWithAdviceFitsTheRoom|TestClampedDelegateResultWithStepNoticeFitsTheRoom|TestUnknownWindowClampCountsTrailers|TestTrailerFenceOrder|TestFreshResult|TestStepNotice|TestTokenNotice|TestContextFillNotice' ./internal/agent/`
**Commit:** `fix(agent): count a tool result's trailers inside its fresh-result bound`

## 9. A delegate's time-budget notice

Depends on item 8.

**What:**
**Goal:** A time-bounded delegate is told once, on the tool result that closes at or past three quarters of its `delegate-timeout`, how much time is spent and how much is left. The notice is an engine note under `[engine — time budget]` … `[end engine — time budget]`, with the step notice's latch, its depth-0 and unbounded silence, and on under Bypass. **Model-facing change:** a new note.
**Approach (assumed at the header base):**
- New `internal/agent/prompts/time-notice.txt`, exactly: `time: %s of %s elapsed — %s left before the wrap-up Turn; write your output now`. The fields are elapsed, cap and left. Each goes through `boundDurationText` after `Truncate(time.Second)`; left floors at 0. Example: cap 2h, elapsed 90m → `time: 1h30m of 2h0m elapsed — 30m left before the wrap-up Turn; write your output now`.
- The time row's notice: topic `time budget`, share 0.75. It fires when `a.now()` minus the start reaches `ceil(0.75 × time cap)` while no copy stands (`Conversation.HasEngineNote`). It rides `budgetNotices()` after the token note, so item 8 already counts it inside the bound.
- Update `stepnotice.go`'s header comment (three notices, one table) and the prompts listing in `doc.go`.

**Files:** internal/agent/delegatelimits.go, internal/agent/stepnotice.go, internal/agent/prompts/time-notice.txt, internal/agent/stepnotice_test.go, internal/agent/doc.go
**Read first:** internal/agent/stepnotice.go — stepBudgetNotice, stepNoticeLine; internal/agent/agent.go — Run (capStart = a.now()), boundDurationText; internal/agent/compact.go — mustPrompt; internal/agent/stepnotice_test.go — stepNoticeChild, TestStepNoticeIsToldAgainAfterAFoldSwallowedIt; internal/agent/subagent_test.go — TestSubAgent_TimeLimitEndsTheChildThroughTheWrapUp (steppingClock at 3h: the notice now fires there; its HasSuffix tail check stays green)
**Tests:** `TestTimeNoticeFiresOnceAtThreeQuartersOfTheLimit` (exact string, injected clock), `TestTimeNoticeIsSilentAtDepthZeroUnboundedAndUnderTheThreshold`, `TestTimeNoticeFiresUnderBypass`, `TestTimeNoticeIsToldAgainAfterAFoldSwallowedIt`, `TestTrailerFenceOrder` extended (step, token, time).
**Acceptance:**
- `go vet ./internal/agent/`
- `GOMEMLIMIT=2GiB go test -count=1 -run 'TestTimeNotice|TestTrailerFenceOrder|TestStepNotice|TestTokenNotice|TestBoundRows' ./internal/agent/`
**Commit:** `feat(agent): tell a delegate when three quarters of its time is spent`

## 10. Record the request room, the trailer rule and the time notice

Depends on item 9.

**What:**
**Goal:** The decision records, glossary and manual state what items 2–9 shipped, and no comment or doc names the fresh-result bound as composed from parts or leaves out its trailers.
**Approach (assumed at the header base):**
- `capcom know add --id adr-0018-amend-1102b --parent adr-0018`. It records: one request-room module (measure + share + clamp; `elideNewestMessage`, `ElideMiddle`, `CapToolResults` out); the bound weighs `buildRequest`'s contents; trailers count inside the bound; @file/skill refs keep their History share, an asymmetry kept on purpose. Name it on `adr-0018.md`'s Status line and reading list.
- A dated `> **Amended 2026-11-02 (this plan).**` blockquote under D1 in `adr-0084-decision.md`: one standing render per commit, passed as a value, is not a memo. Name it on `adr-0084.md`'s Status line.
- `capcom know add --id adr-0077-addendum-1102 --parent adr-0077`: the time notice, its wording and share. Name it on `adr-0077.md`'s Status line and reading list.
- Glossary `step-cap` Note and `docs/manual/configuration.md`'s `delegate-timeout` text: the time notice beside the step and token notices.
- Prose guard: every comment or doc naming the bound's old composition, or saying trailers ride outside the bound. Find them with `grep -rniE 'composed here|from its parts|freshResultBound|after the clamp|ladder order AFTER|advise slot' internal/ docs/manual .capcom/knowledge`. Fix every hit that states either. Comments in code are fixed only where items 2–9 left them stale; add their files to this item's NOTES.

**Regression guard.** Every doc or template comment that lists the delegate's budget notices names the time notice; find them with `grep -rnE 'token[- ]budget notice|step and token' docs/manual layout.md .capcom/knowledge/glossary internal/config/defaults` and add each hit to this item's NOTES (decision records change only by the amendments above). If `internal/config/defaults/config.yaml` changes, check `internal/config/testdata` goldens. The prose grep is case-insensitive and also matches `ladder order AFTER|advise slot`, so `appendToolResult`'s trailer comment is caught.

**Files:** .capcom/knowledge/decisions/adr-0018.md, .capcom/knowledge/decisions/adr-0018/adr-0018-amend-1102b.md, .capcom/knowledge/decisions/adr-0084.md, .capcom/knowledge/decisions/adr-0084/adr-0084-decision.md, .capcom/knowledge/decisions/adr-0077.md, .capcom/knowledge/decisions/adr-0077/adr-0077-addendum-1102.md, .capcom/knowledge/glossary/glossary-turns/step-cap.md, .capcom/knowledge/glossary/glossary-reactions/advice-span.md, docs/manual/configuration.md, docs/manual/commands.md, layout.md, internal/config/defaults/config.yaml
**Read first:** .capcom/knowledge/decisions/adr-0018.md — Status line, reading list; .capcom/knowledge/decisions/adr-0077.md — reading list (addendum-0919b); .capcom/knowledge/decisions/adr-0084/adr-0084-decision.md — section 1; .capcom/knowledge/glossary/glossary-turns/step-cap.md — token-budget notice paragraph; docs/manual/configuration.md — delegate-max-tokens notice paragraph, delegate-timeout; internal/agent/dispatch.go — appendToolResult comment; internal/config/reactions_test.go — TestManualStatesTheFloorGuardCount
**Tests:** none (docs).
**Acceptance:**
- `capcom check`
- `capcom know show "ADR 0018"` names `adr-0018-amend-1102b`; `capcom know show "ADR 0077"` names `adr-0077-addendum-1102`
- `GOMEMLIMIT=2GiB go test -count=1 -run 'TestManualStates' ./internal/config/`
**Commit:** `docs(adr): record the request room, trailers inside the bound and the time notice`
