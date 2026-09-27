# Engine-run workflows — plan

**Goal:** The engine runs Workflows (fan-outs the model asks for through `fan_out`, and Recipes a human wrote into a skill), hands back one line per item plus a report path, can run them in the background and wake the agent, and a cancel keeps every finished piece of work.
**Date:** 2026-09-27
**Status:** unexecuted
**sized for:** ~200k-context host
**base:** 882e7bec
**Closes:** apogee-engine-run-delegation
**Sources:**
- `docs/adr/0087-the-engine-runs-workflows-the-model-or-a-recipe-asks-for.md`, `docs/adr/0088-cancel-settles-and-never-rewinds-finished-work.md`, `docs/adr/0089-a-workflow-may-run-in-the-background-and-wakes-the-agent-when-it-ends.md`
- `CONTEXT.md` — Workflow, Recipe, Receipt, Background workflow, Exchange, Stop (a delegation), Fan-out ceiling
- `docs/handoffs/2026-09-26 - 00 - engine-run-sub-agent-orchestration-planning.md`; `docs/design/test-drivers.md`
- `~/.claude/skills/code-audit/` (source of the shipped `audit` recipe's prompts)

**Ratified design calls** (owner, 2026-09-27 grill; ADRs 0087–0089 carry the rest):
- **Background launch:** the `/bg` prefix command — `/bg /audit internal/`.
- **Recipe inputs:** a recipe declares its inputs; the user's text binds them in order or as `key=value`; a missing required input is asked of the user. No model call.
- **Background question:** a background workflow's `ask` stage waits; the status line reads `1 workflow waiting for you`; the question opens when the user is idle or opens `/workflows`.
- **Shipped recipe name:** `audit` (a user's own `code-audit` skill is untouched).
- **Per-class gate:** `fan_out` and `workflow` are default-off tools lifted per model by `tools.enabled:` or a model profile's tool roster (the Console precedent); no shipped profile lifts them yet. `fan_out`'s `background` field is in its schema only while `workflow` is on the roster.
- **Result shape:** one line per item `#<n> <item> — <status> — <summary>[ k=v…]`, a totals line, `report: <path>`; past 40 items only non-ok lines are listed plus `items: <path>`.
- **Foreground recipe:** a recipe launched in the foreground opens an Exchange; the engine runs the workflow first and the model's first request carries the user's line plus the result lines.
- **Esc stop summary:** a cancel's per-child summary is capped at 20 s, a second esc×2 skips the rest, quit and daemon shutdown never wait (owner, 2026-09-27).

**Standing requirements:**
- skills: coding-standards
- Bubble Tea v2: keys are `tea.KeyPressMsg` matched on `msg.String()`; the `Model` is copied by value — no no-copy types (`internal/tui/doc.go`, ADR 0011).
- The engine stays wire-silent and Driver-free (ADR 0031); `ctx` is the only cancellation.
- Every workflow child is spawned through the recursion point (ADR 0014); no fabricated history.
- Any deviation from item text lands as a dated NOTES line under the item.

**Regression check (2026-09-27, 882e7bec):**
- 1: recast — leaf calls only; the pool drop and retained-set restore stay at base until item 2; guard folded (Turn-0 mid-tool cancel keeps the prompt per ADR 0088 D1; supersedes the turn.go:98-111/:211-236 and agent.go:1087-1111 doc comments)
- 2: recast — takes the retained-set restore from item 1; guard folded (+ `-run Cancel ./cmd/apogee/`)
- 3: guard folded (+ `-run Cancel ./cmd/apogee/`)
- 4: guard folded; supersedes CONTEXT.md:492's _Avoid_ "plan" for the Go type `workflow.Plan` only
- 5: guard folded
- 6: guard folded (window from item 15); yields to CONTEXT.md **Budget** / `domain.Budget.ContextLimit`
- 7: guard folded
- 8: guard folded
- 9: guard folded (doc.go)
- 10: guard folded
- 11: guard folded
- 12: guard folded (child contract binds items 15, 25, 27)
- 13: guard folded
- 14: guard folded
- 15: guard folded
- 17: guard folded (domain.WorkflowReceipt; tui fold)
- 18: guard folded
- 19: guard folded
- 20: recast — a leading `/<recipe>` launches engine-side in every Driver; guard folded; yields to ADR 0010 (the loop never imports internal/skills)
- 21: recast — load_skill names a recipe skill's launch; guard folded
- 22: recast — detects and renders only; guard folded
- 23: guard folded; supersedes ADR 0065 §1 "Four skills ship embedded" (dated amendment)
- 24: guard folded
- 25: guard folded
- 26: guard folded; yields to ADR 0025 (Considered options) and Run's depth-0 no-drain contract (agent.go:833)
- 27: guard folded; yields to ADR 0089 D1 (no background in headless/daemon)
- 28: guard folded (regression decision a–d)
- 29: guard folded
- 30: guard folded
- 31: guard folded
- 32: guard folded; yields to the chords-for-the-filter rule (internal/tui/sessions.go:297)
- 33: guard folded
- 34: guard folded; supersedes ClearContext's drop of session-owned live state for the `n` answer only (agent.go:1773, ADR 0086 D3)
- 35: guard folded; yields to ADR 0033 decision 2 (a Firing's Asker is nil)
- 36: guard folded
- 37: guard folded
- 38: guard folded
- 1: re-check — guard folded (width-1 settle keyed on slot kind; comment rule by grep; + loop.go); supersedes ADR 0007's rollback consequence (0007:90-95) and amends ADR 0011 C3 (0011:69), per ADR 0088's header
- 2: re-check — recast (cancel folds bounded to 20 s, second esc×2 skips them, quit/daemon shutdown skip them; never-started slot commits the not-run result); supersedes ADR 0075 D12 (0075:188-196) and the events.go SubAgentPhaseEvent.Cancelled contract
- 15: re-check — guard folded (a cancelled blocking fan_out uses item 2's fold guard)
- 20: re-check — guard folded (launch keyed on SkillIDs[0], never in a delegate; headless test in internal/run)
- 21: re-check — guard folded (recipe line decided per call from the calling Agent's menu)
- 22: re-check — guard folded (parseInput signature kept; session EntryKind + ./internal/session/)
- 28: re-check — recast (names `TakeWorkflowNotes`, the Engine call that takes the held finish note)
- 33: re-check — recast (save key is `ctrl+s`, not a bare `s`)

**Out of scope:**
- Running the bench experiment (it lives in `apogee-sim`); enabling `fan_out`/`workflow` in any shipped model profile.
- Token-level constrained receipts (llama.cpp grammar, Anthropic forced tool choice); cross-session workflow reuse.
- `/implement-plan`-style per-item implement→verify→commit loops as recipes; VERSION and release acts.

## 1. A cancelled Turn keeps its finished tool results — ✅ DONE (2026-09-27)

NOTES (2026-09-27): the settle is a sixth turnEnd row, `endSettled` (keep the Turn, advance, Exchange open, StatusCancelled, no observer fire), which step selects when dispatchTools returns the new `dispatchSettled` outcome. `endCancelled` keeps its DropRange for the stream, fold and delegation cancels. The Approach wanted the settle inside the endCancelled row; the Regression guard says step's call tells a leaf from a pool, and a separate row does that.

NOTES (2026-09-27): the synthetic results are written by the dispatch rather than by end(), because only the dispatch knows whether a call ran. executeTool/executeGate/executeConfineFallback now return the cancelled result text themselves. A ctx check at the top of executeTool makes a call reached after the cancel "not run". Calls never reached, including every delegation of a reply whose leaf took the cancel, get a ToolCallEvent and then their not-run result through appendToolResult (commitNotRun), so a Driver pairs them with a row instead of rendering an orphan result.

NOTES (2026-09-27): TestContextFillNoticeReArmsAfterACancelledTurnRollsBack drove a leaf cancel, so it is rewritten as TestContextFillNoticeStandsWhenACancelSettlesTheTurn (a kept Turn re-arms nothing). The rollback re-arm is still reached by the stream and delegation cancels and stays pinned by the direct rearmNotices tests in stepnotice_test.go. interject_test.go, stepnotice_test.go and harness_test.go needed no change: their cancels are streaming cancels or direct calls. construct.go and stepnotice.go comments still describe only the rollback row, so they are unchanged. ADR 0007/0011 already carry their dated ADR 0088 amendments.

**What:** Recast at the regression check (2026-09-27).
**Goal:** after `esc×2` mid-Turn, the Turn's assistant reply stays in the conversation and every tool call it issued has a result: a finished call its real result, a running call `cancelled by the user while it ran`, an unstarted call `not run: cancelled by the user`; a Turn cancelled before its reply finished streaming is dropped; a cancelled delegation group and the retained-delegation set behave exactly as at base (item 2 changes both); `SettleExchange` then places its cancelled marker on the last result.
**Approach (assumed at the header base):** In `internal/agent/turn.go` `turnLifecycle.end`'s `endCancelled` row, replace the `DropRange(t.rollback, …)` truncation with a settle that commits the missing results when an assistant tool-call message was committed; keep the drop when none was. `dispatchGroup` (dispatch.go) width-1 leaves: the call that sees the cancel commits its cancelled result instead of returning before commit. `Agent.turnRolledBack` (construct.go) keeps calling `retained.rollBackTurn()` (item 2 stops it); notices re-arm only on the drop branch. ADR 0088 D1.
**Regression guard.** item 1 changes only leaf tool calls of a cancelled Turn; the pool drop and the retained-set restore stay exactly as at base (a cancelled Turn that dispatched a delegation group still rolls back) — item 2 changes both together, and the cancel-test rewrites for pools/retention move to item 2. A Turn-0 cancel mid-tool keeps the prompt: its settled Turn is a kept Turn (ADR 0088 D1 — only an Exchange whose one Turn was dropped mid-stream is scrapped), so `TestSettleExchange_LoneUserMessageFallsBackToAbort` is rewritten to the streaming case and item 3 rewrites the manual's "A cancel that finds nothing finished … the prompt itself comes back" (commands.md:113) and TUI `markAborted`. Notices re-arm and the deferred queue is restored only on the drop branch (a cancel during streaming), as `settle()` already leaves the fill ladder (turn.go:392); a kept Turn advances the Turn index. A call cancelled before it executed (at its approval gate or pre-exec) gets `not run: cancelled by the user`, never the while-it-ran text; synthetic results commit through `appendToolResult` so a `ToolResultEvent` reaches the Drivers. Every internal/agent comment saying a cancelled Turn is rolled back or re-attempted (leaf path) is rewritten here — the set is `grep -n -i 'roll.*back\|re-attempt' internal/agent/*.go | grep -v _test | grep -i 'cancel\|re-attempt'` (turn.go, loop.go, dispatch.go, construct.go, agent.go), not a closed list; `step`'s `endCancelled` call in loop.go tells leaf from pool.
The width-1 settle is keyed on the slot kind, never the width: `dispatchGroup`'s width-1 loop also runs every delegation `fanOutWidthFor` sizes at 1 (a lone `sub_agent`, cap<2), so a `resolveDelegate` slot keeps the base early return (the Turn rolls back) until item 2 and `TestSubAgent_CancelledChildRollsTheParentTurnBack` stays green here. This supersedes ADR 0007's "cancellation rolls the whole Turn back" consequence (0007:90-95) and amends ADR 0011 C3 (0011:69), as ADR 0088's header records.
**Files:** internal/agent/turn.go, internal/agent/dispatch.go, internal/agent/construct.go, internal/agent/turn_test.go, internal/agent/statemachine_test.go, internal/agent/interject_test.go, internal/agent/fillnotice_test.go, internal/agent/stepnotice_test.go, internal/agent/harness_test.go, internal/agent/agent.go, internal/agent/stepnotice.go, internal/agent/loop.go
**Read first:** internal/agent/turn.go — turnLifecycle.end, settle; internal/agent/dispatch.go — dispatchGroup, appendToolResult; internal/agent/loop.go — step (dispatchTools → endCancelled);
  internal/agent/construct.go — Agent.turnRolledBack; internal/agent/stepnotice.go — rearmNotices; internal/agent/subagent_test.go — TestSubAgent_CancelledChildRollsTheParentTurnBack
**Tests:** rewrite `TestStep_CancelMidTool`, `TestTurnEnd_Table` (cancelled row) to assert the kept results and their exact texts; new test: a cancel during streaming drops the Turn; `TestSettleExchange_KeepsFinishedTurnsAndNotesTheCut` extended to a cancelled Turn with a finished leaf. Also: `TestSettleExchange_LoneUserMessageFallsBackToAbort` rewritten to the streaming case; a Turn-0 mid-tool cancel keeps the prompt; a call cancelled at its approval gate gets the not-run text; a kept Turn re-arms no notice and restores no deferred correction; a cancelled Turn that dispatched a delegation group still rolls back, a lone cancelled `sub_agent` included (`TestSubAgent_CancelledChildRollsTheParentTurnBack` unchanged).
**Acceptance:** `go build ./... && go test -race -count=1 ./internal/agent/`
**Commit:** `feat(agent): a cancelled Turn settles and keeps its finished tool results`

## 2. A cancelled delegation pool keeps its finished children — ✅ DONE (2026-09-27)

NOTES (2026-09-27): re-derived from the assumption that the pool's post-join early return is the only place a cancelled delegation group is dropped: the width-1 loop, dispatchTools and step (loop.go) each carried a dispatchCancelled rollback branch for a delegation. All three now settle. dispatchGroup ends on settledIfCancelled(ctx), so any group the cancel reached returns dispatchSettled, including a leaf-only group whose last leaf finished as the cancel landed (at base that Turn completed and the next one was dropped mid-stream).

NOTES (2026-09-27): the never-started delegation takes a new error-shaped constant, cancelledQueuedDelegationContent ("sub-agent not started: the user cancelled the turn before it started; …"), not item 1's leaf text "not run: cancelled by the user". The Goal and ADR 0088 D2 say "the not-started result", and the TUI tells a delegation that never ran from finished work by the `sub-agent not started:` head (transcript.go neverStarted). The Regression guard's looser "not-run result" wording was read as that. Item 1's commitNotRun still answers delegations after a leaf cancel with the leaf not-run text. That is left as item 1 shipped it.

NOTES (2026-09-27): a slot is settled unstarted at two sites. settleCancelledDelegation runs at the pool dequeue (beside stopQueuedDelegation) and before runCall in the width-1 loop. A ctx check in runSubAgent before the child is built returns dispatchCancelled and gives a continuation's entry back. runDelegation then answers that with the not-started result on a finished phase with Cancelled false. Under a cancel, commitCall appends without the post-tool-result Moment, as settleCancelledLeaf does, but still books a ran child's audit record.

NOTES (2026-09-27): the 20 s bound is defaultCancelFoldBound. An unexported Agent field, cancelFoldBound, overrides it: children inherit it through the delegation seed, and tests inject it. The fold runs on context.WithoutCancel(ctx), capped at min(StreamIdleTimeout, bound). finishAtStop now takes the bound. The quit/shutdown cause is the new domain.ErrShuttingDown. The TUI quit and Scheduler.Close cancel with it, and foldStoppedChild then skips the fold on a marker naming the shutdown.

NOTES (2026-09-27): the second esc×2 works as follows. worker.cancel is now a CancelCauseFunc with a `stopped` latch. A second stopWorker, or a quit after a stop, routes StopChild to every delegate run on the activity board. childRegistry.arm now takes the armed ctx, and a stop that lands on an already-cancelled run leaves a mark (stopMarks). The next arm, the fold's, is then cancelled at once, so a child still unwinding its Run also skips its fold.

NOTES (2026-09-27): retainedDelegates.markTurn/rollBackTurn/atTurn were deleted. Nothing calls them once no dispatch-time cancel rolls a Turn back, and turnRolledBack now only re-arms notices. The ^x fold cut short by a whole-Turn cancel is kept stopped on a new "turn cancelled" marker. At base it became the cancel.

NOTES (2026-09-27): TestE2EOutcomeCancelledDelegationCarriesTheFailureTone keeps its name because item 3's Read-first anchor names it. It now pins `stopped by you` live and on reopen, in the marker tone (not error red), with no ✓. The golden was re-recorded with -update. The rewritten and renamed tests are TestFanOut_CancelKeepsFinishedSiblingsAndStopsRunningOnes, TestFanOut_CancelKeepsASiblingRetainedBeforeIt, TestSubAgent_ACancelledChildIsStoppedAndTheParentTurnSettles, TestSubAgent_ACancelledDelegateIsStoppedAndRetained, TestSubAgent_ACancelledContinuationIsKeptAsItsNextRound and TestDeferredAction_CancelDuringDelegationKeepsOneDirective. The last now asserts the settled Turn's (1 left) directive, not a restored (2 left).

NOTES (2026-09-27): consequential edit — internal/agent/delegationphase_test.go: made necessary by the finished phase no longer being Cancelled (assertCancelledBracket replaced by assertStoppedBracket)

NOTES (2026-09-27): consequential edit — internal/agent/stop_test.go: made necessary by childRegistry.arm's new ctx parameter (plus one registry test for the stop mark)

NOTES (2026-09-27): consequential edit — internal/agent/statemachine_test.go: made necessary by the rename of TestDeferredAction_CancelDuringDelegationRestoresSingleDirective (comment reference)

NOTES (2026-09-27): consequential edit — internal/agent/loop.go: made necessary by dispatchTools no longer returning dispatchCancelled (step's rollback branch and the markTurn call removed, comments restated)

NOTES (2026-09-27): consequential edit — internal/agent/turn.go: made necessary by a delegation cancel settling instead of rolling back (endCancelled/endSettled/observer comments)

NOTES (2026-09-27): consequential edit — internal/tui/bridge_test.go: made necessary by the worker cancel becoming a CancelCauseFunc

NOTES (2026-09-27): consequential edit — internal/tui/e2e_test.go: made necessary by the worker cancel becoming a CancelCauseFunc

NOTES (2026-09-27): consequential edit — internal/tui/interject_test.go: made necessary by the worker cancel becoming a CancelCauseFunc

NOTES (2026-09-27): consequential edit — internal/tui/worker_test.go: made necessary by the worker cancel becoming a CancelCauseFunc

NOTES (2026-09-27): consequential edit — internal/schedule/schedule_test.go: made necessary by Scheduler.Close's new shutdown cause (new pinning test)

NOTES (2026-09-27): item 3 still owns the rest of the display. Until it lands, the armed-esc hint still says "drops %d finished …" (model.go escStopHintDropsFormat). activity.go:459 and transcript.go:1638 comments still speak of a rolled-back delegation. SubAgentPhaseEvent.Cancelled is now never set, but eventjson, headless.go and the TUI folds still read it. Pre-existing gap exposed: a child whose only Turn was settled (a leaf cancel on its first Turn) has exchangeTurns 0, so a stop or cancel gives it the zero-Turn marker instead of a fold.


**What:** Recast at the regression check (2026-09-27). Depends on item 1. Closes Stage B of `apogee-2un`.
**Goal:** a cancel during a `sub_agent` group commits every child's result: a finished child its report; a running child is stopped exactly as `^x` stops it (fold, `[stopped by the user — engine summary follows]` partial result, retained under its name, ledger `stopped`); a queued child the not-started result. No finished child's report is lost.
**Approach (assumed at the header base):** In `dispatchGroup`, the post-join `dispatchCancelled` early return becomes a commit of every slot in call order. In `runSubAgent`, treat a parent-ctx cancel like `errDelegationStopped`: `finishAtStop` on a fresh fold ctx, the `delegationResult` stopped branch, retention. `delegationResult`'s `StatusCancelled → dispatchCancelled` mapping is kept only for a child that never started.
**Regression guard.** Per the item 1 decision this item changes the pool drop and the retained-set restore together: `Agent.turnRolledBack` (construct.go) stops calling `retained.rollBackTurn()` for a cancelled Turn here, and the pool/retention cancel-test rewrites land here, `TestSubAgent_ACancelledContinuationLeavesTheEntryItTook` included (a stopped continuation now re-retains with 2 rounds, not 1). The cancel's fold runs on a fresh ctx armed as the run's stop handle (`children.arm`, as the `^x` path at subagent.go:1113), so `^x` or a second cancel skips it with `secondStopFoldCause`. `runPooledSlot` checks `ctx.Err()` at the dequeue beside `stopQueuedDelegation` and settles a slot dequeued after the cancel through `skipDelegation` with the not-run result. Acceptance adds `go test -race -count=1 -run Cancel ./cmd/apogee/` (the e2e cancel tests it changes).
Owner's ratified call (2026-09-27): a cancel's stop-summary (fold) of each running child is bounded to 20 seconds (a child that cannot fold in time is kept with `summary unavailable`); a second esc×2 while folds run routes StopChild to every armed run and skips the remaining folds at once; a cancel that is a quit or a daemon shutdown carries a distinct context cause and skips the folds entirely (unavailable marker, as secondStopFoldCause). A slot still ending dispatchCancelled (never started) gets the not-run result and a finished phase carrying it with Cancelled false; the internal/domain/events.go SubAgentPhaseEvent doc (the "no ToolResultEvent follows a cancelled finish" contract) is rewritten to the new rule. ADR 0075 D12 (0075:188-196) is superseded explicitly — ADR 0088's header amends ADR 0075 D12 and ADR 0011 C3 and supersedes ADR 0007's rollback consequence.
**Files:** internal/agent/dispatch.go, internal/agent/subagent.go, internal/agent/children.go, internal/agent/fanout_test.go, internal/agent/subagent_test.go, internal/agent/deferred_exchange_scope_test.go, internal/agent/construct.go, cmd/apogee/e2e_outcome_test.go, cmd/apogee/testdata/frames/t15-cancelled-delegation.txt, internal/agent/agent.go, internal/domain/events.go, internal/domain/errors.go, internal/tui/model.go, internal/tui/worker.go, internal/tui/model_test.go, internal/schedule/schedule.go
**Read first:** internal/agent/subagent.go — runSubAgent (stopped block), delegationResult; internal/agent/agent.go — finishAtStop, foldForParent; internal/agent/dispatch.go — dispatchGroup, runDelegation;
  internal/tui/model.go — stopWorker; internal/domain/events.go — SubAgentPhaseEvent.Cancelled
**Closes:** apogee-2un
**Tests:** rewrite `TestFanOut_CancelRollsTheWholeTurnBack` → keeps finished siblings; `TestFanOut_CancelForgetsASiblingRetainedBeforeIt`, `TestSubAgent_CancelledChildRollsTheParentTurnBack`, `TestSubAgent_CancelledDelegateIsNotRetained` inverted to the new rule; `TestAbortExchange_RestoresRetentionToTheExchangeStart` stays (abort still scraps). Also: `TestSubAgent_ACancelledContinuationLeavesTheEntryItTook` rewritten (2 rounds); a second stop during the cancel's fold returns promptly; a slot dequeued after the cancel gets the not-run result; `TestE2EOutcomeCancelledDelegationCarriesTheFailureTone` and its golden follow the stopped rule. Also: a child whose fold outruns the 20 s bound (injected bound) is kept with `summary unavailable`; a second esc×2 during the folds stops every armed run and returns at once (TUI); a quit-cause and a shutdown-cause cancel skip every fold (unavailable marker) and return at once; a never-started slot commits the not-run result with a finished phase carrying it, Cancelled false.
**Acceptance:** `go build ./... && go test -race -count=1 ./internal/agent/ && go test -race -count=1 -run Cancel ./cmd/apogee/ && go test -race -count=1 ./internal/tui/ && go test -race -count=1 ./internal/schedule/`
**Commit:** `feat(agent): a cancelled delegation pool keeps finished children and stops running ones`

## 3. The TUI and manual follow the settle rule — ✅ DONE (2026-09-27)

NOTES (2026-09-27): re-derived from the assumption that closeInterruptedCalls lives in transcript.go: it is in internal/tui/transcriptbridge.go. Its behaviour is unchanged, because records saved mid-delegation and records written before ADR 0088 still carry open calls. Only its doc comment changed.

NOTES (2026-09-27): escStopHintDropsFormat is reworded, not retired. It is now escStopHintKeepsFormat, "press esc again to cancel — keeps %d finished %s, stops the rest", and keeps its precedence over the queued-skip form. inFlightFanOut's finished count stays as the count this form states. layout.md's copy of the hint changed with it.

NOTES (2026-09-27): SubAgentPhaseEvent.Cancelled is never set since item 2, so addSubAgentPhase, progressSaveTrigger and headless narrate no longer read it. They fold a stopped child's finished phase like any other, so narration prints `finished`, not `cancelled`. The wire keeps `data.cancelled` (always false) so the sub_agent_phase line's shape is unchanged. events.go, eventjson/encode.go and headless.md say so.

NOTES (2026-09-27): TestSubAgentCancelledFinishedLeavesTheHeadInterrupted pinned the rollback display. It is replaced by TestSubAgentCancelKeepsTheFinishedMembersReport, which covers a two-child group with one member finished and the other stopped, live and after a round trip. The Turn-0 mid-tool test, TestCancelMidToolOnTheFirstTurnKeepsThePrompt (model_test.go), drives a real engine through the seam, because the fake engine's settle answer would make it vacuous. TestEscStopHintNamesWhatASecondEscDiscards is renamed TestEscStopHintNamesWhatASecondEscDoes and also asserts that no "drops" wording is shown.

NOTES (2026-09-27): consequential edit — internal/tui/transcriptbridge.go: made necessary by a cancel settling its Turn (closeInterruptedCalls doc comment)

NOTES (2026-09-27): consequential edit — internal/tui/activity.go: made necessary by removing the Cancelled branch from the TUI folds (comment named a rolled-back child)

NOTES (2026-09-27): consequential edit — internal/tui/asker.go: made necessary by a cancel settling its Turn (comments said the loop rolls the Turn back)

NOTES (2026-09-27): consequential edit — internal/tui/sessionsave.go: made necessary by a cancel settling its Turn (progressSave comment said a resume re-attempts the Turn "as a cancelled one does")

NOTES (2026-09-27): consequential edit — internal/eventjson/encode.go: made necessary by the events.go wire statement (subAgentPhaseData comment described a rolled-back bracket)

NOTES (2026-09-27): consequential edit — layout.md: made necessary by the reworded armed-esc hint

NOTES (2026-09-27): consequential edit — docs/manual/configuration.md: made necessary by the manual rule (":868 said an Esc drops the lot")

NOTES (2026-09-27): consequential edit — internal/tui/fold_test.go: made necessary by progressSaveTrigger no longer reading Cancelled (the cancelled-finished case now fires the save on a stopped result)

NOTES (2026-09-27): consequential edit — internal/tui/transcript_test.go: made necessary by addSubAgentPhase no longer reading Cancelled (two residue tests now finish on a stopped result, and a comment is restated)

NOTES (2026-09-27): consequential edit — cmd/apogee/headless_test.go: made necessary by narrate no longer reading Cancelled (TestNarrationSinkNamesTheSubAgent's stopped child reads `finished`)

**What:** Depends on item 2.
**Goal:** the TUI transcript after a cancel shows the kept tool results and stopped children (no rolled-back rows), and `docs/manual/commands.md` describes `esc×2` as "stops and keeps finished work; undo file changes with git or `/undo`".
**Approach (assumed at the header base):** `internal/tui` folds of `SubAgentPhaseEvent{Cancelled}` and the cancel path in `model.go` (`SettleExchange` callers) render kept rows; update every manual sentence saying a cancel rolls back or discards (grep `-i "drop\|roll.*back\|discard" docs/manual/`).
**Regression guard.** Acceptance adds `go test -race -count=1 -run Cancel ./cmd/apogee/`. `escStopHintDropsFormat` ("drops %d finished …", model.go:3910) is retired or reworded to what the cancel keeps, with its model_test.go pins and `inFlightFanOut`'s finished count (transcript.go). The manual rule is every sentence saying a cancel drops, rolls back or discards work (`grep -n -i "drop\|roll.*back\|discard" docs/manual/`), including commands.md:100-104 and :113 (the Turn-0 prompt now stays, item 1), headless.md:217 and sessions.md:12. The folds that change are transcript.go `addSubAgentPhase`/`closeInterruptedCalls`, fold.go `progressSaveTrigger` and cmd/apogee/headless.go `narrate`; `SubAgentPhaseEvent.Cancelled`'s doc (events.go:231) is rewritten and states whether `cancelled` is still emitted on the wire (eventjson).
**Files:** internal/tui/model.go, internal/tui/subagentblock.go, internal/tui/fanout_test.go, internal/tui/subagentblock_test.go, docs/manual/commands.md, internal/tui/transcript.go, internal/tui/fold.go, internal/tui/model_test.go, cmd/apogee/headless.go, internal/domain/events.go, docs/manual/headless.md, docs/manual/sessions.md
**Read first:** internal/tui/model.go — escStopHint, escStopHintDropsFormat; internal/tui/transcript.go — addSubAgentPhase, inFlightFanOut; internal/tui/fold.go — progressSaveTrigger;
  cmd/apogee/headless.go — narrationSink.narrate; internal/domain/events.go — SubAgentPhaseEvent; cmd/apogee/e2e_outcome_test.go — TestE2EOutcomeCancelledDelegationCarriesTheFailureTone
**Tests:** a TUI unit test: cancel during a two-child group, one finished → the finished member row keeps its report state. Also: the armed-esc hint no longer announces a drop; the Turn-0 mid-tool cancel leaves the prompt row un-aborted.
**Acceptance:** `go build ./... && go test -race -count=1 ./internal/tui/ && go test -race -count=1 -run Cancel ./cmd/apogee/ && grep -n -i "cancel" docs/manual/commands.md`
**Commit:** `feat(tui): a cancel shows the work it kept`

## 4. Workflow plan model and validation — ✅ DONE (2026-09-27)

NOTES (2026-09-27): internal/workflow/docmap_test.go is not in the item's Files list. It is the house way a package enlists in docmap.Check (the Regression guard names docmap.Check), and it follows internal/floor/docmap_test.go.

NOTES (2026-09-27): ReceiptSpec is a named `map[string]string` (field name to type spelling), not a struct. Written out, it is the `returns: {findings: int, verdict: confirmed|refuted|unclear}` shape that fan_out (item 14) and verify (item 9) use. It has no struct fields, so there are no yaml/json tags to add. An enum is written `a|b|c`, with an optional `enum ` prefix, and needs two or more distinct values.

NOTES (2026-09-27): these calls were made here because they are not settled elsewhere. A stage's brief is `task` (inline) or `prompt` (a path): exactly one on fanout and merge, at most one on verify (items 14/18). verify and merge take an optional `from` naming the fanout they work over; empty means the nearest earlier fanout. pick takes `from` + `field` (a list field that stage declares) or `file`, plus `cap`/`batch`. A fanout reads a pick's items through `over: {stage: <pick>}`. ask stores its answer in the fixed field `answer` and requires `default`. A repeat names its target with `repeat:`, requires `when:`, and bounds `max:` to 1..10 (MaxRepeatRounds). A key set on a kind that does not read it is a Problem. A stage may refer only to stages before it.

NOTES (2026-09-27): ReceiptSpec.Check requires every declared field on an `ok` receipt only. A partial or blocked receipt may leave fields out, which matches item 5's "a comparison on an absent field is false". ReceiptSpec.Field(name) also returns the core fields' types (status: enum ok|partial|blocked, summary: text) for item 5's type-checker. Validate does not parse `when:`; item 5 adds that.

NOTES (2026-09-27): ValidateModelPlan has no separate "fanout comes first" rule. Every stage that could come before the fanout is already refused: verify or merge fail Validate, and every other kind is refused by the fan_out shape. validate_test.go also carries an import-boundary guard (on internal/webhook's pattern) that refuses internal/agent, config, run, tools, tui and the root facade.

**What:**
**Goal:** a new package `internal/workflow` defines `Plan`, `Stage` (kinds `fanout`, `verify`, `merge`, `pick`, `script`, `ask`, `repeat`), `ReceiptSpec` (core `status ok|partial|blocked`, `summary` ≤ 20 words; typed extras `int`, `enum a|b|…`, `text` ≤ 200 runes, `list`), `ItemSource`, and `Validate(Plan) []Problem` whose problems name the stage and field and say how to fix it.
**Approach (assumed at the header base):** Pure data + validation; imports `internal/domain` only — never `agent`, `config`, `run`, `tools`. `fan_out` plans are limited by `ValidateModelPlan`: one `fanout`, at most one `verify` then one `merge`. Package `doc.go` states the ADR 0087 boundary.
**Regression guard.** plan.go also defines the `Receipt` value (Status, Summary, typed Fields) and `ReceiptSpec.Check(Receipt) []Problem`. Every Plan/Stage/ReceiptSpec/ItemSource field carries explicit `yaml:"…"` and `json:"…"` tags in the recipe's key spelling (`when`, `cap`, `batch`, `max`). The Go type keeps the name `workflow.Plan`: this item amends CONTEXT.md's _Avoid_ "plan" for the definition (CONTEXT.md:492) to record the in-memory type as the one exception. doc.go carries the package file map naming plan.go and validate.go (docmap.Check).
**Files:** internal/workflow/doc.go, internal/workflow/plan.go, internal/workflow/validate.go, internal/workflow/validate_test.go, CONTEXT.md
**Read first:** docs/adr/0087-the-engine-runs-workflows-the-model-or-a-recipe-asks-for.md — D1, D3, D6; CONTEXT.md — Recipe, Receipt;
  internal/skills/parse.go — parseFrontmatterFields; internal/domain/hooks.go — Budget
**Tests:** table tests for every problem kind and for `ValidateModelPlan` refusing a second fanout, a pick, a script. Also: a `Receipt` table test (well-formed, malformed) against `ReceiptSpec.Check`; one Plan round-trips through yaml.v3 and encoding/json.
**Acceptance:** `go build ./... && go test -race -count=1 ./internal/workflow/`
**Commit:** `feat(workflow): plan model and validation`

## 5. Receipt conditions — ✅ DONE (2026-09-27)

NOTES (2026-09-27): ParseCond(string) parses syntax only, and the type-check against a ReceiptSpec is a separate `Cond.Check(ReceiptSpec) error`. This keeps the Goal's one-argument signature and lets item 10 check a condition against a spec it builds itself (a fanout's tally fields). Every parse or check error is a `*CondError{Token, Offset, Message}` that quotes the offending token. Precedence is `not` over `and` over `or`. Values are an integer, a bare word or a "double-quoted" string (no escapes). An int field takes any of the six operators and needs an integer value. An enum or text field takes only == and != (an enum value must be one it declares). A list field cannot be compared. Eval picks the comparison from the receipt value's shape, so an enum declared `1|2|3` still matches `level == 2`. A term on an absent or mistyped value is false, and `not` of that term is true.

NOTES (2026-09-27): Validate parses every stage's `when:`. It type-checks only a verify stage's condition, against the ReceiptSpec of the fanout the verify works over (its `from`, or the nearest earlier fanout). A skip `when:` on any other kind, and a repeat's `when:`, read an earlier stage's receipt or a fanout's tally (`ok < 3`, `parts > 0` in item 4's tests), not the stage's own spec. Which receipt that is belongs to item 10's Regression guard (not yet done), so those conditions are syntax-checked here. Type-checking them against the stage's own Returns would have refused item 4's well-formed recipe.

**What:** Depends on item 4.
**Goal:** `workflow.ParseCond(string)` parses `field op value` terms (`== != > >= < <=`) joined by `and`/`or`/`not` with parentheses, type-checked against a `ReceiptSpec`; `Cond.Eval(Receipt) bool`; errors quote the offending token.
**Approach (assumed at the header base):** A small hand-written recursive-descent parser in `cond.go`; no expression library.
**Regression guard.** `Validate` (validate.go) runs `ParseCond` on every stage condition against that stage's `ReceiptSpec` and reports a failure as a Problem naming stage and field. `status` and `summary` are always known to the type-checker, and a comparison on a typed field the receipt lacks evaluates false (no panic, no error). doc.go names cond.go.
**Files:** internal/workflow/cond.go, internal/workflow/cond_test.go, internal/workflow/validate.go, internal/workflow/validate_test.go, internal/workflow/doc.go
**Read first:** docs/adr/0087-the-engine-runs-workflows-the-model-or-a-recipe-asks-for.md — D1, D3, D6; CONTEXT.md — Receipt; internal/workflow/plan.go — ReceiptSpec, Stage (item 4)
**Tests:** table tests: precedence, enum and int comparisons, unknown field, type mismatch, empty input. Also: Validate refuses a bad `when:`; `status`/`summary` always typed; a comparison on an absent field is false.
**Acceptance:** `go test -race -count=1 ./internal/workflow/`
**Commit:** `feat(workflow): receipt conditions`

## 6. Item sources — ✅ DONE (2026-09-27)

NOTES (2026-09-27): these calls were made here because they are not settled elsewhere. `Item` is `{Label, Units}`: Units are the source's entries in order (a literal, a line, a path, or a split part's files), and a batch concatenates N entries or N whole parts. Label is the single entry, or `first … last (n)` when there are several. `SplitBudget` is in BYTES. `NewSplitBudget(contextLimit)` returns (ContextLimit − 4096-token brief reserve) × 4 bytes/token, or 0 when the limit is at or below the reserve. The 4 bytes/token matches internal/context.DefaultCharsPerToken, but is a local constant because the package imports only internal/domain.

NOTES (2026-09-27): grep's excluded-directory set is copied into items.go as `skippedDirs`, because internal/workflow may not import internal/tools (ADR 0087 D10, item 4's import guard). The walk start is exempt, so `files: build/*.go` still walks build/. Symlinks are skipped (regular files only), so a walk never leaves the fs.FS tree. Unreadable entries are returned as errors, not skipped the way find_files skips them. A `files:` glob under a directory that does not exist yields nothing, which is the "yields no items" error.

NOTES (2026-09-27): only `split:` reads the budget, so a budget ≤ 0 is an error for split alone. A literal list or a glob still expands when the window is unknown. `stage:` (a pick's items) is refused by Expand with an error naming it: those items exist only after the pick has run, and item 10 owns that. `split:` on a file (not a directory) is an error that points to `files:`. `lines:` trims each line. Expand checks exactly-one-source and a negative batch itself, as well as Validate.

**What:** Depends on item 4.
**Goal:** `workflow.Expand(ItemSource, fs.FS, SplitBudget) ([]Item, error)` yields items from a literal list, `files: <glob>` (workspace-relative, `**` supported), `lines: <path>` (non-blank lines), and `split: <dir>` — contiguous parts whose summed size fits `SplitBudget` (a child's context window in tokens at 4 bytes/token, minus a fixed brief reserve); `batch: N` groups items N per child. Items are stable-ordered.
**Approach (assumed at the header base):** `fs.FS` injected for tests (`fstest.MapFS`); the window comes from the caller (the Delegation target's per-slot window via `/props` `n_ctx`, supplied by item 15).
**Regression guard.** The window is supplied by item 15 (not item 11). `**` is a hand-written segment matcher over `fs.WalkDir` matching zero or more segments (fs.Glob/path.Match have no `**`; no doublestar dependency), skipping the directories grep never enters (`grepExcludeDirs`, grep.go:120). Expand `path.Clean`s each workspace-relative path (drops `./` and a trailing `/`) and refuses an absolute or `..` path with an error naming the source. The type is `SplitBudget`, derived from the target's `ContextLimit`, never raw n_ctx — the item yields to CONTEXT.md's **Budget** as the single authority (CONTEXT.md:1803, internal/domain/hooks.go:297-308); a budget ≤ 0 (unknown window, provider/discovery.go:66-71) is an error naming the missing window. doc.go names items.go.
**Files:** internal/workflow/items.go, internal/workflow/items_test.go, internal/workflow/doc.go
**Read first:** internal/tools/grep.go — grepExcludeDirs, grepIncludeSlashHint; internal/tools/find_files.go — FindFiles.walk; internal/domain/hooks.go — Budget.Window, Budget.ContextLimit;
  internal/provider/discovery.go — ModelInfo.ContextWindow; internal/agent/compact.go — deriveGrowthBounds; internal/context/budget.go — DefaultCharsPerToken
**Tests:** each source kind; a split whose largest file alone exceeds the budget becomes its own part; empty expansion is an error naming the source. Also: `**/*.go` returns the top-level and the deep file; `internal/` and `./internal` split alike; an absolute or `..` path and a ≤ 0 budget are errors.
**Acceptance:** `go test -race -count=1 ./internal/workflow/`
**Commit:** `feat(workflow): item sources and context-sized splits`

## 7. Workflow folder store — ✅ DONE (2026-09-27)

NOTES (2026-09-27): the status.json type is named `RunStatus` (with `StageStatus` and `ItemStatus`), because `Status` is already the receipt-status type in plan.go. Workflows, stages and items share one `Phase` enum: pending, running, done, skipped, failed, stopped. Receipts sit inline on `ItemStatus`, and receipt.json is written too.

NOTES (2026-09-27): these calls are made here because nothing else settles them. `NewStore(scratchDir)` refuses an empty or relative dir and roots the store at `<scratchDir>/workflows`; that dir is made on the first Create. `ItemKey(brief, item, contextFiles, fsys)` hashes labelled, length-prefixed fields: the brief, the item's label and units, and each context file's cleaned workspace path and contents, in the order given. An unreadable context file is an error. `PlanHash(plan, inputs, items)` hashes the JSON of {plan, inputs, items}; encoding/json sorts map keys. `Find` returns the newest match by Created, with the id breaking ties, and skips folders whose status.json is missing or unreadable.

NOTES (2026-09-27): these store methods are beyond the goal's literal list: `Dir`, `ReadPlan`, `Read/WriteStatus`, `Read/WriteReceipt`, `WriteTranscript([]domain.Message)`, `Path` and `WriteFile`. `WriteTranscript` writes default-JSON JSONL, because domain.Message has no json tags. `Path` returns a validated path for a stage output and makes its parent dirs. An id must match the minted shape (timestamp, then [a-z0-9-]). A key must be 64 lower-case hex characters. An output name must pass filepath.IsLocal and contain no backslash.

**What:** Depends on item 4.
**Goal:** `workflow.Store` rooted at `<scratch>/workflows/` creates `<id>/` (id `YYYYMMDD-HHMMSS-<slug>`) holding `plan.json`, `status.json`, `items/<key>/{receipt.json,transcript.jsonl}` and the stage outputs; the item key is a SHA-256 of the rendered brief, the item and the contents of every context file; `Store.Find(planHash)` returns an existing workflow for resume; writes are atomic (temp + rename).
**Approach (assumed at the header base):** Plain files, JSON; `status.json` carries per-stage/per-item state and receipts so a reader never opens detail files. Directory modes 0700, files 0600.
**Regression guard.** The Store constructor refuses an empty or non-absolute root with an error and is built per workflow from the live scratch dir (it moves at session boundaries; `ensureScratchDir` answers "" on failure, cmd/apogee/wire.go:621). The slug is limited to `[a-z0-9-]`, length-bounded, fallback `workflow`; `<id>/` is created with `os.Mkdir` and on `ErrExist` retried as `-2`, `-3`…. This item defines `PlanHash(Plan, inputs)` over the plan's canonical JSON plus bound inputs and item list, stores it in status.json, and `Find` matches on it. doc.go names store.go.
**Files:** internal/workflow/store.go, internal/workflow/store_test.go, internal/workflow/doc.go
**Read first:** cmd/apogee/wire.go — ensureScratchDir; cmd/apogee/wire_session.go — followScratch; internal/domain/config.go — ReadMounts.Scratch; internal/session/store.go — atomicWrite;
  internal/session/store_test.go — TestSaveLeavesNoTempFile, unsafeIDs; docs/adr/0087-the-engine-runs-workflows-the-model-or-a-recipe-asks-for.md — D4, D5
**Tests:** round trip; same plan → same key; a changed context file → a new key; Find on a stopped workflow; atomic write leaves no partial file on error. Also: an empty root is refused; a `../` slug stays under the root (the session store's `unsafeIDs` model); two creates in one second yield two folders; Find by PlanHash.
**Acceptance:** `go test -race -count=1 ./internal/workflow/`
**Commit:** `feat(workflow): on-disk workflow folder with resumable item keys`

## 8. Runner: one fan-out stage — ✅ DONE (2026-09-27)

NOTES (2026-09-27): Runner has three fields the plan's list does not name: `Workspace fs.FS` and `Split SplitBudget`, because Expand and ItemKey need them, and a `Now` clock for status.json. Run passes nil inputs to PlanHash, since recipe inputs arrive with items 19/20. `Spawner.Spawn` returns `Outcome{Ending (completed|capped|faulted|stopped), Receipt *Receipt, Report, Transcript}`. A Spawn error that is not a cancel counts as a fault. `Observer` has two callbacks, `StagePhase(StageEvent)` and `ItemPhase(ItemEvent)`, and the Runner serialises the calls under its status lock.

NOTES (2026-09-27): "waves of Width" is built as a pool capped at Width. Items start in order, and each finished child's slot starts the next item, so no more than Width children ever run at once.

NOTES (2026-09-27): retry and continuation rules. A capped child is final if its receipt is ok or blocked. If its receipt is partial, or it has none, it is continued up to Continuations times within one attempt, and `ItemSpec.Prior` grows by one Round {receipt, report, output} each time. A retry starts a fresh child with an empty Prior and a fresh continuation budget. When every chance is used up, the item ends on the last capped partial receipt if there is one. Otherwise the Runner writes a `blocked` receipt of at most 20 words that says why, and stores it.

NOTES (2026-09-27): the item key is not hashed over the rendered brief. It is hashed over the stage's child-facing fields as JSON (name, task template, prompt, out, returns, tools), because the rendered {out} is a path inside the item's own key-named folder. ItemSpec.Brief is the task with {item} (units joined ", ") and {out} filled in. When the stage uses a `prompt:` file, Brief is left empty and the Spawner (item 12) reads and renders the file. A custom `out:` gets {item} filled with the item's label. The default output is `items/<key>/output.md` in the folder.

NOTES (2026-09-27): if a cancel lands before an item's child starts, the item stays `pending`; if the child was running, the item becomes `stopped`. Neither stores a receipt, so a resume restarts both fresh. For now Run refuses non-fanout stages and fanouts over a pick stage's items, until items 9/10 add them. A fanout's `when:` is not read yet; item 10 owns recipe-only stage behaviour.

**What:** Depends on items 5, 6, 7.
**Goal:** `workflow.Runner.Run(ctx, Plan) (Result, error)` runs a `fanout` stage over its items in waves of `Width`, skipping items with a stored `ok`/`partial` receipt, retrying a faulted child or one that ended without a receipt `Retries` times, continuing a capped child `Continuations` times, and returns per-item receipts and tallies; a cancelled ctx stops running items, keeps finished ones and returns `Stopped`.
**Approach (assumed at the header base):** One deep module: `Runner{Spawner, Store, Width, Retries, Continuations, Observer}`; `Spawner` is an interface in `internal/workflow` (`Spawn(ctx, ItemSpec) (Outcome, error)`, where `Outcome` carries the receipt or the ending: capped, faulted, stopped). The agent implements it (item 12); tests use a scripted fake. `Observer` receives stage/item phase callbacks.
**Regression guard.** `ItemSpec` carries `Prior` (earlier rounds' receipt/summary and output path), which the Runner fills on continuation and item 12 renders the ADR 0086 way (`continuationTask`). Rule: Outcome capped (with or without a partial receipt) → Continuations first, then Retries; faulted, or no receipt when uncapped → Retries only. doc.go names runner.go.
**Files:** internal/workflow/runner.go, internal/workflow/runner_test.go, internal/workflow/doc.go
**Read first:** internal/agent/dispatch.go — runDelegation, emitSubAgentPhase; internal/agent/subagent.go — runSubAgent, continuationTask, classifyDelegation, wrapUpDirective;
  internal/workflow/store.go (item 7) — Store, Find
**Tests:** waves never exceed Width; skip-on-resume; retry then `blocked`; continuation count; cancel mid-wave keeps finished receipts in the store. Also: the continuation test asserts `Prior` grows per round; a capped child past Continuations falls to Retries.
**Acceptance:** `go test -race -count=1 ./internal/workflow/`
**Commit:** `feat(workflow): the fan-out runner`

## 9. Runner: verify and merge stages — ✅ DONE (2026-09-27)

NOTES (2026-09-27): runner.go is edited as well, though the item lists only stages.go. Verify reuses the fan-out wave path, so runFanout is split into `runItems`, the shared wave, and `endStage`, which settles the stage. `prepareItems` now takes `itemDraft`s, which carry a key brief, an engine-lead func and a fixed output. `itemJob` carries the rendered brief. Run dispatches by kind through `runStage`, and `expandStages` now lets verify and merge through.

NOTES (2026-09-27): added result fields. `ItemResult.Verdict` holds the verdict (confirmed|refuted|unclear) and is folded into the source fanout's item. `Tally` gains Confirmed, Refuted and Unclear, and the source fanout's tally is recomputed after the fold. `Result.Report` holds the report.md path. `Result.ReportMissing` gives the reason when a merge left no report, and in that case the merge stage ends `PhaseFailed` while the workflow stays done. A merge whose receipt is ok but whose report.md is missing also counts as failed.

NOTES (2026-09-27): verify rules. A verify child gets a copy of its stage with `Returns` set to the engine's `verdict: confirmed|refuted|unclear`. A blocked receipt or an unreadable verdict counts as unclear. A verify with no `when:` checks every finished item. The verify item key also covers the source item's key and its claim, so a source item that is redone gets checked again.

NOTES (2026-09-27): merge rules. The manifest is written to `stages/<merge>/manifest.md`. The merge child's Item is `{Label: stage name, Units: [manifest path]}` and its Output is `<folder>/report.md`. A merge's `when:` is not read yet; item 10 owns skip conditions.

NOTES (2026-09-27): changed the ItemSpec.Brief contract, which item 12 must follow. On verify and merge, Brief leads with the engine brief and the stage's rendered task follows after a blank line. When the stage uses a `prompt:` file, Brief holds only the engine lead, and the Spawner puts the rendered file after it.

NOTES (2026-09-27): runner_test.go's TestRunnerRefusesWhatItCannotRunYet had a "merge is refused" case that this item makes false. It now uses a script stage (a kind item 10 lifts) as the still-refused example.

**What:** Depends on item 8.
**Goal:** a `verify` stage runs one adversarial child per item selected by its condition, with a fixed engine brief (refute the item's claim; receipt `verdict: confirmed|refuted|unclear`) followed by the stage's own brief, and folds verdicts into the item's result; a `merge` stage runs one child over a manifest of every item's output path and writes `report.md` in the workflow folder.
**Approach (assumed at the header base):** Engine briefs are embedded text files under `internal/workflow/briefs/`. Verify reuses the fan-out wave path.
**Regression guard.** doc.go names stages.go (docmap.Check).
**Files:** internal/workflow/stages.go, internal/workflow/briefs/verify.txt, internal/workflow/briefs/merge.txt, internal/workflow/stages_test.go, internal/workflow/doc.go
**Read first:** internal/workflow/runner.go (item 8) — Runner, Spawner, ItemSpec, Outcome; internal/workflow/store.go (item 7) — Store;
  internal/floor/prompts.go — go:embed precedent for briefs/*.txt; internal/agent/subagent.go — wrapUpDirective
**Tests:** only selected items verified; verdict tally; merge sees every output path; merge failure leaves item results intact and says so in the result.
**Acceptance:** `go test -race -count=1 ./internal/workflow/`
**Commit:** `feat(workflow): verify and merge stages`

## 10. Runner: recipe-only stages

**What:** Depends on item 9.
**Goal:** `pick` turns a stage's receipt `list` field or the lines of a named output file into the next stage's items (with `cap:` and `batch:`); `script` runs a script through an injected `ScriptRunner` and reads `KEY=value` stdout lines as receipt fields; `ask` asks through an injected `Asker` (question, options, default) and stores the answer as a field; `repeat` re-runs a named stage while its condition holds, at most `max:` times; any stage's `when:` skips it.
**Approach (assumed at the header base):** `ScriptRunner` and `Asker` are interfaces in `internal/workflow`; with no Asker (unattended Driver) `ask` takes its default and the result notes `(default taken: no one to ask)`.
**Regression guard.** The item key folds in the repeat round, so a repeated fanout re-spawns its items instead of skipping them on resume. A condition names a single-receipt stage (script, ask, merge) or a fanout stage's tally fields (ok/partial/blocked counts); `pick` unions a list field across a fanout stage's items; Validate refuses any other subject.
**Files:** internal/workflow/stages.go, internal/workflow/recipe_stages_test.go, internal/workflow/store.go, internal/workflow/validate.go, internal/workflow/validate_test.go
**Read first:** internal/workflow/runner.go (item 8) — Runner.Run, skip-on-resume; internal/workflow/cond.go (item 5) — ParseCond, Cond.Eval;
  internal/workflow/store.go (item 7) — item key, status.json; internal/workflow/stages.go (item 9) — verify/merge stage path
**Tests:** a pick from a receipt list and from a file; script field parsing; ask with and without Asker; repeat bound; when-skip. Also: a repeated fanout re-spawns its items; a condition on a fanout stage's tally; Validate refuses a condition on a per-item field of a fanout stage.
**Acceptance:** `go test -race -count=1 ./internal/workflow/`
**Commit:** `feat(workflow): pick, script, ask and repeat stages`

## 11. Result lines and the report

**What:** Depends on items 9, 10.
**Goal:** `workflow.Format(Result)` renders the ratified result shape — one line per item `#<n> <item> — <status> — <summary>[ k=v…]`, a totals line `items N · ok A · partial B · blocked C`, verdict tallies when verified, `report: <path>` — and past 40 items lists only non-ok lines plus `items: <path>` to a full `items.md`.
**Approach (assumed at the header base):** Pure function; the full `items.md` is written by the Store.
**Regression guard.** `Store.WriteItems(id, Result)` (store.go) writes the full items.md and the Runner calls it at the end. Format renders the Result's notes (a failed merge, `(default taken: no one to ask)`) after the totals and prints `report:` only when report.md exists. doc.go names format.go.
**Files:** internal/workflow/format.go, internal/workflow/format_test.go, internal/workflow/store.go, internal/workflow/runner.go, internal/workflow/doc.go
**Read first:** internal/workflow/runner.go (item 8) — Result, Stopped; internal/workflow/stages.go (items 9, 10) — merge failure, ask default;
  internal/workflow/store.go (item 7) — Store; internal/agent/subagent.go — classifyDelegation
**Tests:** golden strings for 3 items, 41 items, a stopped workflow (`stopped by the user: K of N done`). Also: the 41-item golden asserts items.md exists; goldens for merge-failed and ask-default.
**Acceptance:** `go test -race -count=1 ./internal/workflow/`
**Commit:** `feat(workflow): result lines and item listing`

## 12. The finish tool and the workflow child spawner

**What:** Depends on item 8.
**Goal:** `internal/agent` implements `workflow.Spawner`: each item child is spawned through the recursion point (`runDelegation` path, run id minted, phase events emitted) with a `finish` tool whose schema is built from the stage's `ReceiptSpec`; a malformed `finish` is refused with a specific error and the child keeps running; a valid `finish` ends the child; a capped child's closing Turn offers only `finish`; the child's conversation is written to the item's `transcript.jsonl`.
**Approach (assumed at the header base):** `tools.FinishToolName` placeholder in `internal/tools/finish.go` (not in the default registry, not in `KnownToolNames` listing for the menu); the capped closing Turn reuses `wrapUpWriter`/`wrapUpCalls`/`wrapUpDirective` with a finish-only variant. Depth and privilege rules as for `sub_agent` (ADR 0005, max depth 1).
**Regression guard.** workflow item children write no delegate-ledger row, no retention entry and run no out-of-band namer (the item name is the name; the workflow folder is the record); the finish instructions replace the delegate report block; headless eventTap brackets key on a `fan_out` ToolCallEvent as on `sub_agent` — items 15, 25 and 27 follow this contract. `finish` declares `ReadOnly()` (the task_list precedent), so `Classify` (classify.go) resolves it to run, never gate or refuse, in Plan and ask-before. The finish-only closing Turn branches on a workflow-child flag ahead of `wrapUpWriter` in `toolMenu`, `wrapUpCalls` and `wrapUpDirective` with a new directive const; `wrapUpWriter` and the pinned `wrapUp*` consts stay untouched. internal/agent/doc.go names workflowspawn.go and internal/tools/doc.go names finish.go.
**Files:** internal/agent/workflowspawn.go, internal/agent/workflowspawn_test.go, internal/agent/subagent.go, internal/agent/loop.go, internal/tools/finish.go, internal/tools/finish_test.go, internal/agent/doc.go, internal/tools/doc.go
**Read first:** internal/agent/subagent.go — runSubAgent, newChildAgentOn, wrapUpWriter, wrapUpCalls, wrapUpDirective, startDelegationNaming;
  internal/agent/loop.go — toolMenu; internal/tools/classify.go — Classify
**Tests:** valid finish ends the child with its receipt; an out-of-enum verdict is refused then corrected; capped child → finish-only closing Turn; transcript file written. Also: a finish call resolves to run in Plan and ask-before; a workflow child books no ledger row, no retention entry and no namer call.
**Acceptance:** `go build ./... && go test -race -count=1 ./internal/agent/ && go test -race -count=1 ./internal/tools/`
**Commit:** `feat(agent): workflow children report through a checked finish tool`

## 13. Workflow configuration keys

**What:**
**Goal:** file-only keys `workflow-retries` (default 1), `workflow-continuations` (default 2) and `workflow-wake` (`on|off`, default `on`) exist, reach a `domain.WorkflowConfig` on the engine config, are listed in `docs/manual/configuration.md` and the starter template.
**Approach (assumed at the header base):** Rows in `internal/config/registry.go` beside the `delegate-*` rows (`fileConfig` pointer fields where 0 is meaningful); fold in `cmd/apogee/wire_config.go` next to `Delegation:`; facade alias in `apogee.go`.
**Regression guard.** The rows are not Editable (file-only keys), so wire_settings.go needs no entry; the Acceptance runs `-run 'Setting|Settings'` on ./cmd/apogee/ to prove the settings tests stay green. `workflow-wake` is a `KindEnum` with EnumValues on|off and a validator (the sub-agents-choice pattern, registry.go:286), never a `KindBool`. A zero `domain.WorkflowConfig` resolves in the engine to the documented defaults (retries 1, continuations 2, wake on), as the RestreamBudget pointer does (wire_config.go:172-177).
**Files:** internal/config/registry.go, internal/config/config.go, internal/config/options.go, internal/config/defaults/config.yaml, internal/domain/config.go, cmd/apogee/wire_config.go, apogee.go, docs/manual/configuration.md, internal/domain/config_test.go
**Read first:** internal/config/registry.go — delegate-fanout-rounds row, sub-agents-choice row; cmd/apogee/wire_settings_test.go — TestEveryEditableSettingKeyHasAnApply, TestApplySettingRefusesEveryKeyItCannotReach, settingKeysWithNoMemberToReach;
  cmd/apogee/wire_config.go — Delegation fold, RestreamBudget; internal/config/configwrite_scalar.go — renderSettingValue
**Tests:** `TestRegistryIsBijectionWithFileConfig`, `TestRegistryDefaultsFollowTheStarterTemplate`, `TestManualDocumentsEverySettingsKey` pass; a config test reads each key. Also: a zero `domain.WorkflowConfig` resolves to 1/2/on; `workflow-wake: off` parses and `true` is refused.
**Acceptance:** `go build ./... && go test -race -count=1 ./internal/config/ && go test -race -count=1 ./internal/domain/ && go test -race -count=1 -run 'Setting|Settings' ./cmd/apogee/`
**Commit:** `feat(config): workflow retry, continuation and wake keys`

## 14. The fan_out tool schema

**What:** Depends on item 4.
**Goal:** a default-off `fan_out` tool is in the registry with fields `task` (brief with `{item}`/`{out}`), `over`, `batch`, `context`, `returns`, `out`, `verify{when, task}`, `merge{task}`, `tools`, `recipe` + `inputs`, `run_on` (only under `sub-agents-choice: model`, as `sub_agent`'s) and `background` (only while `workflow` is on the roster); the default menu is byte-identical to the base.
**Approach (assumed at the header base):** `internal/tools/fan_out.go` placeholder like `sub_agent` (`Execute` errors; dispatch owns it), `DefaultOff() true`; registered after `NewSubAgentWith`. Update `KnownToolNames`, the registry ordered-list and count pins, the manual tool list.
**Regression guard.** `TestDefaultToolsHonourTheRoster`'s default-off pin (roster_test.go:216-218) is repinned to the Console family plus `fan_out`, and `fan_out` joins `writeCapableNonFileBuiltins` in internal/agent/writedetection_test.go. The `background` gate reads "workflow on the roster" from `host.rosterDeltas()` inside `builtinToolsWith` (no new HostTools field), testable today with `Enabled: ["workflow"]`. internal/tools/doc.go names fan_out.go.
**Files:** internal/tools/fan_out.go, internal/tools/fan_out_test.go, internal/tools/registry.go, internal/tools/registry_test.go, docs/manual/configuration.md, internal/tools/roster_test.go, internal/agent/writedetection_test.go, internal/tools/doc.go
**Read first:** internal/tools/registry.go — builtinToolsWith, rosterDeltas, HostToolsOf, KnownToolNames; internal/tools/roster_test.go — TestDefaultToolsHonourTheRoster;
  internal/tools/registry_test.go — TestHostToolsOfFillsEveryHostField; internal/agent/writedetection_test.go — writeCapableNonFileBuiltins; internal/tools/sub_agent.go — subAgentSchema
**Tests:** schema without `background`/`run_on` by default; with each gate; `internal/agent` `TestContextCostGolden` unchanged (`go test ./internal/agent -run TestContextCostGolden`). Also: the default-off roster pin; a schema built with `Enabled: ["workflow"]` carries `background`; `TestFloorWriteSupersetCoversEveryWorkspaceWritingBuiltin` passes.
**Acceptance:** `go build ./... && go test -race -count=1 ./internal/tools/ && go test -race -count=1 -run 'TestContextCostGolden|TestFloorWriteSuperset' ./internal/agent/`
**Commit:** `feat(tools): the default-off fan_out tool schema`

## 15. fan_out runs a blocking workflow

**What:** Depends on items 11, 12, 13, 14.
**Goal:** a model `fan_out` call is validated (`ValidateModelPlan`; problems returned as a tool error the model can fix), runs a workflow in `<scratch>/workflows/` at the dispatch width, and returns the formatted result lines; the same call again resumes the stored workflow; a cancel answers the call with `stopped by the user: K of N done` and the report-so-far path (ADR 0088 D3).
**Approach (assumed at the header base):** Recognise `fan_out` beside `isSubAgentCall` in `prepareCall`/`resolve`, route to a `runWorkflowCall` in a new `internal/agent/workflowcall.go`; width = `min(a.delegationWidth(), N)`; `fan_out` calls are not counted by the fan-out ceiling. The split budget reads the Delegation target's per-slot window.
**Regression guard.** The runner width is `min(a.delegationWidth(), N)`, never `fanOutWidthFor` (which answers 1 for fewer than 2 calls, dispatch.go:115). `fan_out` is withheld wherever `sub_agent` is (`defaultSubAgentTools`, `withoutSeatChoice`) and `resolve` refuses it at the depth bound, so no child starts a nested workflow past delegate-max-depth (ADR 0005, 0013, 0069 D3). On cancel `runWorkflowCall` answers its own call with `stopped by the user: K of N done` + the report path, never item 1's while-it-ran text. With no Delegation target latched the split budget falls back to the session's own window; with `ScratchDir() == ""` fan_out is refused with a tool error. Children follow item 12's contract (no ledger row, retention or namer; headless eventTap brackets key on a `fan_out` ToolCallEvent). internal/agent/doc.go names workflowcall.go.
A blocking fan_out stopped by a cancel uses item 2's guard: a quit or daemon-shutdown cause skips any fold (unavailable marker), an esc×2 fold is bounded to 20 s.
**Files:** internal/agent/workflowcall.go, internal/agent/workflowcall_test.go, internal/agent/dispatch.go, internal/agent/resolution.go, internal/agent/subagent.go, internal/agent/doc.go, internal/run/run.go
**Read first:** internal/agent/dispatch.go — prepareCall, fanOutWidthFor, delegationWidth, refusePastCeiling; internal/agent/resolution.go — resolve;
  internal/agent/subagent.go — defaultSubAgentTools, withoutSeatChoice; internal/agent/delegationtarget.go — DelegationTarget.ContextWindow
**Tests:** stub-upstream fan-out of 3 items → 3 lines; invalid plan → fixable error; resume skips finished items; cancel mid-run keeps finished items and answers the call. Also: a 3-item fan_out on a cap-3 server runs 3 at once; a child's menu does not offer fan_out; the cancel pins the stopped text after item 1's settle; an unrouted session uses its own window; an empty ScratchDir refuses the call; a quit-cause cancel skips every fold and a child's esc×2 fold stops at the 20 s bound.
**Acceptance:** `go build ./... && go test -race -count=1 ./internal/agent/`
**Commit:** `feat(agent): fan_out runs a blocking workflow`

## 16. The fan-out ceiling points to fan_out

**What:** Depends on item 15.
**Goal:** while `fan_out` is on the roster, the ceiling refusal for `sub_agent` ends with `— for more items, use fan_out`; without it the refusal text is unchanged.
**Approach (assumed at the header base):** `fanOutCeilingResultFormat` in dispatch.go gains the suffix conditionally.
**Files:** internal/agent/dispatch.go, internal/agent/fanout_test.go
**Read first:** internal/agent/dispatch.go — refusePastCeiling, fanOutCeilingResult, fanOutCeilingResultFormat; internal/agent/fanout_test.go — the two exact-refusal pins (want consts);
  cmd/apogee/e2e_fanout_test.go — fanOutCeilingRefusal; internal/tui/transcript.go — unstartedDelegationPrefix
**Tests:** both refusal texts pinned exactly.
**Acceptance:** `go test -race -count=1 ./internal/agent/`
**Commit:** `feat(agent): the fan-out ceiling names fan_out when it is enabled`

## 17. Workflow events

**What:** Depends on item 15.
**Goal:** the engine emits `WorkflowPhaseEvent` (started, stage started, item finished with receipt, finished/stopped, waiting-for-answer) carrying the workflow id; headless NDJSON encodes it as kind `workflow_phase`; the facade exports it; the manual's event kinds table lists it.
**Approach (assumed at the header base):** New type in `internal/domain/events.go` on `EventBase`; runner `Observer` adapter in `internal/agent`; `internal/eventjson/encode.go` kind const, `Kinds()`, case, data struct; `apogee.go` event alias list.
**Regression guard.** add internal/tui/fold.go and internal/tui/fold_test.go to Files — WorkflowPhaseEvent is folded (TestFoldEventCoversEveryEventVariant); the event carries a domain-side receipt shape (domain.WorkflowReceipt{Status, Summary, Fields map[string]string}) because internal/workflow imports internal/domain and not the reverse — workflow converts to it. The cmd/apogee run is `-run 'EventLine'` so `TestManualListsEveryEventLineKind` runs; `TestKindsAreTwentyOne` (encode_test.go:517) is repinned to 22 and renamed, and both "twenty-one" mentions in headless.md (:185, :203) change.
**Files:** internal/domain/events.go, internal/agent/workflowcall.go, internal/eventjson/encode.go, internal/eventjson/encode_test.go, apogee.go, docs/manual/headless.md, internal/tui/fold.go, internal/tui/fold_test.go
**Read first:** internal/domain/events.go — SubAgentPhaseEvent, EventBase; internal/eventjson/encode.go — Kinds, Encode; internal/eventjson/encode_test.go — TestKindsAreTwentyOne;
  cmd/apogee/docs_eventlines_test.go — TestManualListsEveryEventLineKind; internal/tui/fold_test.go — TestFoldEventCoversEveryEventVariant; apogee_alias_internal_test.go — TestEveryDomainEventVariantIsAliased
**Tests:** encode round trip; `cmd/apogee` `docs_eventlines_test.go` passes. Also: `TestFoldEventCoversEveryEventVariant` has a WorkflowPhaseEvent row; the kinds pin reads 22.
**Acceptance:** `go build ./... && go test -race -count=1 ./internal/eventjson/ && go test -race -count=1 -run TestFoldEventCoversEveryEventVariant ./internal/tui/ && go test -race -count=1 -run 'EventLine' ./cmd/apogee/`
**Commit:** `feat(engine): workflow phase events`

## 18. Recipes parse from a skill header

**What:** Depends on item 4.
**Goal:** a `SKILL.md` frontmatter may carry `inputs:` (name, required, default, description) and `recipe:` (a stage list mapping onto `workflow.Plan`, prompt paths relative to the skill dir); a skill with an invalid recipe fails to load with the validator's problems; `skills.Skill` exposes `Recipe` and `Inputs`.
**Approach (assumed at the header base):** Extend `internal/skills/parse.go` (yaml.v3 strict path); `internal/skills` may import `internal/workflow`. `{{SKILL_DIR}}` expands in prompt paths; `shipped:<id>` paths stay virtual.
**Regression guard.** a frontmatter carrying `recipe:` or `inputs:` whose YAML fails the strict parse is a load error naming the problem, never the lenient fallback. `{{SKILL_DIR}}` expands in `loadSkillFile` after `sk.Dir = src.dirFor(...)` (load.go), tested through Load over a temp dir plus the shipped `shipped:<id>` case. A recipe skill still needs a summary and a non-empty body (validate is unchanged). `Skill.Inputs` is `[]workflow.InputDecl`: this item creates internal/workflow/inputs.go holding that type (item 19 adds BindInputs beside it) and names it in internal/workflow/doc.go. A stage takes its brief as a prompt path or an inline `task:` (exactly one), so item 33 can emit a fan_out plan's inline text.
**Files:** internal/skills/parse.go, internal/skills/skill.go, internal/skills/parse_test.go, internal/skills/load.go, internal/skills/load_test.go, internal/workflow/inputs.go, internal/workflow/doc.go
**Read first:** internal/skills/parse.go — parseFrontmatterFields, scanFrontmatterFields, frontmatter, validate; internal/skills/load.go — loadSkillFile, loadShipped;
  internal/skills/skill.go — Skill
**Tests:** a valid recipe; each validation problem surfaces as a load error; a skill without `recipe:` is unchanged. Also: a mistyped `recipe:` (a scalar) fails the load; `{{SKILL_DIR}}` expansion through Load, shipped case included; the valid-recipe fixture carries a summary and body; a stage with an inline `task:`.
**Acceptance:** `go build ./... && go test -race -count=1 ./internal/skills/`
**Commit:** `feat(skills): recipes and inputs in the skill header`

## 19. Recipe inputs bind from the user's text

**What:** Depends on item 18.
**Goal:** `workflow.BindInputs(decls, text) (map[string]string, missing []string, error)` binds `key=value` tokens by name and the remaining tokens in declared order (quoted tokens allowed); unknown keys and surplus tokens are errors naming them.
**Approach (assumed at the header base):** Pure function in `internal/workflow/inputs.go`.
**Regression guard.** `workflow.InputDecl{Name, Required, Default, Description}` lives in inputs.go (created by item 18 so `Skill.Inputs` names it without an import cycle); `BindInputs` takes `[]InputDecl`, fills `Default` for any unbound input, and `missing` lists only required inputs with neither a value nor a default.
**Files:** internal/workflow/inputs.go, internal/workflow/inputs_test.go
**Read first:** internal/skills/skill.go — Skill; internal/skills/parse.go — frontmatter, parseFrontmatterFields, unquoteValue; internal/workflow/plan.go (item 4) — Plan;
  internal/workflow/inputs.go — BindInputs (new; pure, no existing callers); internal/refs/refs.go — ScanToken (existing quoted-token precedent)
**Tests:** positional, keyed, mixed, quoted, missing required, unknown key, surplus. Also: a declared default fills an unbound input; a required input with a default is not missing.
**Acceptance:** `go test -race -count=1 ./internal/workflow/`
**Commit:** `feat(workflow): bind recipe inputs from text`

## 20. The engine starts a recipe

**What:** Recast at the regression check (2026-09-27). Depends on items 15, 19.
**Goal:** `Agent.StartRecipe(ctx, RecipeLaunch{SkillID, Text, Background})` resolves the skill, binds inputs (a missing required one is asked through the Asker; with no Asker it is an error), runs the workflow with a `ScriptRunner` that obeys the Mode and approval rules of the agent's shell tool, and an `Asker` from the engine config; a foreground launch opens an Exchange whose first model request carries the user's line plus the result lines.
**Approach (assumed at the header base):** New `internal/agent/recipe.go`; `domain.UserInput` gains an optional `Recipe` field consumed in `composeUserMessage` ahead of `resolveSkillRefs`; facade export.
**Regression guard.** a `/<recipe-skill>` reference at the START of a user input launches that recipe in every Driver (engine-side, composeUserMessage/run.go knownSkillID path — TUI, headless, daemon, firings alike); elsewhere in the text it attaches the body as today; item 20's ScriptRunner stages a script from a `shipped:<id>` skill dir into the workflow folder before running it (item 23 relies on this). The catalog reaches the agent through a port declared in agent or workflow (set by an agent Option or Config field) returning Plan, Inputs and Dir by id plus the list of recipe ids, implemented in internal/skills (catalog.go, provider.go) and wired in cmd/apogee/wire_config.go — the loop still never imports internal/skills (the item yields to ADR 0010 and domain/config.go:914-917). A recipe launches only on `step()`'s `a.turns.open()` branch; `Interject` refuses an input carrying a recipe launch. `StartRecipe` binds text and calls a core `runRecipe(ctx, id, inputs map[string]string)` that opens no Exchange (item 21 calls the core). internal/agent/doc.go names recipe.go.
The launch is detected only on `in.SkillIDs[0]` (Driver-parsed through `refs.SkillRefs`) with `Text` beginning `"/"+id`, and only when `!a.isDelegate()` — a delegate's task opens through the same `turns.open()` branch (subagent.go:1061) and never launches a workflow (ADR 0087 D9). The headless test lives in internal/run/run_test.go (internal/run imports internal/agent).
**Files:** internal/agent/recipe.go, internal/agent/recipe_test.go, internal/agent/loop.go, internal/domain/config.go, apogee.go, internal/agent/interject.go, internal/agent/doc.go, internal/skills/catalog.go, internal/skills/provider.go, cmd/apogee/wire_config.go, internal/run/run.go, internal/run/run_test.go
**Read first:** internal/agent/loop.go — step (turns.open branch), composeUserMessage, resolveSkillRefs; internal/agent/subagent.go — runSubAgent (sub.Submit of the task);
  internal/run/run.go — knownSkillID; internal/domain/config.go — UserInput, Config.Asker; cmd/apogee/wire_config.go — projectConfig
**Tests:** foreground launch with stub children → first request carries result lines; script stage refused in Plan mode when it writes outside scratch; missing input asked. Also: a leading `/<recipe>` launches while a mid-text one attaches the body; a headless run with a leading recipe reference launches it; `Interject` refuses a recipe; a `shipped:` script is staged and run; a delegate whose task opens with `/<recipe>` launches nothing (attaches as today).
**Acceptance:** `go build ./... && go test -race -count=1 ./internal/agent/ && go test -race -count=1 ./internal/run/ && go test -race -count=1 ./internal/skills/`
**Commit:** `feat(agent): start a recipe as a workflow`

## 21. fan_out starts a recipe

**What:** Recast at the regression check (2026-09-27). Depends on item 20.
**Goal:** `fan_out{recipe, inputs}` starts the named recipe blocking and returns its result lines; an unknown recipe lists the recipe skills available; `recipe` with any fan-out field is a fixable error.
**Approach (assumed at the header base):** `runWorkflowCall` branches to the `StartRecipe` core.
**Regression guard.** load_skill on a recipe skill returns its body plus one line: `this is a recipe: start it with fan_out{recipe: "<id>"}` when fan_out is on the roster, else `the user starts it with /<id>`. `runWorkflowCall` calls item 20's `runRecipe(ctx, id, inputs)` core (no Exchange is opened; fan_out's `inputs` is keyed, not text); the unknown-recipe error lists recipe ids through item 20's port.
load_skill is one shared instance across parent and children (wire_config.go:122-125), so the recipe line is decided per call from the calling Agent's live menu — a ctx value dispatch sets, as `WithPromptSlot` is — never bound at construction; the child case is tested. If a `tools.HostTools` field is added, extend the every-field check (cmd/apogee/wire_tools_test.go:344) and add `go test -race -count=1 -run HostTools ./cmd/apogee/` to Acceptance.
**Files:** internal/agent/workflowcall.go, internal/agent/workflowcall_test.go, internal/tools/load_skill.go, internal/tools/load_skill_test.go, internal/domain/config.go, internal/skills/lookup.go, internal/agent/dispatch.go
**Read first:** internal/tools/load_skill.go — LoadSkill, NewLoadSkill, Execute; internal/domain/config.go — SkillLookup, SkillLookupResult, ResolvedSkill;
  cmd/apogee/wire_config.go — projectConfig; cmd/apogee/wire_tools_test.go — HostToolsOf every-field check
**Tests:** recipe run via fan_out; unknown recipe; mixed fields refused. Also: load_skill on a recipe skill carries the recipe line in both roster states; a child whose menu lacks fan_out gets `the user starts it with /<id>` from the shared instance.
**Acceptance:** `go build ./... && go test -race -count=1 ./internal/agent/ && go test -race -count=1 ./internal/tools/ && go test -race -count=1 ./internal/skills/`
**Commit:** `feat(agent): fan_out starts a recipe`

## 22. Invoking a recipe skill in the TUI runs it

**What:** Recast at the regression check (2026-09-27). Depends on item 20.
**Goal:** submitting `/<recipe-skill> <text>` runs the recipe as a foreground workflow (not an attached skill body); the transcript shows a workflow block with live per-item progress and the result lines; a missing required input opens the ask pane.
**Approach (assumed at the header base):** `Model.submit` → `parseInput` detects a recipe skill id (via `knownSkillID` + a recipe flag) and sets `UserInput.Recipe`; a workflow block renderer beside `renderSubAgentGroup` in `subagentblock.go` fed by `WorkflowPhaseEvent`.
**Regression guard.** item 22 only detects/renders — the launch rule itself is item 20's. A recipe line's id stays out of `skillIDs` (its body is never attached) and `parsedInput` marks it a recipe line; `joinedInterjections` never merges a recipe line behind held rows (it goes alone, keeping its leading position); `stageInterjection` and `stageChildMessage` refuse a recipe line with a line saying a recipe starts only when idle. WorkflowPhaseEvent's fold row is item 17's; if the block is a new entryKind it gains an `entryKindRules` row (plus a `session.EntryKind*` name, or "" if unpersisted) and a `renderEntryLines` case. internal/tui/doc.go names workflowblock.go.
`parseInput(raw, known)` keeps its signature (50 test call sites in 7 files): the recipe is marked after the parse, in `Model.submit` or a `parsedInput` method taking the recipe predicate. A persisted workflow entryKind adds its `session.EntryKind*` constant in internal/session/transcript.go (read by `TestEntryKindPersistedNamesAreUnique`).
**Files:** internal/tui/model.go, internal/tui/command.go, internal/tui/workflowblock.go, internal/tui/workflowblock_test.go, internal/refs/refs.go, internal/tui/interject.go, internal/tui/fold.go, internal/tui/entrykind.go, internal/tui/doc.go, internal/session/transcript.go
**Read first:** internal/tui/command.go — parseInput, parsedInput; internal/tui/prompteditor.go — submitParse; internal/tui/skills.go — Model.knownSkillID;
  internal/tui/interject.go — stageInterjection, joinedInterjections; internal/tui/entrykind.go — entryKindRules; internal/tui/entrykind_test.go — TestEntryKindPersistedNamesAreUnique
**Tests:** unit: recipe submit sets `Recipe`; block renders progress and result; a non-recipe skill still attaches. Also: a recipe line with held rows or a running worker is refused, never attached; `TestEntryKindRulesAnswerForEveryKind` and `TestEntryKindPersistedNamesAreUnique` pass; every existing `parseInput` call site compiles unchanged.
**Acceptance:** `go build ./... && go test -race -count=1 ./internal/tui/ && go test -race -count=1 ./internal/refs/ && go test -race -count=1 ./internal/session/`
**Commit:** `feat(tui): a recipe skill runs as a workflow`

## 23. The shipped audit recipe: skeleton and split

**What:** Depends on item 18.
**Goal:** a shipped skill `audit` (`internal/skills/shipped/audit/`) declares inputs `scope` (required) and `focus` (optional), and a recipe: script `split.sh` (parts and groups sized to the window), ask `focus` (default `all`), fanout ground-truth, then lens fanout over parts × lenses (concurrency lens `when:` the ground-truth flag), merge per group on large scopes, pick claims, verify, merge report. Prose body explains the stages for a model that loads it with `load_skill`.
**Approach (assumed at the header base):** Port `~/.claude/skills/code-audit/SKILL.md` phases and `prompts/split.sh` (POSIX sh; staged to the workflow folder before it runs, since `shipped:` paths are not executable). Update `TestShippedCatalogIDsArePinned`, `TestShippedTriggersKeepTheirTokenCounts`, and the `cmd/apogee/testdata/frames/t12-skills.txt` golden.
**Regression guard.** add docs/adr/0065-*.md to Files with a dated `> **Amended 2026-09-27 (ADR 0087 D6).**` note that the shipped set is five skills with `audit`. `hostileSkillsHeader` (cmd/apogee/e2e_hostile_test.go:353) becomes `· 6 skills available:` and its "four shipped skills" comments (:130, :348) say five. The port writes `<RUN>/scope.txt` from `scope`, emits one `KEY=value` per line (item 10's shape) and takes the window as `PART_LINES`; item 20's ScriptRunner stages it. The prose body names only read-tool addresses (prompts via `{{SKILL_DIR}}`) and never tells the model to execute a bundled script from its `shipped:` address (TestShippedSkillAnnouncesOnlyReadableAddresses). Every prose site naming the shipped set or its count is updated: `grep -rn -i "four shipped\|ships four\|There are four\|commit-hygiene\`)" README.md docs/manual CONTEXT.md internal/skills`.
**Files:** internal/skills/shipped/audit/SKILL.md, internal/skills/shipped/audit/split.sh, internal/skills/load_test.go, internal/skills/suggest_test.go, cmd/apogee/testdata/frames/t12-skills.txt, docs/adr/0065-shipped-skills-and-the-load-skill-door.md, cmd/apogee/e2e_hostile_test.go, README.md, docs/manual/configuration.md, CONTEXT.md, internal/skills/doc.go
**Read first:** internal/skills/load_test.go — TestShippedCatalogIDsArePinned, shippedIDs; cmd/apogee/e2e_hostile_test.go — hostileSkillsHeader; internal/skills/suggest_test.go — TestShippedTriggersKeepTheirTokenCounts (needs a non-nil row even with no triggers);
  internal/skills/load.go — loadShipped, shippedFiles; internal/agent/skillmount_test.go — TestShippedSkillAnnouncesOnlyReadableAddresses; docs/manual/configuration.md — "Skills apogee ships" section
**Tests:** the shipped skill loads with a valid recipe; split.sh on a fixture tree yields parts under budget. Also: split.sh emits one `KEY=value` per line and reads `PART_LINES`; the shipped skill's body passes TestShippedSkillAnnouncesOnlyReadableAddresses.
**Acceptance:** `go test -race -count=1 ./internal/skills/ && go test -race -count=1 -run TestShippedSkillAnnouncesOnlyReadableAddresses ./internal/agent/ && go test -race -count=1 -run 'TestE2EHostileSurfacesKeepTheirOwnRows|TestE2ESmokeInProcess' ./cmd/apogee/`
**Commit:** `feat(skills): the shipped audit recipe skeleton`

## 24. The audit recipe's prompts

**What:** Depends on item 23.
**Goal:** `internal/skills/shipped/audit/prompts/` holds shared rules, ground-truth, machine-checks, the five lenses, rollup, claim enumeration, verify and report prompts, each ending in a `finish` call with the stage's receipt fields instead of a text receipt; no prompt names a Claude Code tool or a `~/.claude` path.
**Approach (assumed at the header base):** Port from `~/.claude/skills/code-audit/prompts/*.md`; replace the six-line receipt contract with `finish` fields; drop the coordinator-staging steps (the engine stages).
**Regression guard.** The load_test.go test also asserts every stage prompt the recipe names contains `finish`, and no prompt contains `Agent(`, `Task(`, `TodoWrite`, `subagent_type`, `STATUS:` receipt lines or `~/.claude`.
**Files:** internal/skills/shipped/audit/prompts/*.md, internal/skills/shipped/audit/SKILL.md, internal/skills/load_test.go
**Read first:** internal/skills/load_test.go — shippedIDs, TestLoadShippedSkillsAllParse; internal/skills/load.go — shippedFiles, ShippedMountPrefix; internal/skills/export.go — copyTree (nested prompts/ exports);
  ~/.claude/skills/code-audit/prompts/_shared.md — receipt contract, rule 8 step-cap numbers; ~/.claude/skills/code-audit/prompts/verify.md, merge.md, verify-enumerate.md
**Tests:** a test asserts every prompt path the recipe names exists in the embed and no prompt contains `~/.claude` or `Agent(`. Also: every named prompt contains `finish`; none contains `Task(`, `TodoWrite`, `subagent_type` or a `STATUS:` receipt line.
**Acceptance:** `go test -race -count=1 ./internal/skills/ && ! grep -rn "\.claude" internal/skills/shipped/audit/`
**Commit:** `feat(skills): the audit recipe's stage prompts`

## 25. Background workflow manager

**What:** Depends on items 15, 20.
**Goal:** `internal/agent` runs background workflows outside any Turn: at `ParallelAgents − 1` width (min 1, sharing the slot on a width-1 server), one at a time per server with the rest queued, listed by `Agent.Workflows()`, stopped by `Agent.StopWorkflow(id)` (keeping finished items), all stopped by `Close`; the running set is saved as an additive `workflows` snapshot key and resumed on restore.
**Approach (assumed at the header base):** New `internal/agent/background.go` owning goroutines and a per-server queue; snapshot pattern of `Retained` in `state.go` (`encodeState`/`restoreState`). Background asks queue in the manager (item 30 surfaces them).
**Regression guard.** `restoreState` only loads and validates the `workflows` set; a separate `Agent.ResumeWorkflows()` starts it, called by the Driver after Bind or RestoreSession (item 34's path) — never inside `resumeAgent`, which runs before `lateEngine.Bind` replays scratch, reactions, profile and target. `RestoreSession` stops the outgoing session's set (finished items kept) before resuming the incoming one. `Close` stops background workflows only when `!a.isDelegate()` (closeConsoles' rule), so a finishing child's `sub.Close` never kills them. Approvals from background children queue in the manager beside asks (item 30 surfaces both). `checkRestoredStructure` validates the entries (ids only, folder resolved under `<scratch>/workflows/`, `ErrSnapshotRefused` otherwise); `CutSession` writes no `workflows` key; `workflows` joins TestAgentState_EncodesStableKeyNames as omitempty. Children follow item 12's contract. internal/agent/doc.go names background.go.
**Files:** internal/agent/background.go, internal/agent/background_test.go, internal/agent/state.go, internal/agent/state_test.go, internal/agent/agent.go, internal/agent/doc.go
**Read first:** internal/agent/state.go — restoreState, checkRestoredStructure, CutSession; internal/agent/agent.go — Close, closeConsoles, RestoreSession;
  internal/agent/construct.go — resumeAgent; cmd/apogee/wire_engine.go — lateEngine.Bind
**Tests:** width minus one; queueing; stop keeps finished; Close stops all; snapshot round trip resumes. Also: RestoreSession stops the outgoing set; a finished delegation leaves a background workflow running; a background approval queues; a folder outside `<scratch>/workflows/` is refused; a cut session carries no `workflows` key.
**Acceptance:** `go build ./... && go test -race -count=1 ./internal/agent/`
**Commit:** `feat(agent): background workflows`

## 26. Finish notes and the wake

**What:** Depends on items 13, 25.
**Goal:** when a background workflow ends, a one-line note (name, item counts by status, headline tallies, report path) reaches the parent at the next between-Steps boundary if an Exchange is running; otherwise the engine emits `WorkflowPhaseEvent{Finished}` and holds the note, and `Agent.Wake(ctx)` opens an Exchange on the held note (ADR 0007 as amended); with `workflow-wake: off` the note joins the next user message.
**Approach (assumed at the header base):** Reuse the interjection drain point for the mid-Exchange note; `Wake` composes an engine-note opener instead of a user message.
**Regression guard.** The mid-Exchange note is delivered through the Driver's drain: the engine hands the held note to the Driver and the TUI's `deliverInterjections` commits it via `Interject` (item 28 adds that call beside Wake) — no engine slot consumed by `step()`, so the item yields to ADR 0025's rejected option (docs/adr/0025-…:244) and Run's depth-0 no-drain contract (agent.go:833). `WorkflowPhaseEvent{Finished}` is always emitted; the held note is consumed by whichever comes first — the next Turn of a running Exchange, Wake, or the next Submit. The wake opener carries its note as recorded text (a fixed plain header plus the line), not only an engine-note fence, so a snapshot and restore keep it.
**Files:** internal/agent/background.go, internal/agent/interject.go, internal/agent/loop.go, internal/agent/wake_test.go, apogee.go
**Read first:** internal/agent/interject.go — Interject; internal/agent/agent.go — Run, Submit; internal/tui/worker.go — deliverInterjections; internal/domain/advice.go — WithEngineNote, recordContent;
  internal/agent/state.go — exchangeOpenings; docs/adr/0025-interjections-commit-at-the-between-steps-boundary.md — Considered options
**Tests:** mid-Exchange delivery at the boundary; idle → event + Wake opens an Exchange; wake off → note rides next Submit. Also: a finish during an Exchange's final Turn still wakes; a snapshot and restore keep the wake opener's note.
**Acceptance:** `go build ./... && go test -race -count=1 ./internal/agent/`
**Commit:** `feat(agent): background workflows report back and wake the agent`

## 27. The workflow control tool and background fan_out

**What:** Depends on items 14, 25.
**Goal:** a default-off `workflow` tool offers `status` (all or one), `stop` and `message` (to a running item by run id or item name, delivered as an Interjection); `fan_out{background: true}` starts a background workflow and returns its id and status path at once.
**Approach (assumed at the header base):** `internal/tools/workflow.go` placeholder handled in dispatch like `fan_out`; `message` routes through `InterjectChild`. Registry pins, `KnownToolNames`, manual list.
**Regression guard.** `workflow` joins `writeCapableNonFileBuiltins` in internal/agent/writedetection_test.go. When the engine has no conversation to go on (the internal/run Driver), `background` stays off the fan_out schema and a `background: true` reaching dispatch runs blocking — the item yields to ADR 0089 D1 ("Headless and daemon runs offer no background"), pinned in workflow_test.go. Children follow item 12's contract. internal/tools/doc.go names workflow.go.
**Files:** internal/tools/workflow.go, internal/tools/workflow_test.go, internal/tools/registry.go, internal/tools/registry_test.go, internal/agent/workflowcall.go, internal/agent/workflowcall_test.go, docs/manual/configuration.md, internal/agent/writedetection_test.go, internal/tools/doc.go, internal/run/run.go
**Read first:** internal/tools/registry.go — builtinToolsWith, KnownToolNames; internal/agent/writedetection_test.go — writeCapableNonFileBuiltins; internal/agent/children.go — InterjectChild, StopChild;
  internal/tools/manual_drift_test.go — TestManualListsEveryKnownToolName; internal/run/run.go — Run
**Tests:** each action; background fan_out returns immediately; message reaches the child's mailbox. Also: an internal/run engine offers no `background` and runs one blocking.
**Acceptance:** `go build ./... && go test -race -count=1 ./internal/tools/ && go test -race -count=1 ./internal/agent/`
**Commit:** `feat(tools): the workflow control tool and background fan_out`

## 28. The TUI wakes the agent

**What:** Recast at the regression check (2026-09-27). Depends on item 26. The Engine call that takes the held finish note (item 26's guard) is `TakeWorkflowNotes() []string` — the worker's call at the between-Steps boundary, returning and clearing the held notes, which `deliverInterjections` commits via `Interject`; it joins `tui.Engine`, `lateEngine` and `fakeEngine` here beside `Wake`.
**Goal:** the TUI shows a background workflow's finish line in the transcript; when idle and `workflow-wake: on` it opens an Exchange through `Agent.Wake`; when busy the note arrives at the boundary; `esc` never stops a background workflow.
**Approach (assumed at the header base):** A `Bridge.NotifyWorkflow` → Msg route as `NotifySchedule` does; launch like `runContinue` via `launchExchange` with a wake input.
**Regression guard.** (a) the wake's transcript row counts as a depth-0 fork point and as the prompt markAborted marks, so TUI and engine Exchange openings agree, pinned by /fork-after-wake and esc-on-wake tests; (b) the wake launches only at stateIdle with no idle-only operation in flight (sessions load, queued fork/record write, open modal pane), otherwise it is held until the next idle fold; (c) events of runs belonging to a background workflow (known from WorkflowPhaseEvent) skip foldEvent's transcript, activity and stall folds and go to the workflow's own state — rule named and tested; (d) Wake and TakeWorkflowNotes join tui.Engine, lateEngine (cmd/apogee/wire_engine.go) and fakeEngine (internal/tui/seam_test.go) with a worker driver beside driveExchange in internal/tui/worker.go — add those files and `go build ./...` to Acceptance. internal/tui/doc.go names workflow.go.
**Files:** internal/tui/bridge.go, internal/tui/workflow.go, internal/tui/workflow_test.go, internal/tui/commandrun.go, internal/tui/tui.go, cmd/apogee/wire_engine.go, internal/tui/seam_test.go, internal/tui/worker.go, internal/tui/transcript.go, internal/tui/fold.go, internal/tui/doc.go
**Read first:** internal/tui/transcript.go — forkPoints, markAborted; internal/tui/commandrun.go — launchExchange; internal/tui/worker.go — driveExchange; internal/tui/tui.go — Engine;
  cmd/apogee/wire_engine.go — lateEngine; internal/tui/seam_test.go — fakeEngine; internal/tui/sessions.go — resumeLoaded
**Tests:** unit: finish while idle launches an Exchange; while running does not; esc leaves the workflow running. Also: /fork after a wake and esc on a wake's first Turn; a wake held while a session load is in flight; a background workflow's child events reach neither the transcript, the activity board nor the stall clock; a note held while a worker runs is taken by `TakeWorkflowNotes` at the boundary and committed via `Interject`.
**Acceptance:** `go build ./... && go test -race -count=1 ./internal/tui/`
**Commit:** `feat(tui): a finished background workflow wakes the agent`

## 29. The /bg command

**What:** Depends on items 22, 25.
**Goal:** `/bg /<recipe-skill> <text>` starts that recipe as a background workflow and prints `started <id> in the background`; `/bg` with a non-recipe skill or no skill is refused with a line naming the recipe skills.
**Approach (assumed at the header base):** A `commandSpecs` row (alphabetical) with `restVerb`; calls `StartRecipe{Background: true}`.
**Regression guard.** adds StartRecipe to tui.Engine, lateEngine and fakeEngine (Files + `go build ./...`); unbound, lateEngine answers `errNoServerBound`. `StartRecipe` is called from a tea.Cmd under a manager-owned context; inputs are bound first and a missing required input is refused with `missing input: <name>` instead of asked, until item 30's idle ask exists.
**Files:** internal/tui/command.go, internal/tui/commandrun.go, internal/tui/command_test.go, docs/manual/commands.md, internal/tui/tui.go, internal/tui/seam_test.go, cmd/apogee/wire_engine.go
**Read first:** internal/tui/command.go — commandSpecs, parseInput; internal/tui/commandrun.go — restVerb; internal/tui/tui.go — Engine; cmd/apogee/wire_engine.go — lateEngine, errNoServerBound;
  internal/tui/seam_test.go — fakeEngine; internal/tui/parkedcall.go — parkCall
**Tests:** `TestCommandSpecsReadAlphabetically`; start and both refusals, exact texts. Also: `missing input: <name>` pinned exactly.
**Acceptance:** `go build ./... && go test -race -count=1 ./internal/tui/`
**Commit:** `feat(tui): /bg starts a recipe in the background`

## 30. Status indicator and waiting questions

**What:** Depends on items 25, 28.
**Goal:** the status line shows `N workflows running` and, when a background `ask` waits, `1 workflow waiting for you`; the waiting question opens in the ask pane when the user is idle; the answer resumes the workflow.
**Approach (assumed at the header base):** A `statusLeft` qualifier beside the queued readout; the manager's queued asks surface through the Bridge; reuse `foldAskRequest`/`askPromptSpec` outside an Exchange.
**Regression guard.** adds the waiting-question engine calls to tui.Engine, lateEngine and fakeEngine (Files + `go build ./...`). The ask request carries its origin: a background ask's answer returns the TUI to stateIdle through the engine seam (never `resumeRunning`), and esc dismisses it back to the waiting queue (never `stopWorker`); queued background approvals (item 25) surface the same way. The running count and the waiting flag are folded into the Model from WorkflowPhaseEvent; `statusLeft` never reads the Engine.
**Files:** internal/tui/model.go, internal/tui/ask.go, internal/tui/workflow.go, internal/tui/workflow_test.go, internal/tui/tui.go, internal/tui/seam_test.go, cmd/apogee/wire_engine.go, internal/tui/approval.go, internal/agent/background.go
**Read first:** internal/tui/ask.go — foldAskRequest, submitAnswer; internal/tui/model.go — statusLeft, statusLine, resumeRunning; internal/tui/asker.go — uiAsker.Ask;
  internal/tui/tui.go — Engine; cmd/apogee/wire_engine.go — lateEngine
**Tests:** indicator texts; a waiting ask opens only when idle; answer delivered. Also: an idle answer returns to stateIdle; esc re-queues the question; a queued background approval opens at idle.
**Acceptance:** `go build ./... && go test -race -count=1 ./internal/tui/ && go test -race -count=1 ./internal/agent/`
**Commit:** `feat(tui): background workflow indicator and waiting questions`

## 31. The /workflows view: list and detail

**What:** Depends on items 17, 25.
**Goal:** `/workflows` opens a pane listing the session's workflows (name, state, item counts); enter opens one: stages, items with status and summary; enter on an item opens its detail output and conversation read-only; `esc` goes one level up.
**Approach (assumed at the header base):** A new `framePane` + `paneSpecs` row + `keyClaimOrder` rung (check the 16-pane cap), built on `listSurface` like the `/sessions` browser; data from `status.json` via `Agent.Workflows()`.
**Regression guard.** adds Workflows to tui.Engine, lateEngine and fakeEngine (Files + `go build ./...`). Every list pinned per pane or per rung gains the new pane: keyclaim_test.go, panes_test.go (the `modal` map plus a `paneFixtures` row), mouse.go `pointerPanes` and mouse_test.go. The list and each detail load through a tea.Cmd and fold into plain values on the Model (ADR 0011), refreshed on WorkflowPhaseEvent; render and height read only Model state. `/workflows` is recallable (no `noRecall`, as /sessions) and `whileRunning`; its pane's keyOpen follows the picker's `state.live()` gate. internal/tui/doc.go names workflows.go.
**Files:** internal/tui/workflows.go, internal/tui/workflows_test.go, internal/tui/panes.go, internal/tui/model.go, internal/tui/command.go, internal/tui/tui.go, internal/tui/seam_test.go, cmd/apogee/wire_engine.go, internal/tui/keyclaim_test.go, internal/tui/panes_test.go, internal/tui/mouse.go, internal/tui/mouse_test.go, internal/tui/doc.go
**Read first:** internal/tui/panes.go — paneSpecs; internal/tui/model.go — framePane, keyClaimOrder; internal/tui/mouse.go — pointerPanes; internal/tui/sessions.go — openSessionBrowser, listSessions;
  internal/tui/panes_test.go — TestEveryFramePaneHasASpec; internal/tui/keyclaim_test.go — TestKeyClaimOrderMatchesTheDocumentedPrecedence
**Tests:** `TestEveryFramePaneHasASpec`; list → detail → item → esc chain. Also: `TestKeyClaimOrderMatchesTheDocumentedPrecedence`, `TestPointerPanesWalkInTheClickChainOrder`, `TestOverlayHeightQueryMatchesItsRender`; the pane opens while a Turn runs.
**Acceptance:** `go build ./... && go test -race -count=1 ./internal/tui/`
**Commit:** `feat(tui): the /workflows view`

## 32. /workflows actions: stop and re-run failed

**What:** Depends on item 31.
**Goal:** in the `/workflows` detail, `ctrl+x` stops a running workflow (finished items kept) and `ctrl+r` re-runs its blocked and faulted items only, as a new run of the same workflow.
**Approach (assumed at the header base):** `Agent.StopWorkflow`, and `Agent.RerunFailed(id)` resetting those items' store state and resuming.
**Regression guard.** adds StopWorkflow and RerunFailed to tui.Engine, lateEngine and fakeEngine (Files + `go build ./...`); unbound, both answer `errNoServerBound`. Re-run is the chord `ctrl+r` beside `ctrl+x`, since `listKey` sends every printable key to the filter — the item yields to the ratified chords-for-the-filter rule (internal/tui/sessions.go:297); the key is pinned in the test.
**Files:** internal/tui/workflows.go, internal/tui/workflows_test.go, internal/agent/background.go, internal/agent/background_test.go, internal/tui/tui.go, internal/tui/seam_test.go, cmd/apogee/wire_engine.go
**Read first:** internal/tui/sessions.go — sessionBrowserVerb, sessionConfirmKey; internal/tui/listsurface.go — listKey; internal/tui/tui.go — Engine.StopChild;
  internal/agent/children.go — Agent.StopChild; cmd/apogee/wire_engine.go — lateEngine; internal/tui/seam_test.go — fakeEngine
**Tests:** stop keeps finished; re-run touches only failed items. Also: `ctrl+r` is the re-run key; a bare `r` types into the filter.
**Acceptance:** `go build ./... && go test -race -count=1 ./internal/tui/ && go test -race -count=1 ./internal/agent/`
**Commit:** `feat(tui): stop and re-run failed workflow items`

## 33. Save a fan_out as a recipe

**What:** Recast at the regression check (2026-09-27). Depends on items 18, 31.
**Goal:** in the `/workflows` detail of a `fan_out` workflow, `ctrl+s` asks for a skill name and writes `~/.apogee/skills/<name>/SKILL.md` whose recipe reproduces the workflow's plan, with `scope` as an input where the `over` source was a path; an existing name is refused, never overwritten; the new skill is loadable at once.
**Approach (assumed at the header base):** `workflow.PlanToRecipe(Plan) ([]byte, error)` in the workflow package; the name prompt reuses the picker text-entry pattern.
**Regression guard.** torecipe_test.go is an external `package workflow_test` that writes to a temp dir and reads back through `skills.Load(Sources{Home: tmp}).Get(name).Recipe` (an in-package test importing skills is a cycle; `parseSkill` is unexported). The skill is written to `filepath.Join(m.opts.ConfigHome, "skills", name)` with ExportShipped's `os.Mkdir` claim and name rule (`validShippedID`); a name the catalog already serves (`Get`) or a command verb (`commandByName`) is refused, and `skillRescanCmd` runs so it is loadable at once. PlanToRecipe emits inline `task:` stages (item 18's schema) and a SKILL.md with a description and a non-empty body. internal/workflow/doc.go names torecipe.go.
The save key is `ctrl+s`, not a bare `s`: a bare letter hits the listSurface filter, as item 32 found with ctrl+r.
**Files:** internal/workflow/torecipe.go, internal/workflow/torecipe_test.go, internal/tui/workflows.go, internal/tui/workflows_test.go, internal/workflow/doc.go
**Read first:** internal/skills/export.go — ExportShipped, validShippedID; internal/tui/skillscmd.go — exportShippedSkill; internal/skills/parse.go — validate; internal/skills/load.go — Load, Sources;
  internal/tui/autocomplete.go — skillRescanCmd; internal/tui/command.go — commandByName
**Tests:** round trip plan → SKILL.md → parse → same plan; refusal on existing name. Also: `../x`, a spaced name, `audit` and a command verb are refused; `ctrl+s` opens the name prompt while a bare `s` types into the filter.
**Acceptance:** `go build ./... && go test -race -count=1 ./internal/workflow/ && go test -race -count=1 ./internal/tui/ && go test -race -count=1 ./internal/skills/`
**Commit:** `feat(tui): save a fan_out as a recipe`

## 34. /clear, quit and resume with workflows running

**What:** Depends on items 25, 26, 32.
**Goal:** `/clear` with a background workflow running asks `stop running workflows? (y/n)` — `y` stops them, `n` keeps them and their finish notes go to the new conversation; quitting stops them; resuming the session (flag or `/sessions`) resumes them.
**Approach (assumed at the header base):** A confirm step in `Model.startNewSession` on the `sessionBrowser.confirming` pattern; resume via the snapshot key of item 25 in `resumeLoaded` and the `wire_live.go` resume path; quit via `engine.Close()`.
**Regression guard.** Depends on item 32 as well (uses StopWorkflow), adds no Engine method. While the `/clear` confirm is open the deferred-command drain and the staged-message flush stop, resuming from the answer's fold; the confirm is a picker kind (the start-up `pickerKeyMigration` precedent, picker.go:90), not the browser's in-pane confirm. A `/sessions` switch and `/fork` take the same stop-or-keep decision, and a restore skips a workflow the live manager already runs; `saveAtIdle` also saves while background workflows are live, so a `/bg`-only session survives quit and resume. A stop from the confirm, and Close at quit, emit no held note and no wake. The `n` answer is an exception to ClearContext's drop of session-owned live state (agent.go:1773, ADR 0086 D3), named in that doc comment.
**Files:** internal/tui/commandrun.go, internal/tui/commandrun_test.go, internal/tui/sessions.go, cmd/apogee/wire_live.go, cmd/apogee/wire_live_test.go, cmd/apogee/wire_engine.go, docs/manual/commands.md, internal/tui/picker.go, internal/tui/sessionsave.go, internal/tui/fork.go, internal/agent/agent.go
**Read first:** internal/tui/commandrun.go — startNewSession, runDeferredCommands; internal/tui/sessions.go — resumeLoaded; internal/tui/sessionsave.go — saveAtIdle; internal/agent/state.go — CutSession;
  cmd/apogee/wire.go — rootWiring.close; cmd/apogee/wire_engine.go — buildAgent, lateEngine
**Tests:** both answers; resume restarts a workflow with finished items skipped. Also: a `TestWorkflowResume…` in cmd/apogee/wire_live_test.go; a queued `/clear` plus a message with a workflow running; `/bg` alone, quit, resume; `/fork` and a `/sessions` switch ask the same question.
**Acceptance:** `go build ./... && go test -race -count=1 ./internal/tui/ && go test -race -count=1 ./internal/agent/ && go test -race -count=1 -run Workflow ./cmd/apogee/`
**Commit:** `feat(tui): /clear, quit and resume respect running workflows`

## 35. Headless --recipe

**What:** Depends on items 17, 20.
**Goal:** `apogee headless --recipe <id> [text]` runs the recipe blocking (no background), `ask` stages take their defaults and the result says so, `--format json` streams `workflow_phase` events, and the exit status is non-zero when the workflow is stopped or every item blocked.
**Approach (assumed at the header base):** A flag in `newHeadlessCommandWith`; `runHeadlessBody` passes a `Recipe` input through `raise`/`run.Once`.
**Regression guard.** the recipe carrier is a firingInputs field flowing to run.Spec.Recipe with raise's signature unchanged. A nil Asker is the engine rule: an `ask` stage takes its declared default and the report notes it; `Config.Asker` stays nil so ask_user stays off a --recipe run's menu (the item yields to ADR 0033 decision 2), pinned. Under --recipe a missing argument means empty recipe text: no stdin read and no `errHeadlessNoPrompt`; unbound required inputs fail via item 20's no-Asker error. A stopped or all-blocked workflow exits `exitRunFailed` (1); the Long help's exit-1 cause list and the manual's exit-1 row say so, and TestHeadlessFormatJSONFramesEveryExit covers the --recipe exits.
**Files:** cmd/apogee/headless.go, cmd/apogee/headless_test.go, internal/run/run.go, docs/manual/headless.md, cmd/apogee/wire_firing.go, cmd/apogee/headless_help_test.go
**Read first:** cmd/apogee/headless.go — runHeadlessBody, resolveHeadlessPrompt, exitCodeFor; cmd/apogee/wire_firing.go — raise, firingInputs; internal/run/run.go — Spec, Once;
  cmd/apogee/headless_help_test.go — TestHeadlessHelpNamesEveryExitCode
**Tests:** stub-upstream recipe run; default-taken note; json events. Also: ask_user is not on a --recipe run's menu; `--recipe <id>` with no text and all-optional inputs runs; the exit-1 cases in `TestHeadlessHelpNamesEveryExitCode` and `TestHeadlessFormatJSONFramesEveryExit`.
**Acceptance:** `go build ./... && go test -race -count=1 -run Headless ./cmd/apogee/`
**Commit:** `feat(headless): run a recipe with --recipe`

## 36. Daemon run: workflow:

**What:** Depends on item 35.
**Goal:** `schedules.yaml` accepts `run: {workflow: {recipe: <id>, inputs: <text>}, workspace, mode, server, model}`; `prompt` and `workflow` are mutually exclusive (a defect naming both); a firing runs the recipe as headless does.
**Approach (assumed at the header base):** `Action` in `internal/daemon/file.go` gains `Workflow WorkflowAction` (a comparable value; zero means absent); `validateEntry`; `daemonWiring.fire` passes it to `raise`.
**Regression guard.** reuses item 35's carrier; the daemon key is `run: workflow: {recipe, inputs}` (the ADRs now say so). `Entry.Spec()` (internal/daemon/diff.go) gives a workflow entry a non-empty label as its Prompt, so schedule's `ErrPrompt` never refuses adoption (it is also the `fired … — %s` line, daemon.go), and `fire()` reads `entry.Run.Workflow` instead of `f.Prompt`. `Workflow` is a comparable value field, as Entry's doc requires (file.go:37-39). The template's `prompt: required` (cmd/apogee/defaults/schedules.yaml:81) and validateEntry's no-prompt defect name `workflow:` as the alternative. The workflow-firing test builds its Entry without entryFor's prompt default.
**Files:** internal/daemon/file.go, internal/daemon/file_test.go, cmd/apogee/daemonfire.go, cmd/apogee/daemonfire_test.go, docs/manual/daemon.md, internal/daemon/diff.go, internal/daemon/diff_test.go, cmd/apogee/daemon.go, cmd/apogee/daemon_test.go, cmd/apogee/defaults/schedules.yaml, cmd/apogee/wire_firing.go
**Read first:** internal/daemon/file.go — Action, Entry, validateEntry; internal/daemon/diff.go — Diff, Entry.Spec; internal/schedule/schedule.go — ErrPrompt;
  cmd/apogee/daemonfire.go — daemonWiring.fire; cmd/apogee/daemonfire_test.go — entryFor
**Tests:** parse, exclusivity defect, a firing runs the recipe. Also: a workflow entry goes through daemon.Load→Apply against the real scheduler (the TestDaemonFiresAnAdoptedScheduleOnItsTick pattern); a Diff test keeps an unchanged workflow entry.
**Acceptance:** `go test -race -count=1 ./internal/daemon/ && go test -race -count=1 -run Daemon ./cmd/apogee/`
**Commit:** `feat(daemon): schedule a recipe with run: workflow:`

## 37. Bench readiness for workflows

**What:** Depends on items 15, 20, 25.
**Goal:** the root facade exports what an embedder needs to run a `fan_out` workflow and a recipe in-process (types, `StartRecipe`, `Workflows`, events), and `benchreadiness_test.go` drives both against a stub upstream to quiescence.
**Approach (assumed at the header base):** Aliases in `apogee.go`; compile pins in `example_test.go`.
**Regression guard.** Depends on item 25 (it defines `Agent.Workflows()`). The public route by which a recipe reaches StartRecipe is item 20's recipe port, exported and aliased in apogee.go with workflow Plan, Stage and ReceiptSpec; TestBenchReadinessRunsAWorkflow uses only that route and adds no internal import beyond session, tools and stubllm (the benchreadiness_test.go header).
**Files:** apogee.go, example_test.go, benchreadiness_test.go
**Read first:** apogee.go — SkillResolver, ResolvedSkill; benchreadiness_test.go — TestBenchReadinessContract, runToQuiescence, hermeticArm; apogee_alias_internal_test.go — TestEveryDomainEventVariantIsAliased;
  example_test.go — compile-pin block; internal/domain/config.go — Tools ("taken exactly as given", so the bench registers fan_out itself)
**Tests:** a new `TestBenchReadinessRunsAWorkflow`.
**Acceptance:** `go build ./... && go test -race -count=1 -run BenchReadiness .`
**Commit:** `feat(apogee): export workflows to embedders`

## 38. Workflows manual page and README

**What:** Depends on items 29, 33, 36.
**Goal:** `docs/manual/workflows.md` documents `fan_out`, the result shape, recipes (header format, the seven stage kinds, inputs, conditions), `/bg`, `/workflows`, background workflows and the wake, the `workflow` tool, the config keys and how to enable the tools per model; the manual index links it; the README's tool counts match `internal/tools/registry.go`.
**Approach (assumed at the header base):** Plain-language manual style of `docs/manual/commands.md`; counts read from the registry at run time.
**Regression guard.** The Acceptance also greps docs/manual/README.md for `(workflows.md)` and README.md for `36 built-in tools` (`len(KnownToolNames())` after items 14 and 27, both default-off; the menu stays 30). docs/manual/commands.md gains a `/workflows` entry linking workflows.md (the index promises every in-chat command there).
**Files:** docs/manual/workflows.md, docs/manual/README.md, README.md, docs/manual/commands.md
**Read first:** internal/tools/manual_drift_test.go — TestManualListsEveryKnownToolName; internal/tools/registry.go — KnownToolNames, domain.IsDefaultOff; README.md — "built-in tools" bullet, Skills bullet;
  docs/manual/README.md — page table; docs/manual/commands.md — command list; docs/manual/configuration.md — tools roster list
**Tests:** `go test -race -count=1 ./internal/tools/` (manual drift) passes.
**Acceptance:** `test -s docs/manual/workflows.md && grep -q "(workflows.md)" docs/manual/README.md && grep -q "36 built-in tools" README.md && go test -race -count=1 -run Manual ./internal/tools/`
**Commit:** `docs(manual): workflows and recipes`

## 39. The bench experiment design

**What:**
**Goal:** `docs/design/workflow-bench-experiment.md` states the experiments that gate turning `fan_out`, `background` and `workflow` on per model class: `audit` three ways on a small model (prose orchestrator via the user's `code-audit` skill, prose-sequential, recipe), and a task set with `fan_out` on vs off, with metrics (findings confirmed, context tokens at the orchestrator, completion rate) under ADR 0009's decision rule.
**Approach (assumed at the header base):** Design doc only; the runs live in `apogee-sim`.
**Files:** docs/design/workflow-bench-experiment.md
**Read first:** docs/adr/0009-the-ab-decision-rule.md — decision rule; docs/adr/0087-the-engine-runs-workflows-the-model-or-a-recipe-asks-for.md — D8 flagship experiment;
  docs/design/test-drivers.md — design-doc house style; ~/.claude/skills/code-audit/SKILL.md — prose-orchestrator arm
**Tests:** none (docs).
**Acceptance:** `test -s docs/design/workflow-bench-experiment.md`
**Commit:** `docs(design): the workflow bench experiment`
