# Workflow resume: its own block, a kept trace, a resume hint

**Goal:** A re-issued recipe that resumes a stopped workflow draws its own block instead of
hijacking the stopped one, a cancelled recipe launch stays in the model's conversation, and every
stopped workflow says how to resume it.
**Date:** 2026-09-29
**Status:** unexecuted
**sized for:** ~200k-context host
**base:** 5a1f8459

**Regression check (2026-09-29, 5a1f8459):**
- 1: guard folded (stale one-block-per-Workflow comments).
- 2: guard folded (no model request after the opening; flag through a turnLifecycle verb); supersedes ADR 0088 D1, CONTEXT.md **Exchange** and the `settle` doc comment per the ratified cancel-trace call.
- 3: guard folded (writer decision: one launch-kind value on recipeCall).
- 4: guard folded (writer decision: Resume derived from item 3's launch-kind value).
- 5: guard folded (Resume set in the Started view literal; stripped on decode).
- 6: guard folded (writer decision: "re-run" wording kept apart from a session resume).
- 2 (second pass): guard folded (ctx.Err() check after the opening; flag verb reset in openExchange/closeExchange/abort) — already carried; Read first refreshed; supersedes ADR 0088 D1, CONTEXT.md **Exchange** 832-833, the `settle` doc comment.
- 3 (second pass): guard folded (writer decision: pinned cancel test updated and added to Acceptance; resume line immediately before SeatFallbackNote, last without it; yields to workflowAnswer's "SeatFallbackNote once, last" doc comment).
- 4 (second pass): guard folded (writer decision: eventjson/encode.go comment names Resume; file added to Files).
- 6 (second pass): guard folded (writer decision: ADR 0088 D1 and CONTEXT.md **Exchange** fallback named as amendment sites; `settle` comment is item 2's).

**Sources:**
- docs/adr/0087-the-engine-runs-workflows-the-model-or-a-recipe-asks-for.md (D4 resume by plan hash)
- docs/adr/0088-cancel-settles-and-never-rewinds-finished-work.md (D3, D4)
- docs/adr/0090-workflow-stages-are-enterable-views.md (D1, D5)
- docs/manual/workflows.md
- Observed: session `20260929T085540Z-834ba18f` — second `/audit internal/mcp security` resumed
  folder `20260929-105542-audit`, but its helper's rows rendered loose under the prompt and the old
  block read `stopped` with `lens-security running`; the cancelled first launch left no message.

**Ratified design calls** (owner, 2026-09-29):
- **Cancel trace:** a cancelled recipe launch keeps the user's line and the stopped result lines in
  the conversation, with the usual cancelled note; no model request follows.
- **Resume hint:** shown in the block footer (recipe launches) and in the stopped result text the
  model reads (recipes and `fan_out`).
- **Old block:** a resumed run opens a new block; the stopped block stays frozen as it was.

**Standing requirements:**
- skills: coding-standards
- Run tests narrowed with `-run` to the named tests and one package; never a package pattern.

**Out of scope:**
- Cross-session resume (ADR 0087 D4 deferral stands).
- Background workflows' resume (already restart on session resume); a `/workflows` resume key.
- The audit recipe's prompts (a helper reading `part-all/bundle.md` is a model error).

## 1. Resumed workflow opens its own block — ✅ DONE (2026-09-29)

NOTES (2026-09-29): the Started guard asks a new transcript.workflowRunning helper (newest block of the id with end == ""); the non-Started path returns when the newest block has ended rather than scanning past it to an older one (any older block of the id has ended too).

**What:** Fix: a `WorkflowStarted` for a workflow id whose earlier block is stopped is dropped by
the TUI, so the resumed run's item runs (spawned under the new call, e.g. `recipe-audit-1`) find no
heading block and render loose in the main conversation, and the old block folds the new phases.
**Regression guard.** Update every comment that says a Workflow id has one block or that later
phases always fold into it (workflowblock.go file doc "ONE block per Workflow", the
`addWorkflowPhase` doc "finds the block by the Workflow's id", `workflowAt`'s doc "the live block") —
`grep -n -i 'one block\|ONE block\|by the Workflow.s id\|workflowAt' internal/tui/*.go`.
**Goal:** A `WorkflowStarted` opens a new block whenever no block of that workflow id is still
running; every later phase of that id folds into the newest block; an ended block never changes
again; the resumed run's item heads are seated under the new block.
**Approach (assumed at the header base):** In `transcript.addWorkflowPhase`
(internal/tui/workflowblock.go) the Started branch returns early when `workflowAt(e.Workflow) >= 0`.
Narrow that guard to a block of the same id whose `workflow.end == ""` (a duplicate Started for a
live run); keep the `fanOutOpen(run)` guard. `workflowAt` already scans newest-first, so later
phases reach the new block; make the non-Started path skip a block whose `end != ""` so a stray
late phase can never reopen a frozen one. Item seating (`entry.headsWorkflowRuns`,
`entry.seatsItemHead`) matches on the block's `callID`, which the new block takes from `e.Call`.
**Files:** internal/tui/workflowblock.go; internal/tui/workflowblock_test.go
**Read first:** internal/tui/workflowblock.go — addWorkflowPhase, workflowAt, fanOutOpen, addWorkflowItem;
internal/tui/transcript.go — entry.headsWorkflowRuns, entry.seatsItemHead; internal/agent/recipe.go — recipe call ID `recipe-%s-%d`;
internal/tui/workflowblock_test.go — TestResumedBlockIgnoresARunOfTheSameWorkflow
**Tests:** `TestResumedWorkflowOpensItsOwnBlock` — Started(W, call `recipe-audit-0`), stage/item
phases, Stopped; then Started(W, `recipe-audit-1`) and an item run under `recipe-audit-1`: two
workflow blocks, the first still reads `Workflow audit — stopped` with unchanged rows, the second
reads `running` and heads the item run. `TestDuplicateStartedForLiveWorkflowKeepsOneBlock`.
**Acceptance:**
- `go build ./...`
- `go test -count=1 -run 'TestResumedWorkflowOpensItsOwnBlock|TestDuplicateStartedForLiveWorkflowKeepsOneBlock|TestWorkflowBlock|TestWorkflowStage' ./internal/tui/`
**Commit:** `fix(tui): give a resumed workflow its own block`

## 2. A cancelled recipe launch keeps its opening — ✅ DONE (2026-09-29)

NOTES (2026-09-29): re-derived from "composeUserMessage records the flag" — the flag is set by `launchRecipe` (recipe.go) through the new `turnLifecycle.carryRecipeResult` verb, only after `runRecipe` returned without error (a refused launch is still scrapped).
NOTES (2026-09-29): the ctx.Err() check after the opening sets `t.rollback`/`t.deferredFloor` to the current length before `end(t, endCancelled)` — armRequest has not run yet there, and an unset rollback of 0 would drop the whole conversation.
NOTES (2026-09-29): consequential edit — internal/agent/agent.go: made necessary by settle keeping a recipe opening (SettleExchange doc comment named the abort fallback as unconditional).
NOTES (2026-09-29): consequential edit — internal/domain/advice.go: made necessary by settle noting the opening (NoteMessage doc comment named only the last-tool-result use).

**What:** Fix: on cancel, `turnLifecycle.settle` finds no tool result (a recipe launch commits
none) and falls back to `abort`, which drops the opening holding the user's `/<recipe>` line and
the `stopped by the user: K of N done` result, so the model's next request knows nothing of it.
**Regression guard.** `step` (internal/agent/loop.go) checks `ctx.Err()` right after the opening
is appended and returns `a.turns.end(t, endCancelled)`, so no `upstream.Stream` call follows a
cancelled launch. The recipe-result flag is set through a `turnLifecycle` verb and reset in
`openExchange`/`closeExchange`/`abort`; any cancel in a launch's first Turn keeps the opening.
Supersedes ADR 0088 D1 ("An Exchange with no finished Turn is still scrapped"), CONTEXT.md
**Exchange** (lines 832-833) and the `settle` doc comment (turn.go) for a recipe launch, per the
ratified cancel-trace call: rewrite the `settle` comment here; item 6 amends the ADR and CONTEXT.md.
**Goal:** After a cancel during a recipe launch, the conversation ends on the opening user message
(the typed line plus `recipe /<id> ran as a workflow:` and the stopped result lines) carrying the
cancelled note; `SettleExchange` reports not dropped; no model request is issued. An Exchange
with no recipe result and no tool result is still aborted as before.
**Approach (assumed at the header base):** `composeUserMessage` (internal/agent) records on the
`turnLifecycle` that the opening carries a recipe result (a private flag, set only when
`recipeLaunch` ran the workflow). `settle` checks it before the `lastToolResult() < 0` fallback:
put `cancelledNoteLine` on the opening via `conv.NoteMessage`, keep the retained set, and
`closeExchange`. The TUI's `Model.foldCancelled` already follows `dropped`, so `markAborted` is
not called; no TUI change.
**Files:** internal/agent/turn.go; internal/agent/loop.go; internal/agent/recipe.go; internal/agent/recipe_test.go
**Read first:** internal/agent/turn.go — turnLifecycle.settle, abort, lastToolResult, openExchange, closeExchange;
internal/agent/loop.go — step (opening block), composeUserMessage, respondAndReview; internal/agent/collect.go — collectCompletion;
internal/agent/recipe_test.go — requestLog, reviewRecipe, reviewUpstream; internal/tui/model.go — foldCancelled
**Tests:** `TestCancelledRecipeLaunchKeepsItsOpening` — a recipe whose fanout blocks until cancel;
cancel, settle: dropped false, last message is the user opening containing the typed line and
`stopped by the user:`, with the cancelled note; the stub provider saw no request.
`TestCancelledPlainExchangeStillAborts` — run on the same Agent after a recipe launch, so a leaked
flag fails it.
**Acceptance:**
- `go build ./...`
- `go test -count=1 -run 'TestCancelledRecipeLaunch|TestCancelledPlainExchange|TestRecipe' ./internal/agent/`
**Commit:** `fix(agent): keep a cancelled recipe launch in the conversation`

## 3. The stopped result tells the model how to resume — ✅ DONE (2026-09-29)

NOTES (2026-09-29): the launch-kind value is `workflowLaunch` (kind: launchFanOut / launchTypedRecipe / launchStartRecipe, plus the recipe id and the trimmed typed line) carried on `recipeCall.launch`; plain fan_out passes `workflowLaunch{kind: launchFanOut}` straight to `workflowAnswer`, which gained a third parameter. `recipeLaunchKind` (recipe.go) builds it from the UserInput; item 4 can derive the Started event's Resume from the same value.
NOTES (2026-09-29): the resume line is added to every stopped answer, also one whose Result has no Listing path (the Goal says every stopped answer carries it).

**What:** Add one resume line to a stopped workflow's result text, after the listing line.
Wording, binding:
- typed recipe launch: ``to resume: re-run `/<id> <text>` — finished items are kept`` where
  `/<id> <text>` is the user's typed line, trimmed;
- `StartRecipe` launch (inputs bound without text): ``to resume: run `/<id>` again with the same
  inputs — finished items are kept``;
- `fan_out` (plain or `recipe` form): `to resume: call fan_out again with the same arguments —
  finished items are kept`.
**Regression guard.** One launch-kind value (typed line / StartRecipe, detected by in.RecipeInputs != nil / fan_out) is added once to the recipe launch plumbing (recipeCall) and is the single input both resumeHint (item 3) and the Started event's Resume (item 4) are derived from.
Fold the report's guard on the existing pinned cancel test — TestWorkflowCall_ACancelKeepsFinishedItemsAndAnswersTheCall is updated to expect the resume line and `TestWorkflowCall_ACancel` is added to the item's Acceptance -run pattern; the item yields to workflowAnswer's documented "SeatFallbackNote once, last" rule (workflowcall.go doc comment, unchanged): the resume line goes immediately before SeatFallbackNote, and is the last line when no fallback note is present; TestWorkflowSpawn_AnItemThatFellBackFromTheSubAgentsServerSaysSo stays valid.
**Goal:** Every stopped blocking workflow's answer (recipe launch and `fan_out`) carries the
matching resume line above (SeatFallbackNote, when present, stays last); a finished or failed
workflow's answer carries none.
**Approach (assumed at the header base):** One producer, `resumeHint` (internal/agent), built from
how the workflow was launched and passed into `workflowAnswer` (internal/agent/workflowcall.go),
which appends it when `result.Stopped()`. `launchRecipe` passes the typed line; `fan_out`'s
dispatch passes the fan_out form. Item 4 reuses the same producer.
**Files:** internal/agent/workflowcall.go; internal/agent/recipe.go; internal/agent/workflowcall_test.go; internal/agent/recipe_test.go
**Read first:** internal/agent/workflowcall.go — workflowAnswer, fanOutListingLineFormat, workflowCallResult, recipeCallResult, runWorkflowCall;
internal/agent/recipe.go — launchRecipe, StartRecipe, recipeResultFormat; internal/workflow/format.go — Format, render;
internal/agent/workflowcall_test.go — TestWorkflowCall_ACancelKeepsFinishedItemsAndAnswersTheCall, callResult; internal/agent/workflowspawn_test.go — TestWorkflowSpawn_AnItemThatFellBackFromTheSubAgentsServerSaysSo
**Tests:** `TestStoppedRecipeAnswerCarriesResumeLine` (exact string for `/audit internal/mcp
security`), `TestStoppedFanOutAnswerCarriesResumeLine`, `TestFinishedWorkflowAnswerHasNoResumeLine`;
update `TestWorkflowCall_ACancelKeepsFinishedItemsAndAnswersTheCall` to expect the resume line (the
listing path is cut before it).
**Acceptance:**
- `go build ./...`
- `go test -count=1 -run 'ResumeLine|TestRecipe|TestFanOut|TestWorkflowCall_ACancel' ./internal/agent/`
**Commit:** `feat(agent): tell the model how to resume a stopped workflow`

## 4. The workflow's started phase carries its resume command — ✅ DONE (2026-09-29)

NOTES (2026-09-29): Resume is derived by a new resumeCommand(workflowLaunch) beside resumeHint, from item 3's launch-kind value; runRecipe sets it on the observer, so a fan_out's recipe form (launchFanOut) and a background workflow (driveBackground's own observer, never set) carry "". The background case is empty by construction and has no dedicated test; TestFanOutStartedEventHasNoResume covers plain and recipe-form fan_out.
NOTES (2026-09-29): no CHANGELOG entry — the field is not user-visible until item 5 draws it; not added to the NDJSON workflow_phase line (encode.go comment names it among the Driver-only members).

**What:** Depends on item 3. Add `Resume string` to `domain.WorkflowPhaseEvent`, set on
`WorkflowStarted` for a recipe launch only: the user-facing text ``re-run `/<id> <text>` to
resume`` (typed launch) or ``run `/<id>` again with the same inputs to resume`` (`StartRecipe`).
Empty for `fan_out` and background workflows. Producer: the observer that emits Started
(`workflowObserver`, internal/agent/workflowcall.go) fed from `resumeHint`'s launch data.
Consumers: internal/tui (item 5); any other `WorkflowPhaseEvent` consumer (grep
`WorkflowPhaseEvent` in internal/ and cmd/, e.g. eventjson) passes it through or ignores it.
**Regression guard.** Derive Resume from the same launch-kind value item 3 adds to recipeCall; never a second copy of the typed line or launch detection.
Fold the guard that the eventjson/encode.go comment (and any encoder of WorkflowPhaseEvent) is updated to cover the new Resume field, and add that file to Files.
**Goal:** A recipe launch's `WorkflowStarted` event carries the resume text above; a `fan_out`'s
and a background workflow's carry an empty `Resume`.
**Approach (assumed at the header base):** Field on the event struct in internal/domain/events.go
with its doc comment; set where Started is built for the recipe call.
**Files:** internal/domain/events.go; internal/agent/workflowcall.go; internal/agent/recipe.go; internal/agent/recipe_test.go; internal/eventjson/encode.go
**Read first:** internal/agent/workflowcall.go — workflowObserver, startLocked, emitLocked, observeWorkflow;
internal/agent/recipe.go — runRecipe, recipeCall, launchRecipe; internal/domain/events.go — WorkflowPhaseEvent and its doc comment;
internal/eventjson/encode.go — workflowPhaseData; internal/agent/background.go — driveBackground observer; internal/agent/recipe_test.go — recipeConfig, reviewRecipe
**Tests:** `TestRecipeStartedEventCarriesResume`, `TestFanOutStartedEventHasNoResume`.
**Acceptance:**
- `go build ./...`
- `go test -count=1 -run 'StartedEvent' ./internal/agent/`
**Commit:** `feat(agent): carry a recipe's resume command on its started phase`

## 5. A stopped block shows the resume hint

**What:** Depends on items 1 and 4. `workflowView` stores `Resume` from its Started phase and, when
`end == stopped` and the text is non-empty, renders it as one dim line after the totals line in
both `stageBody`/`renderWorkflowStages` and the text-mode `text()`. Persist it: `Resume string
json:"resume,omitempty"` on `session.Workflow` (internal/session/transcript.go), written by
`toWireWorkflow` and read by `fromWireWorkflow` (internal/tui/transcriptbridge.go), so a replayed
stopped block keeps the line.
**Regression guard.** Set `resume: stripEscapes(e.Resume)` in the view literal of
`addWorkflowPhase`'s Started branch (workflowblock.go), not in `workflowView.fold` — fold never
sees Started. Add `w.Resume = sanitize.StripEscapes(w.Resume)` to `stripWorkflow`
(internal/session/transcript.go), keeping `session.Workflow`'s "stripped on decode" contract.
**Goal:** A stopped recipe block ends with ``re-run `/audit internal/mcp security` to resume``,
live and after a save and reopen; finished, failed and `fan_out` blocks show no such line.
**Approach (assumed at the header base):** as above, in the Started view literal of
`addWorkflowPhase`, `workflowView.stageBody`, `workflowView.text`.
**Files:** internal/tui/workflowblock.go; internal/tui/transcriptbridge.go; internal/session/transcript.go; internal/session/transcript_test.go; internal/tui/workflowblock_test.go
**Read first:** internal/tui/workflowblock.go — addWorkflowPhase (Started literal), workflowView.text, workflowView.stageBody, renderWorkflowStages;
internal/tui/transcriptbridge.go — toWireWorkflow, fromWireWorkflow; internal/session/transcript.go — Workflow, stripWorkflow
**Tests:** `TestStoppedRecipeBlockShowsResumeHint`, `TestFinishedBlockHasNoResumeHint`,
`TestResumeHintSurvivesSaveAndReopen`; extend `TestTranscriptRoundTripsAWorkflowBlock` (Resume
round-trips) and `TestDecodeStripsTheWorkflowRecord` (an escape in Resume is stripped).
**Acceptance:**
- `go build ./...`
- `go test -count=1 -run 'ResumeHint' ./internal/tui/`
- `go test -count=1 ./internal/session/`
**Commit:** `feat(tui): show how to resume a stopped workflow`

## 6. Document resume, the kept trace and the new block

**What:** Depends on items 1–5. Rule: every doc sentence that says how a stopped workflow is
resumed, what a cancel leaves in the conversation, or that a workflow has one block. Find them with
`grep -rn -i -E "stopped by the user|resum|cancel" docs/manual/workflows.md docs/adr/0087-* docs/adr/0088-* docs/adr/0090-* CONTEXT.md`.
At least: workflows.md ("What comes back", "Starting a recipe", "Watching a workflow in the TUI");
ADR 0088 D1 ("An Exchange with no finished Turn is still scrapped") and D3 — dated amendment
covering a recipe launch's kept opening; ADR 0090 D1 — dated amendment: a resumed run opens its own
block, the stopped one is frozen; CONTEXT.md **Workflow**, **Recipe**, **Exchange** (the
cancelled-note fallback lines, "An Exchange with no finished Turn … is scrapped instead") — dated
amendment noting a recipe launch's opening is kept. The `turnLifecycle.settle` doc comment is
item 2's, not this item's. CHANGELOG via sidecar only.
**Regression guard.** The manual already uses "resume" for a session resume (--resume, --continue, /sessions); every new sentence about re-issuing a stopped workflow must say "re-run the same workflow" / "re-running `/<id> <text>`" and keep it distinct from a session resume, and where both meet (a replayed stopped block after a session resume) say both explicitly. The ratified hint strings of items 3–5 stay unchanged.
Name ADR 0088 D1 (a Turn with nothing issued is dropped), the turnLifecycle settle doc comment's owner is item 2 not item 6, and CONTEXT.md's Exchange cancelled-note fallback lines explicitly in item 6's What as sites to amend with a dated amendment noting a recipe launch's opening is kept.
**Goal:** The manual, ADRs 0088/0090 and CONTEXT.md describe the resume hint, the kept cancelled
recipe opening and the new block per resumed run, and none still says a recipe launch's cancel
drops the exchange.
**Approach (assumed at the header base):** prose edits; ADR changes as dated amendment blocks in
the style ADR 0087 already uses.
**Files:** docs/manual/workflows.md; docs/adr/0088-cancel-settles-and-never-rewinds-finished-work.md; docs/adr/0090-workflow-stages-are-enterable-views.md; CONTEXT.md
**Read first:** docs/manual/workflows.md — "What comes back", "Starting a recipe", "Watching a workflow in the TUI";
docs/adr/0088-cancel-settles-and-never-rewinds-finished-work.md — D1, D3; docs/adr/0090-workflow-stages-are-enterable-views.md — D1, D5;
docs/adr/0087-the-engine-runs-workflows-the-model-or-a-recipe-asks-for.md — "Amended 2026-09-27" block; CONTEXT.md — Workflow, Recipe, Exchange
**Tests:** none (docs).
**Acceptance:**
- `grep -n "to resume" docs/manual/workflows.md`
- `grep -n "Amended 2026-09-29" docs/adr/0088-cancel-settles-and-never-rewinds-finished-work.md docs/adr/0090-workflow-stages-are-enterable-views.md`
**Commit:** `docs(workflows): document resuming a stopped workflow`
