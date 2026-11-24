# Architecture-review cleanups — plan

**Goal:** close the ten remaining architecture-review cleanups: one advise call, one publish-step row, a run view's `~` estimate, a question pane that is one module, paste on the pane table with one draft edit door, one session seat, one Exchange-hold question, one config source with per-key provenance, and one Driver host. Behaviour changes only where a ratified call below says so.

**Date:** 2026-11-23
**Status:** in progress (2026-11-24) — items 1–14 done on the run branch, items 15–25 to do
**Resume (2026-11-24):**
- Run branch: local `implement-plan/2026-11-23_-_06_-_architecture-review-cleanups-plan`, cut from `main`; items 1–14 are one commit each there (1 `59d0d6d04` … 14 `d1b2bfd0b`). `main` is untouched and nothing is pushed.
- Next is item 15 (its dependency, item 14, is done).
- To resume: check out the run branch and invoke `/implement-plan 06` (or with this plan's path). The skill picks the branch up, skips the items marked done and continues at item 15; it takes a fresh capcom tree lock.
- Run state lives in `docs/skill-runs/implement-plan/2026-11-23_-_06_-_architecture-review-cleanups-plan/` (`ledger.md`, `notes-<id>.md`). That directory is git-ignored (`/docs/skill-runs/` in `.gitignore`), so it exists only on this machine and must survive here: it is the only copy of the pending CHANGELOG entries and ticket closes.
- Deferred to the closeout: each done item's CHANGELOG entry and the capcom tickets it closes (both held in the `notes-<id>.md` sidecars), and one non-blocking DEFER finding to file in capcom — item 12: with /stop-server or /unload-model, a heartbeat sent before the hold's release but folded after it also clears `beatOwed` (the beat chain is not re-armed at release); this matches the plan's "a beat has folded since the release" rule.
**sized for:** ~200k-context host
**base:** e3079a0c

**Sources:**
- `.capcom/issues/` — advise-call-copied, publish-step-tables, run-fill-value, question-pane-key-rung, paste-outside-pane-table, session-seat-copies, exchange-hold-subsets, config-read-entries, setting-source-split, driver-preamble-copied
- `docs/reviews/archived/architecture-review-2026-10-14.html` (#3–#6, #11, #14, #15), `-2026-10-27.html` (#7, #8), `-2026-11-02.html` (#9)
- ADR 0076 D7; ADR 0098; ADR 0053 (+ amend 1014); ADR 0029 D5; ADR 0025 D7; ADR 0089 D3; ADR 0096 D1/D6/D7; ADR 0083 §2/§5 + part 5; ADR 0103 D1; ADR 0075 D9; ADR 0031
- `internal/tui/doc.go` (value-copied Model rule, pane table, engine holds)

**Ratified design calls:**
- **Scope:** all ten review cleanups in one plan (owner, 2026-11-23).
- **Driver host:** ADR 0083 part 5 reopened by a dated amendment for the shared boot steps only; each Driver keeps its confinement verdict, the daemon its per-Firing fenced key resolver; the `firingInputs` composer stays out (owner, 2026-11-23).
- **Run fill:** a run view's gauge shows the viewed run's `~` estimate; window authority unchanged; sub-agent rows unchanged (owner, 2026-11-23).
- **Exchange holds:** every Exchange opener refuses mid-actuation; flush and wake wait and retry on release (owner, 2026-11-23); a flush refused by ANY refusing hold retries at that hold's release (owner, 2026-11-23).
- **Late ctrl+v:** clipboard text that arrives after a pane opened walks the pane table, never reaching the hidden draft (owner, 2026-11-23).
- **Missing workspace:** config's answer wins — no Project root, the existing skip notice, no project watcher/adoption watch/save target; the daemon drops the notice (owner, 2026-11-23).
- **Advise failures:** one call returns the failure and emits nothing; the cascade reports at once, the session lanes at the join (writer, 2026-11-23).
- **Estimate home:** a view-only `ctxEstimate` on the transcript run head (writer, 2026-11-23).
- **Question pane:** the picker keeps its own `isArmed`; esc at a conversation approval is declined to handleKey's shared double-tap; non-text widget Msgs walk on, a modal row swallows only typed text (writer, 2026-11-23).
- **Exchange question placement:** asked at the head of each opener's ladder, not inside `launchExchange`; `blockedUpstream` stays out of it; ladder order is holds, then upstream (writer, 2026-11-23).
- **Session seat:** per-door differences preserved (first-contact lines on a record seed only, `titleTouched` kept on resume, empty title keeps the name); the resume payload carries the record's raw currency (writer, 2026-11-23).
- **Config reads:** the package-level read entries stay exported for tests, a guard forbids production use; the TUI learns precedence from row/outcome bools (writer, 2026-11-23).
- **Driver host shape:** the TUI opens the store via `openStore` at its current beat; posture is a value each Driver judges; `close()` returns the teardown sentence (writer, 2026-11-23).

**Standing requirements:**
- skills: coding-standards
- Tests are narrowed to one package and a `-run` pattern; never race-run a whole heavy package (`./internal/tui/`, `./cmd/apogee/`) or a pattern.
- The Bubble Tea `Model` is copied by value: no pointer-held memo, `sync.Once` or `strings.Builder` on it; keys are `tea.KeyPressMsg` on `msg.String()`.
- Any authorized deviation from item text lands as a dated NOTES line under the item.

**Out of scope:**
- The event-chain composer replacing `firingInputs`; ADR 0083 part 5's full `hostBoot` (keys, skills, Bridge, globals).
- Unexporting the config read entries; deleting `LoadLayeredConfig`/`OverlayProjectLayer`.
- One fill value owning its window for every run (ADR 0063 D4 rebind rule); `/usage` and sub-agent row fill.
- Merging the picker latch into the question gate; full /clear vs resume parity.

**Regression check (2026-11-23, e3079a0c):**
- 1: guard folded
- 3: recast (writer decision); yields to ADR 0063 amend 0924
- 5: guard folded
- 7: guard folded; yields to model.go's `tea.ColorProfileMsg` rule (reaches the widgets through foldWidgetMsg)
- 8: guard folded
- 9: guard folded
- 10: guard folded
- 12: guard folded; supersedes ADR 0025 D7 and adr-0025-consequences' ungated auto-flush for the retry
- 13: guard folded (writer decision)
- 14: guard folded (writer decision)
- 15: recast (writer decision)
- 16: guard folded
- 18: guard folded
- 19: guard folded
- 20: guard folded
- 21: guard folded
- 22: recast (writer decision)
- 23: guard folded; yields to the CHANGELOG daemon startup sweep pin
- 24: recast (writer decision)
- 25: recast (writer decision)
- 3 (re-check): guard folded; yields to ADR 0063 amend 0924
- 14 (re-check): guard folded (writer decision)
- 15 (re-check): guard folded
- 23 (re-check): guard folded (writer decision)
- 24 (re-check): guard folded (writer decision)
- 25 (re-check): guard folded (writer decision)
- 24 (re-check, regression-8 G: lines): guard folded
- 25 (re-check, regression-8 G: lines): guard folded

## 1. One advise call answers every advise Moment as a value — ✅ DONE (2026-11-24)

NOTES (2026-11-24): adviseUser now lands every callAdvise error through reportSyncFailure, wrongHandler included (it used to return that one error unreported); the seams route only command/webhook/MCP handlers to it, so the branch is unreachable there and no behaviour changes.
NOTES (2026-11-24): isAdvice now reads domain's advise list (Moment.TakesAdvise), which also names file-changed; a cascade never fires at file-changed (subscribes widens it onto post-tool-result), so no firing's label or advice changes.

**What:**
**Goal:** one function in `internal/agent` switches on advise handler kind and redacts; `callSessionAdvice` is gone; `attachSessionAdvice` calls no `bookFiring`; `isAdvice` names no Moment list of its own; every existing advise test passes unchanged.
**Approach (assumed at the header base):**
- Add `func (a *Agent) callAdvise(ctx, turn int, doc domain.SeamPayload, r domain.Reaction) (domain.Outcome, error)` in reactions.go beside `adviseReplyCap` (parameter order of `adviseCaller`). Body = today's `callSessionAdvice`: `SessionStartedFunc` returns `Outcome{Inject, Detail}` unredacted; Argv/Webhook (`adviseReplyCap`)/MCP go through `runSync*` then `tools.RedactSecrets`; anything else `wrongHandler`. It never emits. Doc states ADR 0076 D7: fail-open, redaction first, the cap applied once downstream in `adviceOf`.
- `adviseUser` keeps its three seamPayload call sites (args reordered `(ctx, turn, doc, r)`) and becomes the cascade's landing: on error `a.reportSyncFailure(turn, r, doc.Event, err)` and `Outcome{}, nil`.
- Delete `callSessionAdvice`; construct.go sets `a.briefing.call = a.callAdvise`; fix the `adviseCaller`/`sessionBriefing.call` comments (briefing.go).
- Split `bookFiring`: the label rule + `ReactionFiredEvent` emit become `emitFiring(turn, m, r armedReaction, out)`; `attachSessionAdvice` calls it and drops its throwaway `result`/`fired`.
- domain/reaction.go: `takesAdvise` becomes exported method `func (m Moment) TakesAdvise() bool` (update Validate). `isAdvice` = advise class && Inject non-empty && `m.TakesAdvise()` && (`m != MomentPostResponse || !out.Retry`).
**Regression guard.** Every comment that names `callSessionAdvice` or contrasts the session lane with `adviseUser`'s dispatch (`grep 'callSessionAdvice\|through adviseUser\|adviseUser applies\|as adviseUser' internal/agent/*.go`, including sessionadvise.go's package comment at lines 32-37) is rewritten to name `callAdvise`.
**Files:** internal/agent/reactions.go, internal/agent/sessionadvise.go, internal/agent/briefing.go, internal/agent/construct.go, internal/agent/advisecall_test.go, internal/domain/reaction.go, internal/domain/reaction_test.go
**Read first:** internal/agent/sessionadvise.go — callSessionAdvice, attachSessionAdvice, landAdvice; internal/agent/reactions.go — adviseUser, bookFiring, isAdvice, adviceOf, fireLeg, seamPayload; internal/agent/briefing.go — adviseCaller, sessionBriefing.call; internal/agent/construct.go — seedTopLevel; internal/domain/reaction.go — takesAdvise, adviseMoments, Reaction.Validate; internal/agent/advise_webhook_test.go — replyWith, userAdviseWebhook; internal/agent/advise_argv_test.go — adviseArgvAgent; internal/agent/advise_test.go — firedAdvice (recordingSink in harness_test.go)
**Tests:** new `TestAdviseCall_AnswersAsAValue` (kind × outcome: redacted webhook answer, empty reply → zero/nil, non-2xx → error, `SessionStartedFunc` keeps Detail, `PostToolResultFunc` → wrongHandler; no event recorded in any row); new `TestMomentTakesAdvise`.
**Acceptance:**
- `go build ./... && go vet ./internal/agent/ ./internal/domain/`
- `go test -race -count=1 -run 'TestAdviseCall|TestAdvise|TestSessionAdvise_|TestSessionWindow_|TestCompactAdvise_|TestPreRequestAdvice|TestPostResponseAdvice' ./internal/agent/`
- `go test -race -count=1 -run 'TestMomentTakesAdvise|TestReactionValidate' ./internal/domain/`
- `! grep -n 'callSessionAdvice' internal/agent/*.go`
- `test "$(grep -ho 'runSyncWebhook(' internal/agent/reactions.go internal/agent/sessionadvise.go | wc -l)" -eq 1`
**Closes:** advise-call-copied
**Commit:** `refactor(agent): one advise call answers every advise Moment as a value`

## 2. A publish step is one row — ✅ DONE (2026-11-24)

**What:**
**Goal:** `cmd/release` has one step table `publishSteps []publishStep` whose rows carry name, probe, describe, converge and doneDetail; `publishAction`/`publishActions` and the by-name `slices.IndexFunc` join are gone; step names and order unchanged (JSON contract with the release skills, ADR 0098).
**Approach (assumed at the header base):** the merged type and table live in publish.go in place of `publishAction`/`publishActions`, field docs from both types, the order comment keeping both explanations. probes.go keeps the step-name consts, `stepState`, `stepReport`, `stepProbe` and `probeSteps` (ranges `publishSteps`, reads only `.probe`). `publishRun.probe` returns `probeSteps(ctx, r.commands, r.options.dir, r.version)`; `converge` and `printPublish` range `publishSteps`. Fix the `runPublish` comment naming `publishActions`.
**Files:** cmd/release/probes.go, cmd/release/publish.go, cmd/release/publish_test.go
**Read first:** cmd/release/publish.go — publishAction, publishActions, publishRun.probe, publishRun.converge, printPublish, runPublish; cmd/release/probes.go — publishStep, publishSteps, probeSteps, stepProbe; cmd/release/publish_test.go — TestPublishActionsMatchThePublishSteps; cmd/release/probes_test.go — TestProbeStepsRunsEveryStepInPublishOrder, TestProbesNeverReadAFailedCallAsDone; cmd/release/status.go — runStatus
**Tests:** delete `TestPublishActionsMatchThePublishSteps` (holds by construction); `TestProbeStepsRunsEveryStepInPublishOrder` pins the order.
**Acceptance:**
- `go build ./cmd/release/ && go vet ./cmd/release/`
- `go test -race -count=1 -run 'TestProbe|TestPublish|TestStatus|TestConverge' ./cmd/release/`
- `! grep -n 'publishAction' cmd/release/*.go`
**Closes:** publish-step-tables
**Commit:** `refactor(release): a publish step is one row`

## 3. A run view's gauge shows the viewed run's `~` estimate — ✅ DONE (2026-11-24)

NOTES (2026-11-24): the "before any child reading, against topLevelWindow" subtest the item says to replace does not exist in the tree; the "before any reading the slot reads esc back" case lands as a new subtest of TestRunViewGaugeStatesTheViewedRunsEstimate instead, and TestRunViewGaugeStatesTheViewedRun is unchanged.
NOTES (2026-11-24): scopeFill's per-scope rule is factored into a plain function isEstimateShown (top level: no reading or newer; run scope: a standing reading and newer), per the regression guard's recast rather than dropping the scope condition.
NOTES (2026-11-24): the stateErrored case is pinned both ways — with no child reading the slot reads `enter dismiss`; with a reading standing the gauge states that reading without `~`, matching the top level's "unless a reading stands" rule.

**What:** Recast at the regression check (2026-11-23).
**Goal:** inside a run view the gauge reads `~<est>/<limit>` once the viewed run's estimate is newer than its reading, and that run's next counting reading clears it; the top-level gauge, watch and transcript rows are byte-identical; the side panel's usage follows the gauge.
**Approach (assumed at the header base):**
- `entry` gains view-only `ctxEstimate int` (doc beside `ctxUsed`/`ctxLimit`), never projected by `toWireEntry`, not in the paint key.
- New `(t *transcript) applyEstimate(e domain.Event)`: a `ContextEstimateEvent` with `Depth > 0 && Tokens > 0` sets `openSubAgentHead(runOf(e.EventBase)).ctxEstimate` (no `t.touch()`). `applyUsage` clears it on a counting reading; a Maintenance reading leaves it. `foldEventAgainst` (fold.go) calls it beside `applyUsage`; `foldStats` stays depth-0 only.
- `viewedFill` run-view branch: limit as today (`head.ctxLimit`, else `topLevelWindow()`); if `head.ctxUsed > 0 && head.ctxEstimate > 0 && m.state != stateErrored` return `contextUsage{Used: est, Limit: limit, Estimated: true}`.
- `sidepanel.State.scopeFill` drops its `scope == (RunKey{})` condition. `readingFill` untouched.
- layout.md gauge paragraph: a tool result in the viewed run moves it to that run's `~` estimate until its next reading.
**Regression guard.** The run-view estimate shows only over a standing reading, `head.ctxUsed > 0 && head.ctxEstimate > 0 && m.state != stateErrored`. Before the viewed run's first reading the slot falls through to its hints (`esc back`, ADR 0063 amend 0924), so goldens t17-run-view and t18-run-view-finished stay byte-identical. Replace the "before any child reading, against topLevelWindow" subtest with "before any reading the slot reads esc back". `applyEstimate` does not call `t.touch()` (ctxEstimate is outside the paint key). Add internal/tui/sidepanel_test.go to Files. Add the cmd/apogee e2e test(s) that capture t17/t18 (from e2e_subagent_view_test.go) to Acceptance as a narrowed `go test -race -count=1 -run '<names>' ./cmd/apogee/`. Writer decision 2026-11-23. The item yields to ADR 0063 amend 0924 (`esc back` until the viewed run has reported usage). `scopeFill`'s child branch takes the run view's rule instead of dropping the scope condition: a scope other than `RunKey{}` takes its estimate only when `state.readUsed > 0 && state.isEstimateNewer`; `TestUsage_AChildScopeTakesItsEstimate` pins the no-reading child case (no estimate).
**Files:** internal/tui/transcript.go, internal/tui/fold.go, internal/tui/model.go, internal/sidepanel/sidepanel.go, internal/tui/fold_test.go, internal/tui/runview_test.go, internal/tui/sidepanel_test.go, internal/sidepanel/sidepanel_test.go, layout.md
**Read first:** internal/tui/model.go — viewedFill, contextGauge, statusRight, contextUsage.view; internal/tui/transcript.go — applyUsage, openSubAgentHead, entry (ctxUsed/ctxLimit), touch; internal/tui/fold.go — foldEventAgainst, foldStats; internal/sidepanel/sidepanel.go — scopeFill, readingFill, State.Fold, foldUsage; internal/tui/sidepanel.go — panelFill, panelSection; internal/tui/runview_test.go — TestRunViewGaugeStatesTheViewedRun, viewedRunUsage, plainSlot; internal/tui/sidepanel_test.go — TestSidePanelRunViewUsageMatchesTheGauge, panelModel, runUsage, gaugeFigures, openViewOf; cmd/apogee/e2e_subagent_view_test.go — TestE2ESubAgentView (run-view.yaml scripts no usage, so t17/t18 keep `esc back`)
**Tests:** new `TestApplyEstimateMovesOnlyItsRunsHead`, `TestRunViewGaugeStatesTheViewedRunsEstimate` (child reading limit; before any reading the slot reads `esc back`; stateErrored shows `enter dismiss`; top level unchanged; `subAgentFill` has no `~`), `TestUsage_AChildScopeTakesItsEstimate` (a child scope with an estimate and no reading takes no estimate); `TestSidePanelRunViewUsageMatchesTheGauge` gains an estimate case.
**Acceptance:**
- `go build ./... && go vet ./internal/tui/ ./internal/sidepanel/`
- `go test -race -count=1 -run 'TestApplyEstimate|TestFoldStatsEstimate|TestRunViewGauge|TestSidePanelRunViewUsage|TestContextGauge|TestWatch' ./internal/tui/`
- `go test -race -count=1 -run 'TestUsage_' ./internal/sidepanel/`
- `go test -race -count=1 -run '^TestE2ESubAgentView$' ./cmd/apogee/` (captures goldens t17-run-view and t18-run-view-finished)
**Closes:** run-fill-value
**Commit:** `feat(tui): a run view's gauge shows the viewed run's estimate`

## 4. One close for a question pane — ✅ DONE (2026-11-24)

**What:**
**Goal:** `pendingDecision.reset()` and `restoreAskDraft()` are each called from exactly one place, `letGoOfQuestion`; every way a question pane closes goes through it.
**Approach (assumed at the header base):**
- approval.go beside `openDecisionLatch`: `func (m *Model) letGoOfQuestion()` (`pendingDecision.reset()` then `restoreAskDraft()`) and `resumeAfterQuestion() tea.Cmd` (`letGoOfQuestion` then `resumeRunning`).
- `sendApproval`: send, `answeredInExchange = true`, return `resumeAfterQuestion()`; drop its hand-clears of `pending`, `ruleEdit`, `approvalArmed`, `approvalDrainMark`, `approvalDrainRelayed`.
- `submitAnswer` (ask.go): send, set `answeredInExchange`, `m.input.Reset()`, return `resumeAfterQuestion()`; drop the hand-clears and the separate `restoreAskDraft`.
- `withdrawAbandonedDecision` uses `resumeAfterQuestion`; `closeWorkflowPrompt` (workflow.go) and `finishWorker` (model.go) use `letGoOfQuestion` (finishWorker keeps `answeredInExchange = false`).
**Files:** internal/tui/approval.go, internal/tui/ask.go, internal/tui/workflow.go, internal/tui/model.go, internal/tui/approval_test.go
**Read first:** internal/tui/ask.go — submitAnswer, restoreAskDraft; internal/tui/approval.go — sendApproval, withdrawAbandonedDecision, takeApprovalOption; internal/tui/model.go — finishWorker, resumeRunning, pendingDecision.reset; internal/tui/workflow.go — closeWorkflowPrompt, answerWorkflowPrompt, dismissWorkflowPrompt; internal/tui/model_test.go — TestAskGivesTheBorrowedDraftBack; internal/tui/approval_test.go — TestStoppedChildWithdrawsItsApprovalPane
**Tests:** new `TestEveryQuestionCloseForgetsTheWholeDecision` (approval letter, ask ⏎, abandoned approval, workflow prompt dismiss, `cancelledMsg`: `pendingDecision` zero and `askDraft == ""`).
**Acceptance:**
- `go build ./internal/tui/ && go vet ./internal/tui/`
- `go test -race -count=1 -run 'TestEveryQuestionCloseForgetsTheWholeDecision|TestAskGivesTheBorrowedDraftBack|TestStoppedChildWithdrawsItsApprovalPane|TestBackgroundWorkflow_EndingTheWorkflowClosesItsOpenPrompt|TestReleaseEngine_CloseWorkflowPrompt|TestModelApprovalRuleFieldSavesEachRuleOfTheLine' ./internal/tui/`
- `test "$(grep -hE '^\s*m\.pendingDecision\.reset\(\)' internal/tui/*.go | wc -l)" = 1 && test "$(grep -hE '^\s*m\.restoreAskDraft\(\)' internal/tui/*.go | wc -l)" = 1`
**Commit:** `refactor(tui): close every question pane through one let-go`

## 5. One armed gate for a question's answer — ✅ DONE (2026-11-24)

**What:** Depends on item 4.
**Goal:** no `m.approvalArmed` read outside `answerWhenArmed`; the latch-generic reads (`startupLatchCmds`, `picker.isArmed`) stay; ⏎, the decision letters and the second click on both panes go through it; `picker.isArmed` is untouched.
**Approach (assumed at the header base):** add `func (m Model) answerWhenArmed(answer func(Model) (tea.Model, tea.Cmd)) (tea.Model, tea.Cmd)` beside `foldApprovalArmed` (unarmed → `(m, nil)`, the key or click swallowed). Callers: handleKey enter for ask (`submitAnswer`) and approval (`resolveApproval`); `handleApprovalKey` decision letter (closure over `takeApprovalOption(opt)`); `handleAskClick`/`handleApprovalClick` second click (closure clearing `m.clickArmed` then answering; still claimed; an unarmed click leaves the arm standing).
**Regression guard.** `handleApprovalKey`'s letter gate (`if !m.approvalArmed`, approval.go:361) converts too: after this item approval.go holds exactly one `m.approvalArmed` read, the one in `answerWhenArmed` (item 4 removed the clear at approval.go:480), pinned by the Acceptance count below.
**Files:** internal/tui/approval.go, internal/tui/model.go, internal/tui/mouse.go, internal/tui/approval_test.go
**Read first:** internal/tui/approval.go — handleApprovalKey, foldApprovalArmed, resolveApproval, takeApprovalOption; internal/tui/model.go — handleKey (enter case), startupLatchCmds, acceptedModel; internal/tui/mouse.go — handleAskClick, handleApprovalClick; internal/tui/picker.go — picker.isArmed; internal/tui/model_test.go — TestModelApprovalKeysAreDeadUntilArmed, TestModelAskEnterIsDeadUntilArmed; internal/tui/mouse_test.go — TestApprovalClickBeforeTheLatchRulesNothing
**Tests:** new `TestAnswerWhenArmedSwallowsUntilTheLatchOpens`.
**Acceptance:**
- `go build ./internal/tui/ && go vet ./internal/tui/`
- `go test -race -count=1 -run 'TestAnswerWhenArmed|TestModelApprovalKeysAreDeadUntilArmed|TestModelAskEnterIsDeadUntilArmed|TestApprovalClickBeforeTheLatchRulesNothing|TestAskClickSendsNothingUntilThePaneArms|TestAskClickHighlightsThenTheSecondClickSends' ./internal/tui/`
- `! grep -n 'm\.approvalArmed' internal/tui/model.go internal/tui/mouse.go internal/tui/ask.go`
- `test "$(grep -c 'm\.approvalArmed' internal/tui/approval.go)" = 1`
**Commit:** `refactor(tui): gate every question answer on one armed check`

## 6. The prompt row gets a soft-modal key rung — ✅ DONE (2026-11-24)

NOTES (2026-11-24): internal/tui/approval_test.go needed no change (named in **Files:**); TestPromptKeyClaimIsSoftModal lives in keyclaim_test.go beside the order test, and TestEveryFramePaneHasASpec now also asserts every row has a non-zero key rank.
NOTES (2026-11-24): a prompt claim now goes through claimKey, so the transcript clamp is freshened after it (claimKey's shared repair); Update's tail settled it anyway, so nothing observable moves.

**What:** Depends on item 5.
**Goal:** every `paneSpecs` row has a key claim; `keyClaimOrder` lists "prompt" after "mcp pane"; handleKey holds no awaiting-state, rule-field, workflow-prompt or `askChoiceKey` branch; `handleApprovalKey` is gone; the prompt claim is testable without handleKey.
**Approach (assumed at the header base):**
- panes.go panePrompt row: `key: paneClaim(Model.promptKey)`, `keyOpen: Model.promptOpen`, `rank: paneRank{key: 11, pointer: 10}` (below the five reports; its gate is disjoint from every rung above). `promptKey` dispatches by state to `approvalKey` or `askKey`, else declines. Rewrite the paneSpec/paneRank doc lines.
- approval.go: `handleApprovalKey` → `approvalKey(msg) (bool, tea.Model, tea.Cmd)`: rule field open → `projectRuleKey` takes every key; esc claimed only for a workflow prompt while `!m.watch.isActive` (`dismissWorkflowPrompt`), else declined to handleKey's shared esc; enter and decision letters via `answerWhenArmed`; ↑/↓ move `approvalSel`; everything else declined.
- ask.go: new `askKey`: esc declined while watching, workflow prompt → `dismissWorkflowPrompt`, else `stopWorker`; enter via `answerWhenArmed(submitAnswer)`; the rest to `askChoiceKey`.
- model.go handleKey: delete those branches; update the frameChord and keyClaimOrder comments. doc.go: update the approval.go/ask.go paragraphs.
**Files:** internal/tui/panes.go, internal/tui/approval.go, internal/tui/ask.go, internal/tui/model.go, internal/tui/doc.go, internal/tui/keyclaim_test.go, internal/tui/panes_test.go, internal/tui/approval_test.go
**Read first:** internal/tui/panes.go — paneSpecs panePrompt row, keyClaimRungs, paneClaimant, promptOpen; internal/tui/model.go — handleKey, claimKey, paneClaim, transcriptClaimants, frameChord; internal/tui/approval.go — handleApprovalKey, projectRuleKey, editingProjectRule; internal/tui/ask.go — askChoiceKey; internal/tui/workflow.go — dismissWorkflowPrompt, workflowPromptOrigin; internal/tui/blockcursor.go — blockCursorOwnsKeys; internal/tui/runview.go — runViewOwnsEsc; internal/tui/keyclaim_test.go — TestKeyClaimOrderMatchesTheDocumentedPrecedence
**Tests:** new `TestPromptKeyClaimIsSoftModal` (calls `paneSpecs[panePrompt].key`: approval claims a/s/d/p/↑/↓/⏎ and declines pgup/pgdown/shift+tab/tab/ctrl+g/x; ask claims ⏎, esc, and ↑/↓/␣ on an empty box only, declines printable keys and esc while watching); update `TestKeyClaimOrderMatchesTheDocumentedPrecedence`, `TestEveryFramePaneHasASpec`.
**Acceptance:**
- `go build ./internal/tui/ && go vet ./internal/tui/`
- `go test -race -count=1 -run 'TestPromptKeyClaimIsSoftModal|TestKeyClaimOrderMatchesTheDocumentedPrecedence|TestEveryFramePaneHasASpec|TestTheFirstClaimantThatWantsAKeyAnswersIt|TestModelApprovalRuleFieldTakesTheKeys|TestFrameChordPassesTheApprovalRuleField|TestModelApprovalKeysAreDeadUntilArmed|TestModelAskEnterIsDeadUntilArmed|TestStoppedChildWithdrawsItsApprovalPane' ./internal/tui/`
- `! grep -n 'handleApprovalKey' internal/tui/*.go`
- `! sed -n '/^func (m Model) handleKey/,/^}/p' internal/tui/model.go | grep -nE 'stateAwaiting|editingProjectRule|workflowPromptOrigin|askChoiceKey'`
**Closes:** question-pane-key-rung
**Commit:** `refactor(tui): give the question pane its own key rung`

## 7. Text walks the pane table — ✅ DONE (2026-11-24)

NOTES (2026-11-24): the ask-at-an-image-path case of TestNoModalPaneLetsAPasteReachTheDraft is its own top-level test, TestNoModalPaneLetsAPasteReachTheDraftAskAttachesAnImagePath (matched by the Acceptance -run pattern), because it needs a vision-server model rather than a paneFixtures row.
NOTES (2026-11-24): pasteIntoBox now returns (Model, tea.Cmd) instead of (tea.Model, tea.Cmd) so the ask's text answer can hand it back in the row's three-part currency; the /settings row's text answer (settingsText) lives in panes.go beside promptText, since settings.go is not in the item's Files and its two routes (settingsPaste, settingsEditorMsg) are unchanged.
NOTES (2026-11-24): TestEveryModalRowGatesItsText also pins that every modal row has a keyOpen gate, since walkText's modal swallow is gated only by keyOpen.

**What:** Depends on item 6. Behaviour change (owner call): a ctrl+v clipboard reply that arrives after a pane opened walks the table instead of landing in the hidden draft.
**Goal:** `foldPaste`, `foldWidgetMsg` and `foldClipboardText` each ask `walkText` before the box; `listPaste` and the hand-ordered chains are gone; no modal row the walk reaches lets pasted text into the draft (ADR 0053 amend-1014 rule 9); the /settings key list keeps its own adapter.
**Approach (assumed at the header base):**
- paneSpec gains `text func(Model, tea.Msg) (Model, tea.Cmd, bool)` (nil = no field). panes.go derives `keyPanes []framePane` (key-rank order; `keyClaimRungs` reads it) and `walkText(msg)`: skip a row whose `keyOpen` is non-nil and false, ask `row.text`, first claim answers; a modal row that does not claim swallows typed text (`tea.PasteMsg`, `clipboardTextMsg` via `pastedText`), any other widget Msg walks on.
- Rows: browser → `sessionBrowserPaste`; picker → `pickerPaste` (listPaste's picker branch, moved into listsurface.go); workflows → `workflowsPaste`; settings → `settingsPaste` for text, `settingsEditorMsg` otherwise; prompt → `projectRuleMsg` first, then text into the box the ask borrows; dropdown and the reports nil.
- prompteditor.go: `foldPaste` = `walkText` then `pasteIntoDraft` (inputEditable → `attachPastedImage` → `pasteIntoBox`); `foldWidgetMsg` = `walkText` then `fitWrapMemo` + `m.input.Update`. clipboard.go: `foldClipboardText` = `walkText` then `pasteIntoBox`; `foldClipboardImage` keeps its `inputEditable` gate.
- Delete `listPaste`; fix comments naming it (sessions.go, workflows.go) and doc.go's pane-table field list.
**Regression guard.** The prompt row's ask branch sends a `tea.PasteMsg` to `pasteIntoDraft` (attach, then box) and a `clipboardTextMsg` to `pasteIntoBox` only (foldClipboardText's rule, clipboard.go:187: ctrl+v text never attaches). The browser/picker/workflows/prompt-ask text adapters claim only what `pastedText` recognises and return false on any other Msg; only `settingsEditorMsg` and `projectRuleMsg` take every Msg, as today. The item yields to model.go's `tea.ColorProfileMsg` case: it still reaches the widgets through `foldWidgetMsg`.
**Files:** internal/tui/panes.go, internal/tui/prompteditor.go, internal/tui/clipboard.go, internal/tui/listsurface.go, internal/tui/sessions.go, internal/tui/workflows.go, internal/tui/doc.go, internal/tui/panes_test.go
**Read first:** internal/tui/prompteditor.go — foldPaste, foldWidgetMsg, pasteIntoBox, attachPastedImage; internal/tui/listsurface.go — listPaste, typeIntoOverlayFilter, listKey; internal/tui/clipboard.go — foldClipboardText, foldClipboardImage; internal/tui/settings.go — settingsPaste, settingsEditorMsg, settingsOwnsInput; internal/tui/approval.go — projectRuleMsg; internal/tui/sessions.go — sessionBrowserPaste; internal/tui/workflows.go — workflowsPaste; internal/tui/panes_test.go — paneFixtures
**Tests:** new `TestNoModalPaneLetsAPasteReachTheDraft` (sweep `paneFixtures` modal rows with `tea.PasteMsg` and `clipboardTextMsg`; draft unchanged; ask fixture lands in the box, and an image-path bracketed paste at the ask attaches), `TestTextWalkLandsAPasteInTheApprovalRuleField`, `TestEveryModalRowGatesItsText` (a non-text widget Msg walks past an open browser, picker or workflows row), `TestLateClipboardTextLandsInTheOpenPane` (fails against the pre-item tree).
**Acceptance:**
- `go build ./internal/tui/ && go vet ./internal/tui/`
- `go test -race -count=1 -run 'TestNoModalPaneLetsAPasteReachTheDraft|TestTextWalk|TestEveryModalRowGatesItsText|TestLateClipboardText|TestPickerPasteLandsInTheFilter|TestQuestionPickerPasteIsDropped|TestSessionBrowserPaste|TestSettingsPaste|TestWorkflowsViewPaste|TestPasteInsertsAndRefreshes|TestPasteIgnoredWhereInputIsInert|TestPasteCtrlV|TestPasteImagePath' ./internal/tui/`
- `! grep -n 'listPaste' internal/tui/*.go`
**Commit:** `refactor(tui): route paste and widget text through the pane table`

## 8. One edit operation for the prompt draft — ✅ DONE (2026-11-24)

NOTES (2026-11-24): consequential edit — internal/tui/lineeditor.go: made necessary by the fitWrapMemo doc naming handleKey's fall-through and foldPaste as the prompt's widget routes; it now names Model.editDraft (and foldWidgetMsg)
NOTES (2026-11-24): the edits are named funcs in prompteditor.go (draftMsg, emptyDraft, draftTo, draftSpliced, draftCut) rather than inline closures, so no caller file writes the widget at all; spliceCompletion edits with draftKeep and then runs its own recomputeAutocomplete in every state, as before, because its menu can still be accepted (by click) after a run ends — at errored the accept must close it (TestAcceptingACompletionAtErroredClosesTheMenu)

**What:** Depends on items 4 and 7.
**Goal:** outside prompteditor.go no file calls `m.input.SetValue`, `Reset` or `Update`; every draft write goes through `editDraft` and names its overlay policy.
**Approach (assumed at the header base):**
- prompteditor.go: `type draftOverlay uint8` (`draftRederive`, `draftDismiss`, `draftKeep`); `func (m *Model) editDraft(change func(*promptEditor) tea.Cmd, overlay draftOverlay) tea.Cmd`: `m.sel = fieldSel{}`, `m.dropRecall()`, `m.fitWrapMemo()`, `change`, then the policy (rederive = `recomputeAutocomplete` in live states, batching its reload; dismiss = `dismissAutocomplete`; keep = nothing).
- Callers: handleKey typing fall-through and selection delete (rederive); `pasteIntoBox` (rederive); ask.go `foldAskRequest` (Reset, dismiss), `submitAnswer` (Reset, keep), `restoreAskDraft` (SetValue+MoveToEnd, keep; empty-stash early return stays outside); autocomplete.go `removeCompletionToken` (dismiss), `spliceCompletion` (rederive); interject.go `popDeferredCommand` (keep), `popInterjection` (rederive); recall.go `showRecall` (dismiss), `recallPastNewest` (Reset, dismiss).
- Out: the send-boundary `promptEditor.reset()` callers and `foldWidgetMsg`'s box branch (a widget Msg is not an edit). Accepted change: the ask's restore and submit drop a stale drag-selection.
**Regression guard.** `showRecall` sets `recall.pos`/`recall.active` after `editDraft` returns (or inside `change`), never before it: `editDraft` runs `dropRecall` first, so a marker set ahead of the call is cleared and a second ↑ would restart at the newest entry.
**Files:** internal/tui/prompteditor.go, internal/tui/model.go, internal/tui/ask.go, internal/tui/autocomplete.go, internal/tui/interject.go, internal/tui/recall.go, internal/tui/prompteditor_test.go
**Read first:** internal/tui/prompteditor.go — promptEditor.reset, pasteIntoBox; internal/tui/model.go — handleKey (selection delete, typing fall-through); internal/tui/autocomplete.go — recomputeAutocomplete, dismissAutocomplete, removeCompletionToken, spliceCompletion, completionRegion; internal/tui/recall.go — showRecall, recallPastNewest, recallKey, dropRecall; internal/tui/interject.go — popDeferredCommand, popInterjection; internal/tui/ask.go — foldAskRequest, restoreAskDraft, submitAnswer; internal/tui/lineeditor.go — fitWrapMemo
**Tests:** new `TestDraftEditRunsEveryAfterEditDuty` (per policy, from a selection + live recall + open overlay), `TestAskDraftRestoreDropsAStaleSelection`.
**Acceptance:**
- `go build ./internal/tui/ && go vet ./internal/tui/`
- `go test -race -count=1 -run 'TestDraftEditRunsEveryAfterEditDuty|TestAskDraftRestoreDropsAStaleSelection|TestPasteInsertsAndRefreshes|TestPasteRecomputesAutocomplete|TestSelectionDelete|TestAcceptSplicesInPlaceAndReSeatsTheCaret|TestBackspaceEmptyPops|TestRecallCommandOpensNoAutocomplete|TestRecallDownWalksNewerAndEmptiesPastTheNewest|TestRecallEndsOnPasteAndOnAClickInTheBox|TestAskGivesTheBorrowedDraftBack|TestBandRanksOncePerPaste' ./internal/tui/`
- `! grep -nE 'm\.input\.(SetValue|Reset|Update)\(' internal/tui/ask.go internal/tui/autocomplete.go internal/tui/interject.go internal/tui/recall.go internal/tui/model.go`
**Closes:** paste-outside-pane-table
**Commit:** `refactor(tui): give the prompt draft one edit operation`

## 9. Seat a session through one operation: /clear, /new, /sessions, /fork — ✅ DONE (2026-11-24)

NOTES (2026-11-24): consequential edit — internal/tui/autotitle.go: made necessary by deleting resetSessionView's direct sessionName clear; nameSession's doc now names seatSession's zero seed as the one other writer instead of startNewSession
NOTES (2026-11-24): resumeLoaded's naming latch and cacheBoundary now run at the end of the seat (after the repaint) rather than before beginSession; no step in between reads either, so the seated fields and the frame are the same — the restore/fork/clear acceptance tests pass unchanged
NOTES (2026-11-24): the regression guard was checked by mutation: seeding the side panel's usage for a zero seed fails TestSeatSessionZeroSeedEmptiesEverySessionField's live-workflow subtest

**What:**
**Goal:** `Model.seatSession(sessionSeed)` is the only code writing the session-owned fields at /clear, /new, a /sessions restore and /fork; `resetSessionView` no longer exists; frames unchanged.
**Approach (assumed at the header base):**
- sessionview.go: `type sessionSeed struct{ fromRecord bool; meta session.Meta; transcript []byte; inExchange bool; boundary *domain.Session }` (zero = empty session); `recordSeed(rec session.Record, inExchange bool)`.
- `seatSession` order: `beginSession`; [record] `replaceFirstContactLines`; `panel = nil`, `resetPanelDisk`; [record] `replayScrollback`; `reseatViewStack`, `noteContextFiles`; `spentSkills = nil`, `resetSessionBoards`, `liveStats.reset`, `ctxUsed = meta.CtxUsed`; usage/delegate usage from meta, `seedSidePanelUsage`; `workflows.rebased()`, `servedModels`; `sessionCurrency = stripEscapes(meta.EffectiveCurrency(""))`; `detached = false`, `flash = ""`, `pendingTitle.drop()`; naming (record: `autoTitleFired = true`, `nameSession(meta.Title)`; zero: clear `autoTitleFired`, `titleTouched`, `sessionName`); boundary zeroed then `cacheBoundary(*seed.boundary)` when set.
- commandrun.go: `startNewSession`'s prebound branch and `clearSession` call `seatSession(sessionSeed{})`; delete `resetSessionView`. sessions.go `resumeLoaded` keeps its engine half and then calls `seatSession(recordSeed(msg.rec, m.eng.InExchange()))`.
- Retarget `TestSessionViewHoldsEveryFieldTheBoundariesReset` to {"seatSession","resumeLoaded"}; fix every comment naming `resetSessionView` (`grep -rn resetSessionView internal/tui .capcom/knowledge`).
**Regression guard.** `seatSession` calls `seedSidePanelUsage` only for a record seed (today's per-door split): a zero seed runs before `workflows.rebased()`, so a live background workflow's earlier spend (`delegateUsageTotal`) would otherwise create a usage section where /clear today leaves the panel nil.
**Files:** internal/tui/sessionview.go, internal/tui/commandrun.go, internal/tui/sessions.go, internal/tui/workflow.go, internal/tui/watch_test.go, internal/tui/sidepanel_test.go, internal/tui/usage_test.go, internal/tui/sessionview_test.go, .capcom/knowledge/glossary/glossary-identity/watch-mode.md
**Read first:** internal/tui/commandrun.go — resetSessionView, startNewSession, clearSession; internal/tui/sessions.go — resumeLoaded; internal/tui/watch_test.go — TestSessionViewHoldsEveryFieldTheBoundariesReset, fieldsWritten; internal/tui/sidepanel.go — seedSidePanelUsage, resetPanelDisk; internal/tui/startupchrome.go — beginSession; internal/tui/model.go — replayScrollback, noteContextFiles, replaceFirstContactLines; internal/tui/sidepanel_test.go — panelDiskModel ("/clear" subtest); internal/tui/autotitle.go — nameSession
**Tests:** new `TestSeatSessionZeroSeedEmptiesEverySessionField` (with a zero-seed case under a spent live workflow: the panel stays nil), `TestSeatSessionRecordSeedRelightsFromTheRecord`.
**Acceptance:**
- `go build ./... && go vet ./internal/tui/`
- `go test -race -count=1 -run 'TestSeatSession|TestSessionViewHoldsEveryFieldTheBoundariesReset|TestSessionBrowserResume|TestSessionNameFollowsResume|TestClearResetsToStartupBox|TestClearResetsTheUsageTallies|TestSessionsResumeSeatsTheRecordsLabel|TestSessionNameResetByClear|TestAutoTitleFiresAgainAfterNewSession|TestSidePanelClearEmptiesThePanel|TestSidePanelResume|TestClearOnInterruptedStillAborts|TestContextGaugeEstimateAfterClear|TestSessionSwitchWithAWorkflowRunning|TestForkStartsAtZeroSpend' ./internal/tui/`
- `! grep -rn 'resetSessionView' internal/tui/ .capcom/knowledge/`
**Commit:** `refactor(tui): seat a session through one operation from a record or a zero seed`

## 10. The boot resume takes the same seat — ✅ DONE (2026-11-24)

NOTES (2026-11-24): consequential edit — internal/tui/perfaudit_bench_test.go: made necessary by deleting replayResumed; paModel's doc comment named it as the --resume path and now names resumedSeed
NOTES (2026-11-24): the boot seat now derives sessionCurrency as a /sessions restore does (meta.EffectiveCurrency("") over a Meta built from ResumedSession.Currency), so a record with no priced call seats "" (reads the configured label live) where replayResumed seated the binary-resolved configured label as an override; the label shown and counted is the same unless `currency:` changes mid-session — item 11 owns the one currency rule
NOTES (2026-11-24): the seat-owned fields comparison clears each entry's commit time (`at`) before comparing transcripts: the resume note is stamped when added, so two seats a moment apart differ in that alone
NOTES (2026-11-24): sessionview.go's usageBase/sessionCurrency field docs and seatSession's naming comment also named replayResumed or the separate --resume start; reworded to the one seat

**What:** Depends on item 9.
**Goal:** boot resume, /sessions and /clear reach the session-owned fields through `seatSession` alone; `replayResumed` is gone; a record boot-resumed and the same record restored through /sessions end with identical seat-owned fields and transcript.
**Approach (assumed at the header base):** sessionview.go `resumedSeed(r *ResumedSession) sessionSeed` (nil → zero; else `fromRecord`, meta from Title, CtxUsed, Usage, DelegateUsage, ServedModels, Currency; transcript and inExchange from the payload; boundary nil). `newModel`: `m.beginSession()` becomes `m.seatSession(resumedSeed(opts.Resumed))`; delete the `replayResumed` and separate `noteContextFiles` calls (order box → replay → context notice holds); `resumePending` unchanged. Move doc references to the seat (autotitle.go, transcript.go, sidepanel.go, usage_test.go).
**Regression guard.** `TestBootResumeAndBrowserRestoreSeatAlike` compares the listed seat-owned fields — transcript entries, naming, `usage`/`usageBase`/`delegateUsage`, `servedModels`, `sessionCurrency`, `ctxUsed`, panel usage — and excludes `boundary`/`hasBoundary` (the browser door caches the record's boundary, `resumedSeed`'s is nil) and `panelDisk.gen` (boot 0→1, a browser restore 1→2). sessions.go's `replayResumed` comments (lines 883, 945) are rewritten.
**Files:** internal/tui/model.go, internal/tui/sessionview.go, internal/tui/sessions.go, internal/tui/autotitle.go, internal/tui/transcript.go, internal/tui/sidepanel.go, internal/tui/usage_test.go, internal/tui/sessionview_test.go
**Read first:** internal/tui/model.go — newModel, replayResumed, replayScrollback; internal/tui/sessions.go — resumeLoaded; internal/tui/tui.go — ResumedSession; internal/tui/sessionsave.go — cacheBoundary; internal/tui/usage_test.go — resumedUsageModel, labelledResumeModel; internal/tui/sidepanel_test.go — TestSidePanelResumeRebuildsThroughBothDoors; internal/tui/sessions_test.go — loadingBrowserModel
**Tests:** new `TestBootResumeAndBrowserRestoreSeatAlike` (blob, priced EUR usage, delegate usage, served models, title; boundary/hasBoundary and panelDisk.gen excluded).
**Acceptance:**
- `go build ./... && go vet ./internal/tui/`
- `go test -race -count=1 -run 'TestBootResumeAndBrowserRestoreSeatAlike|TestSeatSession|TestNewModelReplaysResumedScrollback|TestNewModelResumeCorruptBlobDegrades|TestResumedMidExchangeShowsInterruptedNote|TestFirstContactNotesStayOutOfAResumedRecord|TestColdStartOfflineStaysOutOfAResumedRecord|TestAutoTitleLatchedByAResumedSession|TestUsageAccumulatesOverAResumedReading|TestResumedSessionCountsUnderItsOwnLabel|TestSidePanelResumeRebuildsThroughBothDoors|TestSessionViewHoldsEveryFieldTheBoundariesReset' ./internal/tui/`
- `! grep -rn 'replayResumed' internal/ cmd/ .capcom/knowledge/`
**Commit:** `refactor(tui): seat the boot resume through the session seat`

## 11. A resumed session's currency label by one rule — ✅ DONE (2026-11-24)

NOTES (2026-11-24): internal/tui/sessionview.go needed no edit — seatSession already held the one non-test EffectiveCurrency call (item 10) and its comments still hold; it is listed in the plan's Files but absent from FILES
NOTES (2026-11-24): consequential edit — internal/tui/usage_test.go: made necessary by ResumedSession.Currency now carrying the record's own label; labelledResumeModel's doc said the binary had resolved the label
NOTES (2026-11-24): consequential edit — .capcom/knowledge/decisions/adr-0093/adr-0093-amend-1004.md: made necessary by the payload no longer carrying the effective label; dated blockquote under the sentence that said it did
NOTES (2026-11-24): consequential edit — .capcom/knowledge/decisions/adr-0093.md: made necessary by the amend-1004 blockquote; the Status line names it, as AGENTS.md requires

**What:** Depends on item 10.
**Goal:** `EffectiveCurrency` is called for the TUI's label in exactly one non-test place (`seatSession`); `resumedSession` takes no configured-currency argument; the label shown is unchanged.
**Approach (assumed at the header base):** cmd/apogee/wire_session.go `resumedSession(rec *session.Record, inExchange bool)` sets `Currency: rec.Meta.Currency`; its wire_options.go caller drops `w.opts.Currency`. tui.go: `ResumedSession.Currency` doc = the record's own stored label, the seat applies the rule. The host's `sessionLabel` is untouched.
**Files:** internal/tui/tui.go, internal/tui/sessionview.go, internal/tui/sessionview_test.go, cmd/apogee/wire_session.go, cmd/apogee/wire_options.go, cmd/apogee/wire_session_test.go
**Read first:** cmd/apogee/wire_session.go — resumedSession, sessionHost.sessionLabel; cmd/apogee/wire_options.go — the Resumed: resumedSession(...) option; cmd/apogee/wire_session_test.go — TestResumedSessionPayloadCarriesTheEffectiveCurrency (and three more resumedSession call sites); internal/session/store.go — Meta.EffectiveCurrency; internal/tui/usage.go — Model.currency, countedUsage; internal/tui/usage_test.go — labelledResumeModel, TestResumedSessionCountsUnderItsOwnLabel; internal/tui/tui.go — ResumedSession.Currency
**Tests:** rename `TestResumedSessionPayloadCarriesTheEffectiveCurrency` → `TestResumedSessionPayloadCarriesTheRecordsOwnLabel`; new `TestBootResumeSeatsTheLabelByTheOneRule` (`Options.Currency` "USD": priced EUR → "EUR"; unpriced EUR → `currency()` "USD").
**Acceptance:**
- `go build ./... && go vet ./internal/tui/ ./cmd/apogee/`
- `go test -race -count=1 -run 'TestResumedSession' ./cmd/apogee/`
- `go test -race -count=1 -run 'TestBootResumeSeatsTheLabelByTheOneRule|TestResumedSessionCountsUnderItsOwnLabel|TestSessionsResumeSeatsTheRecordsLabel|TestBootResumeAndBrowserRestoreSeatAlike' ./internal/tui/`
- `test "$(grep -rn 'EffectiveCurrency(' internal/tui/ --include=*.go | grep -vc _test.go)" = 1`
**Closes:** session-seat-copies
**Commit:** `refactor(tui): derive a resumed session's currency label by one rule in the seat`

## 12. Flush and wake wait out every refusing hold — ✅ DONE (2026-11-24)

NOTES (2026-11-24): the "beat folded since the release" guard is `actuation.beatOwed`. `foldActuationDone` sets it only when the heartbeat is wired (`observesUpstream`), so a session with no monitor never waits for a beat that will not come. `foldBeatMsg` clears it on any live beat, and the snapshot carries it as `engineHolds.isBeatOwed`, which `canRetryFlush` and `canOpenWake` read.
NOTES (2026-11-24): beyond the item text, `noteHeldQueue` also clears `flushPending`, so rows a stop or a fault holds are never retried at a later release, even if a hold had turned their flush away earlier.
NOTES (2026-11-24): consequential edit — .capcom/knowledge/decisions/adr-0025/adr-0025-decision.md: made necessary by the flush retry; dated blockquote under D7's "a natural completion auto-sends what is left" pointing to adr-0029-amend-1123
NOTES (2026-11-24): consequential edit — .capcom/knowledge/decisions/adr-0025/adr-0025-consequences.md: made necessary by the gated retry; dated blockquote under the "auto-flush ... deliberately not gated" bullet
NOTES (2026-11-24): consequential edit — .capcom/knowledge/decisions/adr-0025.md: made necessary by the two blockquotes above; its Status line names adr-0029-amend-1123
NOTES (2026-11-24): `loadHoldNote` stays for the human openers (item 13 removes it). Its doc comment, the hold-notes const comment and `Model.canWake`'s doc were reworded because the completion flush now asks `exchangeRefusal` and the wake asks `canOpenWake`. doc.go's heartbeat paragraph is left for item 13, as the plan says.
NOTES (2026-11-24): edge case left open: with `/stop-server` or `/unload-model`, a beat sent before the release but folded after it also clears `beatOwed`, because the old beat chain keeps its generation and is not re-armed. The plan's rule is "a beat has folded since the release", and this follows it.

**What:** Behaviour change by owner call (2026-11-23): neither the completion flush (ADR 0025 D7) nor the background-workflow wake (ADR 0089 D3) opens an Exchange while a refusing hold stands; both retry at its release. Today a queued idle-only `/model` drained at completion, the start-up restore, or a workflow ending during a load can open one into a restarting server.
**Goal:** `engineHolds` answers one "refuse this Exchange?" question and both automatic openers ask it; a completion under a hold leaves rows staged with the hold's note and flushes at release; a pending wake fires at release; rows held by esc or a fault still wait for ⏎; ADR 0029 carries `adr-0029-amend-1123`.
**Approach (assumed at the header base):**
- engineholds.go: `holdsRefusingExchange = holdSessionLoad|holdBgLaunch|holdGitAction|holdActuation`; `refusingExchange() (engineHold, bool)` (first held, submit's note order); `mayOpenExchange()`; `canOpenWake() = canWake() && mayOpenExchange()`; `canRetryFlush()` (idle, interjections staged, `mayOpenExchange`, none of worker/quitting/watching/prebound, no confirm open, no queued commands). `canWake` unchanged.
- model.go: `exchangeRefusal() (string, bool)` maps the hold to `sessionLoadHoldNote`, `bgLaunchHoldNote`, `gitActionHoldNote`, `m.actuationBlockNote()`; field `flushPending bool` beside `wakePending`; the Update tail runs `flushAfterFold` between `adoptAfterFold` and `offerAfterFold`.
- interject.go: `flushInterjections` asks `exchangeRefusal`; refused → note, `flushPending = true`, rows stay. `flushAfterFold` clears the latch on an empty queue, flushes when `canRetryFlush && !m.blockedUpstream()`. workflow.go `wakeIfIdle` asks `canOpenWake`.
- actuation.go `foldActuationDone`: rewrite the comment allowing an Exchange or wake during an actuation.
- `capcom know add --id adr-0029-amend-1123 --parent adr-0029`: D5's "sends" covers every opener; human openers refuse with the note as before; the flush notes once and retries at any refusing hold's release; the wake waits silently. Name it on adr-0029's Status line and reading list.
**Regression guard.** After an actuation release, the flush retry and the wake hold until a beat has folded since the release (a `/stop-server` or `/unload-model` drained at completion releases the latch while `hb.offline` is still false). heartbeat.go's `blockedUpstream` doc paragraph is rewritten to say the retry is gated (doc.go stays with item 13). This supersedes, for the retry, ADR 0025 D7 ("a natural completion auto-sends what is left", adr-0025-decision.md) and adr-0025-consequences' "auto-flush … deliberately not gated"; adr-0029-amend-1123 names both.
**Files:** internal/tui/engineholds.go, internal/tui/model.go, internal/tui/interject.go, internal/tui/workflow.go, internal/tui/actuation.go, internal/tui/heartbeat.go, internal/tui/actuation_test.go, .capcom/knowledge/decisions/adr-0029.md, .capcom/knowledge/decisions/adr-0029/adr-0029-amend-1123.md
**Read first:** internal/tui/interject.go — flushAfterCompletion, drainThenFlush, flushInterjections; internal/tui/engineholds.go — engineHolds, canWake, holdsBlockingResume; internal/tui/model.go — Update (deferred tail), loadHoldNote; internal/tui/workflow.go — wakeAfterFold, wakeIfIdle; internal/tui/actuation.go — foldActuationDone; internal/tui/heartbeat.go — blockedUpstream, foldServerSwitch; internal/tui/actuation_test.go — startLoad, wireLauncher, TestActuationLatchRefusesEveryMoveWhileHeld; internal/tui/engineholds_test.go — TestEngineHolds_Questions
**Tests:** these fail against the pre-item tree: `TestCompletionFlushWaitsOutTheActuationLatch` (with a `/stop-server` row: no Exchange opens before a beat folds after the release), `TestCompletionFlushWaitsOutASessionLoad`, `TestWakeWaitsOutTheActuationLatch`; plus `TestHeldQueueAfterACancelIsNotRetriedAtTheLatchRelease`.
**Acceptance:**
- `go build ./... && go vet ./internal/tui/`
- `go test -race -count=1 -run 'TestCompletionFlushWaitsOut|TestWakeWaitsOutTheActuationLatch|TestHeldQueueAfterACancel|TestExchangeDoneFlushesQueue|TestCompactDoneFlushes|TestQuitDeferredBeatsFlush|TestQueuedClearWithAWorkflowRunningHoldsTheFlushUntilAnswered|TestBackgroundWorkflow_|TestBgLaunchHoldsAWakeUntilItLands|TestWatchIsolatesAPendingWakeUntilEsc|TestActuation|TestEngineHolds_|TestReleaseEngine_' ./internal/tui/`
- `capcom know show adr-0029-amend-1123 && capcom check`
**Commit:** `fix(tui): flush and wake wait out a refusing hold instead of opening an Exchange`

## 13. Every Exchange opener asks the one hold question — ✅ DONE (2026-11-24)

NOTES (2026-11-24): internal/tui/interject.go needed no edit — item 12 already rewrote flushInterjections' doc to name exchangeRefusal, so the plan's "line 699 names loadHoldNote" no longer held; it is listed in the plan's Files but absent from FILES
NOTES (2026-11-24): when a launcher verb and a session load coincide, /continue and /compact now answer with the session load's note (refusingExchangeOrder) instead of the latch's; the typed message already used that order
NOTES (2026-11-24): TestEngineHolds_Questions also gains a "beat owed after a launcher verb's release" row (opensWake no), and the staged-rows case for retriesFlush is the sibling table test TestEngineHolds_RetriesFlush
NOTES (2026-11-24): scripts/test-timings.seed still lists the renamed test under its old name (TestTheActuationLatchRefusesExactlyTheServerAndExchangeVerbs); it is a harvested shard-timing fallback regenerated by `make test-timings-seed`, so it is left as is

**What:** Depends on item 12.
**Goal:** the hold rule for opening an Exchange is stated once (`holdsRefusingExchange`) and asked at the head of all six openers' ladders (typed message, held-queue ⏎, /continue, /compact, completion flush, wake); `loadHoldNote` is gone; `actuationBlocked` answers only for server-moving verbs.
**Approach (assumed at the header base):** submit (model.go): the `loadHoldNote` and `m.actuation.inFlight` rungs become one `exchangeRefusal()` rung (order: prebound → holds → `blockedUpstream` → InExchange settle). `runCommand`: `parsed.opensExchange()` → `exchangeRefusal()`, else `m.actuation.inFlight && actuationBlocked(cmd)`, before the upstream gate (when two refusals coincide the hold's note wins). `actuationBlocked` narrows to `spec.touchesServer`. `runContinue`/`runCompact` drop their `loadHoldNote` rung. Delete `loadHoldNote`. `launchExchange`'s doc states the precondition. Rewrite every comment naming `loadHoldNote` or the latch's Exchange half (`grep -rn 'loadHoldNote\|actuationBlocked' internal/tui`), and doc.go's engineholds and heartbeat paragraphs.
**Regression guard.** The `retriesFlush` answer needs staged rows, so it is not a column on the at-rest row whose doc says every answer is yes. Give it its own case with staged rows in TestEngineHolds_Questions (or a sibling table test). interject.go's `flushInterjections` doc (line 699, names `loadHoldNote`) is rewritten. In `TestEveryExchangeOpenerWaitsOutEveryRefusingHold`, /continue and /compact under holdBgLaunch or holdGitAction never reach `runCommand` (`commandRunnable` queues them): those four cells assert "queued in deferredCommands, no worker started", and the wake cells assert no worker and no note.
**Files:** internal/tui/model.go, internal/tui/commandrun.go, internal/tui/actuation.go, internal/tui/command.go, internal/tui/sessions.go, internal/tui/interject.go, internal/tui/doc.go, internal/tui/engineholds_test.go, internal/tui/command_test.go
**Read first:** internal/tui/model.go — submit, loadHoldNote, sessionLoadHoldNote; internal/tui/commandrun.go — runCommand, runContinue, runCompact, launchExchange; internal/tui/actuation.go — actuationBlocked, actuationBlockNote; internal/tui/command.go — commandSpec (opensExchange, touchesServer), parsedInput.opensExchange; internal/tui/workflows.go — the actuationBlocked("bg") save gate; internal/tui/command_test.go — TestTheActuationLatchRefusesExactlyTheServerAndExchangeVerbs, TestSendDuringBgLaunch; internal/tui/engineholds_test.go — TestEngineHolds_Questions, atRest
**Tests:** `TestEngineHolds_Questions` gains `opensExchange`/`opensWake` columns, a git-action row and its own staged-rows case for `retriesFlush`; new `TestEveryExchangeOpenerWaitsOutEveryRefusingHold` (4 holds × 6 openers; the /continue and /compact cells under holdBgLaunch or holdGitAction assert queued, no worker); `TestTheActuationLatchRefusesExactlyTheServerAndExchangeVerbs` → `...TheServerVerbs`.
**Acceptance:**
- `go build ./... && go vet ./internal/tui/`
- `go test -race -count=1 -run 'TestEngineHolds_|TestEveryExchangeOpenerWaitsOutEveryRefusingHold|TestTheActuationLatchRefusesExactlyTheServerVerbs|TestActuationLatchRefusesEveryMoveWhileHeld|TestSendDuringBgLaunch|TestSendDuringSessionLoad|TestContinueDuringSessionLoad|TestGitActionPromptWaitsMidAction|TestCompletionFlushWaitsOut|TestWakeWaitsOut|TestQueuedDrainStopsAtCompactAndResumesAfterItsFold|TestContinueOnInterruptedResumesStepOnly' ./internal/tui/`
- `! grep -rn 'loadHoldNote' internal/tui/ && ! grep -n 'actuation.inFlight' internal/tui/model.go`
**Closes:** exchange-hold-subsets
**Commit:** `refactor(tui): every Exchange opener asks the one hold question`

## 14. One config source: `config.Files`, resolved once and carried on Options — ✅ DONE (2026-11-24)

NOTES (2026-11-24): consequential edit — internal/config/doc.go: made necessary by the new files.go (TestDocMapNamesEveryFile's package map).
NOTES (2026-11-24): consequential edit — internal/config/allow_test.go: made necessary by resolveOptions gaining its mayMigrate parameter (one test call site passes `true`, the startup pass it always was).
NOTES (2026-11-24): consequential edit — internal/config/execlayer_test.go: made necessary by resolveOptions gaining its mayMigrate parameter (one test call site passes `true`).
NOTES (2026-11-24): consequential edit — .capcom/knowledge/glossary/glossary-safety/project-root.md: made necessary by deleting `Options.ProjectRoot`/`ProjectRootPinned`, which the Project root Note named as the mechanism; it now names `config.Options.Files`.
NOTES (2026-11-24): `projectRootNotice` moved from layer.go to files.go with `resolveProjectRoot`, its only user; `noFlagChanged`/`noEnvironment` moved from settingsedit.go to settingsedit_test.go, since no production caller remains (the re-read now runs inside config as `Files.Reread`, with its own no-flag/no-env stubs).
NOTES (2026-11-24): probeconfig's line-25 comment now names `config.Files.Layered` (it named `config.LoadLayeredOffer`, which this item stopped calling there), so item 18's comment sweep finds nothing left in that file. `apogee probe config` now announces under `notices` a current directory that cannot be resolved (ResolveFiles' skip notice); the old `probeProjectRoot` dropped that case silently.
NOTES (2026-11-24): `Files.Reread` on the zero Files reads nothing and answers the zero values (pinned in TestZeroFilesReadsNothing); no production caller can reach it, since `Options.ConfigFiles()` never answers the zero source.

**What:**
**Goal:** `config.Files` exists (`ResolveFiles`, `FilesAt`); every resolution records the Files it read on `Options.Files` and reuses one already set; the non-migrating re-read is `Files.Reread`; `Options.ProjectRoot`, `ProjectRootPinned` and `LiveReread` are gone; `apogee probe config` takes its root from `config.ResolveFiles`.
**Approach (assumed at the header base):**
- New internal/config/files.go: `type Files struct{configDir, workspace, root string; globalOnly, resolved bool}`. `ResolveFiles(configDir, workspace, globalOnly, notify) Files` (root from `resolveProjectRoot`, moved here unchanged; none when globalOnly); `FilesAt(configDir, workspace, projectRoot) Files`. Accessors `IsZero`, `Workspace`, `GlobalPath` ("" for zero), `ProjectRoot`, `ProjectPath` ("" when globalOnly, no root, or equal to `GlobalPath` — the `projectConfigWatchPath` rule), `WorkspacesDir`. Reads: `Layered(readFile, notify)`; `Reread(serverFlagBound, readFile, notify) (Options, ProjectOffer, error)` (no flags, env or migration).
- config.go: `ApplyConfigOffer`'s body → `applyConfigOffer(..., mayMigrate bool)`; exported signatures unchanged. The resolve step uses `opts.Files` or resolves it, then layers from its paths. `Options.ConfigFiles() Files` returns `o.Files` or resolves with notices dropped.
- options.go drops the three fields; layer.go deletes `layerProjectRoot`/`resolveProjectRoot`.
- cmd/apogee: `externalEdit.projection` uses `e.opts.ConfigFiles().Reread(...)`; wire_live.go's hand pin becomes `editOpts.Files = config.FilesAt(...)` (item 15 deletes it); probeconfig deletes `probeProjectRoot` and reads through `ResolveFiles(configDir, "", false, collect)`.
**Regression guard.** State `Files.Layered(readFile, notify) (Options, ProjectOffer, error)` once in this item. Item 17 and probeconfig use the offer, item 18's layeredConfig drops it. probeconfig's `configReport` read (`config.LoadLayeredOffer`, probeconfig.go:121) becomes `files.Layered(readFile, collect)` and `startupRefusal` becomes `files.Reread(false, readFile, notify)`. `Files.WorkspacesDir` is `config.WorkspacesDir(filepath.Dir(f.GlobalPath()))` (configDir is "" by default), pinned in `TestFilesProjectPath` with configDir "". layer_test.go's `sansRoots` also clears `o.Files`, so `TestOverlayProjectLayerMatchesTheStartupLayer` stays green. Every ApplyConfig/ApplyConfigOffer return path sets `Options.Files`, the `StartupUndetermined` return included, so a later `ConfigFiles()` never re-walks.
**Files:** internal/config/files.go, internal/config/files_test.go, internal/config/config.go, internal/config/options.go, internal/config/layer.go, internal/config/layer_test.go, cmd/apogee/settingsedit.go, cmd/apogee/settingsedit_test.go, cmd/apogee/wire_live.go, cmd/apogee/probeconfig.go
**Read first:** internal/config/config.go — resolveOptions, ApplyConfigOffer, LoadFileConfig, resolveConfigDir; internal/config/layer.go — layerProjectRoot, resolveProjectRoot, OverlayProjectOffer, parseLayeredConfig; internal/config/options.go — ProjectRoot, ProjectRootPinned, LiveReread, GlobalConfigOnly; cmd/apogee/settingsedit.go — externalEdit.projection, newExternalEdit, noFlagChanged; cmd/apogee/probeconfig.go — configReport, startupRefusal, probeProjectRoot; cmd/apogee/wire_live.go — editOpts pin in wireSession; internal/config/layer_test.go — TestResolveOptionsReadsThePinnedProjectRoot, sansRoots, overlayOnStartup; cmd/apogee/settingsedit_test.go — TestExternalEditReloadKeepsThePinnedProjectRoot, TestRunRootPinsTheReReadToTheBootProjectRoot
**Tests:** new `TestResolveFilesWalksFromTheWorkspace`, `TestResolveFilesTakesNoRootWhenGlobalOnly`, `TestResolveFilesSkipsAMissingWorkspaceWithANotice`, `TestZeroFilesReadsNothing`, `TestFilesProjectPath` (includes `WorkspacesDir` with configDir ""), `TestFilesRereadRefusesARetiredShape`, `TestFilesRereadKeepsItsRoot`, `TestResolveOptionsRecordsTheFilesItRead` (every return path, the `StartupUndetermined` return included); retarget the pin fixtures; layer_test.go's `sansRoots` clears `o.Files`.
**Acceptance:**
- `go build ./... && go vet ./internal/config/ ./cmd/apogee/`
- `go test -race -count=1 -run 'TestResolveFiles|TestZeroFiles|TestFiles|TestResolveOptions|TestOverlayProject|TestLoadLayered' ./internal/config/`
- `go test -race -count=1 -run 'TestProbeConfig|TestExternalEdit|TestRunRootPins' ./cmd/apogee/`
- `! grep -rn 'ProjectRootPinned\|LiveReread' --include=*.go internal cmd && ! grep -n projectroot cmd/apogee/probeconfig.go`
- `! grep -nE '^\s*ProjectRoot ' internal/config/options.go`
**Commit:** `refactor(config): one config source resolved once and carried on Options`

## 15. Drivers take the Project root from the config source — ✅ DONE (2026-11-24)

NOTES (2026-11-24): `(w *rootWiring) files()` is not added in this item: with the `editOpts` pin gone, `newExternalEdit` takes `w.opts` (whose Files runRootWith now always sets), so nothing in item 15 calls it, and an uncalled method fails golangci-lint's `unused`. Item 16's watchers are its first callers and add it there.
NOTES (2026-11-24): consequential edit — cmd/apogee/wire_live_test.go: made necessary by resolveRoots no longer filling `roots.project`; `urlGuardWiringWith` mirrors runRootWith's root step, so it now takes `sessionRoots(opts)` and sets `opts.Files` (the startup-MCP repo-server tests write into `w.roots.project`).
NOTES (2026-11-24): consequential edit — cmd/apogee/readfence_test.go: made necessary by resolveRoots no longer filling `roots.project`; `readFenceWiring` mirrors runRootWith's root step and now takes `sessionRoots(opts)` and sets `opts.Files`.
NOTES (2026-11-24): consequential edit — cmd/apogee/settingsapplier_helper_test.go: made necessary by resolveRoots no longer filling `roots.project`; `fakeApplier` takes its roots from `sessionRoots` so `TestWatchedMalformedProjectConfigLeavesAServersApplyWorking` still finds the workspace's Project root.
NOTES (2026-11-24): `Files.GlobalDocument` on the zero Files reads nothing and answers the empty document, matching the zero-value rule of the other Files reads. The daemon's Firing source drops ResolveFiles' skip notice (ratified call); the comment in `fire` cites internal/daemon's load-time check that an entry's workspace exists.

**What:** Recast at the regression check (2026-11-23). Depends on item 14. Ratified: a missing workspace takes config's answer (no root, skip notice; the daemon drops the notice).
**Goal:** no production file outside internal/config and internal/projectroot imports internal/projectroot (guard test); `stateRoots.project` is filled from a `config.Files`; the TUI's re-read uses `w.opts.Files` with no hand pin.
**Approach (assumed at the header base):**
- wire.go: `resolveRoots` drops its `project:` walk; new `sessionRoots(opts config.Options) (stateRoots, config.Files, error)` (`resolveRoots` + `opts.ConfigFiles()`, `roots.project = files.ProjectRoot()`); `(w *rootWiring) files() config.Files` (`w.opts.Files`, else `config.FilesAt(w.roots...)` for test literals).
- `runRootWith` uses `sessionRoots` and sets `opts.Files`; wire_live.go deletes the `editOpts` pin. `runHeadless`, `resolveProjectAdoption` (project_cmd.go), `loadMCPConfig` (mcp_cmd.go) use `sessionRoots`.
- daemonfire.go: `fire` builds `config.ResolveFiles(w.opts.ConfigDir, entry.Run.Workspace, false, discard)` and sets `roots.project`; `firingOptions` calls `firing.Overlay(...)`; `newDaemonWiring` reads `opts.ConfigFiles().GlobalDocument(...)`. files.go adds `GlobalDocument` and `Overlay`.
- daemon startup, undo.go, probe.go, probemodel.go keep plain `resolveRoots`; probecontext.go uses `sessionRoots(opts)`.
- Guard `TestOnlyConfigImportsProjectroot` (internal/config/files_guard_test.go), shaped like internal/realpath/guard_test.go.
**Regression guard.** `apogee probe context` (probecontext.go) uses `sessionRoots(opts)`, since its opts are resolved. Plain `resolveRoots` stays only for undo, probe, probemodel and daemon startup. Delete `userHome` (wire.go) in this item, since it has no caller left. The Acceptance projectroot grep is restricted to production files (`--exclude=*_test.go`). `Files.Overlay` sets the overlaid Options' `Files` to the Firing's own Files on every return, the no-layer return included (`OverlayProjectOffer` returns `base` unchanged then), so a Firing's opts.Files names its root; a daemon Firing test pins the no-project-file case. `TestOnlyConfigImportsProjectroot` parses each file's imports (go/parser with `parser.ImportsOnly`) and judges only import paths, never comment text (internal/skills/load.go and internal/tui/tui.go name the package in comments). The Acceptance grep matches the import spelling, not the bare path, so the `stateRoots.project` comment in wire.go may keep naming the package.
**Files:** cmd/apogee/wire.go, cmd/apogee/wire_live.go, cmd/apogee/headless.go, cmd/apogee/project_cmd.go, cmd/apogee/mcp_cmd.go, cmd/apogee/daemonfire.go, cmd/apogee/probecontext.go, cmd/apogee/wire_boot_test.go, cmd/apogee/daemonfire_test.go, internal/config/files.go, internal/config/files_guard_test.go
**Read first:** cmd/apogee/wire.go — resolveRoots, runRootWith, stateRoots, userHome; cmd/apogee/daemonfire.go — fire, firingOptions, newDaemonWiring, latchProjectNotice; internal/config/layer.go — resolveProjectRoot, OverlayProjectOffer, ReadGlobalDocument; cmd/apogee/wire_live.go — editOpts pin in wireSession, projectConfigWatchPath, adoptionWatchPath; cmd/apogee/probecontext.go — probeContextCommand RunE; internal/realpath/guard_test.go — TestNoRawEvalSymlinksOutsideTheLeaf; cmd/apogee/settingsedit_test.go — TestRunRootPinsTheReReadToTheBootProjectRoot; cmd/apogee/daemonfire_test.go — newDaemonFireHarnessOn, writeDaemonProjectConfig, TestDaemonFiringTakesItsWorkspacesProjectLayer
**Tests:** new `TestSessionRootsTakeTheRootTheResolutionRecorded`, `TestOnlyConfigImportsProjectroot` (import paths only; a comment naming the package passes), `TestDaemonFiringFilesNameTheRootWithNoProjectLayer` (daemonfire_test.go; a workspace with no .apogee/config.yaml).
**Acceptance:**
- `go build ./... && go vet ./cmd/apogee/ ./internal/config/`
- `go test -race -count=1 -run 'TestOnlyConfigImportsProjectroot|TestResolveFiles' ./internal/config/`
- `go test -race -count=1 -run 'TestSessionRoots|TestResolveRoots|TestRunRootPins|TestDaemonFiring|TestDaemonStartupTakesNoProjectLayer|TestProjectCmd|TestHeadlessFiresOnlyAdoptedRepoReactions|TestRepoSymlinkedSkillsDir|TestRealSkillsDir' ./cmd/apogee/`
- `! grep -rn '"github.com/airiclenz/apogee/internal/projectroot"' --include=*.go --exclude=*_test.go cmd && ! grep -n 'editOpts' cmd/apogee/wire_live.go && ! grep -n 'func userHome' cmd/apogee/wire.go`
**Commit:** `refactor(cmd): drivers read the Project root from the config source`

## 16. Project-file locations come from the config source

**What:** Depends on item 15.
**Goal:** `projectConfigWatchPath` is gone; every project-file path, global path and workspaces dir used by the watchers, the adoption watch, the `/settings` save root and the project/mcp CLIs comes from `config.Files`.
**Approach (assumed at the header base):** wire_live.go: delete `projectConfigWatchPath`; `adoptionWatchPath(files config.Files)` ("" when `ProjectPath()` is ""; else `adoption.New(files.WorkspacesDir(), files.ProjectRoot())`); `wireSession` watches `w.files().GlobalPath()`, `.ProjectPath()`, `adoptionWatchPath(w.files())`. wire_options.go `projectSaveRoot(files)` returns `files.ProjectRoot()` when `ProjectPath() != ""`. project_cmd.go `resolveProjectAdoption` and mcp_cmd.go `refuseUnadoptedRepoServer` read `opts.Files`.
**Regression guard.** `ResolveFiles`' third argument is `globalOnly`, and true answers no root: the fixtures (`newWatchedLayeredConfig` and its kin) use `ResolveFiles(home, workspace, false, …)` or `config.FilesAt(home, workspace, workspace)`, never `globalOnly` true.
**Files:** cmd/apogee/wire_live.go, cmd/apogee/wire_options.go, cmd/apogee/project_cmd.go, cmd/apogee/mcp_cmd.go, cmd/apogee/configwatch_apply_test.go, cmd/apogee/wire_settings_test.go, cmd/apogee/projectrules_test.go, cmd/apogee/settingsrows_test.go
**Read first:** cmd/apogee/wire_live.go — projectConfigWatchPath, adoptionWatchPath, wireSession; cmd/apogee/wire_options.go — projectSaveRoot, settingsHost.inProject, errNoProjectConfig; cmd/apogee/project_cmd.go — resolveProjectAdoption; cmd/apogee/mcp_cmd.go — refuseUnadoptedRepoServer; cmd/apogee/configwatch_apply_test.go — newWatchedLayeredConfig, TestProjectConfigWatchPath; cmd/apogee/projectrules_test.go — TestProjectRuleAdoptionWatchPathFollowsTheProjectLayer; cmd/apogee/settingsrows_test.go — TestSettingsHostRefusesAProjectSaveItCannotMake
**Tests:** delete `TestProjectConfigWatchPath` (its table lives in `TestFilesProjectPath`); fixtures switch to `config.FilesAt(home, workspace, workspace)` / `ResolveFiles(home, workspace, false, ...)`.
**Acceptance:**
- `go build ./... && go vet ./cmd/apogee/`
- `go test -race -count=1 -run 'TestWatched|TestRunRootWiresTheConfigWatch|TestProjectRuleAdoptionWatch|TestSettingsHost|TestSettingsRowsOfferTheProjectTarget|TestProjectCmdAdopt' ./cmd/apogee/`
- `! grep -rn 'projectConfigWatchPath' --include=*.go cmd`
**Commit:** `refactor(cmd): project-file locations are the config source's`

## 17. The Adoption snapshot and project rules read through the source

**What:** Depends on item 15.
**Goal:** `project.Load` and `project.Answer` take a `config.Files`; internal/project names no `config.LoadLayeredOffer`.
**Approach (assumed at the header base):** files.go adds `ProjectAlone(readFile)` (`LoadLayeredOffer("", f.ProjectRoot(), ...)`). snapshot.go `Load(files, readFile, notify)` calls `files.Layered`; answer.go `Answer(files, readFile, store, entries, adopt)`; `heldPrints(files, readFile)` returns nil when `ProjectPath()` is "" or the same file as `GlobalPath()`, else reads `ProjectAlone`. cmd: projectrules.go passes `w.files()`; project_cmd.go `projectAdoptTarget` carries `files config.Files` (from `opts.Files`) in place of `globalPath`/`projectRoot`.
**Files:** internal/config/files.go, internal/project/snapshot.go, internal/project/answer.go, internal/project/doc.go, internal/project/answer_test.go, internal/project/snapshot_test.go, cmd/apogee/projectrules.go, cmd/apogee/project_cmd.go
**Read first:** internal/project/snapshot.go — Load; internal/project/answer.go — Answer, heldPrints, isSameFile; cmd/apogee/projectrules.go — answerProjectRules, settleAllowRules, loadSnapshot; cmd/apogee/project_cmd.go — projectAdoptTarget, resolveProjectAdoption, answerProposal; internal/project/answer_test.go — answerFixture; internal/project/snapshot_test.go — Load call sites; cmd/apogee/projectrules_test.go — projectRuleWiring
**Tests:** the `Load`/`Answer` call sites in internal/project tests pass `config.FilesAt(filepath.Dir(globalPath), root, root)`; existing tests are the proof.
**Acceptance:**
- `go build ./... && go vet ./internal/project/ ./cmd/apogee/`
- `go test -race -count=1 -run 'TestAnswer|TestRecord|TestSnapshot' ./internal/project/`
- `go test -race -count=1 -run 'TestProjectRule|TestProjectCmd' ./cmd/apogee/`
- `! grep -rn 'config.LoadLayeredOffer' --include=*.go internal/project`
**Commit:** `refactor(project): the adoption snapshot reads through the config source`

## 18. Every production config read asks the one source

**What:** Depends on items 16 and 17.
**Goal:** no non-test file outside internal/config references `config.LoadFileConfig`, `LoadLayeredConfig`, `LoadLayeredOffer`, `ReadGlobalDocument`, `OverlayProjectLayer` or `OverlayProjectOffer`, enforced by a guard test; `loadLayeredOptions` is gone. `ApplyConfig`/`ApplyConfigOffer` stay each Driver's boot call (ADR 0083 §5); `ResolveOptions` stays the test seam.
**Approach (assumed at the header base):** files.go adds `Global(readFile, notify)`. `settingsApplier` gets `files config.Files` (set from `w.files()`); `fileConfig` → `a.files.Global(...)`, `layeredConfig` → `a.files.Layered(...)`. Delete `loadLayeredOptions` (projectrules.go). Fix comments naming these entries (`grep -rn 'LoadFileConfig\|LoadLayered\|ReadGlobalDocument\|OverlayProject' cmd internal --include=*.go`). Guard `TestProductionReadsGoThroughTheConfigSource` (go/parser over non-test files under cmd/ and internal/, excluding internal/config).
**Regression guard.** `settingsApplier` gets a `files()` method that answers `a.files`, else `config.FilesAt(filepath.Dir(a.configPath), a.roots.workspace, a.roots.project)` (mirroring `rootWiring.files`), and `fileConfig`/`layeredConfig` read through it, so the ~45 tests that set `applier.configPath` keep reading that file. The comment rule covers probeconfig.go (its line-25 comment) and daemonfire.go (its line-67 comment).
**Files:** cmd/apogee/wire_settings.go, cmd/apogee/wire_options.go, cmd/apogee/projectrules.go, cmd/apogee/probeconfig.go, cmd/apogee/daemonfire.go, internal/config/files.go, internal/config/files_guard_test.go, cmd/apogee/settingsapplier_helper_test.go, cmd/apogee/mcp_cmd_test.go, cmd/apogee/wire_mcp_test.go, cmd/apogee/projectrules_test.go
**Read first:** cmd/apogee/wire_settings.go — settingsApplier, fileConfig, layeredConfig, reloadServers; cmd/apogee/projectrules.go — loadLayeredOptions; cmd/apogee/wire_options.go — settingsApplier construction (configPath: w.configPath()); cmd/apogee/settingsapplier_helper_test.go — fakeApplier; cmd/apogee/configwatch_apply_test.go — layeredApplier; cmd/apogee/probeconfig.go — configReport; internal/realpath/guard_test.go — TestNoProductionFileNamesPlatformRealPath
**Tests:** new `TestProductionReadsGoThroughTheConfigSource`; test calls of `loadLayeredOptions` become `w.files().Layered(...)`.
**Acceptance:**
- `go build ./... && go vet ./cmd/apogee/ ./internal/config/`
- `go test -race -count=1 -run 'TestProductionReadsGoThroughTheConfigSource|TestOnlyConfigImportsProjectroot' ./internal/config/`
- `go test -race -count=1 -run 'TestApplySetting|TestWatchedProjectConfig|TestProjectRuleAdoptsARepoServer|TestMCP' ./cmd/apogee/`
- `! grep -rn 'func loadLayeredOptions' cmd/apogee`
**Closes:** config-read-entries
**Commit:** `refactor(config): every config read asks the one source`

## 19. internal/config answers per key: may a project state it, which layer, who wins

**What:** Depends on item 18 (order only).
**Goal:** the project-settable class rule, an override's name (including the update-check exception), the source rank order and the file-layer merge each exist once, in internal/config; `savableToProject`, `settingSource` and `withBootUnreportedKeys` are gone.
**Approach (assumed at the header base):**
- registry.go: `func (k Key) ProjectSettable() bool` (`ClassProjectParam` or `ClassTightenOnly`); callers `projectWritableKey`, `applyProjectRows`, `projectWalk.key`.
- New internal/config/provenance.go: `type Provenance struct{Source Source; Name string; ProjectUnion bool}`; `(o *Options) ProvenanceOf(k Key) Provenance` on `SourceOf` (env name `k.EnvVar` or `EnvNoUpdateCheck`; flag `"--"+k.FlagName`; `ProjectUnion` = tighten-only and in `ProjectKeys`); `(s Source) Outranks(file Source) bool` from one rank table (default < global < project < env < flag); `(o Options) WithFileLayers(files Options, keep func(Key) bool) Options` (fresh maps).
- cmd/apogee settingsrows.go reads `ProvenanceOf` (Source, SourceName, `carriesProjectEntries` → `ProjectUnion`; `setInProject`/`ProjectCapable` → `ProjectSettable`); wire_options.go `layeredOpts` → `WithFileLayers(h.edits.fileLayers(), reloadNeverReports)`; `notHeldByProject` reads `ProjectUnion`; settingsedit.go `layers()` → `fileLayers()`.
**Regression guard.** `setInProject` = `Source == project && !p.ProjectUnion && (!Editable || !ProjectSettable)`, with `ProjectUnion` defined independently of Source (tighten-only && in `ProjectKeys`), so a project's `dangerous-rules` entries keep "⏎ opens $EDITOR". `layeredOpts` keeps its `h.edits == nil` early return of `h.opts` (a nil `*externalEdit`'s `fileLayers` would panic at `e.mu.Lock`).
**Files:** internal/config/registry.go, internal/config/provenance.go, internal/config/provenance_test.go, internal/config/projectwrite.go, internal/config/layer.go, cmd/apogee/settingsrows.go, cmd/apogee/wire_options.go, cmd/apogee/settingsedit.go, cmd/apogee/settingsrows_test.go
**Read first:** cmd/apogee/settingsrows.go — settingsRowsOffered, settingSource, setInProject, savableToProject, carriesProjectEntries; cmd/apogee/wire_options.go — settingsHost.layeredOpts, withBootUnreportedKeys, notHeldByProject; cmd/apogee/settingsedit.go — externalEdit.layers, reloadNeverReports; internal/config/config.go — Options.SourceOf, overrideSources, EnvNoUpdateCheck; internal/config/projectwrite.go — projectWritableKey; internal/config/layer.go — applyProjectRows, projectWalk.key; cmd/apogee/settingsrows_test.go — TestSettingsRowsPointReadOnlyKeysAtTheirEditor, TestSettingsHostRowsWithNoBaselineMarkFromTheBootLayers, layeredSettingsHost
**Tests:** new `TestProvenanceOfNamesEveryOverride`, `TestSourceOutranksFollowsThePrecedence`, `TestWithFileLayersKeepsTheNamedKeys`, `TestProjectSettableIsTheParamAndTightenOnlyClasses`, `TestSettingSourcesSpellConfigSources` (cmd/apogee settingsrows_test.go; pins `tui.SettingSource` = `config.Source` for all five); `TestSettingsRowsHoldProjectEntriesOutOfTheGlobalFile` gains a `dangerous-rules` project-entries row case.
**Acceptance:**
- `go build ./... && go vet ./internal/config/ ./cmd/apogee/`
- `go test -race -count=1 -run 'TestProvenance|TestSourceOutranks|TestWithFileLayers|TestProjectSettable|TestSourceOf|TestOverrideSources|TestProjectWrite' ./internal/config/`
- `go test -race -count=1 -run 'TestSettingsRows|TestSettingsHost|TestSettingSources' ./cmd/apogee/`
- `! grep -n 'ClassProjectParam\|ClassTightenOnly' internal/config/projectwrite.go cmd/apogee/settingsrows.go && ! grep -n 'func withBootUnreportedKeys\|func settingSource\|func savableToProject' cmd/apogee/*.go`
**Commit:** `refactor(config): a key's layer and winning source are config's answer`

## 20. The settings pane reads config's precedence answer

**What:** Depends on item 19.
**Goal:** internal/tui compares no `SettingSource` values to decide precedence; `settingsOverride` is gone; `settingsSourceOf` and `settingsOutranksNote` read only row and outcome fields; notes on screen unchanged.
**Approach (assumed at the header base):** tui.go: `SettingRow` gains `OutranksGlobal`, `OutranksProject bool`; `SettingOutcome` gains `Outranked bool`. settings.go: delete `settingsOverride`; `settingsSourceOf` keeps `row.Source` when the row outranks both files; `settingsOutranksNote` cases: outranks both → "<label> outranks at next launch"; `edit.outranked` → `settingsProjectOutranksNote`; untargeted, not a re-read, `row.OutranksGlobal` → "<label> outranks at next launch"; `settingEdit` gains `outranked`. settingsapply.go `settingsSaveToTarget` journals `outcome.Outranked`. cmd/apogee settingsrows.go fills the flags from `p.Source.Outranks(config.SourceGlobal|SourceProject)`; wire_options.go `landed` sets `Outranked` from the row's flag for that target.
**Regression guard.** `landed` takes the target, so it reads the right flag; a global `ResetIn` under a project value answers `Outranked: true`, a project reset `Outranked: false`, and `TestSettingsHostResetInAnswersTheLayeredValue`'s wants follow.
**Files:** internal/tui/tui.go, internal/tui/settings.go, internal/tui/settingsapply.go, internal/tui/settings_test.go, cmd/apogee/settingsrows.go, cmd/apogee/wire_options.go, cmd/apogee/settingsrows_test.go
**Read first:** internal/tui/settings.go — settingsOverride, settingsOutranksNote, settingsSourceOf, settingsSourceLabel, settingEdit; internal/tui/settingsapply.go — settingsSaveToTarget, settingsDefaultTarget; internal/tui/tui.go — SettingRow, SettingOutcome, SettingSource; cmd/apogee/wire_options.go — settingsHost.landed, SaveTo, ResetIn; cmd/apogee/settingsrows_test.go — TestSettingsHostResetInAnswersTheLayeredValue, TestSettingsHostSavesAParamKeyToTheProjectConfig; internal/tui/settings_test.go — TestSettingsPaneProjectRowSaysTheProjectConfigOutranksItAtTheNextStart, TestSettingsPaneGlobalSaveUnderAProjectValueNotesTheProjectOutranksIt, TestSettingsPaneOverriddenRowSaysTheOverrideOutranksItAtTheNextStart, settingsProjectBoolRow
**Tests:** env/flag/project rows in settings_test.go set the flags; `TestSettingsPaneGlobalSaveUnderAProjectValueNotesTheProjectOutranksIt`'s fake outcome sets `Outranked`; `TestSettingsRowsMarkOverriddenKeys` asserts both flags; `TestSettingsHostSavesAParamKeyToTheProjectConfig` asserts `Outranked`; `TestSettingsHostResetInAnswersTheLayeredValue`'s global-reset want gains `Outranked: true`, its project-reset want stays false.
**Acceptance:**
- `go build ./... && go vet ./internal/tui/ ./cmd/apogee/`
- `go test -race -count=1 -run 'TestSettingsPane|TestSettingsSourceMarker' ./internal/tui/`
- `go test -race -count=1 -run 'TestSettingsRows|TestSettingsHost' ./cmd/apogee/`
- `! grep -n 'func settingsOverride\|== SettingFromEnv\|== SettingFromFlag' internal/tui/settings.go`
**Closes:** setting-source-split
**Commit:** `refactor(tui): the settings pane reads config's precedence answer`

## 21. Reopen ADR 0083 part 5 narrowly with a dated amendment

**What:**
**Goal:** `capcom know show "ADR 0083" --with-children` prints `adr-0083-amend-1123`; adr-0083's Status line and reading-order list name it; part 5 carries a dated pointer to it.
**Approach (assumed at the header base):** `capcom know add --id adr-0083-amend-1123 --parent adr-0083 --title "ADR 0083 amendment of 2026-11-23: part 5 reopened narrowly — one Driver host, the verdicts stay per Driver" --body-file -` in the shape of `adr-0110-amend-1118`: why now (part 5's drift trigger fired: `d00f0f9c` after `6de72666`; `serve-http-driver` not yet counted); (a) taken — one cmd-internal `driverHost` (`cmd/apogee/driverhost.go`) for roots + scratch sweep, confiner, stats recorder, session store with share warning and session/snapshot sweeps (the Driver names what they keep), one `close()` returning the teardown sentence; (b) verdicts stay per Driver via a `confinementPosture` value (TUI degrades, headless refuses, daemon latches); (c) the daemon keeps its rootless resolver fenced per Firing (`ResolveWithin`); (d) not taken — the `firingInputs` composer, keys, skills, Bridge, globals; the host wraps no sink (ADR 0103 D1 / ADR 0075 D9 untouched); reopens further on a fourth Driver or a drift in a per-Driver step. Then append to adr-0083's Status line and reading list, and a `> **Amended 2026-11-23 (adr-0083-amend-1123).**` blockquote under part 5's 2026-10-29 Note.
**Regression guard.** The amendment's `refs:` name only files that exist at this item (e.g. cmd/apogee/headless.go, cmd/apogee/daemon.go, cmd/apogee/wire_live.go), never cmd/apogee/driverhost.go, which item 22 creates: an unresolved ref fails `capcom check`.
**Files:** .capcom/knowledge/decisions/adr-0083/adr-0083-amend-1123.md, .capcom/knowledge/decisions/adr-0083.md, .capcom/knowledge/decisions/adr-0083/adr-0083-part-5.md
**Read first:** .capcom/knowledge/decisions/adr-0083.md — Status line, reading-order sentence; .capcom/knowledge/decisions/adr-0083/adr-0083-part-5.md — Reopens on, Note 2026-10-29; .capcom/knowledge/decisions/adr-0110/adr-0110-amend-1118.md — shape, refs frontmatter
**Tests:** none (knowledge only).
**Acceptance:**
- `test "$(capcom know show 'ADR 0083' --with-children | grep -c 'adr-0083-amend-1123')" -ge 3`
- `capcom check`
**Commit:** `docs(adr): reopen ADR 0083 part 5 for one Driver host, verdicts per Driver`

## 22. One Driver host for the shared process-scope steps

**What:** Recast at the regression check (2026-11-23). Depends on items 15 and 21. No Driver migrated yet.
**Goal:** `cmd/apogee/driverhost.go` exists with `driverHost`, covered by `TestDriverHost*`; it owns no sink and calls no `wrap`.
**Approach (assumed at the header base):** unexported names. `driverHost{roots stateRoots; files config.Files; confiner domain.Confiner; stats *statsRecorder; sessions config.SessionSettings}`.
- `newDriverHost(roots, opts, newConfiner func() apogee.Confiner)` (nil → `platform.NewConfiner`; recorder `newStatsRecorder(serverStatsPath(roots.config), opts.ServerStats)`; no sweeps). `openDriverHost(opts config.Options, withProject bool, newConfiner) (*driverHost, error)`: roots and files via `sessionRoots(opts)` (item 15) when withProject, plain `resolveRoots(opts.ConfigDir, "")` (zero files) otherwise; then `gcScratchDirs`, then `newDriverHost`.
- `(h) openStore(say func(tui.Notice), keep func(*session.Store) ([]string, error)) (*session.Store, error)`: `sessionStoreOptions`, share warning via `say` (`NoticeShareGroupConfig`), `session.NewStore`, `keep`, then `gcSessions` and `gcSnapshotDirs` with the kept ids.
- `(h) posture(mode, confine) confinementPosture{backend string; caps apogee.ConfinementCaps; unconfined, degraded, residual string; prewarm bool}` from `unconfinedAutoWarning`, `probe.DegradedNotice`, `probe.ResidualNotice`, `shouldPrewarmLabelWalk`; judges nothing. `(h) prewarm(workspace, out)` calls the `prewarmLabelWalk` seam (moved here from headless.go).
- `(h) close() string`: nil-safe; `stats.close()`, confiner `Close`, returns `platform.ConfinementTeardownNotice(...)` or "".
- Update wire.go's seam list to name driverhost.go.
**Regression guard.** `openDriverHost(opts config.Options, withProject bool, newConfiner)`. TUI and headless pass true and get roots from `sessionRoots(opts)` (which resolves an empty workspace to cwd), the daemon passes false and gets plain `resolveRoots(opts.ConfigDir, "")`. Never select by `workspace != ""`. `driverHost` carries `files config.Files` from sessionRoots (zero on the daemon path). `confinementPosture.caps` is `apogee.ConfinementCaps` (the type probe.AutoUnattendedBlocked and ResidualNotice take). Add cmd/apogee/doc.go to Files with a one-line driverhost.go entry and TestDocMapNamesEveryFile to the Acceptance -run. Add a TestDriverHost case with opts.Workspace "" run from a repo subfolder (serial, t.Chdir) that finds the Project root. Writer decision 2026-11-23.
**Files:** cmd/apogee/driverhost.go, cmd/apogee/driverhost_test.go, cmd/apogee/headless.go, cmd/apogee/wire.go, cmd/apogee/doc.go
**Read first:** cmd/apogee/wire.go — runRootWith, resolveRoots, gcScratchDirs, gcSnapshotDirs, gcSessions; cmd/apogee/wire_live.go — wireSession (store block), sessionStoreOptions; cmd/apogee/wire_boot.go — announceConfinement, unconfinedAutoWarning, shouldPrewarmLabelWalk; cmd/apogee/serverstats.go — newStatsRecorder, statsRecorder.close; cmd/apogee/headless.go — prewarmLabelWalk; internal/config/layer.go — resolveProjectRoot; internal/projectroot/projectroot.go — Resolve, walkToProjectRoot (fixture needs .git and .apogee/ at the repo top); cmd/apogee/seams_guard_test.go — packageSeams
**Tests:** `TestDriverHostOpenFindsTheProjectRootFromASubfolder` (opts.Workspace "", cwd a repo subfolder; serial, t.Chdir), `TestDriverHostOpenSweepsStaleScratchDirs`, `TestDriverHostOpenStoreSweepsStaleSnapshotDirs`, `TestDriverHostOpenStoreKeepsWhatTheDriverNames`, `TestDriverHostOpenStoreSaysTheShareWarning`, `TestDriverHostStatsFollowsTheServerStatsKey`, `TestDriverHostCloseStopsStatsAndTearsTheConfinerDown`, `TestDriverHostPostureCells`, `TestDriverHostPrewarmGoesThroughTheSeam` (serial).
**Acceptance:**
- `go build ./... && go vet ./cmd/apogee/`
- `go test -race -count=1 -run 'TestDriverHost|TestShouldPrewarmLabelWalk|TestNoParallelTestSwapsAPackageSeam|TestHeadlessConfinedAutoPrewarmsTheLabelWalk|TestDocMapNamesEveryFile' ./cmd/apogee/`
- `! grep -n 'var prewarmLabelWalk' cmd/apogee/headless.go && ! grep -n 'wrap(' cmd/apogee/driverhost.go`
**Commit:** `refactor(cmd): one Driver host for the shared process-scope boot steps`

## 23. The daemon opens its facilities through the Driver host

**What:** Depends on item 22.
**Goal:** the daemon builds no startup roots, sweeps, store, confiner or recorder of its own (the per-Firing roots stay the daemon's); its residual and teardown sentences come from the host; its unconfined-warning and prewarm latches and rootless key resolver stay its own.
**Approach (assumed at the header base):** `newDaemonWiring`: `openDriverHost(opts, false, deps.confiner)`, `h.openStore(func(n tui.Notice){ log.line("%s", n.Text) }, nil)`, then the global document; on a later error join `h.close()`'s sentence. `daemonWiring` gains `boot *driverHost` and keeps `confiner`/`store`/`stats` copies. Delete `closeConfiner`; the `runDaemonWith` defer prints `wiring.boot.close()` to `errOut`. Boot residual from `boot.posture(domain.ModeAuto, opts.ConfineToWorkspace).residual`. `fire`: `p := w.boot.posture(f.Mode, ...)`; `p.unconfined != "" && w.latchUnconfinedWarning()`; `p.prewarm && w.latchPrewarm(ws)` → `w.boot.prewarm(ws, daemonLogWriter{log: w.log})`. Rewrite the "for the reason runRoot runs it" comments.
**Regression guard.** Keep `TestDaemonStartupSweepsStaleScratchDirs` and `TestDaemonStartupSweepsStaleSnapshotDirs` (they pass unchanged through the host and are the only proof `newDaemonWiring` reaches the sweeps); the item yields to the CHANGELOG pin of the daemon startup sweep ("the daemon suite stayed green with the call deleted"). Every comment in daemonfire.go/daemon.go naming an identifier the Acceptance grep forbids (e.g. daemonfire.go:79, :141, :178, :199, :415) is reworded to name the Driver host. The daemon prints `wiring.boot.close()`'s sentence to errOut only when it is non-empty (`if n := ...; n != ""`).
**Files:** cmd/apogee/daemonfire.go, cmd/apogee/daemon.go, cmd/apogee/daemonfire_test.go, cmd/apogee/daemon_test.go
**Read first:** cmd/apogee/daemonfire.go — newDaemonWiring, daemonWiring, closeConfiner, fire, latchUnconfinedWarning, latchPrewarm; cmd/apogee/daemon.go — runDaemonWith (teardown defer, ResidualNotice), daemonHost; cmd/apogee/daemonfire_test.go — newDaemonFireHarnessOn, TestDaemonStartupSweepsStaleScratchDirs, TestDaemonFirePrewarmsEachWorkspaceOnce; cmd/apogee/daemon_test.go — TestDaemonDisclosesTheFenceResidualAtStartup, closeConfiner cleanup
**Tests:** keep `TestDaemonStartupSweepsStaleScratchDirs` and `TestDaemonStartupSweepsStaleSnapshotDirs`; delete `TestDaemonFireServerStatsOffWritesNothing` (item 22 covers it); `wiring.closeConfiner()` cleanups become `wiring.boot.close()`.
**Acceptance:**
- `go build ./... && go vet ./cmd/apogee/`
- `go test -race -count=1 -run 'TestDaemonDisclosesTheFenceResidualAtStartup|TestDaemonFireWarnsOnceOnUnconfinedAuto|TestDaemonFirePrewarmsEachWorkspaceOnce|TestDaemonFireRecordsServerStats|TestDaemonFireGivesEachFiringItsOwnScratchDir|TestDaemonHostRefusesAutoOnAHostThatCannotFence|TestDaemonReloadSurvivesTheWatchEnding|TestDaemonRefusesWhenAnotherDaemonHoldsTheLock|TestDaemonStartupSweepsStale|TestDriverHost' ./cmd/apogee/`
- `! grep -nE 'gcScratchDirs|gcSnapshotDirs|gcSessions|newStatsRecorder|platform\.NewConfiner|sessionStoreOptions|ConfinementTeardownNotice|ResidualNotice|closeConfiner' cmd/apogee/daemonfire.go cmd/apogee/daemon.go`
**Commit:** `refactor(daemon): open the daemon's process-scope facilities through the Driver host`

## 24. Headless opens its facilities through the Driver host

**What:** Recast at the regression check (2026-11-23). Depends on item 22.
**Goal:** headless builds no roots, sweeps, store, confiner or recorder of its own; its Auto refusal stays its own; stderr order unchanged (skipped reactions, hygiene, Auto sentences, share warning).
**Approach (assumed at the header base):** `runHeadlessBody`: `h, err := openDriverHost(*opts, true, deps.confiner)` (error → `notStarted`), then a defer printing `h.close()` via `cmd.PrintErrln`; `roots := h.roots`. The Auto block: `p := h.posture(mode, opts.ConfineToWorkspace)`; `probe.AutoUnattendedBlocked("a headless run", p.backend, p.caps, ...)` stays; print `p.unconfined`/`p.residual` when set; `if p.prewarm { h.prewarm(roots.workspace, cmd.ErrOrStderr()) }`. `sessions, err := h.openStore(func(n tui.Notice){ cmd.PrintErrln(n.Text) }, nil)` (keep the `--no-save` nil record store); `firingInputs.confiner: h.confiner, stats: h.stats`. The run's recorder is now closed by `h.close()` (unasserted: `statsRecorder.close` only nils the store).
**Regression guard.** Headless calls `openDriverHost(*opts, true, deps.confiner)`. Every headless.go comment naming an identifier the Acceptance grep forbids is reworded to name the host. Keep the sweep assertions in TestHeadlessRunGetsItsOwnScratchDirAndSweepsStaleOnes and TestHeadlessRunSweepsStaleSnapshotDirs (no rename, no delete), since they prove headless reaches the sweeps through the host. Writer decision 2026-11-23. Headless prints `h.close()`'s sentence only when it is non-empty. New `TestHeadlessStartupNoticesKeepTheirOrder` pins stderr order by `strings.Index`: skipped-reactions notice < hygiene notice < Auto sentence < share warning; it joins the Acceptance -run (no listed test compares those positions today).
**Files:** cmd/apogee/headless.go, cmd/apogee/headless_test.go, cmd/apogee/serverstats_test.go
**Read first:** cmd/apogee/headless.go — runHeadlessBody, skippedReactionsNotice, firingSkillProvider call, firingInputs literal; cmd/apogee/headless_test.go — headlessRunOn, TestHeadlessConfinedAutoPrewarmsTheLabelWalk, TestHeadlessRunGetsItsOwnScratchDirAndSweepsStaleOnes, TestHeadlessRunSweepsStaleSnapshotDirs; cmd/apogee/serverstats_test.go — headlessStatsRun, TestServerStatsOffWritesNothing; internal/probe/confinement.go — AutoUnattendedBlocked
**Tests:** keep `TestHeadlessRunGetsItsOwnScratchDirAndSweepsStaleOnes` and `TestHeadlessRunSweepsStaleSnapshotDirs` as they are; delete `TestServerStatsOffWritesNothing`; new `TestHeadlessStartupNoticesKeepTheirOrder` (headless_test.go; one run printing all four notices).
**Acceptance:**
- `go build ./... && go vet ./cmd/apogee/`
- `go test -race -count=1 -run 'TestHeadlessRunGetsItsOwnScratchDir|TestHeadlessRunSweepsStaleSnapshotDirs|TestHeadlessNoSaveStillAppliesTheRetentionPolicy|TestHeadlessAutoDisclosesTheFenceResidual|TestHeadlessAutoDegradedCellIsARefusalNotANotice|TestHeadlessAutoRefusalSharesTheScheduleSentence|TestHeadlessConfinedAutoPrewarmsTheLabelWalk|TestHeadlessSecondInterruptExitsHard|TestServerStatsHeadlessFiringWritesOneLine|TestHeadlessSpecStatesTheShareTheConfigDividesBy|TestHeadlessStartupNoticesKeepTheirOrder' ./cmd/apogee/`
- `! grep -nE 'resolveRoots|sessionRoots|gcScratchDirs|gcSnapshotDirs|gcSessions|newStatsRecorder|platform\.NewConfiner|sessionStoreOptions|ConfinementTeardownNotice|ResidualNotice|unconfinedAutoWarning' cmd/apogee/headless.go`
**Commit:** `refactor(headless): open the headless run's process-scope facilities through the Driver host`

## 25. The TUI opens its facilities through the Driver host

**What:** Recast at the regression check (2026-11-23). Depends on item 22.
**Goal:** the TUI builds no roots, sweeps, store, confiner or recorder of its own; `startupNotices` and stderr order unchanged; no Driver body calls a sweep directly.
**Approach (assumed at the header base):** `runRootWith`: `ParseMode`, `h, err := openDriverHost(opts, true, deps.confiner)`, then `opts.Files = h.files`, `w := newRootWiringOn(opts, mode, h, deps)` (wire_boot.go; `w.boot = h`, roots/confiner/stats from `h`; key resolver stays `NewKeyResolver(roots.workspace)`). `resolveConfig` drops its `newStatsRecorder`; the `newLiveJournalSink(w.stats.wrap(...))` order is unchanged. `announceConfinement` reads `w.boot.posture(...)` and announces `unconfined`/`degraded`/`residual` with their existing kinds; prewarm via `w.boot.prewarm(..., os.Stderr)`. `wireSession` at its current beat (after the MCP dial): `w.boot.openStore(w.announce, keep)` where keep runs `resolveResume`, sets `w.resumed`/`w.watch`, returns the resumed id. `rootWiring.close`: keep the early `w.stats.close()` before `engine.Close`; the confiner block becomes printing `w.boot.close()`. Move `newRootWiring`/`newRootWiringWith` into wire_helpers_test.go over `newDriverHost`.
**Regression guard.** The TUI calls `openDriverHost(opts, true, deps.confiner)` and sets `opts.Files = h.files` before `newRootWiringOn`. Every comment in wire.go, wire_config.go and wire_boot.go naming `platform.NewConfiner` or `newRootWiring` (now test-only) is reworded to name `openDriverHost`. Add a runRootWith case with opts.Workspace "" and cwd in a repo subfolder (serial, t.Chdir) that keeps the Project root. Writer decision 2026-11-23. The TUI prints `w.boot.close()`'s sentence only when it is non-empty. Acceptance asserts runRootWith's body names none of `sessionRoots`, `resolveRoots`, `gcScratchDirs` (the file-wide greps cannot, as wire.go defines them).
**Files:** cmd/apogee/wire.go, cmd/apogee/wire_boot.go, cmd/apogee/wire_config.go, cmd/apogee/wire_live.go, cmd/apogee/wire_helpers_test.go, cmd/apogee/wire_boot_test.go
**Read first:** cmd/apogee/wire.go — runRootWith, rootWiring.close, rootDeps; cmd/apogee/wire_boot.go — newRootWiringWith, resolveConfig (stats/events), announceConfinement, announce; cmd/apogee/wire_live.go — wireSession (store, resolveResume, gc block); cmd/apogee/wire_boot_test.go — TestRunRootConfinementStartupNotices, captureStderr; cmd/apogee/wire_live_test.go — urlGuardWiringWith; cmd/apogee/wire_options.go — options (ProjectRoot field, the new test's observable); cmd/apogee/settingsedit_test.go — TestRunRootPinsTheReReadToTheBootProjectRoot, recordingLauncher
**Tests:** new `TestRunRootTakesTheProjectRootWithNoWorkspace` (wire_boot_test.go; opts.Workspace "", cwd a repo subfolder; serial, t.Chdir); otherwise the existing order tests are the proof.
**Acceptance:**
- `go build ./... && go vet ./cmd/apogee/`
- `go test -race -count=1 -run 'TestRunRootConfinementStartupNotices|TestWireSessionOpensTheBootSessionsSnapshotStore|TestWireSessionBootToolsReadTheAnnouncedScratchDir|TestWorkflowResumeReachesTheResumedSessionsSet|TestServerStatsSessionTurnWritesOneLine|TestServerStatsSettingsToggleOpensAndStopsTheStore|TestRunRootPins|TestRunRootTakesTheProjectRootWithNoWorkspace|TestDriverHost' ./cmd/apogee/`
- `! grep -nE 'platform\.NewConfiner|newStatsRecorder\(|:= sessionStoreOptions\(|ConfinementTeardownNotice|(Residual|Degraded)Notice\(' cmd/apogee/wire.go cmd/apogee/wire_boot.go cmd/apogee/wire_live.go`
- `test -z "$(grep -lE '^\s+(gcScratchDirs|gcSnapshotDirs|gcSessions)\(' cmd/apogee/*.go | grep -v _test.go | grep -v driverhost.go)"`
- `! sed -n '/^func runRootWith/,/^}/p' cmd/apogee/wire.go | grep -E 'sessionRoots|resolveRoots|gcScratchDirs'`
**Closes:** driver-preamble-copied
**Commit:** `refactor(tui): open the session's process-scope facilities through the Driver host`
