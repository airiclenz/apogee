# A workflow run's ending is settled once — plan

**Goal:** `internal/workflow` settles every exit of a run and is the only writer of its phase; the note, the Driver event and the stop answer read one `workflow.Ending` value; the background manager ends a run through one path. Three drifts are fixed on the way: a failed final write, a failed stage and a background run that fails before it starts each record `failed`.
**Date:** 2026-11-23
**Status:** unexecuted
**sized for:** ~200k-context host
**base:** 1f9f5369
**Closes:** workflow-ending-settlement

**Sources:**
- `.capcom/issues/workflow-ending-settlement.md`; `docs/reviews/archived/architecture-review-2026-10-14.html` (#8); items 8–10 of `docs/plans/archived/2026-10-14 - 01 - architecture-review-defects-plan.md`
- ADR 0088 (stop keeps finished work), ADR 0089 D3 (the finish note) and D5 (a clear drops held notes and never waits), ADR 0010 (workflow never imports agent): `capcom know show "ADR NNNN" --with-children`
- `docs/manual/workflows.md` (phases, stopping, re-running)

**Ratified design calls** (owner):
- **Drifts (2026-11-23):** fix all three in this plan — final-write failure, failed stage's phase, background early failure — each as its own `fix` item.
- **Queued stop (2026-10-14 plan, item 9, kept):** stopping a queued workflow holds no note and emits no event.

**Writer's calls** (2026-11-23):
- **Library entry points:** `(*workflow.Runner).SettleUnstarted(id string) (Phase, error)` for a run that never ran; `workflow.EndingOf(result Result, err error) Ending` for one that did.
- **Admit refusal:** the folder is left as it was (documented today); every other early failure marks a launch-created folder `failed`.
- **Clear / stop-all:** still drops notes, never waits, swallows the unstarted-settle error (ADR 0089 D5).

**Standing requirements:**
- skills: coding-standards
- Machine rule (Raspberry Pi 5, 8 GB): never `go test -race`/`-cover` over a whole heavy package or a pattern; one package + one `-run` with `GOMEMLIMIT=2GiB`; full suite only as `APOGEE_TEST_SLOW=1 GOMEMLIMIT=2GiB make test`; never two test runs at once. Forward to every sub-agent.
- Byte-identical: the finish notes (`workflow %s finished|stopped — … — report: |items: …`, `workflow %s failed — %s`), the note header, `workflowControlStopped`, `workflowControlStoppedQueued`, `workflowControlStoppedQueuedRerun`, `unknownWorkflowFormat`, the status words.
- A test that needs a model scripts `internal/stubllm`; durations go through `internal/testtime`.
- An authorized deviation from item text lands as a dated NOTES line under the item.

**Out of scope:**
- The blocking launch's answer text (`runBlocking`) and `Admit`'s own rules.
- `StateOf` showing queued/background over the recorded phase; `hasRerunnableItem`'s predicate.
- Whether `admitBlocking` should also guard a retiring run's folder.

**Regression check (2026-11-23, 1f9f5369):** 1, 2: guard folded (writer's decisions; 2's fault rule also binds 6 and 7)
- 3: guard folded — no new stage event (yields to `internal/domain/events.go`, `WorkflowStageFailed`), every `running` stage marked, TUI test named
- 5: guard folded — `NeverStarted` only from `SettleUnstarted`, test call sites, Goal narrowed with a source scan
- 6: guard folded — a settling set of its own, clock-blocked test, Goal made the observable half
- 7: guard folded — pending-only mark, a library call for an id-less failure, launch id for a background observer only

## 1. The Runner settles every exit through one function

**What:**
**Goal:** inside `Runner.Run`, every exit taken after the run's folder is open — done, stopped on cancel, stage error — writes the workflow phase and `items.md` through one unexported settle function; recorded phases, `items.md` content and returned `(Result, error)` are what they are at the header base.
**Approach (assumed at the header base):**
- One unexported settle function on `runState` in `internal/workflow/runner.go` replaces the loop tail (`setWorkflowPhase` + `Store.WriteItems`) and the body of `failRun`; `failRun`'s best-effort writes stay best-effort, the normal end's write errors stay returned.
- No behaviour change in this item; items 2, 3 and 7 change behaviour inside `settle`.
**Regression guard.** (writer's fix for reviewer NOTES) the settle function's parameter list is not binding — it takes whatever both writes and the returned listing need (the run's Result included); the item names it as "one unexported settle function on runState", and items 2, 3 and 7 refer to it that way, never by a fixed signature.
**Files:** internal/workflow/runner.go, internal/workflow/runner_test.go
**Read first:** internal/workflow/runner.go — Runner.Run, runState.failRun, runState.setWorkflowPhase; internal/workflow/store.go — Store.WriteItems; internal/workflow/stages.go — runState.settleStage, runState.settleRecorded (names already taken beside the new settle); internal/workflow/runner_test.go — newTestRunner, TestRunnerRecordsFailedWhenAStageErrors
**Tests:** new `TestRun_EveryExitSettlesStatusAndItems` — table over done, cancel mid-stage (stopped, finished items kept), cancel after the last stage (done), stage error (failed): asserts the status phase, that `items.md` exists, and the returned error class; existing runner tests pass unchanged.
**Acceptance:** `go build ./... && GOMEMLIMIT=2GiB go test -race -count=1 -run 'TestRun' ./internal/workflow/`
**Commit:** `refactor(workflow): the runner settles every exit through one function`

## 2. A run whose final write fails records failed

Depends on item 1.

**What:** Fixes a drift: when `Store.WriteItems` fails after the phase was written, `status.json` reads `done` or `stopped` while the finish note and the Driver event say failed.
**Goal:** when either final write of a settling run fails, the run's recorded workflow phase is `failed` (best-effort) and `Run` returns the write error; status, note and event then agree.
**Approach (assumed at the header base):** `settle` writes `items.md` before the terminal phase, and on any failure records `failed` through the best-effort path `failRun` uses, returning the original error. A cause that was already a stage error keeps `failed`.
**Regression guard.** (writer's fix for reviewer NOTES; applies equally to items 6 and 7) `Runner.Store` is a concrete `*workflow.Store` with no fault seam and none is added to production code for a test: a failing write is produced by real filesystem state (for example a directory standing where the file must be written, so the write fails for any user, root included — never a permission bit), and a blocking settle in item 6 by the injectable `Runner.Now` clock blocking on a channel the test releases; each such test's **Tests** line names which of the two it uses.
**Files:** internal/workflow/runner.go, internal/workflow/runner_test.go
**Read first:** internal/workflow/runner.go — runState.failRun, runState.setWorkflowPhase, runState.writeStatus, Runner.now; internal/workflow/store.go — Store.WriteItems, atomicWrite, Store.WriteStatus; internal/workflow/runner_test.go — TestRunnerRecordsFailedWhenAStageErrors
**Tests:** new `TestRun_AFailedFinalWriteRecordsFailed`, both rows by real filesystem state — row 1: `items.md` pre-made a non-empty directory in the `Open`-ed folder, so `WriteItems` fails: `Run` returns the error and `ReadStatus` reads `failed`; row 2 fails the phase write instead: a directory stands at `status.json` for that one write, put there and removed again from the test's `Runner.Now` (which `writeStatus` calls just before each write), so the best-effort `failed` write lands. Both fail against the pre-item tree.
**Acceptance:** `GOMEMLIMIT=2GiB go test -race -count=1 -run 'TestRun' ./internal/workflow/`
**Commit:** `fix(workflow): a run whose final write fails records failed`

## 3. A failed stage reads failed

Depends on item 1.

**What:** Fixes a drift (noted as pre-existing in the 2026-10-14 plan, item 8): a stage whose error fails the run keeps phase `running` in `status.json`, so `/workflows` shows a running stage in a failed, non-live run.
**Goal:** when a stage error fails a run, that stage's recorded phase is `failed`; stages after it stay `pending`; the workflow phase stays `failed`.
**Approach (assumed at the header base):**
- `settle` (stage-error cause) records the failing stage's phase before the workflow phase (not through `setStagePhase` — see the guard), best-effort like the rest of that path.
- Consumers of a stage phase: `workflow.TallyOfStatus`, `Runner.resetStatus` (a replay resets it), `internal/tui/workflows.go` (`workflowState`, the stage rows), `workflowStatusResult` (`internal/agent/workflowcall.go`). Each already has a word for `failed`; confirm by test, change none.
- `docs/manual/workflows.md`: where a failed run is described, say the stage that failed reads `failed`.
**Regression guard.** No Driver event changes: `settle` sets the stage phase in `s.status` under `mu` and its own status write carries it — never `setStagePhase`, whose `notifyStage` would newly emit a `WorkflowStageFinished`/failed before `WorkflowFailed`. The item yields to `internal/domain/events.go`, where `WorkflowStageFailed` is documented as a stage after which "The Workflow goes on".
- `settle` marks `failed` the stage at the loop index and every stage still recorded `running`: an error inside a repeat's re-run returns at the repeat's index while the re-run target stage is the one recorded `running`.
**Files:** internal/workflow/runner.go, internal/workflow/runner_test.go, internal/tui/workflows_test.go, docs/manual/workflows.md
**Read first:** internal/workflow/runner.go — runState.setStagePhase, runState.failRun, Runner.resetStatus; internal/workflow/stages.go — runState.runRepeat, runState.runRound; internal/agent/workflowcall.go — workflowObserver.StagePhase, stageOutcomes; internal/tui/workflows_test.go — workflowsFixture
**Tests:** new `TestRun_AFailedStageReadsFailed` (stage phases `done, failed, pending` after a second-stage error; a re-run resets it; a repeat row: an error in a repeat's re-run leaves no stage `running`; the Observer sees no stage event for the failed stage); new TUI test `TestWorkflowsViewShowsAFailedStage` that the workflows view renders the failing stage's row with the `failed` word from a status file in that shape. The runner test fails against the pre-item tree.
**Acceptance:** `GOMEMLIMIT=2GiB go test -race -count=1 -run 'TestRun' ./internal/workflow/ && GOMEMLIMIT=2GiB go test -race -count=1 -run 'TestWorkflowsView' ./internal/tui/`
**Commit:** `fix(workflow): a stage that fails its run reads failed`

## 4. The library settles a run that never started

**What:**
**Goal:** `internal/agent` contains no call to `Store.WriteStatus`; a queued run that is stopped is settled by `(*workflow.Runner).SettleUnstarted(id) (Phase, error)`, which records `stopped` for a pending, running or stopped folder, keeps `done` and `failed`, stamps `Updated` from the Runner's clock, and returns the phase now recorded.
**Approach (assumed at the header base):**
- New `internal/workflow/settle.go` holds `SettleUnstarted`, taking the read-modify-write now in `markStopped` (`internal/agent/background.go`); `markStopped` is deleted and its two callers (the queued branch of `stopWorkflow`, `stopAllBackground`) call the Runner. Error handling per caller is unchanged: the stop returns it, stop-all swallows it.
- The stop answer still chooses `workflowControlStoppedQueued` or `…QueuedRerun` from the returned phase.
**Files:** internal/workflow/settle.go, internal/workflow/settle_test.go, internal/agent/background.go, internal/agent/background_test.go
**Read first:** internal/agent/background.go — markStopped, Agent.stopWorkflow, Agent.stopAllBackground, Agent.StopWorkflow (doc names markStopped); internal/workflow/runner.go — Runner.now; internal/workflow/store.go — Store.ReadStatus, Store.WriteStatus; internal/workflow/runner_test.go — storeClock
**Tests:** `settle_test.go`: table over the five prior phases, `Updated` equals the injected clock, a missing folder returns an error. `internal/agent`: new `TestAgentWritesNoWorkflowStatus` (no non-test file in the package names `WriteStatus`); existing `TestBackground_StoppingAQueuedWorkflowSaysItNeverStarted`, `…QueuedRerunLeavesTheFolderDone`, `…QueuedRerunLeavesTheFolderFailed` pass unchanged.
**Acceptance:** `go build ./... && GOMEMLIMIT=2GiB go test -race -count=1 -run 'TestSettleUnstarted' ./internal/workflow/ && GOMEMLIMIT=2GiB go test -race -count=1 -run 'TestBackground_Stopping|TestAgentWritesNoWorkflowStatus' ./internal/agent/`
**Commit:** `refactor(workflow): the library settles a run that never started`

## 5. One Ending value for the note, the event and the answer

Depends on item 4.

**What:**
**Goal:** `workflow.Ending` (`Phase`, `Err`, `NeverStarted`) is derived in exactly one place per kind of ending — `workflow.EndingOf(result, err)` for a run that ran, `SettleUnstarted`'s result for one that did not — and `finishNote`, `workflowObserver.end`, `finishPhase` and the stop answer read it; none of those four re-derives finished, stopped or failed from `(Result, error)` (`workflowAnswer`, the blocking answer, is out of scope and exempt). Every string and every emitted event is unchanged.
**Approach (assumed at the header base):**
- New `internal/workflow/ending.go`: the type and `EndingOf`, holding the one copy of the rule the three agent functions each restate.
- `internal/agent/background.go` (`finishNote`, `endBackground`), `internal/agent/workflowcall.go` (`workflowObserver.end`, the stop result) and `internal/agent/launch.go` (`runBlocking`, `finishPhase`) take an `Ending`, or derive it inside (see the guard).
- A run with an empty `result.ID` still emits no event in this item (item 7 changes that).
**Regression guard.** `EndingOf` always returns `NeverStarted` false — only the `SettleUnstarted` ending sets it — so the early error (empty `result.ID`) keeps the `workflow %s failed — %s` note held today; the event skip stays keyed on the empty ID.
- `finishNote` and `workflowObserver.end` either keep their `(result, err)` signatures and call `EndingOf` inside, or their call sites in `wake_test.go`, `workflowcall_test.go` and `workflowspawn_test.go` are rewritten with every assertion unchanged.
- `workflowAnswer` keeps reading `result.Stopped()`; a source-scan test pins that it is the only non-test code in `internal/agent` that does.
**Files:** internal/workflow/ending.go, internal/workflow/ending_test.go, internal/agent/background.go, internal/agent/workflowcall.go, internal/agent/launch.go, internal/agent/background_test.go, internal/agent/wake_test.go, internal/agent/workflowcall_test.go, internal/agent/workflowspawn_test.go
**Read first:** internal/agent/background.go — finishNote, subAgentFinishNote, finishPhase, driveBackground; internal/agent/workflowcall.go — workflowObserver.end, workflowStopResult, workflowAnswer; internal/agent/launch.go — runBlocking
**Tests:** `ending_test.go`: `EndingOf` over done, stopped, stage error, early error (empty ID; `NeverStarted` false in every row). New `TestAgentReadsAWorkflowEndingInOnePlace` (in the package's non-test files `.Stopped()` appears only inside `workflowAnswer`) — fails against the pre-item tree. Existing `TestFinishNote_*`, `TestRenderWorkflowNotes_*`, `TestWorkflowControl_StatusStopAndRefusals`, `TestWorkflowBlockNamesHowItEnded` pass with their assertions unchanged.
**Acceptance:** `go build ./... && GOMEMLIMIT=2GiB go test -race -count=1 -run 'TestEndingOf' ./internal/workflow/ && GOMEMLIMIT=2GiB go test -race -count=1 -run 'TestFinishNote|TestRenderWorkflowNotes|TestWorkflowControl|TestAgentReadsAWorkflowEndingInOnePlace' ./internal/agent/`
**Commit:** `refactor(workflow): one ending value feeds the note, the event and the answer`

## 6. The background manager ends a run through one path

Depends on item 5.

**What:** Fixes a race: a queued run's stop writes its phase outside the manager's lock after the id is free, so a relaunch of the same id can write `running` and be overwritten with `stopped`.
**Goal:** an unstarted run's settle completes before its id can be launched again; notes, events and the server hand-over stay as the listed tests pin them.
**Approach (assumed at the header base):**
- New `(*backgroundManager).end(run, ending workflow.Ending, opts)` in `internal/agent/background.go`, the one end function: `endBackground`, the queued branch of `stopWorkflow` and both loops of `stopAllBackground` call it, and it performs the ending's steps in one order — settle an unstarted run, hold the note, emit the event, cancel, drop, start the next — with each step's condition stated in that one place.
- Conditions, each explicit: no note and no event when `ending.NeverStarted` (ratified); no note when the run is retiring or stop-all is clearing; "start the next" only when the run held the server; stop-all never waits and resets `m.held` and `m.launched` as today.
- The settle of an unstarted run runs while the id still counts as live for `startBackground`'s check, without holding `m.mu` across the disk write.
**Regression guard.** The settling run is held in a set of its own that only `startBackground`'s id check reads — not `m.runs` (a run ending on its server would start it, `entries()` would list it, a second stop would settle it again) and not `m.retiring` (`serverBusyLocked` would keep its server busy with nothing to start the next).
- Item 2's guard binds the test: no fault seam in production code; the blocking settle is the injectable `Runner.Now` clock (`a.now`, which the launch Runner takes) blocking on a channel the test releases.
**Files:** internal/agent/background.go, internal/agent/background_test.go
**Read first:** internal/agent/background.go — stopWorkflow, endBackground, stopAllBackground, startBackground, serverBusyLocked; internal/agent/background_test.go — TestBackground_AClearDoesNotWaitOnABlockedSink (an emit under m.mu turns it red), TestBackground_FolderStampsFollowTheAgentClock; internal/agent/launch.go — newLaunchRunner
**Tests:** new `TestBackground_ARelaunchWaitsForAQueuedStopToSettle` (the settle blocks in the injected clock — `a.now`, read by `SettleUnstarted` between its read and its write — on a channel the test releases; the server is freed first, so a relaunch of the same id would start rather than queue: it is refused or waits, and the folder never ends `stopped` under a running relaunch) — fails against the pre-item tree; existing `TestBackground_ARetiringRunKeepsItsServerUntilItEnds`, `…AClearDoesNotWaitOnABlockedSink`, `…AStopAllDuringAnOpenExchangeLeavesNoNoteForAnAbortToHoldAgain`, `…TheIdleOnlyMutatorsDoNotRaceARunningWorkflow`, `…StoppingAQueuedWorkflowSaysItNeverStarted` pass unchanged.
**Acceptance:** `go build ./... && GOMEMLIMIT=2GiB go test -race -count=1 -run 'TestBackground_' ./internal/agent/`
**Commit:** `fix(agent): one end path for a background run; a queued stop settles before its id is reused`

## 7. A background run that fails before it starts is recorded and announced

Depends on item 6.

**What:** Fixes a drift: when `Runner.Run` fails before its loop (folder cannot be opened, status cannot be reset), a background launch holds a "failed" note but emits no event, so nothing wakes, and the launch-created folder stays `pending` and non-live for good.
**Goal:** a background run that fails before its first stage leaves a folder that never ran (recorded `pending`) `failed` — a found `done`, `failed` or `stopped` folder stays as it was — and emits `WorkflowFailed` carrying the launch-time id, so the held note is delivered; `/workflows` shows `failed` and re-run (`^r`, `RerunFailed`) accepts it. An `Admit` refusal leaves the folder as it was and emits nothing new.
**Approach (assumed at the header base):**
- `Runner.Run`: an early failure after the folder is known calls the best-effort `failed` write `settle` uses; the `Admit` refusal returns before it.
- `EndingOf`/the manager's `end`: an ending with an empty `result.ID` uses the run's launch-time id for the event instead of skipping it.
- `docs/manual/workflows.md`: the failure section says a run that could not start reads `failed` and can be re-run.
- ADR 0089 gains a dated amendment child Note (`adr-0089-amend-MMDD`, the day the item runs: every ending of a started-or-launched background run emits its event), named on the Status line and in the reading-order list.
**Regression guard.** `Run` cannot tell a launch-created folder (the launch's `Open` created it, so `openFolder` reports found): mark `failed` only when the phase read is `pending`, so a queued `RerunFailed` that cannot start keeps its `done` folder.
- A failure before `Run` knows an id (`openFolder` errors, empty `result.ID`) is marked by a new exported Runner method beside `SettleUnstarted` in `settle.go`, same pending-only rule; the agent still writes no status (item 4).
- Only a background run's observer is given the launch id; an id-less blocking observer still emits nothing, so the pin in `workflowcall_test.go` stays green unchanged.
- Item 2's guard binds the tests: real filesystem state, no fault seam. A fault that fails the reset's write for good fails the `failed` write too.
**Files:** internal/workflow/runner.go, internal/workflow/runner_test.go, internal/workflow/settle.go, internal/workflow/settle_test.go, internal/workflow/ending.go, internal/agent/background.go, internal/agent/workflowcall.go, internal/agent/background_test.go, docs/manual/workflows.md, .capcom/knowledge/decisions/adr-0089.md, .capcom/knowledge/decisions/adr-0089/adr-0089-amend-MMDD.md
**Read first:** internal/workflow/runner.go — Run, openFolder, resetStatus, Runner.Admit; internal/agent/workflowcall.go — workflowObserver.end, startLocked; internal/agent/background.go — driveBackground, RerunFailed
**Tests:** new `TestRun_AnEarlyFailureRecordsFailed` (filesystem state: a directory stands at `status.json` for the reset's one write, put and removed from the test's `Runner.Now`; rows: a `pending` folder reads `failed`, a found `done` folder stays `done`), `TestRun_AnAdmitRefusalLeavesTheFolder` and `TestSettleUnstarted_AFailedStartMarksOnlyAPendingFolder` (`internal/workflow`); new `TestBackground_ARunThatCannotStartWakesWithAFailedNote` — filesystem state: a queued background launch whose item source is replaced by a directory before it starts, so `Run` returns no id: the `WorkflowFailed` event carries the launch id, the note `workflow <id> failed — <cause>` is held, the folder reads `failed`, `RerunFailed` accepts the id. All four fail against the pre-item tree.
**Acceptance:** `go build ./... && GOMEMLIMIT=2GiB go test -race -count=1 -run 'TestRun|TestSettleUnstarted' ./internal/workflow/ && GOMEMLIMIT=2GiB go test -race -count=1 -run 'TestBackground_' ./internal/agent/ && capcom check`
**Commit:** `fix(workflow): a background run that fails before it starts records failed and wakes`
