# Sub-agent wave and background sub_agent: leftovers

**Goal:** Close the five beads left by the subagent-wave-and-background run: a background sub_agent child survives an aborted root Exchange, every background sub_agent call starts a fresh run, the unreadable `run_on` refusal is pinned, a serial wave shows the same queued and esc hints as a pooled one, and a background sub_agent's finish line names its origin.
**Date:** 2026-10-05
**Status:** done (status corrected 2026-10-07)
**sized for:** ~200k-context host
**base:** 41ad57ff

**Sources:**
- `docs/handoffs/2026-10-05 - 00 - subagent-wave-and-background-leftovers.md`
- `bd show` for each bead on a **Closes:** line
- `docs/adr/0094-sub-agent-may-run-as-a-one-item-background-workflow.md` (D3, D7, D8)
- `docs/adr/0025-interjections-commit-at-the-between-steps-boundary.md` (Amended 2026-10-05), `docs/adr/0031-…`, `docs/adr/0039-…`, `docs/adr/0075-…`
- `docs/plans/archived/2026-10-05 - 00 - subagent-wave-and-background-plan.md`

**Ratified design calls (owner, 2026-10-05):**
- **Call-id salt:** the background sub_agent plan is salted with the call id plus a freshly minted run id (a nonce), so every call starts a new folder; ADR 0094 D8 is amended.
- **Serial hint:** a count-only `domain.SubAgentGroupEvent{Size, Width}` from the engine; `eventjson` skips it (no NDJSON line kind); no scheduled rows for serial members.
- **Finish line:** a background sub_agent reads `background sub_agent <name> <finished|stopped> — <tally>` and keeps the tally; its failed line also says `background sub_agent`; fan_out and recipe lines are unchanged.
- **Doc points:** included as one docs-only item.

**Closes:** see each item.

**Standing requirements:**
- skills: coding-standards
- Bubble Tea v2 names only (`tea.KeyPressMsg`, `msg.String()`).
- Run one package per `go test` line; never `./...` (AGENTS.md).
- Closeout gate on this machine: `APOGEE_TEST_SHARDS=1 APOGEE_TEST_SLOW=1 make check`.
- Deviations from item text land as a dated NOTES line under the item.

**Out of scope:**
- Scheduled rows for serial wave members.
- An NDJSON line for the group event.
- Background sub_agent for headless or daemon (ADR 0094 D7).
- Pushing `main`; any `VERSION` change.
- sub_agent wording for a workflow question (no production producer).

**Regression check (2026-10-05, 41ad57ff):**
- 1: guard folded
- 3: guard folded
- 4: guard folded
- 5: guard folded
- 7: recast; guard folded; supersedes `layout.md:1735` and `CHANGELOG.md:30` ([Unreleased]) on the sub_agent finish line
- 8: guard folded
- re-check of 7 (recast): SAFE

## 1. An aborted Exchange keeps a retained background sub_agent child — ✅ DONE (2026-10-05)

NOTES (2026-10-05): added `takePastExchange` beside `retainPastExchange`, so a background continuation that consumes an entry also drops it from the Exchange-start copy; without it, an abort would bring back a consumed entry under its old name next to the continued one. Both are routed through new `delegateSite.retain`/`take` methods on an `isPastExchange` flag, set from `workflowSpawner.isBackground` (set in `wireLaunch`'s background branch).
NOTES (2026-10-05): added `TestBackground_ABlockingWorkflowChildIsRolledBackWithAnAbortedExchange` as the counter-case pinning the goal's blocking half for a workflow item, in addition to the root-call `TestAbortExchange_RestoresRetentionToTheExchangeStart`.
NOTES (2026-10-05): consequential edit — internal/agent/loop.go: made necessary by the write-through (the markExchange call-site comment said every change to the set rides a Turn the abort drops).

**What:**
**Goal:** A background sub_agent child that is retained on the root while the root's own Exchange is open is still retained (and continuable by name) after that Exchange aborts; a blocking delegation retained inside an aborted Exchange is still rolled back.
**Approach (assumed at the header base):** fixes the bug from apogee-background-child-exchange-rollback (retention added in `4afed139`). `runDelegate` (`internal/agent/subagent.go`) calls `site.retained.retain`, which writes only `byName`; `retainedDelegates.rollBackExchange` (`internal/agent/children.go`) restores `byName` from the `atExchange` snapshot taken by `markExchange`, so the background entry is lost. Add a write-through method on `retainedDelegates` (e.g. `retainPastExchange`) that writes both `byName` and `atExchange` under the same lock, allocating `atExchange` when nil. Route to it only for background launches: a flag on `delegateSite`, set by the workflow spawner when the launch mode is background (`wireLaunch`, `internal/agent/launch.go`; `workflowSpawner.spawnSubAgent`, `internal/agent/workflowspawn.go`). Blocking workflows and the root's own `sub_agent` calls keep `retain`.
**Regression guard.** Update every comment that describes `atExchange`/`markExchange`/`rollBackExchange` as the Exchange-start set or an abort as undoing all retention (`grep -n 'atExchange\|markExchange\|rollBackExchange\|as its Exchange opened' internal/agent/*.go` — today `children.go:271-289`, `:353-366`, `construct.go:202`, `agent.go:436`) to name the background write-through exception.
**Files:** internal/agent/children.go; internal/agent/subagent.go; internal/agent/workflowspawn.go; internal/agent/launch.go; internal/agent/construct.go; internal/agent/agent.go; internal/agent/children_test.go; internal/agent/background_test.go
**Read first:** internal/agent/children.go — retain, markExchange, rollBackExchange; internal/agent/subagent.go — runDelegate, delegateSite;
internal/agent/workflowspawn.go — spawnSubAgent; internal/agent/launch.go — wireLaunch;
internal/agent/background_test.go — TestBackground_ANamedSubAgentChildIsRetainedOnTheRootAndContinued
**Tests:** write the failing test first: beside `TestBackground_ANamedSubAgentChildIsRetainedOnTheRootAndContinued` (`background_test.go`), call `a.retained.markExchange()` before the background run, then `a.exchangeAborted()`, and assert `a.retained.names()` still holds the child; a unit case in `children_test.go` for the write-through method; `TestAbortExchange_RestoresRetentionToTheExchangeStart` keeps passing.
**Acceptance:**
- `go build ./...`
- `go test -race -count=1 -run 'TestBackground_|TestRetainedDelegates|TestAbortExchange|TestResume_MidExchange' ./internal/agent/`
**Commit:** `fix(agent): keep a retained background sub_agent child across an aborted Exchange`
**Closes:** apogee-background-child-exchange-rollback

## 2. Pin the unreadable run_on refusal of a background sub_agent — ✅ DONE (2026-10-05)

NOTES (2026-10-05): the per-row config comes from a new helper `backgroundSubAgentConfigWith(t, sink, seatChoice)`, which `backgroundSubAgentConfig` now delegates to with `false`; its other eight callers and the existing rows' registry are unchanged. The table's rows switched to keyed fields so the new `seatChoice` field need not be spelled `false` on the three existing rows.
NOTES (2026-10-05): mutation check — flipping the new row to `seatChoice: false` makes it fail (the call starts a background workflow), so the row pins the seat-choice gate as well as the refusal text.

**What:**
Test-only. A background `sub_agent` call with an unreadable `run_on` (e.g. `"gpu"`) is refused with `invalid run_on "gpu": want "session" or "sub-agents-server"` before any workflow folder exists (`backgroundSubAgentResult`, `internal/agent/workflowcall.go`; `parseDelegationSeat`, `internal/agent/subagent.go`). The check runs only when the tool publishes seat choice, and `backgroundSubAgentConfig` (`workflowcall_test.go`) registers `SubAgentOptions{Background: true}` without `SeatChoice`. Add a `seatChoice bool` field to the table of `TestWorkflowCall_ABackgroundSubAgentIsRefusedBeforeItStartsAWorkflow` and build the config per row, so the existing rows keep their registry unchanged; the new row asserts the refusal text and `len(a.Workflows()) == 0`.
**Files:** internal/agent/workflowcall_test.go
**Read first:** internal/agent/workflowcall_test.go — TestWorkflowCall_ABackgroundSubAgentIsRefusedBeforeItStartsAWorkflow, backgroundSubAgentConfig, backgroundSubAgentArgsJSON, callResult;
internal/agent/workflowcall.go — backgroundSubAgentResult; internal/agent/subagent.go — parseDelegationSeat, publishesSeatChoice;
internal/tools/sub_agent.go — SubAgentOptions
**Tests:** the new table row; the existing rows unchanged.
**Acceptance:**
- `go test -race -count=1 -run 'TestWorkflowCall_ABackgroundSubAgent|TestOffersBackgroundSubAgent' ./internal/agent/`
**Commit:** `test(agent): pin a background sub_agent's unreadable run_on refusal`
**Closes:** apogee-background-run-on-refusal-untested

## 3. Every background sub_agent call starts a fresh run — ✅ DONE (2026-10-05)

**What:**
**Goal:** Two background `sub_agent` calls with the same call id and identical arguments, in two Exchanges, start two workflow runs and run two children; crash resume and `/workflows` rerun of a background sub_agent run still resume that run's own folder; ADR 0094 D8 states the nonce.
**Approach (assumed at the header base):** fixes the bug from apogee-reused-call-id-resumes-run. `backgroundSubAgentPlan` (`internal/agent/workflowcall.go`) salts the stage Task with `call.ID` only, and `Runner.openFolder` (`internal/workflow/runner.go`) resumes the newest folder whose `PlanHash` matches. Salt with `call.ID` plus a value from `a.runIDs.mint()`, so the hash is unique per call. Resume paths read the plan back from the folder (`folderLaunch`, `store.ReadPlan`) and are unaffected. Amend D8 in ADR 0094 with an `Amended 2026-10-05` note: the salt is the call id plus a minted nonce, because upstream ids are not guaranteed unique.
**Regression guard.** in the new test keep the "please delegate" route registered before subAgentTask in workflowResponder, because the second Exchange's opening carries the first run's finish note labelled with subAgentTask and routing is by substring in registration order
**Files:** internal/agent/workflowcall.go; internal/agent/workflowcall_test.go; docs/adr/0094-sub-agent-may-run-as-a-one-item-background-workflow.md
**Read first:** internal/agent/workflowcall.go — backgroundSubAgentPlan, backgroundSubAgentResult; internal/workflow/runner.go — Runner.openFolder;
internal/agent/background.go — folderLaunch; internal/agent/agent.go — runIDMinter.mint; internal/agent/workflowcall_test.go — TestWorkflowCall_TwoIdenticalBackgroundSubAgentsBothRun, workflowResponder.route;
docs/adr/0094-sub-agent-may-run-as-a-one-item-background-workflow.md — D8
**Tests:** a new test beside `TestWorkflowCall_TwoIdenticalBackgroundSubAgentsBothRun` issuing call id `sa1` twice in two Exchanges, asserting two folders and that the child responder was asked twice, with the `"please delegate"` route registered before `subAgentTask` (Regression guard); it must fail against the pre-item tree.
**Acceptance:**
- `go test -race -count=1 -run 'TestWorkflowCall_|TestBackground_|TestResume' ./internal/agent/`
- `grep -n "Amended 2026-10-05" docs/adr/0094-sub-agent-may-run-as-a-one-item-background-workflow.md`
**Commit:** `fix(agent): salt a background sub_agent plan with a nonce beside the call id`
**Closes:** apogee-reused-call-id-resumes-run

## 4. The engine announces a sub-agent group's size — ✅ DONE (2026-10-05)

NOTES (2026-10-05): the emission sits in a small helper, `Agent.announceSubAgentGroup`, which `dispatchTools` calls between the leaf group and the delegations' `dispatchGroup`, passing the very width that `dispatchGroup` then receives. `encode.go` skips the event the way it skips `WireEvent`: through the switch's `default` arm, with no explicit case. The doc comment says so.
NOTES (2026-10-05): the doc comment on `TestEncodeSkipsTheWireEvent` (`encode_test.go`) called WireEvent "the one variant the lines never carry". It now reads "a variant the lines never carry", which is one of the prose sites the item says to reword.

**What:**
**Goal:** Before the first delegation of a reply's group of two or more `sub_agent` calls is prepared, the engine emits one `domain.SubAgentGroupEvent` carrying the group's size (capped at the fan-out ceiling) and its run width, at every width; `eventjson` writes no line for it; ADR 0025's 2026-10-05 amendment names the event as the Driver's source for the queued count.
**Approach (assumed at the header base):** the engine already holds the whole delegation list in `Agent.dispatchGroup` (`internal/agent/dispatch.go`, fed by `partitionDispatch`). Define `SubAgentGroupEvent{EventBase; Size, Width int}` in `internal/domain/events.go` with a doc block like its neighbours, and re-export it from the root facade (`apogee.go`) the way the other events are. Emit it from `dispatchTools`, between the leaf group and the delegations' `dispatchGroup` call, with `Size` and `Width` as the Regression guard sets them. It is an in-process event to the Driver's sink, not wire output, so ADR 0031's wire-silent engine holds. In `internal/eventjson/encode.go` skip it the way `WireEvent` is skipped; the kind count stays unchanged.
**Regression guard.** Emit in `dispatchTools` (`dispatch.go:81`), never in `dispatchGroup` (it also runs the leaf group), only when `len(delegations) >= 2`, with `Width` = the `fanOutWidthFor` result passed to that call; `Size` = `len(delegations)`, capped at `a.fanOutCeiling()` only when that is > 0 (the rounds × `statedDelegationWidth` inputs `refusePastCeiling` uses — 0 means no ceiling). Add an inert `foldCases` row (`wantEntries` 0) in `internal/tui/fold_test.go` so `TestFoldEventCoversEveryEventVariant` stays green at this commit.
Reword every prose site claiming `WireEvent` is the only unwritten event (`grep -rn 'one SINK-ONLY\|one thing never written' internal docs` — `encode.go:74`, `docs/manual/headless.md:260`).
**Files:** internal/domain/events.go; apogee.go; internal/agent/dispatch.go; internal/agent/fanout_test.go; internal/eventjson/encode.go; internal/eventjson/encode_test.go; internal/tui/fold_test.go; docs/manual/headless.md; docs/adr/0025-interjections-commit-at-the-between-steps-boundary.md
**Read first:** internal/agent/dispatch.go — dispatchTools, partitionDispatch, dispatchGroup, refusePastCeiling; internal/agent/delegationwidth.go — fanOutWidthFor, fanOutCeiling;
internal/eventjson/encode_test.go — TestEncodeSkipsTheWireEvent; internal/tui/fold_test.go — foldCases
**Tests:** beside `TestFanOut_CapOneKeepsTheGroupSerial`: the event fires once, before the first `ToolCallEvent`, at width 1 and at a pool width, with `Size` capped at the ceiling (and uncapped when `delegate-fanout-rounds` is 0); none for a single delegation or a leaf-only reply; an eventjson case beside `TestEncodeSkipsTheWireEvent`; the inert `foldCases` row.
**Acceptance:**
- `go build ./...`
- `go test -race -count=1 -run 'TestFanOut|TestDispatchSerially' ./internal/agent/`
- `go test -race -count=1 ./internal/eventjson/`
- `go test -race -count=1 -run 'TestFoldEvent' ./internal/tui/`
- `go test -count=1 -run TestEveryDomainEventVariantIsAliased .`
**Commit:** `feat(agent): announce a sub-agent group's size to the Driver`

## 5. A serial wave shows the queued and esc hints — ✅ DONE (2026-10-05)

NOTES (2026-10-05): an announced group is over only once every announced member is drawn AND the last of them is paired, not once any member is paired — the guard states the "short of its size" half; the other half keeps a serial group running its LAST member after earlier ones finished in flight, so its esc hint reads "keeps N finished …" as a pooled group's does (test case "a serial group running its last member keeps the finished ones"). A group no announcement anchors keeps the old any-member-paired rule unchanged.
NOTES (2026-10-05): "drop it on a new Turn" is implemented as: a depth-0 ToolCallEvent whose Turn differs from the announcing Turn drops the wave (transcript.anchorWave); "a different group heads" is answered at read time — the wave counts only when its anchor head is in the group of the most recent depth-0 head (transcript.announcedMembers), and members are counted from the anchor onward. The anchor is the head's call id plus spawned run id.
NOTES (2026-10-05): the delegate-group and next-Turn tests are new functions in model_test.go named TestFanOutCountIgnoresADelegatesGroup and TestFanOutCountDropsAnAnnouncedGroupOnTheNextTurn, so the acceptance `-run 'TestFanOut'` pattern picks them up.
NOTES (2026-10-05): consequential edit — layout.md: made necessary by the serial readout; its queued-readout paragraph said the hint shows only for "a pooled group, the one whose queued members have rows".
NOTES (2026-10-05): the waveReadout constant's comment in cmd/apogee/e2e_subagent_preempt_test.go said only the pooled journey could show the hint, and the escStopHint* constant comments in model.go said "pooled fan-out". Both were reworded alongside the item's own edits to those files.

**What:**
**Goal:** With `parallel-agents: 1`, a message staged while a sub_agent group still has members to run shows `after the wave · ctrl+g sends now` in the queued readout, and an armed esc names what a second esc does, the same as in a pooled group; a finished serial member counts toward the esc hint's finished count.
**Approach (assumed at the header base):** fixes the bug from apogee-serial-wave-hint-hidden (item 5 of the archived plan, `a8374732`). `transcript.inFlightFanOut` (`internal/tui/transcript.go`) counts only rows already drawn and reports "over" once any member is done, so at width 1 it never reports queued members. Fold `domain.SubAgentGroupEvent` (item 4) in a case of `transcript.apply` (`internal/tui/transcript.go`) as non-entry transcript state; `inFlightFanOut` then computes queued as the Regression guard sets it, and an announced group still short of its size is not "over" because one member is done. `Model.waitsForWave`/`queuedSegment` (`interject.go`) and `Model.escStopHint` (`model.go`) read it unchanged; correct `escStopHint`'s width-1 doc comment.
**Regression guard.** Fold only a `Depth == 0` event (delegates share the parent's sink) in `transcript.apply`, never `transcriptbridge.go` (the persistence codec), and anchor it to the first depth-0 sub_agent head placed after it — drop it when a different group heads, on a new Turn and on /clear. queued = drawn heads with no phase + max(0, Size − drawn members), keeping the `neverStarted` exclusion (a pooled group draws every member before any runs).
Lift the floor of two only inside `inFlightFanOut` for an announced group, never in `ownGroup`/`subAgentGroupAt` (the painter's `✦ Sub-Agent (N)` header reads them); update item 4's `foldCases` row to what the fold now does.
**Files:** internal/tui/transcript.go; internal/tui/model.go; internal/tui/fold_test.go; internal/tui/interject_test.go; internal/tui/model_test.go; cmd/apogee/e2e_subagent_preempt_test.go
**Read first:** internal/tui/transcript.go — inFlightFanOut, subAgentGroupAt, ownGroup, transcript.apply; internal/tui/interject.go — Model.queuedSegment;
internal/tui/model.go — Model.escStopHint; internal/tui/model_test.go — fanOutOf; cmd/apogee/e2e_subagent_preempt_test.go — TestE2EQueuedMessageWaitsForTheWholeWave
**Tests:** a serial case in `TestInterjectQueuedReadoutNamesTheWave` (group event, member 1 done, no member 2 row) and in `TestEscStopHintNamesWhatASecondEscDoes`, plus a "member 1 running, no member 2 row" case in both; a depth-1 group event leaves the count unchanged; a lone delegation after a finished announced group keeps the plain hint; the `foldCases` row updated; `TestEscStopHintIgnoresDelegationsThatNeverStarted` and `TestE2EQueuedMessageWaitsForAPooledWaveAndSaysSo` keep passing; `TestE2EQueuedMessageWaitsForTheWholeWave` waits for `after the wave · ctrl+g sends now` after the ⏎ send.
**Acceptance:**
- `go test -race -count=1 -run 'TestInterject|TestEscStopHint|TestFanOut|TestFoldEvent' ./internal/tui/`
- `go test -race -count=1 -run TestE2EQueuedMessage ./cmd/apogee/`
**Depends on item 4.**
**Commit:** `fix(tui): show the wave hints for a serial sub-agent group`
**Closes:** apogee-serial-wave-hint-hidden

## 6. Workflow phase events carry the run's origin — ✅ DONE (2026-10-05)

NOTES (2026-10-05): no CHANGELOG entry — the field is read in process only and changes nothing a user sees until item 7's finish line reads it; the NDJSON workflow_phase line is unchanged.

**What:**
**Goal:** Every `domain.WorkflowPhaseEvent` of a background sub_agent run carries `Origin == "sub_agent"` (`workflow.OriginSubAgent`); fan_out and recipe runs carry `""`; the NDJSON line for the event is unchanged.
**Approach (assumed at the header base):** add `Origin string` to `WorkflowPhaseEvent` (`internal/domain/events.go`), documented as `workflow.RunStatus.Origin` is (what launched the run when that is not a recipe or a fan_out) and distinguished from the `domain.Origin` reaction type in its doc. `observeWorkflow` (`internal/agent/workflowcall.go`) sets `origin: workflow.OriginOf(plan)` on the `workflowObserver`, and `emitLocked` stamps it beside `Workflow`, `Name`, `Background`, `Call`. Add `Origin` to the list of fields deliberately off the line in `internal/eventjson/encode.go`'s `workflowPhaseData` comment.
**Files:** internal/domain/events.go; internal/agent/workflowcall.go; internal/agent/workflowcall_test.go; internal/eventjson/encode.go
**Read first:** internal/agent/workflowcall.go — workflowObserver, observeWorkflow, emitLocked; internal/workflow/store.go — OriginOf, OriginSubAgent; internal/domain/events.go — WorkflowPhaseEvent;
internal/eventjson/encode.go — the "per-variant data values" comment block above tokenData (the off-line field list lives there, not on workflowPhaseData);
internal/agent/workflowcall_test.go — TestWorkflowCall_EmitsItsPhases
**Tests:** a background sub_agent run's phase events carry `sub_agent`; a fan_out run's carry `""`.
**Acceptance:**
- `go build ./...`
- `go test -race -count=1 -run 'TestWorkflowCall_|TestBackground_' ./internal/agent/`
- `go test -race -count=1 ./internal/eventjson/`
**Depends on item 4** (same files).
**Commit:** `feat(agent): stamp a workflow phase event with its run's origin`

## 7. A background sub_agent's finish line names it — ✅ DONE (2026-10-05)

NOTES (2026-10-05): the CHANGELOG text above REPLACES the existing [Unreleased] entry "**The TUI shows a background `sub_agent`**" (CHANGELOG.md:30) in place — amend that entry, do not add a second one (plan item 7: supersedes CHANGELOG.md:30).
NOTES (2026-10-05): backgroundWorkflows.view now takes the phase event (was view(id, name)) so a view opened without a folded started phase takes its origin from the event, as it already took its name; both callers are in foldBackgroundPhase.

**What:**
Recast at the regression check (2026-10-05).
**Goal:** When a background sub_agent run finishes or stops, the transcript reads `background sub_agent <name> <finished|stopped> — <tally>`; a failed run reads `background sub_agent <name> failed — <reason>`; fan_out and recipe runs keep `background workflow …`.
**Approach (assumed at the header base):** `backgroundWorkflow` (`internal/tui/workflow.go`) keeps only `name`; record the started event's `Origin` (item 6) on it and pick the `background sub_agent` formats beside `backgroundFinishFormat` and `backgroundFailedFormat` when it is `workflow.OriginSubAgent`. Flip `bgSubAgentFinishLine` in `cmd/apogee/e2e_background_subagent_test.go`; `bgFinishLine` in `workflow_test.go` (a fan_out) stays.
**Regression guard.** drop the `background sub_agent <name> asks:` wording from the Goal, Approach and Tests — a one-item sub_agent workflow has no ask stage, so no production path reaches it; workflowQuestionFormat stays unchanged; amend the header's "Finish line" ratified call to name only the finished/stopped and failed lines, and add "sub_agent wording for a workflow question (no production producer)" to Out of scope
Supersedes `layout.md:1735` and `CHANGELOG.md:30` ([Unreleased]), which record `background workflow <name> finished` as the sub_agent finish line: restate layout.md:1735 (line 1725's `asks:` sentence stays), and have the sidecar amend CHANGELOG.md:30's wording rather than add a second, conflicting entry. Flip `TestBackgroundSubAgent_FinishLineNamesTheDelegation` (`workflow_test.go`): set `Origin = workflow.OriginSubAgent` on its started event and want `background sub_agent scout finished — …`.
**Files:** internal/tui/workflow.go; internal/tui/workflow_test.go; cmd/apogee/e2e_background_subagent_test.go; layout.md
**Read first:** internal/tui/workflow.go — backgroundWorkflow, finishLine, foldBackgroundPhase, backgroundFinishFormat, backgroundFailedFormat;
internal/tui/workflow_test.go — TestBackgroundSubAgent_FinishLineNamesTheDelegation; cmd/apogee/e2e_background_subagent_test.go — bgSubAgentFinishLine;
layout.md — the background `sub_agent` paragraph (ADR 0094)
**Tests:** sub_agent-origin finish, stop and fail cases beside `TestBackgroundWorkflow_FinishWhileIdleWakesTheAgent`; `TestBackgroundSubAgent_FinishLineNamesTheDelegation` flipped to the new line; fan_out cases unchanged; `TestE2EBackgroundSubAgent` reads the new line.
**Acceptance:**
- `go test -race -count=1 -run 'TestBackgroundWorkflow|TestBackgroundSubAgent|TestWorkflow' ./internal/tui/`
- `go test -race -count=1 -run TestE2EBackgroundSubAgent ./cmd/apogee/`
**Depends on item 6.**
**Commit:** `feat(tui): word a background sub_agent's finish line by its origin`
**Closes:** apogee-finish-line-origin-wording

## 8. Manual and CONTEXT name the one-item background workflow — ✅ DONE (2026-10-05)

NOTES (2026-10-05): configuration.md's "Workflow retries" paragraph was rewrapped (~91 columns) around the added sentence; wording outside the new sentence is unchanged. The prose guard's second hit, workflows.md:397 ("starting the same workflow blocking …"), already says a background `sub_agent` never resumes (item 3), so it was left as is.

**What:**
**Goal:** `docs/manual/configuration.md`'s "Workflow retries" section says a background `sub_agent`'s one item runs once and takes neither second chance; `docs/manual/workflows.md` says starting the same `fan_out` or recipe again skips done items while a background `sub_agent` call always starts a fresh run; CONTEXT.md's skip tool-result line is wrapped like its neighbours (~100 columns).
**Approach (assumed at the header base):** after "…runs one sub-agent per item." in configuration.md add a sentence linking `workflows.md`'s background sub_agent section (ADR 0094 D2). Restate workflows.md's "starting the same workflow again skips the ones already done" (ADR 0094 D8). Rewrap the CONTEXT.md line holding `sub-agent not started: the user sent a message while this group was running`, without changing the quoted string. Prose guard: every manual sentence claiming a re-started workflow resumes (`grep -rn "skips the ones already done\|starting the same workflow" docs/manual`) is restated.
**Regression guard.** Run the docs drift tests one package per line (AGENTS.md): `go test -count=1 -run 'Docs|Manual|Readme' ./cmd/apogee/` and `go test -count=1 -run 'Manual|Readme' ./internal/tools/`, never both packages on one line.
**Files:** docs/manual/configuration.md; docs/manual/workflows.md; CONTEXT.md
**Read first:** docs/manual/configuration.md — "## Workflow retries"; docs/manual/workflows.md — the intro's "starting the same workflow again skips the ones already done", "### A background `sub_agent`";
CONTEXT.md — the line holding `sub-agent not started: the user sent a message while this group was running` (179 cols today);
cmd/apogee/docs_settings_test.go — TestManualDocumentsEverySettingsKey; internal/tools/manual_drift_test.go — manualPath
**Tests:** none (docs).
**Acceptance:**
- `grep -n "background .sub_agent" docs/manual/configuration.md`
- `grep -n "fresh run" docs/manual/workflows.md`
- `awk 'length > 110 && /sub-agent not started/' CONTEXT.md | wc -l` prints `0`
- `go test -count=1 -run 'Docs|Manual|Readme' ./cmd/apogee/`
- `go test -count=1 -run 'Manual|Readme' ./internal/tools/`
**Depends on item 3.**
**Commit:** `docs(manual): name the one-item background sub_agent workflow`
