# Engine-run workflows — plan

**Goal:** The engine runs Workflows (fan-outs the model asks for through `fan_out`, and Recipes a human wrote into a skill), hands back one line per item plus a report path, can run them in the background and wake the agent, and a cancel keeps every finished piece of work.
**Date:** 2026-09-27
**Status:** unexecuted
**sized for:** ~200k-context host
**base:** 1fb91ac0
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
- **Plan-mode recipe scripts amend ADR 0012** (owner, 2026-09-27): item 42 adds a dated amendment to ADR 0012 beside its 2026-09-14 amendment (a) — the one further loosening is an engine-built recipe script stage in Plan mode, run confined so it can write only its own workflow folder; every other subprocess route into scratch stays refused in Plan. ADR 0087's amendment cites it.
- **/workflows answers a question:** ^a in a workflow's detail opens its oldest waiting prompt (dismissed included), at idle only; mid-Turn it opens nothing and says why (owner, 2026-09-28).

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
- 27: run finding folded (2026-09-27, 7115ec0c) — `background` and `workflow` need the Driver's opt-in (TUI only; headless, daemon and the facade default off), per ADR 0089 D1/D4
- 34: run finding folded (2026-09-27, 7115ec0c) — held finish notes survive `/clear`-`n`, a kept switch or fork (`KeepWorkflows`, lifting "no Engine method") and quit→resume (`workflow_notes` snapshot key); `y` drops them
- 40–45: added (2026-09-27, 7115ec0c) from the run's findings (run_on + mistyped recipe; Plan offers fan_out per ADR 0087 D4; Plan script confine amending ADR 0087 D4/D6; workflow item runs nest; `inputs:` read only beside `recipe:`; split.sh quoting); not yet reviewed by independent regression reviewers
- 40: late check (2026-09-27, 28cd8ee5) — guard folded (background seat rides backgroundLaunch → backgroundRun; `newWorkflowSpawner` signature kept; `recipe: {}` stays no recipe, test uses `recipe: true`)
- 41: late check (2026-09-27, 28cd8ee5) — guard folded (resolve refuses fan_out/workflow in Plan with no scratch dir; Plan-menu case with `tools.NewWorkflow()`)
- 42: late check (2026-09-27, 28cd8ee5) — guard folded (narrowing keyed on Plan + workflowScriptDir; applyOverlays Tier-2 site; `platform.NewConfiner()` + agent TestMain sentinel; repo-wide doc grep); yields to ADR 0012's 2026-09-14 amendment (a) (0012:396-399)
- 43: late check (2026-09-27, 28cd8ee5) — recast (runHeadAt/headsRunFor stay sub_agent-only; runEnd, span/collapse walks, resolveBlock and continuesOpenRun take a separate head lookup; + recipe.go, background.go, render.go, doc.go)
- 44: late check (2026-09-27, 28cd8ee5) — guard folded (site is `parseWithFrontmatter`; every inputs-refusal sentence restated; + skill.go)
- 45: late check (2026-09-27, 28cd8ee5) — recast (`set -f` scoped to word-splitting sites; name lists read with `while IFS= read -r`; non-regular scope lines dropped; `*` test is a pin; `x*y.go` skipped on Windows)
- 42: re-check (2026-09-27, 28cd8ee5) — decision folded: amends ADR 0012 by a dated `## Amendment (2026-09-27)` beside its 2026-09-14 amendment (a) (supersedes "Nothing else loosens" for the confined recipe script stage only); + ADR 0012 in Files and Acceptance; recorded in Ratified design calls
- 43: re-check (2026-09-27, 28cd8ee5) — guard folded (subAgentSpan answers for a workflow-run head, arrival-order test; workflow block span never elided, yields to layout.md:1211; fan_out card born collapsed keeps its item runs painted; `observeWorkflow` signature kept; + workflowcall_test.go)
- 45: re-check (2026-09-27, 28cd8ee5) — guard folded (`git -c core.quotePath=false ls-files -z`; Goal restated as the tested invariants; `q?.go` fixture; one Windows skip rule; one git-listed case)
- 46–49: added (2026-09-28, 1fb91ac0) from the run's ledger findings (workflow item spend reaches /usage and the record; `make lint`'s ineffassign in commandrun_test.go; a delegate is offered neither `background` nor `workflow`; `/workflows` answers a waiting question). 46 and 47 are regression fixes (from 62d76268 and ad3fee2b), never deferred. 49's `^a` route, idle only, is the plan writer's reading of the Background question call, ratified by the owner (2026-09-28; see Ratified design calls). Regression-checked (2026-09-28, 1fb91ac0) by independent reviewers: guards folded on 46, 48 and 49, 47 scoped by decision — one line per item below
- 46: late check (2026-09-28, 1fb91ac0) — guard folded (live background views rebased at every session boundary, yields to commandrun.go:376-387 and sessions.md:108-110; the workflow's name persisted through `Tool` on the wire entry; a claimed UsageEvent's `ServedModel` reaches `m.servedModels`; doc rule by grep; + commandrun.go, sessions.go, transcriptbridge_test.go); needs item 43's head lookup `headsWorkflowRuns` (absent at 1fb91ac0, already a declared dependency)
- 47: late check (2026-09-28, 1fb91ac0) — decision folded: Goal and Acceptance scoped to golangci-lint over `./internal/tui/...` plus the named test; a lint finding in another package is a FOLLOW-UP of the item that introduced it
- 48: late check (2026-09-28, 1fb91ac0) — guard folded (fan_out cases set `MaxDepth = 2` and assert fan_out is on the child roster first; comment rule by grep over internal/agent; + loop.go); 41 and 48 both restate toolMenu's doc comment in loop.go — whichever lands second merges it
- 49: late check (2026-09-28, 1fb91ac0) — guard folded (`^a` gated on canWake's conditions with the /workflows pane taken as closed, errored-state note test; + layout.md, whose "until the next exchange ends" is restated)

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

## 10. Runner: recipe-only stages — ✅ DONE (2026-09-27)

NOTES (2026-09-27): runner.go is edited as well, though the item does not list it. Run and expandStages refused every recipe-only kind and fanouts over a pick until now. The changes: `Runner` gains `Scripts ScriptRunner` and `Asker Asker`. Run refuses a plan that has a script stage when no ScriptRunner is set. expandStages skips everything except a fanout with its own source. `runFanout` and `stageKeyBrief` take the repeat round. `StageResult` gains `Note` and `Round`. `notifyStage` is factored out of setStagePhase.

NOTES (2026-09-27): condition subjects. Outside a verify, every field in a `when:` names its stage as `<stage>.<field>`: `split.parts`, `scope.answer`, `find.blocked`. The stage must be a script, ask or merge stage (fields status, summary and its returns; for ask, `answer`, typed as an enum of its options) or a fanout (tally fields ok, partial and blocked, as ints). A repeat's condition may leave the stage off for a field of the stage it repeats (`ok < 3`). Validate (stageConditionError) refuses an unqualified field, a fanout's per-item field, a verify, pick or repeat stage as the subject, and a later stage. At run time, a skipped or unfinished stage contributes no fields, so a term that reads it is false.

NOTES (2026-09-27): skip rules. A false `when:` skips a fanout, merge, pick, script or ask stage (PhaseSkipped, Note `skipped: <when> is false`). A stage whose source stage was skipped is skipped as well (Note `skipped: stage X was skipped`). The source is a verify's or merge's fanout, a pick's `from`, a fanout's pick, or a repeat's target. This keeps a merge from running a child over an empty manifest.

NOTES (2026-09-27): pick. `file:` is a path inside the workflow folder, where split.sh writes its output (item 23), and Validate refuses `..` and absolute paths. `field:` unions the list field across the source stage's finished items in item order, each entry once. Cap, then batch. If the file cannot be read, the pick fails (PhaseFailed, with a Note) and yields no items. The fanout over it then runs zero items and the workflow goes on. The Note gives the counts.

NOTES (2026-09-27): script. `ScriptRunner.RunScript(ctx, ScriptSpec{Workflow, Stage, Command, Dir=workflow folder}) (ScriptOutput{Stdout, ExitCode}, error)`. Keys are lower-cased and only declared `returns:` keys are read. An int is parsed, a list gains one entry per line, text and enum values are kept as written, and `summary=` sets the summary. Exit 0 gives ok, and anything else gives blocked. An ok receipt that fails ReceiptSpec.Check becomes blocked, and the summary names the problem. A RunScript error gives a blocked receipt. A blocked script fails the stage and the workflow goes on. A script or ask stage has one ItemResult (labelled with the stage name), and its receipt is inline in status.json on an ItemStatus with no key. Neither is skipped on resume; both run fresh.

NOTES (2026-09-27): ask. `Asker.Ask(ctx, Question{Workflow, Stage, Text, Options, Default}) (string, error)`. The default is taken, and the Note says why, when there is no Asker (`(default taken: no one to ask)`), when the answer is empty, when the answer is not one of the options, or when Ask returns an error.

NOTES (2026-09-27): repeat. Before each round, the condition is read against the latest results. The target is re-run with round n. Its StageResult in Result.Stages and its status.json line are replaced by the new round's (Round=n). The repeat's Note gives the number of rounds and whether the condition still held at max. The round goes into the item key through stageKeyBrief. It is omitted at round 0, so a stage's own run keeps the key it had before this item.

NOTES (2026-09-27): store.go's StageStatus gains `note` and `round`. The ItemStatus and ItemKey doc comments now describe the keyless script and ask line and the round-carrying brief.

NOTES (2026-09-27): consequential edit — internal/workflow/runner_test.go: made necessary by script stages now running; the refusal case is relabelled "script with no ScriptRunner" and still refuses.

NOTES (2026-09-27): consequential edit — internal/workflow/plan.go: made necessary by the condition-subject rule and pick's folder-local file; the Stage.When and Stage.File doc comments now say so.

NOTES (2026-09-27): consequential edit — internal/workflow/doc.go: made necessary by stages.go now carrying every kind beyond the fanout; its map line says so.

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

## 11. Result lines and the report — ✅ DONE (2026-09-27)

NOTES (2026-09-27): `Store.WriteItems(id, Result)` returns `(string, error)`, where the string is items.md's absolute path. `Result` gains `Listing`, which is that path. `Run` sets it after `setWorkflowPhase` on both a done run and a stopped one. Format stays pure: it prints `items:` only when there are more than 40 items and `Listing` is set, and `report:` only when `Result.Report` is set. The merge sets `Report` only once it has found report.md on disk.

NOTES (2026-09-27): shape details the item left open. Items are numbered from 1 within each fanout stage (matching ItemEvent.Index). When a Result has more than one fanout, each fanout gets a `<stage>:` header and its own totals line. The 40-item cap counts every fanout's items together. An unfinished item reads `— stopped|pending — no receipt`. Typed fields are listed in key order. A list is joined with commas. A text value is quoted when it is empty or contains a space or `=`. A float64 read back from JSON prints as written.

NOTES (2026-09-27): additions to the ratified shape, all only when non-zero or set. Each item line ends with ` verdict=<v>`. The totals line gains `· unfinished U`, `· resumed R`, and `· confirmed X · refuted Y · unclear Z` (the verdict tallies are the Goal's "verdict tallies when verified", placed on the same totals line).

NOTES (2026-09-27): each note line reads `<kind> <stage>: …`, e.g. `merge report: no report — <ReportMissing>`, `ask scope: ok — took the default no answer=no (default taken: no one to ask)`, `fanout deep: skipped: …`. The kind comes first so that a stage named `report` or `items` cannot be mistaken for the `report:` or `items:` line. A stopped Result starts with `stopped by the user: K of N done`. K and N count only the fanout items of stages that ran.

NOTES (2026-09-27): items.md starts with `# workflow <id>`. Under it, each fanout has a `## <stage>` heading, then every item line followed by `   output: <path>`, then its totals. The note lines and `report:` come after the last fanout.

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

## 12. The finish tool and the workflow child spawner — ✅ DONE (2026-09-27)

NOTES (2026-09-27): re-derived from the assumption that the workflow-child flag and the finish block fit in the listed files: the flag (`Agent.workflowItem`) is a field of the Agent struct, declared in internal/agent/agent.go, and the delegate report block is rendered by `delegateReportBlock` in internal/agent/delegatereport.go, where the finish block now takes its place. The finish block reuses the delegate block's first sentence, so the existing `delegateReportFence` still guards it and standingblocks.go is unchanged.

NOTES (2026-09-27): a workflow child's `outputPath`/`outputTarget` stay empty (no `resolveOutputPath`). Otherwise `wrapUpOutput` would make the wrap-up row in resolve refuse the closing Turn's `finish` call. The spawner names the output file in the task instead, adding a line when the rendered brief does not already contain the path.

NOTES (2026-09-27): a stage's `prompt:` file is read from the spawner's `prompts fs.FS`, or from `os.DirFS(Config.WorkspaceDir)` when that is nil. It is rendered with the `{item}`/`{out}` placeholders copied locally from workflow's unexported `renderBrief`, because internal/workflow is outside this item's files. Callers in items 15, 20 and 25 pass the recipe's own FS where they need one.

NOTES (2026-09-27): a stage's `tools:` list is checked through `requestedChildTools`, so an unknown name makes Spawn fail as a fault. `context:` files reach the child as `UserInput.FileRefs`. A continuation (`ItemSpec.Prior`) is seeded through `continuationTask`, each round's report shown as an engine summary headed by the receipt that round left.

NOTES (2026-09-27): a faulted item child still pays for `finishAtFault`'s engine summary call, which exists for retention. Workflow children are never retained, so a later item could skip that call for them.

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

## 13. Workflow configuration keys — ✅ DONE (2026-09-27)

NOTES (2026-09-27): the regression guard asks for a validator on `workflow-wake`, but TestRegistryValidateHooksSitOnEditableKeys refuses a Validate hook on a non-editable row — the row lands through checkedField(ParseWorkflowWake), so the kind's on|off vocabulary and ParseWorkflowWake are its refusal at the file pass (`true` is refused there), and TestRegistryEnumValuesMatchParseSites gains a workflow-wake subtest against ParseWorkflowWake.

NOTES (2026-09-27): the two counts take the delegate-* file posture (intField + atLeast(0)): a negative count resolves to the default rather than being refused, because TestRegistrySetRefusesWhatValidateRefuses forbids a Set refusal no Validate hook or kind check makes, and a non-editable row may carry no hook; the manual says so.

NOTES (2026-09-27): domain.WorkflowConfig holds three pointers plus ResolvedRetries/ResolvedContinuations/ResolvedWake (nil = 1/2/on) and exports DefaultWorkflowRetries/DefaultWorkflowContinuations, which the registry rows take their Default from, so host and engine defaults cannot drift.

NOTES (2026-09-27): consequential edit — internal/config/keyfield.go: made necessary by workflow-wake joining the rows whose file pass lands through the row's Set (the doc comment enumerated them).

NOTES (2026-09-27): consequential edit — internal/config/config_test.go: made necessary by the new Options/fileConfig fields (wantDefaults, TestEveryConfigKeyReachesTheOptions, everyKeyFileConfig, TestResolvePrecedence, the count-fraction table, TestFilePassRefusesThroughTheRows) plus the new TestApplyConfigWorkflowKeys; gofmt realigned the neighbouring lines of those literals.

NOTES (2026-09-27): consequential edit — internal/config/registry_test.go: made necessary by the new rows (TestRegistrySetIsTheInverseOfRead ownership map, enum parse-site subtest).

NOTES (2026-09-27): consequential edit — internal/config/keyfield_test.go: made necessary by workflow-wake refusing at the file pass (added to the refused-file list).

NOTES (2026-09-27): consequential edit — cmd/apogee/settingsrows_test.go: made necessary by TestSettingsRowsFormatEffectiveValues pinning one value per registry key.

NOTES (2026-09-27): consequential edit — cmd/apogee/wire_config_test.go: made necessary by the new Config.Workflow fold (projectionOptions sets the keys, assertCarriesProjection compares them, TestProjectConfigFoldsTheWorkflowKeys pins a stated 0/off surviving the fold).

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

## 14. The fan_out tool schema — ✅ DONE (2026-09-27)

NOTES (2026-09-27): internal/tools/registry_test.go (listed in Files) needed no change: its menu-order and count pins read the default menu, which fan_out (default-off) does not reach, and KnownToolNames/TestKnownToolNamesCoversTheComposedSet derive from the build list with no hand-kept name list to update. The TestDefaultToolsHonourTheRoster default-off repin lives in roster_test.go, as the Regression guard says.

NOTES (2026-09-27): the `background` gate reads the roster ladder's verdict for the name "workflow" through a new RosterDeltas.lifts helper; EffectiveRoster's per-name verdict loop was moved into rosterVerdicts so both read one ladder. The name is an unexported workflowToolName constant in fan_out.go until item 27 registers the tool.

NOTES (2026-09-27): fan_out declares `task`, `verify` and `merge` as delegation prompts (ArgRolePrompt); the path-bearing `over`, `out`, `context` and the recipe `inputs` stay fully inspected. No top-level `required`: `task`/`over` are needed unless `recipe` is set, which the descriptions say and item 15's ValidateModelPlan refusal enforces.

NOTES (2026-09-27): consequential edit — internal/tui/toolregistry.go: made necessary by registering fan_out; TestToolRegistryCoversEveryBuiltInTool walks KnownToolNames and fails on a built-in with no toolRegistry row, so fan_out gets a minimal plain row (task first line as target, first-line detail, solo).

NOTES (2026-09-27): consequential edit — internal/tools/registry.go: the HostTools.SubAgentSeatChoice and HostToolsOf doc comments now say the gate shapes fan_out's schema too.

NOTES (2026-09-27): consequential edit — internal/tools/doc.go: "Thirty-three files carry the built-ins" -> "Thirty-four" for fan_out.go. The count was already approximate before this item: item 12 added finish.go without moving it.

NOTES (2026-09-27): README.md's "34 built-in tools" is now one short. Left alone because item 38 (not yet done) owns the README counts and repins them to 36 after items 14 and 27.

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

## 15. fan_out runs a blocking workflow — ✅ DONE (2026-09-27)

NOTES (2026-09-27): comment lines edited by items 1, 2 and 10 were not re-wrapped and run past 120 columns: internal/agent/dispatch.go (the doc comments above dispatchGroup and executeTool), internal/agent/turn.go (the `observer` field doc and the comment above turnLifecycle.settle), internal/agent/loop.go (the rollback comment in armRequest) and internal/workflow/store.go (the ItemKey doc). Re-wrap them to the file's width when next touched; this item touches dispatch.go.

NOTES (2026-09-27): fan_out resolves to a new Resolution kind, `resolveWorkflow`, on the sub_agent row of `resolve` (same depth-bound refusal), rather than reusing `resolveDelegate`. It stays in the leaf group (partitionDispatch unchanged), so it never takes a pool slot and the fan-out ceiling never counts it. A cancelled fan_out therefore settles through settleCancelledLeaf with its own stopped answer, and the calls after it are answered not-run. runWorkflowCall books its audit record itself, as a leaf arm does.

NOTES (2026-09-27): consequential edit — internal/agent/gate.go: made necessary by resolveWorkflow; a gate reaction's `ask` is deferred for a Workflow as for a delegation. Otherwise the ask would force a Gate verdict and run the fan_out placeholder's Execute.

NOTES (2026-09-27): internal/agent/loop.go (not listed): the working-window rule inside `budget()` is extracted into `workingLimit(domain.ContextConfig)`, so the split budget applies the same rule to a Delegation target's binding (`target.binding().applyTo(cfg)`). With no target latched it reads `budget().ContextLimit`.

NOTES (2026-09-27): internal/run/run_test.go (not listed): added TestEventTapBracketsEachFanOutItemByItsPhases for the run.go change. The tap notes a fan_out ToolCallEvent by (depth, call id). Each item child's started phase under that call opens a bracket under the child's run id, and its finished phase files it. No tool result answers one item child.

NOTES (2026-09-27): a stopped item child is not folded: the Runner discards a stopped Outcome's report and restarts the item fresh on resume. So the only folds under a fan_out cancel are those of the item children's own delegations, which foldStoppedChild holds to the inherited cancelFoldBound and skips on domain.ErrShuttingDown. The two fold-guard tests exercise exactly that, with delegate-max-depth 2 and an item child that delegates. A shutdown cause makes no fold request. An esc×2 cancel makes one fold request, cut at the injected 20 ms bound.

NOTES (2026-09-27): the report-so-far path is items.md (Result.Listing). A stopped answer ends on `items: <path>` when Format has not already listed it, because Format prints `items:` only past 40 items.

NOTES (2026-09-27): Runner.Width is `a.delegationWidth()`. The Runner's own semaphore never runs more children than a stage has items, so the width in effect is min(delegationWidth, N). fanOutWidthFor is not used.

NOTES (2026-09-27): a fan_out call with `recipe` set is refused with a fixable tool error until item 21 wires recipes. `run_on` and `background` are parsed by nothing yet (see DEFER; background belongs to item 27).

NOTES (2026-09-27): the dispatch.go comment lines past 120 columns that item 15's earlier NOTES named are re-wrapped: the partition paragraph above dispatchTools, the dispatchGroup doc and the executeTool doc.

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

## 16. The fan-out ceiling points to fan_out — ✅ DONE (2026-09-27)

NOTES (2026-09-27): the pointer is decided per refusal from the refusing Agent's own roster (`lookupTool(tools.FanOutToolName)` in refusePastCeiling), so a delegate whose roster lacks fan_out gets the unchanged text; `fanOutCeilingResult` gained a `fanOutOffered bool` parameter and the suffix is the new constant `fanOutCeilingFanOutPointer`.

NOTES (2026-09-27): test helper `manyFanOutParentOffering` added (manyFanOutParent now delegates to it) so the new pin can put fan_out on the roster; the no-fan_out text stays pinned by TestFanOut_CeilingRefusesTheCallsPastIt and TestDispatchSerially_CeilingAppliesAtDepthOne, the fan_out text by the new TestFanOut_CeilingRefusalPointsToFanOut.

NOTES (2026-09-27): consequential edit — CONTEXT.md: made necessary by the refusal's new conditional suffix (the Fan-out ceiling entry quotes the refusal text verbatim).

NOTES (2026-09-27): consequential edit — docs/manual/configuration.md: made necessary by the refusal's new conditional suffix (the delegate-fanout-rounds paragraph describes what the refusal says).

**What:** Depends on item 15.
**Goal:** while `fan_out` is on the roster, the ceiling refusal for `sub_agent` ends with `— for more items, use fan_out`; without it the refusal text is unchanged.
**Approach (assumed at the header base):** `fanOutCeilingResultFormat` in dispatch.go gains the suffix conditionally.
**Files:** internal/agent/dispatch.go, internal/agent/fanout_test.go
**Read first:** internal/agent/dispatch.go — refusePastCeiling, fanOutCeilingResult, fanOutCeilingResultFormat; internal/agent/fanout_test.go — the two exact-refusal pins (want consts);
  cmd/apogee/e2e_fanout_test.go — fanOutCeilingRefusal; internal/tui/transcript.go — unstartedDelegationPrefix
**Tests:** both refusal texts pinned exactly.
**Acceptance:** `go test -race -count=1 ./internal/agent/`
**Commit:** `feat(agent): the fan-out ceiling names fan_out when it is enabled`

## 17. Workflow events — ✅ DONE (2026-09-27)

NOTES (2026-09-27): the Runner's Observer has no workflow-level start or end call, so the agent-side adapter (`workflowObserver` in workflowcall.go) emits `started` on the first notification naming the workflow's id and the end phase from the Result/error `Runner.Run` returns; stage_started is a stage's `running` notification, item_finished an item's `done` notification with a receipt (a resumed item included, `Resumed` set).

NOTES (2026-09-27): added a `failed` phase (with `Detail` = the cause) beside finished/stopped so every workflow that starts ends on exactly one end phase even when `Runner.Run` returns a store error mid-run; the goal's list names finished/stopped only.

NOTES (2026-09-27): `waiting` is emitted by an Asker wrapper `observeWorkflow` installs when the Runner has an Asker (Detail = the question); the fan_out path runs with no Asker, so it is live only once a later item (20/25) supplies one — pinned by a unit test on the observer.

NOTES (2026-09-27): per the item's regression guard ("workflow converts to it"), the conversion to `domain.WorkflowReceipt` is `workflow.Receipt.Domain()` in internal/workflow/format.go (reusing its field renderer; a text field verbatim), a path the item's Files did not list; pinned by TestReceiptDomain.

NOTES (2026-09-27): internal/tui/fold.go needed no change — the event is inert in the view (its fold_test row says so; the workflow block is item 22's), so it is not in FILES.

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

## 18. Recipes parse from a skill header — ✅ DONE (2026-09-27)

NOTES (2026-09-27): prompt paths are normalised at parse (a leading `{{SKILL_DIR}}/` stripped, the path cleaned, and an absolute path, a `..` climb, a non-leading token or the folder itself refused) and resolved in loadSkillFile after `sk.Dir` through the same `src.dirFor` seam — so both spellings (folder-relative and `{{SKILL_DIR}}`-led) land on the Dir-rooted address (host path, or `shipped:<id>/…`), not only the token-led one.

NOTES (2026-09-27): beyond workflow.Validate, a recipe stage, its `over:` mapping and an input entry refuse unknown keys (key sets read off the workflow types' yaml tags) so a misspelt `promt:` is a load error; `recipe:`/`inputs:` are held as yaml.Node and decoded separately so a type error names the recipe instead of sending the block to the lenient scan.

NOTES (2026-09-27): internal/workflow/inputs.go also carries `ValidateInputs` (name present, one `[A-Za-z][A-Za-z0-9_-]*` token, unique) beside `InputDecl`, tested through the skills parse tests; `inputs:` is accepted without a `recipe:` (no binding site exists before item 19/20).

NOTES (2026-09-27): loadShipped's dirFor closure became the named `shippedDirFor` so the shipped `shipped:<id>` case is tested through walkSkills over an fstest.MapFS — no shipped recipe skill exists until item 23, so Load itself cannot reach one yet.

NOTES (2026-09-27): consequential edit — internal/skills/doc.go: made necessary by the recipe exception to the parse paragraph's "only a hard YAML failure falls through to the scan" rule

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

## 19. Recipe inputs bind from the user's text — ✅ DONE (2026-09-27)

NOTES (2026-09-27): the tokenizer is written in internal/workflow rather than reusing refs.ScanToken — the package may import only the standard library and internal/domain (ADR 0087 D10, enforced by validate_test.go); a quote may open anywhere in a token (so `focus="a b"` is one keyed token), and a token is keyed only when an input-name-shaped word precedes the first unquoted `=` (so a URL or a quoted `"a=b"` stays positional).

NOTES (2026-09-27): beyond the item text — an unterminated quote is an error naming the token (ScanToken's lenient run-to-end-of-line is not copied, since a silent mis-bind would start a workflow on the wrong values); an empty value (`scope=`, a bare `""`) binds nothing and falls to the default, a positional `""` still taking its slot; the returned map holds every declared input except the missing ones, an optional input with no value or default binding ""; every problem is reported in one error.

NOTES (2026-09-27): consequential edit — internal/workflow/doc.go: made necessary by BindInputs joining inputs.go (the file-map line names it)

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

## 20. The engine starts a recipe — ✅ DONE (2026-09-27)

NOTES (2026-09-27): the recipe port is `workflow.RecipeSource` (new internal/workflow/recipe.go, with the `workflow.Recipe` value: Plan, Inputs, Dir, Files), and the agent reads it off `Config.Skills` by type assertion instead of taking a new agent Option or Config field. domain cannot name a workflow type, and an Option would have to be threaded through apogee.New/Resume and cmd/apogee/wire_engine.go. `*skills.Provider` (already wired as Skills in wire_config.go, which only gains a comment) implements it, so every Driver gets recipes with no new wiring. The loop still never imports internal/skills.

NOTES (2026-09-27): consequential edit — internal/workflow/doc.go: made necessary by recipe.go joining the package (the file map names it)

NOTES (2026-09-27): `runRecipe` takes `(ctx, turn, call, id, inputs)`, not `(ctx, id, inputs)`: turn and call stamp the item children's phase events. Item 21 passes its fan_out call, and the launch passes a synthetic `recipe` call that is used only for events and is never put in history. runRecipe fills defaults, refuses an unknown key and fails a missing required input as `missing input: <name>`.

NOTES (2026-09-27): `StartRecipe(ctx, RecipeLaunch) (string, error)` binds and asks, then submits `UserInput{Text: "/<id> <text>", SkillIDs: [id], RecipeInputs: bound}`. The caller then drives the Exchange with Step/Run. The id it returns is for a background workflow, and `Background: true` is refused (`errBackgroundRecipe`) until item 25 exists. `domain.UserInput` gains `RecipeInputs map[string]string` (json omitempty) rather than a `Recipe` field.

NOTES (2026-09-27): inputs reach the stages as `{<name>}` placeholders. They are filled in task, question, default, out, pick file, context and item sources as written, and in `run:` shell-quoted; the names `item` and `out` are never bound. Binding happens before Runner.Run, so a run with other inputs gets another plan hash and another folder. A script's `run:` also renders `{{SKILL_DIR}}` (the host folder, or, for a `shipped:` skill, `<workflow>/skill/` with every `{{SKILL_DIR}}/<path>` it names copied there first), `{workflow_dir}`, and `{part_bytes}` (the split budget in bytes). Item 23's split.sh should take its window from `{part_bytes}` on its `run:` line, since there is no PART_LINES environment variable.

NOTES (2026-09-27): a launch whose inputs cannot bind or whose run fails emits an ErrorEvent (Source `recipe`), and the opening message carries `recipe /<id> could not run: <error>` where the result lines would go. The model still answers.

NOTES (2026-09-27): a Plan-mode test script stage is refused because runScriptCall puts it through the terminal's own Resolution, and Plan refuses every ClassSubprocess call. So Plan refuses all script stages, not only those that write outside scratch.

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

## 21. fan_out starts a recipe — ✅ DONE (2026-09-27)

NOTES (2026-09-27): the calling Agent's menu reaches load_skill through a new `domain.WithFanOutOffered` / `domain.FanOutOffered` context pair (internal/domain/config.go). `Agent.runTool` sets it on every leaf call from a new `offersTool` helper, which checks that the tool is registered and not hidden by Plan mode, mirroring toolMenu's filter. Unlike WithPromptSlot it always overrides the value already on the context. No `tools.HostTools` field was added, so the HostTools every-field check was not touched.

NOTES (2026-09-27): `domain.ResolvedSkill` gains `Recipe bool`, which only `LookupSkill` sets (internal/skills/lookup.go). The loop's attach and `ResolveSkills` ignore it.

NOTES (2026-09-27): the recipe form is read by a new `parseFanOutRecipe` over the raw argument map. A fan-out field counts as set unless its value is null, "", [], {}, 0 or false, so a small model that fills every field with an empty value is not refused. The `Recipe` field and the old `fanOutRecipeUnavailable` refusal are removed from fanOutArgs/parseFanOutPlan. The unknown-recipe answer lists ids through `workflow.RecipeSource.RecipeIDs` in workflowcall.go rather than reusing recipeByID's Driver-facing `apogee:`-prefixed error.

NOTES (2026-09-27): runRecipe errors (missing/unknown input, no scratch dir or workspace, a Runner failure) answer as `fan_out could not run recipe <id>: <error>`.

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

## 22. Invoking a recipe skill in the TUI runs it — ✅ DONE (2026-09-27)

NOTES (2026-09-27): re-derived from "submit sets `UserInput.Recipe`". Item 20 added no such field: the engine launches on `SkillIDs[0]` plus a text that opens with `/<id>`. So `parsedInput` gains `recipe`, set after the parse by `parsedInput.withRecipe(Model.recipeSkill)`, and the recipe id is taken out of `skillIDs`. `parsedInput.userInput()` then puts it back as `SkillIDs[0]` of the UserInput that submit sends. `parseInput(raw, known)` keeps its signature. `Model.submitLine()` is the one parse that submit, stageInterjection and stageChildMessage share.

NOTES (2026-09-27): re-derived from "the fold row is internal/tui/fold.go". Every Event reaches the transcript through `transcript.apply` (transcript.go), so the WorkflowPhaseEvent case lives there and fold.go is unchanged. The `renderEntryLines` case is in render.go. The entry gains a view-only `workflow workflowView` field. fold_test.go's WorkflowPhaseEvent row is restated, and a second row covers a started phase opening a block.

NOTES (2026-09-27): the block's text is its whole paint and its whole record, so a resumed session paints it with no view to rebuild. Because the text changes after the entry is committed, `entryWorkflow` is not cacheable (like the start-up box). It is persisted as `session.EntryKindWorkflow` = "workflow" and is not a host note. A Workflow started while a fan_out call in the same run is still open draws no block.

NOTES (2026-09-27): a recipe line with held rows is refused in submit instead of being merged: joinedInterjections is unchanged, and staging already refuses recipe lines, so a held row can never be one. `refs.LeadingSkill` is new in refs.go and tested in refs_test.go.

NOTES (2026-09-27): the ask-pane goal is proven end to end by TestE2ERecipeMissingInputOpensTheAskPane: a real Agent with a `skills.Load` catalog, the TUI Bridge's Asker, and a stubllm upstream that answers only a request carrying `recipe /review-tree could not run: missing input: scope`. No wiring change was needed, because cmd/apogee already sets `Config.Asker` to the bridge.

NOTES (2026-09-27): consequential edit — layout.md: made necessary by the new workflow block and the recipe-line refusals (the TUI rendering spec describes each block kind, next to the firing block).

NOTES (2026-09-27): a recipe launch's item children emit events under the synthetic `recipe-<id>-<turn>` call, which has no head block in the transcript. Their entries therefore land as unheaded depth-1 entries after the workflow block, not nested under it. Giving the block run-head behaviour would change the run-grouping walk, which is outside this item's detect-and-render scope.

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

## 23. The shipped audit recipe: skeleton and split — ✅ DONE (2026-09-27)

NOTES (2026-09-27): the concurrency lens is gated by a `flags` script stage (`split.sh --flags` reads the CONCURRENCY line ground-truth writes into bundle.md). A `when:` reads only a fanout's tally, never an item's receipt field, so the ground-truth flag cannot be read off the fanout directly.

NOTES (2026-09-27): a brief renders only {item} and {out}, so the workflow folder reaches the children through item labels. split.sh writes absolute item lists (run-dir.txt, parts.txt, conc-parts.txt, groups.txt) for pick stages, and a folder per part and per group (part-<name>/scope.txt and tests.txt; group-<name>/parts.txt and cap.txt). Stage outputs sit beside those folders through `out: "{item}/…"`: bundle.md and tools.md at the top, findings-<lens>.md per part, merged.md and claims.md per group.

NOTES (2026-09-27): an ask answer is not a placeholder, so split.sh echoes the focus input back as `focus=` (`none` when it is absent or not a focus area). The focus ask runs only `when: split.focus == none`, and each lens's `when:` reads split.focus or focus.answer.

NOTES (2026-09-27): stages beyond the item's list. machine-checks sits beside ground-truth (the Phase 1 pair). An `enumerate` fanout per group always runs (on a one-group scope it is the single enumerator), while rollup runs only `when: split.parts > 1`. `verify` is a fanout over the picked claims returning `verdict`, not the engine's verify kind, because the claims are the items, not an earlier fanout's receipts. The report is a merge from verify.

NOTES (2026-09-27): split.sh port changes. A scope that fits one part is one part, `all`. The small-part merge is refused when it would push a part over a bound. A test file matched by no part goes to the root part, or else the first part. PART_LINES comes from the env, or else from the {part_bytes} argument (half the budget at 40 bytes a line, clamped to 200..8000; 8000 when the budget is unknown). A changed scope or bound re-splits.

NOTES (2026-09-27): the recipe names prompts/*.md files that item 24 writes. Load does not check prompt paths, so the recipe loads now, but a run before item 24 lands blocks at its first child stage. The SKILL.md body names only `{{SKILL_DIR}}/split.sh` (readable), not the prompts folder, so TestShippedSkillAnnouncesOnlyReadableAddresses stays green.

NOTES (2026-09-27): consequential edit — internal/agent/skillmount_test.go: made necessary by the item's Tests line (the audit body must pass TestShippedSkillAnnouncesOnlyReadableAddresses); the test now loops over debugging and audit.

NOTES (2026-09-27): improvement idea — a re-run with the same scope and inputs reuses the same part-folder paths. ItemKey hashes a unit's path, not the contents of the files it lists, so lens receipts resume even after the audited code changed. A content-derived part name or a scope hash in the plan would force fresh work.

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

## 24. The audit recipe's prompts — ✅ DONE (2026-09-27)

NOTES (2026-09-27): a brief renders only {item} and {out} and cannot include another file, and a prompt body gets no {{SKILL_DIR}} expansion, so a lens child could not reach `_shared.md` on its own. `_shared.md` is the canonical copy of the shared lens rules and every lens-*.md carries it verbatim at its end (so the brief closes on the finish section); the new test fails when a lens copy drifts from `_shared.md`.

NOTES (2026-09-27): `_report-format.md` was folded into report.md (its only reader), and the source's all-lenses.md (small-scope single agent) and chat-summary OUT file were not ported: the recipe always fans the lenses, and the engine's result lines plus `report:` path replace the chat summary.

NOTES (2026-09-27): children find the workflow folder by their item: a lens's {item} is a part folder (bundle.md and tools.md one folder up), rollup and enumerate take a group folder, ground-truth and machine-checks the workflow folder, the report the folder {out} is in, and verify derives it from {out} (`<workflow folder>/items/<key>/output.md`), since its {item} is the claim line itself. Enumerate hands claims back as `<severity> | <file:line> | <claim> | <source ids>` in the `claims` list field and writes the same lines to claims.md.

NOTES (2026-09-27): the step budget is restated for apogee: aim to be done at about half the child step limit (80 steps by default, delegate-max-steps), with the draft of {out} due after the third scope file or the tenth step; the capped wrap-up turn allows only finish, so the write-first rule stands.

NOTES (2026-09-27): SKILL.md gains one body paragraph naming `{{SKILL_DIR}}/prompts/_shared.md` and the finish hand-back; TestShippedSkillAnnouncesOnlyReadableAddresses reads the new address through the shipped mount and passes. The recipe's stages and `returns:` are unchanged from item 23.

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

## 25. Background workflow manager — ✅ DONE (2026-09-27)

NOTES (2026-09-27): resume re-asks. Item 10 (7235dbbd) skips only finished fan-out items on resume: `prepareItems` (runner.go) reads each item's stored receipt through `Store.ReadReceipt`, but `openStatus` resets every stage to pending and `runRound` sends script and ask stages to `runScript` and `runAsk` (stages.go), which look up nothing stored and run fresh. A resumed recipe therefore re-runs its scripts and asks the user its questions again. Weigh this here: either persist ask answers and script results and replay them on resume, or accept the re-ask and document it.

NOTES (2026-09-27): re-derived from the assumption that the background approval queue fits in background.go. Every approval goes through `queuedApprover.Approve` (internal/agent/construct.go), so the background branch sits there. It keys on a context value that background.go sets (`withBackgroundPrompts`), and the allow-for-session memory still applies to it.

NOTES (2026-09-27): re-derived from item 20's NOTES, which leave `StartRecipe{Background: true}` to item 25. internal/agent/recipe.go now sends a background launch to the manager, and `errBackgroundRecipe` is gone. The background case in internal/agent/recipe_test.go's refusal table is now the `missing input: scope` refusal. A background launch binds inputs from the text alone and refuses a missing required input instead of asking (item 29's guard).

NOTES (2026-09-27): a snapshot `workflows` entry is `{"id", "recipe"}`, identifiers only. The recipe skill id is needed on resume to find the recipe's prompt files and script staging again, and the plan is read back from the folder's plan.json. The decode check (`checkRestoredWorkflows`, called from `checkRestoredStructure`) is shape-only: one folder name in the store's alphabet, no repeats, and a recipe id that is a single name, otherwise `ErrSnapshotRefused`. decodeState does not know the scratch dir, so `ResumeWorkflows` resolves the folder under the live `<scratch>/workflows/` through `Store.Dir`/`ReadPlan`.

NOTES (2026-09-27): the workflow folder is opened when the workflow is launched, not when it starts (`openWorkflowFolder` repeats Runner.Run's expand → PlanHash → Find-or-Create). That way a queued workflow already has the id and status path it is listed and stopped by, and the Run that follows reopens the same folder. The snapshot round-trip test pins that it is the same folder.

NOTES (2026-09-27): per the DECISION, resume re-asks: script stages run again and ask stages ask again, and neither is persisted or replayed. This is documented in the background.go file comment and in `ResumeWorkflows`'s doc comment.

NOTES (2026-09-27): the queue of background approvals and asks has only unexported accessors so far (`waiting`, `answer`). Item 30 adds the Driver-facing calls. Until then, a background gate or ask waits until its workflow is stopped. `StopWorkflow` returns without waiting for the stop to finish; `Close` (top-level Agent only) and `RestoreSession` do wait. `WorkflowInfo` has no alias in apogee.go, which is outside this item's files.

NOTES (2026-09-27): "sharing the slot on a width-1 server" needs no code of its own. The background width is `max(delegationCap−1, 1)`, and the server queues the conversation's requests and the children's requests on its one slot.

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

## 26. Finish notes and the wake — ✅ DONE (2026-09-27)

NOTES (2026-09-27): engine API. `Agent.Wake(ctx) (bool, error)` queues the held notes as the opening message of a new Exchange, which the Driver then Steps the same way it Steps a Submitted message. It reports whether it opened an Exchange. It opens nothing and keeps the notes held in these cases: ctx is done (returns ctx's error); `workflow-wake: off` is set (Wake checks the config itself, so every Driver gets the setting); the Agent is a delegate; no note is held; an Exchange is running or input is queued. With no model bound it returns errNoModelBound, as Submit does. `Agent.TakeWorkflowNotes() (domain.UserInput, bool)` in interject.go is the call item 28 plumbs beside Wake. It hands over the held notes as one interjection only while an Exchange is open, and takes nothing otherwise, so an idle Driver cannot lose a note to Interject's ErrNoOpenExchange.

NOTES (2026-09-27): note shape. The line is `workflow <name> finished|stopped — items N · ok A · partial B · blocked C[ · unfinished U][ · confirmed x · refuted y · unclear z] — report: <path>`. When the workflow wrote no report, the last part is `items: <items.md>` instead. A run that could not proceed gives `workflow <name> failed — <cause>`. Counts are summed across the fan-out stages that were not skipped. The name and the cause are folded onto one line. The note is always recorded text, `Background workflow report (a note from apogee, not from the user):` followed by one line per note. The same text opens a wake, is interjected mid-Exchange, and follows the human's text (and a recipe's result lines) in an opening message. A stopped or failed background workflow leaves a note as well.

NOTES (2026-09-27): the note is held before observer.end emits the WorkflowPhaseEvent that ends the workflow, so a Driver that wakes on that event always finds it. step() consumes held notes only when an Exchange opens, never mid-Exchange (ADR 0025's rejected engine slot is untouched). finishTally repeats the tally words of internal/workflow's unexported totalsLine rather than exporting that function, because format.go is outside this item's files.

NOTES (2026-09-27): held notes are live state only. A snapshot does not carry an undelivered note, and stopAllBackground (Close, RestoreSession) drops the held notes along with the session they belonged to. A note held when the session is saved and resumed is therefore lost. Only the wake opener's recorded text survives a snapshot.

NOTES (2026-09-27): consequential edit — internal/agent/doc.go: made necessary by the finish notes and Wake added to background.go and TakeWorkflowNotes added to interject.go (the package map's roles for both files).

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

## 27. The workflow control tool and background fan_out — ✅ DONE (2026-09-27)

NOTES (2026-09-27): run finding folded — `fan_out`'s `background` is published whenever `workflow` is on the roster (registry.go builds it from `host.rosterDeltas().lifts(workflowToolName)` alone), so a headless run or a daemon firing (cmd/apogee/wire_firing.go's `registryWithMCP` call) offers it too, against ADR 0089 D1. The Driver opt-in paragraph below closes it.

NOTES (2026-09-27): `tools.HostToolsOf` takes the opt-in as a third argument, `HostToolsOf(cfg, seatChoice, offersBackground)`, next to seatChoice. It does not set the field after the call. That way `TestHostToolsOfFillsEveryHostField` and `TestHostToolsForFillsEveryHostField` still check every field, the new one included. The engine's `defaultRoster` (internal/agent/construct.go) passes false for both. `registryWithMCP(workspace, cfg, seatChoice, offersBackground, mcpTools)` and `toolSetSpec.offersBackground` carry it. wire_live.go passes true and wire_firing.go passes false.

NOTES (2026-09-27): the `workflow` tool is always built, so `KnownToolNames` lists it. `backedTools` drops it when `HostTools.OffersBackground` is false. That drop runs before the roster ladder, so `tools.enabled:` cannot bring it back. fan_out's `background` is `OffersBackground && rosterDeltas().lifts(WorkflowToolName)`.

NOTES (2026-09-27): re-derived from the assumption that dispatch recognises a placeholder only in workflowcall.go. The recognition lives in resolve's row 3 (internal/agent/resolution.go). A `workflow` call now resolves to `resolveWorkflow`, with no depth-bound check because it spawns nothing. `runWorkflowCall` answers it through `workflowControlResult`. An unregistered `workflow` is still refused as an unknown tool, in prepareCall before resolve runs.

NOTES (2026-09-27): the unexported forward declaration `workflowToolName` in internal/tools/fan_out.go is replaced by the exported `WorkflowToolName` in workflow.go, which dispatch keys on. The action names are exported too (`WorkflowActionStatus/Stop/Message`). fan_out_test.go's `TestFanOut_RegistryGates` now sets `OffersBackground` where it expects `background` and has a new case: no opt-in means no `background`. That test pins the gate this item redefines.

NOTES (2026-09-27): re-derived from the assumption that a background launch only takes a plan. `fan_out{recipe, inputs, background: true}` also starts in the background. internal/agent/background.go splits `startKeyedBackgroundRecipe` out of `startBackgroundRecipe` (same behaviour) so both the keyed inputs and the text-bound ones reach it. A background launch is refused, never asked, when a required input is missing, and the call answers with that error.

NOTES (2026-09-27): `message` addresses only the running item children of this Agent's background workflows. They are the children registered under call id `workflow-<id>`. Status detail lists each as `<run id> <name>`, where the name is the child's display name (the item label's first line). A run id matches first, then the name. Two items with the same name get a refusal that lists their run ids. `id` narrows the match to one workflow. On a delegate, the workflow tool is refused ("only the main agent…") and fan_out's `background` runs blocking.

NOTES (2026-09-27): the "internal/run engine offers no background and runs one blocking" guard is pinned in two places. internal/tools/workflow_test.go covers the schema and tool side, including `HostToolsOf(cfg, false, false)`, the facade roster. internal/agent/workflowcall_test.go covers the dispatch side (`TestWorkflowCall_BackgroundRunsBlockingWhereTheSwitchIsNotOffered`, `TestWorkflowCall_TheFacadeRosterOffersNoBackground`). internal/run/run.go only gains a comment in Once.

NOTES (2026-09-27): consequential edit — internal/tools/roster_test.go: made necessary by registering `workflow` default-off (the test lists the default-off built-ins).

NOTES (2026-09-27): consequential edit — internal/tui/toolregistry.go: made necessary by adding `workflow` to KnownToolNames. `TestToolRegistryCoversEveryBuiltInTool` requires a card row for every known tool, and the new row is the plain floor: `stringArg("action")` plus `firstLineDetail`.

NOTES (2026-09-27): consequential edit — internal/agent/doc.go: made necessary by workflowcall.go now answering the background start and the workflow control call (the package map's role line).

NOTES (2026-09-27): consequential edit — internal/agent/construct.go: made necessary by the new `HostToolsOf` argument (defaultRoster passes false; the doc comment says why).

NOTES (2026-09-27): README.md's "34 built-in tools (30 on the default menu)" is stale. KnownToolNames now has 36 names and the default menu 30. The count was already stale after fan_out (35); item 38 owns the README.

**What:** Depends on items 14, 25.
**Goal:** a default-off `workflow` tool offers `status` (all or one), `stop` and `message` (to a running item by run id or item name, delivered as an Interjection); `fan_out{background: true}` starts a background workflow and returns its id and status path at once; a headless run and a daemon firing publish neither `background` nor `workflow`, whatever the roster says.
**Approach (assumed at the header base):** `internal/tools/workflow.go` placeholder handled in dispatch like `fan_out`; `message` routes through `InterjectChild`. Registry pins, `KnownToolNames`, manual list.
**Regression guard.** `workflow` joins `writeCapableNonFileBuiltins` in internal/agent/writedetection_test.go. When the engine has no conversation to go on (the internal/run Driver), `background` stays off the fan_out schema and a `background: true` reaching dispatch runs blocking — the item yields to ADR 0089 D1 ("Headless and daemon runs offer no background"), pinned in workflow_test.go. Children follow item 12's contract. internal/tools/doc.go names workflow.go.
Driver opt-in (ADR 0089 D1, D4): the background gate is "`workflow` on the roster AND the Driver offers background". `tools.HostTools` gains `OffersBackground bool`, which `registryWithMCP` takes as an argument beside `seatChoice` and `toolSetSpec` carries for rebuilds (the seatChoice precedent): wire_live.go (the TUI) passes true, wire_firing.go (headless and daemon) false, and the facade default in internal/agent/construct.go leaves it false. Without it the fan_out schema carries no `background` and `workflow` is not registered (the switch and its control tool travel together). Extend `TestHostToolsForFillsEveryHostField`.
**Files:** internal/tools/workflow.go, internal/tools/workflow_test.go, internal/tools/registry.go, internal/tools/registry_test.go, internal/agent/workflowcall.go, internal/agent/workflowcall_test.go, docs/manual/configuration.md, internal/agent/writedetection_test.go, internal/tools/doc.go, internal/run/run.go, cmd/apogee/wire_tools.go, cmd/apogee/wire_tools_test.go, cmd/apogee/wire_live.go, cmd/apogee/wire_firing.go
**Read first:** internal/tools/registry.go — builtinToolsWith, KnownToolNames; internal/agent/writedetection_test.go — writeCapableNonFileBuiltins; internal/agent/children.go — InterjectChild, StopChild;
  internal/tools/manual_drift_test.go — TestManualListsEveryKnownToolName; internal/run/run.go — Run; cmd/apogee/wire_tools.go — registryWithMCP, toolSetSpec; cmd/apogee/wire_firing.go — registryWithMCP call
**Tests:** each action; background fan_out returns immediately; message reaches the child's mailbox. Also: an internal/run engine offers no `background` and runs one blocking. Also: a firing-built set with `tools.enabled: [fan_out, workflow]` has no `background` in fan_out's schema and no `workflow` tool; the TUI-built set with the same roster has both; a rebuild carries the opt-in (`TestRegistryWithMCPCarriesTheSeatChoiceGate` pattern).
**Acceptance:** `go build ./... && go test -race -count=1 ./internal/tools/ && go test -race -count=1 ./internal/agent/ && go test -race -count=1 -run 'RegistryWithMCP|HostToolsFor|FiringConfig' ./cmd/apogee/`
**Commit:** `feat(tools): the workflow control tool and background fan_out`

## 28. The TUI wakes the agent — ✅ DONE (2026-09-27)

NOTES (2026-09-27): re-derived from "the TUI knows from WorkflowPhaseEvent which workflow and runs are background": at 28cd8ee5 the event carried no such marker. internal/domain/events.go now gives `WorkflowPhaseEvent` a `Background bool` field and exports `BackgroundWorkflowCallPrefix` (the agent's `backgroundCallPrefix` now aliases it). It also adds `Identity() EventBase` to the sealed `Event` interface (EventBase implements it) so the TUI can read any event's run. internal/agent/workflowcall.go's observer stamps the flag, and background.go's driveBackground sets it. The NDJSON encoding does not carry the flag, because headless and daemon runs never start a background workflow (ADR 0089 D1). internal/agent/wake_test.go pins the flag and the child call id.

NOTES (2026-09-27): re-derived from "a Bridge.NotifyWorkflow → Msg route as NotifySchedule does": WorkflowPhaseEvents already reach the Model through the event sink as eventMsg, so bridge.go is unchanged and no Bridge method was added.

NOTES (2026-09-27): re-derived from "TakeWorkflowNotes() []string": the engine's call is `TakeWorkflowNotes() (domain.UserInput, bool)`. The worker's `deliverWorkflowNotes` commits the returned input through Interject right after `deliverInterjections`. A refused Interject is reported as `workflowNoteLostMsg`, which folds to a note.

NOTES (2026-09-27): rule (b) is checked in the Update tail (`wakeAfterFold`, deferred in Update in model.go), not at a list of idle sites. That way any fold that leaves the session idle can release a held wake: an Exchange ending, a save landing, a load returning or a pane closing. `canWake` requires all of the following: stateIdle, bound, not quitting, no /sessions load in flight, no queued or in-flight record/fork write, no held messages, and no modal pane. The load needed a new `sessionLoading` flag, set in `acceptBrowser` and cleared on `sessionLoadedMsg`.

NOTES (2026-09-27): Engine.Wake is called on the Update goroutine at idle, so the prompt row is written only when the wake actually opened an Exchange. The boundary cached for mid-wake progress saves is the Snapshot taken before Wake, restored after `enterRunning`. A record saved mid-wake therefore never carries the queued opening as pending input.

NOTES (2026-09-27): rule (c): a claimed event (a Background phase, an event of a run under `workflow-<id>` of a live background workflow, or of a run such a run spawned via ToolCallEvent.SpawnRunID) is folded only into the Inspector rings and the workflow's own state. It skips the whole of foldEvent's other folds (stats, thinking, advice, transcript, usage, activity), the stall clock, progress saves and the parked-decision withdraw. A background workflow's start therefore no longer opens a transcript Workflow block.

NOTES (2026-09-27): consequential edit — internal/tui/model.go: made necessary by the Model fields (workflows, wakePending, sessionLoading), the eventMsg routing, the Update tail's wake and the sessionLoadedMsg/workflowNoteLostMsg folds this item adds.

NOTES (2026-09-27): consequential edit — internal/tui/sessions.go: made necessary by the session-load-in-flight hold (acceptBrowser sets sessionLoading).

NOTES (2026-09-27): transcript.go and fold.go are unchanged. The wake row is an ordinary depth-0 entryUser, which forkPoints and markAborted already count. The background routing sits before foldEvent in Update rather than inside it.

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

## 29. The /bg command — ✅ DONE (2026-09-28)

NOTES (2026-09-27): re-derived from the assumption that the TUI's Engine seam could name StartRecipe's request type — `RecipeLaunch` lived in internal/agent, which internal/tui does not import (ADR 0010) while `*apogee.Agent` must still satisfy tui.Engine (cmd/apogee/wire.go); it moved to `domain.RecipeLaunch` (ADR 0010's lowest-layer rule) with `agent.RecipeLaunch` kept as an alias, so apogee.go's alias and every caller are unchanged

NOTES (2026-09-27): consequential edit — internal/domain/config.go: made necessary by the tui.Engine StartRecipe method (domain.RecipeLaunch)

NOTES (2026-09-27): consequential edit — internal/agent/recipe.go: made necessary by moving RecipeLaunch to domain (now an alias)

NOTES (2026-09-27): consequential edit — internal/tui/model.go: made necessary by runBg's tea.Cmd — the Update loop folds its bgStartedMsg (foldBgStarted)

NOTES (2026-09-27): consequential edit — internal/tui/mouse_test.go: made necessary by the new /bg row — TestDropdownClickHighlightsThenTheSecondClickAccepts clicked /confine, which the extra row pushed onto the menu's last visible row, where the first click's highlight scrolls the list and the second click lands on another row; the test now clicks /color-scheme (also takes arguments, not run bare), in /confine's old geometry

NOTES (2026-09-27): consequential edit — internal/tui/heartbeat.go: made necessary by the /bg launch latch (observeBinding stashes a rebind while bgLaunching; pendingRebind/applyPendingRebind comments name foldBgStarted)

NOTES (2026-09-27): consequential edit — internal/tui/workflow.go: made necessary by the /bg launch latch (canWake holds a wake while bgLaunching)

NOTES (2026-09-27): consequential edit — internal/tui/doc.go: made necessary by the /bg launch latch (the rebind-stash boundary list names foldBgStarted)

NOTES (2026-09-27): retry per the verifier's FIX — the in-flight /bg launch is latched like sessionLoading: runBg sets Model.bgLaunching, foldBgStarted clears it, applies a stashed rebind and drains the queued commands; observeBinding, canWake and commandRunnable (plus runDeferredCommands and submit's idle command branch, which bypassed commandRunnable) honour it. TestBgLaunchStashesARebindUntilItLands runs the launch Cmd on its own goroutine beside a beat fold and reports a DATA RACE under -race with the observeBinding clause removed (checked); TestBgLaunchQueuesIdleOnlyCommandsUntilItLands and TestBgLaunchHoldsAWakeUntilItLands pin the other two gates

NOTES (2026-09-27): /bg is idle-only (no whileRunning) — a background launch reads the Agent where idle-only mutators cannot run (background.go startBackground); typed mid-run it is queued for idle like /continue (⧖ in commands.md). The engine's refusal is noted verbatim, so `missing input: <name>` is pinned exactly by a real-Agent test (TestBgMissingInputIsRefusedNotAsked)

NOTES (2026-09-27): observed, pre-existing — a first click on the dropdown's last visible row scrolls the list under the pointer, so a second click at the same cell highlights the next row instead of accepting; the click-then-click contract does not hold on that row

NOTES (2026-09-28): retry per the verifier's FIX — the latch closed at its two remaining boundaries: finishWorker and foldActuationDone skip applyPendingRebind while bgLaunching, leaving the stash for foldBgStarted, and /bg's row is touchesServer so an in-flight launcher verb refuses it (actuationBlocked). TestBgLaunchKeepsARebindStashedPastAnExchangeEnd, TestBgLaunchKeepsARebindStashedPastAnActuationEnd (the overlap forced by setting bgLaunching, since the latch now refuses /bg) and a /bg line in TestActuationLatchRefusesEveryMoveWhileHeld each fail with their guard removed (checked); TestTheActuationLatchRefusesExactlyTheServerAndExchangeVerbs's pinned sets gain bg

NOTES (2026-09-28): consequential edit — internal/tui/actuation.go: made necessary by the bgLaunching guard at foldActuationDone (and actuationBlocked's doc naming /bg)

NOTES (2026-09-28): consequential edit — internal/tui/actuation_test.go: made necessary by /bg joining the actuation latch's refused set

NOTES (2026-09-28): consequential edit — docs/manual/configuration.md: made necessary by /bg joining the actuation latch's refused set (the "one actuation runs at a time" sentence names it)

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

## 30. Status indicator and waiting questions — ✅ DONE (2026-09-28)

NOTES (2026-09-28): DECISION applied (persist and replay). `workflow.StageRecord{Phase, Note, Receipt}` is written by `Store.WriteStageRecord` to `results/<stage>/<round>.json` and read back by `ReadStageRecord`. The stage name is held to the plan's `namePattern`, and a negative round is refused (`ErrInvalidName`). `runScript` and `runAsk` now take the round. They replay the record (`replayStage`, item marked Resumed) before they run or ask anything, and they record through `settleRecorded` before status.json shows the stage settled. Only outcomes the stage actually reached are recorded: a script that ran (any exit code) and a question the Asker answered, including an empty or off-option answer that took the default. A script that could not run, a cancel, and a "no one to ask" default are not recorded, so a resume runs or asks them. The replay is in `Runner.Run`, so item 34's session resume (Close marks the folder stopped, then `ResumeWorkflows` then Run) gets it without further work. `TestBackground_AResumeReplaysAnAnsweredQuestion` pins that path. With the replay disabled it fails with two waiting events.

NOTES (2026-09-28): replay is keyed on the found folder not having finished. `openStatus` returns `replay = found && status.Phase != PhaseDone`. A re-issue of a workflow that ran to its end still skips finished fan-out items (ADR 0087 D4), but it runs its scripts and asks its questions afresh. A stale split listing or answer is therefore never replayed into new work. Pinned by `TestReissueOfAFinishedWorkflowRunsItsScriptAndAsksAgain`.

NOTES (2026-09-28): item 25's CHANGELOG text ("A resumed workflow skips its finished items, but runs its script stages and asks its questions again") is contradicted by this item. The closeout should drop that clause when it applies both entries. background.go's file comment ("Resume replays") and `ResumeWorkflows`'s doc comment are updated here.

NOTES (2026-09-28): re-derived from "the manager's queued asks surface through the Bridge": no Bridge route was added. A background prompt is reported by a Background `WorkflowWaiting` event, which already reaches the Model as eventMsg. At idle, the Update tail (`offerAfterFold`, run before `wakeAfterFold`) reads the queue through two new Engine calls, `WorkflowPrompts() []domain.WorkflowPrompt` and `AnswerWorkflowPrompt(id uint64, domain.WorkflowPromptAnswer) bool`. Both are on `*agent.Agent`, lateEngine and fakeEngine. The two types live in internal/domain/ask.go (ADR 0010's lowest-layer rule, as `domain.RecipeLaunch` did in item 29). The idle gate is `canWake`, and the fold syncs the waiting counts to the queue it read.

NOTES (2026-09-28): re-derived from the assumption that a background ask's waiting event could be trusted. At the base it was emitted by `observedAsker` BEFORE the question was queued, and background approvals emitted none. The observer is now built at launch in `startBackground` and kept on `backgroundRun`. The background Asker is set after `observeWorkflow`, so it is not wrapped. `backgroundManager.wait` mints a prompt id, queues the prompt, and only then announces it (`backgroundScope.announce`), with Detail set to the question or `approve <tool>`. internal/agent/workflowcall.go gains `workflowObserver.waitingOn`, and `waiting` now delegates to it. Pinned by `TestBackground_AQuestionIsListedBeforeItIsReportedAndItsAnswerResumesTheWorkflow`, whose sink lists the queue inside Emit.

NOTES (2026-09-28): consequential edit — internal/domain/events.go: made necessary by WorkflowWaiting now also reporting a background approval (the doc comment says so).

NOTES (2026-09-28): consequential edit — internal/tui/messages.go: made necessary by "the ask request carries its origin": `approvalReqMsg.Workflow` and `askReqMsg.Workflow` (`*domain.WorkflowPrompt`, nil for the conversation's own, with no Reply when set).

NOTES (2026-09-28): consequential edit — internal/tui/doc.go: made necessary by workflow.go's new role (readout, waiting prompts); its map line says so.

NOTES (2026-09-28): consequential edit — layout.md: made necessary by the status line's left slot gaining the workflow readout (new paragraph after "what the left slot sheds").

NOTES (2026-09-28): consequential edit — docs/manual/commands.md: made necessary by the user-visible indicator, waiting prompt and resume replay (the /bg row).

NOTES (2026-09-28): consequential edit — internal/workflow/store.go, runner.go, stages.go, doc.go, recipe_stages_test.go: made necessary by the DECISION (persist and replay), which lives in the Runner, outside the item's Files.

NOTES (2026-09-28): the pane's close path is `closeWorkflowPrompt`. It resets the pane, hands back the borrowed draft, returns to stateIdle (never resumeRunning), applies a rebind stashed while the pane stood (unless a /bg launch or an actuation is in flight), and re-arms the offer. esc and the approval menu's Cancel row dismiss (`dismissWorkflowPrompt`, never stopWorker). A dismissed id is skipped by the offer until the next Exchange ends: `finishWorker` calls `reofferDismissed`. Item 31's /workflows is the other planned route back. `quit()` dismisses an open background pane first, so a quit is the idle one rather than a deferred busy quit that would wait for a worker that does not exist. A workflow ending while its prompt is open closes the pane unanswered.

NOTES (2026-09-28): the readout shows both counts, "2 workflows running · 1 workflow waiting for you" (`statusTrail`, after the queued count, kept whole). `statusLeft` reads only the folded `backgroundWorkflows`. No apogee.go alias was added for domain.WorkflowPrompt(Answer): the facade is outside this item's files, and `*apogee.Agent` exposes the two methods typed in domain.

NOTES (2026-09-27): resume re-asks. Item 10 (7235dbbd) skips only finished fan-out items on resume: `prepareItems` (runner.go) reads each item's stored receipt through `Store.ReadReceipt`, but `openStatus` resets every stage to pending and `runRound` sends script and ask stages to `runScript` and `runAsk` (stages.go), which look up nothing stored and run fresh. A resumed recipe therefore re-runs its scripts and asks the user its questions again. Weigh this here: either persist ask answers and script results and replay them on resume, or accept the re-ask and document it.

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

## 31. The /workflows view: list and detail — ✅ DONE (2026-09-28)

NOTES (2026-09-28): re-derived from the assumption that the TUI's Engine seam could name `Agent.Workflows()`'s result type. `WorkflowInfo` lived in internal/agent, which internal/tui does not import (ADR 0010), while `*apogee.Agent` must still satisfy tui.Engine. It moved to `workflow.Info` in internal/workflow/store.go, the lowest layer that can define it because it carries `workflow.RunStatus`. `agent.WorkflowInfo` is kept as an alias, so every agent caller is unchanged. This is the item 29 `domain.RecipeLaunch` precedent.

NOTES (2026-09-28): re-derived from "data from status.json via Agent.Workflows()". status.json carries an item's receipt, but not its detail output path or its conversation. internal/workflow gains two readers beside WriteTranscript, both taking the folder dir an Info carries: `ReadItemTranscript(dir, key)` reads items/<key>/transcript.jsonl back, and `ItemOutputPath(dir, stage, key, label)` renders the stage's `out:` from plan.json, falling back to items/<key>/output.md. store_test.go pins both. The TUI resolves a relative `out:` against the workspace, reads at most 64 KiB of the output file and caps the item level at 2000 rows. Every line is escape-stripped.

NOTES (2026-09-28): the pane holds a `listCursor` per level (list, detail, item) rather than a filtering `listSurface`. The Goal names no filter, and a per-level cursor lets esc return to the row the human left. The item level shows its reading as rows under a highlight that ↑/↓ and the wheel move, with no wrap.

NOTES (2026-09-28): the pane is `paneWorkflows`, placed after `panePicker` in framePane, keyClaimOrder ("workflows view") and pointerPanes. It is modal (so it also holds the wake and the waiting-prompt offer while it is up, via `modalPaneOpen`), and its keyOpen is the picker's `state.live()` gate. That makes 10 panes, under the 16-bit cap. The listing loads through a tea.Cmd, on open and again on each WorkflowPhaseEvent while the pane is up, on both the background and the conversation event paths. The item detail also loads through a tea.Cmd, on open and on each re-list. `listSeq` and `itemSeq` drop an overtaken read.

NOTES (2026-09-28): consequential edit — internal/tui/command_test.go: made necessary by the new `workflows` row (TestCommandTableDrivesParserAndMenu pins the parser's verb list).

NOTES (2026-09-28): consequential edit — internal/workflow/store.go, internal/workflow/store_test.go, internal/agent/background.go: made necessary by the two re-derivations above (the Info move and the item readers).

NOTES (2026-09-28): consequential edit — docs/manual/commands.md: made necessary by the new user-visible `/workflows` command (a ✅ row, and its name in the list of commands that answer mid-run).

NOTES (2026-09-28): not in this item: item 30's notes name /workflows as "the other planned route back" to a dismissed waiting prompt, and the ratified design call says the question opens "when the user is idle or opens /workflows". Item 31's Goal does not include answering a waiting prompt from the view, and this item does not add it.

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

## 32. /workflows actions: stop and re-run failed — ✅ DONE (2026-09-28)

NOTES (2026-09-28): re-derived from "`Agent.RerunFailed(id)` resetting those items' store state and resuming". No reset is needed: the Runner already re-runs every item without an ok or partial receipt, and blocked items include faulted ones, whose exhausted retries end on a blocked receipt. A resume, though, needs the recipe id, and the snapshot entry carries it only while the workflow is live. So `workflow.RunStatus` gains `recipe` (omitempty) and `workflow.Runner` gains `Recipe`, which openStatus records and newRecipeRunner sets. `RerunFailed` then relaunches through `resumeBackground(entry, folder)`, and `openWorkflowFolder` takes the folder the re-run must find. It refuses a plan whose hash now leads to no folder or to a newer one, and creates nothing in that case. Files outside the plan's list: internal/workflow/runner.go, internal/workflow/store.go, internal/workflow/runner_test.go, internal/agent/recipe.go.

NOTES (2026-09-28): `StopWorkflow` already existed on the Agent (item 25). This item adds it to tui.Engine, lateEngine and fakeEngine only.

NOTES (2026-09-28): a re-run is refused for a workflow whose phase is not `done` (stopped, or cut off while running). Re-running one of those would also run its unfinished items, which breaks the Goal's "only". A finished workflow's re-run is a new run in the Runner's sense: script and ask stages run afresh, as any re-issue of finished work does.

NOTES (2026-09-28): ^x runs StopWorkflow in a tea.Cmd, because a queued workflow's stop writes its status.json. Its answer is a new `workflowStoppedMsg`, which needs a case in internal/tui/model.go's Update switch; that file is not in the plan's list. ^r goes only at idle: it sets the /bg launch latch (`bgLaunching`), because RerunFailed takes the launch-time snapshot. It then folds through the existing `bgStartedMsg`, whose note is `started <id> in the background`. Mid-Turn, ^r is refused with a note.

NOTES (2026-09-28): the plan's test "a bare `r` types into the filter" does not apply. Item 31 built the pane on per-level `listCursor`s with no filter. The test now pins that a bare `r` or `x` does nothing, and that ^x and ^r act only in the detail (TestWorkflowsViewVerbsAreDetailChordsOnly).

NOTES (2026-09-28): consequential edit — internal/tui/doc.go: made necessary by the detail's two new chords (the workflows.go line).

NOTES (2026-09-28): consequential edit — internal/agent/doc.go: made necessary by the new Agent.RerunFailed (the background.go summary).

NOTES (2026-09-28): consequential edit — docs/manual/commands.md: made necessary by the new user-visible ^x and ^r in the /workflows detail.

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

## 33. Save a fan_out as a recipe — ✅ DONE (2026-09-28)

NOTES (2026-09-28): the name rule and the Mkdir claim are exposed as a new `skills.WriteNew(id, libraryDir, content)` in internal/skills/export.go beside ExportShipped (validShippedID is unexported and the TUI cannot reach it) — internal/skills/export.go is outside the item's Files list.

NOTES (2026-09-28): `workflow.ReadFolderPlan(dir)` added in torecipe.go so the TUI reads a workflow's plan.json by its listed folder (Info.Dir) without a Store; torecipe.go imports gopkg.in/yaml.v3 (external module, not from the tree — the boundary test allows it; doc.go line says so).

NOTES (2026-09-28): consequential edit — internal/tui/model.go: made necessary by the new off-loop workflowSavedMsg, which the Update switch must dispatch to foldWorkflowSaved.

NOTES (2026-09-28): consequential edit — docs/manual/commands.md: made necessary by the new `^s` chord in the /workflows detail (the row lists the detail's chords).

NOTES (2026-09-28): the /workflows pane has no filter (it is a listCursor pane), so the "bare `s` types into the filter" test is recast as: a bare `s` in the detail opens no name row, and inside the open name row a bare `s` types into the name.

NOTES (2026-09-28): a plan whose text already spells `{scope}` keeps its path literal and declares no input (binding would rewrite that text); a plan with a `prompt:` file, or one Validate refuses, is refused by PlanToRecipe; a recipe's own workflow is refused with a note naming its recipe.

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

## 34. /clear, quit and resume with workflows running — ✅ DONE (2026-09-28)

NOTES (2026-09-28): per the DECISION, session resume uses item 30's persist-and-replay (workflow.StageRecord): `ResumeWorkflows` relaunches each workflow from its folder, so answered asks and finished scripts are replayed, not re-asked or re-run. Nothing here re-asks or documents a re-ask limit.

NOTES (2026-09-28): re-derived from the assumption that ClearContext never stops the background set and that `y` reaches `stopAllBackground` without an Engine method. ClearContext now stops the set (`endSessionWorkflows`, which also drops a restored set not yet resumed) unless `KeepWorkflows()` was called just before. The same one-shot mark serves RestoreSession. So `y` is a plain clear or restore and `n` is `KeepWorkflows` + the boundary: one Engine method covers all three paths. The mark is consumed even by a refused boundary. Headless and daemon never run background workflows (ADR 0089 D1), so nothing changes for them.

NOTES (2026-09-28): re-derived from the assumption that the TUI already had a way to start a restored set. No Driver called `ResumeWorkflows`, and it was not on the tui.Engine seam. So `ResumeWorkflows()` joins tui.Engine, lateEngine and fakeEngine beside `KeepWorkflows()`, a second lift of the guard's "adds no Engine method". The TUI calls it from an Update-tail hook (`resumeAfterFold`, sessions.go) once the engine is bound, idle and the record-write queue has drained. That covers a `--resume`/`--continue` start (after the bind, for a pre-bound start too) and a `/sessions` switch or `/fork` (after the queued Activate has moved the scratch dir). cmd/apogee/wire_live.go needed no change: lateEngine forwards the call, and `TestWorkflowResumeReachesTheResumedSessionsSet` in wire_live_test.go drives a real `--resume` wiring.

NOTES (2026-09-28): re-derived from the assumption that `workflow_notes` is restored straight into the held notes. RestoreSession stops the outgoing set after the swap, and that stop would drop incoming notes too. So a restore loads them beside the `workflows` set (`restoredNotes`), `ResumeWorkflows` adopts them after any kept notes, and the snapshot writes held notes plus not-yet-adopted ones. They are checked like other text the model reads (`checkRestoredWorkflowNotes`: one line each, no forged fence, within `maxRestoredMessageBytes` in total). `CutSession` writes none.

NOTES (2026-09-28): a workflow kept across `/clear` or a switch keeps running in the OLD session's scratch folder, because the scratch dir follows the session id. To keep it listed and resumable, a snapshot entry now carries an optional `home`: the sibling session scratch dir's base name, validated as a session-id-shaped name (`isSessionDirName`). `ResumeWorkflows` resolves the folder there. `Workflows()` also lists every workflow launched since the set was last stopped whole whose folder is in another store (`storesElsewhere`), running or ended. This goes beyond the item text. Without it a kept workflow disappears from `/workflows` and fails to resume.

NOTES (2026-09-28): re-derived from the assumption that the staged-message flush lives in commandrun.go. It lives in `drainThenFlush` (internal/tui/interject.go), which now holds the flush while the confirm is up and marks `pendingBoundary.flush`. `answerBoundary` then drains the queued commands and flushes. The resume flag and the Update tail's resume hook go on the Model in internal/tui/model.go.

NOTES (2026-09-28): the confirm is a picker kind (`pickerWorkflowBoundary`) whose `picker.boundary` carries the pending boundary: clear, switch (session id) or fork (point). `y`/`n` answer it ahead of the filter, `⏎` answers from the rows, `esc` cancels. The keep answer rides `sessionLoadedMsg.keep` and `forkPayload.keep` to the restore it belongs to, so a failed load or a skipped fork switch never leaves a stale mark. `n` on `/clear` also adds the note `background workflows keep running — their reports come to this conversation`.

NOTES (2026-09-28): consequential edit — internal/agent/doc.go: made necessary by ClearContext/RestoreSession stopping the set unless KeepWorkflows kept it, and by held notes riding the snapshot (the package map's background.go role).

NOTES (2026-09-28): consequential edit — internal/tui/picker_test.go: made necessary by the new pickerWorkflowBoundary kind (TestEveryPickerKindHasAnOffering enumerates the pickerKind enum through its last constant).

NOTES (2026-09-28): caveat. A kept workflow's children still run on the launch-time host snapshot, whose scratch dir is the old session's. If a confinement box follows the session scratch dir, those children could lose write access to the old scratch after the move. This was not exercised here.

NOTES (2026-09-28): retry per the verifier's FIX: a ClearContext with no workflow live (the unasked `/clear`) now stops nothing and drops no held note (`endSessionWorkflows` calls `stopAllBackground` only when a run is live; it still drops a restored set not yet resumed and the launched list). `TestBackground_AClearWithNothingRunningKeepsTheHeldNote` pins it. RestoreSession is unchanged: a non-kept switch still drops held notes, which the outgoing session's saved record carries.

NOTES (2026-09-27): resume re-asks. Item 10 (7235dbbd) skips only finished fan-out items on resume: `prepareItems` (runner.go) reads each item's stored receipt through `Store.ReadReceipt`, but `openStatus` resets every stage to pending and `runRound` sends script and ask stages to `runScript` and `runAsk` (stages.go), which look up nothing stored and run fresh. A resumed recipe therefore re-runs its scripts and asks the user its questions again. Weigh this here: either persist ask answers and script results and replay them on resume, or accept the re-ask and document it.

NOTES (2026-09-27): run finding folded — held finish notes are live state only: `stopAllBackground` (Close, RestoreSession) takes and drops them, and the snapshot does not carry them (item 26's NOTES). So a kept workflow's note can be lost on a `/sessions` switch or `/fork` that keeps the set, and any held note is lost at quit→resume. The "Held notes" paragraph below binds how each path keeps them.

**What:** Depends on items 25, 26, 32.
**Goal:** `/clear` with a background workflow running asks `stop running workflows? (y/n)` — `y` stops them, `n` keeps them and their finish notes go to the new conversation; quitting stops them; resuming the session (flag or `/sessions`) resumes them; a finish note held and not yet delivered survives the `n` answer on every path and a quit→resume.
Held notes: `n` on `/clear` leaves the manager (running set and held notes) untouched, since ClearContext does not call `stopAllBackground`. So a note held at the clear, and every later one, reaches the new conversation. `y` stops through `stopAllBackground`, which drops the held notes with the set. On a `/sessions` switch or `/fork`, `RestoreSession` stops the outgoing set and drops its notes, so `n` there goes through a new `Agent.KeepWorkflows()` the Driver calls just before the restore: it makes that one restore skip `stopAllBackground`, so the kept set and its notes carry over. It joins tui.Engine, lateEngine and fakeEngine, which lifts the guard's "adds no Engine method" for this one call. Held notes join the snapshot as an additive omitempty `workflow_notes` key (strings, beside `workflows`), restored into the manager and consumed like any held note (next opening, Wake, or `TakeWorkflowNotes`). as for `workflows`, `CutSession` writes none, and the key joins `TestAgentState_EncodesStableKeyNames`. The record saved at quit encodes the held notes before `Close` drops them.
**Approach (assumed at the header base):** A confirm step in `Model.startNewSession` on the `sessionBrowser.confirming` pattern; resume via the snapshot key of item 25 in `resumeLoaded` and the `wire_live.go` resume path; quit via `engine.Close()`.
**Regression guard.** Depends on item 32 as well (uses StopWorkflow), adds no Engine method. While the `/clear` confirm is open the deferred-command drain and the staged-message flush stop, resuming from the answer's fold; the confirm is a picker kind (the start-up `pickerKeyMigration` precedent, picker.go:90), not the browser's in-pane confirm. A `/sessions` switch and `/fork` take the same stop-or-keep decision, and a restore skips a workflow the live manager already runs; `saveAtIdle` also saves while background workflows are live, so a `/bg`-only session survives quit and resume. A stop from the confirm, and Close at quit, emit no held note and no wake. The `n` answer is an exception to ClearContext's drop of session-owned live state (agent.go:1773, ADR 0086 D3), named in that doc comment.
**Files:** internal/tui/commandrun.go, internal/tui/commandrun_test.go, internal/tui/sessions.go, cmd/apogee/wire_live.go, cmd/apogee/wire_live_test.go, cmd/apogee/wire_engine.go, docs/manual/commands.md, internal/tui/picker.go, internal/tui/sessionsave.go, internal/tui/fork.go, internal/agent/agent.go, internal/agent/background.go, internal/agent/state.go, internal/agent/state_test.go, internal/agent/background_test.go, internal/tui/tui.go, internal/tui/seam_test.go
**Read first:** internal/tui/commandrun.go — startNewSession, runDeferredCommands; internal/tui/sessions.go — resumeLoaded; internal/tui/sessionsave.go — saveAtIdle; internal/agent/state.go — CutSession;
  cmd/apogee/wire.go — rootWiring.close; cmd/apogee/wire_engine.go — buildAgent, lateEngine; internal/agent/background.go — stopAllBackground, takeNotes; internal/agent/agent.go — ClearContext, RestoreSession
**Tests:** both answers; resume restarts a workflow with finished items skipped. Also: a `TestWorkflowResume…` in cmd/apogee/wire_live_test.go; a queued `/clear` plus a message with a workflow running; `/bg` alone, quit, resume; `/fork` and a `/sessions` switch ask the same question. Also: a note held before a `/clear` answered `n` opens the new conversation's first request; `y` leaves no note; a `KeepWorkflows` restore keeps the set running and its held note; a held note survives snapshot → restore; `TestAgentState_EncodesStableKeyNames` lists `workflow_notes` as omitempty.
**Acceptance:** `go build ./... && go test -race -count=1 ./internal/tui/ && go test -race -count=1 ./internal/agent/ && go test -race -count=1 -run Workflow ./cmd/apogee/`
**Commit:** `feat(tui): /clear, quit and resume respect running workflows`

## 35. Headless --recipe — ✅ DONE (2026-09-28)

NOTES (2026-09-28): run.Once launches Spec.Recipe through the Agent's own StartRecipe rather than a spelled "/<id>" message, so an unknown recipe or an unbound required input is refused before anything is sent (exit 2, zero Turns) instead of reaching the model as a refusal line; the record's title and replayed scrollback use the "/<id> <text>" launch line (Spec.line).

NOTES (2026-09-28): exit 1 also covers a --recipe workflow that never started (the launch could not run) or ended `failed`, beside the plan's stopped and all-blocked cases — each leaves the model answering over no usable work; the outcome rides run.Result.Workflow (WorkflowOutcome), read off the first top-level workflow_phase stream of a recipe Firing.

NOTES (2026-09-28): `make lint` reports one ineffassign at internal/tui/commandrun_test.go:334, introduced by item 34's commit ad3fee2b — untouched here.

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

## 36. Daemon run: workflow: — ✅ DONE (2026-09-28)

NOTES (2026-09-28): `recipe:` is validated as a bare skill id — a leading `/` or embedded whitespace is a load defect pointing at `inputs:` — since a Firing cannot check that the recipe exists until it runs (the skills catalog is per workspace); an unknown recipe fails the firing before anything is sent, as headless's does.

NOTES (2026-09-28): the daemon does not turn a stopped, failed or all-blocked workflow into a failed firing (headless's exit-1 mapping, recipeWorkflowFailure); like a faulted final turn, the firing completes and its record carries the result lines — headless.go was left untouched.

NOTES (2026-09-28): the Long help of `apogee daemon`, the package comment in daemon.go and the commented template (a second, `workflow:` example entry) name the new key; the real-engine test drives Load → Apply → tick → run.Once end to end in daemon_test.go.

NOTES (2026-09-28): retry — the three actionDefects messages no longer end on a key name with a trailing colon ("…under run: workflow: recipe: instead", "…under inputs: beside it"), the asserted substrings kept.

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

## 37. Bench readiness for workflows — ✅ DONE (2026-09-28)

NOTES (2026-09-28): re-derived from "aliases in apogee.go" — Recipe, RecipeSource, InputDecl and RecipeLaunch were already aliased by item 20, so this item adds only the missing workflow definition and listing aliases (WorkflowPlan, WorkflowStage, StageKind + the seven Stage… consts, ItemSource, ReceiptSpec, WorkflowInfo). The root `Plan`/`Stage` names are prefixed `Workflow…` so they do not read as ModePlan or a docs/plans/ plan (CONTEXT.md _Avoid_ "plan").

NOTES (2026-09-28): example_test.go also pins the workflow surface earlier items aliased but never pinned (WorkflowConfig, WorkflowPhaseEvent, WorkflowPhase, WorkflowReceipt, the seven WorkflowPhase consts, Recipe, RecipeSource, InputDecl, RecipeLaunch), plus `(*apogee.Agent).StartRecipe` and `.Workflows` as method values.

NOTES (2026-09-28): benchreadiness_test.go's runToQuiescence now submits and calls a new stepToQuiescence helper, so the recipe case can step an Exchange that StartRecipe opened; the file header gains a paragraph on the workflow proof's import rule (internal/tools only stocks the menu with fan_out).

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

## 38. Workflows manual page and README — ✅ DONE (2026-09-28)

NOTES (2026-09-28): docs/manual/commands.md already carried a `/workflows` row (and a `/bg` row) from items 29-33, so the item's "gains a `/workflows` entry linking workflows.md" was done by linking the existing rows to workflows.md rather than adding a second row.

NOTES (2026-09-28): AGENTS.md:32 still cites "34 tools, 30 on the default menu" as its example of a README count; left untouched because it is an agent-instruction file, so the owner should update it to 36.

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

## 39. The bench experiment design — ✅ DONE (2026-09-28)

NOTES (2026-09-28): the doc adds a third experiment (background + `workflow` vs `fan_out` alone, run only after a superior `fan_out` verdict) beside the Goal's two, because the Goal names `background` and `workflow` among the switches it gates and ADR 0087 D8's two experiments cover `fan_out` only.

NOTES (2026-09-28): "context tokens at the orchestrator" and "completion rate" are pre-registered secondaries, not gate endpoints — ADR 0009 gates on the ordinal mean only and apogee-sim ADR 0015 §3 keeps efficiency secondary; "findings confirmed" (seeded-defect recall, 0 for a run with no report) is Experiment 1's primary ordinal.

NOTES (2026-09-28): the doc names instrument prerequisites outside this repo's scope (apogee-sim: a roster/launch-route arm axis, a seeded-defect audit pool with matchers, a wake-capable Driver loop, a depth-0 usage read) and one in apogee: the facade offers no opt-in for fan_out's `background` field or the `workflow` tool (construct.go defaultRoster passes offersBackground false; an out-of-module embedder cannot import internal/tools), so Experiment 3 cannot run through apogee-sim's facade-only coreagent yet.

**What:**
**Goal:** `docs/design/workflow-bench-experiment.md` states the experiments that gate turning `fan_out`, `background` and `workflow` on per model class: `audit` three ways on a small model (prose orchestrator via the user's `code-audit` skill, prose-sequential, recipe), and a task set with `fan_out` on vs off, with metrics (findings confirmed, context tokens at the orchestrator, completion rate) under ADR 0009's decision rule.
**Approach (assumed at the header base):** Design doc only; the runs live in `apogee-sim`.
**Files:** docs/design/workflow-bench-experiment.md
**Read first:** docs/adr/0009-the-ab-decision-rule.md — decision rule; docs/adr/0087-the-engine-runs-workflows-the-model-or-a-recipe-asks-for.md — D8 flagship experiment;
  docs/design/test-drivers.md — design-doc house style; ~/.claude/skills/code-audit/SKILL.md — prose-orchestrator arm
**Tests:** none (docs).
**Acceptance:** `test -s docs/design/workflow-bench-experiment.md`
**Commit:** `docs(design): the workflow bench experiment`

## 40. fan_out honours run_on and refuses a mistyped recipe

**What:** Depends on item 27. Fixes two run findings in fan_out's argument handling: the published `run_on` is never read (`workflowSpawner.Spawn` builds every item child with `newChildAgentOn(seatConfigured, …)`), and a `recipe` whose value is not a string (e.g. `5`) makes `parseFanOutRecipe` report "not a recipe call", so the call runs as a plain fan-out.
**Goal:** a fan_out call's `run_on` is the seat of every item child the workflow it starts spawns, recipe form and background included. The Runner's width and the split budget follow that seat. An invalid `run_on` value, or a `recipe` set to a non-empty value that is not a string, is refused as a fixable tool error and runs nothing.
**Approach (as read at 28cd8ee5):** `run_on` is read only where this Agent's fan_out published it (`FanOut.OffersSeatChoice`, the `publishesSeatChoice` rule `runSubAgent` applies to sub_agent), parsed with `parseDelegationSeat`, and its error text is sub_agent's. Everywhere else it is ignored. `workflowSpawner` gains a `seat` field passed to `newChildAgentOn`. `newWorkflowRunner`/`newRecipeRunner` size by seat: `seatSession` takes `parallelAgentsCap()` and the session's own window (`budget().ContextLimit`); `seatConfigured`/`seatSubAgentsServer` keep `delegationWidth()` and `workflowContextLimit()` (a sub-agents-server ask with no target latched falls back to the session, as `newChildAgentOn` does). A background fan_out queues on its seat's server (`backgroundServer`, `backgroundWidth` read the seat). `parseFanOutRecipe` treats a `recipe` key whose value is set but not in `emptyJSONValues` and not a string as a recipe call carrying a refusal: `fan_out was not run: recipe must be the name of a recipe (a string), got <value>`.
**Regression guard.** A call that names no `run_on` spawns, sizes and queues exactly as at the header base (`TestWorkflowCall_ThreeItemsOnACapThreeServerRunAtOnce`, `TestWorkflowContextLimit` unchanged). The fan-out ceiling and `seatsAreSplit` still never count fan_out. A `recipe: ""`/`null` stays "no recipe", as today. The seat rides the launch: backgroundLaunch → backgroundRun carries it, startBackground sets it on the rebuilt spawner (background.go:359) and startRunLocked passes it to backgroundServer/backgroundWidth (:429); resumeBackground resumes on seatConfigured (workflowEntryJSON carries no seat). `newWorkflowSpawner(turn, call, prompts)` keeps its signature and the seat is set on the returned spawner (zero value is seatConfigured). `{}`, `[]`, `0`, `false` stay in emptyJSONValues, so `recipe: {}` stays "no recipe".
**Files:** internal/agent/workflowcall.go, internal/agent/workflowcall_test.go, internal/agent/workflowspawn.go, internal/agent/recipe.go, internal/agent/background.go, internal/agent/background_test.go
**Read first:** internal/agent/workflowcall.go — workflowCallResult, parseFanOutRecipe, emptyJSONValues, newWorkflowRunner, workflowContextLimit; internal/agent/background.go — startBackground, startRunLocked, backgroundWidth, backgroundServer, resumeBackground, startKeyedBackgroundRecipe;
  internal/agent/workflowspawn.go — workflowSpawner, newWorkflowSpawner, Spawn; internal/agent/subagent.go — runSubAgent (seat block), parseDelegationSeat, newChildAgentOn; internal/agent/recipe.go — runRecipe, newRecipeRunner;
  internal/agent/dispatch.go — delegationWidth, delegationCap, fanOutWidthFor; internal/agent/workflowcall_test.go — TestWorkflowContextLimit, TestWorkflowCall_ARecipeCallThatCannotRunIsAFixableError
**Tests:** under `sub-agents-choice: model` with a target latched, `run_on: "session"` builds every item child on the parent's server at the session cap and window, and `"sub-agents-server"` routes them. `run_on: "elsewhere"` is refused with sub_agent's text. `run_on` on a fan_out that did not publish it is ignored. `recipe: 5` and `recipe: true`-with-a-task are refused with the exact text and spawn nothing. A background fan_out with `run_on: "session"` queues on the session server.
**Acceptance:** `go build ./... && go test -race -count=1 ./internal/agent/`
**Commit:** `fix(agent): fan_out honours run_on and refuses a mistyped recipe`

## 41. Plan mode offers fan_out beside sub_agent

**What:** Depends on items 16, 27. Fixes two run findings. `toolMenu` (loop.go) keeps `sub_agent` in Plan through a name exception but withholds fan_out (`planOffers` rejects its class), while `resolve` still runs a fan_out call in Plan (the `resolveWorkflow` row sits ahead of the ladder). And `refusePastCeiling` decides its `— for more items, use fan_out` pointer with `lookupTool`, so in Plan the refusal names a tool the menu does not offer. ADR 0087 D4 decides it: the workflow folder lives in the scratch dir, which Plan writes, so "a read-only audit works in Plan mode". A Plan fan_out's children inherit Plan, as a Plan sub_agent's do (ADR 0013).
**Goal:** in Plan mode with a session scratch dir set, the menu offers `fan_out` (and `workflow`, when it is on the roster and the Driver offers background — item 27) whenever the roster carries it. Without a scratch dir it offers neither, because fan_out is refused there on every call. The fan-out ceiling's pointer names fan_out exactly when the refusing Agent's menu offers it.
**Approach (as read at 28cd8ee5):** Widen the recursion-point exception in `toolMenu` from `tools.SubAgentToolName` to a small predicate (`planOffersDelegation(t, scratchSet)`: sub_agent always; fan_out and workflow iff scratchSet). `offersTool` (dispatch.go) uses the same predicate. `refusePastCeiling` asks `offersTool(tools.FanOutToolName)` instead of `lookupTool`. The toolMenu and planAdmits doc comments that name sub_agent as the only exception are restated (`grep -n "sub_agent" internal/agent/loop.go internal/agent/resolution.go | grep -i plan`).
**Regression guard.** The default Plan menu is byte-identical (fan_out and workflow ship default-off): `TestContextCostGolden` and `TestPlanToolMenuWithoutAScratchDirIsReadOnly` stay green. `planMenuTools` gains fan_out so `TestPlanToolMenuAgreesWithTheLadder` covers it. In that test fan_out resolves to `resolveWorkflow` (not a refusal), and the no-scratch case must hold too. The load_skill recipe line (item 21, read through `offersTool`) now names fan_out in Plan with a scratch dir. Its test follows. `resolve()` refuses fan_out and workflow in Plan when `in.scratchDir == ""`, with `fanOutNoScratch`'s text, so the menu and the ladder agree in `TestPlanToolMenuWithoutAScratchDirIsReadOnly` with no exemption. A Plan-menu case registers `tools.NewWorkflow()` (OffersBackground set) with and without a scratch dir.
**Files:** internal/agent/loop.go, internal/agent/dispatch.go, internal/agent/resolution.go, internal/agent/planmenu_test.go, internal/agent/fanout_test.go, internal/agent/workflowcall_test.go
**Read first:** internal/agent/loop.go — toolMenu; internal/agent/resolution.go — planOffers, planAdmits, resolve (fan_out/workflow row); internal/agent/dispatch.go — refusePastCeiling, offersTool, lookupTool, runTool (WithFanOutOffered);
  internal/agent/planmenu_test.go — planMenuTools, TestPlanToolMenuAgreesWithTheLadder, TestPlanToolMenuWithoutAScratchDirIsReadOnly; internal/agent/fanout_test.go — TestFanOut_CeilingRefusalPointsToFanOut; internal/tools/registry.go — builtinToolsWith, backedTools;
  internal/agent/workflowcall_test.go — TestWorkflowCall_LoadSkillNamesHowARecipeStartsFromTheCallersMenu
**Tests:** a Plan agent with fan_out on the roster and a scratch dir offers fan_out, and with no scratch dir does not. Its fan_out children run in Plan: a child's `write_file` outside scratch is refused. In Plan, the ceiling refusal carries the fan_out pointer exactly when the menu offers fan_out. `TestWorkflowCall_LoadSkillNamesHowARecipeStartsFromTheCallersMenu` gains the Plan case. With `tools.NewWorkflow()` registered, the Plan menu offers `workflow` with a scratch dir and not without one, and the ladder agrees; with no scratch dir, a Plan fan_out/workflow call resolves to a refusal carrying `fanOutNoScratch`'s text.
**Acceptance:** `go build ./... && go test -race -count=1 ./internal/agent/`
**Commit:** `fix(agent): plan mode offers fan_out beside sub_agent`

## 42. Plan mode runs a recipe's script stages confined to the workflow folder

**What:** Depends on items 20, 41. Fixes a run finding. `runScriptCall` (recipe.go) puts a script stage through the terminal's own Resolution, and Plan refuses every `ClassSubprocess` call. So the shipped `audit` recipe's `split.sh` is blocked in Plan, which is also the default mode of headless runs and daemon firings (items 35, 36). That contradicts ADR 0087 D4 ("a read-only audit works in Plan mode"). Decided here: a confine-to-the-workflow-folder rule for engine-built script calls only, recorded as a dated amendment of ADR 0087 D4/D6.
**Goal:** in Plan mode, a recipe's script stage runs inside the Confinement box when the backend can confine, with the workflow folder as its only writable root. A write anywhere else (the workspace included) fails inside the sandbox. With no confinement backend the stage is refused with a reason saying Plan runs a recipe script only inside a sandbox. A model's own `terminal` call in Plan stays refused, and every other mode resolves script stages exactly as at the header base.
**Approach (as read at 28cd8ee5):** `resolutionInput` gains `workflowScriptDir string`, set by `runScriptCall` alone (the `ScriptSpec.Dir` workflow folder). The Plan row of `resolveLadder` returns `resolveConfine` for a `ClassSubprocess` call carrying it when `fsConfineAvailable`, else refuses with the new reason. The box is narrowed in `finishConfine` for that call: `WorkspaceRoot` and `ScratchDir` are the workflow folder, `WritablePaths` is empty, and `NetworkAllow` is kept. That makes it the "deliberately NARROWER box" `Config.ConfinementBox` documents, and the divergence stays visible in one line. The confine fallback for that call is a refusal, never an unconfined run or a gate.
**Regression guard.** The dangerous-action guard still runs first (`guards.PreExecute` in runScriptCall). A Tier-2 force-approval in Plan refuses rather than gates. `TestRecipe_AScriptStageIsRefusedInPlanMode` is rewritten into both halves (confined run with a backend, refusal without). `TestPlanToolMenuAgreesWithTheLadder` is unchanged, because the model's menu never offers terminal. The ADR amendment is a dated `> **Amended 2026-09-27.**` note under D4, and every sentence saying Plan is read-only except scratch, or that a subprocess is refused in Plan, gets the rule (`grep -rn -i "refused in Plan\|read-only except\|read-only, except" docs/manual CONTEXT.md README.md internal/agent/*.go`; resolution.go:247-249 included). The narrowed box and the refusal fallback key on `in.mode == domain.ModePlan && in.workflowScriptDir != ""`, never on the field alone. applyOverlays' Tier-2 branch (resolution.go:537-548) is where Plan + workflowScriptDir refuses, before the forced-gate upgrade. Ratified design call (owner, 2026-09-27): this item explicitly amends ADR 0012 — add a dated `## Amendment (2026-09-27)` to `docs/adr/0012-confinement-attaches-to-blast-radius-and-confine-to-workspace-flag.md` (beside its 2026-09-14 amendment (a), 0012:396-399) stating that the one further loosening is an engine-built recipe script stage in Plan mode, run confined so it can write only its own workflow folder; every other subprocess route into scratch stays refused in Plan. ADR 0087's amendment cites it. This supersedes amendment (a)'s "Nothing else loosens" for that one route.
**Files:** internal/agent/resolution.go, internal/agent/recipe.go, internal/agent/dispatch.go, internal/agent/recipe_test.go, internal/agent/resolution_test.go, internal/agent/confine_linux_test.go, internal/agent/loop.go, docs/adr/0087-the-engine-runs-workflows-the-model-or-a-recipe-asks-for.md, docs/adr/0012-confinement-attaches-to-blast-radius-and-confine-to-workspace-flag.md, docs/manual/headless.md, docs/manual/daemon.md, docs/manual/configuration.md, docs/manual/commands.md, CONTEXT.md
**Read first:** internal/agent/recipe.go — runScriptCall, recipeScripts.RunScript, recipeScripts.render, newRecipeRunner; internal/agent/resolution.go — resolveLadder (Plan row), applyOverlays (Tier-2 branch), finishConfine, confineFallback, resolutionInput;
  internal/agent/dispatch.go — resolutionInput builder, confinementBox, fsConfinementAvailable, executeConfine, executeConfineFallback; internal/domain/confinement.go — ConfinementBox, Config.ConfinementBox; internal/tools/console_confine_linux_test.go — TestMain, runConfinedExecChild;
  internal/skills/shipped/audit/SKILL.md — split stage; internal/skills/shipped/audit/split.sh — main, list_scope; internal/agent/recipe_test.go — TestRecipe_AScriptStageIsRefusedInPlanMode, scriptRecipe
**Tests:** a resolution table: Plan + script stage + backend gives Confine with the narrowed box; Plan + script stage + no backend refuses with the exact reason; Plan + a model terminal call still refuses; ask-before/allow-edits/auto script stages are unchanged; Plan + a Tier-2 match refuses. Where the host confines (`platform.NewConfiner()`, skipped when `Capabilities().FSWrite` is false; a new internal/agent/confine_linux_test.go `TestMain` handles `platform.ConfinedExecSentinel()` as internal/tools/console_confine_linux_test.go:25-50 does), a Plan script that writes into the workflow folder succeeds and one that writes into the workspace fails. An Auto script stage keeps today's workspace+scratch box and forced-gate demote. The shipped audit's split stage runs in Plan on a fixture workspace.
**Acceptance:** `go build ./... && go test -race -count=1 ./internal/agent/ && grep -q "Amended 2026-09-27" docs/adr/0087-the-engine-runs-workflows-the-model-or-a-recipe-asks-for.md && grep -q "Amendment (2026-09-27)" docs/adr/0012-confinement-attaches-to-blast-radius-and-confine-to-workspace-flag.md`
**Commit:** `fix(agent): plan mode runs recipe scripts confined to the workflow folder`

## 43. A workflow's item runs nest under the block that started them

**What:** Recast at the regression check (2026-09-27). Depends on item 22. Fixes a run finding (item 22's NOTES). A recipe launch's item children emit their events under the synthetic `recipe-<id>-<turn>` call (`launchRecipe`, recipe.go). No transcript entry heads that call (`entry.headsRunFor` matches sub_agent cards only), so the children's entries land as unheaded depth-1 entries after the workflow block.
**Goal:** every workflow item run nests under the block of the call that spawned it: the workflow block for a recipe launch, the fan_out call card for a fan_out. The nesting holds live, collapsed and expanded, and after a session save and reopen. Sub_agent heads and groups render exactly as before.
**Approach (as read at 28cd8ee5):** `domain.WorkflowPhaseEvent` gains `Call string`, the id of the call its item children are bracketed under, which `workflowObserver` fills for every workflow. eventjson's `workflowPhaseData` gains an additive `call` member, and headless.md's workflow_phase row lists it. The workflow block's entry records it as `callID` (persisted through session's existing `CallID`). A new `entry.headsWorkflowRuns(run)` answers true for an `entryWorkflow` or a `fan_out` card whose `callID == run.spawn && depth == run.depth-1`. The span/collapse walks consult it through a separate head lookup (see the guard); `runHeadAt` does not. It matches on call id alone, since one head carries many runs.
**Regression guard.** A background workflow's events stay out of the transcript (item 28's rule (c)). `TestEntryKindRulesAnswerForEveryKind`, `TestEntryKindPersistedNamesAreUnique`, the sub-agent group goldens and `TestManualListsEveryEventLineKind` stay green. The kinds count is unchanged (a field, not a kind). internal/tui/doc.go and layout.md say a workflow block heads its item runs. Adopt both reviewer guards as written — runHeadAt/headsRunFor stay sub_agent-only for the phase, name and result folds (addSubAgentPhase, addSubAgentName, breadcrumbTrail); runEnd, the span/collapse walks, resolveBlock (render.go) and continuesOpenRun (transcript.go) take a separate head lookup that asks headsRunFor first and headsWorkflowRuns only when nothing matches; Files gains internal/agent/recipe.go, internal/agent/background.go (backgroundRun stores its `workflow-<id>` call so observeWorkflow's background caller carries it), internal/tui/render.go and internal/tui/doc.go. Re-check (2026-09-27): `subAgentSpan`'s gate widens to answer for a head that heads workflow runs (else runEnd = head+1 and an item run's later entries land in reverse order); `subAgentFramed` stays `headsRun`-keyed. The workflow block's span is never elided — it yields to layout.md:1211 ("The block paints one way and never collapses"; `entryWorkflow` carries no block state). The fan_out card stays born collapsed, and that fold hides only its own body: its item runs paint inline and railed beneath it as they show today (a head with many runs has no run view). Keep `observeWorkflow(runner, turn, name)` and set the call on the observer it returns.
**Files:** internal/domain/events.go, internal/agent/workflowcall.go, internal/agent/workflowcall_test.go, internal/agent/recipe.go, internal/agent/background.go, internal/eventjson/encode.go, internal/eventjson/encode_test.go, docs/manual/headless.md, internal/tui/workflowblock.go, internal/tui/workflowblock_test.go, internal/tui/transcript.go, internal/tui/subagentblock.go, internal/tui/render.go, internal/tui/doc.go, layout.md
**Read first:** internal/tui/transcript.go — runEnd, entry.headsRunFor; internal/tui/subagentblock.go — subAgentSpan, subAgentFramed;
  internal/tui/render.go — resolveBlock; internal/tui/entrykind.go — entryKindRules (entryWorkflow row); internal/agent/workflowcall.go — observeWorkflow;
  internal/agent/workflowcall_test.go — TestWorkflowObserver_ReportsAWaitingQuestionAndAFailure
**Tests:** a recipe launch with two items: both runs' entries sit inside the workflow block's span live and after a save → reopen round trip. The same for a fan_out call's two items under its card. A sub_agent group interleaved with a workflow keeps its own heads. The fan_out card keeps its own run id and pairs its ToolResultEvent (it closes; no orphan block), and no item child's phase, name or result folds into it. Two entries of one item run keep their arrival order. Collapsing a sub_agent head still elides its run (resolveBlock), while the workflow block and a collapsed fan_out card never elide their item runs; a host note between item runs does not split the span (continuesOpenRun). A background workflow's phase events carry its `workflow-<id>` call. The encode round trip carries `call`.
**Acceptance:** `go build ./... && go test -race -count=1 ./internal/tui/ && go test -race -count=1 ./internal/eventjson/ && go test -race -count=1 ./internal/agent/ && go test -race -count=1 -run 'EventLine' ./cmd/apogee/`
**Commit:** `fix(tui): a workflow's item runs nest under the block that started them`

## 44. `inputs:` is read only beside `recipe:`

**What:** Depends on item 18. Regression from 29ef8ff6 (item 18): any SKILL.md header with an `inputs:` key now fails to load when that key is malformed, carries unknown keys or fails the strict YAML parse, even with no `recipe:`. A SKILL.md shared with another tool that shapes `inputs:` differently loaded at the header base and no longer does. No ADR requires `inputs:` without a recipe, and a Recipe is what declares inputs (ADR 0065's amendment, ADR 0087 D6).
**Goal:** a SKILL.md whose header carries `inputs:` but no `recipe:` loads exactly as at the header base: `Skill.Inputs` is empty, the key is ignored, and a strict-parse failure still falls to the lenient scan. `inputs:` is decoded and validated strictly (all of item 18's refusals) only when `recipe:` is present.
**Approach (as read at 28cd8ee5):** In `parseWithFrontmatter`, call `parseInputs` only when `fm.Recipe` is set (not `Kind == 0` and not `isNullNode`). `recipeKeyRe` (parse.go) keys the no-lenient-fallback rule on a `recipe:` key alone. The internal/skills/doc.go parse paragraph and the `parseFrontmatterFields`/`parseInputs` comments are restated.
**Regression guard.** Every refusal row in `TestParseSkillRecipeRefused` carries a recipe stage and stays refused, "an inputs block that is not YAML" included. The shipped `audit` still loads with its two inputs (`TestShippedAuditRecipeLoads`). Every sentence that ties an `inputs:` refusal to anything but a recipe is restated — rule: `grep -n 'inputs' internal/skills/*.go` plus the run's notes-18.md (its pending CHANGELOG text, :17) and item 18's NOTES/guard; the Skill.Inputs comment (skill.go:63) says inputs are read only beside a recipe.
**Files:** internal/skills/parse.go, internal/skills/parse_test.go, internal/skills/doc.go, internal/skills/skill.go
**Read first:** internal/skills/parse.go — parseWithFrontmatter, parseInputs, parseRecipe, parseFrontmatterFields, recipeKeyRe, isNullNode; internal/skills/parse_test.go — TestParseSkillRecipeRefused, TestParseSkillWithoutRecipeHasNone;
  internal/skills/skill.go — Skill.Inputs; internal/skills/doc.go — package parse paragraph; internal/skills/catalog.go — Catalog.Recipe
**Tests:** these load, each with no inputs and body intact: `inputs: scope` with no recipe, `inputs: {a: {type: string}}` with no recipe, an unknown input key with no recipe, and a no-recipe header whose `inputs:` line breaks the strict parse (lenient scan). The item-18 refusals still fire beside a recipe.
**Acceptance:** `go test -race -count=1 ./internal/skills/`
**Commit:** `fix(skills): a skill without a recipe ignores its inputs key`

## 45. split.sh handles paths with spaces and glob characters

**What:** Recast at the regression check (2026-09-27). Depends on item 23. Fixes a run finding. `internal/skills/shipped/audit/split.sh` word-splits and glob-expands file paths: `xargs cat` / `xargs grep` over path lists, `for path in $SCOPE`, `for dir in $(…)`, and `set -- $(…)` over part names. So a file named `a b.go`, `x*y.go` or `[z].go` is miscounted, dropped or expanded into other files.
**Goal:** for a tree whose file and directory names contain spaces, `*`, `?` or `[`, split.sh puts every listed file in exactly one part's scope.txt, `src=` counts them, and each part's line count equals `wc -l` over its files. Only a name containing a newline is out of scope, and it is skipped. The scope input still splits on whitespace into its entries, but no entry is glob-expanded.
**Approach (as read at 28cd8ee5):** `set -f` only around the word-splitting sites (as `list_scope` already does); split.sh's own `$W`/`$RUN` globs keep pathname expansion. Path lists are read one line at a time with `while IFS= read -r` instead of `xargs` (line counts via `wc -l < "$file"`, the concurrency scan via `grep -lE -- … "$file"` per file). Directory, part and group name lists are read with `while IFS= read -r`, not `for … in $(…)`: those names come from workspace directory names, not a safe alphabet. The script stays POSIX sh.
**Regression guard.** `TestAuditSplitFitsPartsUnderTheBudget` and `TestAuditSplitTakesItsBoundFromTheWindow` stay green with unchanged expectations. A re-run with the same scope still skips the re-split (the `layout.txt` check). Adopt the reviewer guards — keep glob expansion for split.sh's own $W/$RUN globs and the part-* rm (scope `set -f` to the word-splitting sites, as list_scope already does); read every directory/part/group name list with `while IFS= read -r` instead of `for … in $(…)`, with a fixture that exceeds PART_LINES so the directory mechanics run; record the literal-`*` scope test as a pin, not a bite; add a step that drops listed scope lines naming no regular file (`[ -f "$line" ]`), which is what makes the Goal's newline-name "skipped" claim true; skip the `x*y.go` fixture on Windows. Re-check (2026-09-27): `list_scope` lists with `git -c core.quotePath=false ls-files -z … | tr '\000' '\n'`, since git's default quoting prints `café.go` as `"caf\303\251.go"` and `q"t.go` as `"q\"t.go"`, which the `[ -f ]` step would drop; a newline name splits into fragments that step drops. The `*`, `?`, `"` and newline fixtures are skipped on Windows by one rule.
**Files:** internal/skills/shipped/audit/split.sh, internal/skills/load_test.go
**Read first:** internal/skills/shipped/audit/split.sh — list_scope, list_source_lines, split_by_top_level_dir, write_groups;
  internal/skills/load_test.go — runAuditSplit, auditFixture, TestAuditSplitFitsPartsUnderTheBudget; internal/skills/shipped/audit/SKILL.md — split stage `run:`
**Tests:** a fixture tree holding `a b.go`, `x*y.go`, `q?.go` (both skipped on Windows), `[z].go` and a `dir with space/` subtree, sized above PART_LINES so the directory split runs: every file appears once in exactly one part's scope.txt, `src=` counts them all, and the per-part line counts match `wc -l`. A literal `*` in the scope input names no other file (a pin: the SCOPE loop already runs under `set -f`). A file whose name holds a newline is absent from every scope.txt, and no scope line names a non-regular file. One git-listed case (a fixture that is a git repository, run without `runAuditSplit`'s `GIT_DIR=no-git` override) holding `café.go` and `q"t.go` puts both in a scope.txt.
**Acceptance:** `go test -race -count=1 -run AuditSplit ./internal/skills/ && sh -n internal/skills/shipped/audit/split.sh`
**Commit:** `fix(skills): split.sh handles paths with spaces and glob characters`

## 46. Workflow item runs' token spend reaches /usage and the session record

**What:** Depends on items 28, 43. Regression from 62d76268 (item 28), never deferred: `Update`'s eventMsg case hands every event `backgroundWorkflows.owns` claims to `foldBackgroundEvent` (internal/tui/workflow.go), which skips `transcript.applyUsage`. So a sub_agent run a background item spawned, whose head stood in the transcript at 28cd8ee5 and counted toward `/usage` and `delegateUsageTotal` (and so `session.Meta.DelegateUsage`), no longer counts. The item runs themselves never counted, background or foreground: `applyUsage` folds a delegated reading only into an open sub_agent head (`openSubAgentHead`), and a fan_out card, a recipe's workflow block and a background workflow are none, so a blocking fan_out's spend is missing too. One fold closes both.
**Goal:** every token a workflow item run reports, or a run it spawned reports, foreground or background, counts exactly once in the `/usage` pane's session row and in the delegate usage a saved record stores. `/usage` shows one row per workflow that reported a count, labelled with the workflow's name, after the sub_agent rows, live and after a reopen. A record saved after the workflow ended reopens with the same session total. The status gauge, the depth-0 accounting and the sub_agent rows are unchanged, and a background workflow's events still reach no transcript entry but its finish line.
**Approach (as read at 1fb91ac0):** A workflow's spend is kept per run, latest-wins (each child keeps its own cumulative sum, as `applyUsage` already treats a head's), and summed per workflow; per-run maps are copied before they grow (ADR 0011). Foreground: when `openSubAgentHead` finds no head, `applyUsage` folds the reading into the entry item 43's `headsWorkflowRuns` answers for (the recipe's workflow block or the fan_out card). Background: `foldBackgroundEvent` folds a `UsageEvent` into its workflow's `backgroundWorkflow` view (`workflowOf` already maps a run a background run spawned), and `foldBackgroundPhase` stamps the view's sum and name onto the finish line's entry when the workflow ends. The sum persists through the entry's existing usage fields (`session.Entry.Usage*`, `toWireEntry`), so no record field is added. `delegateUsageHeads`/`delegateUsageTotal` (usage.go) walk every entry carrying a workflow's spend beside the sub_agent heads, plus the live background views, so `snapshotPayload`'s `delegateUsage` and the pane agree; `usageSubAgentRows` gains the workflow rows.
**Regression guard.** The existing usage tests (sub_agent rows, maintenance readings, the resumed-record fallback `Model.delegateUsage`) stay green unchanged. A reading an open sub_agent head takes never also counts toward a workflow. Claimed background events still skip `foldStats`, so `m.usage` and the gauge never move on them — but `foldBackgroundEvent` folds a claimed `UsageEvent`'s `ServedModel` into `m.servedModels` (only the half ahead of `foldStats`' depth guard, fold.go), which 62d76268 lost and the `/usage` `served:` line and `Meta.ServedModels` read (commands.md:33, sessions.md:101 "a sub-agent's included"). Doc rule: every manual sentence naming what the delegate half or the `/usage` rows sum says workflows count among the delegates — found with `grep -n -i "sub-agents\? reported\|one per sub-agent\|sub-agent's included" docs/manual/*.md` (commands.md:33, sessions.md:95). Late check (2026-09-28): `m.workflows` survives `resetSessionView` and saveAtIdle's closing record already counted each live view's sum, so at every session boundary (`resetSessionView`, `resumeLoaded`) each live background view's per-run readings are rebased and only spend past the boundary is stamped on its stop or finish line — this yields to commandrun.go:376-387 and sessions.md:108-110 (the fresh session starts from zero). The workflow's name reaches a reopen through `toWireEntry`/`fromWireEntry` attaching `Tool` (Target = the workflow's name) to an `entryWorkflow` or `entryNote` carrying usage (`workflowView.name` is never persisted, and `Tool` rides only entryToolCall/entrySchedule today); the `session.Entry` members are unchanged, so transcriptbridge_test's "the wire structs carry exactly the members that were decided on" holds. The foreground fold rests on item 43's head lookup (`headsWorkflowRuns`, absent at 1fb91ac0).
**Files:** internal/tui/workflow.go, internal/tui/workflow_test.go, internal/tui/transcript.go, internal/tui/usage.go, internal/tui/usage_test.go, internal/tui/transcriptbridge.go, internal/tui/transcriptbridge_test.go, internal/tui/commandrun.go, internal/tui/sessions.go, docs/manual/commands.md, docs/manual/sessions.md
**Read first:** internal/tui/workflow.go — foldBackgroundEvent, foldBackgroundPhase; internal/tui/transcript.go — applyUsage; internal/tui/usage.go — usageSubAgentRows, delegateUsageTotal;
  internal/tui/transcriptbridge.go — toWireEntry; internal/tui/commandrun.go — resetSessionView; internal/tui/fold.go — foldStats
**Tests:** a background workflow whose two item runs report cumulative readings (a later reading of one replacing its earlier one), and one of whose items spawned a sub_agent run that reports too: `/usage` lists one row named for the workflow with their sum, the session row adds it, `snapshotPayload`'s `delegateUsage` equals the pane's delegate sum, and the depth-0 `m.usage` is untouched. After the workflow finishes, save → decode → reopen keeps the row, its workflow name and the session total. A blocking fan_out's two item runs and a recipe launch's item run count the same way. A sub_agent head's reading counts once, as before. A claimed background `UsageEvent`'s `ServedModel` reaches the `served:` line. `/clear` answered `y` and answered `n` while a background workflow that reported spend runs: the fresh session's `/usage` delegate sum starts at 0, and its stop or finish line carries only spend past the boundary. Each new test fails against the pre-item tree.
**Acceptance:** `go build ./... && go test -race -count=1 ./internal/tui/`
**Commit:** `fix(tui): workflow item runs' token spend reaches /usage and the session record`

## 47. `make lint` passes over commandrun_test.go

**What:** Depends on item 34. Regression from ad3fee2b (item 34), never deferred: `TestABgOnlySessionIsSavedAtQuitAndResumedOnStart` (internal/tui/commandrun_test.go) assigns the second `WindowSizeMsg` fold to `resumed` and never reads it, so `make lint` fails with `ineffectual assignment to resumed (ineffassign)`. `make lint` passed before that commit, and the closeout's `make check` runs it.
**Regression guard.** The Goal and Acceptance are scoped to the package the item touches — the Makefile's golangci-lint run over `./internal/tui/...` (the same `$(GOLANGCI_LINT)` binary `make lint` uses) reports no issue, plus the named test; a lint finding in another package belongs to the item that introduced it and is filed as a FOLLOW-UP, never fixed under 47.
**Goal:** the Makefile's golangci-lint (`$(GOLANGCI_LINT)`) run over `./internal/tui/...` reports no issue, and `TestABgOnlySessionIsSavedAtQuitAndResumedOnStart` still proves the stored set resumes exactly once across two folds of a bound, idle engine.
**Approach (as read at 1fb91ac0):** Keep the second fold, which is what shows the resume is not repeated, and drop only the dead assignment of its result. The assertion on `eng.boundaryKeeps()` is unchanged.
**Files:** internal/tui/commandrun_test.go
**Read first:** internal/tui/commandrun_test.go — TestABgOnlySessionIsSavedAtQuitAndResumedOnStart; internal/tui/model_test.go — step; Makefile — lint, GOLANGCI_LINT; .golangci.yml — linters.default
**Tests:** `TestABgOnlySessionIsSavedAtQuitAndResumedOnStart` passes; the lint run below is the bite (it fails on the pre-item tree).
**Acceptance:** `go run github.com/golangci/golangci-lint/v2/cmd/golangci-lint@v2.13.2 run ./internal/tui/... && go test -race -count=1 -run 'TestABgOnlySessionIsSavedAtQuitAndResumedOnStart' ./internal/tui/` (the lint command is the Makefile's `$(GOLANGCI_LINT)` at its pinned `GOLANGCI_LINT_VERSION`; use the Makefile's pin if it has moved)
**Commit:** `fix(tui): drop the ineffectual assignment make lint flags in commandrun_test.go`

## 48. A delegate is offered neither fan_out's background nor the workflow tool

**What:** Depends on item 27. Fixes a run finding (item 27). `withoutSeatChoice` (internal/agent/subagent.go) rebuilds a child's fan_out only when it publishes `run_on`, and keeps `background` when it does, while `defaultSubAgentTools` withholds nothing but `childWithheldTools`. So every child built through it (a sub_agent child and a workflow item child alike, `newChildAgentOn`) is offered `background` and the `workflow` tool whenever its parent is. On a delegate `offersBackground` is false, so `background` runs blocking and `workflow` is refused (`workflowControlDelegate`): the schema offers a knob the engine ignores, which the function's own doc names as the lie it exists to prevent.
**Goal:** no child roster carries the `workflow` tool, and a child's fan_out, when it has one, publishes neither `background` nor `run_on`. The top-level Agent's menu and schemas are unchanged.
**Approach (as read at 1fb91ac0):** `withoutSeatChoice` rebuilds fan_out as the plain variant (`tools.NewFanOut()`) whenever the parent's publishes `run_on` or `background`. `defaultSubAgentTools` withholds `tools.WorkflowToolName` from every child in a list of its own beside `childWithheldTools`, with its reason (a background workflow belongs to the top-level Agent, ADR 0089 D1/D4). The delegate refusal and `offersBackground`'s `isDelegate` check stay as defence in depth. Every comment that states what a child's roster withholds or how its fan_out is narrowed is restated (the rule and its grep are in the guard).
**Regression guard.** `TestWorkflowCall_AChildBelowTheBoundGetsFanOutWithoutRunOn` pins "the variant without run_on that keeps background" and is rewritten to want neither argument. `TestWorkflowControl_ADelegateIsRefused` and the depth-0 menu goldens (`TestContextCostGolden`) stay green. A sub_agent call whose `tools` names `workflow` passes `requestedChildTools`' unknown-name check (the parent holds it) and is dropped by the intersection, as `ask_user` is. Late check (2026-09-28): both fan_out cases set `cfg.Delegation.MaxDepth = 2` (under the default bound `defaultSubAgentTools` drops fan_out at subagent.go:1898, so the case would be vacuous) and first assert the child roster holds fan_out, then that it offers neither `OffersBackground()` nor `OffersSeatChoice()` and has no `background`/`run_on` in its schema. The closed grep is replaced by a rule: restate every comment that states what a child's roster withholds or how its fan_out is narrowed (`grep -n "childWithheldTools\|withoutSeatChoice\|human-seat tools\|variant without\|do not arrive verbatim" internal/agent/*.go` — childWithheldTools, defaultSubAgentTools, requestedChildTools, withoutSeatChoice, orientation.go:234, loop.go's toolMenu). Items 41 and 48 both restate toolMenu's doc comment (loop.go); whichever lands second merges it.
**Files:** internal/agent/subagent.go, internal/agent/orientation.go, internal/agent/loop.go, internal/agent/workflowcall_test.go, internal/agent/subagent_test.go
**Read first:** internal/agent/subagent.go — withoutSeatChoice, defaultSubAgentTools, childWithheldTools, requestedChildTools; internal/agent/workflowcall.go — offersBackground;
  internal/agent/workflowcall_test.go — TestWorkflowCall_AChildBelowTheBoundGetsFanOutWithoutRunOn; internal/agent/subagent_test.go — TestSubAgent_ChildNeverHoldsTheHumanSeatTools; internal/agent/loop.go — toolMenu
**Tests:** a parent registering fan_out with `Background` (and, in a second case, `SeatChoice` too) plus `tools.NewWorkflow()`, both with `cfg.Delegation.MaxDepth = 2`: the child roster from `defaultSubAgentTools` holds fan_out and has no `workflow`, and its fan_out reports `OffersBackground()` and `OffersSeatChoice()` false with no `background` or `run_on` in its schema. A workflow item child's roster has no `workflow`. A sub_agent call with `tools: ["workflow", "read_file"]` spawns a child holding `read_file` and not `workflow`, with no refusal. The parent's own fan_out still publishes `background`.
**Acceptance:** `go build ./... && go test -race -count=1 ./internal/agent/`
**Commit:** `fix(agent): a delegate is offered neither fan_out's background nor the workflow tool`

## 49. /workflows answers a waiting question

**What:** Depends on items 30, 31. Fixes a run finding (item 31). The ratified Background question call says a waiting question opens "when the user is idle or opens `/workflows`", but the `/workflows` pane neither shows nor answers one, and because the pane is modal, `canWake` (`modalPaneOpen`) holds the idle offer (`offerWaitingPrompt`) for as long as it stays open. A question esc dismissed waits until the next Exchange ends (`reofferDismissed`) with no route back. Decided here: `/workflows` is the explicit route, and it opens a question at idle only, never mid-Turn, where the decision panes belong to the running Exchange (item 30's guard).
**Goal:** a workflow with a waiting approval or question reads `waiting for you` as its state on the `/workflows` list row, and its detail's hint offers `^a answer`. In that detail `^a` closes the pane and opens the workflow's oldest waiting prompt, a dismissed one included, in the approval or ask pane, through the same route as item 30's idle offer; the answer resumes the workflow and returns to idle. While the session is not idle as `canWake` reads it with the /workflows pane taken as closed (busy, errored, a `/bg` launch or `/sessions` load in flight, quitting, a write or interjection pending), `^a` opens nothing and notes `a question opens only while the agent is idle — press ^a again once it is`. The idle offer is otherwise unchanged, its hold while `/workflows` is open included.
**Approach (as read at 1fb91ac0):** `workflowsVerb` gains `workflowAnswerKey = "ctrl+a"` beside `workflowStopKey`/`workflowRerunKey`/`workflowSaveKey`, gated on `canWake`'s conditions with the /workflows pane taken as closed (see the guard), not the narrower check `^r` uses. It reads `m.eng.WorkflowPrompts()`, takes the first whose `Workflow` is the shown id whatever `workflows.dismissed` says, clears that one dismissal, closes the pane and calls `openWorkflowPrompt`. The list's state reads the folded `backgroundWorkflows` view's `waiting` count (never the Engine at render), in `workflowState`'s place for a running background workflow; the detail hint adds `^a answer` only when the shown workflow waits.
**Regression guard.** `^x`, `^r` and `^s` and their notes are unchanged (`TestKeyClaimOrderMatchesTheDocumentedPrecedence`, the workflows_test.go chord tests). esc on a prompt opened by `^a` dismisses it back to the queue as item 30's esc does, and the answer never calls `resumeRunning`. docs/manual/commands.md's `/workflows` and `/bg` rows and layout.md's background-workflows readout name the route — layout.md:1590-1592's esc-dismissed prompt waiting "until the next exchange ends" is restated (`grep -n "waiting for you\|as soon as you are idle\|sends it back" docs/manual/*.md layout.md`). Late check (2026-09-28): `^a` is gated on `canWake`'s conditions with the /workflows pane taken as closed — `state == stateIdle`, `!sessionLoading`, `!bgLaunching`, `!quitting`, no pending writes or held interjections — else it writes the note; `m.busy() || m.bgLaunching` (the `^r` gate, workflows.go:339) misses a `/sessions` load, a queued write and `stateErrored` (errored is not busy, model.go:68).
**Files:** internal/tui/workflows.go, internal/tui/workflows_test.go, internal/tui/workflow.go, docs/manual/commands.md, layout.md
**Read first:** internal/tui/workflows.go — workflowsVerb, workflowState, workflowsDetailHint; internal/tui/workflow.go — canWake, openWorkflowPrompt;
  internal/tui/workflows_test.go — TestWorkflowsViewCtrlRRerunsTheFailedItems; internal/tui/seam_test.go — fakeEngine.WorkflowPrompts; layout.md — "And the background workflows' readout"
**Tests:** with a background ask waiting, the list row reads `waiting for you` and the detail hint shows `^a answer`; `^a` at idle closes the pane and opens the ask pane under the workflow's line, and the answer reaches `AnswerWorkflowPrompt` and leaves the TUI idle. A prompt esc dismissed opens again through `^a` before any Exchange ends. An approval opens in the approval pane the same way. `^a` mid-Turn opens nothing and writes the exact note; so does `^a` in `stateErrored`. `^a` on a workflow with nothing waiting does nothing.
**Acceptance:** `go build ./... && go test -race -count=1 ./internal/tui/`
**Commit:** `fix(tui): /workflows answers a waiting question`
