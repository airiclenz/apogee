# Stop one sub-agent, and keep named delegations continuable for the session — plan

**Goal:** The human stops one running delegation with `^x` while the parent's Turn goes on, and the parent receives a folded partial result. Every named delegation stays continuable with `continue:` for the whole session: saved with it, cut by forks, dropped by `/clear`, restored by a cancelled Turn's rollback. Interjection and stop address a child by its run id.
**Date:** 2026-09-25
**Status:** unexecuted
**sized for:** ~200k-context host
**base:** 27552c2d
**Closes:** apogee-single-delegation-stop; apogee-session-delegate-retention

**Sources:**
- `docs/adr/0086-a-delegation-is-stopped-singly-and-a-named-one-stays-continuable-for-the-session.md` (the spec; D1–D5)
- `docs/handoffs/2026-09-24 - 00 - sub-agent-control-handoff.md` (code map; its retention line refs are partly stale)
- `docs/adr/0013-*.md` §5, `docs/adr/0022-*.md` 2026-09-18 addendum, `docs/adr/0039-*.md` D4 + 2026-09-24 amendment, `docs/adr/0063-*.md` D1/D4, `docs/adr/0068-*.md`, `docs/adr/0072-*.md`, `docs/adr/0031-*.md`
- `CONTEXT.md` — **Sub-agent**, **Step cap**, **Delegation name**, **Retained delegation**, **Stop (a delegation)**, **Interjection**
- `docs/design/test-drivers.md`

**Ratified design calls** (owner, 2026-09-25, unless marked writer):
- **Spec:** ADR 0086 D1–D5 bind every item; no item re-opens them.
- **Continuation retention:** a `continue:` appends its round to the entry it continued, under the inherited name, whatever its outcome — even when the call named nothing.
- **Seed layout:** rounds are selected newest-first into the 4096-token budget and rendered chronologically after the task, with `[N earlier rounds omitted]` first when any were dropped; the newest round is always laid in whole, even over budget.
- **Bound wording:** engine result heads (`[delegate stopped at its …]`) stay byte-identical; every human-facing text (TUI verdicts, undelivered notes, the `sub_agent` schema description, docs) says "capped" for an engine bound; "stopped" means the human's stop only.
- **Stop wording** (owner, 2026-09-25, revised after the regression check): row verdict `stopped by you` in the step-cap tone, no ✓; undelivered note `<name> was stopped by you before your message landed`, capped `<name> was capped before your message landed`; `domain.UndeliveredReason` / headless `data.reason` `"stopped"`.
- **Cancel label** (owner, 2026-09-25): the whole-Turn cancel's human-facing labels say cancel — `esc×2 cancel`, `press esc again to cancel…`; nothing model-facing changes.
- **Queued stop text** (writer, from D4 and the unstarted-result precedent): a pooled child stopped before it started returns the error-shaped `sub-agent not started: the user stopped it before it started; delegate again if the task is still needed`; the folded stopped result is non-error, like a capped one.
- **Conversation point** (writer, from D3): each round records the call id of the tool call that spawned it; a fork drops the rounds whose spawning result lies in the cut tail, and an entry left with no rounds.
- **`^x` reach** (writer, from D5): `^x` follows the run view's and block cursor's existing gates (a standing ask/approval pane owns keys first); on a finished run or a run with no run id it does nothing; the `· ^x stop` hint shows only while the viewed run is running.

**Standing requirements:**
- skills: coding-standards
- Bubble Tea v2: keys are `tea.KeyPressMsg` matched on `msg.String()` (`"ctrl+x"`); the `Model` is copied by value, so no no-copy type is held by value (ADR 0011).
- ADR 0031: the stop is an engine call; `ctx` stays the only cancellation mechanism (a per-child ctx is a child of the parent's); the engine stays wire-silent.
- Floor invariant: the model-facing changes are structural and ship on, on under Bypass; nothing here is a Reaction.
- Any authorised deviation from item text lands as a dated NOTES line under the item.

**Out of scope:**
- Asynchronous delegation (ADR 0086 option A); the parent stopping a child.
- `apogee-2un` (a Turn cancel keeps finished siblings).
- A CLI or headless surface for the stop.
- Renaming the engine's `[delegate stopped at its …]` result heads.
- `VERSION` bumps.

**Regression check (2026-09-25, 27552c2d):**
- 1: guard folded (the jsonl commit is checked; the id count reads the `"id":` rows, since an unanchored grep also matches `apogee-2un`'s row and prints 3).
- 2: guard folded (naming_test lookup by run id; run id from the resolved head; call-ID prose swept by rule).
- 3: guard folded (stopped branch only when the stop cut the Run short); writer addition folded (the folded stopped result is non-error; `classifyDelegation` checks the stop before its IsError cases).
- 4: guard folded (pool width 2; ledger row at the queued skip; queued set filled before `runPool`, marked under one lock); recast (writer decision: the queued-stop result takes the error-shaped unstarted form, `sub-agent not started: the user stopped it …`, classified `stopped` before `IsError && !ran`).
- 5: recast (owner decision: agent.go ErrorEvent lines say capped, undelivered-note wording, cancel labels); guards folded (✓ withheld, head-derived pins, one-line stopped text not promoted); writer decision folded (the stopped verdict recognises item 4's queued-stop content as item 4 now spells it; `entry.neverStarted` already reads it through its `sub-agent not started:` prefix).
- 6: guard folded (owner decision: a stop withdraws a standing pane; `helpKeyStopRun`, e2e hint, nested cursor claim, width fallback, gauge slot, driver_test.go).
- 8: guard folded (e2e + state_test pins; a renamed continuation keys under the name its continue line spells; set compare; deterministic estimator; cancel retains nothing).
- 9: guard folded (owner decision: restoreState empties retention); yields to ADR 0086 D1 and Consequences — only the `continue` description changes.
- 10: guard folded (owner decision: the load replaces item 9's clear; restored strings through the ingestion guard; duplicate-id tie-break; comment sweep; roster/path per round).
- 11: guard folded (owner decision: depends on item 10, restored copies reset to the loaded set; turn_test.go observer).
- 12: guard folded (owner decision: previous-attempt label swept; whole-entry read, joined-text gate, verbatim engine heads, ADR 0039 and 0013's 2026-09-18 amendment).
- 13: guard folded (owner decision: stop/cancel split with the renamed labels; bound wording gated, excluding layout.md's unrelated "stopped at its last word").
- 14: guard folded (owner decision: previous-attempt label and cancel labels swept; gate replaced, since `rest of the exchange` hits the unrelated `/undo` prose at commands.md:481).

## 1. Land the ADR 0086 spec documents — ✅ ALREADY-DONE (afb48417)

**What:**
**Goal:** `docs/adr/0086-a-delegation-is-stopped-singly-and-a-named-one-stays-continuable-for-the-session.md`, the `CONTEXT.md` entries **Retained delegation** and **Stop (a delegation)**, and the three beads' rows in `.beads/issues.jsonl` are committed.
**Approach (assumed at the header base):** the three files are uncommitted in the working tree. Compare `.beads/issues.jsonl` with `bd list` first and re-export with `bd export -o .beads/issues.jsonl` if they disagree. Commit the files as they stand; change no wording.
**Regression guard.** The jsonl commit is gated like the docs: after the commit `git diff --quiet HEAD -- .beads/issues.jsonl` holds (re-stage and amend if the pre-commit hook's re-export was left unstaged, AGENTS.md hook note), and HEAD's jsonl carries both new beads' rows.
**Files:** docs/adr/0086-a-delegation-is-stopped-singly-and-a-named-one-stays-continuable-for-the-session.md; CONTEXT.md; .beads/issues.jsonl
**Read first:** CONTEXT.md — **Retained delegation**, **Stop (a delegation)**; docs/adr/0086-a-delegation-is-stopped-singly-and-a-named-one-stays-continuable-for-the-session.md — D1–D5, Consequences; .beads/issues.jsonl — apogee-single-delegation-stop, apogee-session-delegate-retention, apogee-2un; .beads/hooks/pre-commit — BEADS INTEGRATION block
**Tests:** none (docs only).
**Acceptance:**
- `git ls-files --error-unmatch "docs/adr/0086-a-delegation-is-stopped-singly-and-a-named-one-stays-continuable-for-the-session.md"`
- `grep -c '^\*\*Retained delegation\*\*:$\|^\*\*Stop (a delegation)\*\*:$' CONTEXT.md` prints `2`
- `git diff --quiet HEAD -- CONTEXT.md docs/adr/`
- `git diff --quiet HEAD -- .beads/issues.jsonl`
- `git show HEAD:.beads/issues.jsonl | grep -c '"id":"apogee-single-delegation-stop"\|"id":"apogee-session-delegate-retention"'` prints `2`
**Commit:** `docs(adr): 0086 — a delegation is stopped singly, and a named one stays continuable for the session`

## 2. Address a child's interjection by run id — ✅ DONE (2026-09-26)

NOTES (2026-09-26): internal/tui/runview_test.go was listed in Files but needed no change — the existing viewOn fixture is left as it is; the run-id cases are driven by new stamped-event tests in interject_test.go (modelViewingStampedChild, TestRunViewAddressesTheChildByRunIDNotItsCallID, TestRunViewSteersARedirectedDelegationByItsAdoptedRunID).
NOTES (2026-09-26): domain.ErrNoSuchChild's message now reads "apogee: no running sub-agent with that run id" (was "… with that call-ID"), per the item's call-ID prose sweep.
NOTES (2026-09-26): foldChildDelivery still matches a staged row to its ChildInterjectionEvent by spawn call id (row.spawn == e.CallID), so two colliding-call-id children could clear each other's band row; pre-existing, display-only, left as is.
NOTES (2026-09-26): CONTEXT.md's Sub-agent entry still says a child is addressed by its spawning call-ID (`Agent.InterjectChild(spawnCallID, in)`); that sweep belongs to item 12 (not yet done), whose rule greps `spawn call` in CONTEXT.md.

**What:**
**Goal:** `InterjectChild` takes the delegation's run id; the child registry is keyed by run id; the TUI's run view steers the child whose run id it shows; a view whose run id is `""` gets `ErrNoSuchChild`.
**Approach (assumed at the header base):** in `internal/agent/children.go`, re-key `childRegistry` (`byCallID` → by run id) at its `register`/`unregister` sites in `runSubAgent`, where `runID` is in scope. Rename `InterjectChild(spawnCallID string, …)` to `InterjectChild(runID string, …)`; keep its recursion through `children.all()`. Update the `Engine` interface doc in `internal/tui/tui.go`, `lateEngine` in `cmd/apogee/wire_engine.go`, and `fakeEngine` in `internal/tui/seam_test.go`. `internal/tui/interject.go` passes `m.viewedRun().id`, not `.spawn`. Producers of the key: `runIDMinter` via `prepareCall`/`runDelegation`. Consumers: registry lookups, `InterjectChild`, the TUI caller — test each.
**Regression guard.** `naming_test.go`'s running-child lookup (`parent.children.lookup("c1")`) goes by run id, with a fixed minter (`a.runIDs = newRunIDMinter("0badc0de")`, as `fanout_test.go` does). `stageChildMessage` takes the run id from the head `viewedChild()` resolves (`head.spawnRunID`, adopted at start), not from the view ref's id, so a view opened on a Reaction-redirected delegation (ref id `""`) still steers its child. Every comment or error text saying a child is addressed or keyed by its spawn call-ID is restated to run id — rule: `grep -rn -i 'call-ID' apogee.go internal/domain/*.go internal/agent/*.go internal/tui/*.go`, plus `internal/domain/events.go`'s `ChildInterjectionEvent` doc.
**Files:** internal/agent/children.go; internal/agent/subagent.go; internal/agent/agent.go; internal/agent/doc.go; internal/agent/children_test.go; internal/agent/subagent_test.go; internal/agent/fanout_test.go; internal/agent/naming_test.go; internal/domain/errors.go; internal/domain/events.go; apogee.go; internal/tui/tui.go; internal/tui/interject.go; internal/tui/seam_test.go; internal/tui/interject_test.go; internal/tui/runview_test.go; cmd/apogee/wire_engine.go; cmd/apogee/wire_engine_test.go; apogee_test.go
**Read first:** internal/agent/children.go — childRegistry, InterjectChild; internal/agent/subagent.go — runSubAgent; internal/tui/interject.go — stageChildMessage, foldChildDelivery; internal/tui/runview.go — viewedRun, viewedChild, openRunAt; internal/tui/transcript.go — entry.spawned, headsRunFor, addSubAgentPhase; internal/tui/seam_test.go — fakeEngine.InterjectChild; internal/tui/runview_test.go — viewOn, modelViewingChild; internal/agent/naming_test.go — TestDelegationNaming_TheRunningChildWearsTheNewName
**Tests:** two concurrent delegations sharing one spawn call id — an interjection by each run id reaches only its own child (the case the call-id key got wrong); a TUI test asserts `fakeEngine` receives the viewed run's run id; a view whose ref id is `""` but whose head adopted a run id steers by that run id; `naming_test.go` finds the running child by run id.
**Acceptance:**
- `go build ./... && go vet ./internal/agent/ ./internal/tui/ ./cmd/apogee/`
- `go test -race -count=1 ./internal/agent/`
- `go test -race -count=1 ./internal/tui/`
- `go test -race -count=1 -run 'Engine|Interject' ./cmd/apogee/`
- `go test -race -count=1 -run Interject .`
**Closes:** apogee-interject-by-run-id
**Commit:** `refactor(agent): address a child's interjection by run id, not spawn call id`

## 3. Engine stops one running delegation and folds it — ✅ DONE (2026-09-26)

NOTES (2026-09-26): internal/eventjson/encode.go needed no change — it already encodes `Reason` as `string(e.Reason)`, so `stopped` is carried as-is; encode_test.go pins the new value instead.
NOTES (2026-09-26): consequential edit — internal/domain/errors.go: made necessary by StopChild now also returning ErrNoSuchChild (doc comment).
NOTES (2026-09-26): consequential edit — example_test.go: made necessary by the new exported UndeliveredStopped (the compile-time export enumeration).
NOTES (2026-09-26): the stopped result renders each undelivered message as `[the user's message to this delegate, never delivered before the stop]` plus its text (text only, no file/skill refs), first in the note slot; the draft-output note also rides a stopped result when the spawn-named file was written during the run, as on a fault.
NOTES (2026-09-26): the stop state reaches delegationResult through two child fields (stoppedByUser, stopUndelivered) rather than a new parameter, leaving its existing test call sites untouched; the second-stop marker cause is `the user stopped the delegate again before the summary finished`.

**What:** Depends on item 2.
**Goal:** `(*Agent).StopChild(runID string) error` stops that running delegation (and everything under it) while the parent's Turn continues; the delegation's tool result is committed with head `[stopped by the user — engine summary follows]`, then the fold, the closing text, undelivered interjections and the continue line; the ledger outcome is `stopped`; the run is retained as a capped run is; a second stop during the fold skips it and leaves the unavailable marker; an unknown or finished run id returns `ErrNoSuchChild`.
**Approach (assumed at the header base):** `runSubAgent` runs the child on `context.WithCancelCause(ctx)` and registers the cancel against the run id beside the registry entry. `StopChild` cancels with a package sentinel cause, recursing through `children.all()` like `InterjectChild`. After `sub.Run`, when `context.Cause(childCtx)` is that sentinel and the parent ctx is live, `delegationResult` takes a new stopped branch ahead of the `StatusCancelled` case, so the outcome is not `dispatchCancelled` and `dispatchGroup` does not roll back. The fold is modelled on `finishAtFault`: `foldForParent` on a ctx derived from the parent's, bounded by `StreamIdleTimeout`, and cancellable by a second `StopChild` on the same run id; zero exchange Turns gives `engineFoldUnavailableFormat`. Add `delegationStopped` to the outcome constants and to `classifyDelegation`; map it in `undeliveredReason` to a new `domain.UndeliveredStopped` (`"stopped"`), re-exported in `apogee.go`; `internal/eventjson` encodes it. Retention: extend the `res.StepCapped || res.Faulted` condition so a stopped run retains, and its continue line is appended.
**Regression guard.** The stopped branch is taken only when the stop cut the Run short: `res.Status == StatusCancelled`, or `res.Faulted` with an empty `capFold` (`finishAtFault`'s fold cancelled). The run's stop handle is withdrawn as `sub.Run` returns and re-armed only for the stopped fold, so a `StopChild` landing during the namer join or `delegationResult` returns `ErrNoSuchChild` and leaves a completed or capped result untouched.
Binding addition — the folded stopped result is a non-error result (IsError=false), like the capped result; classifyDelegation checks the stop before its IsError cases so the ledger records `stopped`.
**Files:** internal/agent/children.go; internal/agent/subagent.go; internal/agent/agent.go; internal/agent/stop_test.go; internal/domain/events.go; apogee.go; internal/eventjson/encode.go; internal/eventjson/encode_test.go
**Read first:** internal/agent/subagent.go — runSubAgent, delegationResult, classifyDelegation, continuationTask; internal/agent/agent.go — Run, finishAtFault, foldForParent; internal/agent/children.go — childRegistry, retainedDelegates.retain, undeliveredReason; internal/agent/dispatch.go — dispatchGroup, runDelegation; internal/domain/events.go — UndeliveredReason; internal/agent/harness_test.go — blockingResponder; internal/agent/overflowrecovery_test.go — foldBlockingResponder, isSummaryRequest
**Tests:** new `internal/agent/stop_test.go` with scripted responders: a stop mid-run yields the exact head and a fold request, the parent's next request carries the result, nothing rolls back; a second stop skips the fold; a stopped run is retained and continuable; `StopChild` on a finished run id returns `ErrNoSuchChild`; a `StopChild` after `sub.Run` returned a completed result returns `ErrNoSuchChild` and the report stands; the ledger records `stopped`; the stopped result is committed with IsError=false; the undelivered reason is `stopped`.
**Acceptance:**
- `go build ./... && go vet ./internal/agent/ ./internal/domain/ ./internal/eventjson/`
- `go test -race -count=1 ./internal/agent/`
- `go test -race -count=1 ./internal/eventjson/`
- `go test -race -count=1 ./internal/domain/`
**Commit:** `feat(agent): stop one running delegation, fold it and let the parent's Turn go on`

## 4. A stop reaches a queued pooled child and a nested child — ✅ DONE (2026-09-26)

NOTES (2026-09-26): the queued set lives on the parent's childRegistry (a `queued` map of run id to stop mark, under the registry's one lock) rather than as a separate Agent field; `childRegistry.stop` marks a queued id, the pool worker's `runPooledSlot` reads the mark at dequeue (`stopQueuedDelegation`, checked before `preemptDelegation`), `arm` carries a mark set after the dequeue into the child ctx, and the worker `unqueue`s every slot as it settles.
NOTES (2026-09-26): fanout_test.go needed no change — the new tests reuse its threeWayFanOutParent, phasesFor and subAgentResults from stop_test.go; a registry-level unit test pins the dequeue-to-arm window.
NOTES (2026-09-26): a stop marked after the dequeue on a delegation that runSubAgent then refuses before arming a child (bad arguments, unknown continue name) returns nil but changes nothing — the refusal result stands, and the mark is dropped at unqueue.
NOTES (2026-09-26): consequential edit — internal/agent/subagent.go: made necessary by stopQueuedDelegation booking a ledger row outside runSubAgent (the runSubAgent ledger doc) and by arm carrying a post-dequeue stop (the arm-site comment).

**What:** Recast at the regression check (2026-09-25). Depends on item 3.
**Goal:** `StopChild` on the run id of a pooled delegation that has not started makes it run nothing and fold nothing, returning the error-shaped `sub-agent not started: the user stopped it before it started; delegate again if the task is still needed` with ledger outcome `stopped`, while its siblings run on; `StopChild` on a grandchild's run id stops only that grandchild and its own parent child continues.
**Approach (assumed at the header base):** run ids are minted in `prepareCall` before `runPool` dequeues. Keep a set of queued run ids on the parent `Agent` (added when the pool enqueues, removed at dequeue); `StopChild` marks a queued id, and the dequeue check in `preemptDelegation` skips a marked slot through `skipDelegation` with the new result text. Nested reach comes from `StopChild`'s recursion through `children.all()`; the stopped grandchild's result goes to its own parent child like any tool result.
**Regression guard.** Every pooled delegate slot's run id enters the queued set in `dispatchGroup`'s prepare loop, before `runPool` (whose `jobs <- i` send is unbuffered); `StopChild`'s check-and-mark and the dequeue-and-remove run under one lock, and a mark set after dequeue is carried into `runSubAgent` so the child ctx is cancelled as it is created. The queued-stop skip books its ledger row the way `recordCeilingRefusal` does (`spawnIndex: a.delegations.open(call.ID)`, outcome `delegationStopped`).
Binding addition — the queued-stop result takes the existing unstarted shape so a Driver and the model read every unstarted kind alike: error-shaped (built like skippedDelegationResult, IsError=true) with content `sub-agent not started: the user stopped it before it started; delegate again if the task is still needed`, and classifyDelegation checks the stop before `IsError && !ran` so the ledger records `stopped`, not `refused`.
**Files:** internal/agent/dispatch.go; internal/agent/children.go; internal/agent/subagent.go; internal/agent/stop_test.go; internal/agent/fanout_test.go
**Read first:** internal/agent/dispatch.go — runPool, preemptDelegation, skipDelegation, dispatchGroup, recordCeilingRefusal, prepareCall; internal/agent/children.go — delegationLedger.reserve/open/record, InterjectChild; internal/agent/subagent.go — runSubAgent; internal/agent/fanout_test.go — newRoutedResponder, fanOutScript, newRunIDMinter use at the shared-"c1" test
**Tests:** in `stop_test.go`: a three-way fan-out at pool width 2 with the first two children blocked (`blockingResponder`-style) and the third stopped while queued, by the `SpawnRunID` on its `ToolCallEvent` or a fixed-minter id — the third makes no request, its result is the exact queued text with IsError=true, its ledger row reads `stopped` (not `refused`), the other two complete; a nested delegation whose grandchild is stopped — the child receives the stopped head and completes.
**Acceptance:**
- `go build ./... && go vet ./internal/agent/`
- `go test -race -count=1 ./internal/agent/`
**Commit:** `feat(agent): a stop reaches a queued pooled delegation and a nested one`

## 5. TUI renders a stopped run; human-facing text says capped and cancel — ✅ DONE (2026-09-26)

NOTES (2026-09-26): internal/tui/toolview.go (a Read-first anchor, not on **Files:**) carries the queued-stop recognition: toolView.absorbFailure words an error-shaped `sub-agent not started: the user stopped it …` result on a run head as `stopped by you` in the marker tone (not `error`, not red) and keeps the text as the failure body, so entry.neverStarted still reads it — the sub_agent failure hook can only word an `error: …` slot, so the goal's queued-stop row could not be reached from toolregistry.go alone.
NOTES (2026-09-26): consequential edit — internal/tui/doc.go: made necessary by renaming the running legend's `esc×2 stop` to `esc×2 cancel` (package prose quoted the old label).
NOTES (2026-09-26): delegationBoundLead is now `capped at its `; delegationBoundHead still matches the engine's `[delegate stopped at its …;` head byte-for-byte. A new delegationStoppedByUser (stopped head at the body's start, or the queued-stop whole text) outranks the bound head and the no-report reading; stoppedSummary is ANDed into subAgentFinished. help_test.go and interject_test.go needed no change (help_test pins helpKeyStop through the constant and the running legend). Public docs (layout.md, docs/layout/tool-layout.md, docs/manual/commands.md) still carry the old wording — owned by items 13 and 14.

**What:** Recast at the regression check (2026-09-25). Depends on item 3.
**Goal:** a delegation result headed `[stopped by the user — engine summary follows]` renders the verdict `stopped by you` in the step-cap tone with no ✓ in its leader and umbrella member row; an undelivered interjection with reason `stopped` renders `<name> was stopped by you before your message landed`; every human-facing TUI text for an engine bound says "capped" (verdict `capped at its step cap` etc., undelivered note for a capped run); the engine's `[delegate stopped at its …]` heads still parse.
**Approach (assumed at the header base):** in `internal/tui/toolregistry.go`, `delegationVerdict` gains a stopped case recognised by the D4 head (and by the queued-stop text of item 4); `delegationBoundHead` keeps matching the engine head, while the displayed lead (`delegationBoundLead`) changes to `capped at its `. The tone mapping in `internal/tui/toolleader.go` treats stopped like the step cap. `undeliveredNote` in `internal/tui/transcript.go` gains the stopped reason and rewords the capped one.
**Regression guard.** (a) the engine's human-facing bound ErrorEvent lines `stepCapErrFormat`, `tokenCapErrFormat`, `timeCapErrFormat` in `internal/agent/agent.go` say "delegate capped at its step cap (%d steps) …" (and token budget / time limit likewise), rest byte-identical; the model-facing result heads stay byte-identical. (b) undelivered notes follow the existing `<name> … before your message landed` pattern: stopped → `<name> was stopped by you before your message landed`; capped → `<name> was capped before your message landed`. (c) the whole-Turn cancel's human-facing labels say cancel: `esc×2 stop` → `esc×2 cancel` (`runningPlaceholder` in `internal/tui/prompteditor.go`, `statusRight` in `internal/tui/model.go`, `helpKeyStop` in `internal/tui/help.go`), `press esc again to stop` → `press esc again to cancel` in `escStopHintPlain` and its drops/skips variants; update every pinning test and golden.
Folded guards: a whole-phrase `stoppedSummary` (bare verdict + steered cell, shaped like `endedWithoutReportSummary`) is ANDed into `subAgentFinished`, the ✓ site, so live and replay both withhold ✓. `delegationDetail` refuses one-line promotion for both stopped texts as it does for no-report. Every test deriving a bound verdict from an engine head is updated — rule: `grep -rn 'stopped at its' internal/tui cmd/apogee`.
Writer decision: the TUI's stopped verdict recognises the queued-stop content of item 4 exactly as item 4 now spells it.
**Files:** internal/tui/toolregistry.go; internal/tui/toolleader.go; internal/tui/transcript.go; internal/tui/subagentblock.go; internal/tui/prompteditor.go; internal/tui/model.go; internal/tui/help.go; internal/tui/toolregistry_test.go; internal/tui/transcript_test.go; internal/tui/subagentblock_test.go; internal/tui/toolbranch_test.go; internal/tui/toolpresent_test.go; internal/tui/prompteditor_test.go; internal/tui/help_test.go; internal/tui/model_test.go; internal/tui/interject_test.go; internal/tui/runview_test.go; internal/agent/agent.go; internal/agent/subagent_test.go; cmd/apogee/e2e_delegation_test.go; cmd/apogee/e2e_smoke_test.go; cmd/apogee/testdata/frames/t04-step-cap-block.txt; cmd/apogee/testdata/frames/t10-forced-pane.txt; cmd/apogee/testdata/frames/t12-pane-60.txt; cmd/apogee/testdata/frames/popup-approval.txt
**Read first:** internal/tui/toolregistry.go — delegationVerdict, delegationBoundLead, delegationDetail, delegationEndedWithoutReport; internal/tui/subagentblock.go — subAgentFinished, subAgentVerdictWord; internal/tui/transcript.go — undeliveredNote, entry.neverStarted, inFlightFanOut;
internal/tui/toolview.go — enrichWithResult; internal/tui/model.go — escStopHintPlain, escStopHint, statusRight; internal/agent/agent.go — stepCapErrFormat, boundErrText;
cmd/apogee/e2e_smoke_test.go — escStopArmedHint, stopRun; internal/tui/transcriptbridge_test.go — TestTranscriptCodecReplaysANoReportDelegationWithoutACheck (replay ✓ pattern)
**Tests:** a stopped result renders `stopped by you` without ✓ in both the leader and the member row, live and on replay; a queued-stop row (the error-shaped one-line `sub-agent not started: the user stopped it before it started; delegate again if the task is still needed`) reads `stopped by you` without ✓; a capped result renders `capped at its step cap`; the undelivered stopped and capped notes' exact text; an old-session capped head still classifies as capped; the three ErrorEvent formats pinned with `capped`; the cancel labels pinned in `help_test.go`, `prompteditor_test.go` and `model_test.go`.
**Acceptance:**
- `go build ./... && go vet ./internal/tui/ ./internal/agent/`
- `go test -race -count=1 ./internal/tui/`
- `go test -race -count=1 ./internal/agent/`
- `go test -race -count=1 -run 'E2E' ./cmd/apogee/` (re-record any golden only when its diff is exactly the capped or cancel rewording)
**Commit:** `feat(tui): render a stopped delegation, and say capped for an engine bound`

## 6. `^x` stops the viewed run or the cursor's member row — ✅ DONE (2026-09-26)

NOTES (2026-09-26): re-derived from the assumption that a standing approval/ask pane knows the run id that raised it — domain.ApprovalRequest carries none, so approvalReqMsg/askReqMsg (messages.go) now carry their parked call's ctx.Done() (set in approver.go/asker.go), and a delegation's finished phase withdraws a pane whose own call was abandoned (withdrawAbandonedDecision, approval.go) — which is exactly "that run or any run beneath it", since a child's ctx is a child of its parent's. A whole-Turn stop (actStopping) is left to finishWorker as before. The alternative (parkCall sending a withdraw msg) was rejected because approver_test/asker_test pin exactly one sent msg after a cancel.
NOTES (2026-09-26): "running" is read as "not yet reported" (stoppable: headsRun, non-empty spawnRunID, !subAgentReported) for both the key and the hint, so a queued pooled delegation — which item 4's engine stop settles without starting — is stoppable too, and the hint never hides a live key.
NOTES (2026-09-26): TestRunViewStatusSlotOffersTheWayBack keeps its want: its fixture (modelWithRun) is a finished run with no run id, where `esc back` stays correct; the running want is pinned by the new TestRunViewHintOffersTheStopWhileTheRunRuns instead.
NOTES (2026-09-26): internal/tui/doc.go's run-view paragraph ("the status line says esc back…") is left to item 13, which owns doc.go.

**What:** Depends on items 3 and 5.
**Goal:** `ctrl+x` inside a run view of a running delegation calls `Engine.StopChild` with that run's run id; `ctrl+x` with the block cursor on a running member row of a `✦ Sub-Agent (N)` umbrella stops that row's run; there is no confirmation; `esc` still means back and `esc×2` at the top level still cancels the Turn; the run view's header hint and status right slot read `esc back · ^x stop` while the viewed run runs and `esc back` otherwise; `/help` lists `^x`.
**Approach (assumed at the header base):** add `StopChild(runID string) error` to the `Engine` interface in `internal/tui/tui.go` (documented as Update-goroutine-safe, like `InterjectChild`), to `lateEngine` (returns `errNoServerBound` unbound) and to `fakeEngine` (records calls). Extend `runViewKey` in `internal/tui/runview.go` to claim `"ctrl+x"` under its existing gate, and `blockCursorKey` in `internal/tui/blockcursor.go` for a row whose entry `headsRun()`; both ignore finished runs and an empty run id. `backHint()` picks the hint; `breadcrumbRow`'s narrow-width drop still drops it whole. `helpKeyStop` in `internal/tui/help.go` gains `^x`. Add `CtrlX Key = "\x18"` to `internal/tuitest/keys.go` and pin it in `TestKeysDecodeAsIntended`. Re-record `cmd/apogee/testdata/frames/t17-run-view.txt` (running view shows the new hint); `t18-run-view-finished.txt` must not change.
**Regression guard.** A stop cancels the ctx a stopped child's parked approval or ask waits on, and parkCall returns abandoned without withdrawing the pane it raised; so when a delegation's finished phase arrives with any outcome, the TUI withdraws a standing approval or ask pane raised under that run id or any run beneath it; add a runview/approval test that stops a child with a standing approval pane and asserts the pane is gone and keys return to the prompt.
Folded guards: `helpKeyStop` is left to item 5; a new cell `helpKeyStopRun = "^x stop"`, pinned to the run-view hint constant, joins `helpKeyLegend` and `help_test.go`. `runViewKey` declines `ctrl+x` while `m.cursor.active` stands on a run-heading row, so `blockCursorKey` answers it; `keyClaimOrder` is unchanged. The long hint is chosen only when it fits, falling back to `esc back` (as `escStopHint` does), in both `breadcrumbRow` and `statusRight`'s `breadcrumbHint` site beside `backHint()`; the status slot shows it wherever it shows the view's key hint today (a reported `contextGauge` still takes the slot first).
**Files:** internal/tui/tui.go; internal/tui/runview.go; internal/tui/blockcursor.go; internal/tui/subagentblock.go; internal/tui/model.go; internal/tui/help.go; internal/tui/approval.go; internal/tui/ask.go; internal/tui/seam_test.go; internal/tui/runview_test.go; internal/tui/blockcursor_test.go; internal/tui/approval_test.go; internal/tui/help_test.go; internal/tuitest/keys.go; internal/tuitest/driver_test.go; cmd/apogee/wire_engine.go; cmd/apogee/e2e_subagent_view_test.go; cmd/apogee/testdata/frames/t17-run-view.txt
**Read first:** internal/tui/runview.go — runViewKey, runViewOwnsEsc, backHint, viewedChild, openRunAt; internal/tui/model.go — keyClaimOrder, statusRight, escStopHint; internal/tui/blockcursor.go — blockCursorKey, blockCursorOwnsKeys; internal/tui/mouse.go — toggleBlockAt (lineTarget.entry → entries[i].spawnRunID); internal/tui/subagentblock.go — breadcrumbHint, breadcrumbRow, subAgentReported; internal/tui/help.go — helpKeyLegend; internal/tui/seam_test.go — fakeEngine.InterjectChild; cmd/apogee/e2e_subagent_view_test.go — TestE2ESubAgentView, runViewHint
**Tests:** runview_test: `^x` in a running view calls `StopChild` with the run id, in a finished view calls nothing, `esc` still walks back; `^x` inside a running child's view with the cursor on a running grandchild row stops only that row; a stop of a child with a standing approval pane withdraws the pane and returns keys to the prompt; blockcursor_test: `^x` on a running member row stops that row's run only; hint text per running/finished, and the `esc back` fallback at a narrow width; `TestRunViewStatusSlotOffersTheWayBack` takes the new want; `TestKeysDecodeAsIntended` gains `{key: CtrlX, code: 'x', mod: tea.ModCtrl, name: "ctrl+x"}` before Esc; `TestE2ESubAgentView` asserts the running header ends in the new hint and the finished one (t18) in `esc back`; keyclaim order unchanged.
**Acceptance:**
- `go build ./... && go vet ./internal/tui/ ./internal/tuitest/ ./cmd/apogee/`
- `go test -race -count=1 ./internal/tui/`
- `go test -race -count=1 ./internal/tuitest/`
- `go test -race -count=1 -run 'E2ESubAgentView|Engine' ./cmd/apogee/`
**Commit:** `feat(tui): ctrl+x stops the viewed sub-agent run or the cursor's member row`

## 7. End-to-end: `^x` stops a hanging delegate and the parent goes on — ✅ DONE (2026-09-26)

NOTES (2026-09-26): openWorkingRun is not reused verbatim — it waits on the view test's fixed `← main › scout` crumb; the new file carries openRunNamed, the same ⌥↑ ⏎ keystrokes waiting on a crumb it is given (`← main › survey`). frameWhen, holds, rowContaining, carriesTask and resultBody are reused; statusRow was not needed.

**What:** Depends on item 6.
**Goal:** a driven e2e test opens a running delegate's run view, reads the `^x stop` hint from the frame, presses `^x`, and sees the member row settle as `stopped by you` while the parent's Turn continues to its final answer; the parent's next request carries the `[stopped by the user — engine summary follows]` head.
**Approach (assumed at the header base):** new `cmd/apogee/e2e_subagent_stop_test.go` following `docs/design/test-drivers.md`'s new-e2e checklist, reusing `openWorkingRun`, `frameWhen` and `statusRow` from `e2e_subagent_view_test.go`, and a new stubllm fixture `cmd/apogee/testdata/stubllm/delegate-stop.yaml` modelled on `delegate-hang.yaml` (a child that hangs until stopped, a fold response, a parent final answer). Assert behaviour semantically; no golden.
**Files:** cmd/apogee/e2e_subagent_stop_test.go; cmd/apogee/testdata/stubllm/delegate-stop.yaml
**Read first:** cmd/apogee/e2e_subagent_view_test.go — openWorkingRun, frameWhen, statusRow, rowContaining; cmd/apogee/testdata/stubllm/delegate-hang.yaml — the `hang:` child turn; cmd/apogee/testdata/stubllm/delegate-cap.yaml — the fold `system:` turn and the parent `tool_result: sub_agent` turn (both must sit above the hang turn); internal/stubllm/server.go — hang released by sleep(r.Context()); cmd/apogee/e2e_delegation_test.go — goldenRedactions, launchTUIConfigured
**Tests:** the e2e test above.
**Acceptance:**
- `go test -race -count=1 -run 'E2ESubAgentStop' ./cmd/apogee/`
**Commit:** `test(e2e): ctrl+x stops a hanging delegate and the parent's Turn goes on`

## 8. A retained entry holds the task and its rounds — ✅ DONE (2026-09-26)

NOTES (2026-09-26): consequential edit — internal/agent/stop_test.go: made necessary by replacing retainedDelegate's fold/closingReport/spawnCallID with rounds (its literals and the stopped-then-continued seed); its "continuation retains nothing" assertion now pins the second round, per the ratified continuation-retention call.
NOTES (2026-09-26): the use sequence is stamped by retain only, not take — every take is followed by a retain (the continuation's round, or giveBack on a refusal) except a cancelled continuation, which retains nothing, so a take stamp would never be read.
NOTES (2026-09-26): spawnCallID moved from the entry onto each round (delegateRound.spawnCallID), the per-round call id item 10's fork cut reads; the entry keeps bound for its latest run. The capped/faulted/stopped round report reuses the capped result's body below its engine-summary head (new Agent.foldWithClosing, which cappedResultBody now wraps byte-identically), so a wordless child's round carries stepCapNoTextMarker.
NOTES (2026-09-26): the omitted marker is spelled literally `[N earlier rounds omitted]` for every N, including 1, as the Goal line writes it.
NOTES (2026-09-26): the first-run retention gate now also requires outcome != dispatchCancelled (was implied by delegationResult's case order); a recovered panic in a continued child still drops the taken entry, as it did before this item.

**What:** Depends on item 3.
**Goal:** a retained entry holds the original task and one round per run under its name — instructions (round 1: none, the task) and report (completed: the child's final report; capped, faulted, stopped: fold plus closing text); a `continue:` seed renders the task, then `[N earlier rounds omitted]` when rounds were dropped, then each kept round chronologically as `[round K — instructions]` (K ≥ 2) and `[round K — report]` or `[round K — engine summary]`, then `[continuation instructions]` and the new instructions; rounds are kept newest-first within 4096 tokens and the newest is always whole; the unknown-name refusal lists the 16 most recently used names, newest first, then `(and N more)`.
**Approach (assumed at the header base):** replace `retainedDelegate`'s `fold`/`closingReport` with a rounds slice and add a use sequence number stamped on `retain` and `take`. Rewrite `continuationTask` to the layout above, counting with the estimator compact.go applies to `compactMaxTokens`. A continuation appends its round to the entry it `take`s and re-retains it whatever its outcome (ratified). `names()` orders by the sequence, and `unknownContinueResult` caps at 16. This item keeps retention's lifetime and which completed runs retain unchanged.
**Regression guard.** Tokens are counted deterministically with `domain.Budget{CharsPerToken: apogeectx.DefaultCharsPerToken}.EstimateTokens(len(s))` (not the per-Agent calibrating `a.tokens`; `compactMaxTokens` is a reply cap), keeping `continuationTask` a pure function of (prior, instructions). A continuation re-retains under `sub.displayName()`, the name its continue line spells, keeping the prior entry's name only when the call named none (`inheritedName`). A cancelled continuation (`dispatchCancelled`) re-retains nothing; a Run-error or error-shaped completion records its result Content as the round report.
**Files:** internal/agent/children.go; internal/agent/subagent.go; internal/agent/children_test.go; internal/agent/subagent_test.go; internal/agent/state_test.go; cmd/apogee/e2e_delegation_test.go
**Read first:** internal/agent/subagent.go — runSubAgent, continuationTask, unknownContinueResult, delegationResult, cappedResultBody, completedResult; internal/agent/children.go — retainedDelegate, retainedDelegates.retain, retainedDelegates.take, retainedDelegates.names; internal/agent/subagent_test.go — TestSubAgent_ContinueSpawnsAChildFromTheFoldWithAFreshCap, TestFanOut_CappedChildrenAreRetainedFromThePool, TestSubAgent_ContinueOfAnUnknownNameIsRefused, continueArgs, cappedSurveyScripts, runCappedSurveyParent; internal/agent/children_test.go — TestRetainedDelegates_KeepsTheLatestUnderEachName; internal/agent/state_test.go — TestSnapshot_NeverCarriesRetainedDelegates; cmd/apogee/e2e_delegation_test.go — TestE2EDelegationStepCap, previousAttemptHead; internal/context/budget.go — TokenEstimator.EstimateTokens
**Tests:** exact seed text for one, two and many rounds (the omitted marker and count); the newest round over budget laid whole; a capped round carries fold and closing text; refusal listing for 3 and 20 names (order, `(and 4 more)`); a continuation of a capped run that then completes leaves an entry with two rounds; a continuation that renames is kept under the new name; a cancelled continuation retains nothing. `TestFanOut_CappedChildrenAreRetainedFromThePool`'s names assertion becomes an order-insensitive set compare (order is pinned only by `TestRetainedDelegates_KeepsTheLatestUnderEachName`, re-pinned); `TestSnapshot_NeverCarriesRetainedDelegates`' literal moves onto the rounds shape; `e2e_delegation_test.go`'s `previousAttemptHead` retargets to the round-1 head.
**Acceptance:**
- `go build ./... && go vet ./internal/agent/`
- `go test -race -count=1 ./internal/agent/`
- `go test -race -count=1 -run TestE2EDelegationStepCap ./cmd/apogee/`
**Commit:** `feat(agent): a retained delegation keeps its task and every round, laid into a continue within budget`

## 9. Named delegations stay retained for the session — ✅ DONE (2026-09-26)

NOTES (2026-09-26): consequential edit — internal/agent/testdata/contextcost.golden: made necessary by the widened `continue` schema description (tool menu 23859 → 23886 bytes), regenerated with `-update` as the golden's own failure message directs.
NOTES (2026-09-26): the "completed" gate is `err == nil && res.Status != domain.StatusCancelled` beside the existing `outcome != dispatchCancelled`; the retention emptying on restore sits at the tail of Agent.restoreState, so it covers both Resume and RestoreSession and runs only after a clean decode (a refused restore keeps retention).
NOTES (2026-09-26): the ClearContext test lives in subagent_test.go (beside the retention tests, reusing their fixtures) rather than console_test.go; console_test.go is untouched. The `continue` description now also says the child restarts from "its task and earlier reports" rather than "that run's engine summary", which the round seed of item 8 made false.
NOTES (2026-09-26): the delegate-ledger comment in children.go that compared its lifetime to retainedDelegates' ("Like retainedDelegates it lives in memory only, is cleared as the next Exchange opens") and the continue-line comment in delegationResult ("A completed child has nothing to continue from") were restated, since this item made both false.

**What:** Depends on item 8.
**Goal:** a delegation that completes normally is retained when its `sub_agent` call gave a name (a namer-generated name does not retain it); a continuation retains per item 8; retention survives into later Exchanges; `/clear` (`ClearContext`) drops it whole; the `sub_agent` schema's `continue` description and the `SubAgentArgs.Continue` Go doc say a named delegation of this session, and "capped" for a bound.
**Approach (assumed at the header base):** in `runSubAgent`, capture `callNamed := delegationName(args.Name) != ""` before a continuation fills `args.Name` from `prior.name`; retain a completed run when `callNamed` or it is a continuation. Remove `a.retained.clear()` from the Exchange-open path in `internal/agent/loop.go` (the delegate ledger's per-Exchange clear stays). Add `a.retained.clear()` to `ClearContext` in `internal/agent/agent.go` and restate the `retained` field comment. Edit `internal/tools/sub_agent.go`'s schema text and Go doc.
**Regression guard.** restoreState (Resume and RestoreSession) empties retention in this item, so no retained entry crosses sessions before item 10 persists it; item 10 then loads the snapshot's entries in its place.
Folded guards: the "completed" gate excludes cancel, Run error and cap/fault (`err == nil`, status not cancelled), so `TestSubAgent_CancelledDelegateIsNotRetained` stays green. Every comment saying retention lasts for the Exchange, clears as the next opens, or is never snapshotted is restated — rule: `grep -rn "rest of its Exchange\|rest of this Exchange\|next Exchange opens\|same Exchange\|Exchange that owned" internal/agent internal/tools`. The schema edit is limited to the `continue` description (plus "capped" wording): the item yields to ADR 0086 D1 ("no new text reaches the model") and its Consequences, which widen only `continue`; the `name` description is unchanged.
**Files:** internal/agent/subagent.go; internal/agent/loop.go; internal/agent/agent.go; internal/agent/children.go; internal/agent/state.go; internal/tools/sub_agent.go; internal/agent/subagent_test.go; internal/agent/console_test.go; internal/agent/restoresession_test.go; internal/tools/sub_agent_test.go
**Read first:** internal/agent/subagent.go — runSubAgent, delegationName, delegationResult; internal/agent/loop.go — step (the a.retained.clear / a.delegations.clear pair); internal/agent/agent.go — ClearContext, RestoreSession, Agent.retained field comment; internal/tui/fork.go — forkAt, foldFork; internal/tools/sub_agent.go — subAgentSchemaTemplate, SubAgentArgs; internal/tools/sub_agent_test.go — the pinned schema literal; internal/agent/subagent_test.go — TestSubAgent_CompletedChildIsNotRetained, TestSubAgent_RetainedChildIsForgottenAsTheNextExchangeOpens, TestSubAgent_CancelledDelegateIsNotRetained
**Tests:** a named completed run is continuable in the next Exchange; a generated-name completed run is not; `ClearContext` empties retention; a `RestoreSession` and a Resume leave retention empty; the schema text asserted by literal. Inverted: `TestSubAgent_CompletedChildIsNotRetained` (a named completed run is now retained) and `TestSubAgent_RetainedChildIsForgottenAsTheNextExchangeOpens` (retention survives the next Exchange); `TestSubAgent_CancelledDelegateIsNotRetained` stays green.
**Acceptance:**
- `go build ./... && go vet ./internal/agent/ ./internal/tools/`
- `go test -race -count=1 ./internal/agent/`
- `go test -race -count=1 ./internal/tools/`
**Commit:** `feat(agent): a named delegation stays continuable for the whole session`

## 10. Retention is saved with the session and cut by a fork — ✅ DONE (2026-09-26)

NOTES (2026-09-26): consequential edit — internal/agent/subagent.go: made necessary by delegateRound gaining its own call fields (runSubAgent stamps each new round with the name, roster, output path and bound its run resolved to).
NOTES (2026-09-26): consequential edit — internal/agent/stop_test.go: made necessary by delegateRound holding a tools.SubAgentRoster (no longer comparable with !=); the assertion now compares the round's text fields through a new sameRoundText helper.
NOTES (2026-09-26): consequential edit — internal/agent/subagent_test.go: made necessary by the new per-round fields: unstamped also zeroes each round's seq, the DeepEqual wants go through a new everyRoundAsTheEntry helper, and one slices.Equal over rounds became slices.EqualFunc(…, sameRoundText).
NOTES (2026-09-26): each round keeps its own name, roster, output path and bound (not only roster and path) plus a use-sequence stamp (seq, set by retain on the newest round); a fork trim re-derives the entry from its newest kept round, so a continuation that renamed the entry and was cut reverts to the old name; a name collision after that re-derivation keeps the more recently used entry.
NOTES (2026-09-26): CutSession's tie-break for a repeated call id pairs from the newest end: the rounds carrying the id (newest first by seq, across entries) are matched one for one against the dropped RoleTool results carrying it. Any RoleTool result counts, not only sub_agent ones, so a reused id on a call that retained nothing can over-cut an older round. That errs toward losing a continuation, never toward keeping one spawned after the cut.
NOTES (2026-09-26): a restored `retained` entry is refused (ErrSnapshotRefused) if it has no name, reuses a name, has no rounds or has an unknown bound word. Every name, task, output path and round instructions/report string is bounded at maxRestoredMessageBytes and checked through forgesRestoredStructure. There is no cap on the number of entries or rounds, so no ordinary session can become unresumable.
NOTES (2026-09-26): the retention-comment sweep has nothing left to change: the two remaining hits (agent.go delegations field, children.go delegate-ledger doc) describe the delegate ledger, which is still never snapshotted. The Agent.retained field comment, the retainedDelegates doc, the agentState doc (new `retained` bullet), CutSession's doc and checkRestoredStructure's doc were restated.
NOTES (2026-09-26): item 9's TestRestoreSession_EmptiesRetention became TestRestoreSession_ReplacesRetention (the incoming snapshot's set replaces the outgoing one; a snapshot with none restores with none). TestResume_StartsWithNoRetention became TestResume_ContinuesANamedDelegation.

**What:** Depends on item 9.
**Goal:** the engine snapshot carries retention under an additive `retained` key (no record-version bump) and `--resume`, `--continue` and live restore load it; each round records the call id that spawned it; `CutSession` keeps only the rounds whose spawning result is not in the cut tail and drops an entry left with none; a snapshot without the key restores with no retention.
**Approach (assumed at the header base):** add `Retained` to `agentState` in `internal/agent/state.go` beside `Tasks` (ADR 0072's precedent): a JSON form of each entry with the bound serialised as a word, the roster via `tools.SubAgentRoster`'s existing JSON, rounds with their spawn call ids, and the use sequence. `restoreState` loads it (covering Resume and `RestoreSession`). `CutSession` filters instead of dropping: a round is cut when a `RoleTool` message with its call id lies in the dropped tail; a call id folded away by compaction counts as before the cut.
**Regression guard.** Item 10 replaces item 9's restore-time clear with the load, and an absent key still restores empty.
Folded guards: every restored retained string (task, name, each round's instructions/report/fold/closing text) goes through `forgesRestoredStructure` in `checkRestoredStructure`, each bounded at `maxRestoredMessageBytes`. A call id can repeat within a session, so `CutSession` states its tie-break for an id found both before and after the cut (e.g. the k-th round carrying an id matches the k-th `RoleTool` carrying it). Roster and output path are kept per round (or re-derived from the newest kept round when `CutSession` trims). Every retention hit of `grep -n 'never reaches the session snapshot\|never snapshotted\|in memory only\|0022 D8' internal/agent/*.go` is restated, and `agentState`'s doc lists the `retained` key.
**Files:** internal/agent/state.go; internal/agent/children.go; internal/agent/agent.go; internal/agent/state_test.go; internal/agent/restoresession_test.go
**Read first:** internal/agent/state.go — agentState, encodeState, restoreState, decodeState, checkRestoredStructure, CutSession, exchangeOpenings; internal/agent/children.go — retainedDelegate, retainedDelegates; internal/agent/subagent.go — runSubAgent, continuationTask, unknownContinueResult; internal/tools/sub_agent.go — SubAgentRoster.MarshalJSON, SubAgentRoster.UnmarshalJSON; internal/agent/agent.go — delegateBound, RestoreSession, CutSnapshot; internal/agent/state_test.go — TestSnapshot_NeverCarriesRetainedDelegates, TestAgentState_EncodesStableKeyNames, TestRestore_WithoutATaskListEmptiesTheHeldOne, TestRestore_RejectsAnOverCapTaskList
**Tests:** invert `TestSnapshot_NeverCarriesRetainedDelegates` into a round-trip; a pre-change snapshot restores with empty retention; `CutSession` keeps a pre-cut entry, drops a post-cut one, trims a post-cut round off a pre-cut entry; a duplicate call id before and after the cut trims only the post-cut round; a trimmed entry carries its newest kept round's roster and output path; a restored retained string that forges engine structure is refused (beside `TestRestore_RejectsAnOverCapTaskList`); a resumed agent continues a named delegation.
**Acceptance:**
- `go build ./... && go vet ./internal/agent/`
- `go test -race -count=1 ./internal/agent/`
- `go test -race -count=1 ./internal/session/`
**Commit:** `feat(agent): save retained delegations with the session and cut them with a fork`

## 11. A cancelled Turn restores retention to its start — ✅ DONE (2026-09-26)

NOTES (2026-09-26): the Turn-start and Exchange-start copies are held on retainedDelegates itself (atTurn / atExchange, shallow map clones, safe because rounds are never written in place), so turnRolledBack keeps its no-argument signature. step() takes markExchange at the Exchange opening and markTurn right after armRequest (not inside it, since refold re-arms mid-Turn). load() resets both copies to the loaded set and clear() empties them.
NOTES (2026-09-26): the abort restore reaches the Agent through a new third observer method, exchangeAborted, fired by turnLifecycle.abort after the rollback and before closeExchange, so AbortExchange and a settle that falls through to abort both restore the Exchange-start set, while a settle that keeps its Turns does not. countingObserver and TestTurnLifecycleNotifiesItsObserver were extended to cover it.
NOTES (2026-09-26): consequential edit — internal/agent/agent.go: made necessary by the rollback; the Agent.retained field comment now says a cancel puts the set back.
NOTES (2026-09-26): TestSubAgent_ACancelledContinuationRetainsNothing pinned the leak this item fixes (it asserted no entry after a cancelled continuation). It is now TestSubAgent_ACancelledContinuationLeavesTheEntryItTook: the taken entry is back, still with one round. The use sequence is not rewound on a restore, since stamps only order.
NOTES (2026-09-26): "a stop does not restore anything" is pinned by the unchanged TestStopChild_StopsOneDelegationAndTheTurnGoesOn (stop_test.go), which still finds the stopped child retained after the parent's Turn goes on; no new stop test was added. The three new behaviour tests and the inverted one all fail with the non-test changes stashed.

**What:** Depends on item 9. Depends on item 10.
**Goal:** a Turn rolled back by cancel leaves retention exactly as it was at the Turn's start, and an aborted Exchange leaves it as it was at the Exchange's start; so a pooled sibling capped before the cancel is not retained, and a continuation cancelled after it spawned leaves the entry it consumed in place.
**Approach (assumed at the header base):** fixes two pre-existing leaks named in ADR 0086 D3. Take a copy of retention on `turnRun` when the Turn arms (`armRequest`/`step` in `internal/agent/loop.go`) and restore it in `Agent.turnRolledBack` (called from `endCancelled`); take a pre-Exchange copy where the Exchange opens and restore it on `AbortExchange` → `turns.abort` (`internal/agent/turn.go`), which has no observer hook today. `SettleExchange` keeps retention.
**Regression guard.** Wherever restoreState loads retention, the pre-Turn and pre-Exchange copies are reset to the loaded set; settle() falling through to abort() restoring the Turn-start copy is correct.
Folded guard: `exchangeObserver.turnRolledBack()` takes no argument, so the item states how the copy reaches the restore — held on the Agent or `turnLifecycle`, or passed through `end()`'s `endCancelled` row; if the interface grows, `countingObserver` and `TestTurnLifecycleNotifiesItsObserver` are extended.
**Files:** internal/agent/loop.go; internal/agent/turn.go; internal/agent/construct.go; internal/agent/children.go; internal/agent/state.go; internal/agent/subagent_test.go; internal/agent/fanout_test.go; internal/agent/turn_test.go; internal/agent/state_test.go
**Read first:** internal/agent/turn.go — exchangeObserver, turnRun, turnLifecycle.end, turnLifecycle.abort, turnLifecycle.settle, openExchange; internal/agent/construct.go — Agent.turnRolledBack, Agent.exchangeClosed; internal/agent/loop.go — step, armRequest; internal/agent/subagent.go — runSubAgent (giveBack, retain); internal/agent/dispatch.go — runPool; internal/agent/agent.go — AbortExchange, SettleExchange; internal/agent/turn_test.go — countingObserver, TestTurnLifecycleNotifiesItsObserver; internal/agent/state_test.go — TestSnapshot_RoundTripsExchangeBoundaryForAbort
**Tests:** both leaks reproduced and fixed (fail before the item); `AbortExchange` restores the pre-Exchange set; resume mid-Exchange, then `AbortExchange` keeps the loaded retention; a stop (item 3) does not restore anything.
**Acceptance:**
- `go build ./... && go vet ./internal/agent/`
- `go test -race -count=1 ./internal/agent/`
**Commit:** `fix(agent): a cancelled Turn restores retained delegations to its start`

## 12. Glossary and ADR amendments

**What:** Depends on items 3–11.
**Goal:** `CONTEXT.md`'s **Retained delegation** and **Stop (a delegation)** carry no "not yet shipped"/"until it lands" markers; **Sub-agent** and **Step cap** describe session-long, saved retention and the stop; no `CONTEXT.md` prose calls an engine bound "stopped" — the quoted `[delegate stopped at its …]` engine heads stay verbatim; ADRs 0013 §5 and its 2026-09-18 amendment, 0022's 2026-09-18 addendum, 0039 (run-id interjection) and 0063 D1, D4 and "What stays out" carry dated amendment lines pointing at ADR 0086.
**Approach (assumed at the header base):** find every site by rule: `grep -n 'stopped at\|in memory only\|rest of its Exchange\|not yet shipped\|until it lands\|spawn call' CONTEXT.md` and each named ADR; restate, never delete, the ADRs' history.
**Regression guard.** Also sweep by rule the "previous attempt — engine summary" seed label (previousAttemptHead, replaced by item 8's round labels): CONTEXT.md and a dated ADR 0013 amendment line; quote shipped wording from the code (internal/tui/transcript.go undeliveredNote, internal/agent seed labels), never from this plan.
Folded guard: the line grep misses wrapped sites, so the rule is to read the whole **Sub-agent**, **Step cap** and **Interjection** entries, and a joined-text gate checks them.
**Files:** CONTEXT.md; docs/adr/0013-*.md; docs/adr/0022-*.md; docs/adr/0039-*.md; docs/adr/0063-*.md
**Read first:** CONTEXT.md — **Sub-agent** (continue:/retains paragraph, delegate ledger outcome list), **Step cap** (P6 retention paragraph, continue refusal listing), **Interjection** (undelivered notes), **Retained delegation**, **Stop (a delegation)**; docs/adr/0013-the-sub-agent-orchestrator-is-the-recursion-point-with-isolated-live-guard-state.md — Amendment (2026-09-18); docs/adr/0022-sessions-persist-per-turn-as-dual-representation-records.md — Addendum (2026-09-18); docs/adr/0039-delegations-fan-out-concurrently-bounded-by-the-servers-parallel-agents-cap.md — Amended 2026-09-24 run id; docs/adr/0063-sub-agent-runs-are-user-addressable-views.md — D1, D4, What stays out; internal/tui/transcript.go — undeliveredNote
**Tests:** none (docs only).
**Acceptance:**
- `grep -c 'not yet shipped\|until it lands' CONTEXT.md` prints `0`
- `tr '\n' ' ' < CONTEXT.md | grep -c 'rest of its own Exchange\|resumed session has nothing to continue\|entry is consumed\|stopped at its cap before'` prints `0`
- `grep -l '0086' docs/adr/0013-*.md docs/adr/0022-*.md docs/adr/0039-*.md docs/adr/0063-*.md` lists all four
**Commit:** `docs(context): retained delegations and the stop have shipped`

## 13. Layout spec and TUI package prose

**What:** Depends on items 5 and 6.
**Goal:** `layout.md`, `docs/layout/tool-layout.md` and `internal/tui/doc.go` describe `^x` (run view and member row), the `esc back · ^x stop` hint while running, the `stopped by you` verdict, the stopped undelivered note and "capped" for bounds; the "`esc` means back before it means stop" section is restated so `^x` stops one run and `esc×2` the whole Turn.
**Approach (assumed at the header base):** find every site by rule: `grep -n 'esc back\|stop\|undelivered\|not delivered\|member row' layout.md docs/layout/tool-layout.md internal/tui/doc.go`; the survey found the Run view section, its `esc` paragraph, the prompt-box legend, the undelivered list and the status right slot.
**Regression guard.** Restate the stop/cancel split using item 5's renamed labels (`esc×2 cancel`, `press esc again to cancel`) and the shipped undelivered-note wording, read from the code.
Folded guard: the `stopped by you` verdict and "capped" bound wording are gated in both layout files; layout.md's unrelated "stopped at its last word" (row-width prose) is excluded from the gate.
**Files:** layout.md; docs/layout/tool-layout.md; internal/tui/doc.go
**Read first:** layout.md — Run view ("`esc` means back before it means stop", "The prompt box addresses the child", "A staged row names the run"), status line right-slot key hint paragraph, collapsed run verdict paragraph; docs/layout/tool-layout.md — Grouped Sub-agents rules, finished-verdict vocabulary; internal/tui/doc.go — run-view paragraph ([Model.legendFor], [Model.contextGauge]); internal/tui/transcript.go — undeliveredNote; internal/tui/docmap_test.go — TestDocMapNamesEveryFile
**Tests:** none (docs only).
**Acceptance:**
- `grep -c 'esc back · ^x stop' layout.md` prints at least `1`
- `tr '\n' ' ' < layout.md | grep -o 'stopped at its [a-z]*' | grep -vc '^stopped at its last$'` prints `0`
- `tr '\n' ' ' < docs/layout/tool-layout.md | grep -c 'stopped at its'` prints `0`
- `grep -c 'stopped by you' layout.md docs/layout/tool-layout.md` prints at least `1` for each file
- `go vet ./internal/tui/`
**Commit:** `docs(layout): ctrl+x stops one sub-agent run`

## 14. Reference manual

**What:** Depends on items 6, 9 and 10.
**Goal:** `docs/manual/` states `^x` and what a stop returns, that a named delegation stays continuable for the session and survives `--resume`, what `/clear` and a fork do to it, the ledger outcome `stopped`, the headless `child_interjection` `data.reason` `"stopped"`, and "capped" for bounds; no manual page says `continue:` is Exchange-only, memory-only or unavailable after `--resume`.
**Approach (assumed at the header base):** find every site by rule: `grep -n 'continue\|esc twice\|esc×2\|stop\|cancelled\|refused' docs/manual/*.md`; the survey found `commands.md` (key legend, esc×2, block cursor, status line), `configuration.md` (`continue:`, ledger, cap), `sessions.md` (interrupted delegations) and `headless.md` (reasons).
**Regression guard.** Also sweep by rule "previous attempt — engine summary" in docs/manual/configuration.md and the `esc×2 stop` / `press esc again to stop` labels in docs/manual/commands.md (renamed by item 5); quote shipped wording from the code, never from this plan.
Folded guard: the retention gate reads configuration.md's joined text (its "in memory for that / one exchange only" wraps), and no longer greps `rest of the exchange`, which matches the unrelated `/undo` prose at commands.md:481.
**Files:** docs/manual/commands.md; docs/manual/configuration.md; docs/manual/sessions.md; docs/manual/headless.md
**Read first:** docs/manual/configuration.md — continued-delegation section (`continue: "<name>"`), delegate ledger outcome list; internal/tools/manual_drift_test.go — TestManualListsEveryKnownToolName (keep every back-ticked tool name); docs/manual/commands.md — /help key legend row, run view paragraph, `esc` twice stop paragraph; docs/manual/sessions.md — interrupted-delegation bullet; docs/manual/headless.md — child_interjection event row
**Tests:** none (docs only).
**Acceptance:**
- `tr '\n' ' ' < docs/manual/configuration.md | grep -c 'in memory for that one exchange\|rest of the exchange it happened in\|your next message clears it\|has nothing to continue'` prints `0`
- `grep -c '\^x' docs/manual/commands.md` prints at least `1`
**Commit:** `docs(manual): stop one sub-agent, and continue a named delegation all session`
