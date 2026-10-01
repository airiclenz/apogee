# Workflow launch, tally and child-run deepening

**Goal:** Deepen the workflow engine per architecture review 2026-09-30 candidates #1, #2, #3: one launch builder with a live-folder guard, one tally the engine owns, one child-run lifecycle. Closes two live defects: two Runners in one folder, and a headless/daemon verdict that counts different items than the model's note.
**Date:** 2026-09-30
**Status:** unexecuted
**sized for:** ~200k-context host
**base:** 704514db
**Run order:** first of plans 2026-09-30 - 00/01/02; never concurrently with them.
**Sources:** `docs/reviews/architecture-review-2026-09-30.html` (#1, #2, #3); ADR 0087 D4, 0089 D1–D3, 0090 D1, 0063 D2, 0086 D4, 0088 D2, 0083 §3.
**Ratified design calls** (user, 2026-09-30):
- **Live folder:** a blocking launch (fan_out plan, fan_out recipe form, typed `/<id>`, foreground `StartRecipe`) onto a folder a background run still drives is refused, naming the bg run; nothing is created or spawned.
- **Verdict set:** the headless/daemon exit code and `failed` verdict use the finish note's tally; verify/merge receipts are not items; the exit-code change is intended (all fan-out items blocked + an ok verify now exits 1).
- **Note text:** the finish note stays byte-identical (no `resumed` segment); `domain.WorkflowTally` (the event's) has no resumed or verdict fields, `workflow.Tally` keeps them for the note.
- **Wire:** `workflow_phase` NDJSON (ADR 0075) is unchanged; the tally rides the Go event only.
- **Item text:** `internal/workflow` owns the shared pieces (status word, summary first line, field-value quoting); each surface keeps its layout and placeholders; wording changes only where today's is a bug (items 2, 8, 9).
- **Recipe source:** a launch's recipe source is a `domain.RecipeLaunch`, no new pair type (user, 2026-10-01).
**Standing requirements:**
- skills: coding-standards
- Acceptance commands run under the checkout's local test-running rules (`AGENTS.local.md`, if present).
**Out of scope:** guarding a background launch against a live blocking run; review 2026-09-30 candidates #10 (reactions), #15 (keystore) and #19 (budget); any version identifier.
**Not covered by plans 00–02:** review 2026-09-30 candidates #9, #11, #12, #13, #14, #16, #18, #20 (except its bead) and #17 (plan 00 item 10 only reuses `domain.RecipeLaunch`).
**Regression check:** three rounds plus a re-check (2026-09-30 ×2, 2026-10-01) at 704514db; reports in docs/skill-runs/implement-plan/2026-09-30_-_00_-_workflow-launch-tally-childrun-plan/.

## 1. workflow: Runner.Open shares Run's folder-open path — ✅ DONE (2026-10-01)

NOTES (2026-10-01): openStatus split into openFolder (validate/expand/PlanHash/Find/Create, shared by Open and Run) and resetStatus (Run's status rewrite with prior/replay); Run's no-ScriptRunner check now runs before Validate so it still refuses before any folder is created — an invalid plan that also has a script stage and no ScriptRunner now reports the ScriptRunner refusal first.
NOTES (2026-10-01): a created folder's Create stamp and Run's Updated stamp now each read Runner.now (two reads, not one shared value); with a pinned Now they are identical.

**Files:** `internal/workflow/runner.go`, `internal/workflow/runner_test.go`, `internal/workflow/doc.go`
**Read first:** internal/workflow/runner.go — Runner.Run, openStatus, expandStages; internal/agent/background.go — openWorkflowFolder, rerunMovedFormat
**What:**
**Goal:** `Runner.Run` and a new exported `Runner.Open(plan, folder)` share one validate/expand/plan-hash/find-or-create path, stamped by `Runner.Now`; `Open` on a found folder writes nothing and refuses a moved plan.
**Approach (assumed at the header base):** move `Validate`, `expandStages`, `PlanHash`, `Store.Find` and `openStatus`'s create branch into the unexported `r.openFolder(plan, folder) (status RunStatus, found bool, stageItems map[int][]Item, err error)`, called once by each of `Open` and `Run`; `Run` derives prior/replay from `found` (nil/false when created), then rewrites status as today. It replaces `agent.openWorkflowFolder` (item 3).
**Regression guard.** A moved plan is refused with the exported sentinel `workflow.ErrFolderMoved` (`errors.Is`-able), no wording of its own; item 3 maps it to `fmt.Errorf(rerunMovedFormat, folder)`, keeping the `RerunFailed` error byte-identical. Folder `""` finds or creates by hash; a named folder that is missing or moved is refused before any `Store.Create`.
**Tests:** `TestRunnerOpenFindsOrCreatesTheFolderRunWillResume` (Now pinned; a second `Open` returns the same id, `status.json` bytes unchanged; `Run` then resumes it; a differing folder id is refused, `errors.Is` the sentinel; a no-match `Open(plan, "<id>")` returns `ErrFolderMoved` and creates no folder).
**Acceptance:** `go test ./internal/workflow -run 'TestRunner|TestStore|TestDocMap'`; `go vet ./internal/workflow`
**Commit:** `refactor(workflow): Runner.Open owns folder open for Run and background start`

## 2. workflow: one tally, workflow state and item-text pieces — ✅ DONE (2026-10-01)

NOTES (2026-10-01): `agent.finishTally`, `workflowState` and `itemCounts` stay in internal/agent untouched — this item adds their engine-side replacements (TallyOf, StateOf, TallyOfStatus); items 4, 8 and 9 rewire the callers and delete the agent copies.
NOTES (2026-10-01): TallyOfStatus leaves a skipped fan-out out (as TallyOf does) and counts a done item with no receipt as unfinished (runner tallyOf's rule, now the shared Tally.countOutcome); agent.itemCounts counted both into its total, and a receipt-less done item as done.
NOTES (2026-10-01): TallyOf sums the Resumed count too (finishTally did not); Line() never renders it, so the finish note text is unchanged. totalsLine and Line share one renderer (Tally.render), and runner.go's tallyOf now classifies through Tally.countOutcome — no behaviour change, TestFormat* unchanged.

**Files:** `internal/workflow/tally.go`, `internal/workflow/tally_test.go`, `internal/workflow/format.go`, `internal/workflow/runner.go`, `internal/workflow/stages.go`, `internal/workflow/doc.go`
**Read first:** internal/workflow/format.go — receiptText, fieldValue; internal/agent/background.go — finishTally, liveStates; internal/agent/workflowcall.go — workflowState, itemCounts
**What:**
**Goal:** `workflow` exports `TallyOf(Result) Tally` (fan-out stages only, skipped excluded, a repeat stage's latest round only — `agent.finishTally`'s rule), `TallyOfStatus(RunStatus) Tally`, `Tally.Total()` (OK+Partial+Blocked+Unfinished), `Tally.Line()` (`items N · ok a · partial b · blocked c`, no resumed), `StateOf(Info) State`, and the three item-text pieces `ItemStatusWord(Phase, *Receipt) string`, `FirstLine(string) string` and `FieldValue(any) string`; `Format` output is byte-identical except a text field holding whitespace other than space, tab or newline (`\r`, `\v`, U+00A0), now quoted.
**Approach (assumed at the header base):** move `agent.finishTally`'s summation, `itemCounts` and the `workflowState` ladder into tally.go. `State{Kind StateKind; Phase Phase}`: `StateQueued`, `StateBackground`, `StateRecorded` (Phase set only there). `receiptText` calls `ItemStatusWord` (its own rule); `fieldValue` (format.go) and `firstLine` (runner.go; stages.go callers renamed) are exported in place, `FieldValue` quoting empty text and any `=` or `unicode.IsSpace` rune. `totalsLine` stays (per stage, with resumed); `TallyOfStatus` cannot fill resumed/verdicts.
**Regression guard.** `TallyOf` sums each counted fan-out stage's `StageResult.Tally` (a repeat stage's latest round), never recounting `Items` (TestFinishNote's fixtures have none); `Tally` keeps its resumed and verdict fields. `TallyOfStatus` counts `StageFanout` lines only. A non-live folder at phase running is `StateRecorded` + `PhaseRunning`, never `StateBackground`. Items 4, 7 and 8 map `State` and `Line()` as pinned here.
**Tests:** `TestTally_OfResultStatusAndLine`, one table: verify/merge and skipped fan-out excluded, unfinished, verdicts, a repeat stage's latest round, a fan-out stage with a `Tally` and no `Items`; `TallyOfStatus` rows incl. an uncounted verify line; `Line()` rows pinning each segment, the order and when each appears (unfinished > 0, verdicts > 0, neither). `TestStateOf`: queued (`Queued` and `Background` both set, as the manager does), background, a recorded phase, a non-live folder at phase running unlike the background row. `TestItemText_Pieces`: `ItemStatusWord` done+receipt, stopped, pending; `FirstLine` of a leading-newline and a space-padded summary; `FieldValue` of a space, `=`, newline, `\r`, empty text, a list, a float. `TestFormat*` unchanged.
**Acceptance:** `go test ./internal/workflow -run 'TestFormat|TestTally_|TestStateOf|TestItemText_|TestReceiptDomain|TestRunner|TestDocMap'`
**Commit:** `refactor(workflow): own the item tally, workflow state and item-text pieces`

## 3. agent: background start opens its folder through Runner.Open — ✅ DONE (2026-10-01)

NOTES (2026-10-01): startBackground (shared by the fan_out and recipe background paths) is the single call site of Runner.Open; startBackgroundRecipe needed no change of its own. The "opened at launch, not at start" rationale moved from the deleted openWorkflowFolder comment to a comment at the Open call.

**Depends on:** item 1.
**Files:** `internal/agent/background.go`, `internal/agent/background_test.go`, `internal/agent/workflowcall.go`, `internal/agent/recipe.go`
**Read first:** internal/agent/background.go — startBackground, openWorkflowFolder, markStopped, backgroundLaunch (its comment names openWorkflowFolder)
**What:** refactor, not a fix: the raw `time.Now()` folder stamp has no runtime effect at base.
**Goal:** the background path opens its workflow folder only through `workflow.Runner.Open`, stamped by `Runner.Now` (no raw `time.Now()` in folder stamps); `openWorkflowFolder` no longer exists.
**Approach (assumed at the header base):** `startBackground` / `startBackgroundRecipe` call `Runner.Open` on the runner they build (a queued run needs its id and status path); a moved rerun maps to `rerunMovedFormat`.
**Regression guard.** Set `Now: a.now` in `newWorkflowRunner` and `newRecipeRunner` (neither sets a clock today), so background, blocking and resume runners all stamp from the agent clock; `markStopped` stamps from `run.runner.Now` (`Runner.now` is unexported), else `time.Now`.
**Tests:** `TestBackground_FolderStampsFollowTheAgentClock` (pinned `Agent.now`; the created folder's and a stopped queued run's stamps come from `Runner.Now`); all existing `TestBackground_*` green.
**Acceptance:** `go test ./internal/agent -run 'TestBackground_'`; `! grep -n 'openWorkflowFolder' internal/agent/*.go`
**Commit:** `refactor(agent): background start opens its folder through Runner.Open`

## 4. agent: finish note, listing and state read the engine's tally — ✅ DONE (2026-10-01)

NOTES (2026-10-01): `itemCounts` is replaced by a two-line adapter `itemProgress` (done = `TallyOfStatus(...).Total()-Unfinished`, total = `Total()`) shared by `workflowListing` and `workflowDetail`; it holds no counting of its own. The listing/detail counts now follow the engine's rule — a skipped fan-out is left out and a done item without a receipt counts as unfinished (item 2's notes).
NOTES (2026-10-01): `internal/agent/wake_test.go` needed no change — `TestFinishNote_SaysHowTheWorkflowEnded` passes unchanged against `workflow.TallyOf(result).Line()`, confirming the note text is byte-identical.

**Depends on:** items 2, 3.
**Files:** `internal/agent/background.go`, `internal/agent/workflowcall.go`, `internal/agent/wake_test.go`, `internal/agent/workflowcall_test.go`
**Read first:** internal/agent/background.go — finishNote, finishTally; internal/agent/workflowcall.go — workflowListing, workflowState, itemCounts
**What:**
**Goal:** `agent` holds no tally or state copies: the finish note, `workflowListing`/`workflowDetail` counts and `workflowState` delegate to `workflow.TallyOf`/`TallyOfStatus`/`StateOf`; the note text is byte-identical.
**Approach (assumed at the header base):** delete `finishTally`, `itemCounts` and the local ladder; their users are `finishNote`, `workflowListing`, `workflowDetail` and the `workflowStatusLineFormat` callers.
**Regression guard.** `workflowState` maps `StateQueued` → `workflowStatusQueued`, `StateBackground` → `workflowStatusRunning` ("running in the background"), `StateRecorded` → its raw phase; the listing's done count is `Total()-Unfinished`. Delete the `finishTallySeparator` const with `finishTally`, its only reader (golangci `unused`).
**Tests:** `TestWorkflowState_MapsTheEngineState` (queued, running in the background, a recorded phase); `TestFinishNote_SaysHowTheWorkflowEnded` and `TestWorkflowControl_StatusStopAndRefusals` unchanged and green.
**Acceptance:** `go test ./internal/agent -run 'TestWorkflowState_|TestFinishNote|TestWorkflowControl_|TestBackground_|TestWorkflowCall_ThreeItems'`; `! grep -nE 'func (finishTally|itemCounts)|finishTallySeparator' internal/agent/*.go`
**Commit:** `refactor(agent): delegate the workflow tally and state to the engine`

## 5. domain, agent: the end phase carries the tally

**Depends on:** item 4.
**Files:** `internal/domain/events.go`, `internal/agent/workflowcall.go`, `internal/agent/workflowcall_test.go`, `internal/eventjson/encode.go`, `apogee.go`, `example_test.go`
**Read first:** internal/agent/workflowcall.go — workflowObserver.end; internal/domain/events.go — WorkflowPhaseEvent; internal/eventjson/encode.go — workflowPhaseData;
apogee.go — WorkflowReceipt alias; internal/agent/workflowcall_test.go — TestWorkflowObserver_ReportsAWaitingQuestionAndAFailure
**What:**
**Goal:** a finished or stopped `WorkflowPhaseEvent` carries `Tally *domain.WorkflowTally` (`OK, Partial, Blocked, Unfinished` + `Total()`), equal to the tally in the model's note, non-nil only on finished and stopped end phases (nil on failed and every non-end phase); the NDJSON line is unchanged.
**Approach (assumed at the header base):** add the type and field in `events.go`; fill it in the single producer `workflowObserver.end` (called by `workflowCallResult`, `runRecipe`, `driveBackground`) from `workflow.TallyOf(result)`; alias `WorkflowTally` in `apogee.go` with an `example_test.go` pin. Consumers are items 6–7.
**Regression guard.** `eventjson` never gains the field: only the comment above `workflowPhaseData`, which lists every member kept off the line, names `Tally`.
**Tests:** `TestWorkflowCall_EndPhaseCarriesTheNotesTally` (verify+merge recipe with a resumed item: verify/merge excluded; a failed end phase and a non-end phase carry a nil `Tally`); `eventjson` encode test still green.
**Acceptance:** `go test ./internal/domain ./internal/eventjson . -run 'Workflow|Example'`; `go test ./internal/agent -run 'TestWorkflowCall_|TestBackground_'`
**Commit:** `feat(agent): the workflow end phase carries the engine's tally`

## 6. run, cmd: the exit verdict reads the end-phase tally

**Depends on:** item 5.
**Files:** `internal/run/run.go`, `internal/run/run_test.go`, `cmd/apogee/wire_firing.go`, `cmd/apogee/headless_test.go`, `cmd/apogee/daemonfire_test.go`
**Read first:** internal/run/run.go — eventTap.noteWorkflowPhase, WorkflowOutcome; cmd/apogee/wire_firing.go — recipeWorkflowFailure;
cmd/apogee/headless_test.go — recipeHeadless, TestHeadlessRecipeWithEveryItemBlockedExits1; internal/workflow/stages.go — selectForVerify
**What:** fix — the recipe verdict counted every `WorkflowItemFinished` (verify/merge/resumed included), so ok verify receipts masked "every item blocked" while the note said otherwise.
**Goal:** `run.WorkflowOutcome.Items` is the end phase tally's OK+Partial+Blocked (items finished on a receipt, as its doc says — never `Total()`, which includes Unfinished) and `Blocked` its Blocked; `recipeWorkflowFailure` keeps its signature and stopped/failed branches.
**Approach (assumed at the header base):** `eventTap.noteWorkflowPhase` reads `e.Tally` on end phases and stops counting item events; `headless.go` and `daemonfire.go` read it via `recipeWorkflowFailure`.
**Regression guard.** A nil Tally keeps today's End-based stopped/failed branches. Counting moves from every round to the latest round; the `WorkflowOutcome` doc says so. `selectForVerify` takes every finished item, so the headless case scripts one verify child per finished item (ok and blocked alike) unless the verify stage has a `when:`; the new recipe skill keeps `id: sweep` (`recipeHeadless` writes `skills/sweep`). The CHANGELOG sidecar (never CHANGELOG.md) names the exit-code change and the latest-round rule.
**Tests:** `TestEventTap_RecipeVerdictReadsTheEndPhaseTally` (`&eventTap{…}` + `Emit`: item events of every round ignored; Items/Blocked from the end phase's `Tally`, Items without its Unfinished; a nil-`Tally` end keeps the End-based verdicts); a headless case pinning `every item of recipe /%s's workflow %s blocked (%d of %d)`: 2 blocked fan-out items + ok verify → fails (2 of 2); 1 of 2 blocked + verify → exits 0.
**Acceptance:** `go test ./internal/run -run 'TestEventTap|TestOnceLaunchesALeadingRecipeReference'`; `go test ./cmd/apogee -run 'TestHeadless|TestDaemonFireFailsARecipeWorkflowThatDidNotLand'`
**Commit:** `fix(run): recipe verdict counts the tally the model's note reports`

## 7. tui: block totals and background finish line read the tally

**Depends on:** item 5.
**Files:** `internal/tui/workflowblock.go`, `internal/tui/workflow.go`, `internal/tui/transcriptbridge.go`, `internal/session/transcript.go`, `internal/tui/workflowblock_test.go`, `internal/tui/workflow_test.go`, `internal/session/transcript_test.go`
**Read first:** internal/tui/workflow.go — foldBackgroundPhase, backgroundWorkflow.count, finishLine; internal/tui/workflowblock.go — workflowView.fold, workflowView.totals;
internal/tui/transcriptbridge.go — toWireWorkflow, fromWireWorkflow
**What:** fix — block totals and the background finish line counted every `WorkflowItemFinished`, verify/merge children's included (`runItems` → `ItemPhase`). CHANGELOG sidecar: a recipe's TUI totals and background finish line no longer count verify/merge steps as items.
**Goal:** an ended block that carries a tally, and the background finish line, render the end phase's `Tally` through `Tally.Line()`; only a block with no tally (running, failed, replayed from a pre-change record) counts its items; `backgroundWorkflow.count` and its `ok/partial/blocked` fields are gone; line wording is unchanged.
**Approach (assumed at the header base):** `workflowView.fold` stores `e.Tally` on the end phase; `foldBackgroundPhase`/`finishLine` read it, a nil `Tally` on a finished or stopped background end (hand-built test events only) rendering as zero. `session.Workflow` gains `Tally *WorkflowTally` (`json:"tally,omitempty"`), a session-owned struct tagged `ok`, `partial`, `blocked`, written by `toWireWorkflow`, restored by `fromWireWorkflow`.
**Regression guard.** ADR 0090 D5 stands: a post-change record replays the live tally; a pre-change record and a running block count their items (`TestWorkflowBlockListsOnlyTroubleOnALargeRun`). Never store an untagged `domain.WorkflowTally`. Render via a `workflow.Tally` from `*e.Tally`: items = OK+Partial+Blocked, no unfinished or verdict segment. `usage_test.go` needs no change. Delete `receiptPartial`/`receiptBlocked` with `count`, their only reader, and reword the "receipt statuses a finish line counts" comment; `receiptOK` stays only while `workflow_test.go` names it (golangci `unused`).
**Tests:** end-event builders carry a tally; pin `background workflow sweep finished — items 1 · ok 1 · partial 0 · blocked 0` (`bgFinishLine`); `TestWorkflowBlockTotalsExcludeAVerifyItem`; `TestBackgroundWorkflow_AStoppedRunsFinishLineReadsItsTally` (folds an ok verify-stage item, a second-round item or no item events, so the pre-item tree fails; end `Tally` 1 ok, 2 unfinished: `items 1 · ok 1 · partial 0 · blocked 0`); a failed and a running block keep counting items; `TestBackgroundWorkflow_EventsReachNeitherTranscriptBoardNorStallClock` checks the view's name, not the removed `view.ok`; `TestTranscriptRoundTripsAWorkflowTally` (survives encode/decode; absent decodes as nil); `TestWorkflowBlockReplaysTheEndPhaseTally` (with an ok verify item, pins `items 1 · ok 1 · partial 0 · blocked 0` both live and after save and reopen, not mere equality; a pre-change record counts its items).
**Acceptance:** `go test ./internal/tui -run 'TestBackgroundWorkflow_|TestWorkflowBlock|TestStoppedRecipeBlock|TestResumeHint|TestFinishedBlockHasNoResumeHint|TestResumedWorkflow|TestAStageOutcome|TestWorkflowStage|TestDuplicateStarted'`; `go test ./internal/session -run 'TestTranscript|TestDecode'`; `! grep -n 'func (.*backgroundWorkflow) count' internal/tui/*.go`; `! grep -nE 'receiptPartial|receiptBlocked' internal/tui/*.go`
**Commit:** `fix(tui): workflow totals and finish line count the engine's item set`

## 8. tui: list row, state and item line come from the engine

**Depends on:** items 2, 7.
**Files:** `internal/tui/workflows.go`, `internal/tui/workflows_test.go`, `internal/tui/workflowblock.go`, `internal/tui/workflowblock_test.go`
**Read first:** internal/tui/workflowblock.go — workflowItemLine, workflowFieldValue, transcript.finishWorkflowItem; internal/tui/workflows.go — workflowListRow, workflowState
**What:** fix — the TUI item line left a newline in a field value unescaped, and a summary opening with a newline read "(no summary)" in the item line and an empty gist on the item head.
**Goal:** `workflowListRow` counts via `workflow.TallyOfStatus`, `workflowState` maps `workflow.StateOf` (the TUI adds only `waiting`), `workflowItemLine` quotes through `workflow.FieldValue`, and every receipt summary in workflowblock.go takes its first line through `workflow.FirstLine`; wording changes only for a field value holding whitespace other than space or tab (newline, \r, \v, U+00A0 — quoted, as in Format) and a summary with leading blank lines or padding spaces (its first non-blank line, trimmed) in the item line, stage body and item-head gist.
**Approach (assumed at the header base):** delete `workflowListRow`'s inline `itemCounts` copy, the local ladder and `workflowFieldValue`; `workflowView.fold`, `workflowItemLine` and `finishWorkflowItem` call `workflow.FirstLine` on `e.Receipt.Summary`; the TUI's `firstLine` (textutil.go) stays for non-receipt text. `tui` imports `internal/workflow`, never `internal/agent` (ADR 0010).
**Regression guard.** `workflowState` keeps its order: `StateQueued` → queued, then `waiting`, then `StateBackground` → running, `StateRecorded` → its phase through `sanitize.StripEscapesToLine`. `workflowNoSummary` stays the empty-first-line placeholder. `workflowFieldValue` quoted only ` \t=`, so `\r` (dropped by the escape strip today), `\v` and U+00A0 values are now quoted too.
**Tests:** `TestWorkflowsListRowCountsFanOutItemsOnly` (a run with verify items); `TestWorkflowsStateMapsTheEngineState` (queued, waiting, running, a recorded `running`, an escape in the phase); `TestWorkflowItemLineEscapesNewlines` (a newline row and a `\r` row); `TestWorkflowItemLineReadsALeadingNewlineSummary` (a leading-newline and a space-padded row: item line, stage body and item-head gist show the first non-blank line, trimmed).
**Acceptance:** `go test ./internal/tui -run 'TestWorkflows|TestWorkflowItemLine|TestWorkflowBlock'`; `! grep -n 'firstLine(e.Receipt' internal/tui/workflowblock.go`
**Commit:** `fix(tui): workflow item line quotes newlines and reads a summary's first real line`

## 9. agent, tui: item status text reads the engine's pieces

**Depends on:** items 2, 4, 8.
**Files:** `internal/agent/workflowcall.go`, `internal/agent/workflowcall_test.go`, `internal/tui/workflows.go`, `internal/tui/workflows_test.go`
**Read first:** internal/agent/workflowcall.go — itemStatusText; internal/tui/workflows.go — workflowItemStatus, workflowItemLines; internal/workflow/runner.go — runState.finishItem (receipt set only with PhaseDone)
**What:** fix — the `workflow` status detail wrote a receipt field value unquoted (`k=a b`, `k=`), blurring its pair.
**Goal:** agent `itemStatusText` takes its status from `workflow.ItemStatusWord` and each field value as `oneLine(workflow.FieldValue(v))`; tui `workflowItemStatus` takes its status from `workflow.ItemStatusWord` and `workflowItemLines` reads it through `workflowItemStatus`; every output is byte-identical except an agent text field value that is empty or holds `=` or whitespace, which is quoted as in Format.
**Approach (assumed at the header base):** agent keeps `<status> — <summary>[ k=v…]`, the whole summary folded by `oneLine`; tui keeps `<status> — <summary>` escape-stripped onto one line and `workflowItemLines`' unquoted `key: value` lines. `FieldValue` reads the raw receipt field, so `Receipt.Domain()` leaves `itemStatusText`.
**Regression guard.** `oneLine` stays around `FieldValue`, which joins list elements raw. The tui keeps `sanitize.StripEscapesToLine` around `ItemStatusWord` (status.json is read from disk).
**Tests:** `TestItemStatusText_QuotesAFieldValueThatBlursItsPair` (no receipt, no fields and plain fields byte-identical; a spaced, an empty, a newline and a `\r` value quoted; a list joined by commas; a list element's newline folded to a space); `TestWorkflowItemStatus_ReadsTheEngineStatusWord` (pending, running, done with and without a summary, all byte-identical).
**Acceptance:** `go test ./internal/agent -run 'TestItemStatusText_|TestWorkflowControl_'`; `go test ./internal/tui -run 'TestWorkflowItemStatus_|TestWorkflows'`
**Commit:** `fix(agent): workflow status detail quotes a field value that blurs its pair`

## 10. agent: one launch builder for the background and resume paths

**Depends on:** items 3, 4, 5.
**Files:** `internal/agent/launch.go`, `internal/agent/launch_test.go`, `internal/agent/background.go`, `internal/agent/recipe.go`, `internal/agent/workflowcall.go`, `internal/agent/doc.go`
**Read first:** internal/agent/background.go — startBackground, resumeBackground, backgroundLaunch, RerunFailed, entryScratch; internal/agent/workflowcall.go — workflowLaunch
**What:**
**Goal:** one `workflowLaunch` value (source plan|recipe+inputs|folder, mode blocking|background, seat, turn, resume command) and one builder wire runner, spawner, observer and asker; `startBackground`, `startKeyedBackgroundRecipe`, `resumeBackground` and `RerunFailed` use it; behaviour is unchanged.
**Approach (assumed at the header base):** the value replaces `workflowLaunch{kind,recipe,line}` and `backgroundLaunch`; its recipe source is a `domain.RecipeLaunch` (SkillID = recipe id, Text = inputs), mode its own field (review #17). The builder subsumes `newWorkflowRunner`/`newRecipeRunner`'s Prompts/Scripts/Recipe/Asker difference and `startBackground`'s post-construction writes (`newWorkflowSpawner` on `backgroundHost()`, `spawner.children = &a.children`, `recipeScripts.agent`, `observeWorkflow`, `backgroundAsker`); callers pass the value, never write runner fields. `recipeScripts` keeps its host swap for background.
**Regression guard.** Builder users include the old value's readers (`resumeHint`, `resumeCommand`, `workflowAnswer`), `workflowCallResult`'s fan_out background branch and `recipeLaunchKind`. The folder source carries the recipe id and its store (`entryScratch`; today `launch.runner.Store = store`), both applied by the builder. Only `RerunFailed` pins its folder id (refused with `rerunMovedFormat` when the hash leads elsewhere); a resume (`ResumeWorkflows`, folder `""`) finds or creates by hash, so a moved `files:` source runs in a new folder.
**Tests:** `TestLaunch_BackgroundAndResumeSharePlanAndRecipeWiring` (runner width, spawner seat, asker scope per mode; a moved resume opens a new folder; a moved `RerunFailed` is refused with `rerunMovedFormat`); existing `TestBackground_*` (`TestBackground_AWorkflowKeptAcrossAScratchMoveIsListedAndResumesFromItsHome` included), `TestRecipe_*` green.
**Acceptance:** `go test ./internal/agent -run 'TestLaunch|TestBackground_|TestRecipe_|TestDocMap'`
**Commit:** `refactor(agent): one launch builder for background and resume workflows`

## 11. agent: blocking workflow paths use the launch builder

**Depends on:** item 10.
**Files:** `internal/agent/launch.go`, `internal/agent/workflowcall.go`, `internal/agent/recipe.go`, `internal/agent/workflowcall_test.go`, `internal/agent/doc.go`
**Read first:** internal/agent/workflowcall.go — workflowCallResult, recipeCallResult; internal/agent/recipe.go — runRecipe, launchRecipe, StartRecipe, completeInputs
**What:**
**Goal:** `workflowCallResult`, `recipeCallResult`/`runRecipe` and `launchRecipe` build their runner only through the builder of item 10; `newWorkflowRunner` and `newRecipeRunner` no longer exist; the refusal texts per surface are unchanged.
**Approach (assumed at the header base):** blocking mode sets `observer.call`/`observer.resume` inside the builder; only `completeInputs` moves into it. `TestWorkflowCall_TheRunnerIsSizedForItsSeat` moves from `newWorkflowRunner` to the builder.
**Regression guard.** `bindRecipeInputs` stays at `StartRecipe` before Submit and in `launchRecipe`, and `startBackgroundRecipe`'s `BindInputs` stays, so `missing input`/unknown-key refusals stay synchronous. Keep the `fanOutNoScratch` const (resolution.go reads it).
**Tests:** existing `TestWorkflowCall_*`, `TestRecipe_*` (`TestRecipe_StartRecipeRefusals`, `TestRecipe_AMissingInputWithNoAskerRunsNothing` included), `TestStoppedRecipeAnswerCarriesResumeLine`, `TestStoppedFanOutAnswerCarriesResumeLine`, `TestFinishedWorkflowAnswerHasNoResumeLine`, `TestRecipeStartedEventCarriesResume`, `TestFanOutStartedEventHasNoResume` green.
**Acceptance:** `go test ./internal/agent -run 'TestWorkflowCall_|TestWorkflowControl_|TestRecipe_|TestStopped|TestFinishedWorkflow|TestFanOutStarted|TestRecipeStarted|TestBackground_|TestLaunch|TestDocMap'`; `! grep -nE 'func .*(newWorkflowRunner|newRecipeRunner)' internal/agent/*.go`
**Commit:** `refactor(agent): blocking workflow launches go through the launch builder`

## 12. workflow: Runner.Admit can refuse a folder before Run writes to it

**Depends on:** item 1.
**Files:** `internal/workflow/runner.go`, `internal/workflow/runner_test.go`
**Read first:** internal/workflow/runner.go — Runner.Run, openStatus; internal/workflow/runner_test.go — newTestRunner, recordingSpawner
**What:**
**Goal:** `Runner.Admit func(id string) error` (nil admits every folder) is called by `Run` with the id `openFolder` found or created, after the sources are expanded and before any write to a found folder; a refusal returns the hook's error with a zero `Result` (no id) and leaves a found folder's bytes unchanged.
**Approach (assumed at the header base):** in `Run`, between `openFolder` (item 1) and the status rewrite. `Admit`'s doc says a created folder is already written (a live folder is always a found one). The zero `Result` makes `agent.workflowObserver.end` emit nothing (it reports only a run with an id).
**Tests:** `TestRunnerAdmitRefusesBeforeAnyWrite` (a stored folder; an `Admit` refusing its id: `Run` returns that error, `errors.Is`-able, a zero `Result`, `status.json` bytes unchanged, no Spawner call; a nil `Admit` resumes the same folder).
**Acceptance:** `go test ./internal/workflow -run 'TestRunner'`
**Commit:** `feat(workflow): Runner.Admit can refuse a folder before Run writes to it`

## 13. agent: a blocking launch refuses a folder a background run is driving

**Depends on:** items 11, 12.
**Files:** `internal/agent/launch.go`, `internal/agent/workflowcall.go`, `internal/agent/launch_test.go`
**Read first:** internal/agent/background.go — backgroundManager.isLive, workflowAlreadyRunningFormat; internal/agent/recipe.go — recipeRefusal; internal/agent/workflowcall_test.go — workflowResponder, askedCount
**What:** fix — `openStatus` resumes any folder with a matching plan hash and only `RerunFailed` consulted `background.isLive`, so a blocking `fan_out` or `/<id>` re-issuing a live background plan ran a second Runner in the same folder (racing `status.json`, duplicate children).
**Goal:** on the top-level Agent, a blocking launch (fan_out plan, fan_out recipe form, typed `/<id>`, foreground `StartRecipe`) onto a folder `background.isLive` reports live is answered through its surface's own refusal wrapper with `workflowAlreadyRunningFormat` naming the background id; nothing is created, spawned or written, and no `WorkflowPhaseEvent` is emitted.
**Approach (assumed at the header base):** in blocking mode the builder sets `Runner.Admit` (item 12) to refuse an id `a.background.isLive` reports (a stopped-but-draining run counts as live); background mode sets none. workflowcall.go's top comment ("The same call again finds the stored workflow by its plan hash") adds that a folder a background run still drives is refused.
**Regression guard.** The test gates every item that starts (one-item plans), reads `status.json` only after the gate's started channel closes, and asserts each item route's `askedCount` unchanged across the re-issue; each gated route queues a second script (e.g. `finishScript`), since `workflowResponder` counts only consumed scripts. Delegates are item 14.
**Tests:** `TestBlockingLaunchRefusedOnALiveBackgroundFolder` (launch_test.go), one subtest per Goal surface: start a gated one-item bg run, re-issue; assert the texts (`fan_out could not run: …`, `fan_out could not run recipe <id>: …`, `recipe /<id> could not run: …`) contain the bg id, asked-counts unchanged, one workflow folder, `status.json` bytes untouched, no `WorkflowPhaseEvent` naming the bg id from the re-issue.
**Acceptance:** `go test ./internal/agent -run 'TestBlockingLaunchRefused|TestLaunch|TestBackground_RerunFailedRefuses|TestWorkflowCall_TheSameCall|TestRecipe_'`
**Commit:** `fix(agent): refuse a blocking launch onto a live background workflow folder`

## 14. agent: a delegate's blocking launch sees the root's background runs

**Depends on:** item 13.
**Files:** `internal/agent/agent.go`, `internal/agent/construct.go`, `internal/agent/subagent.go`, `internal/agent/background.go`, `internal/agent/launch.go`, `internal/agent/launch_test.go`
**Read first:** internal/agent/construct.go — seedTopLevel, delegation.seed; internal/agent/subagent.go — newChildAgentOn, defaultSubAgentTools; internal/agent/background.go — backgroundHost
**What:** fix — a delegate shares its parent's scratch folder but has its own empty background manager, so item 13's guard never fired for a sub_agent's or a background item child's blocking fan_out.
**Goal:** every Agent of a tree answers workflow liveness from the root's background manager, so a delegate's and a background item child's blocking fan_out onto a live background folder is refused as in item 13.
**Approach (assumed at the header base):** `Agent` gains `workflowLive func(id string) bool`: `seedTopLevel` sets it to `a.background.isLive`; the `delegation` value carries it, composed by `newChildAgentOn` from the parent's field and applied by `delegation.seed` on `newDelegateAgent`'s path; `backgroundHost` copies it into its `&Agent` literal. The builder's `Admit` reads `a.workflowLive`.
**Regression guard.** A grandchild inherits its parent's field, never `a.background` (empty on a delegate). Both subtests set `cfg.Delegation.MaxDepth = 2` (at depth 1 a child has no `fan_out`); the delegate subtest also registers `tools.NewSubAgent()` on `cfg.Tools` after `backgroundWorkflowConfig(t, sink)`.
**Tests:** `TestBlockingLaunchRefusedOnALiveBackgroundFolder` gains subtests delegate (a sub_agent's blocking fan_out of the parent's live bg plan) and bg item child (a bg run's item child, under `backgroundHost`, re-issuing another live bg plan), with item 13's assertions.
**Acceptance:** `go test ./internal/agent -run 'TestBlockingLaunchRefused|TestNewChildAgent_|TestBackground_|TestWorkflowCall_'`; `! grep -n 'background.isLive' internal/agent/launch.go`
**Commit:** `fix(agent): a delegate's blocking launch refuses a live background folder`

## 15. docs: workflow launch and tally amendments

**Depends on:** items 6, 7, 14.
**Files:** `docs/adr/0087-the-engine-runs-workflows-the-model-or-a-recipe-asks-for.md`, `docs/adr/0089-a-workflow-may-run-in-the-background-and-wakes-the-agent-when-it-ends.md`, `docs/adr/0090-workflow-stages-are-enterable-views.md`, `docs/adr/0088-cancel-settles-and-never-rewinds-finished-work.md`, `CONTEXT.md`, `docs/manual/workflows.md`, `docs/manual/headless.md`, `docs/manual/daemon.md`, `internal/tools/fan_out.go`
**Read first:** ADRs 0087 D4, 0088 D3, 0089 D3, 0090 D1/D5; docs/manual/headless.md — exit-code table; docs/manual/daemon.md — recipe verdict; internal/tools/fan_out.go — fanOutSpec
**What:**
**Goal:** every doc that states the old mechanism matches the tree: re-issuing a workflow whose folder a background run drives is refused; the tally is the engine's, and the note, the TUI totals line and the exit code count the same item set (fan-out items, latest round, verify/merge excluded); `fan_out`'s description ends "…skips the items already done; a call whose workflow a background run is still driving is refused."
**Approach (assumed at the header base):** dated amendment blockquotes in the house style (ADR 0087 D4, 0088 D3, 0089 D3, 0090 D1; 0088 D2 needs none); one sentence each in CONTEXT.md's Workflow/Background workflow entries and the three manuals; the clause appended to `fanOutSpec`'s last sentence (no test pins it).
**Regression guard.** Review each hit of `blocked on every item|every item blocked|plan hash|re-issu` (e.g. headless.md, ADR 0087, ADR 0088 D3's 2026-09-29 amendment) and fix those stating the old mechanism; a review list, not a zero-hit check. The ADR 0090 amendment says D5 holds (the record persists the end-phase tally; pre-change records count their items) and the TUI totals line omits the note's unfinished and verdict segments. Say the same item SET, never the same "items N" (the note's N is `Tally.Total()`, the TUI's OK+Partial+Blocked).
**Tests:** none (docs); `TestFanOut*` green.
**Acceptance:** `grep -c 'Amended 2026-09-30' docs/adr/0087-*.md docs/adr/0089-*.md docs/adr/0090-*.md docs/adr/0088-*.md` prints a non-zero count for each; `grep -c 'still driving' CONTEXT.md docs/manual/workflows.md` and `grep -c 'fan-out item' docs/manual/headless.md docs/manual/daemon.md` print a non-zero count for each; `grep -n 'a background run is still driving is refused' internal/tools/fan_out.go`; `go test ./internal/tools -run 'TestFanOut'`
**Commit:** `docs: amend ADRs and manuals for the one launch builder and the engine tally`

## 16. agent: a shared child-run lifecycle, the workflow spawner first

**Depends on:** item 14.
**Files:** `internal/agent/childrun.go`, `internal/agent/childrun_test.go`, `internal/agent/workflowspawn.go`, `internal/agent/workflowspawn_test.go`, `internal/agent/background_test.go`, `internal/agent/doc.go`
**Read first:** internal/agent/workflowspawn.go — workflowSpawner.Spawn; internal/agent/children.go — childRegistry.register, arm, disarm, childMailbox.close; internal/agent/subagent.go — runSubAgent
**What:**
**Goal:** one helper owns register → arm → Run → disarm → settle hook → stop verdict → optional fold for a child run; `workflowSpawner.Spawn` uses it with fold off; behaviour unchanged.
**Approach (assumed at the header base):** `runChild(ctx, childRun{registry, runID, sub, onArmed, settled, foldStopped}) (res, err, stopped, stopLeftover)` plus `reapChild(registry, runID, sub)`; `foldStopped func(ctx context.Context)` is nil for fold off (callers pass a closure over `a.foldStoppedChild`); the `stopped` predicate (`StatusCancelled` or faulted without `capFold`, and parent `ctx.Err()` or `errDelegationStopped` cause) moves in.
**Regression guard.** The helper does not recover (callers keep their recover frames), takes the registry explicitly (background workflow children live on the top-level Agent), leaves fold off for the spawner (a stopped item is re-run on resume) and returns the stop leftover. Order: `Submit` before register; disarm before any namer join; `stopRun(nil)` before the reap defer. `reapChild` unregisters, closes the mailbox and RETURNS the undelivered leftover, never reporting it (the spawner reports in its reap defer, `runSubAgent` after classification); with fold on the stop closes the mailbox, with fold off `reapChild` does (a second `childMailbox.close` yields nothing), and the leftover is nil. Item 17 never reshapes this signature.
**Tests:** `TestChildRun_*` verdict table (cancelled; faulted with/without capFold; parent ctx cancelled; `errDelegationStopped` cause; stop after disarm); `TestWorkflowSpawn_AHumanStopOfAnItemChildEndsItStopped` with an item undelivered-on-stop reported as `domain.UndeliveredCancelled` (`undeliveredWorkflowReason`); `TestBackground_AnItemChildStoppedThroughTheRootEndsStopped` (stopped through the ROOT Agent's `StopChild`; pins the explicit registry); a `reapChild` case: returns the leftover, reports nothing; all `TestWorkflowSpawn_*` green.
**Acceptance:** `go test ./internal/agent -run 'TestChildRun|TestWorkflowSpawn|TestBackground_|TestWorkflowCall_ACancelEndsItsPhasesStopped|TestStoppedFanOutAnswerCarriesResumeLine|TestWorkflowControl|TestDocMap'`; `grep -n 'runChild(' internal/agent/workflowspawn.go`; `! grep -n 'children.arm(\|errDelegationStopped' internal/agent/workflowspawn.go`
**Commit:** `refactor(agent): one child-run lifecycle, used by the workflow spawner`

## 17. agent: sub-agent delegation runs on the child-run helper

**Depends on:** item 16.
**Files:** `internal/agent/subagent.go`, `docs/adr/0083-the-standing-denials-of-the-architecture-reviews.md`
**Read first:** internal/agent/subagent.go — runSubAgent, classifyDelegation, foldStoppedChild, startDelegationNaming, delegationResult; internal/agent/stop_test.go — TestStopChild_AfterTheRunReturnedLeavesTheReportStanding
**What:**
**Goal:** `runSubAgent` owns only task composition, ledger and result shaping; its register/arm/Run/disarm/stop-verdict/fold block is one `runChild` call, with no hand-spelled copy of the `stopped` predicate left in the package.
**Approach (assumed at the header base):** `onArmed` = inherited-name emit + `startDelegationNaming`; `settled` = `stopNaming` + naming wait + `ledgerName`; `foldStopped` = a closure setting `runSubAgent`'s `stopped`, then calling `a.foldStoppedChild(ctx, runID, sub)`; post-construction writes (narrowing, output path, `stepCap`) stay in `runSubAgent`. ADR 0083 §3 stands; add a dated note there that the run protocol, not the runtime-state value, moved.
**Regression guard.** `ran = true` is the last act of `onArmed`, never after `runChild` returns, or a panic reads refused. The closure sets `stopped` before it folds, so a panicking fold still reads stopped. The reap defer stays namer-join → `reapChild` → `reportLeftover` capture, `stopLeftover` prepended to what `reapChild` returns. The predicate check greps `errDelegationStopped` (`stopped := ` already passes at base).
**Tests:** `TestStopChild_*`, `TestChildRegistry_*`, `TestUndeliveredReason_*`, `TestInterjectChild_*`, `TestRunDelegation_*`, `TestSubAgent_*`, `TestDelegationNaming_*`, `TestDelegationPhases_*`, `TestFanOut_*`, `TestNewChildAgent_*` green (the disarm-before-namer-join test pins the order).
**Acceptance:** `go test ./internal/agent -run 'TestStopChild_|TestChildRegistry_|TestUndeliveredReason_|TestInterjectChild_|TestRunDelegation_|TestSubAgent_|TestDelegationNaming_|TestDelegationPhases_|TestFanOut_|TestNewChildAgent_|TestDelegationLedger'`; `! grep -n 'errDelegationStopped' internal/agent/subagent.go internal/agent/workflowspawn.go`; `awk '/^## 3\./{s=1;next} /^## 4\./{s=0} s && /run protocol/' docs/adr/0083-the-standing-denials-of-the-architecture-reviews.md | grep .` (prints a line between the `## 3.` and `## 4.` headings)
**Commit:** `refactor(agent): runSubAgent runs its child through the shared lifecycle`
