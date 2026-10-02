# Architecture review candidates #9–#20 and the teardown globals — plan

**Goal:** Land the non-speculative candidates #9–#17 and #20 of the 2026-09-30 architecture
review plus the two deferred teardown globals, as behaviour-preserving refactors except where an
item says otherwise. The review records #1–#8 as landed.
**Date:** 2026-10-02
**Status:** unexecuted
**sized for:** ~200k-context host
**base:** f68c5c56
**Sources:**
- `docs/reviews/architecture-review-2026-09-30.html` (candidates #9–#20)
- archived plans `2026-09-30 - 00/01/02` (their "Not covered" lines), `2026-10-01 - 00`
- ADRs 0001 (public API), 0013, 0031, 0054, 0069 D6, 0071, 0076 A4/A8

**Ratified design calls (owner, 2026-10-02):**
- **#9:** reopen design call 14 of `2026-08-10 - 04 - tool-display-overhaul-plan.md`; a typed outcome on the ToolResult, prose parsing kept only as fallback.
- **#10:** the Driver builds the observe Runner and hands it in Config; the engine validates both lanes and swaps; `run.Spec.Sync` becomes a whole Generation.
- **#12:** 12a only — the guard table carries id + gate; `FloorConfig`'s seven bools stay (no API change).
- **#15:** `userexec.Result` gains raw capped `Stderr` + `StderrCapped`; keystore keeps its own sentences.
- **#16:** delete the audit ring; amend ADR 0013; `MergeDangerousRules` untouched.
- **#17:** `schedule.Spec.Prompt` stays the label.
- **#20b:** spinner only; the transcript's three fields stay the fold seed (owner, 2026-10-02).
- **#20c:** dropped — nil Options families are ADR 0054's unwired degrade and decide-before-call signal, not residue (owner, 2026-10-02).
- **Teardown globals:** fields on host values (plan 01's `mcp.Host` precedent); zero = platform default.
- **#18, #19:** deferred.

**Standing requirements:**
- skills: coding-standards
- Pi 5 machine rule: never `go test -race`/`-cover` over a package pattern or a whole heavy package (`./internal/tui/`, `./cmd/apogee/`, `./internal/agent/`); narrow with `-run`. Never two test runs at once.
- Every added, moved or deleted file updates its package's `doc.go` file map (docmap tests).

**Out of scope:**
- #18 keep/resume latch (until a second Driver, ADR 0031); #19 Budget in internal/context (until a budget-ordering bug).
- #12's id-set representation of `FloorConfig`; `LoopView.ParallelAgents` removal; `schedule.Spec.Label`.
- Every bead; plan `2026-10-02 - 00`'s items.
- #20c, no-op defaults for the Skills, title and Schedules `tui.Options` families: dropped — a nil family is ADR 0054's unwired degrade and the decide-before-call signal (`configHostOrNoop` guards at call time, never at construction).

**Regression check (2026-10-02, f68c5c56):** six read-only reviewers; ids below are the final numbering.
- 1: guard folded (badge full slugs; Acceptance counts the badge text).
- 2: guard folded (detail-hook routing past `absorbProse`, Goal narrowed to `delegationResult`, root alias, cmd/apogee e2e run); supersedes design call 14 of `2026-08-10 - 04` as the header ratifies — its comments are fixed by grep.
- 3: guard folded (view stays on `delegationWidth`, `forgetFarWidth` moves, declaration grep).
- 4: guard folded (`fullyComposedApplier` folds into `fakeApplier`, 14 members; `entry.reaches` partials exempt until item 6).
- 5: guard folded (every migrated test run by name).
- 6: recast — `cannotApply` and the no-entry refusal stay; yields to ADR 0037:20.
- 7: recast — the id→field table lives in `internal/domain`; `apogee.go:202-205` and `config/reactions.go:65-68` stay true.
- 8: guard folded (per-lane rule, `ErrInvalidReaction` wrap, `ValidateAll` kept, a per-entry-only fault in the test).
- 9: guard folded (`Generation()` seeds and reports Observe; observe-moved check kept in the agent); supersedes `wire_engine.go`'s "Runner swaps bound or not" doc and `TestLateEngineReplaysThePendingGeneration`'s unbound swap.
- 10: guard folded (`Once` keeps read-edit-hand-back, one named split seam, run.Spec test readers in Files, `NewReactionRunner` sentence conditional).
- 11: guard folded (the four sibling mirrors move, `cellToRuneOffsetIn` named the painter-measured sibling); supersedes ADR 0030 §6's file locator, amended in place.
- 12: guard folded (prose grep, `TimedOut`-first rewrap, child-kill test in a `!windows` file).
- 13: guard folded (`grep -rn`).
- 14: guard folded (`var ProcessWaitDelay` grep).
- 15: guard folded (three more agent test files, `AuditDecision` → `guard.go`, prose grep).
- 16: Acceptance narrowed to named tests (Pi rule).
- 17: guard folded (also depends on item 10; every run.Spec test reader in Files; `Spec.Prompt` for a recipe run stated).
- 18: guard folded (`StartRecipe` keeps Text/SkillIDs beside `Recipe`); yields to ADR 0075 — the eventjson mirror is untouched.
- 19: guard folded (derivation stated, built in `init()`); supersedes the in-code decision at `panes.go:46-49` ("keyClaimOrder's decision, not the row's").
- 20: recast (owner) — spinner only; yields to `transcript.go:55-66` (the fold seed stays on the transcript).
- former 21, 22 (#20c): dropped (owner); former 23–25 are now 21–23.
- 21: guard folded (`clipSubAgentTask` keeps its fit test, `internal/title/title.go`, per-package runs).
- 22: guard folded (test files, `compact.go`, delegation/background carry, `localDialer` value, `ReStream` tests).
- 23: guard folded (each file's set named, prose grep, type-placement grep).
- all items: every chained race command carries its own `GOMEMLIMIT=2GiB`; every `-run` over `./internal/tui/`, `./internal/agent/` or `./cmd/apogee/` names anchored tests; Files paths absent at the base corrected.
- re-check round (items 6, 7, 20):
- 6: guard folded (prose grep widened to `cannotApply`'s doc and TestRunRootWiresTheLiveApplySeam's reactions block; removal-count Acceptance grep).
- 7: guard folded (decision: loop-built rows carry no `reaches`, KeyRegistry order, config's derived var follows the domain table; comment grep over the five sites; `config/reactions.go:65-68` no longer claimed true; literal-count Acceptance greps).
- 20: guard folded (`paintcache.go`, `mouse_test.go`, `model_test.go` in Files; blink/spinner tests run by name; Goal restated; `spin.style|spin.color` grep; the field-reading test sites rewritten instead of a new test).

## 1. The review records #1–#8 as landed — ✅ DONE (2026-10-02)

NOTES (2026-10-02): the landed badges use a solid emerald (bg-emerald-600, white text) rather than the legend's bg-emerald-100, so they don't read as a second "Strong" badge.
NOTES (2026-10-02): #20's layout-calls note says every remaining layout()/refreshViewport() call outside model.go carries a same-line "// geometry:" comment and TestArmsLeaveLayoutToTail keeps it so (retry fix: the first attempt wrongly claimed no such calls remain).

**What:**
**Goal:** in `docs/reviews/architecture-review-2026-09-30.html`, articles `c1`–`c8` each carry a
first badge `Landed · plan <plan>` (#1–#3 → `2026-09-30 - 00`; #4, #7, #8 → `2026-09-30 - 01`,
#7 noting three host types remain; #5, #6 → `2026-09-30 - 02`), each nav link `#c1`–`#c8` carries
a "✓ landed" marker, and #20's text notes its layout-calls sub-trim landed (plan `2026-10-01 - 00`).
**Approach (assumed at the header base):** badge span in each article's `flex flex-wrap gap-2
text-xs` row, styled like the existing badges (emerald background).
**Regression guard.** Each badge names its plan's full slug — `Landed · plan 2026-09-30 - 00 - workflow-launch-tally-childrun`
(#1–#3), `… 2026-09-30 - 01 - exec-host-guarded-client-budget-latch` (#4, #7, #8), `… 2026-09-30 - 02 -
engine-holder-and-dial-facts` (#5, #6) — because three archived plans share the `2026-09-30 - 00` prefix. The
Acceptance counts the badge text: the nav link and the `#landed` heading already hold "Landed" twice.
**Files:** docs/reviews/architecture-review-2026-09-30.html
**Read first:** docs/reviews/architecture-review-2026-09-30.html — nav links #c1–#c8, article c1–c8 `flex flex-wrap gap-2 text-xs` badge rows, legend span (emerald = Strong), article c20 "Doc/arm drift" li, #landed ledger;
docs/plans/archived/2026-09-30 - 01 - exec-host-guarded-client-budget-latch-plan.md — Out of scope line (three host types remain)
**Tests:** none (docs).
**Acceptance:**
- `grep -c 'Landed · plan' docs/reviews/architecture-review-2026-09-30.html` prints `8`
**Commit:** `docs(reviews): mark architecture review candidates 1-8 landed`

## 2. A delegation's outcome is typed (#9) — ✅ DONE (2026-10-02)

NOTES (2026-10-02): the `detail` hook keeps its `func(content string)` shape for every other tool; sub_agent gets a new `toolPresenter.resultDetail func(domain.ToolResult) toolOutcome` hook that absorbProse runs whether or not the result carries a summary (the guard's "routed through the detail hook"), instead of changing the signature shared by ~34 entries. `failure` does take the ToolResult (subprocessFailure updated to match).
NOTES (2026-10-02): the prose readers stay as the fallback (`delegationVerdict(content)`, `delegationDetail(content)`, now built on `proseDelegationOutcome`); `delegationBoundVerdict` takes a `domain.DelegationBound`, read back from a matched head through the new `delegationBoundNamed`. With a summary, the steered notice is taken off the body as the result's final line (`delegationSteering`), not by regex.
NOTES (2026-10-02): new tests — TestSubAgent_NarratedClosingTextHasNoReport (agent); TestDelegationOutcomeOutranksTheProse, TestDelegationResultDetailReadsTheOutcome, TestDelegationFailureReadsTheOutcomesSteering, TestSummaryBearingDelegationRendersAsItsProse (tui). Summary assertions also added to the existing DelegatesAndReportsBack, FaultedDelegationReportsAsError, StepCapReturnsAPartialResultToTheParent, TokenBudgetEndsTheChildThroughTheWrapUp, TimeLimitEndsTheChildThroughTheWrapUp, ACancelledDelegateIsStoppedAndRetained, SteeredChildResultCarriesTheParentNotice and AcknowledgementIsNoReport tests.
NOTES (2026-10-02): consequential edit — internal/tui/toolleader.go: made necessary by the live slot now being worded by delegationOutcomeVerdict (three doc comments named delegationVerdict / delegationEndedWithoutReport / delegationStoppedByUser as the wording source).

**What:**
**Goal:** every result `delegationResult` renders (`subagent.go`; success and `IsError`) carries a typed
delegation summary — bound hit (step/token/time cap or none), stopped-by-user, no-report, steered
count — and `internal/tui/toolregistry.go`'s delegation hooks render from it; the prose regexes
run only for a delegation result with no summary. Rendered output is unchanged for every existing fixture.
**Approach (assumed at the header base):** a new variant in the sealed sum
`internal/domain/toolsummary.go` (update the variant count in `domain/doc.go`), set at
`subagent.go` `delegationResult` and the stopped-result site; the `detail`/`failure` hooks take
the `ToolResult` (as `stat` does); `delegationBoundHead`, `delegationSteeredTail`,
`delegationBodyNote`, `delegationStoppedByUser`, `delegationEndedWithoutReport` become fallbacks.
**Regression guard.** `absorbProse` (toolview.go) returns early for any result carrying a `Summary` and calls only
`p.body`; `sub_agent` has no body hook, so a summary-bearing delegation is routed through the detail hook (or a body
hook reproducing `delegationDetail`'s Summary AND Details), else every live delegation loses its promoted report line —
which only the cmd/apogee e2e pins catch. `runSubAgent`'s refusal returns and the queued stop (dispatch.go
`stopQueuedDelegation`, read by `absorbFailure` through `delegationStoppedByUser`) stay prose-only. The new variant gets
its root alias (`apogee.go`, `example_test.go`) and joins `TestToolSummaryVariantsAreSealed` (want 8). Design call 14 of
`2026-08-10 - 04` is superseded (header): fix every comment `grep -n 'design call 14\|growing the engine for presentation' internal/tui/` finds on the delegation readers.
**Files:** internal/domain/toolsummary.go; internal/domain/toolsummary_test.go; internal/domain/doc.go; apogee.go; example_test.go; internal/agent/subagent.go; internal/agent/subagent_test.go; internal/tui/toolview.go; internal/tui/toolregistry.go; internal/tui/toolregistry_test.go; internal/tui/doc.go
**Read first:** internal/tui/toolview.go — enrichWithResult, absorbProse, absorbFailure; internal/tui/toolregistry.go — "sub_agent" entry, delegationDetail, delegationVerdict, delegationStat, delegationFailure, readDelegationSteering; internal/agent/subagent.go — delegationResult, runSubAgent, cappedResult;
internal/domain/toolsummary.go — ToolSummary, EditRegions; internal/domain/toolsummary_test.go — TestToolSummaryVariantsAreSealed; apogee.go — ToolSummary alias block; internal/tui/toolregistry_test.go — TestDelegationVerdictReadsTheHumansStop, TestDelegationDetailNeverPromotesANonReport;
cmd/apogee/e2e_delegation_test.go — TestE2EDelegationStepCap
**Tests:** subagent tests assert the summary per outcome; toolregistry tests feed summaries with
prose that would parse differently (proves the summary wins) plus one summary-less fallback case.
**Acceptance:**
- `go vet . ./internal/domain/ ./internal/agent/ ./internal/tui/`
- `GOMEMLIMIT=2GiB go test -race -count=1 ./internal/domain/`
- `GOMEMLIMIT=2GiB go test -race -count=1 -run '^TestSubAgent_DelegatesAndReportsBack$|^TestSubAgent_FaultedDelegationReportsAsError$|^TestSubAgent_ACancelledChildIsStoppedAndTheParentTurnSettles$|^TestSubAgent_StepCapReturnsAPartialResultToTheParent$|^TestSubAgent_StepCapMarksAWordlessDelegate$|^TestSubAgent_CappedChildReplyReportsAsErrorNamingTheCause$|^TestSubAgent_SteeredChildResultCarriesTheParentNotice$|^TestSubAgent_UnsteeredChildResultIsUnchanged$|^TestSubAgent_AcknowledgementIsNoReport$|^TestSubAgent_ACancelledDelegateIsStoppedAndRetained$|^TestSubAgent_ResultIsCappedAtSixtyFourKiB$' ./internal/agent/` plus the tests this item adds, by name
- `GOMEMLIMIT=2GiB go test -race -count=1 -run '^TestDelegationBoundHeadReadsEveryBoundsHead$|^TestDelegationBoundHeadReadsTheNonReportVariants$|^TestDelegationVerdictReadsTheHumansStop$|^TestDelegationDetailDoesNotPromoteAStoppedText$|^TestDelegationRecognisersReadThroughTheRoutingNote$|^TestDelegationValidationFaultsReadThroughTheErrorSlot$|^TestDelegationVerdictReadsANonReport$|^TestDelegationDetailNeverPromotesANonReport$|^TestDelegationDoneReadsInTheSuccessTone$|^TestFailedDelegationPaintsItsSlotRed$|^TestSummaryStyleGreensOnlyTheDelegationVerdict$|^TestToolRegistryCoversEveryBuiltInTool$|^TestTranscriptCodecReplaysAFinishedDelegationGreen$|^TestTranscriptCodecReplaysANoReportDelegationWithoutACheck$' ./internal/tui/` plus the tests this item adds, by name
- `GOMEMLIMIT=2GiB go test -race -count=1 -run '^TestE2EDelegationStepCap$|^TestE2EDelegationChildCarriesTheReportBlock$|^TestE2ESubAgentStop$|^TestE2ESubAgentView$|^TestE2EOutcomeSlotsCarryTheToolsVerdict$|^TestE2EOutcomeCancelledDelegationCarriesTheFailureTone$' ./cmd/apogee/`
**Commit:** `refactor(agent): attach a typed delegation outcome to the sub-agent tool result`

## 3. Delegation width is one module (#11) — ✅ DONE (2026-10-02)

NOTES (2026-10-02): fanOutCeilingOf (the ceiling formula the refusal text also reads) moved into delegationwidth.go beside fanOutCeiling, so the module alone answers the ceiling; the Approach's move list did not name it.
NOTES (2026-10-02): added seatCap(seat) in delegationwidth.go — the cap of the server a child on one seat runs on — which workflowWidthOn and backgroundWidth now both ask instead of each restating the session-seat rule; every value is unchanged (workflowWidthOn = 1 on a delegate else max(seatCap,1); backgroundWidth = max(seatCap-1,1)).
NOTES (2026-10-02): the far-width stickiness (ADR 0069 decision 6) is named once, in delegationwidth.go's file comment; statedDelegationWidth's doors paragraph moved there and the farWidth field comment in agent.go now points at it.
NOTES (2026-10-02): consequential edit — internal/agent/delegationseat.go: made necessary by statedDelegationWidth moving out of agent.go (file pointer in SetDelegationSeat's doc).

**What:**
**Goal:** `internal/agent/delegationwidth.go` alone answers the width stated to the model, the
per-batch width (seat-aware) and the ceiling, and names ADR 0069 D6's far-width stickiness once;
the workflow and background paths and `LoopView.ParallelAgents` (kept, fed from `delegationWidth`,
the default-seat answer) ask it. Every width value is unchanged.
**Approach (assumed at the header base):** move `fanOutWidth`, `fanOutWidthFor`, `fanOutCeiling`,
`delegationWidth`, `delegationCap` (dispatch.go), `parallelAgentsCap`, `statedDelegationWidth`,
`stateFarWidth`, `forgetFarWidth` (agent.go) into the new file; `workflowWidthOn` (workflowcall.go) and
`backgroundWidth` (background.go) call it. Read only those ranges of the large files.
**Regression guard.** `LoopView.ParallelAgents` stays on `delegationWidth` — the default-seat answer that takes no calls
(the batch hint dispatch.go documents, ADR 0069) — and `loop.go`'s `req.SetParallelAgents(a.delegationWidth())` is unchanged:
`fanOutWidthFor` answers 1 below two calls, so a view fed from it is pinned at 1. `forgetFarWidth` moves too, so the
stickiness is named once (`delegationseat.go` untouched); each moved `func (a *Agent) …` is declared only in `delegationwidth.go`.
**Files:** internal/agent/delegationwidth.go; internal/agent/dispatch.go; internal/agent/agent.go; internal/agent/workflowcall.go; internal/agent/background.go; internal/agent/delegationtarget.go; internal/agent/doc.go; internal/agent/fanout_test.go
**Read first:** internal/agent/dispatch.go — fanOutWidth, fanOutWidthFor, fanOutCeiling, fanOutCeilingOf, delegationWidth, delegationCap; internal/agent/agent.go — parallelAgentsCap, statedDelegationWidth, stateFarWidth, forgetFarWidth, farWidth field; internal/agent/loop.go — buildRequest (req.SetParallelAgents);
internal/agent/workflowcall.go — workflowWidthOn; internal/agent/background.go — backgroundWidth; internal/agent/delegationtarget.go — SetDelegationTarget;
internal/agent/fanout_test.go — TestDelegationCapPicksTheGoverningServer, TestStatedDelegationWidth_LatchesPerSeat, TestRoutedWidthReachesTheHookView; internal/agent/launch_test.go — workflowWidthOn check
**Tests:** existing `fanout_test.go`, `orientation_test.go`, `launch_test.go` stay green; one
table test in `fanout_test.go` driving the module's three answers.
**Acceptance:**
- `go vet ./internal/agent/`
- `grep -n "^func (a \*Agent) \(fanOutWidth\|fanOutWidthFor\|fanOutCeiling\|delegationWidth\|delegationCap\|parallelAgentsCap\|statedDelegationWidth\|stateFarWidth\|forgetFarWidth\)(" internal/agent/*.go` lists only `internal/agent/delegationwidth.go`
- `GOMEMLIMIT=2GiB go test -race -count=1 -run '^TestDelegationCapPicksTheGoverningServer$|^TestDispatchSerially_CeilingAppliesAtDepthOne$|^TestRoutedWidthReachesTheHookView$|^TestStatedDelegationWidth_LatchesPerSeat$|^TestFanOutWidth_BoundsTheGroup$|^TestFanOutWidth_MixedSeatsTakeTheSmallerCap$|^TestFanOutWidth_UnparseableSeatIsNotASplit$|^TestFanOut_CeilingReadsTheLatchedFarWidth$|^TestFanOut_CeilingRefusesTheCallsPastIt$|^TestFanOut_RoutedWidthComesFromTheTargetCap$|^TestFanOut_LatchClearedMidGroupKeepsTheGroupWidth$|^TestLoopViewParallelAgents_StampsTheDelegationWidth$|^TestAgentSetParallelAgentsMovesTheFanOutWidth$|^TestBackground_RunsAtTheServerWidthMinusOne$|^TestOrientation_DelegationBoundsStateWidthCeilingAndCap$|^TestLaunch_BackgroundAndResumeSharePlanAndRecipeWiring$|^TestDocMapNamesEveryFile$' ./internal/agent/` plus the table test this item adds
**Commit:** `refactor(agent): answer delegation width from one module`

## 4. Settings tests build one complete fake applier — wire_settings_test.go (#13a) — ✅ DONE (2026-10-02)

NOTES (2026-10-02): the two "through an applier holding nothing" checks in TestApplySettingAcceptsTheEditorKey and TestApplySettingAcceptsTheStartupOnlyKeys moved into TestApplySettingRefusesEveryKeyItCannotReach. Its exempt-key branch now applies each settingKeysWithNoMemberToReach key at its Default through the zero applier and expects success, where it used to `continue`. This keeps every `settingsApplier{}` inside the refusal test, as the Acceptance grep requires, and keeps the coverage. The two Accepts tests now migrate onto fakeApplier, and the comments that pointed between the tests were updated to match.
NOTES (2026-10-02): migrated call sites drop override lines that only repeat fakeApplier's own default (a fresh `&applySettingSpy{}` engine, an empty-snapshot live holder, the "bound-model" binding). A binding override stays only where a test needs an unbound session.
NOTES (2026-10-02): TestSettingsApplierReloadsRefuseAnUnparseableFile now runs over the full fake applier with only configPath overridden; it used to hold just `mcp: &liveMCP{}`. Its comment was reworded to match, since every reload refuses at the file read before it reaches any member.

**What:**
**Goal:** `cmd/apogee/wire_settings_test.go` builds every `settingsApplier` through one helper
`fakeApplier(t)` returning a fully composed applier (callers override members), except
`TestApplySettingRefusesEveryKeyItCannotReach` and the partial appliers handed to `entry.reaches`, which stay until item 6. No production change.
**Approach (assumed at the header base):** add `fakeApplier` in a test helper file; migrate the
literals in `wire_settings_test.go`.
**Regression guard.** `fullyComposedApplier` folds into `fakeApplier`, which fills all 14 members — `hooks`, `caps`
(`newParallelAgentsCap` over a `parallelAgentsSpy`, upstream_test.go) and `delegation` (a `delegationWiring` over
`delegationSpy`, delegation_test.go) included — so item 6's removal of the nil guards cannot panic a `servers` apply. The
partial appliers fed to `entry.reaches` in `TestBypassRowAppliesOneGeneration`, `TestContextFillNoticeRowAppliesOneGeneration` and
`TestApplySettingReactionsRefusesWithoutTheRunnerOrTheHolder` stay until item 6, beside the refusal test.
**Files:** cmd/apogee/settingsapplier_helper_test.go; cmd/apogee/wire_settings_test.go
**Read first:** cmd/apogee/wire_settings_test.go — fullyComposedApplier, TestEveryEditableSettingKeyHasAnApply, TestApplySettingRefusesEveryKeyItCannotReach, TestApplySettingReactionsRefusesWithoutTheRunnerOrTheHolder; cmd/apogee/wire_helpers_test.go — applySettingSpy, rebindProbe, writeSettingsFixture;
cmd/apogee/wire_settings.go — settingsApplier, reloadServers, readmitMCP; cmd/apogee/upstream_test.go — parallelAgentsSpy; cmd/apogee/delegation_test.go — delegationSpy; cmd/apogee/wire_options.go — rootWiring.options
**Tests:** the migrated tests themselves.
**Acceptance:**
- `go vet ./cmd/apogee/`
- `grep -n "settingsApplier{" cmd/apogee/wire_settings_test.go` lists only lines inside `TestApplySettingRefusesEveryKeyItCannotReach`, `TestApplySettingReactionsRefusesWithoutTheRunnerOrTheHolder` and the `entry.reaches(` checks of `TestBypassRowAppliesOneGeneration` and `TestContextFillNoticeRowAppliesOneGeneration`
- `GOMEMLIMIT=2GiB go test -count=1 -run '^TestApplySettingToolsDisabledSwapsTheSet$|^TestApplySettingURLSafetyHostsSwapTheSet$|^TestApplySettingDrivesTheRightEngineSeam$|^TestApplySettingCarriesTheOtherHalfOfTheContextFilesBlock$|^TestFloorRowAppliesOneGeneration$|^TestBypassRowAppliesOneGeneration$|^TestLiveSettingsGenerationIsDerivedFromTheOverlay$|^TestContextFillNoticeRowAppliesOneGeneration$|^TestApplySettingRefusesWhatItCannotApply$|^TestApplySettingAcceptsTheEditorKey$|^TestApplySettingAcceptsTheStartupOnlyKeys$|^TestApplyStreamIdleTimeoutReadsThroughTheOneParser$|^TestApplyDelegateTimeoutReadsThroughTheOneParser$|^TestApplyMirrorMovesOnlyTheKeysFieldOfTheBlock$|^TestApplySettingRememberModelFlipsTheLiveToggle$|^TestApplySettingSubAgentsChoiceSwapsTheSeatGate$|^TestApplySettingSubAgentsChoiceSwapRefusalKeepsTheGate$|^TestLiveSettingsOptionsFollowEveryApply$|^TestRebindInputsCarriesTheToggledToolRoster$|^TestApplySettingRefusesEveryKeyItCannotReach$|^TestApplySettingContextWindowPinRidesTheRebind$|^TestApplySettingRideIsSilentBeforeAServerIsBound$|^TestApplySettingReportsARefusedRebind$|^TestApplySettingSystemPromptReResolvesFromTheFile$|^TestApplySettingWebSearchEndpointMovesTheRegisteredTool$|^TestApplySettingWebSearchEndpointSwapsWhenTheToolIsAbsent$|^TestApplySettingWebSearchSwapRefusalKeepsTheOldSet$|^TestApplySettingOnAnEmptyValueResolvesTheBuiltInDefault$|^TestApplySettingOnAnEmptyIntValueLandsTheRowDefault$|^TestApplySettingMCPReconnectSwapsTheToolsAndClosesTheOldSessions$|^TestMCPReconnectRebuildsWithTheEndpointTheSessionIsOn$|^TestApplySettingMCPReconnectKeepsTheOldSessionsWhenTheDialFails$|^TestApplySettingMCPReconnectKeepsEverythingWhenTheEngineIsBusy$|^TestApplySettingURLSafetyRowCarriesTheMCPLabelOnce$|^TestApplySettingUseProjectSkillsRescansTheSources$|^TestApplySettingSkillGatesLeaveEachOtherAlone$|^TestApplySettingPresentRebuildsTheLadder$|^TestApplySettingServersReResolvesTheParallelAgentsCap$|^TestApplySettingServersReResolvesTheBoundEntrysContextWindow$|^TestApplySettingServersRidesTheRebindForTheBoundEntrysWindow$|^TestApplySettingServersDoesNotRebindForAnEditThatMovesNoWindow$|^TestApplySettingServersRidesTheRebindForTheBoundEntrysReplyCap$|^TestApplySettingServersDoesNotRebindForACapEditThatMovesNothing$|^TestApplySettingServersRidesTheRebindForTheBoundEntrysResponseReserve$|^TestApplySettingServersDoesNotRebindForAReserveEditThatMovesNothing$|^TestApplySettingSavesTheTopLevelResponseReserveWithoutMovingTheSession$|^TestApplySettingReactionsReplacesTheRunnerAndTheProjection$|^TestApplySettingReactionsRefusesABrokenFileWithoutMovingAnything$|^TestApplySettingReactionsRefusesWithoutTheRunnerOrTheHolder$|^TestReactionsRowReloadSwapsObserveOnly$|^TestReactionsRowReloadArmsTheSyncLaneOnTheBoundAgent$|^TestSettingsApplierReloadsRefuseAnUnparseableFile$|^TestEveryEditableSettingKeyHasAnApply$' ./cmd/apogee/`
**Commit:** `test(apogee): build settings appliers through one complete fake`

## 5. Remaining settings-applier literals use the fake (#13a) — ✅ DONE (2026-10-02)

NOTES (2026-10-02): TestApplySettingModelProfilesResolvesForTheBoundModel held no rebind closure, with a comment saying the key must not need one. Under fakeApplier it now installs a rebindProbe and asserts the probe got no calls. That turns "must not need one" into a check, so a model-profiles edit cannot ride a whole rebind unnoticed.
NOTES (2026-10-02): migrated call sites drop override lines that only repeat fakeApplier's own default (a fresh `&applySettingSpy{}` engine in schedule_test.go and settingsedit_test.go, an empty-snapshot live holder in TestApplySettingModelProfilesRecomposesTheToolSet), following item 4.

**What:** Depends on item 4.
**Goal:** no `settingsApplier{` literal remains in `cmd/apogee/*_test.go` outside
`fakeApplier` and the refusal test.
**Approach (assumed at the header base):** migrate `configwatch_apply_test.go`,
`delegation_test.go`, `modelprofile_test.go`, `schedule_test.go`, `serverstats_test.go`,
`settingsedit_test.go`, `wire_live_test.go`.
**Regression guard.** The Acceptance runs every migrated test by name: five of the eleven
(`TestApplySettingURLSafetyHosts*`, `TestApplySettingServersDrivesTheSubAgentServer`, `TestApplySettingServersInstallsTheReReadList`)
match none of the draft's `-run` words.
**Files:** cmd/apogee/configwatch_apply_test.go; cmd/apogee/delegation_test.go; cmd/apogee/modelprofile_test.go; cmd/apogee/schedule_test.go; cmd/apogee/serverstats_test.go; cmd/apogee/settingsedit_test.go; cmd/apogee/wire_live_test.go
**Read first:** cmd/apogee/delegation_test.go — TestApplySettingServersDrivesTheSubAgentServer, delegationSpy; cmd/apogee/wire_live_test.go — TestApplySettingURLSafetyHostsDropsAnMCPServerTheNewListDenies; cmd/apogee/settingsedit_test.go — TestApplySettingServersInstallsTheReReadList; cmd/apogee/schedule_test.go — TestScheduleFiringFollowsLiveSettingsEdits;
cmd/apogee/serverstats_test.go — TestServerStatsSettingsToggleOpensAndStopsTheStore; cmd/apogee/configwatch_apply_test.go — TestWatchedConfigRepointedMCPServerReconnects; cmd/apogee/modelprofile_test.go — TestApplySettingModelProfilesResolvesForTheBoundModel
**Tests:** the migrated tests.
**Acceptance:**
- `grep -ln "settingsApplier{" cmd/apogee/*_test.go` lists only the helper and `wire_settings_test.go`
- `GOMEMLIMIT=2GiB go test -count=1 -run '^TestWatchedConfigRepointedMCPServerReconnects$|^TestApplySettingServersDrivesTheSubAgentServer$|^TestApplySettingModelProfilesResolvesForTheBoundModel$|^TestApplySettingModelProfilesRecomposesTheToolSet$|^TestApplySettingModelProfilesWithNothingBoundHoldsOnly$|^TestScheduleFiringFollowsLiveSettingsEdits$|^TestServerStatsSettingsToggleOpensAndStopsTheStore$|^TestApplySettingServersInstallsTheReReadList$|^TestApplySettingURLSafetyHostsDropsAnMCPServerTheNewListDenies$|^TestApplySettingURLSafetyHostsLeavesMCPAloneWhenNoVerdictMoved$|^TestApplySettingURLSafetyHostsReportsAFailedReconnectInTheNote$' ./cmd/apogee/`
**Commit:** `test(apogee): move the remaining settings tests onto the complete fake applier`

## 6. The settings applier is total (#13b) — ✅ DONE (2026-10-02)

NOTES (2026-10-02): the prose grep also found three comments over nil guards that the regression guard keeps: readmitMCP (nil MCP holder or empty config path), applyMirror (nil holder) and recordToolSet (nil holder). The guards stay, since the item removes only reaches, the reaches* predicates, unreachable and rides. The comments now describe the nil check itself rather than "a Driver that composed no …". The caps and delegation member docs were reworded the same way, to "reloadServers skips a nil one".
NOTES (2026-10-02): after this item, several defensive nil checks remain in code the item did not remove: `a.live` in the auto-compact and prune-tool-results applies, mirrorSetting, recordToolSet and recordSeatChoice; `a.mcp`/`configPath` in readmitMCP; and `a.caps`/`a.delegation` in reloadServers. They are unreachable through the one production constructor, and pruning them is left for later.
NOTES (2026-10-02): the prose grep also matched `unreachable from the pane` in the doc of TestApplyDelegateTimeoutReadsThroughTheOneParser, which is about the pane and not the applier. It now reads "the pane never sends one", so the grep comes back clean. The reactions block of TestRunRootWiresTheLiveApplySeam sits at about line 2713 at this base, not the plan's 2655-2665. It keeps its check for the absence of "cannot be applied", reworded to name the reactions row and the engine's generation door.
NOTES (2026-10-02): settingKeysWithNoMemberToReach's doc moved into TestApplySettingAcceptsTheStartupOnlyKeys, without its nil-member exemption wording. The doc names `editor` and the two `sessions.` bounds the old list also carried. TestApplySettingAcceptsTheEditorKey's doc and the (c) branch of TestEveryEditableSettingKeyHasAnApply's doc no longer point at the deleted refusal test.

**What:** Recast at the regression check (2026-10-02). Depends on item 5.
**Goal:** `settingsApplier` has no reachability column: the `reaches` field, its predicates
(`reachesTheEngine`… `reachesWithoutAMember`), `unreachable`, the nil-member
degrades inside the `servers:` apply (`rides`) and the write-only `hooks` member are gone (`cannotApply` and the
no-entry refusal stay); the refusal test is deleted. A comment on
the type says the seam returns when a second Driver builds an applier.
**Approach (assumed at the header base):** `wire_settings.go`, plus `wire_options.go` (the single
production constructor, which always sets every member) losing `hooks`.
**Regression guard.** Keep `cannotApply` and the no-entry/unknown-key refusal (ADR 0037:20 — the item yields to it); remove
only the `reaches` field, the reaches* predicates, `unreachable` and `rides`; delete the `reaches` assertions in
TestBypassRowAppliesOneGeneration and TestContextFillNoticeRowAppliesOneGeneration and delete
TestApplySettingReactionsRefusesWithoutTheRunnerOrTheHolder together with TestApplySettingRefusesEveryKeyItCannotReach; delete
`settingKeysWithNoMemberToReach`, moving its doc into TestApplySettingAcceptsTheStartupOnlyKeys; drop the now write-only
`hooks` member from settingsApplier, cmd/apogee/wire_options.go and the fake; prose rule: fix every comment
`grep -n "nil ⇒\|composed no\|composed without\|unreachable\|RefusesEveryKeyItCannotReach" cmd/apogee/wire_settings*.go` finds.
Re-check: the prose grep also takes `composed the dispatcher\|forgot to pass\|refuses the key by name\|its Runner to the applier`;
`cannotApply`'s doc gives one reason (no entry); the reactions block of TestRunRootWiresTheLiveApplySeam
(wire_settings_test.go:2655-2665, "the composition root did not pass its Runner") is dropped or reworded.
**Files:** cmd/apogee/wire_settings.go; cmd/apogee/wire_options.go; cmd/apogee/wire_settings_test.go; cmd/apogee/settingsapplier_helper_test.go
**Read first:** cmd/apogee/wire_settings.go — settingsApplier, settingsTable, settingsApplier.unreachable, settingsApplier.rides, cannotApply; cmd/apogee/wire_options.go — rootWiring.options;
cmd/apogee/wire_settings_test.go — TestRunRootWiresTheLiveApplySeam; cmd/apogee/wire_boot.go — reactions.New runner (w.hooks)
**Tests:** existing settings tests stay green.
**Acceptance:**
- `grep -c "reachesThe\|unreachable(" cmd/apogee/wire_settings.go` prints `0`
- `grep -c "reaches:\|reaches func\|reachesWithoutAMember\|) rides()\|hooks \*reactions" cmd/apogee/wire_settings.go` prints `0`
- `GOMEMLIMIT=2GiB go test -race -count=1 -run '^TestLandSettingIsTheRowsOwnReading$|^TestApplySettingRefusesWhatItCannotApply$|^TestApplySettingAcceptsTheStartupOnlyKeys$|^TestEveryEditableSettingKeyHasAnApply$|^TestSettingsTableIsInRegistryOrder$|^TestBypassRowAppliesOneGeneration$|^TestContextFillNoticeRowAppliesOneGeneration$|^TestFloorRowAppliesOneGeneration$|^TestApplySettingDrivesTheRightEngineSeam$|^TestApplySettingRideIsSilentBeforeAServerIsBound$|^TestApplySettingServersRidesTheRebindForTheBoundEntrysWindow$|^TestApplySettingServersReResolvesTheParallelAgentsCap$|^TestApplySettingServersDrivesTheSubAgentServer$|^TestApplySettingReactionsReplacesTheRunnerAndTheProjection$|^TestRunRootWiresTheLiveApplySeam$' ./cmd/apogee/`
**Commit:** `refactor(apogee): treat the settings applier as always composed`

## 7. Floor guard rows loop over the engine's guard table (#12a) — ✅ DONE (2026-10-02)

NOTES (2026-10-02): the domain table is the unexported `floorGuards` slice behind `domain.FloorGuards()`, which returns a fresh copy; each row pairs `ID` with a `Gate func(*FloorConfig) *bool` accessor. The engine's join is `floorGates` in builtins.go, and it panics at init if the two id sets differ. internal/agent/floorguards.go needed no edit because its `guardIDs` comment is still true.
NOTES (2026-10-02): `floorFromOptions` now reads each key's positive value through its registry row (`Read(o) == "false"` sets the gate). `floorGuardRows` also requires a KindBool row with a Read. settingsTable is now `slices.Concat(head, floorGuardSettings(), tail)` so the rows stay in place before `context-fill-notice`.
NOTES (2026-10-02): consequential edit — internal/config/reactions_test.go: made necessary by deriving floorGuardKeys from domain.FloorGuards (the TestFloorGuardKeysAreRegistryKeys doc comment called the list a literal); the matching "second literal" phrase on contextFillNoticeKey in reactions.go was reworded too.

**What:** Recast at the regression check (2026-10-02). Depends on item 6.
**Goal:** a table in `internal/domain` beside `FloorConfig` pairs each guard id with its `FloorConfig`
field accessor, and the engine's floor guard table joins its gates on those ids; `floorFromOptions`, `floorGuardRows` and the seven settings rows are produced by
looping over it, and `config.FloorGuardKeys()` reads the same ids. `FloorConfig` is unchanged.
**Approach (assumed at the header base):** `internal/domain/config.go` holds the table;
`internal/agent/builtins.go`/`floorguards.go` join on it; `cmd/apogee/wire_settings.go` (`floorFromOptions`, `setFloorGuard`, `floorGuardRows`)
and `internal/config/reactions.go` (`floorGuardKeys`) consume it.
**Regression guard.** The id→FloorConfig-field table lives in internal/domain beside FloorConfig (internal/domain/config.go), in
KeyRegistry order; internal/agent's floor guard table keeps gate/handler and joins on the domain ids; config's
`floorGuardKeys` stays as a var derived from the domain table; cmd/apogee reads the domain table (no internal/agent import, no
facade change — apogee.go:202-205 stays true); the seven settings rows are emitted in
KeyRegistry order at today's slot before `context-fill-notice`; retarget TestFloorGuardTableMatchesTheConfigKeys (and its
"cannot import one another" comment) to registry-row coverage instead of adding a new test.
Re-check: the loop-built floor rows carry no `reaches` (item 6 removed the column first) and are emitted in KeyRegistry order (internal/config registry), which differs from config's literal floorGuardKeys order — the domain table is in registry order and config's derived var follows it.
`config.FloorGuardKeys` loses its only production reader (wire_settings.go:691), so config/reactions.go:65-68 is NOT claimed true; prose rule: fix every comment
`grep -rn "FloorGuardKeys\|floorGuardKeys\|floorGuardFields\|TestFloorGuardTableMatchesTheConfigKeys\|import one another\|import the other" internal/agent internal/config internal/domain cmd/apogee apogee.go` finds
(builtins.go:49-52, reactions.go:49-54 and 65-67, wire_settings.go:667-670 and 682-686 among them).
**Files:** internal/domain/config.go; internal/agent/floorguards.go; internal/agent/builtins.go; internal/config/reactions.go; internal/config/reactions_test.go (if touched); cmd/apogee/wire_settings.go; cmd/apogee/wire_settings_test.go; apogee.go (comment only, if the prose grep finds it false)
**Read first:** internal/agent/builtins.go — floorGuards; internal/domain/config.go — FloorConfig; internal/config/reactions.go — floorGuardKeys, FloorGuardKeys; cmd/apogee/wire_settings.go — floorFromOptions, settingsTable;
cmd/apogee/wire_settings_test.go — TestFloorGuardTableMatchesTheConfigKeys; internal/config/registry.go — KeyRegistry floor rows (tool-use-enforcer…tool-call-salvage)
**Tests:** `TestFloorGuardTableMatchesTheConfigKeys` retargeted to registry-row coverage (its "cannot import
one another" comment with it); no new test.
**Acceptance:**
- `grep -c "DisableTool\|DisableEmpty\|DisableRead" cmd/apogee/wire_settings.go` prints `0` (today 7)
- `grep -c '"tool-use-enforcer"\|"read-cache"' cmd/apogee/wire_settings.go internal/config/reactions.go` prints `0` for each (today 2 and 2)
- `grep -n "FloorConfig" internal/domain/config.go` shows the id table beside `FloorConfig`
- `go vet ./internal/domain/ ./internal/agent/ ./internal/config/ ./cmd/apogee/`
- `GOMEMLIMIT=2GiB go test -race -count=1 ./internal/domain/`
- `GOMEMLIMIT=2GiB go test -race -count=1 -run '^TestBuiltinReactionsAreTheSevenFloorGuardsWhenEveryGuardIsOn$|^TestFloorGuardKeysIsAFreshCopyOfTheTable$|^TestFloorGuardTableGatesEachGuardByItsOwnField$|^TestSetReactionsFloorOnlySwapLeavesTheSyncLaneArmed$|^TestSetReactionsRebuildsTheLadderOnlyWhenTheFloorMoves$|^TestSetReactionsSwapsFloorAndBypassAtomically$' ./internal/agent/ && GOMEMLIMIT=2GiB go test -race -count=1 ./internal/config/`
- `GOMEMLIMIT=2GiB go test -count=1 -run '^TestFloorGuardTableMatchesTheConfigKeys$|^TestFloorRowAppliesOneGeneration$|^TestSettingsTableIsInRegistryOrder$|^TestEveryEditableSettingKeyHasAnApply$|^TestLiveSettingsGenerationIsDerivedFromTheOverlay$|^TestBootConfigCarriesTheFloorGuardKeys$|^TestFiringConfigCarriesTheFloorGuardKeys$|^TestLateEngineReplaysTheFloorGatesAtTheBind$' ./cmd/apogee/`
**Commit:** `refactor(agent): drive floor guard config and settings rows from one table`

## 8. One arm validates both reaction lanes (#10a) — ✅ DONE (2026-10-02)

NOTES (2026-10-02): consequential edit — apogee.go: made necessary by SetReactions now validating Observe (the Generation alias doc said the agent "ignores Observe")
NOTES (2026-10-02): the one validation is a new agent-private `validateGeneration` beside SetReactions in agent.go (Generation.Validate, then reactions.ValidateAll over Observe, then refuseReservedIDs over Sync); refuseReservedIDs stays in reactions.go unchanged; an observe-lane error not already wrapping domain.ErrInvalidReaction is wrapped as `%w: %w`, so the sentence reads `apogee: invalid reaction: reaction "x": ...`
NOTES (2026-10-02): internal/domain/reaction.go and internal/reactions/hooks.go change in doc comments only; added test TestSetReactionsRefusesAMalformedObserveLane (zero timeout, seam Moment, blank argv, plus an accepted lane sharing an id with Sync)

**What:** Depends on item 7.
**Goal:** `Agent.SetReactions` validates the whole `domain.Generation` — sync and observe lanes —
through one function and refuses a bad observe lane as it refuses a bad sync lane; the Runner
swap is unchanged in this item.
**Approach (assumed at the header base):** merge `Generation.Validate`, `reactions.ValidateAll`
and `refuseReservedIDs` into one validation the arm calls (`internal/agent/agent.go`
`SetReactions`/`installGeneration`).
**Regression guard.** The one validation is per lane: `reactions.ValidateAll`'s per-entry checks over Observe only (a gate's
seam Moment fails `ParseEvent`), dedup within each lane (one id in both lanes stays legal), `refuseReservedIDs` on Sync. It
lives in internal/agent (domain cannot import reactions; `reservedIDs` is agent-private) and calls `reactions.ValidateAll`,
which stays for `config/reactions.go` `validateReactionBlocks` and `reactions/hooks_test.go`. The observe-lane refusal wraps
`domain.ErrInvalidReaction` (agent.go `SetReactions` doc, public `apogee.ErrInvalidReaction`) and keeps the entry-naming sentence.
**Files:** internal/agent/agent.go; internal/agent/setreactions_test.go; internal/domain/reaction.go; internal/reactions/hooks.go
**Read first:** internal/agent/agent.go — SetReactions, installGeneration; internal/domain/reaction.go — Generation.Validate, SplitLanes; internal/reactions/hooks.go — Validate, ValidateAll; internal/agent/reactions.go — refuseReservedIDs, reservedIDs, armReactions;
internal/config/reactions.go — validateReactionBlocks; internal/agent/setreactions_test.go — TestSetReactionsRefusesAMalformedGeneration
**Tests:** `setreactions_test.go`: a Generation with an observe Reaction only the per-entry rules catch (`Timeout`
0, an `on:` seam Moment, or blank argv — `Generation.Validate` already refuses a wrong class or a duplicate) is refused
with `domain.ErrInvalidReaction`.
**Acceptance:**
- `go vet ./internal/agent/ ./internal/domain/ ./internal/reactions/`
- `GOMEMLIMIT=2GiB go test -race -count=1 -run '^TestSetReactionsArgvGateNeverReachesTheSeamCascade$|^TestSetReactionsArmsAnAdviseEntryForTheNextToolResult$|^TestSetReactionsArmsAScopedGateOnlyInItsWorkspace$|^TestSetReactionsFloorOnlySwapLeavesTheSyncLaneArmed$|^TestSetReactionsReachesAChildSpawnedAfterTheSwap$|^TestSetReactionsRebuildsTheLadderOnlyWhenTheFloorMoves$|^TestSetReactionsRebuildsTheLadderWhenOnlyTheNoticeSwitchMoves$|^TestSetReactionsRefusesAMalformedGeneration$|^TestSetReactionsRefusesAReservedBuiltinID$|^TestSetReactionsRemovingAGateStopsTheDenial$|^TestSetReactionsSwapsFloorAndBypassAtomically$|^TestUndoRevertRefusesAStaleGeneration$' ./internal/agent/` plus the test this item adds
**Commit:** `refactor(agent): validate both reaction lanes in the generation arm`

## 9. The engine holds the observe Runner (#10b) — ✅ DONE (2026-10-02)

NOTES (2026-10-02): re-derived from "domain.Config carries the Runner" with no domain file in **Files:** — the new `Config.ObserveRunner` and `Config.Observe` fields live in internal/domain/config.go and the one-method `ObserveRunner` interface in internal/domain/reaction.go (domain cannot import internal/reactions). The Runner can't report the list it was built from, so a second seed field carries it. apogee.go re-exports the interface as `apogee.ObserveRunner`
NOTES (2026-10-02): consequential edit — apogee.go: made necessary by the Agent now swapping the Runner (Generation alias and ReactionRunner docs named the Driver as the one handing both halves)
NOTES (2026-10-02): consequential edit — cmd/apogee/wire_settings.go: made necessary by the observe-moved check leaving lateEngine (applyFloorGuard's doc pointed at lateEngine.SetReactions)
NOTES (2026-10-02): the Runner half is the new agent-private `swapObserve`, which SetReactions calls after installGeneration. A new `swapMu` runs each SetReactions call start to finish, one at a time, so two concurrent calls cannot split the engine and the Runner across two generations. With a nil Runner the lane is recorded as handed. The seed is validated at construction through validateGeneration
NOTES (2026-10-02): a Runner refusal while bound no longer parks the generation in lateEngine.pendingGeneration (harmless, since nothing replays it after the bind). An unbound edit whose observe list the Runner refuses (an unresolvable `workspace:`) is now accepted at the row and refused at the bind's replay, which releases the Agent and fails the bind, the way a reserved sync id already did. The regression guard ratifies this ("an unbound edit reaches the Runner only at Bind's replay")
NOTES (2026-10-02): child agents copy the parent's cfg, so they carry `ObserveRunner`/`Observe` too. Nothing calls SetReactions on a child, so subagent.go was left as it is
NOTES (2026-10-02): the unbound settings tests (TestApplySettingReactionsReplacesTheRunnerAndTheProjection, ...RefusesABrokenFileWithoutMovingAnything) now bind over the Runner through the shared bindOverRunner helper, because an unbound edit no longer reaches it. The swap test the item asks for is TestSetReactionsSwapsTheObserveRunnerOntoTheNewLane (internal/agent, a real reactions.Runner)

**What:** Depends on item 8.
**Goal:** `domain.Config`/agent Config carries the Driver-built observe Runner; `SetReactions`
swaps both halves together (`Replace` on the Runner); `lateEngine.SetReactions`'s
`reflect.DeepEqual` pairing and the `reactionSwapper` are gone.
**Approach (assumed at the header base):** `cmd/apogee/wire_engine.go` `seedReactions`/`lateEngine`;
`wire_boot.go` hands the Runner it builds.
**Regression guard.** Agent.Generation() seeds and reports the Observe lane, so every read-edit-hand-back caller keeps the
Runner's lane (`run.Once`, `probecontext.go` `measureTurn1`); agent.go's "gen.Observe is IGNORED" and "Observe is always
empty" docs change with it. The agent keeps lateEngine's "observe moved" check against the list it last applied, recorded
only after `Replace` commits; a nil Runner skips the swap (`TestSetReactionsWithoutARunner`). An unbound edit reaches the
Runner only at Bind's replay — this supersedes wire_engine.go's "Runner swaps bound or not" doc and
`TestLateEngineReplaysThePendingGeneration`'s "runs without an Agent".
**Files:** internal/agent/agent.go; internal/agent/construct.go; internal/agent/setreactions_test.go; cmd/apogee/wire_engine.go; cmd/apogee/wire_engine_test.go; cmd/apogee/wire_boot.go; cmd/apogee/wire_live.go; cmd/apogee/wire_settings_test.go
**Read first:** internal/agent/agent.go — SetReactions, installGeneration, Generation; internal/agent/construct.go — New (gen seed); cmd/apogee/wire_engine.go — lateEngine.SetReactions, seedReactions, Bind, reactionSwapper; cmd/apogee/wire_boot.go — rootWiring.resolveConfig; cmd/apogee/wire_live.go — rootWiring.wireSession (seedReactions call);
cmd/apogee/wire_engine_test.go — recordingRunner, TestSetReactionsSkipsTheRunnerWhenObserveIsUnchanged, TestLateEngineReplaysThePendingGeneration; cmd/apogee/wire_settings_test.go — TestApplySettingReactionsReplacesTheRunnerAndTheProjection; internal/run/run.go — Once
**Tests:** one swap test: after `SetReactions`, the observe Runner fires only the new lane.
**Acceptance:**
- `grep -n "reactionSwapper\|reflect\.DeepEqual" cmd/apogee/wire_engine.go` prints nothing
- `GOMEMLIMIT=2GiB go test -race -count=1 -run '^TestSetReactionsArgvGateNeverReachesTheSeamCascade$|^TestSetReactionsArmsAnAdviseEntryForTheNextToolResult$|^TestSetReactionsArmsAScopedGateOnlyInItsWorkspace$|^TestSetReactionsFloorOnlySwapLeavesTheSyncLaneArmed$|^TestSetReactionsReachesAChildSpawnedAfterTheSwap$|^TestSetReactionsRebuildsTheLadderOnlyWhenTheFloorMoves$|^TestSetReactionsRebuildsTheLadderWhenOnlyTheNoticeSwitchMoves$|^TestSetReactionsRefusesAMalformedGeneration$|^TestSetReactionsRefusesAReservedBuiltinID$|^TestSetReactionsRemovingAGateStopsTheDenial$|^TestSetReactionsSwapsFloorAndBypassAtomically$' ./internal/agent/` plus the swap test this item adds
- `GOMEMLIMIT=2GiB go test -count=1 -run '^TestSetReactionsAppliesEngineThenRunner$|^TestSetReactionsSkipsTheRunnerWhenObserveIsUnchanged$|^TestSetReactionsWithoutARunner$|^TestSetReactionsReportsTheRunnersRefusal$|^TestSetReactionsRefusedByTheEngineReachesNeitherHalf$|^TestLateEngineReplaysThePendingGeneration$|^TestLateEngineBindRefusesAPendingGenerationTheEngineWillNotArm$|^TestLateEngineReplaysTheFloorGatesAtTheBind$|^TestApplySettingReactionsReplacesTheRunnerAndTheProjection$|^TestApplySettingReactionsRefusesABrokenFileWithoutMovingAnything$|^TestReactionsRowReloadArmsTheSyncLaneOnTheBoundAgent$|^TestReactionsRowReloadSwapsObserveOnly$|^TestProbeContextLiveSendsTwiceWhenReactionsArmed$|^TestE2EReactionsReloadSwapsTheArmedList$' ./cmd/apogee/`
**Commit:** `refactor(agent): swap the observe runner with the sync lane in one arm`

## 10. Hosts hand the generation whole (#10c) — ✅ DONE (2026-10-02)

NOTES (2026-10-02): the one named split seam is a new domain constructor `domain.LanesOf(list) Generation` (beside SplitLanes in internal/domain/reaction.go, with TestLanesOfCarriesBothLanesAndNothingElse in reaction_test.go). The regression guard names "a domain constructor" as an allowed seam, but **Files:** lists no domain file. SplitLanes stays exported and is used by LanesOf and by internal/config's validateReactionBlocks
NOTES (2026-10-02): run.Spec.Sync becomes `run.Spec.Generation`. Once takes only Observe and Sync from it and keeps Floor, Bypass and ContextFillNotice from a.Generation(). It swaps when either lane is non-empty, and the observe lane reaches Config.ObserveRunner through SetReactions (TestOnceHandsTheSpecsObserveLaneToTheFiringsRunner, which uses a recording runner). The refusal sentence changes from "arm the firing's sync lane" to "arm the firing's reaction lanes"
NOTES (2026-10-02): raise still builds one Runner per Firing over lanes.Observe, so an unresolvable `workspace:` is still a stageCompose refusal. It hands the whole value to Spec.Generation. The Firing's Config still sets no ObserveRunner (domain.Config's doc names "a Firing" among the nil-Runner callers), so for a Firing the Agent only validates and records the observe lane
NOTES (2026-10-02): wire_boot reads the Runner's observe seed off generationOf(w.opts), the projection the engine holder is seeded with. measureTurn1 now takes the whole lanes Generation and arms it as Once does. An observe-only config therefore makes one SetReactions call on the probe Agent, which validates and records the lane and has no Runner to reach. Its refusal now reads "arm the reaction lanes"
NOTES (2026-10-02): NewReactionRunner's "validate first" sentence is dropped. The Runner's initial list passes the arm at agent.New when it is handed as Config.Observe (item 9's construction-time validateGeneration), and the doc now says that
NOTES (2026-10-02): consequential edit — internal/config/options.go: made necessary by Drivers dividing the list through domain.LanesOf (the Reactions field doc named [domain.SplitLanes] as the Driver's divider)

**What:** Depends on item 9.
**Goal:** no host calls `domain.SplitLanes`: `wire_boot.go`, `wire_settings.go`
(`generationOf`, `reloadReactions`), `probecontext.go`, `wire_firing.go` and `run.Spec` (a
whole Generation replaces `Sync`) hand the list or Generation to the arm; a Firing still gets one
Runner per Firing; `apogee.go`'s `NewReactionRunner` doc stops asking embedders to validate only if the
Runner's initial list passes the arm (Regression guard).
**Approach (assumed at the header base):** `SplitLanes` stays only inside the arm and `internal/config`.
**Regression guard.** `run.Once` keeps read-edit-hand-back: it takes only Observe/Sync from the Spec's Generation and keeps
Floor/Bypass/ContextFillNotice from `a.Generation()` (run.go's documented hazard: a `bypass: true` Firing would run with every
guard on). The list is divided at one named seam — a domain constructor or the arm — and the Runner takes class observe only
(`reactions` `buildSet` does not filter by class); no Generation field is named `Reactions` (`TestNoWiringSiteWritesConfigReactions`).
`NewReactionRunner`'s "validate first" sentence stays unless the Runner's initial list passes the arm at `agent.New` (item 9's
Config route); the item says which.
**Files:** cmd/apogee/wire_boot.go; cmd/apogee/wire_settings.go; cmd/apogee/probecontext.go; cmd/apogee/wire_firing.go; cmd/apogee/headless_test.go; cmd/apogee/schedule_test.go; cmd/apogee/wire_firing_test.go; internal/run/run.go; internal/run/run_test.go; apogee.go
**Read first:** internal/run/run.go — Spec.Sync, Once; cmd/apogee/wire_firing.go — raise, firingHooks; cmd/apogee/wire_settings.go — generationOf, reloadReactions, liveSettings.setReactionLanes; cmd/apogee/probecontext.go — measureContextCost, measureTurn1;
cmd/apogee/wire_boot.go — rootWiring.resolveConfig; apogee.go — NewReactionRunner; cmd/apogee/wire_firing_test.go — TestNoWiringSiteWritesConfigReactions; internal/run/run_test.go — TestOnceArmsTheSpecsSyncLaneBeforeTheFirstStep
**Tests:** existing firing/run tests green; `run_test.go` asserts a Spec Generation's observe lane reaches the Firing's Runner.
**Acceptance:**
- `grep -rn "SplitLanes" cmd/ internal/run/` prints nothing
- `go test -race -count=1 ./internal/run/`
- `GOMEMLIMIT=2GiB go test -count=1 -run '^TestNoWiringSiteWritesConfigReactions$|^TestHeadlessArmsTheSyncLaneOnTheFiringsSpec$|^TestScheduleFiringCarriesTheSessionsSyncLane$|^TestFiringConfigCarriesTheFloorGuardKeys$|^TestBootConfigCarriesTheFloorGuardKeys$|^TestProbeContextNamesArmedReactions$|^TestProbeContextLiveSendsTwiceWhenReactionsArmed$|^TestReactionsRowReloadArmsTheSyncLaneOnTheBoundAgent$|^TestReactionsRowReloadSwapsObserveOnly$|^TestApplySettingReactionsReplacesTheRunnerAndTheProjection$|^TestLiveSettingsGenerationIsDerivedFromTheOverlay$' ./cmd/apogee/`
**Commit:** `refactor(run): hand the reaction generation to the engine whole`

## 11. The textarea mirror lives behind lineEditor (#14) — ✅ DONE (2026-10-02)

NOTES (2026-10-02): consequential edit — internal/tui/chromelayout_test.go: made necessary by moving sanitizeInputLine to editorgeometry.go (comment locator "(sanitizeInputLine, inputaccent.go)" fixed).
NOTES (2026-10-02): consequential edit — internal/tui/lineeditor.go: made necessary by moving cellToRuneOffsetIn to editorgeometry.go (comment locator "(cellToRuneOffsetIn, mouse.go)" fixed, the two-line comment reflowed).
NOTES (2026-10-02): besides the three Read-first tests, the rest of inputaccent_test.go's wrapRowStarts cost section (wrapRowCorpus, longProseLine, wrapRowStartsTotalAlloc, TestWrapRowStartsAllocationIsIndependentOfWidth) moved to editorgeometry_test.go with baseWrapRowStarts; the promptEditor row-count memo tests and BenchmarkInputContentRows stay in inputaccent_test.go under a re-labelled section header, and chromelayout_test.go's inputContentRows oracle stays in place.

**What:**
**Goal:** the string geometry that mirrors `bubbles/textarea` — `wrapRowStarts`,
`inputContentRows`, `sanitizeInputLine`, `visualSubline`, `cellToRuneOffset`,
`cellToRuneOffsetIn`, `caretOffset`, `offsetToLineCol`, `runeOffsetOf`, `byteOffsetOf`,
`selectionText`, with their sibling mirrors `runesWidth`, `growingWidth`, `inputTabCells` and
`sanitizerDropsRune` — lives in `internal/tui/editorgeometry.go`, owned by the line editor (`cellToRuneOffsetIn`
moves as the painter-measured sibling, not a mirror);
`mouse.go` keeps only gestures. Free functions, unchanged signatures and behaviour.
**Approach (assumed at the header base):** move from `inputaccent.go` and `mouse.go`; move their
oracle tests from `mouse_test.go`/`inputaccent_test.go` into `editorgeometry_test.go`.
**Regression guard.** `runesWidth`, `growingWidth`, `inputTabCells` and `sanitizerDropsRune` move with `wrapRowStarts` (ADR 0030
§6 and doc.go name them the same mirrors); `cellToRuneOffsetIn`'s oracle is the painter's `widthAuthority`, and its doc says so
in the new file. Prose rule: every comment or ADR line placing a moved name in inputaccent.go or mouse.go —
`grep -rn 'inputaccent\.go\|mouse\.go' internal/tui/*.go docs/adr/0030*` — is fixed; ADR 0030 §6's locator is amended in place.
**Files:** internal/tui/editorgeometry.go; internal/tui/editorgeometry_test.go; internal/tui/inputaccent.go; internal/tui/mouse.go; internal/tui/mouse_test.go; internal/tui/inputaccent_test.go; internal/tui/doc.go; docs/adr/0030-the-tui-has-one-width-authority-and-it-mirrors-the-painter.md
**Read first:** internal/tui/inputaccent.go — wrapRowStarts, growingWidth, inputContentRows, runesWidth, sanitizeInputLine, sanitizerDropsRune; internal/tui/mouse.go — visualSubline, cellToRuneOffset, cellToRuneOffsetIn, caretOffset, offsetToLineCol, runeOffsetOf, byteOffsetOf, selectionText; internal/tui/lineeditor.go — caretRune, caretTo, caretByteOffset, seatCaret, caretToOffset;
internal/tui/mouse_test.go — TestCaretOffset, TestCaretOffsetRoundTrips, TestSelectionText, TestCellToRuneOffset, TestCellToRuneOffsetInvertsWidth, TestVisualSubline; internal/tui/inputaccent_test.go — TestWrapRowStartsMirrorsTheWidget, baseWrapRowStarts, TestWrapRowStartsMatchesTheWholeRunMeasure; internal/tui/chromelayout_test.go — inputContentRows oracle;
internal/tui/doc.go — inputaccent.go/width.go paragraph; docs/adr/0030-the-tui-has-one-width-authority-and-it-mirrors-the-painter.md — §6
**Tests:** the moved tests; `chromelayout_test.go` oracle green.
**Acceptance:**
- `go vet ./internal/tui/`
- `grep -c '^func \(wrapRowStarts\|inputContentRows\|runesWidth\|sanitizeInputLine\|sanitizerDropsRune\|visualSubline\|cellToRuneOffset\|cellToRuneOffsetIn\|caretOffset\|offsetToLineCol\|runeOffsetOf\|byteOffsetOf\|selectionText\)(' internal/tui/editorgeometry.go` prints `13`, and `grep -c '^type growingWidth \|^const inputTabCells ' internal/tui/editorgeometry.go` prints `2`
- `GOMEMLIMIT=2GiB go test -race -count=1 -run '^TestCaretOffset$|^TestCaretOffsetRoundTrips$|^TestSelectionText$|^TestCellToRuneOffset$|^TestCellToRuneOffsetInvertsWidth$|^TestVisualSubline$|^TestClickPositionsCaret$|^TestClickPositionsCaretCJK$|^TestWrapRowStartsMirrorsTheWidget$|^TestWrapRowStartsMatchesTheWholeRunMeasure$|^TestWrapRowStartsAllocationIsIndependentOfWidth$|^TestInputCellSpans$|^TestAccentSpansFollowTheCatalog$|^TestInputContentRowsWithoutAMemo$|^TestInputContentRowsMeasuredOncePerKeypressAndView$|^TestInputContentRows$|^TestInputContentRowsZeroWidth$|^TestInputContentRowsMirrorsTheWidget$|^TestInputContentRowsMirrorsTheWidgetOnGeneratedDrafts$|^TestDocMapNamesEveryFile$' ./internal/tui/`
**Commit:** `refactor(tui): move the textarea geometry mirror behind the line editor`

## 12. The keystore runs its tools through userexec (#15) — ✅ DONE (2026-10-02)

NOTES (2026-10-02): consequential edit — internal/keystore/keystore.go: made necessary by trimCappedKeyTail now taking the toolResult (it reads stderrCapped instead of measuring the text against the cap)
NOTES (2026-10-02): keystore's maxToolStderr and waitGrace constants are gone with cappedBuffer (userexec.MaxStderr / userexec.WaitGrace now govern); keystore_test.go's padding reads userexec.MaxStderr. maxErrorStderr and said() stay as keystore's own sentence.
NOTES (2026-10-02): trimming now fires only when the tool overran the cap (StderrCapped), not when the capture merely filled it to exactly MaxStderr bytes — such a capture was never cut. A store program missing at run time now reads `<tool> could not be run: "<path>" is not on this machine's PATH` (userexec's resolve sentence) instead of exec's raw error; Store.Write's sentence shape is unchanged.
NOTES (2026-10-02): the new keystore_unix_test.go test was confirmed failing against the pre-item run.go (grandchild survived the deadline).

**What:**
**Goal:** `internal/keystore` runs credential tools via `userexec.Run`, so a timed-out tool's
whole process tree is torn down (latent defect: only the leader was killed); redaction runs on
the raw capped stderr before folding; keystore's own user-facing sentences are unchanged.
**Approach (assumed at the header base):** `userexec.Result` gains `Stderr` (raw, capped at
`MaxStderr`) and `StderrCapped`; `keystore/run.go` `runTool` calls `userexec.Run`, `cappedBuffer`
goes, `trimCappedKeyTail` reads `StderrCapped`; rewrite the comments that explained why
keystore stayed separate (Regression guard).
**Regression guard.** Prose rule: every comment saying keystore runs outside userexec, or mirroring its guard —
`grep -n "keystore" internal/userexec/userexec.go; grep -n "userexec" internal/keystore/run.go`. `runTool` checks
`Result.TimedOut` first (`userexec.Run` returns a nil error on a deadline kill) and rewraps any non-nil Run error as
`"<filepath.Base(program)> could not be run: %w"`, unwrapping userexec's "could not run" wrapper, so `Store.Write`'s sentence
is unchanged. The child-killed test lives in `internal/keystore/keystore_unix_test.go` (`//go:build !windows`, as
`userexec_unix_test.go`) and drives `runTool` directly with a short ctx (`writeTimeout` is 60 s).
**Files:** internal/userexec/userexec.go; internal/userexec/userexec_test.go; internal/keystore/run.go; internal/keystore/keystore_test.go; internal/keystore/keystore_unix_test.go
**Read first:** internal/keystore/run.go — runTool, toolResult, trimCappedKeyTail, cappedBuffer, said; internal/userexec/userexec.go — Run, Result, ResolveProgram, cappedWriter, WaitGrace; internal/keystore/keystore.go — Store.Write, Store.answers, probe;
internal/keystore/keystore_test.go — TestMain, useFakeTools, probedStore, padToCutInsideTheKey, TestWriteRedactsASecretTheStderrCapCutInHalf, TestWriteKeepsWhatAToolSaidWhenTheCapWasNeverReached; internal/security/execsafety.go — ResolveProgram
**Tests:** keystore redaction tests green; a userexec test for `Stderr`/`StderrCapped`; a keystore
test in `keystore_unix_test.go` that a timed-out tool's child is killed (fails before).
**Acceptance:**
- `go vet ./internal/userexec/ ./internal/keystore/`
- `go test -race -count=1 ./internal/userexec/ ./internal/keystore/`
**Commit:** `fix(keystore): run credential tools through userexec for whole-tree teardown`

## 13. The subprocess teardown constructor is injected — ✅ DONE (2026-10-02)

**What:** Depends on item 12.
**Goal:** `subprocess.NewProcessTeardown` is no longer a package var: the subprocess Spec/host
value carries a `NewTeardown` field, zero meaning the platform constructor; tests set the field.
**Approach (assumed at the header base):** mirror plan 01's `mcp.Host{NewTeardown}`; `tools`
`execHost.run` is the production seat.
**Regression guard.** The Acceptance greps the directory with `-r`: without it grep prints "Is a directory" to stderr and
nothing to stdout, so the check could never fail.
**Files:** internal/subprocess/subprocess.go; internal/subprocess/teardown_test.go; internal/tools/exec_host.go
**Read first:** internal/subprocess/subprocess.go — NewProcessTeardown, run, SubprocessSpec; internal/subprocess/teardown_test.go — installFakeTeardown, fakeTeardown, TestRunSubprocessReleasesTheTeardownOnEveryExitPath; internal/mcp/client.go — Host.NewTeardown, withStdioDefaults;
internal/tools/exec_host.go — execHost, defaultExecHost; internal/tools/exec_host_test.go — TestNoPackageLevelExecSeam, TestNoPackageLevelExecSeamBites
**Tests:** `installFakeTeardown` becomes a field set.
**Acceptance:**
- `grep -rn "^var NewProcessTeardown" internal/subprocess/` prints nothing
- `GOMEMLIMIT=2GiB go test -race -count=1 ./internal/subprocess/ && GOMEMLIMIT=2GiB go test -race -count=1 -run Exec ./internal/tools/`
**Commit:** `refactor(subprocess): inject the process teardown constructor`

## 14. The process wait delay is a default, not a global — ✅ DONE (2026-10-02)

NOTES (2026-10-02): re-derived from the assumption that mcp.Host's command is built in internal/mcp/client.go — buildStdioTransport (internal/mcp/transport.go) builds the Cmd and teardown, so the Host.WaitDelay assignment and its doc sentence live there; client.go carries the field and its withStdioDefaults resolution.
NOTES (2026-10-02): consequential edit — internal/platform/doc.go: made necessary by ProcessWaitDelay becoming an overridable const default (file-map sentence).
NOTES (2026-10-02): teardown_unix.go, teardown_windows.go and namespace_linux.go need no edit — they read ProcessWaitDelay, which compiles unchanged as a const.
NOTES (2026-10-02): TestRunSubprocessReportsAWedgedDrain now runs t.Parallel() (its only shared state is gone); TestClose_BoundsTheDrainOfAWedgedStdioServer stays serial because assertNoGoroutineIn scans every goroutine in the binary — comment says so.

**What:** Depends on item 13.
**Goal:** `platform.ProcessWaitDelay` is a const default; `SubprocessSpec` and `mcp.Host` carry a
`WaitDelay` field (zero = default) set on the command after the teardown is built; no test
mutates a platform global.
**Approach (assumed at the header base):** readers `teardown_unix.go`, `teardown_windows.go`,
`namespace_linux.go`; overriders `subprocess_test.go`, `mcp_test.go`; `userexec` keeps its per-command override.
**Regression guard.** The Acceptance checks the Goal's const default itself: `grep -rn "^var ProcessWaitDelay" internal/platform/`
prints nothing — a tree that only stops the two tests assigning the var fails it. (`mcp.Host` lives in `internal/mcp/client.go`.)
**Files:** internal/platform/teardown.go; internal/platform/teardown_unix.go; internal/platform/teardown_windows.go; internal/platform/namespace_linux.go; internal/subprocess/subprocess.go; internal/subprocess/subprocess_test.go; internal/mcp/client.go; internal/mcp/mcp_test.go
**Read first:** internal/platform/teardown.go — ProcessWaitDelay; internal/subprocess/subprocess_test.go — TestRunSubprocessReportsAWedgedDrain; internal/mcp/mcp_test.go — TestClose_BoundsTheDrainOfAWedgedStdioServer, TestBuildStdioTransport_CancelArmsTheCmdsTeardown;
internal/mcp/transport.go — buildStdioTransport; internal/mcp/client.go — Host, withStdioDefaults; internal/subprocess/subprocess.go — run, SubprocessResult.DrainWedged; internal/userexec/userexec.go — Run (WaitGrace override)
**Tests:** the two overriding tests set the field.
**Acceptance:**
- `GOOS=windows go vet ./internal/platform/ && go vet ./internal/platform/ ./internal/subprocess/ ./internal/mcp/`
- `grep -rn "^var ProcessWaitDelay" internal/platform/` prints nothing
- `go test -race -count=1 ./internal/platform/ ./internal/subprocess/ ./internal/mcp/`
**Commit:** `refactor(platform): make the process wait delay a default callers override`

## 15. The audit ring goes; the event stream is the trail (#16)

**What:**
**Goal:** `security.AuditLog` and `Guards.Audit` no longer exist; `Guards` holds the floor and
the breaker; `domain.AuditEvent` on the EventSink is the only audit trail and tests assert a
recording sink; `AuditDecision` stays; ADR 0013 carries an amendment that sub-agent isolation is
by depth and spawn ids, not a fresh AuditLog; `CONTEXT.md` lines naming the ring are updated.
**Approach (assumed at the header base):** delete `internal/security/audit.go` and
`audit_test.go`; trim `guard.go` (`ForSubAgent`, `RecordExecution`, `RecordBlocked`);
`internal/agent/dispatch.go` `recordExecuted`/`recordBlocked`/`emitAudit`.
**Regression guard.** The agent test files that read the ring change too: `audit_event_test.go` (`child.guards.Audit.Len`),
`subagent_test.go` (`childGuards.Audit == a.guards.Audit`), `guardrails_test.go`; `AuditEvent` carries no result text, so the
halves that asserted `r.Result` move to the ToolResult event (`dispatch_test.go` "denied by approver", `guardrails_test.go`).
`AuditDecision` and its four constants move to `guard.go`, and `security/doc.go`'s file map says so. Prose rule: every comment
or doc naming the audit ring/log, `AuditLog`, `AuditRecord` or a fresh audit — the Acceptance grep.
**Files:** internal/security/audit.go; internal/security/audit_test.go; internal/security/guard.go; internal/security/guard_test.go; internal/security/doc.go; internal/agent/dispatch.go; internal/agent/dispatch_test.go; docs/adr/0013-the-sub-agent-orchestrator-is-the-recursion-point-with-isolated-live-guard-state.md; internal/agent/audit_event_test.go; internal/agent/guardrails_test.go; internal/agent/subagent_test.go; internal/domain/events.go; CONTEXT.md
**Read first:** internal/security/guard.go — Guards, NewDefaultGuards, ForSubAgent, RecordExecution, RecordBlocked, PreCheck; internal/agent/dispatch.go — recordExecuted, recordBlocked, emitAudit; internal/agent/audit_event_test.go — TestAuditEvent_SubAgentRecordReachesParentObserver, auditEvents;
internal/agent/guardrails_test.go — TestGuardrails_AuditRecordsCallDecisionResult; internal/agent/subagent_test.go — ForSubAgent identity check; internal/domain/events.go — AuditEvent; internal/security/doc.go — file map; CONTEXT.md — sub-agent guard isolation, guardrails entry
**Tests:** `dispatch_test.go`/`guardrails_test.go` assert `AuditEvent`s on a recording sink.
**Acceptance:**
- `go vet ./internal/security/ ./internal/agent/ && GOMEMLIMIT=2GiB go test -race -count=1 ./internal/security/`
- `GOMEMLIMIT=2GiB go test -race -count=1 -run '^TestAuditEvent_EmittedForExecutedCall$|^TestAuditEvent_EmittedForRefusedCall$|^TestAuditEvent_SubAgentRecordReachesParentObserver$|^TestDispatch_ApproverErrorRefuses$|^TestDispatch_ByteIdenticalRepeatStillRuns$|^TestGuardrails_AuditRecordsCallDecisionResult$|^TestGuardrails_CircuitBreakerTrips$|^TestNewChildAgent_IsBuiltFromOneDelegationValue$|^TestSubAgent_DangerousFloorSharedReadOnly$|^TestSubAgent_EventsCarryTheSpawningCallID$|^TestSubAgent_UnknownToolNameIsRefusedBeforeAnyChildRuns$' ./internal/agent/`
- `grep -rniE "audit ring|audit log|AuditLog|AuditRecord|fresh audit" --include=*.go --include=*.md internal CONTEXT.md docs/adr | grep -v 0013-` prints nothing
**Commit:** `refactor(security): drop the audit ring for the event-stream trail`

## 16. One renderer for the recipe launch line (#17a)

**What:**
**Goal:** `domain.RecipeLaunch.Line()` is the only producer of the `/<id> <inputs>` line;
`daemon.WorkflowAction.Launch`, `run.Spec.line` and `Agent.StartRecipe` call it. Output bytes unchanged.
**Approach (assumed at the header base):** add `Line()` in `internal/domain/config.go`.
**Files:** internal/domain/config.go; internal/domain/config_test.go; internal/daemon/file.go; internal/run/run.go; internal/agent/recipe.go
**Read first:** internal/domain/config.go — RecipeLaunch; internal/daemon/file.go — WorkflowAction.Launch (keep its empty-Recipe ""); internal/daemon/diff.go — Entry.Spec; internal/run/run.go — Spec.line, Spec.title; internal/agent/recipe.go — StartRecipe; internal/daemon/diff_test.go — the Launch table cases
**Tests:** a `Line()` table test (with and without inputs).
**Acceptance:**
- `go test -race -count=1 ./internal/domain/ ./internal/daemon/ ./internal/run/`
- `GOMEMLIMIT=2GiB go test -race -count=1 -run '^TestRecipe_StartRecipeSubmitsTheLaunch$|^TestRecipe_StartRecipeRefusals$|^TestRecipe_ALeadingReferenceLaunchesAndTheFirstRequestCarriesTheResultLines$|^TestRecipeStartedEventCarriesResume$|^TestCancelledRecipeLaunchKeepsItsOpening$|^TestLaunch_BackgroundAndResumeSharePlanAndRecipeWiring$' ./internal/agent/`
**Commit:** `refactor(domain): render the recipe launch line in one place`

## 17. Firings carry the recipe launch value (#17b)

**What:** Depends on items 10 and 16.
**Goal:** `run.Spec`, `cmd/apogee/wire_firing.go` `firingInputs` and `daemonfire.go` carry a
`domain.RecipeLaunch` instead of a recipe id + prompt pair; `schedule.Spec.Prompt` stays the label.
**Approach (assumed at the header base):** consumers: headless `--recipe`, `daemonfire.go`,
`wire_firing.go` `recipeWorkflowFailure`.
**Regression guard.** Also Depends on item 10 (both rewrite run.Spec); give it the full set of run.Spec test readers
(headless/schedule/daemonfire tests) in Files. The item states whether `Spec.Prompt` stays empty for a recipe run:
`TestHeadlessRecipeFlowsToTheRunnerSpec` and `TestDaemonFireRunsTheEntrysRecipe` pin it to the inputs text today.
**Files:** internal/run/run.go; internal/run/run_test.go; cmd/apogee/wire_firing.go; cmd/apogee/daemonfire.go; cmd/apogee/headless.go; cmd/apogee/headless_test.go; cmd/apogee/schedule_test.go; cmd/apogee/daemonfire_test.go; cmd/apogee/wire_firing_test.go; cmd/apogee/undo_test.go
**Read first:** internal/run/run.go — Spec, Once (StartRecipe branch), Spec.line; cmd/apogee/wire_firing.go — firingInputs.recipe, raise, recipeWorkflowFailure; cmd/apogee/daemonfire.go — daemonWiring.fire; cmd/apogee/headless.go — runHeadlessBody; cmd/apogee/schedule.go — the raise call;
cmd/apogee/headless_test.go — TestHeadlessRecipeFlowsToTheRunnerSpec; cmd/apogee/daemonfire_test.go — TestDaemonFireRunsTheEntrysRecipe
**Tests:** existing firing/run tests green.
**Acceptance:**
- `go test -race -count=1 ./internal/run/`
- `GOMEMLIMIT=2GiB go test -count=1 -run '^TestHeadlessRecipeFlowsToTheRunnerSpec$|^TestHeadlessComposesTheRunnerSpec$|^TestHeadlessReadsThePromptFromStdin$|^TestHeadlessArmsTheSyncLaneOnTheFiringsSpec$|^TestScheduleFiringCarriesTheSessionsSyncLane$|^TestDaemonFireRunsTheEntrysRecipe$|^TestDaemonFireRunsAPromptEntryAsAMessage$|^TestDaemonFireDoesNotJudgeAPromptEntryAsARecipe$|^TestDaemonFireFailsARecipeWorkflowThatDidNotLand$|^TestDaemonFiresAWorkflowEntryThroughTheEngine$|^TestRaiseCallsOnIDBeforeRefusingAndLatchesTheSchedule$|^TestUndoVerbRefusesAFiringInFlight$' ./cmd/apogee/`
**Commit:** `refactor(run): carry the recipe launch as one value through firings`

## 18. The foreground recipe launch stops re-parsing its text (#17c)

**What:** Depends on item 17.
**Goal:** `Agent.StartRecipe` submits a `domain.UserInput` with a `Recipe *RecipeLaunch`
(`json:",omitempty"`, so saved snapshots without it still load) and the opening Step reads it;
`recipeLaunch`'s prefix match serves only user-typed `/<id>` text.
**Approach (assumed at the header base):** `internal/agent/recipe.go` `recipeLaunch`/`launchRecipe`;
`UserInput` persists via `state.go` `PendingInput`; `eventjson/encode.go` `userInputOf` is left untouched
(Regression guard).
**Regression guard.** `StartRecipe` keeps `Text` "/<id> …" and `SkillIDs[0] = id` beside `Recipe` (recipe.go), because
`composeUserMessage` drops `skillIDs[1:]` whenever `recipeLaunch` reports a launch and reads the model's line from `in.Text`;
otherwise `composeUserMessage` drops the leading skill only when `SkillIDs[0] == Recipe.SkillID` and loop.go joins Files. The
eventjson `userInput` mirror stays as it is (it feeds only `ChildInterjectionEvent`, which never launches a recipe): the item
yields to ADR 0075 — a new wire member would fall under its versioning rule.
**Files:** internal/domain/config.go; internal/agent/recipe.go; internal/agent/recipe_test.go; internal/agent/state_test.go
**Read first:** internal/agent/recipe.go — StartRecipe, recipeLaunch, launchRecipe, recipeLaunchKind; internal/agent/loop.go — composeUserMessage; internal/agent/interject.go — Interject; internal/domain/config.go — UserInput, RecipeLaunch;
internal/agent/state.go — PendingInput, restore checks; internal/agent/state_test.go — TestSnapshot_RestoresPendingInput, TestRestore_RefusesAForgedOrOversizedPendingInput; internal/agent/workflowcall.go — resumeHint, resumeCommand
**Tests:** a pending-input snapshot round-trip with `Recipe` set and one without.
**Acceptance:**
- `go test -race -count=1 ./internal/domain/ ./internal/eventjson/`
- `GOMEMLIMIT=2GiB go test -race -count=1 -run '^TestRecipe_StartRecipeSubmitsTheLaunch$|^TestRecipe_StartRecipeRefusals$|^TestRecipe_ALeadingReferenceLaunchesAndTheFirstRequestCarriesTheResultLines$|^TestRecipe_AMidTextReferenceAttachesTheBody$|^TestRecipe_InterjectRefusesALaunch$|^TestRecipe_ADelegateLaunchesNothing$|^TestCancelledRecipeLaunchKeepsItsOpening$|^TestSnapshot_RestoresPendingInput$|^TestRestore_RefusesAForgedOrOversizedPendingInput$' ./internal/agent/` plus the round-trip tests this item adds
**Commit:** `refactor(agent): hand the recipe launch to the step instead of re-parsing it`

## 19. Pane rank lives on the pane row (#20a)

**What:** Depends on item 11.
**Goal:** each pane's key-claim and pointer rank live on its `framePane` row; `keyClaimOrder` and
`pointerPanes` derive from the rows; `model.go`'s pane label reads "autocomplete dropdown" as
`panes.go` does. Ordering unchanged.
**Regression guard.** `keyClaimOrder` derives as the pane rungs sorted by each row's key rank (the prompt has none) followed
by the literal "run view" and "block cursor" rungs, in that order. Both lists are built inside panes.go's `init()` after
`paneSpecs` is assigned (or as funcs read at call time) — a package-var initializer runs first and reads zero rows. This
supersedes the in-code decision at panes.go:46-49 ("keyClaimOrder's decision, not the row's", cited to ADR 0053 D3): fix every
comment `grep -n "not the row's\|never the row's\|\[pointerPanes\] alone\|keyClaimOrder's decision" internal/tui/*.go` finds, doc.go included.
**Files:** internal/tui/model.go; internal/tui/mouse.go; internal/tui/panes.go; internal/tui/doc.go; internal/tui/keyclaim_test.go; internal/tui/mouse_test.go
**Read first:** internal/tui/panes.go — paneSpec, paneSpecs init, paneClaimant, reportPaneRow; internal/tui/model.go — keyClaimant, keyClaimOrder, claimKey, framePane constants; internal/tui/mouse.go — pointerPanes, handleMouseClick pane walk, foldMouseWheel pane walk;
internal/tui/keyclaim_test.go — TestKeyClaimOrderMatchesTheDocumentedPrecedence, TestTheFirstClaimantThatWantsAKeyAnswersIt; internal/tui/mouse_test.go — TestPointerPanesWalkInTheClickChainOrder; internal/tui/panes_test.go — TestEveryFramePaneHasASpec; internal/tui/doc.go — pointerPanes/keyClaimOrder prose
**Tests:** existing order tests green.
**Acceptance:**
- `GOMEMLIMIT=2GiB go test -race -count=1 -run '^TestKeyClaimOrderMatchesTheDocumentedPrecedence$|^TestTheFirstClaimantThatWantsAKeyAnswersIt$|^TestTabAtIdleWithHintsReachesTheFramesOwnVerb$|^TestPointerPanesWalkInTheClickChainOrder$|^TestEveryFramePaneHasASpec$|^TestTheFourReportsPaintInTheFramePaneOrder$' ./internal/tui/`
**Commit:** `refactor(tui): keep each pane's rank on its row`

## 20. The spinner reads UI preferences (#20b)

**What:** Recast at the regression check (2026-10-02). Depends on item 19.
**Goal:** `settingsApplyLocal` writes the spinner's preferences only through `m.opts.UI`; `spinnerAnim`
holds no style or colour; the spinner paints from `m.opts.UI`. The transcript keeps `taskListOpen`/`toolsOpen`/`toolsFoldOver`.
Behaviour unchanged.
**Regression guard.** Owner call 2026-10-02 — spinner only: the transcript keeps taskListOpen/toolsOpen/toolsFoldOver as the
fold seed read with no Model in reach (document that on the fields) and the setters' repaint bumps stay; only the spinner's
style/color stop mirroring and read UIPrefs. The item yields to transcript.go:55-66 (the fold seed lives on the transcript for
`ws`'s reason).
Re-check: interval/blink/framesPerBlinkHalf/view change, so their callers paintcache.go:446, mouse_test.go:1967 and model_test.go:6558 are in
Files and their tests run; the field-reading test sites (settings_test.go:844,1676; spinner_test.go:886-903) are rewritten to assert the
painted glyph instead of adding a duplicate test (settingsApplyLocal already moves m.spin, so a new test passes pre-item).
**Files:** internal/tui/settingsapply.go; internal/tui/spinner.go; internal/tui/transcript.go; internal/tui/model.go; internal/tui/paintcache.go; internal/tui/settingsapply_test.go; internal/tui/settings_test.go; internal/tui/spinner_test.go; internal/tui/mouse_test.go; internal/tui/model_test.go
**Read first:** internal/tui/spinner.go — spinnerAnim, newSpinnerAnim, spinnerAnim.blink; internal/tui/settingsapply.go — settingsApplyLocal; internal/tui/paintcache.go — Model.frameKey;
internal/tui/model.go — newModel; internal/tui/mouse_test.go — TestTranscriptClickTogglesALiveBlockAcrossTheBlink; internal/tui/spinner_test.go — TestNewModelSelectsTheConfiguredSpinner
**Tests:** existing settings-apply and spinner tests green; TestNewModelSelectsTheConfiguredSpinner,
TestSettingsPaneEnumSubListCommitsAndBacksOut and TestSettingsPaneRendererOwnedKeysApplyWithoutTheSeam assert the painted glyph
instead of `m.spin.style`/`m.spin.color`; no new test.
**Acceptance:**
- `GOMEMLIMIT=2GiB go test -race -count=1 -run '^TestNewModelSelectsTheConfiguredSpinner$|^TestSettingsApplyLocalLandsEveryUIKeyThroughSet$|^TestSettingsPaneEnumSubListCommitsAndBacksOut$|^TestSettingsPaneRendererOwnedKeysApplyWithoutTheSeam$|^TestSpinnerColorIsOrthogonalToStyle$|^TestSpinnerColorOffPaintsNoForeground$|^TestSpinnerColorLoops$|^TestSpinnerColorIsSoft$|^TestSpinnerColorPeriodIsTenSeconds$|^TestGlitterSparkles$|^TestTranscriptClickTogglesALiveBlockAcrossTheBlink$|^TestPageDownAdvancesOneDrawnScreenfulWithAPaneOpen$|^TestSpinnerFrameWidth$|^TestSpinnerStyleFallsBackToClassic$|^TestSpinnerClassicUncolouredIsUnchanged$|^TestSpinnerTickChainGeneration$|^TestSpinnerFlipRepaintsForRunningWorkflow$|^TestSpinnerFlipIsSettledByTheTail$|^TestSnakeFrames$|^TestSnakeIsSixDotsOnTheRing$|^TestSnakeCycles$' ./internal/tui/`
- `grep -c "spin\.style\|spin\.color" internal/tui/*.go` prints `0` for every file (today 2/3/5)
**Commit:** `refactor(tui): read UI preferences instead of mirroring them`

## 21. Rune clipping goes through sanitize.ClampRunes (#20d)

**What:**
**Goal:** `capRunes` and `Clip` (`internal/title/title.go`), `tui/textutil.go` `clipRunes` and `cmd/apogee/headless.go`
`clipSubAgentTask` call `sanitize.ClampRunes` wherever their ellipsis and boundary rules match;
a helper whose rule differs stays, with a comment naming the difference. Output bytes unchanged.
**Regression guard.** `clipSubAgentTask` keeps its fit test at `headlessTaskMax` and clamps to `headlessTaskMax-1` only past it
(an ellipsis inside the cap, unlike the other three), so an exactly-`headlessTaskMax` task still prints whole; a headless test
pins that case. `capRunes` and `Clip` live in `internal/title/title.go`, whose tests the Acceptance runs; one package per run.
**Files:** internal/title/title.go; internal/tui/textutil.go; cmd/apogee/headless.go; cmd/apogee/headless_test.go; internal/sanitize/sanitize.go
**Read first:** cmd/apogee/headless.go — clipSubAgentTask, headlessTaskMax; internal/sanitize/sanitize.go — ClampRunes; internal/title/title.go — capRunes, Clip, excerpt, entryExcerpt; internal/tui/textutil.go — clipRunes, clipDetail; internal/sanitize/doc.go — ClampRunes bullet;
cmd/apogee/headless_test.go — TestHeadlessOutputRouting, TestNarrationSinkSummarisesTheFirstStringArgument; internal/title/title_test.go — TestClip
**Tests:** existing tests green; an exactly-`headlessTaskMax` case in the headless tests.
**Acceptance:**
- `go test -race -count=1 ./internal/sanitize/`
- `go test -race -count=1 ./internal/title/`
- `GOMEMLIMIT=2GiB go test -count=1 -run '^TestPaintedWideDetailLineWrapsWithoutDisplacement$|^TestSubAgentPromptLineComposition$|^TestSlashMenuBoundsAHostileSkillID$|^TestSubAgentPromptDetailsLeadsWithTheTask$|^TestClipDetail$' ./internal/tui/`
- `GOMEMLIMIT=2GiB go test -count=1 -run '^TestHeadlessOutputRouting$|^TestNarrationSinkSummarisesTheFirstStringArgument$' ./cmd/apogee/` plus the exactly-`headlessTaskMax` test, by name
**Commit:** `refactor: clip runes through sanitize.ClampRunes`

## 22. Test-seam globals become fields (#20e)

**What:**
**Goal:** `agent/loop.go` `restreamHoldoff` and `provider/localdial.go` `dialAddress` are fields
on the value that uses them (zero = production default), not package vars tests mutate.
**Regression guard.** `shortRestreamHoldoff` (overflow_test.go) takes the `*Agent`, each call moving after `newAgent`;
`holdOffRestream`/`restreamHoldoffFor` become `*Agent` methods read by loop.go and compact.go (names kept: ADR 0082 cites them).
The field is carried through the delegation (seeded like `a.now` in `delegation.seed`/`newChildAgentOn`) and copied in
`backgroundHost`, so a child keeps the test's short wait. `dialAddress` gets an owning value — a `localDialer{dial, lookup}`
`withLocalFallback` builds, zero = `orig`/`mdns.Lookup` — and the `TestLocalFallback_*` tests switch from `NewClient(...).Discover`
to `WithHTTPClient` over a `withLocalFallback`-built transport; `lookupLocal` joins it or the item says why it stays a var.
**Files:** internal/agent/loop.go; internal/agent/compact.go; internal/agent/construct.go; internal/agent/subagent.go; internal/agent/background.go; internal/agent/overflow_test.go; internal/agent/loop_test.go; internal/agent/compact_test.go; internal/agent/emptyreply_test.go; internal/agent/subagent_test.go; internal/provider/localdial.go; internal/provider/localdial_test.go
**Read first:** internal/agent/loop.go — restreamHoldoff, restreamHoldoffFor, holdOffRestream, respondAndReview; internal/agent/compact.go — fold re-stream loop (holdOffRestream); internal/agent/construct.go — buildAgent, delegation.seed; internal/agent/background.go — backgroundHost;
internal/agent/overflow_test.go — shortRestreamHoldoff; internal/agent/loop_test.go — TestRestreamHoldoffLadder; internal/provider/localdial.go — dialAddress, lookupLocal, withLocalFallback, dialWithLocalFallback, dialResolved, providerTransport;
internal/provider/localdial_test.go — stubResolver, stubMDNS, TestLocalFallback_DialsThroughCapturedDialer
**Tests:** the overriding tests set fields.
**Acceptance:**
- `go test -race -count=1 ./internal/provider/`
- `GOMEMLIMIT=2GiB go test -race -count=1 -run '^TestCancelInsideRestreamHoldOffStaysResumable$|^TestCapRetryLatchIsSeparateFromTheReStreamLatch$|^TestCompactDoesNotRestreamAPlainSummaryFault$|^TestCompactRestreamsUpToTheBudgetOnATransientSummaryFault$|^TestOverflowGiveUpNamesTheWindowRemedy$|^TestRespondAndReviewReStreamsAMidStreamEOF$|^TestRespondAndReviewReStreamsATransientFaultUpToTheBudget$|^TestReStreamBudgetComesFromConfig$|^TestReStreamBudgetIsPerTurn$|^TestReStreamBudgetNilConfigDefaultsToThree$|^TestReStreamBudgetZeroNeverReStreams$|^TestRestreamHoldoffLadder$|^TestRestreamHoldOffThatElapsesStillReStreams$|^TestRetryExchange_EmptyInjectIsBareRestream$|^TestSubAgent_TransientChildBlipStaysInsideTheDelegation$' ./internal/agent/`
**Commit:** `refactor: turn the restream holdoff and local dial seams into fields`

## 23. domain/hooks.go holds only hook types (#20f)

**What:** Depends on item 2.
**Goal:** `domain.Request`, `domain.Response` and `domain.Conversation` (with their methods) live
in `internal/domain/request.go` (`Request`, `NewRequest`, `RequestState`, `SamplingParams`, `MergeSystem`),
`response.go` (`Response`, `FinishReason`) and `conversation.go` (`Conversation`, `conversationJSON`); `hooks.go` keeps the hook
types, `Message` and the shared helpers; `domain/doc.go`'s map names the new files. Pure move.
**Regression guard.** Each new file's set is the Goal's; `firstIndex`, `lastIndex`, `insertMessage` and `cloneRawMap` stay in
hooks.go. Prose rule: every in-code comment that places Request/Response/Conversation or their methods in domain/hooks.go —
`grep -rn 'hooks\.go' internal/ | grep -v 'internal/reactions\|internal/config'` (hooks.go's header, domain/doc.go,
tooledit.go, agent/subagent.go's wrap-up marker). ADRs 0001, 0017 and 0046 are history and stay.
**Files:** internal/domain/hooks.go; internal/domain/request.go; internal/domain/response.go; internal/domain/conversation.go; internal/domain/doc.go; internal/domain/tooledit.go; internal/agent/subagent.go
**Read first:** internal/domain/hooks.go — header comment, Request, NewRequest, SamplingParams, MergeSystem, Response, FinishReason, Conversation, conversationJSON, firstIndex; internal/domain/doc.go — "The loop's working values" paragraph; internal/domain/docmap_test.go — TestDocMapNamesEveryFile;
internal/domain/hooks_test.go; internal/domain/tooledit.go — header comment; internal/agent/subagent.go — wrap-up marker comment
**Tests:** existing domain tests green, docmap included.
**Acceptance:**
- `go vet ./internal/domain/ && GOMEMLIMIT=2GiB go test -race -count=1 ./internal/domain/`
- `grep -nE '^type (Request|Response|Conversation) struct' internal/domain/*.go` lists exactly `request.go`, `response.go` and `conversation.go`, one line each
**Commit:** `refactor(domain): give request, response and conversation their own files`
