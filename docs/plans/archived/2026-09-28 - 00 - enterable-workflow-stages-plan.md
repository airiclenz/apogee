# Enterable workflow stages — plan

**Goal:** A foreground Workflow stops painting its children's work inline. Its block shows one row per stage, and each row opens that stage's work full-screen the way a sub-agent run view does. The same run also fixes three filed beads, the /-dropdown click re-centre, and the missing context gauge during a workflow.
**Date:** 2026-09-28
**Status:** done (status corrected 2026-10-02)
**sized for:** ~200k-context host
**base:** 9fafafb1
**Sources:** `IDEAS.md` (Workflows / Sub-Agents lines 30, 33); `docs/adr/0063-sub-agent-runs-are-user-addressable-views.md`; `docs/adr/0086-a-delegation-is-stopped-singly-and-a-named-one-stays-continuable-for-the-session.md`; `docs/adr/0087-the-engine-runs-workflows-the-model-or-a-recipe-asks-for.md`; `docs/adr/0089-a-workflow-may-run-in-the-background-and-wakes-the-agent-when-it-ends.md`; `layout.md` ("The workflow block", "Run view", "The status line's right slot"); `docs/plans/archived/2026-09-27 - 00 - engine-run-workflows-plan.md` (items 22, 43)

**Ratified design calls** (owner, 2026-09-28):
- **Granularity:** a recipe block has one row per stage. A one-item stage opens its item's run view directly. A multi-item stage opens a *stage view* that lists one delegation-style row per item, each of which opens its run view. The breadcrumb reads `← main › <workflow> › <stage>[ › <item>]`, and `esc` goes up one level.
- **Pending stages:** every stage of the Plan is shown from the start. A stage that has not started is dim, reads `pending`, and has no ▶.
- **Scope:** the recipe block and the model-called `fan_out` card both get the new shape. The fan_out card shows one enterable row per item, since it has one stage.
- **Background workflows:** unchanged. They keep the status-line readout and `/workflows`.
- **Resume:** the workflow structure is persisted, so rows reopen read-only after a resume.
- **Results:** each item's receipt summary appears on its row in the stage view. The main block keeps a line for each item whose receipt is not `ok`, plus the finish/totals line.
- **Facade opt-in (`apogee-facade-background-optin`):** one `domain.Config` bool that `defaultRoster` hands to `tools.HostToolsOf`. It defaults to false.
- **Gauge (IDEAS line 30):** until the first depth-0 reading, the gauge shows the engine's estimate of the next request, prefixed `~`.
- **Reversal:** this plan supersedes the engine-run workflows plan's item 43 rule that the block "paints one way and never collapses … a head with many runs has no run view". ADR 0090 records the change (item 10).
- **Repeat:** a repeat stage's target keeps one row (`round n/m`); its stage view groups items under `round N` sub-headers (owner, 2026-09-28).
- **Retries:** an item keeps one row that opens its latest attempt; earlier attempts are dim `attempt N` sub-rows in the stage view, each openable (owner, 2026-09-28).

**Regression check (2026-09-28, 9fafafb1):**
- 1: recast — ItemStarted from `workflowSpawner.Spawn` after the mint, Round/Attempt on the events; guards folded (headless silence, `StageEvent.Items`, stage names via `observeWorkflow`, the two phase-sequence tests, facade constants).
- 2: guard folded — the note rides the item's phase result and the call's answer, never the Receipt; yields to ADR 0069 D9.
- 3: guard folded — tests lift `fan_out`/`workflow`; every "Config does not carry offersBackground" comment is rewritten.
- 4: recast — item heads seated at the workflow block's depth, the span covers them, `/usage` keeps its per-workflow row, one head per run with one row per item.
- 5: recast — a repeat stage's target keeps one `round n/m` row; guards folded (the `failed:` cause and ask question stay, text paint when there is no view, the view reaches the painter via `paintInput`, the span is skipped only for a block with a view). It supersedes the `workflowblock.go` header comment and the `entrykind.go` entryWorkflow row comment.
- 6: recast — `round N` sub-headers and `attempt N` sub-rows in the stage view; guards folded (a zero-value `runView` means no stage, the stage level's identity, the crumb source, `setRoot` kept, the choice keyed on Items, the Goal's tests). Amends ADR 0063 D4 through item 10.
- 7: guard folded — restored views never match `workflowAt`, interrupted item heads and stages are closed, and the Workflow record is stripped.
- 8: recast — the estimate is taken synchronously on the Update goroutine; `lateEngine` and `liveStats` guards folded; the idle-only rule of `internal/context/budget.go` and `contextcost.go` stands.
- 9: guard folded — ask/approval specs, `popupPaneHit` returns `start` for all seven handlers, pinned cases on `popupRowSeat`, and twin-click checks for every pane. Supersedes the `popup.go` rowTop comment and `autocomplete.go`'s "scrolls around the selected item".
- 10: guard folded — Files cover every file the Rule's grep hits, the gauge grep is widened, and the workflows manual page is named and checked.
- 11: guard folded — the Goal counts against README.md, and the IDEAS.md ticks are checked.
- 12: guard folded — the fixture pins no `context-window:`.
- 1 (re-check): guards folded — `ItemSpec.Index` and a `RepeatRound` on spec and events (domain Round = RepeatRound + 1), Attempt counted per Spawn by the observer, the Run map keyed by (stage, repeat round, index) and never by `Key`, the Goal's unpaired events; StageStarted carries `Rounds` (decision). Supersedes `encode.go`'s "a member added to an Event variant is a member added here under the same name" comment.
- 3 (re-check, plan-wide decision): every Acceptance runs `go test` on one package per command line; items 1, 3, 4, 7 and 8 are split (AGENTS.md: a raw multi-package `go test` is unsupported).
- 4 (re-check): guards folded — an item head is placed at `runEnd` of its item run, `done` is set at SubAgentFinished and ItemFinished, the Approach's `depth = parent+1` and `ownHeads`/`subAgentHeads` bullets are struck; yields to `layout.md`'s "a host note arriving mid-run waits below the last of it" (pinned by `assertHeadsItemRuns`).
- 5 (re-check): decision — `round n/m` reads m from item 1's `Rounds` (`round n` at 0); the comment-rewrite rule rides item 10's widened grep.
- 1 (writer, post re-check): `Rounds` pinned as total rounds including the own run (cap + 1), so `round n/m` has n = domain `Round` ≤ m.
- 6 (re-check): guards folded — one single-stage predicate keys both the choice and the crumb, "-1 means none" is struck, every `viewedRun()`/`inRunView()`/`viewedChild()` reader (`statusLeft` included) gets a stage-level answer.
- 8 (re-check): guards folded — `enter dismiss` outranks the estimate, the no-usage-server behaviour is stated, and the estimate is `ContextCost` plus the history term.
- 10 (re-check): decision — the Rule's grep is widened to "never elides|never hides what it heads|no head of its own".

**Standing requirements:**
- skills: coding-standards
- Bubble Tea v2 names only (`tea.KeyPressMsg`, `msg.String()`). Never hold a `strings.Builder` by value in the Model (ADR 0011).
- Any deviation from item text is recorded as a dated NOTES line under the item.
- Every item's Acceptance runs `go test` on ONE package per command line, joined with `&&` (AGENTS.md: a raw multi-package `go test` is unsupported).

**Out of scope:**
- Background workflow rendering, and live views opened from `/workflows`.
- `^x` on a stage row. A stage is not a run. Stopping a whole workflow stays `/workflows` `^x`.
- Headless and daemon output of the new workflow phases, which stay silent there.

## 1. Workflow phase events carry the workflow's structure and each item's run id — ✅ DONE (2026-09-28)

NOTES (2026-09-28): observeWorkflow now takes the whole workflow.Plan (name and stage names, plus the repeat caps `Rounds` is derived from) in place of the name string, and it hands itself to the Runner's Spawner when that is a *workflowSpawner. The spawner reports ItemStarted through it right after the run id is minted.
NOTES (2026-09-28): `Rounds` is computed by the observer from the plan (the repeat stage's `max:` + 1, the larger when two repeats name one stage), not carried on workflow.StageEvent. `Attempt` counts the runs that were minted, so a Spawn refused before the mint neither counts nor starts a row. ItemFinished names the last minted run ("" when none) and also carries its Attempt.
NOTES (2026-09-28): setStagePhase/notifyStage/notifyItem take the stage index (RepeatRound is read from status.Stages[i].Round), and setStagePhase takes the item count, 0 at every caller except runItems. The runner-level tests (repeat, retry, continuation, shared key, pre-mint refusal) sit in workflowspawn_test.go and drive a Runner directly. The fan_out, recipe and cancel sequences sit in workflowcall_test.go and are checked structurally (assertWorkflowShape), so they hold however the items interleave.

**What:** Recast at the regression check (2026-09-28).
**Goal:** A Driver can rebuild a Workflow's shape from `domain.WorkflowPhaseEvent` alone:
- the stage names, in order, at start;
- each stage's item count when that stage starts;
- a started and a finished event per item run, each naming the item child's `RunID`, except where the re-check guard's unpaired events say otherwise;
- a stage-finished event.

Headless and daemon output stays as it is.

**Regression guard.** WorkflowItemStarted is emitted from workflowSpawner.Spawn after the run id is minted (never from ItemPhase(PhaseRunning), which fires before the mint), before the child's SubAgentPhaseEvent Started; ItemStarted and ItemFinished also carry `Round int` (the repeat round of the stage, 1-based) and `Attempt int` (1-based; every retry or continuation Spawn is a new attempt with a new Run); StageStarted/StageFinished carry Round; consumers key a stage by (name, round), never by "current stage" — test a repeat stage and a retried item.
Also binding: `eventjson.Encode` returns ok=false for `WorkflowItemStarted` and `WorkflowStageFinished`, so `apogee run --format json` stays as it is (the item yields to `docs/manual/headless.md`'s `workflow_phase` phase list). `workflow.StageEvent` gains `Items`, set by `runItems` (0 for script/ask/pick/repeat stages). `observeWorkflow` takes the plan's stage names from all three callers (`workflowcall.go`, `recipe.go`, `background.go`) and from the direct call in `TestWorkflowObserver_ReportsAWaitingQuestionAndAFailure`. A resumed item's ItemFinished carries Run "" and precedes StageStarted, because `prepareItems` notifies first. `apogee.go`'s phase const block and `example_test.go` gain both new constants.

**Regression guard (re-check).**
- **Rounds.** `workflow.ItemSpec` gains `Index` (set in `runItem`) and `RepeatRound`; `StageEvent` and `ItemEvent` gain `RepeatRound`, read from `status.Stages[stageIndex].Round` (0 = the stage's own run), never `ItemEvent.Round`, which is the continuation round. The domain `Round` is `RepeatRound + 1`, so the own run is 1. StageStarted for a repeat stage's target also carries `Rounds int` — the total rounds the target can run, its own run included (the repeat's cap + 1), so the domain `Round` never exceeds it (0 when the stage does not repeat or has no cap); item 5 renders `round n/m` when Rounds > 0 and `round n` otherwise.
- **Attempt.** The domain `Attempt` is the observer's own Spawn count per (stage, repeat round, index). A continuation keeps `spec.Attempt`, but it is still a new attempt with a new Run.
- **Run map.** The observer keys its Run map by (stage, repeat round, index), never by `Key`: `ItemKey` hashes brief, label and units, and a fanout list is not deduplicated, so two items can share one Key. This overrides the Approach's "keyed by the item's `Key`".
- **Unpaired events.** A stopped item gets ItemStarted and no ItemFinished. A Spawn that fails before the mint gets ItemFinished with Run "" and no ItemStarted. A skipped stage, or a replayed script/ask stage, gets StageFinished with no StageStarted.
- **Encode comment.** `encode.go`'s comment "a member added to an Event variant is a member added here under the same name" is superseded; rewrite it to name the exception (the new fields are not in `workflowPhaseData`).

**Approach (assumed at the header base):**
- In `internal/domain/events.go`, add `WorkflowItemStarted` and `WorkflowStageFinished` to the phase set.
- Add `Stages []string` (filled on `WorkflowStarted`), `Items int` (filled on `WorkflowStageStarted`) and `Run string` (the item child's RunID, filled on `WorkflowItemStarted` and `WorkflowItemFinished`).
- `workflowObserver.ItemPhase` (`internal/agent/workflowcall.go`) currently drops `PhaseRunning`. Make it emit `WorkflowItemStarted`.
- The child's RunID is minted in `workflowSpawner.Spawn` (`internal/agent/workflowspawn.go`). Pass it back to the observer, keyed by the item's `Key`, so the started event comes before the child's `SubAgentPhaseEvent` Started and both carry the same RunID.
- `fan_out` goes through the same observer, so it gets the same events.
- Every consumer that switches on `WorkflowPhase` must treat the new phases as no-ops wherever output would otherwise change. Grep `WorkflowStageStarted` to find them: the headless printer, the daemon, and `internal/tui`'s `addWorkflowPhase`.

**Files:** internal/domain/events.go; internal/agent/workflowcall.go; internal/agent/workflowspawn.go; internal/agent/recipe.go; internal/agent/background.go; internal/workflow/runner.go; internal/workflow/stages.go; internal/eventjson/encode.go; internal/eventjson/encode_test.go; apogee.go; example_test.go; internal/agent/workflowcall_test.go; internal/agent/workflowspawn_test.go
**Read first:** internal/agent/workflowcall.go — workflowObserver, observeWorkflow, StagePhase, ItemPhase, startLocked, emitLocked; internal/agent/workflowspawn.go — workflowSpawner.Spawn, newWorkflowSpawner;
internal/workflow/runner.go — ItemSpec, StageEvent, ItemEvent, runItems, prepareItems, runItem, notifyStage, notifyItem; internal/workflow/stages.go — runStage, runRound, startRound, settleStage, replayStage, runRepeat;
internal/eventjson/encode.go — Encode, workflowPhaseData; internal/agent/workflowcall_test.go — TestWorkflowCall_EmitsItsPhases, TestWorkflowCall_ACancelEndsItsPhasesStopped, TestWorkflowObserver_ReportsAWaitingQuestionAndAFailure; internal/run/run.go — eventTap.noteWorkflowPhase

**Tests:**
- A recipe run with two stages (1 item, then 3 items) records this event order in `workflowcall_test.go`:
  - Started, with Stages;
  - StageStarted, with Items;
  - one ItemStarted per item, whose Run equals the RunID on the matching `SubAgentPhaseEvent` Started;
  - ItemFinished, with Run;
  - StageFinished;
  - Finished.
- A fan_out call records the same event order.
- A repeat stage's target reports StageStarted/StageFinished once per round with Round 1, 2, …, and its StageStarted carries the repeat's `Rounds`; a retried item reports one ItemStarted per attempt (Attempt 1, 2, each with its own Run), and its ItemFinished names the last attempt's Run.
- A capped item continued once reports two ItemStarted events with Attempt 1 and 2 and different Runs, and its ItemFinished names the continuation's Run.
- A fanout over a list with a repeated entry (two items sharing one Key) reports each item's ItemFinished with its own Run.
- A Spawn that fails before the mint (e.g. the depth limit) reports ItemFinished with Run "" and no ItemStarted.
- `TestWorkflowCall_EmitsItsPhases` and `TestWorkflowCall_ACancelEndsItsPhasesStopped` are updated to the new phase sequence; the cancel case asserts ItemStarted with no ItemFinished for the stopped item.
- An `encode_test.go` case: `Encode` returns ok=false for `WorkflowItemStarted` and `WorkflowStageFinished`.

**Acceptance:** `go build ./... && go test -race -count=1 ./internal/agent/ && go test -race -count=1 ./internal/domain/ && go test -race -count=1 ./internal/workflow/ && go test -race -count=1 ./internal/eventjson/ && go test -race -count=1 . && go test -race -count=1 -run TestHeadlessRecipe ./cmd/apogee/`

**Commit:** `feat(workflow): phase events carry the stage list and each item's run id`

## 2. A workflow item that falls back from the sub-agents server says so — ✅ DONE (2026-09-28)

NOTES (2026-09-28): the fallback is read off the built child (`sub.seatFallback`, decided once in newChildAgentOn) and recorded on the spawner (`workflowSpawner.fellBack`, atomic since items spawn concurrently); `workflowAnswer` gains a `fellBack bool` parameter read through `seatFellBack(runner)`, and `runRecipe` now returns it as a second value so both recipe launches pass it on. The Receipt is untouched.
NOTES (2026-09-28): consequential edit — internal/agent/agent.go: made necessary by the note now also being appended by workflowPhaseResult and workflowAnswer (the `seatFallback` field comment named delegationResult alone).
NOTES (2026-09-28): a background workflow's end message (background.go) does not go through workflowAnswer and so carries no answer-level note; its items' phase results do carry it. Left as the plan scoped it (workflowAnswer only).

**What:**
**Goal:** A workflow item that asked for `sub-agents-server`, and ran on the session server because nothing was latched, carries `SeatFallbackNote` as a body note in its result. This is the same note `sub_agent` appends. An item that ran on the server it asked for carries no note.

**Regression guard.** The note is never written into the Receipt, because `receipt.json` replays on resume and feeds merge manifests and `when:`. When `sub.seatFallback` is set, append it to `workflowPhaseResult`'s content, which Drivers read. Because ADR 0069 D9 says "the note is for the MODEL" (the item yields to it), also add ONE note line to the call's answer through `workflowAnswer`. `recipe.go` wraps that function via `recipeResultFormat`, so list `workflowcall.go` and `recipe.go`. Only the fallback test must bite. The latched test is a non-regression guard.

**Approach (assumed at the header base):**
- `workflowSpawner.Spawn` builds the child with `newChildAgentOn(s.seat, …)`. Find out whether the seat fell back, the way `subagent.go`'s delegation path decides it before appending `SeatFallbackNote`.
- Append the note to the item's result body at the one place the item's result/receipt text is assembled.

**Closes:** apogee-workflow-seat-fallback-silent
**Depends on item 1** (same files, serial).
**Files:** internal/agent/workflowspawn.go; internal/agent/workflowcall.go; internal/agent/recipe.go; internal/agent/workflowspawn_test.go
**Read first:** internal/agent/workflowspawn.go — workflowSpawner.Spawn, workflowPhaseResult; internal/agent/subagent.go — SeatFallbackNote, newChildAgentOn (seatFallback), delegationResult;
internal/agent/workflowcall.go — workflowAnswer, fanOutSeat; internal/workflow/format.go — receiptText; internal/agent/seat_test.go — spawnOn;
internal/agent/workflowspawn_test.go — newWorkflowParent, spawnItem, TestWorkflowSpawn_BracketsTheChildWithPhaseEventsUnderTheCall

**Tests:**
- An item routed to `sub-agents-server` with no latched seat carries `SeatFallbackNote` in its `SubAgentFinished` `Result.Content` (in `sink.events`) and in the call's answer. This test fails against the pre-item tree.
- A latched seat, set with `SetDelegationTarget` and a dialer seam (see `routingParent`/`routedTarget` in `routedspawn_test.go`), produces no note. This is a non-regression guard and passes at base too.

**Acceptance:** `go test -race -count=1 -run 'Workflow|Spawn' ./internal/agent/`

**Commit:** `fix(workflow): an item that falls back from the sub-agents server carries the seat-fallback note`

## 3. The facade can opt into background fan_out and the workflow tool — ✅ DONE (2026-09-28)

NOTES (2026-09-28): consequential edit — internal/tools/registry_test.go: made necessary by the Config.OffersBackground field (TestHostToolsOfLeavesSeatChoiceToTheCaller's comment said Config does not carry the policy; the test now also sets Config.OffersBackground opposite to the argument to pin that the argument alone decides)
NOTES (2026-09-28): consequential edit — internal/agent/workflowcall_test.go: made necessary by the Config.OffersBackground field (TestWorkflowCall_TheFacadeRosterOffersNoBackground's comment now names the field as unset)
NOTES (2026-09-28): consequential edit — cmd/apogee/wire_firing.go: made necessary by the Config.OffersBackground field (the firing comment's "exactly as the engine's own roster does on the nil path" now says the firing Config leaves the field unset)
NOTES (2026-09-28): HostToolsOf keeps its three-argument signature and does not read cfg.OffersBackground itself; defaultRoster passes the field as the third argument, as the Approach says. workflow_test.go gains an "embedder's opt-in" case alongside the rewritten comment.

**What:**
**Goal:** An embedder that sets the new `domain.Config` field on an engine that uses its own roster gets fan_out's `background` field and the `workflow` tool. Leaving the field unset gives exactly today's roster. The field also survives a model rebind, which re-composes the roster.

**Regression guard.** `fan_out` and `workflow` are default-off, so the Goal holds only where the roster lifts them. Both tests lift them through `EnabledTools`, as `TestWorkflowCall_TheFacadeRosterOffersNoBackground` does, and set `WorkspaceDir`. The facade test observes the menu through a stubllm Server's `Requests()[i].Tools`, following the `benchreadiness_test.go` pattern. Rewrite every comment that says Config does not carry offersBackground or that the engine passes it false: `grep -rn 'offersBackground false\|Config does NOT carry\|rather than a Config field' internal/`.

**Approach (assumed at the header base):**
- Add `OffersBackground bool` to `domain.Config`, with a doc comment that cites ADR 0089 D1: only a Driver with a conversation to go on sets it.
- `defaultRoster` (`internal/agent/construct.go`) passes `cfg.OffersBackground` as `tools.HostToolsOf`'s third argument. Rewrite the comment above it.
- `apogee.Config` is an alias, so the facade needs no new export. Add a line about it to the facade's package doc in `apogee.go`.

**Closes:** apogee-facade-background-optin
**Files:** internal/domain/config.go; internal/agent/construct.go; internal/tools/registry.go; internal/tools/workflow_test.go; internal/agent/construct_test.go; apogee.go; apogee_test.go
**Read first:** internal/agent/construct.go — defaultRoster, resolveTools, composesDefaultRoster; internal/tools/registry.go — HostToolsOf, HostTools.OffersBackground, backedTools, builtinToolsWith;
internal/agent/setprofile.go — applyRoster; internal/agent/workflowcall.go — offersBackground; internal/agent/workflowcall_test.go — TestWorkflowCall_TheFacadeRosterOffersNoBackground;
internal/agent/construct_test.go — TestHostToolsThreadsTheSkillLookupOntoTheDefaultRoster; benchreadiness_test.go — stubllm Server.Requests; internal/domain/config.go — Config

**Tests:**
- `construct_test.go`: with `fan_out` and `workflow` lifted, the default roster lists `workflow` and fan_out's `background` property only when the field is set, and still does after `SetProfile`/rebind.
- `apogee_test.go`: a facade engine built with the field set and `fan_out`/`workflow` lifted publishes the `workflow` tool, as seen in the stubllm server's `Requests()[i].Tools`.

**Acceptance:** `go test -race -count=1 ./internal/agent/ && go test -race -count=1 ./internal/tools/ && go test -race -count=1 . && go vet ./...`

**Commit:** `feat(apogee): the facade can opt into background fan_out and the workflow tool`

## 4. Each workflow item run gets its own run head, shown as an enterable delegation row — ✅ DONE (2026-09-28)

NOTES (2026-09-28): the item head is `entryWorkflowItem`, persisted as `workflow_item`. `session.Entry` gains `Item *WorkflowItem` (stage, round, index, attempt). The card rides the existing `Tool` slot. The receipt status is kept as the card's stat, and the verdict (ok = succeeded, blocked = failed) is re-derived from it on decode (fromWireWorkflowItem). The wire-member enumeration in transcriptbridge_test.go now lists `Item`.
NOTES (2026-09-28): entryWorkflowItem is never paint-cached (entrykind.go row, paintcache.go banner). The receipt rewrites the card after the done/phase bits the key reads may already have settled at SubAgentFinished. It also carries no block state, since its row opens only the run view.
NOTES (2026-09-28): the card has no tool name and borrows sub_agent's registry label and verb. Its Target and agentName are the item label, and it has no task row because the phase carries no instructions. An item head is seated only when a block in the view heads the Workflow (spanHeadAt), and never for a background phase.
NOTES (2026-09-28): an item keeps one row because the paint walk steps over every head a later attempt of the same (run, call, stage, round, index) superseded (retiredAttempts in workflowblock.go, used in renderView). A retry's head is placed at the end of the Workflow's span, so the item's one row moves to the end of the block's rows.
NOTES (2026-09-28): inFlightFanOut now finds its latest depth-0 head with subAgentHeads instead of headsRun. This keeps its count unchanged now that item heads also answer headsRun. continuesOpenRun accepts an open item head as the enclosing block.
NOTES (2026-09-28): consequential edit — internal/tui/doc.go: made necessary by the new entryWorkflowItem head (the workflowblock.go line of the file map said the block heads the item runs directly)

**What:** Recast at the regression check (2026-09-28).
**Goal:** Every item child of a foreground workflow or fan_out has its own run-head entry inside the workflow head's span. This entry:
- keeps that child's entries together behind it;
- paints as a delegation row (name, ✓, cells, verdict, ▶) with its span left out of the parent's paint;
- opens the existing run view by click or by `⌥↑/⌥↓` then `⏎`;
- supports `^x` stop, prompt-box interject, and the per-run gauge, just as a `sub_agent` run does.

Background workflows are unchanged.

**Regression guard.** The item head is seated at the workflow block's depth with callID=Call and spawnRunID=Run (not `depth = parent+1`), and `subAgentSpan` is extended so a workflow head's span covers its following same-depth item heads and their spans. Item heads take the per-run fill, but totals still reach `foldWorkflowSpend`, and `delegateUsageHeads` excludes item heads (add a /usage test feeding WorkflowItemStarted). `ownHeads`/`subAgentHeads` stay keyed on the tool name, so adjacent item heads never form a "✦ Sub-Agent (N)" group and the fan_out card's item rows are its body. Add `paintcache.go` and `transcriptbridge.go` (`toWireEntry`/`fromWireEntry` for `workflow_item`, new strings stripped in session `stripEntry`) to Files.
Ratified by the owner 2026-09-28 ("Retries" call): one head per run, but an item keeps ONE row. The receipt folds by Run onto the latest attempt's head, and the row opens the latest attempt. Earlier attempts' heads close on their SubAgentFinished phase and paint only in the stage view as dim `attempt N` sub-rows under the item row, each still openable.

**Regression guard (re-check).**
- **Placement.** An item head is inserted at `runEnd` of the ITEM's run (`spanHeadAt` falls back to `headsWorkflowRuns`, i.e. the workflow head and its extended span), never at the end of the run the head itself sits in, so a later stage's item head stays inside the span after a mid-run host note. The item yields to `layout.md`'s "a host note arriving mid-run waits below the last of it" (pinned by `assertHeadsItemRuns`).
- **Done.** An item head never pairs a ToolResultEvent, so it sets `done` at its SubAgentFinished phase and at ItemFinished. A retried item's earlier head then replays done after save and resume, instead of reading `scheduled` with `^x stop` (`stoppable`).

**Approach (assumed at the header base):**
- Add a new entry kind, `entryWorkflowItem`, persisted as `session.EntryKind` `workflow_item`. It is inserted when `transcript.addWorkflowPhase` folds `WorkflowItemStarted`. It holds `spawnRunID = Run`, the item label as the run name, and stage/index. It folds `WorkflowItemFinished`'s receipt as its report (summary as gist, status as verdict).
- The run-head predicates accept it: `entry.headsRun`, `runHeadAt`, `headsRunFor`, and `subAgentFramed`. That makes placement (`runEnd`), elision (`resolveBlock`), `openRunAt`, `breadcrumbTrail`, `viewedChild`, `stoppable`, `childPhaseOf` and `applyUsage` work unchanged.
- Leave `headsWorkflowRuns` as the fallback head only for events that arrive before their item head.
- `renderSubAgentRun` paints the row. Its task row (`targetTask`) is the item's instructions when the event carries them, and is otherwise omitted.

**Depends on item 1.**
**Files:** internal/tui/workflowblock.go; internal/tui/transcript.go; internal/tui/subagentblock.go; internal/tui/render.go; internal/tui/paintcache.go; internal/tui/transcriptbridge.go; internal/tui/usage.go; internal/tui/entrykind.go; internal/session/transcript.go; internal/tui/workflowblock_test.go; internal/tui/runview_test.go; internal/tui/usage_test.go; internal/tui/transcriptbridge_test.go; internal/session/transcript_test.go
**Read first:** internal/tui/workflowblock.go — addWorkflowPhase, workflowAt; internal/tui/transcript.go — place, runEnd, tailBeforeHostNotes, continuesOpenRun, entry.headsRun, headsRunFor, headsWorkflowRuns, applyUsage, foldWorkflowSpend, addSubAgentPhase; internal/tui/subagentblock.go — subAgentSpan, spanHeadAt, runHeadAt, insideCollapsedRun;
internal/tui/usage.go — delegateUsageHeads; internal/tui/transcriptbridge.go — toWireEntry, fromWireEntry; internal/session/transcript.go — Entry, stripEntry;
internal/tui/workflowblock_test.go — feedTwoItems, assertHeadsItemRuns, itemBase, TestFanOutCardHeadsItsItemRuns; internal/tui/runview.go — openRunAt, stoppable, childPhaseOf

**Tests:**
- In `workflowblock_test.go`, replace `TestWorkflowBlockHeadsItsItemRuns` and `TestFanOutCardHeadsItsItemRuns` with tests that assert item output is not painted at top level, and that each item row wears ▶.
- Each item's entries land in arrival order inside its head's span, and a fan_out card paints no "✦ Sub-Agent (N)" group header over its item rows.
- `feedTwoItems`' mid-run host note, then a second stage's ItemStarted: the new item head still lands inside the workflow head's span, and the note stays below it.
- A retried item (two attempts) keeps one row. The receipt folds onto the latest attempt's head, the row opens the latest attempt, and the earlier head closes on its SubAgentFinished phase.
- In `runview_test.go`, opening an item row roots the view at that child. `^x` calls `StopChild(Run)`, and `⏎` calls `InterjectChild(Run, …)`.
- In `usage_test.go`, a workflow fed WorkflowItemStarted still reports one per-workflow /usage row and no per-item sub-agent row.
- A `workflow_item` entry round-trips through `toWireEntry`/`fromWireEntry` with its name, verdict, stage and index. Its new strings are stripped in `stripEntry`. A retried item's earlier head replays done.

**Acceptance:** `go test -race -count=1 ./internal/tui/ && go test -race -count=1 ./internal/session/`

**Commit:** `feat(tui): each workflow item run has its own head and opens as a run view`

## 5. The recipe block paints one row per stage — ✅ DONE (2026-09-28)

NOTES (2026-09-28): re-derived from "StageFinished tells how the stage ended". The phase carries no outcome, so the TUI works it out: a stage that ended with fewer finished items than its `Items` reads `stopped`, a stage still running when the Workflow fails or stops reads `failed`/`stopped`, and any other ended stage reads `done`. A merge stage that fails on a missing report and a skipped stage both read `done` (see DEFER).
NOTES (2026-09-28): a started item also moves a pending stage row to running, and a StageFinished for the stage an ask question waits in clears the question, so the header stops reading `waiting for you` once the answer is in. The `round n/m` prefix shows on every round after the first, including once that round has ended.
NOTES (2026-09-28): TestWorkflowItemRowOpensAsARunView (runview_test.go) and TestARetriedItemKeepsOneRow both entered an item row under a Recipe block, and this item hides that row, so both now use a fan_out card, whose item rows stay. Stage-row entry is item 6's work. TestWorkflowBlockSeatsARunHeadPerItem now checks stage rows on the live block and item rows on the reopened, view-less block, and no longer checks that the two paints are equal.
NOTES (2026-09-28): consequential edit — internal/tui/doc.go: made necessary by the live Recipe block painting stage rows in place of its item heads.
NOTES (2026-09-28): consequential edit — internal/tui/runview_test.go: made necessary by the live Recipe block hiding its item rows at the top level.
NOTES (2026-09-28): transcript.go's headsWorkflow comment ("it never elides what it heads") is now stale for a live Recipe block. It is left to item 10's widened comment grep, as the plan's re-check assigns.

**What:** Recast at the regression check (2026-09-28).
**Goal:** A recipe workflow's block paints these rows, in order:
- the `✦ Workflow <name> — <state>` header;
- one row per Plan stage (`┝`/`┕`, the name, ✓ when every item is `ok`, `⋯` leader, then `pending` (dim, no ▶), `running` (`n/m · running` when the stage has more than one item), `waiting for you`, `done`, or the failed or stopped word, and ▶ once any item has started);
- one line per item whose receipt is not `ok` (`stage · item — status — summary`);
- the totals line when it ends.

Item heads are hidden at top level. The fan_out card keeps item 4's item rows.

**Regression guard.** Ratified by the owner 2026-09-28 ("Repeat" call): a repeat stage's target keeps one row in the block, reading `round n/m · <state>` while it repeats. Stage state is keyed by (name, round).
Also binding:
- A failed workflow's `failed: <cause>` line and an ask stage's waiting question stay among the painted rows.
- A block with no view paints from its text. `TestWorkflowBlockSurvivesTheRecord`'s live-equals-replayed equality is rescoped in this item.
- The view reaches `renderWorkflowBlock` through a `workflowView` field on `paintInput`, filled in `entry.painted`'s literal.
- The workflow block's span is skipped only for a block that carries a view. A view-less block, such as an old record, keeps painting its span.
- This supersedes the `workflowblock.go` header comment ("the painter draws the text alone") and the `entrykind.go` entryWorkflow row comment; rewrite both.

**Regression guard (re-check).** `round n/m` reads its m from item 1's Rounds field (`round n` when it is 0).

**Approach (assumed at the header base):**
- `workflowView` (`workflowblock.go`) gains a stage list, seeded from `Stages` and updated by StageStarted, StageFinished and the item phases.
- `renderWorkflowBlock` composes the rows with the same leader and indicator helpers the sub-agent group rows use (`toolleader.go`, `indicatorRow`, `glyphBranch`/`glyphBranchLast`/`glyphLeaderDot`/`glyphDone`/`glyphCollapsed`).
- Each stage row emits a new `lineMark` kind, `targetStage`, carrying the entry index and the stage index (`blocktarget.go`).
- The block's text (the record) stays the plain summary. The paint comes from the view.

**Depends on item 4.**
**Files:** internal/tui/workflowblock.go; internal/tui/render.go; internal/tui/paintcache.go; internal/tui/entrykind.go; internal/tui/blocktarget.go; internal/tui/workflowblock_test.go; internal/tui/blocktarget_test.go
**Read first:** internal/tui/workflowblock.go — workflowView, workflowView.fold, workflowView.text, renderWorkflowBlock; internal/tui/render.go — renderEntryLines (entryWorkflow case), resolveBlock, renderView (mark→lineTarget);
internal/tui/paintcache.go — paintInput, entry.painted; internal/tui/blocktarget.go — lineMark, lineTarget, targetKind; internal/tui/blockcursor.go — cursorStops;
internal/tui/workflowblock_test.go — TestWorkflowBlockShowsProgressAndResultLines, TestWorkflowBlockNamesHowItEnded, TestWorkflowBlockSurvivesTheRecord, workflowPaint

**Tests:**
- Stage rows for pending, running `2/3`, done, and failed.
- A repeat stage's target keeps one row that reads `round 2/3 · running` while it repeats (StageStarted with Rounds 3), and `round 2 · running` when Rounds is 0.
- The trouble line appears only for items that are not `ok`, and `…ListsOnlyTroubleOnALargeRun` still holds.
- `targetStage` marks are emitted on stage rows only.
- `TestWorkflowBlockShowsProgressAndResultLines` and `TestWorkflowBlockNamesHowItEnded` are rewritten to the new row set. The `failed: disk full` line and the ask question still paint.
- `TestWorkflowBlockSurvivesTheRecord` is rescoped: a view-less (replayed) block paints from its text and its span.

**Acceptance:** `go test -race -count=1 ./internal/tui/`

**Commit:** `feat(tui): the workflow block paints one row per stage`

## 6. A stage row opens a stage view or, for a one-item stage, the item's run view — ✅ DONE (2026-09-28)

NOTES (2026-09-28): the stage level is `runView.stage stageLevel{call, place}` (the stage index is stored as place = stage+1, and the block is found by its call in the level's run) rather than a bare `stage int`. At a stage level, `runView.ref` is the run the workflow block stands in. So `viewedRun()` answers that run (the conversation for a Recipe), and `statusLeft`/`shownSlot`/`isStalled` speak for it and name the working item. `viewedChild()` answers none, so there is no gauge, no ^x and no child legend. `runLabel` is asked only where the new `inRunScope()` holds. /inspect and the thinking pane now gate on `inRunScope()` instead of `inRunView()`. The transcript carries the stage beside `root` through a new `setStage` touch-writer. `setRoot` keeps its signature.
NOTES (2026-09-28): ⏎ at a stage level is refused with a flash naming the stage (`stage <name> is not a run — open one of its items to message it`). It writes no transcript note and keeps the draft. The box legend reads `stage <name> · read-only · esc back`. `blockKey` now takes the `paintRoot`, so the key names both the root and the stage.

**What:** Recast at the regression check (2026-09-28).
**Goal:** Opening a stage row, by click or by block cursor and `⏎`, does one of three things:
- a stage with exactly one started item opens that item's run view;
- a stage with several items pushes a *stage view* level;
- a pending stage opens nothing.

The stage view:
- paints the breadcrumb band `← main › <workflow> › <stage>` and one delegation row per item (item 4's rows, including the receipt summary);
- lets each row be opened by click or cursor;
- makes the prompt box read-only (`esc back`);
- goes up one level on `esc` or a breadcrumb click;
- shows no gauge.

An item opened from a stage view has the crumb `← main › <workflow> › <stage> › <item>`. One opened directly has `← main › <workflow> › <stage>`.

**Regression guard.** The stage view groups a repeated stage's item rows under `round N` sub-headers, oldest first. A retried item's earlier attempts paint as dim `attempt N` sub-rows under its row, each openable. A one-item stage with several rounds or attempts opens the stage view, not a run.
Also binding:
- **Choice.** One predicate, stated here once: a stage is *single* when Items==1 (from StageStarted) AND it has one round AND its item has one attempt. A started single stage opens its item's run; every other started stage opens the stage view; no started item opens nothing. This overrides the Goal's first two bullets where they differ.
- **Zero value.** A zero-value `runView` means "no stage" (store stage+1, or a separate `staged bool`), so `openRun`'s literal and every test `runView{ref: …}` literal keep their meaning.
- **Stage level.** State its ref and what `viewedRun`, `viewedChild` and `runLabel` answer there. `⏎` there is refused and writes no note, or a note that names the stage, instead of `stageChildMessage`'s "sub-agent is not running". `legend()`, `contextGauge`, and the `inRunView`/`viewedRun` readers in `inspector.go` and `thinkingpane.go` each get a stated answer.
- **Crumb.** It is derived from data both open paths share (the single-stage predicate above: a single stage's item wears `… › <stage>`, any other stage's item `… › <stage> › <item>`), or carried into the transcript root by a separate stage setter that calls `touch()` and is listed in `transcript_test.go`'s touch-writers table. `setRoot(ref)` keeps its signature. A fan_out item row's crumb has no stage crumb, because fan_out has no stage name.
- **Restore.** `upRun` and `reseatViewStack` restore the stage level, not only `viewedRun()`.
- **ADR.** A stage level is not a run, so ADR 0063 D4 ("a stack of open runs") is amended by ADR 0090 (item 10).

**Regression guard (re-check).** Every reader of `viewedRun()`/`inRunView()`/`viewedChild()` gets a stated stage-level answer — find them with `grep -n 'viewedRun()\|inRunView()\|viewedChild()' internal/tui/*.go`. That includes `statusLeft` (`model.go`: `shownSlot`, `runningPhrase`, `isStalled`), which the Stage level bullet's list omits.

**Approach (assumed at the header base):**
- Add `stage int` to `runView`, and a stage-level push next to `openRun` in `runview.go`.
- `paintRoot` paints a stage level by rooting at the workflow head and painting only the item heads of that stage.
- The paint-cache key includes the stage.
- `toggleBlockAt` handles `targetStage`.
- `breadcrumbTrail` adds the workflow and stage crumbs for item heads.
- `reseatViewStack` drops a stage level whose workflow head is gone.

**Depends on item 5.**
**Files:** internal/tui/runview.go; internal/tui/render.go; internal/tui/paintcache.go; internal/tui/mouse.go; internal/tui/blockcursor.go; internal/tui/transcript.go; internal/tui/subagentblock.go; internal/tui/model.go; internal/tui/interject.go; internal/tui/inspector.go; internal/tui/thinkingpane.go; internal/tui/runview_test.go; internal/tui/mouse_test.go; internal/tui/transcript_test.go
**Read first:** internal/tui/runview.go — runView, openRunAt, openRun, upRun, reseatViewStack, viewedChild, runLabel, backHint, runViewKey; internal/tui/render.go — renderView (rooted header band), transcript.paintRoot; internal/tui/subagentblock.go — breadcrumbTrail, runHeadAt;
internal/tui/mouse.go — toggleBlockAt; internal/tui/interject.go — stageChildMessage, refuseChildMessage; internal/tui/model.go — contextGauge, statusLeft, shownSlot;
internal/tui/transcript_test.go — TestTranscriptWritersBumpTheGeneration; internal/tui/runview_test.go — TestRunViewEscGoesOneLevelUp, TestRunViewStackFollowsTheEntriesItNames, TestRunViewEnterRefusesANonRunningChild

**Tests:**
- Clicking a one-item stage opens its run. That run view's breadcrumb reads `← main › <workflow> › <stage>`.
- Clicking a multi-item stage shows the stage view with N rows. Opening one gives a four-crumb breadcrumb, and `esc`,`esc` returns to main.
- A repeated stage's view shows `round N` sub-headers, oldest first. A retried item shows a dim `attempt N` sub-row that opens that attempt's run. A one-item stage with two rounds opens the stage view, and an item opened from it wears the four-crumb breadcrumb.
- A pending stage click is a no-op.
- In a stage view, `statusLeft` paints the stated stage-level answer, not a run's empty slot.
- The block cursor reaches the stage rows.
- In a stage view, `⏎` sends nothing and writes no "not running" note, `contextGauge()` is "", and a `targetBreadcrumb` click goes up one level.

**Acceptance:** `go test -race -count=1 ./internal/tui/`

**Commit:** `feat(tui): a workflow stage opens as a stage view or its item's run view`

## 7. A resumed session reopens its workflow blocks — ✅ DONE (2026-09-28)

NOTES (2026-09-28): re-derived from the Approach's "`workflowAt` finds a restored block's view" (the regression guard overrides it). A restored view carries no Workflow id and has a `replayed` flag. Where it paints: `workflowView.drawsStages()` replaces `live()` in `renderWorkflowBlock` and in `render.go` `resolveBlock`'s span skip. Stage levels now live in `runview.go` (item 6), so `stageLevel` gains a `block` entry index for replayed blocks, and `stageBlockAt` resolves by that index and never matches a replayed block for a live level.
NOTES (2026-09-28): the `session.Workflow` record keeps name, end, cause, stages (name, round, rounds, items, finished, troubled, entered, state as a string enum) and every finished item (stage, label, status, summary), not just the stages and states the Approach named. The item receipts are what the trouble lines and totals are painted from. The live id, running stage and ask question are not kept.
NOTES (2026-09-28): `closeInterruptedCalls` closes a replayed, unended workflow block. Its running stages become stopped, the Workflow ends stopped, the text's header line is rewritten to match, and it counts the stopped stages, or 1 when no stage was running. Open `workflow_item` heads are closed and counted the same way as tool calls.
NOTES (2026-09-28): consequential edit — internal/tui/transcript.go: made necessary by persisting the view (the `workflow` field comment said "never persisted").
NOTES (2026-09-28): consequential edit — internal/tui/entrykind.go: made necessary by persisting the view (the entryWorkflow rule comment said "live view (never persisted)").
NOTES (2026-09-28): consequential edit — internal/tui/paintcache.go: made necessary by persisting the view (the workflowView comment said a replayed block paints its text).
NOTES (2026-09-28): consequential edit — internal/tui/blocktarget.go: made necessary by replayed blocks drawing stage rows (the targetStage comment said "live workflow block").
NOTES (2026-09-28): TestTranscriptCodecPersistsANamedDelegationAsItsTarget's wire-member pin gains "Workflow". TestWorkflowBlockSeatsARunHeadPerItem's reopened block now asserts stage rows instead of item rows. The old text-only replay is pinned by the new TestWorkflowBlockFromAnOlderRecordPaintsItsText.
NOTES (2026-09-28): retry fix — `fromWireWorkflow` reads the record's `end` through `workflowEndOf`: "", finished, stopped and failed pass through, and any other value reads as stopped, so no recorded string reaches the header (`stripWorkflow` leaves End alone as a closed enum). Pinned by TestWorkflowBlockFromARecordWithAForeignEndPaintsItStopped.

**What:**
**Goal:** After save and restore, a finished foreground workflow's block paints the same stage rows, trouble lines and totals. Its stage rows and item rows open read-only views over the restored item entries, and no item output paints at top level.

**Regression guard.** A restored workflow view never matches `workflowAt` for a new run with the same Workflow id. Restore it without the id, or flagged replayed, and open restored stage and item views by entry index. `closeInterruptedCalls` closes open `workflow_item` heads and restored running stages, and counts them in the "closed" note. The Workflow record's strings are stripped in session `stripEntry`, with a test. This overrides the Approach's "`workflowAt` finds a restored block's view".

**Approach (assumed at the header base):**
- Add a `Workflow` record (name, state, stages with item count and state) to `session.Entry` for `EntryKindWorkflow`, written by `toWireEntry` and read by `fromWireEntry` in `transcriptbridge.go`.
- `workflow_item` heads already carry run ids through `SpawnRunID`, from item 4.
- `workflowAt` finds a restored block's view.
- Older records without the field restore as text only, and their item entries (which have no heads) paint as they do at the base.

**Depends on item 6.**
**Files:** internal/session/transcript.go; internal/session/transcript_test.go; internal/tui/transcriptbridge.go; internal/tui/workflowblock.go; internal/tui/transcriptbridge_test.go; internal/tui/workflowblock_test.go
**Read first:** internal/tui/transcriptbridge.go — toWireEntry, fromWireEntry, closeInterruptedCalls, decodeTranscript; internal/session/transcript.go — Entry, stripEntry, DecodeTranscript; internal/tui/workflowblock.go — workflowAt, addWorkflowPhase;
internal/agent/workflowcall.go — package doc (plan-hash resume); internal/tui/workflowblock_test.go — TestWorkflowBlockSurvivesTheRecord, roundTrip; internal/tui/transcriptbridge_test.go

**Tests:**
- Extend `TestWorkflowBlockSurvivesTheRecord`: the stage rows round-trip and a restored stage row opens a stage view.
- After a restore, a new run with the same Workflow id opens a new block and leaves the restored one untouched.
- A `workflow_item` head and a running stage saved mid-run come back closed, and they are counted in the "closed" note.
- `stripEntry` strips escapes from the Workflow record's name and stage strings.
- A pre-change record fixture restores without error.

**Acceptance:** `go test -race -count=1 ./internal/tui/ && go test -race -count=1 ./internal/session/`

**Commit:** `feat(tui): a resumed session reopens its workflow stages`

## 8. The context gauge shows an estimate before the first usage reading — ✅ DONE (2026-09-28)

NOTES (2026-09-28): Agent.ContextEstimate lives in internal/agent/contextcost.go beside ContextCost (ContextCost().Tokens + the budget's EstimateTokens over domain.PromptChars(conv.Messages(), nil)), not in agent.go; its tests are in agent_test.go.
NOTES (2026-09-28): internal/tui/usage.go and usage_test.go were not touched — the /usage table keeps real readings only; all TUI tests went into model_test.go (estimate shown, replaced by a depth-0 reading, /clear, /compact, errored `enter dismiss`).
NOTES (2026-09-28): the estimate is taken by Model.takeContextEstimate (model.go) only while ctxUsed == 0, called in launchExchange, runContinue (before both branches) and wakeIfIdle; layout.md's right-slot prose is left to item 10, which owns the gauge doc sweep.

**What:** Recast at the regression check (2026-09-28).
**Goal:** While the session has no depth-0 usage reading, which is the case during a foreground recipe that is the first line or follows `/clear` or `/compact`, the status line's right slot shows the context gauge in its usual format with the value prefixed `~`. The value is the engine's estimate of the next request. The first real reading replaces it.

**Regression guard.** The estimate is taken synchronously on the Update goroutine before the worker is dispatched (`launchExchange`, `runContinue`, `wakeIfIdle`), and is documented as idle-only like `ContextCost`. Prefer the existing `internal/agent/contextcost.go` `ContextCost` path if it already answers the next request's estimate, rather than a second estimator. This replaces the Approach's `tea.Cmd` pull, which races the worker. Add `lateEngine.ContextEstimate` (0 while unbound) in `cmd/apogee/wire_engine.go` to Files. `ctxEstimate` lives in `liveStats` and is zeroed wherever `ctxUsed` is (`reset`, `foldCompactDone`). Add a /compact test case. The item yields to `internal/context/budget.go`'s worker-goroutine-only `TokenEstimator` rule and `contextcost.go`'s idle-only reads.

**Regression guard (re-check).**
- **Precedence.** `stateErrored` keeps `enter dismiss` over an estimate: the `~` estimate renders only when no real reading exists AND the state is not errored (`statusRight` asks `contextGauge` before the state hints). On a server that omits usage no real reading ever arrives, so the `~` estimate stands on the idle line all session, refreshed at each Turn start.
- **Estimate.** The estimate is `ContextCost().Tokens` plus the budget's `EstimateTokens(domain.PromptChars(a.conv.Messages(), nil))`, the history term `loop.go`'s fit check uses, since `ContextCost` alone counts only the Turn-1 standing blocks and the tool surface. This settles the "Prefer … `ContextCost`" sentence above.

**Approach (assumed at the header base):**
- Add `Engine.ContextEstimate() int` to the TUI's Engine interface (`internal/tui/tui.go`). The agent answers it from its context manager's token estimator (`internal/context` `TokenEstimator`) over system prompt, tools and history.
- The TUI pulls it in a `tea.Cmd` at Turn start while `ctxUsed == 0` and stores `ctxEstimate`.
- `contextGauge` renders it when `ctxUsed == 0`. Run views keep showing the viewed run's fill only.
- `fakeEngine` gains the method.

**Files:** internal/tui/tui.go; internal/tui/model.go; internal/tui/commandrun.go; internal/tui/workflow.go; internal/tui/usage.go; internal/agent/agent.go; internal/agent/contextcost.go; cmd/apogee/wire_engine.go; internal/tui/seam_test.go; internal/tui/usage_test.go; internal/tui/model_test.go; internal/agent/agent_test.go
**Read first:** internal/tui/model.go — Model.statusRight, Model.contextGauge, contextUsage.view, liveStats, liveStats.reset; internal/tui/commandrun.go — Model.launchExchange, Model.runContinue, Model.foldCompactDone; internal/tui/workflow.go — Model.wakeIfIdle;
internal/agent/contextcost.go — Agent.ContextCost; internal/tui/fold.go — Model.foldStats; cmd/apogee/wire_engine.go — lateEngine.ContextFilesReport (the unbound-forward pattern);
internal/tui/seam_test.go — fakeEngine.ContextFilesReport; internal/tui/model_test.go — assertStatusRightTail cases ("esc×2 cancel", "enter dismiss")

**Tests:**
- A fresh model that starts a Turn with a fake estimate of 3000 renders a `~` gauge.
- A depth-0 `UsageEvent` replaces it.
- `/clear` goes back to the estimate on the next Turn, and the idle status line right after `/clear` shows no stale `~` value.
- `/compact` zeroes the estimate with `ctxUsed`, and the next Turn shows a fresh `~` estimate; the agent-side case asserts the estimate reflects the compacted history, not a fresh agent's.
- An errored first Turn with a fake estimate of 3000 still ends the right slot with `enter dismiss` (`assertStatusRightTail` in `model_test.go`).
- The engine estimate is greater than 0 for a fresh agent, and grows with the conversation's history.
- `fakeEngine`'s estimate defaults to 0, so fixtures with `ContextWindow` 32768 keep today's right-slot hints.

**Acceptance:** `go build ./... && go test -race -count=1 ./internal/tui/ && go test -race -count=1 ./internal/agent/`

**Commit:** `fix(tui): the context gauge shows an estimate until the first usage reading`

## 9. Clicking a list row never moves the row window — ✅ DONE (2026-09-28)

NOTES (2026-09-28): clickArm has a new pin(pane, selected) method that returns (rowTop, pinTop). listSpec, askPromptSpec and approvalPromptSpec all call it, so the rule is written once rather than as three `holds` checks. popupPaneHit returns (row, top, inRect, ok). settingsPaint.rowAt drops top, because /settings arms no row.
NOTES (2026-09-28): the plan asked for TestClickArmClearsOnKeyAndWheel to cover "a key after a click re-centres". That check is a new sibling test, TestClickArmDropsThePinSoTheWindowRecentres, with a key case and a wheel case. The existing test is table-driven over a bare model with no pane on screen, so it could not look at a window.
NOTES (2026-09-28): the picker/browser/workflows/ask checks are one table test, TestClickOnALowRowKeepsTheRowWindowStill. It also covers the approval prompt. Each case clicks the lowest row the window seats, and the picker, workflows and approval cases shrink the terminal so the list overflows.
NOTES (2026-09-28): consequential edit — layout.md: made necessary by the click no longer re-centring the list; one sentence in "And the click follows the notch" says the rows stay still under the first click.

**What:** This fixes a defect that predates the base, and commit 3f468f6d wrote the workaround into `TestE2EPopupClickDropdown`.

**Goal:** In every list pane (the `/`/`@` dropdown, picker, browser, `/workflows`, ask and approval), the first click on a row highlights it without moving the rows on screen. A second click at the same screen position therefore accepts that row. Arrow keys and the wheel keep centring the window on the selection.

**Regression guard.** `askPromptSpec` (`ask.go`) and `approvalPromptSpec` (`approval.go`) set the pin under `m.clickArmed.holds(panePrompt, selected)`, because they build their own specs outside `listSpec`. `popupPaneHit` also returns `place.start`, and all seven handlers are updated: browser, picker, ask, approval, dropdown, workflows (`handleWorkflowsClick`) and settings. The pinned and fallback cases sit on `popupRowSeat`, not `popupRowWindow`, which has no pin parameter. This supersedes `popup.go`'s rowTop comment ("read only where selected is negative") and `autocomplete.go`'s "scrolls around the selected item". Rewrite both.

**Approach (assumed at the header base):**
- `popupRowSeat` windows every list through `popupRowWindow(selected, …)`, which re-centres on the selection.
- Add `top int` to `clickArm` (`mouse.go`), recorded from the pre-click placement's `start` (`popupPaneHit`/`popupPlacement`).
- Add a `pinTop` flag next to `popupSpec.rowTop`. `popupRowSeat` uses `popupRowWindowFrom(rowTop, …)` when the flag is set and the selection lies inside that window, and otherwise falls back to centring.
- `listSpec` sets the flag only while `m.clickArmed.holds(pane, selected)`.

**Closes:** apogee-slash-dropdown-click-recentre
**Files:** internal/tui/popup.go; internal/tui/mouse.go; internal/tui/listsurface.go; internal/tui/ask.go; internal/tui/approval.go; internal/tui/workflows.go; internal/tui/autocomplete.go; internal/tui/popup_test.go; internal/tui/mouse_test.go; cmd/apogee/e2e_popups_test.go
**Read first:** internal/tui/popup.go — popupRowSeat, popupRowWindow, popupRowWindowFrom, popupSpec.rowTop; internal/tui/listsurface.go — listSpec, renderListPlaced;
internal/tui/mouse.go — clickArm, popupPaneHit, handleDropdownClick, handlePickerClick, handleBrowserClick, handleAskClick, handleApprovalClick; internal/tui/workflows.go — handleWorkflowsClick;
internal/tui/ask.go — askPromptSpec; internal/tui/approval.go — approvalPromptSpec; internal/tui/mouse_test.go — TestDropdownClickHighlightsThenTheSecondClickAccepts, TestClickArmClearsOnKeyAndWheel, dropdownPaneModel; cmd/apogee/e2e_popups_test.go — TestE2EPopupClickDropdown

**Tests:**
- A sibling of `TestDropdownClickHighlightsThenTheSecondClickAccepts` that clicks a row below the middle (e.g. `confine`): the ❯ lands on the same frame row, and a second click at the same (x,y) accepts.
- The same check for the picker, the browser, `/workflows` and ask.
- Extend `TestPopupRowSeatFromCountsIsThePaintersWindow` with a pinned spec whose selection lies inside `[rowTop, rowTop+seats)`, which keeps the pinned top, and one whose selection lies outside it, which falls back to centring.
- The `popupPaneHit` callers in `mouse_test.go` are updated to the widened return.
- `TestClickArmClearsOnKeyAndWheel`: a key after a click re-centres.
- `TestE2EPopupClickDropdown` fires its second click at the original (x,y).
- The new tests fail against the pre-item tree.

**Acceptance:** `go test -race -count=1 ./internal/tui/ && go test -race -count=1 -run 'TestE2EPopupClick' ./cmd/apogee/`

**Commit:** `fix(tui): clicking a list row keeps the row window still`

## 10. ADR 0090 and the docs describe enterable workflow stages — ✅ DONE (2026-09-28)

NOTES (2026-09-28): consequential edit — internal/tui/model_test.go: made necessary by the `~` estimate rewrite (the "gauge is dark until the first turn reports usage" test comment now names the missing estimate too)
NOTES (2026-09-28): consequential edit — internal/tui/doc.go: made necessary by the stage-row rewrite (the file map said stage rows stand for item runs only under a "live" Recipe block; a resumed one draws them too)
NOTES (2026-09-28): consequential edit — docs/manual/commands.md: made necessary by the manual update (the run view paragraph links to the new "Watching a workflow in the TUI" section)
NOTES (2026-09-28): the three test failure messages the Rule's grep still hits ("setup: no run view is open", "opened no run view, so the box addresses nobody" in workflowblock_test.go, interject_test.go, runview_test.go) are left as they are — they report a test setup failure, not a claim that a workflow block has no run view; the remaining gauge-grep hits (throughput, a run view's own gauge, the estimate-aware contextGauge comment) are accurate and stay
NOTES (2026-09-28): CONTEXT.md's **Workflow** keeps its ADR 0087 wording and gains a paragraph on the TUI shape; **Run view** gains a sentence that item runs open the same way

**What:**
**Goal:** The documentation matches the shipped behaviour of items 4 to 9:
- `docs/adr/0090-workflow-stages-are-enterable-views.md` records the stage-row block, the stage view, item run heads, persistence, and the reversal of the "never collapses / no run view" rule. It amends ADR 0063 and cites ADR 0087/0089.
- `layout.md` "The workflow block" and "Run view" (stage level, crumbs), and "The status line's right slot" (`~` estimate), are rewritten.
- `CONTEXT.md` defines **Stage view** and updates **Workflow**.
- The manual page that documents workflows or the run view (find it with `grep -rln "run view\|/workflows" docs/manual`) is updated.

**Regression guard.** Files include every file the Rule's grep hits when the item runs, test files included. At base these are `layout.md`, `render.go`, `transcript.go`, `subagentblock.go`, `workflowblock.go`, `workflowblock_test.go`, `interject_test.go` and `runview_test.go`. The gauge grep is widened to `grep -rn "gauge hidden\|renders nothing\|until the first\|stands in for it\|while a turn runs" layout.md CONTEXT.md docs/manual internal/tui`. This covers `tui.go`'s `Options.ContextWindow` comment and `layout.md`'s `esc×2 cancel` line. The manual page is `docs/manual/workflows.md` "Starting a recipe" ("one line per item as it finishes").

**Regression guard (re-check).** The Rule's grep is widened to also find "never elides|never hides what it heads|no head of its own"; at base these hit `internal/tui/transcript.go` (`headsWorkflow`) and `internal/tui/subagentblock.go` (`subAgentSpan`, the item-run comment) — both named in Files.

**Rule:** every sentence in `layout.md`, `CONTEXT.md`, `docs/manual/` or a code comment that says the workflow block never collapses, paints item runs railed beneath it, never elides or hides what it heads, or has no run view — or that an item run has no head of its own — is rewritten. Find them with `grep -rn "never collapses\|no run view\|railed beneath\|headsWorkflowRuns\|never elides\|never hides what it heads\|no head of its own" layout.md CONTEXT.md docs/manual internal/tui`. The same goes for every statement that the gauge renders nothing before the first reading: `grep -rn "renders nothing\|until the first" layout.md internal/tui/model.go`.

**Depends on items 4–9.**
**Files:** docs/adr/0090-workflow-stages-are-enterable-views.md; docs/adr/0063-sub-agent-runs-are-user-addressable-views.md; layout.md; CONTEXT.md; docs/manual/workflows.md; docs/manual/*.md; internal/tui/model.go; internal/tui/tui.go; internal/tui/workflowblock.go; internal/tui/render.go; internal/tui/transcript.go; internal/tui/subagentblock.go; internal/tui/workflowblock_test.go; internal/tui/interject_test.go; internal/tui/runview_test.go
**Read first:** layout.md — "The workflow block", "Run view", "The status line's right slot"; CONTEXT.md — Run view, Workflow; docs/manual/workflows.md — Starting a recipe; internal/tui/workflowblock.go — file doc;
internal/tui/render.go — workflow-head branch comment; internal/tui/tui.go — Options.ContextWindow; internal/tui/model.go — Model.statusRight, Model.contextGauge

**Tests:** Docs and comments only.

**Acceptance:** `grep -rn "never collapses\|a head with many runs has no run view" layout.md CONTEXT.md docs/manual internal/tui | wc -l` prints 0, `test -f docs/adr/0090-workflow-stages-are-enterable-views.md && grep -q '^\*\*Stage view\*\*' CONTEXT.md` succeeds, and `go build ./...`

**Commit:** `docs(adr): 0090 workflow stages are enterable views`

## 11. AGENTS.md states the current tool count and IDEAS.md ticks the shipped ideas — ✅ DONE (2026-09-28)

NOTES (2026-09-28): IDEAS.md is gitignored (`.gitignore:14`), so its two `[x]` ticks (context gauge -> item 8; enterable steps -> ADR 0090), each with a pointer to this plan by file name, are made in the working tree only and left off FILES — `git add` refuses an ignored path; the Acceptance grep prints 2.
NOTES (2026-09-28): the counts were read from the tree, not from the plan: `KnownToolNames()` returns 36 names; the default menu is 27 without host tools plus `load_skill`, `ask_user` and `present_document` = 30 (the console family, `fan_out` and `workflow` are off by default) — matching README.md's "36 built-in tools (30 on the default menu)".
NOTES (2026-09-28): pre-existing — the AGENTS.md bullet still says each README count "is pinned by a test or a literal in `internal/tools/registry.go`", but no test or literal pins the tool count (the plan's own regression guard says so); the sentence was left as written, outside this item's count-only goal.

**What:**
**Goal:**
- The tool count in `AGENTS.md`'s "Public docs speak the shipped vocabulary" bullet (total, and on the default menu) equals the numbers pinned in `internal/tools/registry.go` and stated in `README.md`. Read both from the tree when the item runs.
- `IDEAS.md` marks the two workflow ideas (context gauge; enterable steps) `[x]` with a pointer to this plan.

**Regression guard.** `registry.go` holds no count literal, and no test pins the numbers. The count must therefore equal README.md's `N built-in tools (M on the default menu)`, counted from `KnownToolNames` and the default menu. Acceptance also runs `grep -c '^- \[x\] when a workflow is running' IDEAS.md`, which must print 2.

**Closes:** apogee-agents-md-tool-count
**Files:** AGENTS.md; IDEAS.md
**Read first:** AGENTS.md — "Public docs speak the shipped vocabulary" bullet; README.md — "built-in tools" bullet; internal/tools/registry.go — KnownToolNames, DefaultToolsWithHost;
internal/tools/registry_test.go — TestNewDefaultRegistry_MenuOrderIsDeterministic; IDEAS.md — Sub-Agents section

**Tests:** None (docs).

**Acceptance:** `grep -n "tools," AGENTS.md` shows the count that `grep -n "built-in tools" README.md` shows. `grep -c '^- \[x\] when a workflow is running' IDEAS.md` prints 2. `go test -count=1 -run 'Count|Menu' ./internal/tools/` passes.

**Commit:** `docs(agents): AGENTS.md states the current tool count`

## 12. End-to-end: a recipe's stages are entered and left in the real program — ✅ DONE (2026-09-28)

NOTES (2026-09-28): beyond the Goal's four scenes the test also opens the one-item `plan` stage and checks it lands straight on its item's run under a trail ending on the stage (`← main › stages › plan`), since the fixture's one-item stage exists to exercise that single-stage path.
NOTES (2026-09-28): observed, not in scope — an item's run view opened from a stage shows only the item's `finish` card, not the brief it was handed; a sub_agent run view opens on its task (ADR 0063 D5), so a workflow item's view reads thinner. No doc claims otherwise; an improvement idea only.

**What:**
**Goal:** A PTY-free e2e test drives a stubbed two-stage recipe (one item, then three) through the real program:
- the top-level frame shows stage rows and no item output;
- clicking the second stage shows the stage view;
- opening an item shows its run view with the four-crumb breadcrumb;
- `esc`,`esc` returns to main.

Goldens pin the block and the stage view.

**Regression guard.** The e2e fixture pins no `context-window:` and the stub advertises no window, so item 8's `~` estimate never enters the goldens.

**Approach (assumed at the header base):**
- Use the `cmd/apogee` e2e harness (`launchTUI`, `e2eSession.Redactions`, `tuitest.Golden`, `WaitText` on breadcrumb text per `docs/design/test-drivers.md` rules 4–6).
- Add a stubllm script next to `testdata/stubllm/fanout.yaml`, and a fixture recipe skill in the test workspace.

**Depends on items 7 and 8.**
**Files:** cmd/apogee/e2e_workflow_stages_test.go; cmd/apogee/testdata/stubllm/recipe-stages.yaml; cmd/apogee/testdata/frames/workflow-stages.txt; cmd/apogee/testdata/frames/workflow-stage-view.txt
**Read first:** cmd/apogee/e2e_subagent_view_test.go — TestE2ESubAgentView, runViewRedactions, frameWhen; cmd/apogee/e2e_support_test.go — launchTUIIn, e2eSession.Redactions; cmd/apogee/headless_test.go — sweepRecipeSkill, sweepUpstream;
cmd/apogee/e2e_delegation_test.go — goldenRedactions; cmd/apogee/e2e_popups_test.go — click; internal/skills/load.go — sourceAnchors (workspace recipes load from `<ws>/.apogee/skills`)

**Tests:** `TestE2EWorkflowStages`.

**Acceptance:** `go test -race -count=1 -run 'TestE2EWorkflowStages|TestE2ESubAgentView' ./cmd/apogee/`

**Commit:** `test(apogee): a recipe's stages are entered and left end to end`
