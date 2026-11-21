# Delegation as one value — offer, ending bracket, request

**Goal:** The engine learns what delegation it offers from one offer value. Every delegation ending is bracketed and classified once. A sub_agent call's arguments are parsed into one request per dispatch slot. Every model-facing byte (schemas, refusals, result contents, ledger notes, phase results) stays identical, except for the one ratified change below.
**Date:** 2026-11-21
**Status:** unexecuted
**sized for:** ~200k-context host
**base:** 7869cca7
**Closes:** see the item lines.

**Sources:**
- `.capcom/issues/delegation-offer-type-probes.md`, `.capcom/issues/delegation-args-reparsed.md`, `.capcom/issues/delegation-endings-hand-bracketed.md`
- ADR 0005, ADR 0031, ADR 0039 (D4), ADR 0069 (D3), ADR 0083 part 3, ADR 0094 (D1): `capcom know show "ADR NNNN"`
- `docs/handoffs/2026-11-21 - 01 - parked review issues triage.md` (cluster 2)

**Ratified design calls:**
- **Never-ran leftover:** a child that never ran but has undelivered mail reads `UndeliveredRefused` on BOTH the sub_agent and the workflow-spawn path. The Spawn "never ran" case flips from Faulted (owner, 2026-11-21).
- **Shipped behaviour otherwise kept** (writer, 2026-11-21):
  - an in-`runDelegate` cancel keeps its started+finished pair;
  - Spawn's pre-mint refusals stay phase-less with no run id;
  - a queued stop keeps the word `stopped`;
  - resolve-level sub_agent refusals book no ledger row;
  - each path keeps its own panic text;
  - a foreign tool named `sub_agent` in a child roster is still replaced as `withoutSeatChoice` does today.
- **Offer derived on demand:** `(*Agent).delegationOffer()` reads `a.tools` per call and is never cached. `a.tools` has six writers, including a mid-Turn rehome (writer, 2026-11-21).
- **`planOffersDelegation` stays:** it is a name rule plus `scratchSet`, not a type probe (writer, 2026-11-21).
- **Two parse points remain, by design:** partition, width sizing and run-id minting read the raw pre-Moment calls, and `spawnSubAgent` parses its own synthetic call. Everything after `prepareCall` reads the slot's request (writer, 2026-11-21).
- **Request validates only the pure checks:** decode, empty task, run_on. Roster, `continue` lookup and `context` reads stay at their sites, and each path keeps its refusal order. The `background` read stays lenient, as `asksBackground` reads it (writer, 2026-11-21).
- **ADR 0083 part 3 stands:** the ending is a return value, never a field on `Agent`. The delegate's flat fields and `runSubAgent`'s post-construction writes stay (writer, 2026-11-21).

**Standing requirements:**
- skills: coding-standards
- Run one package and one `-run` pattern per test command, never two test runs at once.
- Byte identity is checked against the item 1 pins. A pin edit is allowed only where an item names it.

**Out of scope:**
- the Driver-side decodes: `cmd/apogee/headless.go` `narrationSink.remember`, `internal/sidepanel/sidepanel.go` `delegationLabel`
- making the delegate's runtime state one value (ADR 0083 part 3)
- `delegation-outcome-reparsed` and other typed-outcome issues

**Regression check (2026-11-21, 7869cca7):**
- 1: guard folded (item 11's two refusal-order inputs added to the pin list)
- 2: guard folded
- 3: guard folded
- 4: guard folded
- 5: guard folded
- 6: recast
- 7: guard folded
- 8: guard folded; supersedes the recover-boundary comments in dispatch.go (runDelegation doc), childrun.go (header) and workflowspawn.go (spawnSubAgent doc)
- 9: guard folded
- 10: guard folded
- 11: guard folded
- 12: guard folded
- 13: guard folded; supersedes docs/manual/headless.md's `refused` wording ("the child, still running, refused it there")
- re-check round:
  - 5: guard folded (decision: literal want-tables; the test calls none of the classifiers item 6 deletes)
  - 6: guard folded (Spawn's no-ending-yet child maps to faulted via `spawnEnding`; runDelegate's defer keeps classifyDelegation's order; capped-then-panicked row; no-definition grep; stale comment citations corrected)

## 1. Pin the model-facing bytes — ✅ DONE (2026-11-21)

NOTES (2026-11-21): "a bad context" is pinned as two inputs — a `context` that is not an array (shape refusal) and an array naming a file the workspace lacks (read refusal); both paths are pinned for every input, including the two refusal-order inputs added for item 11. The schema pin also covers the plain NewSubAgent()/NewFanOut() constructors beside the 8 + 4 NewSubAgentWith/NewFanOutWith variants. The lenient-background test lives in delegationrefusal_pin_test.go, the item's listed agent file.

**What:**
Test-only. Pin every sub_agent and fan_out schema variant, and every delegation refusal, byte for byte at the base before anything moves.
- Schemas: the 8 `subAgentSchema` variants and the 4 `fanOutSchema` variants, as exact literals captured at the base.
- Refusals: the exact `Content` on the blocking path (`runSubAgent`) and the background path, for each of these inputs:
  - `{not json`
  - `{"task":5}`, whose error text names `SubAgentArgs.task`
  - `{}`
  - `run_on:"gpu"`
  - continue+background
  - an unknown tool
  - a bad `context`
  - `{"task":"x","continue":"nope","run_on":"gpu"}` (blocking: the unknown-`continue` refusal precedes the seat check; added for item 11)
  - `{"continue":"x","background":true}` (background: the continuation refusal precedes the empty-task check; added for item 11)
- `{"task":5,"background":true}` takes the background refusal, with no run id and no started phase.
**Files:** internal/tools/delegation_schema_pin_test.go; internal/agent/delegationrefusal_pin_test.go
**Read first:** internal/agent/subagent.go — runSubAgent, runDelegate (seat, roster, context refusal order); internal/agent/workflowcall.go — backgroundSubAgentResult, asksBackground, isBackgroundSubAgentCall; internal/agent/dispatch.go — prepareCall (SpawnRunID mint); internal/tools/sub_agent.go — subAgentSchema, NewSubAgentWith, SubAgentArgs; internal/tools/fan_out.go — fanOutSchema, NewFanOutWith; internal/agent/workflowcall_test.go — backgroundSubAgentConfig, backgroundSubAgentConfigWith; internal/agent/subagent_test.go — direct parent.runSubAgent call tests
**Tests:** `TestDelegationSchemas_BytesArePinned`; `TestDelegationRefusals_AreByteIdentical`; `TestBackgroundSubAgent_ALenientBackgroundWithBadArgsTakesTheBackgroundRefusal`.
**Acceptance:**
- `go test -count=1 -run 'TestDelegationSchemas' ./internal/tools/`
- `go test -count=1 -run 'TestDelegationRefusals|TestBackgroundSubAgent_ALenient' ./internal/agent/`
**Commit:** `test(agent): pin delegation schema and refusal bytes`

## 2. One published offer in tools

**What:**
**Goal:** `tools.DelegationOffer` is the single type that says what a roster publishes for sub_agent and fan_out, and `tools.ChildRoster` derives a child's roster from it.

**Approach (assumed at the header base):**
- New `internal/tools/delegation_offer.go`:
  - `type DelegationOffer struct{ SubAgent SubAgentOptions; FanOut FanOutOptions }`. It is comparable, and its zero value means absent or plain.
  - `OfferOf(*domain.ToolRegistry) DelegationOffer` holds the ONLY type-assert pair. A nil, missing or foreign tool gives zero.
  - `(DelegationOffer).Child()` keeps `SubAgent.Context` only and drops seat choice, run_on and background (ADR 0005, 0069 D3, 0094 D1).
  - `ChildRoster(roster)` replaces agent's `withoutSeatChoice`. It returns the same pointer when `Child()` equals the published offer, and replaces a foreign `sub_agent` as today.
- `SubAgent` holds `opts SubAgentOptions`, as `FanOut` does.
- `builtinToolsWith` builds one offer and constructs both tools from it.
- Templates and `ArgRoles` are untouched.

**Regression guard.** `ChildRoster`'s rule, as `withoutSeatChoice` does today (subagent.go `&& fanOutChoice`): when `Child()` differs from the published offer, swap `sub_agent` (a foreign one included) for `NewSubAgentWith(Child().SubAgent)`, and swap `fan_out` only when `OfferOf`'s `FanOut` is non-zero, so a plain or foreign `fan_out` keeps its value. Keep menu order. `TestChildRoster_ReplacesAForeignSubAgent` pairs the foreign `sub_agent` with a choice-publishing `fan_out`, because a foreign `sub_agent` alone comes back as the same pointer. The Goal is checked by an Acceptance grep: `OfferOf` holds the only `*SubAgent`/`*FanOut` type-assert in non-test `internal/tools`.
**Files:** internal/tools/delegation_offer.go; internal/tools/delegation_offer_test.go; internal/tools/sub_agent.go; internal/tools/registry.go
**Read first:** internal/agent/subagent.go — withoutSeatChoice, offersContext, publishesFanOutChoice, publishesSubAgentChoice; internal/tools/sub_agent.go — SubAgent, NewSubAgentWith, ArgRoles, OffersSeatChoice/OffersBackground/OffersContext; internal/tools/fan_out.go — FanOut, NewFanOutWith; internal/tools/registry.go — builtinToolsWith; internal/domain/tools.go — ToolRegistry.Subset, Register, All; internal/tools/registry_test.go and sub_agent_test.go — tests that type-assert *SubAgent and read Offers*
**Tests:** `TestOfferOf_NilMissingForeignAndEveryVariant`; `TestDelegationOfferChild_NeverPublishesSeatOrBackgroundAndKeepsContext`, over all combinations plus idempotence; `TestChildRoster_ReturnsTheSameRegistryWhenNothingIsStripped`; `TestChildRoster_ReplacesAForeignSubAgent` (paired with a choice-publishing `fan_out`).
**Acceptance:**
- `go build ./...`
- `test "$(grep -lE '\.\(\*(SubAgent|FanOut)\)' internal/tools/*.go | grep -v _test)" = internal/tools/delegation_offer.go`
- `go test -count=1 -run 'TestOfferOf|TestDelegationOffer|TestChildRoster|TestDelegationSchemas|TestSubAgent|TestFanOut|TestBuiltinTool' ./internal/tools/`
**Commit:** `refactor(tools): one DelegationOffer value for sub_agent and fan_out`

## 3. The agent asks one offer for seat and context

Depends on item 2.

**What:**
**Goal:** `internal/agent` reads seat choice and context from one `delegationOffer` value. No seat or context reader type-asserts a tool.

**Approach (assumed at the header base):**
- New `internal/agent/delegationoffer.go`:
  - `type delegationOffer struct{ seatChoice, context, background, fanOutSeatChoice, fanOutBackground bool }`
  - `(*Agent).delegationOffer()` builds it from `tools.OfferOf(a.tools)` plus the live facts:
    - `background` = `!isDelegate && published && ScratchDir()!="" && cfg.WorkspaceDir!=""`
    - `fanOutBackground` = `!isDelegate && published`
- In `subagent.go`, delete `publishesSeatChoice`, `publishesSubAgentChoice`, `publishesFanOutChoice`, `publishesContext`, `offersContext`, `seatChoosingFanOut` and `withoutSeatChoice`. `defaultSubAgentTools` returns `tools.ChildRoster(...)`.
- These callers read the value instead: `readDelegationContext`, `delegationSeats` (orientation.go), `seatsAreSplit` (dispatch.go), `fanOutWidthFor` (delegationwidth.go), `fanOutSeat` (workflowcall.go), and the seat readers `runDelegate` (subagent.go) and `backgroundSubAgentResult` (workflowcall.go).

**Regression guard.** Every Go comment naming a helper this item deletes is corrected in the same item (known: internal/tools/registry.go, internal/agent/loop.go, internal/agent/resolution.go). Rule: grep -rnwE 'withoutSeatChoice|publishesSeatChoice|publishesSubAgentChoice|publishesFanOutChoice|publishesContext|offersContext|seatChoosingFanOut' --include=*.go internal cmd returns nothing outside _test files after the item. Add the hit files to Files.
`TestSubAgent_BackgroundAloneOffersNoSeat` calls `publishesSeatChoice`, so its fix (read `a.delegationOffer().seatChoice`) lands in this item, not item 4, or the test package does not compile at this commit. The Goal is checked by an Acceptance grep for `Offers(SeatChoice|Context)(` in non-test `internal/agent`.
**Files:** internal/agent/delegationoffer.go; internal/agent/delegationoffer_test.go; internal/agent/subagent.go; internal/agent/orientation.go; internal/agent/dispatch.go; internal/agent/delegationwidth.go; internal/agent/workflowcall.go; internal/agent/loop.go; internal/tools/registry.go; internal/agent/seat_test.go; internal/agent/subagent_test.go
**Read first:** internal/agent/subagent.go — publishesSeatChoice, publishesContext, defaultSubAgentTools, runDelegate, readDelegationContext; internal/agent/workflowcall.go — backgroundSubAgentResult, fanOutSeat; internal/agent/orientation.go — delegationSeats; internal/agent/dispatch.go — seatsAreSplit, lookupTool; internal/agent/delegationwidth.go — fanOutWidthFor; internal/agent/seat_test.go — TestSeat_ChildRosterDropsTheRunOnArgument, TestSeat_PlainParentRosterIsInheritedUnchanged; internal/agent/subagent_test.go — TestSubAgent_BackgroundAloneOffersNoSeat; internal/agent/agent.go — isDelegate, ScratchDir
**Tests:** `TestDelegationOffer_ZeroAgentAndNilRosterOfferNothing`; existing `TestSeat_ChildRosterDropsTheRunOnArgument` and `TestSeat_PlainParentRosterIsInheritedUnchanged` (pointer identity); the fixed `TestSubAgent_BackgroundAloneOffersNoSeat`.
**Acceptance:**
- `go build ./... && go vet ./internal/agent/`
- `test -z "$(grep -nE 'Offers(SeatChoice|Context)\(' internal/agent/*.go | grep -v _test)"`
- `test -z "$(grep -rnwE 'withoutSeatChoice|publishesSeatChoice|publishesSubAgentChoice|publishesFanOutChoice|publishesContext|offersContext|seatChoosingFanOut' --include=*.go internal cmd | grep -v _test)"`
- `go test -count=1 -run 'TestDelegationOffer|TestSeat_|TestSubAgentsContext|TestSubAgentContext|TestFanOutWidth|TestOrientation|TestDelegationRefusals|TestSubAgent_BackgroundAlone' ./internal/agent/`
**Commit:** `refactor(agent): seat and context read one delegation offer`

## 4. Background offer readers on the same value

Depends on item 3.

**What:**
**Goal:** No non-test file in `internal/agent` type-asserts `*tools.SubAgent` or `*tools.FanOut` or calls an `Offers*` method.

**Approach (assumed at the header base):**
- Delete `offersBackground` and `offersBackgroundSubAgent` from workflowcall.go.
- These callers read `a.delegationOffer()`:
  - `workflowCallResult`
  - `partitionDispatch`, which takes a `delegationOffer` in place of its bool
  - `prepareCall`, `preemptDelegation`, `refusePastCeiling`, `resolutionInput`, and the `isDelegationCall` / `isBackgroundSubAgentCall` callers
- Move the `offersBackgroundSubAgent` table test onto `TestDelegationOffer_LiveFactsGateBackground`.
- Fix the test callers of the deleted helpers (`TestSubAgent_BackgroundAloneOffersNoSeat` is fixed in item 3):
  - `TestWorkflowControl_ADelegateIsRefused`
  - `TestOffersBackgroundSubAgent_OnlyTheTopLevelAgentOfASessionThatKeepsWorkflows`
  - the `partitionDispatch` calls in fanout_test.go and workflowcall_test.go

**Regression guard.** The same comment rule for offersBackground|offersBackgroundSubAgent, using grep -rnwE 'offersBackground|offersBackgroundSubAgent' --include=*.go internal cmd
**Files:** internal/agent/workflowcall.go; internal/agent/dispatch.go; internal/agent/subagent.go; internal/agent/resolution.go; internal/agent/delegationoffer_test.go; internal/agent/subagent_test.go; internal/agent/workflowcall_test.go; internal/agent/fanout_test.go
**Read first:** internal/agent/workflowcall.go — offersBackground, offersBackgroundSubAgent, isBackgroundSubAgentCall, isDelegationCall, workflowCallResult; internal/agent/dispatch.go — dispatchTools, partitionDispatch, prepareCall, preemptDelegation, refusePastCeiling, resolutionInput; internal/agent/workflowcall_test.go — TestPartitionDispatch_ABackgroundSubAgentIsALeafWhereOffered, TestOffersBackgroundSubAgent_OnlyTheTopLevelAgentOfASessionThatKeepsWorkflows, TestWorkflowControl_ADelegateIsRefused, backgroundSubAgentConfig; internal/agent/fanout_test.go — TestPartitionDispatch_LeafToolsRunBeforeDelegations
**Closes:** delegation-offer-type-probes
**Tests:** `TestDelegationOffer_LiveFactsGateBackground`; the fixed tests above.
**Acceptance:**
- `test -z "$(grep -nE '\*tools\.(SubAgent|FanOut)\)|Offers(SeatChoice|Background|Context)\(' internal/agent/*.go | grep -v _test)"`
- `go test -count=1 -run 'TestDelegationOffer|TestWorkflowCall_|TestWorkflowControl_ADelegate|TestSubAgent_BackgroundAlone|TestPartitionDispatch|TestDelegationRefusals' ./internal/agent/`
**Commit:** `refactor(agent): background delegation reads the one offer`

## 5. One ending value with its projections

Depends on item 4.

**What:**
**Goal:** `internal/agent/childending.go` defines `childEnding`. Every ledger word, cause, `workflow.Ending`, `UndeliveredReason` and `domain.DelegationOutcome` a delegation can end with is a projection of it, pinned by a table test.

**Approach (assumed at the header base):**
- `type childEnding struct{ kind delegationOutcome }` reuses the seven ledger words.
- Constructors:
  - `runEnding(res, err, stopped)`: the one run switch. err → faulted, then stopped, Cancelled → cancelled, Faulted, StepCapped → capped, else completed.
  - `withReport(result)`: completed plus an error result becomes faulted.
  - `refusedEnding()`
  - `unstartedSkipped`, `unstartedPastCeiling`, `unstartedCancelled`, `unstartedStopped`, each carrying today's fixed result content
- Projections:
  - `ledgerRecord(...)`: the cause only for refused and faulted, via `delegationCause`
  - `workflowEnding()`
  - `undelivered()`: never-ran and refused give `UndeliveredRefused`
  - `summary(...)`
- No caller is rewired in this item.

**Regression guard.** The unstarted constructors carry a content (or result) field: `unstartedPastCeiling` takes the rendered `fanOutCeilingResult` (formatted per call from group, rounds, width and the fan_out pointer); only skipped, cancelled and stopped use the dispatch.go constants. `undeliveredWorkflowReason` joins the cross-checked set. `workflowOutcome` tests StepCapped before Faulted before Cancelled, the reverse of `delegationResult`; the combinations where the orders differ (Faulted+StepCapped, Cancelled+either) are unreachable (`finishAtStepCap` clears Faulted), so the test leaves them out.
TestChildEnding_ProjectsTodaysWords states its expectations as literal tables, with today's outputs read from classifyDelegation, delegationSummary, subAgentEnding, workflowOutcome and undeliveredReason at write time. It never calls those functions, so item 6's deletions leave the test compiling and unchanged except for the rows item 6 names.
**Files:** internal/agent/childending.go; internal/agent/childending_test.go
**Read first:** internal/agent/subagent.go — classifyDelegation, delegationResult, delegationSummary; internal/agent/workflowspawn.go — workflowOutcome, subAgentEnding, undeliveredWorkflowReason; internal/agent/children.go — delegationOutcome, undeliveredReason, delegationCause; internal/agent/dispatch.go — fanOutCeilingResult, skippedDelegationContent, cancelledQueuedDelegationContent, stoppedQueuedDelegationContent; internal/agent/agent.go — finishAtStepCap
**Tests:**
- `TestChildEnding_ProjectsTodaysWords`: every reachable input's expectation is a literal want-table, its words read at write time from `classifyDelegation`, `delegationSummary`, `subAgentEnding`, `workflowOutcome`, `undeliveredReason` and `undeliveredWorkflowReason`; the test calls none of them. The one expected difference is the ratified never-ran case.
- `TestRunEnding_OneOrderForBothPaths`
**Acceptance:**
- `go build ./... && go test -count=1 -run 'TestChildEnding|TestRunEnding' ./internal/agent/`
**Commit:** `refactor(agent): one childEnding value projects every delegation ending`

## 6. Classifiers route through the ending

Depends on item 5.

**What:**
Recast at the regression check (2026-11-21).
**Goal:** `classifyDelegation`, `delegationSummary`, `subAgentEnding`, `undeliveredWorkflowReason` and `undeliveredReason` no longer exist as independent switches. `delegationResult` and `workflowOutcome` switch on a `childEnding`.

**Approach (assumed at the header base):**
- Callers in subagent.go, children.go and workflowspawn.go build the ending once and read projections from it.
- Apply the ratified never-ran call: `TestUndeliveredWorkflowReason{"never ran"}` now expects `UndeliveredRefused`.
- Widen the `UndeliveredRefused` doc to cover a child that never ran.

**Regression guard.** The ratified never-ran call applies only to a child that never ran. Item 6 gives Spawn its own `ran` flag, set in runChild's onArmed as runDelegate does. Only !ran maps to UndeliveredRefused; a workflow child that ran and then panicked stays UndeliveredFaulted. Add both rows to TestUndeliveredWorkflowReason. Add internal/agent/dispatch.go to Files and route settleCancelledDelegation and stopQueuedDelegation through childEnding (item 9 later replaces them). Keep the signature delegationResult(callID, res, err) and build the ending inside it from a.stoppedByUser, so the direct test calls and the StatusCancelled empty-result pin stay untouched. Correct the internal/tools/doc.go comment that cites delegationSummary (add the file to Files). The public prose that defines `refused` as a still-running child is item 13's; item 6 widens only the domain doc comment.
Item 5's `TestChildEnding_ProjectsTodaysWords` holds literal want-tables and calls none of the deleted functions (item 5's guard), so it needs no rewrite here; item 6 only adds the rows named below to it.
`TestUndeliveredWorkflowReason` is retargeted onto `spawnEnding(ran bool, ended *childEnding, stopped bool) childEnding` (workflowspawn.go) and reads `.undelivered()`: stopped → stopped; !ran → never-ran (`UndeliveredRefused`); ran with `ended == nil` → faulted (`UndeliveredFaulted`). Spawn's reaping defer runs before its recover (workflowspawn.go:274 vs :244), so a child that ran and then panicked has no ending yet; it must never read the zero `childEnding`, which `undeliveredReason`'s default (children.go:1011) maps to Completed. The "ran then panicked" row passes `ended == nil`.
runDelegate's defer (subagent.go:982) classifies in `classifyDelegation`'s order: dispatch cancel, then stopped, then an error result → refused if !ran else faulted, ahead of capped. Never build it as `runEnding(res, err, stopped).withReport(result)` alone: a capped run whose render or retain panicked would read `capped`, not today's `faulted` + cause, and spawnSubAgent would hand the Runner `EndCapped`, which it continues (runner.go:919). Add a "capped then panicked → faulted" row to `TestChildEnding_ProjectsTodaysWords`.
Every comment that names `classifyDelegation`, `delegationSummary`, `subAgentEnding`, `undeliveredWorkflowReason` or `undeliveredReason` as current code is corrected (today: subagent.go:105,858,870,937,957; dispatch.go:756,783; workflowspawn_test.go:807,857; tools/doc.go:37). Find them with `grep -rnE 'classifyDelegation|delegationSummary|subAgentEnding|undeliveredWorkflowReason|undeliveredReason\b' internal/`.
Superseded source: docs/manual/headless.md:311 (`refused` = "the child, still running, refused it there"); this item yields to it until item 13 rewrites it.
**Files:** internal/agent/subagent.go; internal/agent/children.go; internal/agent/workflowspawn.go; internal/agent/dispatch.go; internal/agent/children_test.go; internal/agent/workflowspawn_test.go; internal/agent/stop_test.go; internal/agent/childending_test.go (the added rows only); internal/tools/doc.go; internal/domain (the `UndeliveredRefused` doc only)
**Read first:** internal/agent/subagent.go — runDelegate, delegationResult, classifyDelegation; internal/agent/workflowspawn.go — Spawn, spawnSubAgent, undeliveredWorkflowReason; internal/agent/children.go — undeliveredReason; internal/agent/workflowspawn_test.go — TestUndeliveredWorkflowReason
**Tests:** `TestWorkflowSpawn_HumanStopReadsStoppedViaTheEnding` (the failure mode of 630d59b8); retargeted `TestUndeliveredReason_*` and `TestUndeliveredWorkflowReason` (calling `spawnEnding(ran, ended, stopped).undelivered()`), with both rows: never ran → `UndeliveredRefused`, ran then panicked (`ended == nil`) → `UndeliveredFaulted`; a "capped then panicked → faulted" row in `TestChildEnding_ProjectsTodaysWords`.
**Acceptance:**
- `go build ./...`
- `go test -count=1 -run 'TestUndelivered|TestWorkflowSpawn|TestStopChild|TestDelegationLedger|TestChildEnding' ./internal/agent/`
- `! grep -nE 'func (\(a \*Agent\) )?(classifyDelegation|delegationSummary|subAgentEnding|undeliveredWorkflowReason|undeliveredReason)\(' internal/agent/*.go`
**Commit:** `refactor(agent): every delegation classifier reads one childEnding`

## 7. The child bracket, unit-tested

Depends on item 6.

**What:**
**Goal:** `childrun.go` provides `childBracket`. It owns the run id, the started/finished phase pair, the recover boundary (ADR 0039 D4) and the single ledger row, and it is tested alone.

**Approach (assumed at the header base):**
- `host.openBracket(turn, call, runID, ledger)` takes the ledger index with `ledger.open` and mints a run id only on the run path (see the guard). A nil ledger means no row.
- `(b).neverStarted(unstarted)` emits a lone finished phase, then the row.
- `(b).run(started, onPanic, body)` emits started. Its defer does, in order:
  1. recover, then `onPanic`;
  2. the ending, which is refused if `!ran`;
  3. the row;
  4. the leftover undelivered events;
  5. the finished phase.
- `(b).runChild` wraps `runChild` and sets `ran` after `onArmed`.
- No caller is moved in this item.

**Regression guard.** State openBracket's two rules in the item: the ledger index is taken with ledger.open (never re-reserved, and an index a pooled group reserved earlier is honoured), and the run id is minted only on the run path and written back to slot.runID. The acceptance pattern `TestRunChild` matches no test at base: replace it with the prefix of an existing test in internal/agent/childrun_test.go, or drop it.
The ending's order is `classifyDelegation`'s: cancelled, then stopped, then refused only for an error/panic ending with `!ran`, then faulted, so a `!ran` cancelled body stays `cancelled` (pinned at subagent_test.go `TestRunDelegation_ACallTheCancelReachedBeforeItsChildIsNeverStarted`). `b.runChild` records the stop verdict before calling `foldStopped`, so a stopped child whose fold panics reads `stopped` ahead of `faulted`.
**Files:** internal/agent/childrun.go; internal/agent/childbracket_test.go
**Read first:** internal/agent/childrun.go — runChild, childRun, childRunStopped; internal/agent/subagent.go — runDelegate (outer recover defer, onArmed, foldStopped), classifyDelegation; internal/agent/children.go — delegationLedger.open, delegationLedger.reserve, delegationRecord; internal/agent/dispatch.go — runDelegation, emitSubAgentPhase; internal/agent/childrun_test.go — TestChildRun_StopVerdict, TestChildRun_AHumanStopWithFoldOnFoldsAndReturnsTheLeftover
**Tests:**
- `TestChildBracket_PanicBeforeRunBooksRefused`
- `TestChildBracket_PanicAfterRunBooksFaulted` (27654d54)
- `TestChildBracket_CancelledBodyClosesThePair` (4dd18a08)
- `TestChildBracket_CancelledBeforeRunBooksCancelled` (a `!ran` cancelled body)
- `TestChildBracket_AStopWhoseFoldPanicsBooksStopped`
- `TestChildBracket_NeverStartedBooksOneRowAndALoneFinish`, over the four kinds (2635ab57)
- `TestChildBracket_RowAndUndeliveredPrecedeFinished`
- `TestChildBracket_NilLedgerBooksNothing`
**Acceptance:**
- `go build ./... && go test -count=1 -run 'TestChildBracket|TestChildRun' ./internal/agent/`
**Commit:** `refactor(agent): childBracket owns a delegation's phases, recover and row`

## 8. Started sub_agent runs on the bracket

Depends on item 7.

**What:**
**Goal:** `runDelegation` and `spawnSubAgent` run their child through `childBracket`. `runDelegate` keeps no recover, ledger or phase code of its own.

**Approach (assumed at the header base):**
- Move `runDelegation`, `runDelegate`'s deferred block and `spawnSubAgent`'s copied phase pair together, so no caller loses its recover.
- `runDelegate` returns the not-started content itself on cancel.
- The bracket stays in `runDelegation`, not `runSubAgent`. The direct `runSubAgent` test callers must see no new phases.
- Keep each path's panic text.

**Regression guard.** `openBracket` returns or exposes the run id it mints, and `runDelegation` keeps `slot.runID = b.runID`, so `commitCall` still stamps the ToolResultEvent's `SpawnRunID` for a call a Reaction redirected into sub_agent. Keep `runSubAgent(ctx, call, runID)`'s signature: `runDelegation` calls `runDelegate` with the bracket, and `runSubAgent` passes a phase-less, ledger-less one, so its 9 direct test callers compile unchanged. Superseded here: the recover-boundary comments in dispatch.go (`runDelegation` doc, "the recover … sits in runSubAgent's own frame"), childrun.go (header, "The helper owns no recover frame") and workflowspawn.go (`spawnSubAgent` doc, "its recover boundary is runDelegate's") are rewritten to name `childBracket`.
**Files:** internal/agent/dispatch.go; internal/agent/subagent.go; internal/agent/workflowspawn.go; internal/agent/childrun.go; internal/agent/workflowspawn_test.go
**Read first:** internal/agent/dispatch.go — runDelegation, runCall, commitCall, stepCapFor; internal/agent/subagent.go — runSubAgent, runDelegate, delegateSite, delegateEnd; internal/agent/workflowspawn.go — spawnSubAgent, Spawn; internal/agent/subagent_test.go — TestRunDelegation_ACallTheCancelReachedBeforeItsChildIsNeverStarted, TestSubAgent_ChildPanicRecoversAtParentBoundary; internal/agent/dispatch_test.go — TestDispatch_SerialDelegationPanicRecoversAtTheChildBoundary; internal/agent/children_test.go — TestInterjectChild_QueuedBeforeAPanicIsReportedFaulted
**Tests:** `TestWorkflowSpawn_ASubAgentItemPanicIsRecoveredOnce`; the existing `TestDelegationPhases_*`, `TestDispatch_SerialDelegationPanicRecoversAtTheChildBoundary`, `TestSubAgent_ChildPanicRecoversAtParentBoundary` and `TestRunDelegation_*`.
**Acceptance:**
- `go build ./...`
- `go test -count=1 -run 'TestDelegationPhases|TestDispatch_SerialDelegationPanic|TestSubAgent_ChildPanic|TestRunDelegation|TestFanOut_ChildPanic|TestWorkflowSpawn_ASubAgent|TestInterjectChild|TestDelegationRefusals' ./internal/agent/`
**Commit:** `refactor(agent): started sub_agent runs go through childBracket`

## 9. Never-started entries on the bracket

Depends on item 8.

**What:**
**Goal:** `preemptDelegation`, `refusePastCeiling`, `settleCancelledDelegation` and `stopQueuedDelegation` each settle through `childBracket.neverStarted`. `skipDelegation`, `recordCeilingRefusal` and the slot's `ceilingRefused` flag no longer exist.

**Approach (assumed at the header base):**
- Each entry sets `slot.run=false; slot.result=b.neverStarted(kind)`.
- The ceiling row is booked at prepare. This is index-equivalent because ceiling slots are always the tail and earlier slots reserve first.
- `slot.delegated = outcome != dispatchCancelled` is kept.

**Regression guard.** A never-started bracket never mints: it keeps the slot's run id, `""` included (a call a pre-tool-exec Reaction redirected into sub_agent), as `skipDelegation` does today. Only the run path (item 8) mints. `TestDispatchSerially_EveryUnstartedKindBooksOneRow` covers skipped, past-ceiling and cancelled; the stopped kind exists only in a pool and stays on `TestStopChild_AQueuedPooledDelegationNeverStarts`. Rule: every comment naming a removed helper — `grep -rn 'skipDelegation\|recordCeilingRefusal\|ceilingRefused' internal/` — is repointed to `childBracket.neverStarted`.
**Files:** internal/agent/dispatch.go; internal/agent/children.go; internal/agent/interject.go; internal/agent/subagent.go; internal/tui/subagentblock.go; internal/tui/transcript.go; internal/tui/subagentblock_test.go; internal/agent/fanout_test.go; internal/agent/stop_test.go
**Read first:** internal/agent/dispatch.go — dispatchGroup, prepareCall, refusePastCeiling, preemptDelegation, settleCancelledDelegation, stopQueuedDelegation, runPooledSlot, skipDelegation; internal/agent/children.go — delegationLedger.open, delegationLedger.reserve; internal/agent/fanout_test.go — TestFanOut_CeilingRefusesTheCallsPastIt, TestDispatchSerially_PreemptedDelegationBooksASkippedLedgerRow, TestFanOut_ASlotDequeuedAfterTheCancelIsNeverStarted; internal/agent/stop_test.go — TestStopChild_AQueuedPooledDelegationNeverStarts
**Tests:** `TestDispatchSerially_EveryUnstartedKindBooksOneRow` (skipped, past-ceiling, cancelled); the existing `TestFanOut_PreemptedDelegationBooksASkippedLedgerRow`, `TestFanOut_CeilingRefusesTheCallsPastIt` (row numbers), `TestFanOut_ASlotDequeuedAfterTheCancelIsNeverStarted` and `TestStopChild_AQueuedPooledDelegationNeverStarts`.
**Acceptance:**
- `go build ./... && go test -count=1 -run 'TestFanOut|TestDispatchSerially|TestStopChild_AQueued' ./internal/agent/`
**Commit:** `refactor(agent): never-started delegations settle through childBracket`

## 10. Workflow Spawn on the bracket

Depends on item 9.

**What:**
**Goal:** `Spawn` in workflowspawn.go recovers and brackets through `childBracket` with a nil ledger. A panicking item finishes with one phase pair, a `faulted: panic:` finished phase (report `panic: <r>`) and `EndFaulted`.

**Approach (assumed at the header base):**
- Open the bracket at mint. Pre-mint refusals return unchanged, with no phase.
- `onPanic` yields today's `faulted: panic: <r>`.

**Regression guard.** `Outcome.Report` stays `panic: <r>` (only the finished phase reads `faulted: panic: <r>`, via `workflowPhaseBody`'s prefix; `exhaustedReceipt` adds its own "the child faulted: "). The finished phase stays `workflowPhaseResult(callID, outcome, err, seatFallback)` on every exit, and the ErrorEvent (`panic: <r>`, Source `s.call.Tool`) stays. The body returns `(outcome, err)`, and Spawn still returns that err unchanged after the bracket closes the pair, so the post-mint `newChildAgentOn`/`Submit` failures keep the Runner's retry.
**Files:** internal/agent/workflowspawn.go; internal/agent/workflowspawn_test.go
**Read first:** internal/agent/workflowspawn.go — workflowSpawner.Spawn, workflowPhaseResult, workflowPhaseBody, undeliveredWorkflowReason, workflowOutcome; internal/agent/childrun.go — runChild, reapChild; internal/workflow/runner.go — exhaustedReceipt, Spawner; internal/agent/workflowspawn_test.go — newWorkflowParent, spawnItem, TestWorkflowSpawn_ASpawnRefusedBeforeItsRunReportsNoStart; internal/agent/fanout_test.go — routedResponder.route (panic gate); internal/agent/dispatch_test.go — TestDispatch_SerialDelegationPanicRecoversAtTheChildBoundary
**Tests:** `TestWorkflowSpawn_APanicFinishesFaulted` (pair, report `panic: <r>`, finished phase `faulted: panic: <r>`, ErrorEvent, Ending, undelivered reason); the existing `TestWorkflowSpawn_ASpawnRefusedBeforeItsRunReportsNoStart`.
**Acceptance:**
- `go build ./... && go test -count=1 -run 'TestWorkflowSpawn|TestUndelivered|TestChildBracket' ./internal/agent/`
**Commit:** `refactor(agent): workflow Spawn brackets its child through childBracket`

## 11. The delegation request parser

Depends on item 10.

**What:**
**Goal:** `parseDelegationRequest(call, offer)` in `internal/agent/delegationrequest.go` is the one decoder of `tools.SubAgentArgs` in `internal/agent`, and the pre-slot readers use it.

**Approach (assumed at the header base):**
- `type delegationRequest struct{ args tools.SubAgentArgs; decodeErr error; seat delegationSeat; seatErr error; background bool }`
- Decode MUST target `tools.SubAgentArgs`, because the error text names the type.
- `background` is the lenient read (today's `asksBackground`) `&& offer.background && call.Malformed==nil`.
- Methods: `blocking()`, `askedSeat()`, `stepCap(configured)`, `label(name, callID)`, and the separate decode, empty-task and seat checks (see the guard).
- `askedSeat`, `stepCapFor`, `asksBackground` and `delegationLabel` fold into it. `partitionDispatch`, the width functions and run-id minting call it on the raw calls.

**Regression guard.** One `argumentsRefusal()` cannot keep either path's order (blocking answers an unknown `continue` before the seat; background answers continue+background before the empty task), so expose the decode, empty-task and seat checks separately (`decodeErr`, an empty-task check, `seatErr`) and call each at its own site in today's order; item 1 pins both inputs. `parseDelegationRequest` zeroes `args` when `decodeErr != nil`, since `json.Unmarshal` fills fields past a type error and today's label, step cap and seat read nothing then. `asksBackground` stays the shared lenient reader: `request.background` calls it, and fan_out keeps its own call. `spawnSubAgent` also calls `stepCapFor`, so workflowspawn.go is in Files.
**Files:** internal/agent/delegationrequest.go; internal/agent/delegationrequest_test.go; internal/agent/dispatch.go; internal/agent/workflowcall.go; internal/agent/subagent.go; internal/agent/workflowspawn.go
**Read first:** internal/agent/workflowcall.go — asksBackground, isBackgroundSubAgentCall, isDelegationCall, backgroundSubAgentResult, workflowCallResult; internal/agent/subagent.go — runDelegate, delegationLabel; internal/agent/dispatch.go — askedSeat, stepCapFor, partitionDispatch, seatsAreSplit, allAskedSession, prepareCall; internal/agent/delegationwidth.go — fanOutWidthFor; internal/agent/workflowspawn.go — spawnSubAgent; internal/tools/sub_agent.go — SubAgentArgs
**Tests:**
- `TestParseDelegationRequest_DecodeErrorTextNamesSubAgentArgs`
- `TestParseDelegationRequest_SeatReadOnlyWhereOffered`
- `TestParseDelegationRequest_BackgroundIsLenient`
- `TestParseDelegationRequest_MalformedIsNeverADelegation`
- `TestParseDelegationRequest_LabelFallsBackToTaskThenCallID`, including `{"task":"do x","max_steps":"ten"}` → `call <id>`
**Acceptance:**
- `go build ./...`
- `go test -count=1 -run 'TestParseDelegationRequest|TestPartitionDispatch|TestFanOutWidth|TestSeat_|TestDelegationRefusals|TestDispatchGroup' ./internal/agent/`
**Commit:** `refactor(agent): one delegationRequest parser for sub_agent arguments`

## 12. Parse once per dispatch slot

Depends on item 11.

**What:**
**Goal:** `dispatchSlot.request` is parsed in `prepareCall` AFTER the pre-tool-exec Moment. Every post-prepare reader takes it from the slot, and no non-test file outside delegationrequest.go decodes `tools.SubAgentArgs`.

**Approach (assumed at the header base):**
- These readers take the slot's request: the never-started entries, `resolutionInput`, `runDelegation` → `runDelegate(ctx, call, req, …)` and `runWorkflowCall` → `backgroundSubAgentResult(ctx, turn, call, req)` / `backgroundSubAgentPlan`.
- `runDelegate` copies `req.args` before the continuation mutates them. The ledger label keeps the call's own task.
- `spawnSubAgent` and the `runSubAgent` test wrapper parse their own call.
- Never route `spawnSubAgent` or `runDelegate` on `req.background`, because the stored plan carries `"background":true`.

**Regression guard.** TestPrepareCall_ParsesTheRequestAfterThePreToolExecMoment passes at base by design. It pins the post-Moment parse against a future pre-Moment regression and is not a bite test.
`TestRunDelegation_ACallTheCancelReachedBeforeItsChildIsNeverStarted` builds its `dispatchSlot` by hand: set `request: parseDelegationRequest(call, a.delegationOffer())` there, or `runDelegate` sees an empty task and refuses instead of cancelling. Keep `resolutionInput`'s signature (its other callers are `runScriptCall` and the setconfine/setmode tests); `prepareCall` sets `input.backgroundSubAgent` from `slot.request` after the call, as `runScriptCall` sets `workflowScriptDir`.
**Files:** internal/agent/dispatch.go; internal/agent/subagent.go; internal/agent/workflowcall.go; internal/agent/workflowspawn.go; internal/agent/dispatch_test.go; internal/agent/subagent_test.go
**Read first:** internal/agent/dispatch.go — prepareCall, dispatchSlot, runCall, runDelegation, resolutionInput, preemptDelegation, refusePastCeiling, settleCancelledDelegation, stopQueuedDelegation; internal/agent/subagent.go — runDelegate, runSubAgent; internal/agent/workflowcall.go — runWorkflowCall, workflowCallResult, backgroundSubAgentResult, backgroundSubAgentPlan; internal/agent/recipe.go — runScriptCall; internal/agent/subagent_test.go — TestRunDelegation_ACallTheCancelReachedBeforeItsChildIsNeverStarted; internal/domain/tooledit.go — ToolCallEdit.SetArguments
**Closes:** delegation-args-reparsed
**Tests:** `TestPrepareCall_ParsesTheRequestAfterThePreToolExecMoment`: a reaction rewrites `task`, and both the ledger label and the child see the edit (a pin, green at base); the fixed `TestRunDelegation_ACallTheCancelReachedBeforeItsChildIsNeverStarted`.
**Acceptance:**
- `test -z "$(grep -n 'tools.SubAgentArgs' internal/agent/*.go | grep -v _test | grep -v delegationrequest.go | grep -v '^internal/agent/agent.go:.*//')"`
- `go test -count=1 -run 'TestDelegationRefusals|TestPrepareCall_|TestWorkflowCall_ABackground|TestSubAgent_RejectsEmpty|TestBackground_|TestSubAgentContext|TestRunDelegation' ./internal/agent/`
**Commit:** `refactor(agent): a delegation request is parsed once per dispatch slot`

## 13. Record the bracket in the decision records and glossary

Depends on item 12.

**What:**
**Goal:** Each knowledge Note names `childBracket` as the recover boundary and the single place a delegation books its ledger row, and none names the retired hand-bracket sites.

**Approach (assumed at the header base):**
- Add a dated `> **Note 2026-MM-DD**` under ADR 0083 part 3: the bracket moved, and the runtime state stays flat.
- Add `adr-0039-amend-MMDD` (`capcom know add --id adr-0039-amend-MMDD --parent adr-0039`), naming `childBracket` as D4's recover boundary. List it on the record's Status line and in its reading-order list.
- Update the glossary Sub-agent ledger text.
- Rule: every Note naming `skipDelegation`, `recordCeilingRefusal`, `classifyDelegation`, `withoutSeatChoice` or `publishesSeatChoice` as current is corrected. Find them with `grep -rlnE 'skipDelegation|recordCeilingRefusal|classifyDelegation|withoutSeatChoice|publishesSeatChoice' .capcom/knowledge/`. Historical records stay as written.

**Regression guard.** Item 13 also owns the public wording of `refused`: widen it to "the child refused it at the boundary, or never ran" in docs/manual/headless.md, layout.md and the glossary interjection Note (.capcom/knowledge/glossary/…/interjection.md). This explicitly supersedes the headless.md wording that `refused` means "the child, still running, refused it there" (owner call 2026-11-21, never-ran → refused). Rule: every place that spells the undelivered reasons, found with grep -rn 'could not take your message\|`refused`' docs layout.md .capcom/knowledge/glossary; historical records stay as written. Add those paths to Files.
Append "part 3 noted <date> (childBracket)" to adr-0083.md's Status line, which already names part 4's note: an amendment the record does not name is never read. The Goal is checked by an Acceptance grep over the three Notes.
**Files:** .capcom/knowledge/decisions/adr-0083/ (part 3 Note); .capcom/knowledge/decisions/adr-0083.md (Status line); .capcom/knowledge/decisions/adr-0039.md and its new amend Note; .capcom/knowledge/glossary/ (the Sub-agent Note); .capcom/knowledge/glossary/glossary-turns/interjection.md; docs/manual/headless.md; layout.md
**Read first:** .capcom/knowledge/decisions/adr-0083/adr-0083-part-3.md — 2026-10-01 note; .capcom/knowledge/decisions/adr-0083.md — Status line; .capcom/knowledge/decisions/adr-0039.md — Status line, reading order; .capcom/knowledge/decisions/adr-0039/adr-0039-decision.md — decision 4 (panic recovery at its own boundary); .capcom/knowledge/glossary/glossary-identity/sub-agent.md — delegate ledger paragraph; .capcom/knowledge/glossary/glossary-identity/fan-out-ceiling.md — refused row
**Closes:** delegation-endings-hand-bracketed
**Tests:** none (docs only).
**Acceptance:**
- `capcom check`
- `capcom know show "ADR 0039" | grep -q amend`
- `test "$(grep -l childBracket .capcom/knowledge/decisions/adr-0083/adr-0083-part-3.md .capcom/knowledge/decisions/adr-0039/adr-0039-amend-*.md .capcom/knowledge/glossary/glossary-identity/sub-agent.md | wc -l)" = 3`
**Commit:** `docs(knowledge): record childBracket in ADR 0039, ADR 0083 and the glossary`
