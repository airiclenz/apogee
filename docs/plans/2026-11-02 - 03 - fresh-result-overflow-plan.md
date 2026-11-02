# Fresh tool result overflow — plan

**Goal:** a single oversized fresh tool result never overflows the next request: it is byte-elided to the room actually left, and a `context_length_exceeded` answer still reaches ADR 0018's one emergency fold, which now survives an oversized newest message.
**Date:** 2026-11-02
**Status:** unexecuted
**sized for:** ~200k-context host
**base:** e2495391
**Closes:** fresh-result-overflow

**Sources:**
- `.capcom/issues/fresh-result-overflow.md` (Seqcom run 20261102T1700Z)
- ADR 0018 (`capcom know show "ADR 0018" --with-children`) — §5 one fold per Turn, §7 predictive guard, §8 the bound lives in the loop, not the switchable guard
- ADR 0006 (structural reducers stay on under Bypass); glossary Notes `Budget`, `Compaction`, `Tool-result capping`

**Root cause (traced at base):** (A) `provider.isContextOverflow` misses `context_length_exceeded` / "prompt exceeds the context window", so the 400 is a plain fault and the Turn is abandoned; (B) `Agent.clampToolResult` bounds by the whole History allocation and elides by lines, so a minified JSON body passes whole; (C) `renderBudgetedTranscript` always keeps the newest message whole, so the fold's own summary call overflows on the same result.

**Ratified design calls (owner, 2026-11-02):**
- **Fresh-result bound:** dynamic — the room left in the request (advertised window (writer, 2026-11-02, from the regression check: the predictive guard's own room, so working-window never starves results) − reply reserve − estimate of the request so far), never above the History allocation, never below a floor of 1/8 of it; below the floor the predictive fold makes room. Window unknown: the existing 3072-token fallback bound.
- **Elision unit:** bytes — a result the line elision cannot bring under the bound is elided by `apogeectx.ElideMiddle` with the existing marker.
- **Parallel results:** even share that rolls forward — each result gets room ÷ results of the Turn still to append; a small result's unused share passes on.
- **Reactive answer:** ADR 0018's one fold per Turn stays the only retry; it is fixed to byte-elide an oversized newest message. No trim-first retry, §5 unchanged.
- **Placement:** the structural clamp in the loop (`appendToolResult`), on under Bypass, no config key — ADR 0018 §8. The `tool-result-cap` guard and its protected span are unchanged.

**Regression check (2026-11-02, e2495391):**
- 2: recast — one guarded byte-elision helper in `internal/context/toolresult.go`; supersedes ADR 0018's unconditional keep via item 6's amendment
- 3: recast — falls back to item 2's helper; depends on item 2
- 4: recast — room from `deriveGrowthBounds(a.budget())` (advertised window); count carried from `dispatchTools`; fill notice measures the bound
- 5: guard folded — `autoCompactConfig` and a hand-written Script
- 6: guard folded — adr-0018-amend-1102 also supersedes the raw-text consequences line
- 2 (re-check): guard folded — elide the newest only past the room after prefix + notice, to max(room, head + marker floor); the three over-budget tests kept; fold-keeps-newest comment sweep; helper exported and in -run
- 3 (re-check): guard folded (writer's decision) — TestToolResultFloorLeavesEverythingElseVerbatim's unshrinkable subcase flips to the byte-elided form; added to Tests and -run
- 4 (re-check): guard folded — Goal names the advertised window and structuralFloor(); TestClampToBound recast to one explicit bound; parallel-result sizing; -run widened, fill-notice pin named
- 5 (re-check): guard folded (writer's decision) — fixtures on configWithTools(...), not baseConfig/autoCompactConfig
- 6 (re-check): guard folded (writer's decision) — glossary Note path corrected

**Standing requirements:**
- skills: coding-standards
- This box: run tests only as `GOMEMLIMIT=2GiB go test -race -count=1 -run '<TestX>' ./<one package>/`; never a package pattern, never two runs at once.
- Durations in tests go through `internal/testtime`.

**Out of scope:**
- The elision marker wording (its `start_line/end_line` hint is misleading for `web_fetch`; separate issue if wanted).
- `web_fetch`'s 2 MiB transport cap; the `tool-result-cap` guard; `mid-exchange-auto-compaction`.

## 1. Classify `context_length_exceeded` as a context overflow

**What:**
**Goal:** an upstream 400 whose body carries `context_length_exceeded` or "exceeds the context window" classifies as a context overflow (the error reads "apogee: context window exceeded: …"), and every body classified as overflow before still is.
**Approach (assumed at the header base):** add the two markers to the marker list in `provider.isContextOverflow` (`internal/provider/client.go`), matched case-insensitively as the existing ones are. Fixes cause (A); the Seqcom body is the exact fixture.
**Files:** internal/provider/client.go, internal/provider/fault_test.go
**Read first:** internal/provider/client.go — isContextOverflow, faultError; internal/provider/fault.go — classify; internal/provider/fault_test.go — TestClassify;
internal/provider/reliability_test.go — TestRespond_ContextOverflow; internal/provider/stream_test.go — TestStream_ContextOverflow; internal/agent/collect.go — collect (overflow field)
**Tests:** a `TestClassify` case with the verbatim Seqcom body `{"error":{"message":"prompt exceeds the context window","type":"invalid_request_error","code":"context_length_exceeded"}}` on HTTP 400 → overflow; a case with only the code and only the message, each → overflow.
**Acceptance:**
- `go build ./internal/provider/`
- `GOMEMLIMIT=2GiB go test -race -count=1 -run 'TestClassify|TestRespond_ContextOverflow|TestStream_ContextOverflow' ./internal/provider/`
**Commit:** `fix(provider): classify context_length_exceeded as a context overflow`

## 2. The fold's transcript byte-elides an oversized newest message

**What:** Recast at the regression check (2026-11-02).
**Goal:** the compaction transcript rendered for a summary call never exceeds its character budget unless prefix and notice alone leave no room: a newest message over the room left after prefix and notice is kept head-and-tail with the elision marker, at no less than a floor that keeps a head plus the marker; a transcript that fits is unchanged byte for byte.
**Approach (assumed at the header base):** in `renderBudgetedTranscript` (`internal/context/compact.go`), where the most recent message is kept "even if it alone is over budget", render it through `ElideMiddle` (`internal/context/toolresult.go`) bounded by the budget left after the message's framing. Fixes cause (C): ADR 0018's emergency fold faulted on the very result that overflowed.
**Regression guard.** Item 2 introduces ONE guarded byte-elision helper in internal/context/toolresult.go (beside ElideMiddle) that takes only (content, maxBytes), derives headBytes itself from (maxBytes − marker length) split head/tail, never panics on a bound smaller than the marker or on negative room (returns a bare head cut at maxBytes, ≥ 0), and is UTF-8 safe; renderBudgetedTranscript uses it; add internal/context/toolresult.go and its test file to Files; unit tests cover bound < marker and bound ≤ 0. Supersedes ADR 0018's "keeps the most recent message unconditionally" (adr-0018-decision.md §8, adr-0018-amend-0802b) through item 6's adr-0018-amend-1102.
Re-check: elide the newest message only when it alone exceeds the room left after prefix + notice, and then to max(that room, a floor that keeps a head + the marker) — so TestCompactAppliesTranscriptBudget, TestRenderBudgetedTranscriptAlwaysKeepsMostRecentMessage and TestRenderBudgetedTranscriptNoticeOverBudgetKeepsMostRecentMessage are kept, unchanged and green. The helper is exported (item 3 calls it from internal/agent); its tests carry its name (e.g. `ElideToBytes`) and that name joins the -run. Sweep rule: every comment stating the fold transcript keeps the newest message whole — `grep -rn -i "UNCONDITIONALLY\|keeps the most recent\|whatever the budget says" internal/agent internal/context --include=*.go` (internal/agent/compact.go:58-62, filerefs_test.go:308, unknownwindow_test.go:158, emergencyfold_test.go:128) — is updated.
**Files:** internal/context/compact.go, internal/context/compact_test.go, internal/context/toolresult.go, internal/context/toolresult_test.go, internal/agent/compact.go, internal/agent/filerefs_test.go, internal/agent/unknownwindow_test.go, internal/agent/emergencyfold_test.go
**Read first:** internal/context/compact.go — renderBudgetedTranscript, elisionNotice; internal/context/toolresult.go — ElideMiddle, toolResultElisionMarker;
internal/context/compact_test.go — TestCompactAppliesTranscriptBudget, TestRenderBudgetedTranscriptAlwaysKeepsMostRecentMessage, TestRenderBudgetedTranscriptNoticeOverBudgetKeepsMostRecentMessage;
internal/agent/compact.go — compactTranscriptChars
**Tests:** a newest single-line message 10× the budget → rendered transcript ≤ budget and contains the marker; a transcript under budget renders identically to before; `TestCompactAppliesTranscriptBudget`, `TestRenderBudgetedTranscriptAlwaysKeepsMostRecentMessage` and `TestRenderBudgetedTranscriptNoticeOverBudgetKeepsMostRecentMessage` kept unchanged and green; in `toolresult_test.go`, the helper with a bound smaller than the marker and with a bound ≤ 0 → no panic, a bare head of at most max(bound, 0) bytes, valid UTF-8.
**Acceptance:**
- `go build ./internal/context/`
- `GOMEMLIMIT=2GiB go test -race -count=1 -run 'Compact|BudgetedTranscript|ElideMiddle|ElideToBytes' ./internal/context/`
**Commit:** `fix(context): byte-elide an oversized newest message in the fold transcript`

## 3. The structural clamp falls back to byte elision

**What:** Recast at the regression check (2026-11-02).
**Goal:** `Agent.clampToBound` returns content within its bound for any input shape: a few very long lines (minified JSON) come back byte-elided with the elision marker; content the line elision already brings under the bound comes back exactly as today.
**Approach (assumed at the header base):** in `clampToBound` (`internal/agent/dispatch.go`), after `apogeectx.TruncateToolResult`, when the result is still over the bound (in the chars the bound is measured in), elide with `apogeectx.ElideMiddle`. One fallback in the shared clamp — tool results and file refs (`loop.go`) both get it; no second clamp. Fixes cause (B)'s shape half.
**Regression guard.** Depends on item 2 — clampToBound falls back to item 2's guarded helper, never calls ElideMiddle with a hand-computed headBytes; item 3 also updates every dispatch.go comment that states the line-based elision or the fold's unconditional keep of the newest message.
TestToolResultFloorLeavesEverythingElseVerbatim's "single line the head/tail form cannot shrink" subcase is updated by this item to expect the byte-elided form with the marker (the change is the item's intent); add that test to item 3's Tests and its -run pattern.
**Files:** internal/agent/dispatch.go, internal/agent/toolresultfloor_test.go
**Read first:** internal/agent/dispatch.go — clampToBound, clampToolResult, structuralFloor; internal/agent/loop.go — clampRef;
internal/agent/toolresultfloor_test.go — TestToolResultFloorLeavesEverythingElseVerbatim, TestOversizedToolResultIsClampedByTheStructuralFloor, floorAgent;
internal/agent/filerefs_test.go — TestClampToBound_SharedByToolResultsAndFileRefs
**Tests:** in `toolresultfloor_test.go`, a single-line result 4× `floorWindow` through a scripted Turn → the committed tool message is under the structural floor and carries the marker; `TestToolResultFloorLeavesEverythingElseVerbatim`'s "single line the head/tail form cannot shrink" subcase now expects the byte-elided form with the marker, its other subcases unchanged; `TestOversizedToolResultIsClampedByTheStructuralFloor` and `TestClampToBound_SharedByToolResultsAndFileRefs` still pass unchanged.
**Acceptance:**
- `go build ./internal/agent/`
- `GOMEMLIMIT=2GiB go test -race -count=1 -run 'TestOversizedToolResult|TestClampToBound|TestToolResultCap|TestUnknownWindowClamp|TestToolResultFloor' ./internal/agent/`
**Commit:** `fix(agent): byte-elide a tool result the line elision cannot bound`

## 4. Bound a fresh tool result by the room left in the request

**What:** Recast at the regression check (2026-11-02).
**Goal:** with a known window, a tool result is committed at no more than its share of the room left — (advertised window − reply reserve, deriveGrowthBounds' room, − estimate of the next request without it) ÷ results of the Turn still to append — clamped to [1/8 of the History allocation, the History allocation]; a result that fits its share is committed whole; with no window known the bound is structuralFloor().
**Approach (assumed at the header base):** `Agent.clampToolResult` (called from `appendToolResult`, `dispatch.go`) takes the bound from a new room-left computation in place of `structuralFloor()` alone. Estimate with the measure `requestExceedsWindow` (`loop.go`) uses — the Budget's chars-per-token estimate over what the next request carries (system, standing parts, conversation) — no second estimator and no comfort margin (ADR 0018 §7). The count of results still to append comes from the dispatch's slot batch (`dispatch.go`, the `appendToolResult` call sites); it is recomputed at each append, so unused share rolls forward. `structuralFloor` stays the upper bound and the unknown-window value.
Depends on item 3.
**Regression guard.** take room and windowKnown from deriveGrowthBounds(a.budget()) — the advertised Window − ResponseReserve the predictive guard measures — not ContextLimit (header Ratified-design-calls line for the fresh-result bound: replace "working ceiling" with "advertised window", adding "(writer, 2026-11-02, from the regression check: the predictive guard's own room, so working-window never starves results)"); unknown window → structuralFloor(). Estimate = PromptChars over [standingSystem() message + conv.Messages()] plus a.toolMenu(), through a.budget(); never call buildRequest or armRequest from appendToolResult. The results-still-to-append count is computed in dispatchTools over leaves + delegations (commitNotRun and settleCancelledLeaf included), carried as Agent/turn state that reads 0 as 1; appendToolResult's signature unchanged. Tests observe whole-vs-elided at sizes either side of the share (two large results → each ≤ room/2 bites; first small → a second between room/2 and room-left commits whole); the floor and unknown-window cases are stated as regression pins without bite. Sweep rule: every Go comment stating a tool result's bound — `grep -rn "clampToolResult\|structural floor\|number a tool result gets" internal --include=*.go` — add internal/agent/loop.go and internal/agent/compact.go to Files. contextFillNotice (fillnotice.go) measures min(raw length, the bound appendToolResult will apply) so the rung reads the committed fill; add fillnotice.go and a pin test to Files/Tests. Keep TestFloorGuard_ToolResultCapTrimsAnOlderResultUnderBypass green with no added margin term in the room sum. Supersedes adr-0018-consequences "the raw text of a pathological result never reaches … the rendered transcript — the accepted price" through item 6's adr-0018-amend-1102; requestExceedsWindow and TestPredictiveGuardMeasuresTheAdvertisedWindow stand (advertised room).
Re-check: recast TestClampToBound_SharedByToolResultsAndFileRefs so both seams get one explicit bound (call clampToBound(big, a.refBound(1)), or give clampingRefAgent an empty registry) — its default-menu agent at 8192 leaves ~509 tokens of room, so the fresh-result bound and refBound diverge. Size test payloads so the bite fires: the two parallel results single-line and between room-left/2 and the History allocation (or assert first ≤ room/2, second ≤ the room left after the first); test 3's second ≤ min(room-left, History); test 1's result ≤ History. Name the fill-notice pin `TestContextFillNoticeMeasuresTheAppliedBound`.
**Files:** internal/agent/dispatch.go, internal/agent/toolresultfloor_test.go, internal/agent/loop.go, internal/agent/compact.go, internal/agent/fillnotice.go, internal/agent/fillnotice_test.go, internal/agent/filerefs_test.go
**Read first:** internal/agent/dispatch.go — appendToolResult, clampToolResult, dispatchTools; internal/agent/compact.go — deriveGrowthBounds;
internal/agent/loop.go — requestExceedsWindow, standingSystem; internal/agent/fillnotice.go — contextFillNotice;
internal/agent/filerefs_test.go — TestClampToBound_SharedByToolResultsAndFileRefs
**Tests:** with `floorWindow` and history already filling half the room, a multi-line result at most the size of the History allocation, over its share → committed under the room left, next request estimate ≤ the advertised room; two large single-line parallel results, each between room-left/2 and the History allocation → each committed at ≤ room/2 (the biting case); a small first result → a second between room/2 and min(room left, History) commits whole; `TestClampToBound_SharedByToolResultsAndFileRefs` recast to one explicit bound for both seams; regression pins without bite: room below the 1/8 floor → result clamped to the floor, window unknown → bound is `structuralFloor()` as today; in `fillnotice_test.go`, `TestContextFillNoticeMeasuresTheAppliedBound` pins that the rung percentage reads min(raw length, the applied bound); `TestContextFillNotice*`, `TestToolResultFloorLeavesEverythingElseVerbatim`, `TestKnownWindowIgnoresTheUnknownWindowCeiling` and `TestSubAgent_ChildFoldsMidDelegationAndFinishes` stay green.
**Acceptance:**
- `go build ./internal/agent/`
- `GOMEMLIMIT=2GiB go test -race -count=1 -run 'TestOversizedToolResult|TestToolResultCap|TestFreshResult|TestUnknownWindowClamp|TestFloorGuard_ToolResultCap|TestClampToBound|TestContextFillNotice|TestToolResultFloor|TestKnownWindowIgnores|TestSubAgent_ChildFolds' ./internal/agent/`
**Commit:** `fix(agent): bound a fresh tool result by the room left in the request`

## 5. Loop-level pins for the Seqcom overflow

**What:** test-only. Depends on items 1, 2 and 4. In `internal/agent/` (harness `scriptedUpstream`, `stubllm` turns, `cfg.Context.MaxContextTokens` set small):
- **Fresh result:** a stub model with a small window, a `toolCallTurn` whose result is one minified line larger than the window → the next request's estimate is under the window and the tool message carries the elision marker; the Turn completes.
- **Reactive fold:** the stub answers the first post-result request with HTTP 400 and the verbatim Seqcom body → one fold runs (summary request under its budget, carries the marker), the retried request succeeds, the Turn is not abandoned.

**Regression guard.** Fixtures build on configWithTools(...) with only the tools the scenario calls — never baseConfig/autoCompactConfig, whose ~6k-token default menu leaves fresh results bounded near History/8 at an 8192 window; size the window and result so the bound is observable. Set CompactionEnabled on — the zero Config's CompactionEnabled=false opts out of the fold. Write the reactive Script by hand, not with `recoveryScript` (llama.cpp `overflowBody` only, one repeated reply): `summaryTurn` (When-matched) first, then mains `toolCallTurn`, an `overflowTurn`-shaped HTTP 400 carrying the verbatim Seqcom body, `contentTurn`; assert with `up.summaryCalls()` / `up.mains()`.
**Files:** internal/agent/freshresult_overflow_test.go
**Read first:** internal/agent/overflowrecovery_test.go — recoveryScript; internal/agent/compact_test.go — overflowTurn; internal/agent/autocompact_test.go — autoCompactConfig, summaryTurn, summaryCalls, mains;
internal/agent/harness_test.go — scriptResponder, toolCallTurn
**Tests:** `TestFreshOversizedResultFitsTheNextRequest`, `TestSeqcomOverflowBodyFoldsAndRetries` — both on `configWithTools(...)` with only the scenario's tools and CompactionEnabled on, the second with one `summaryCalls()` and a retried main request in `mains()`.
**Acceptance:**
- `GOMEMLIMIT=2GiB go test -race -count=1 -run 'TestFreshOversizedResult|TestSeqcomOverflowBody|TestOverflowRecovery' ./internal/agent/`
**Commit:** `test(agent): pin the fresh-result overflow and the Seqcom overflow body`

## 6. Docs and knowledge follow the room-left bound

**What:**
**Goal:** every doc and Knowledge Note that describes the structural tool-result floor states the room-left, byte-elided bound, and ADR 0018 records the change by a dated amendment.
**Approach (assumed at the header base):** ADR 0018: dated amendment child `adr-0018-amend-1102` (`capcom know add --id adr-0018-amend-1102 --parent adr-0018`) superseding the consequences sentence that the floor is line-based and the fold keeps the newest message whole; name it on the Status line and in the reading-order list. Glossary Note `tool-result-capping`: the structural floor's bound. `docs/manual/configuration.md` §"Keeping the window from overflowing — compaction and pruning". Rule for the sweep: every site naming the structural floor's bound or "line-based" elision — `grep -rn "History allocation\|line-based\|structural floor" docs .capcom/knowledge` — is updated. Depends on items 2 and 4.
**Regression guard.** the ADR 0018 amendment adr-0018-amend-1102 explicitly supersedes the adr-0018-consequences line "the raw text of a pathological result never reaches … the rendered transcript — the accepted price" (now covers ordinary results cut to the room left) and the line that the floor is line-based / the fold keeps the newest message whole; requestExceedsWindow and TestPredictiveGuardMeasuresTheAdvertisedWindow are unaffected because item 4 uses the advertised room.
**Files:** .capcom/knowledge/decisions/adr-0018.md, .capcom/knowledge/decisions/adr-0018/adr-0018-amend-1102.md, .capcom/knowledge/glossary/glossary-context/tool-result-capping.md, docs/manual/configuration.md
**Read first:** .capcom/knowledge/decisions/adr-0018.md — Status line, reading-order list; .capcom/knowledge/decisions/adr-0018/adr-0018-consequences.md — line-based bullet, raw-text bullet;
.capcom/knowledge/decisions/adr-0018/adr-0018-decision.md — §8; .capcom/knowledge/glossary/glossary-context/tool-result-capping.md — structural floor sentence; docs/manual/configuration.md — "Keeping the window from overflowing — compaction and pruning"
**Tests:** none beyond the drift tests.
**Acceptance:**
- `capcom check`
- `GOMEMLIMIT=2GiB go test -race -count=1 -run 'TestManualDocumentsEverySettingsKey' ./cmd/apogee/`
- `GOMEMLIMIT=2GiB go test -race -count=1 -run 'TestManualStatesTheFloorGuardCount|TestReadmeStatesTheFloorGuardCount' ./internal/config/`
**Commit:** `docs: the structural tool-result floor bounds by the room left`
