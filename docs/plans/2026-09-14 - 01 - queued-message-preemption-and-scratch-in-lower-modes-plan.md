# Queued messages pre-empt waiting sub-agents; the scratch dir is writable in Plan and Ask-Before

**Goal:** a message queued while a delegation group runs no longer waits for children that have not started — they are skipped with an explicit tool result and the message lands when the running ones finish; and the session scratch dir becomes the one place Plan writes and the one write Ask-Before does not gate, recorded as ADR 0012's second loosen.

**Date:** 2026-09-14
**Status:** unexecuted
**Sized for:** ~200k-context host

**Sources:**
- `IDEAS.md` (the two entries this plan closes); ADR 0025 (Interjection), ADR 0039 (fan-out pool), ADR 0013 D5, ADR 0063 (child mailbox), ADR 0033 D7 (stands)
- ADR 0012 (blast radius, 2026-09-06 first loosen), `docs/design/confinement-execution-contract.md` §4/§7, ADR 0056 D3, ADR 0023 2026-08-25 amendment, ADR 0049 2026-08-28 amendment
- `internal/agent/dispatch.go` (`dispatchFanOut`, `runDelegationPool`, `dispatchSerially`, `classifyWriteTarget`, `confinementBox`), `resolution.go` (`resolveLadder`, `planAdmits`, `planRefusalReason`), `loop.go` (`toolMenu`), `orientation.go`, `children.go`; `internal/tui/interject.go`, `bridge.go`, `commandrun.go`, `subagentblock.go`; `cmd/apogee/wire_boot.go`, `e2e_announced_test.go`
- `CONTEXT.md` entries Interjection, Sub-agent, Agent mode, Scratch dir, Orientation block, Confinement

**Ratified design calls (owner, 2026-09-14):**
- **Queued message vs. sub-agents:** the not-yet-started children of the running delegation group are pre-empted — skipped with an explicit `not started` tool result; running children finish; the message commits at that boundary as an ordinary Interjection. Running children are never cancelled.
- **Firings:** `/schedule` Firings keep waiting for a quiescent host — ADR 0033 D7 stands; out of scope.
- **Plan and the scratch dir:** Apogee's own writers into the session scratch dir run unprompted in Plan; every other target stays refused with a reason naming the scratch dir; the writers return to Plan's tool menu; the orientation `Scratch dir:` bullet returns in Plan; a Plan Firing writes into its own scratch dir the same way. The terminal route stays refused.
- **Ask-Before and the scratch dir:** native writes into the scratch dir run unprompted; every other write and every command still gates. The terminal route stays gated.

**Regression check (2026-09-14, dca8a712fd2ebe5bd9eae89b8a90352767a2e17e):**
- 1: guard folded — a skipped slot commits with `run` false (no `recordExecuted` audit/breaker entry); ADR 0025 Decision 3 line 70 ("the engine never learns a message exists until it is delivered") is superseded by the ratified pre-emption call, recorded by item 3.
- 2: recast (writer decision) — the skipped row is painted through the existing `absorbFailure` error path; the "first line in the outcome slot" wording is withdrawn; the cap-1 e2e drives `dispatchSerially` only.
- 3: guard folded — the prose-guard rule extends to every "the engine / a third goroutine never sees a staged row" sentence (ADR 0025 Decision 3, `interject.go` doc); the manual site is the keys paragraph (`commands.md:69`), not a "While the model works" paragraph.
- 4: guard folded — `planRefusalReason` stays a const (read as a value in `floorguards_test.go:799`, `resolution_test.go`), `planScratchRefusalReason(dir)` is the new one; the git-staging pin is the absent staging note, not "no subprocess"; the `resolution.go:399-401` "defensive only" comment is superseded and reworded with the item.
- 5: guard folded — `read-only` joins the prose-guard grep; help text, `internal/run/run.go`, `loop.go:1387`, `resolution.go:399` are qualified.
- 6, 7: SAFE.
- Re-check (2026-09-14, dca8a712fd2ebe5bd9eae89b8a90352767a2e17e): 2: guard folded (writer decision) — the skipped delegation is an IsError result painted like the engine's real depth-bound refusal on the wire (`errorToolResult`), not like the `refusedDelegation` fixture: collapsed, the outcome slot reads `error`; expanded, the skip content is the body; every `m.box = nil` site registers nil on the bridge. Items 1, 3-7: not re-reported.

**Standing requirements:**
- `skills: coding-standards`
- Deviations from item text land as a dated NOTES line under the item.
- Bubble Tea v2 (`charm.land/bubbletea/v2`): `tea.KeyPressMsg`, `msg.String()`. The `Model` is value-copied on every `Update`: no `strings.Builder` or mutex by value; the mailbox stays held by pointer.
- Nothing new is put in front of the model except the two engine-produced tool-result / refusal strings this plan binds verbatim (ADR 0031 invariant 4: no Mechanism, no preamble).

**Out of scope:**
- Cancelling running children on a queued message (ADR 0013 D5 rollback stays the only cancel); pre-empting leaf tool calls; any change to child mailboxes' delivery (ADR 0063).
- Firings contending with the live session (ADR 0033 D7); a model-facing schedule tool (`apogee-54f`).
- Terminal / subprocess writes into the scratch dir below Auto (still refused in Plan, gated in Ask-Before); per-project `confine-writable-paths` in Plan or Ask-Before (scratch dir only).
- Any change to the orientation template text, `{{scratch}}`, the dangerous-action guard exemption (ADR 0049), scratch GC, or VERSION.

## 1. Engine: a pending queued message pre-empts the delegations not yet started — ✅ DONE (2026-09-14)

NOTES (2026-09-14): the child-side predicate is named `childMailbox.hasPending` (affirmative `has` prefix per the coding standard) rather than reusing `drainMailbox` state; `Agent.interjectionPending` and the shared `skipDelegation` (result + finished phase) live in dispatch.go, called by both the pool worker and dispatchSerially.
NOTES (2026-09-14): the serial-path test places the leaf tool in the parent's NEXT Turn with the seam still true — partitionDispatch runs a same-reply leaf before every delegation, so that is the only order in which "a leaf after a skipped delegation" is observable.

**What:** Add the host seam and the pre-emption, engine-side, depth-aware:
- `internal/domain/config.go`: `Config.InterjectionPending func() bool` beside `Report` — a host-supplied delegate answering "is a message staged for this top-level agent?"; nil (bench, embedder, every bare test) ⇒ never pre-empts, byte-identical behaviour. May be called from the dispatching goroutine and pool workers; the host makes it goroutine-safe.
- `internal/agent/dispatch.go`: `Agent.interjectionPending() bool` = at depth 0 the Config seam (nil ⇒ false); at depth > 0 "this child's own mailbox holds a message" (`children.go`, the `drainMailbox` state) — one rule for every depth, since a child's queued message waits on its grandchildren the same way.
- `runDelegationPool`: a worker checks `interjectionPending()` the instant it DEQUEUES a job, before `emitSubAgentPhase(Started)`; when true the slot is not run: its result is `skippedDelegationResult(call.ID)` — an error-shaped tool result (as `executeRefuse` builds) whose content is exactly `sub-agent not started: the user sent a message while this group was running; delegate again if the task is still needed` — and a `SubAgentFinished` phase carrying that result (`Cancelled: false`, no Started phase) is emitted at once, so a Driver's row leaves "scheduled" immediately. Committed in call order by `commitDelegation` like a refused slot (`run` false path: productivity signal and post-tool-result reactions as for a refusal). A child already started is never affected.
- `dispatchSerially`: before `resolveAndExecute` of a `tools.SubAgentToolName` call, the same check skips it with the same result and phase; leaf tools are never pre-empted.
- `Agent.Interject` doc and `stepToBoundary`'s contract are unchanged: the message still commits only at the boundary — the seam is a predicate, not a drain (ADR 0025 "drain hook" stays rejected).
- Restate for the implementer: the skip decision is read once per slot at dequeue and never re-read; the result string is a package constant (`skippedDelegationContent`) exported to the test, never rebuilt in a test by hand.

**Regression guard.** `prepareDelegation` leaves `fanOutSlot.run` true for a Delegate verdict (`dispatch.go:398`) and `commitDelegation`'s `slot.run` branch (`dispatch.go:526-529`) calls `recordExecuted`, which books a Delegate audit + circuit-breaker entry. The worker (and `dispatchSerially`) that skips a slot CLEARS `slots[i].run` (or sets a `skipped` flag `commitDelegation` treats as run-false) so no `recordExecuted` / audit event fires for a child that never ran — the refused-slot parity the What names. ADR 0025 Decision 3 (line 70, "The engine never learns a message exists until it is delivered") is superseded by the ratified pre-emption call; item 3 records the supersession (Decision 2's amendment alone does not).

**Files:** `internal/domain/config.go`, `internal/agent/dispatch.go`, `internal/agent/children.go`, `internal/agent/interject.go`, `internal/agent/fanout_test.go`, `internal/agent/children_test.go`
**Read first:** `internal/agent/dispatch.go` — runDelegationPool, dispatchSerially, commitDelegation, fanOutSlot, prepareDelegation, executeRefuse; `internal/agent/children.go` — childMailbox, drainMailbox

**Tests:**
- `TestFanOut_PendingInterjectionSkipsTheQueuedChildren`: cap 2, three delegations, a stub child that blocks on a gate; the seam flips to true while two run; the third commits the exact skip content in call order, a `SubAgentFinished` phase with that result and no `Started` phase was emitted for it, no audit record (`recordExecuted`) was written for the skipped slot, both running children commit their real results, the Turn is `dispatchDone`.
- `TestFanOut_PendingInterjectionNeverTouchesARunningChild`: seam true after all slots dequeued ⇒ every result real.
- `TestDispatchSerially_PendingInterjectionSkipsTheNextDelegation`: cap 1, two delegations, seam true after the first starts ⇒ second skipped; a leaf tool after it still runs.
- `TestInterjectChild_PendingMailboxSkipsAGrandchild`: depth-1 child with a mailbox message dequeues no grandchild.
- Nil-seam test: a Config without the delegate runs every slot (byte-identical history to today's fixture).

**Acceptance:** `go build ./... && go test ./internal/agent/ -run 'FanOut|DispatchSerially|InterjectChild' -count=1 && go test ./internal/domain/ -count=1`

**Commit:** `feat(agent): a pending queued message pre-empts the delegations not yet started`

## 2. TUI and host: the mailbox answers the seam; the skipped row reads its result — ✅ DONE (2026-09-14)

NOTES (2026-09-14): `internal/tui/model.go` edited beyond the plan's file list — the Model struct (the new `registerBox` registrar field) and `finishWorker`'s `m.box = nil` site live there, not in `tui.go`; the plan's "every `m.box = nil` site registers nil" names finishWorker, so this is the item's own work, not a consequential edit.
NOTES (2026-09-14): `subagentblock.go` carries no code change — a `SubAgentFinished` phase with no started one already ends `subAgentScheduled` and paints through `absorbFailure` exactly like the depth-bound refusal (verified by the new test); only `subAgentScheduled`'s doc comment gained the pre-empted case.
NOTES (2026-09-14): the e2e was checked against the rope — with the `InterjectionPending` line removed from `wire_boot.go` it fails on all three claims (skip content, second child asked, row verdict) and passes with it restored.

**What:** Recast at the regression check (2026-09-14). Depends on item 1. Wire the seam to the live mailbox and make the skipped delegation legible:
- `internal/tui/bridge.go`: `Bridge` holds a mutex-guarded pointer to the live `*interjectBox` (`setMailbox(*interjectBox)`) and exposes `InterjectionPending() bool` — true when the registered box holds ≥ 1 item (`interjectBox.pending()`, new, lock-guarded; nil box ⇒ false). The pointer, not the Model, is what the engine reads: the Model is value-copied.
- Every site that assigns `m.box = newInterjectBox()` (`commandrun.go`, three sites) registers the new box on the bridge through one Model helper; the Model gets the bridge (or a `func(*interjectBox)` registrar) at `Build`. Withdraw (Backspace pop) and drain naturally clear the predicate.
- `cmd/apogee/wire_boot.go`: `InterjectionPending: w.bridge.InterjectionPending` beside `Approver: w.bridge.Approver()`. Headless and Firing configs stay nil (no human queue).
- `internal/tui/subagentblock.go`: a delegation that is `subAgentReported` without ever being started paints its result's first line in the outcome slot (the skip content above), never `scheduled`; verify against the existing refused-at-depth-bound path, which is the same shape.
- The queued row and `N queued` readout are unchanged.

**Regression guard.** The e2e `TestE2EQueuedMessagePreemptsTheScheduledSubAgents` runs at `parallel-agents: 1` and therefore drives `dispatchSerially` only; the pool path is pinned by item 1's unit tests and gets no second e2e (writer decision 2026-09-14). The item's "first line in the outcome slot" wording is withdrawn. The skipped delegation is an IsError result and paints like the engine's REAL depth-bound refusal on the wire (`errorToolResult`), not like the `refusedDelegation` test fixture: collapsed, the row's outcome slot reads the `error` verdict (no longer `scheduled`); expanded, the skip content is visible as the body. `TestSubAgentSkippedRowReadsItsResult` asserts both states and builds its head from an IsError result. Every site that sets `m.box = nil` (finishWorker, the compact path) also registers nil on the bridge, so the seam never reads a dead box (writer decisions 2026-09-14, resolving the re-check NOTES).

**Files:** `internal/tui/bridge.go`, `internal/tui/interject.go`, `internal/tui/commandrun.go`, `internal/tui/tui.go`, `internal/tui/subagentblock.go`, `internal/tui/bridge_test.go`, `internal/tui/interject_test.go`, `internal/tui/subagentblock_test.go`, `cmd/apogee/wire_boot.go`, `cmd/apogee/e2e_subagent_preempt_test.go`, `cmd/apogee/testdata/stubllm/subagent-preempt.yaml`
**Read first:** `internal/tui/bridge.go` — Bridge; `internal/tui/interject.go` — interjectBox, push; `internal/tui/commandrun.go` — launchExchange, finishWorker; `internal/tui/toolview.go` — absorbFailure;
`internal/tui/subagentblock.go` — renderSubAgentMemberRows; `internal/tui/subagentblock_test.go` — TestUnframedSubAgentShowsThePromptWhenExpanded

**Tests:**
- `TestBridgeInterjectionPendingFollowsTheLiveBox`: false with no box; true after `push`; false after `withdraw` / `drainAll`; a re-registered box supersedes the old one; registering nil (the `finishWorker` / compact sites) answers false.
- `TestEnterWhileRunningRaisesThePendingSeam`: staging a row in `stateRunning` makes the bridge answer true.
- `TestSubAgentSkippedRowReadsItsResult`: a head built from an IsError result with a `SubAgentFinished` phase and no `Started` (through the `absorbFailure` error path) — collapsed, the outcome slot reads `error` and the word `scheduled` is absent; expanded, the skip content is visible as the body.
- `TestE2EQueuedMessagePreemptsTheScheduledSubAgents` (stubllm `await:` gate, `parallel-agents: 1`): the parent replies with two `sub_agent` calls; the first child's reply is held; the driver types a message and presses ⏎ (`1 queued`); the gate releases; the parent's next request carries, in order, the first child's result, the exact skip content as the second tool result, then the interjected user message — and the transcript shows the second row collapsed to the `error` verdict.

**Acceptance:** `go build ./... && go test ./internal/tui/ -run 'Bridge|Interject|SubAgentSkipped' -count=1 && go test ./cmd/apogee/ -run 'E2EQueuedMessage' -count=1`

**Commit:** `feat(tui): the queued-message mailbox answers the engine's pre-emption seam; skipped sub-agents say so`

## 3. Docs: ADR 0025 / ADR 0039 amendments, CONTEXT.md, manual — the pre-emption rule — ✅ DONE (2026-09-14)

NOTES (2026-09-14): guard grep residue left as-is because each remaining hit is either already qualified or makes no wait-claim — ADR 0025:61 ("next tool-round boundary") is the Decision 2 sentence the new amendment block directly qualifies; README.md:22 (demo alt text, "delivered at the next tool boundary") and ADR 0063:33 (D5 atomicity) name no sub-agent wait; CONTEXT.md "atomically" hits are file writes.
NOTES (2026-09-14): `IDEAS.md` is gitignored (`.gitignore:12`) — the `[P] Scheduled messages …` line is removed on disk as the item instructs and the acceptance grep passes, but the file cannot be staged or committed; it is listed under FILES as a modified file, not as one to stage.

**What:** Depends on item 2. Record the ratified rule where the old wait was implied:
- `docs/adr/0025-…`: dated amendment under Decision 2 — the boundary is unmoved, but a staged message PRE-EMPTS delegations not yet started (skipped with the bound tool result); the "drain hook" rejection stands (the seam is a predicate; the commit is still the boundary's).
- `docs/adr/0039-…`: dated amendment — the pool yields queued slots to a pending top-level message; a child yields queued grandchildren to its mailbox; running children are never cancelled (decision 4 stands).
- `CONTEXT.md` **Interjection** entry: one sentence on pre-emption; **Sub-agent** entry: the skipped delegation and its result; the `_Avoid_ "scheduled message"` note stays.
- `docs/manual/commands.md` "While the model works" paragraph: a queued message no longer waits for sub-agents that have not started — they are skipped and the model is told so.
- Code comments: `internal/tui/worker.go` `stepToBoundary` doc, `internal/agent/children.go` `drainMailbox` doc ("a top-level Run drains nothing" stays true — say the seam is the top level's only signal).
- Prose guard rule: every sentence claiming a queued message waits for "the whole task", "every child", or lands only "when the sub-agents finish" is corrected — `grep -rn -i "next tool boundary\|next tool-round boundary\|atomically" CONTEXT.md docs/adr/0025*.md docs/adr/0039*.md docs/adr/0063*.md docs/manual/*.md README.md`.
- Remove the `[P] Scheduled messages …` line from `IDEAS.md`.

**Regression guard.** The prose-guard rule extends to every sentence stating the engine or a third goroutine never sees a staged row — ADR 0025 Decision 3 line 70 ("The engine never learns a message exists until it is delivered", superseded by item 1's predicate seam) and the `interjectBox` doc (`internal/tui/interject.go:57-61`, "the ONE place the two goroutines touch the same state", which the pool workers now read too) — found with `grep -rn -i "never learns a message\|ONE place the two goroutines\|one real mutex" docs/adr/0025*.md CONTEXT.md internal/tui/interject.go`. `docs/manual/commands.md` has no "While the model works" paragraph (that is a table column header, line 25): the site is the keys paragraph (line 69, "`⏎` sends — *queues*, while the model works"), which gains the one clause.

**Files:** `docs/adr/0025-interjections-commit-at-the-between-steps-boundary.md`, `docs/adr/0039-delegations-fan-out-concurrently-bounded-by-the-servers-parallel-agents-cap.md`, `CONTEXT.md`, `docs/manual/commands.md`, `internal/tui/worker.go`, `internal/tui/interject.go`, `internal/agent/children.go`, `IDEAS.md`
**Read first:** `docs/adr/0025-interjections-commit-at-the-between-steps-boundary.md` — Decision 2, Decision 3, the "drain hook on Agent.Run" rejection; `docs/adr/0039-delegations-fan-out-concurrently-bounded-by-the-servers-parallel-agents-cap.md` — "Failures are independent" / "Cancel is unchanged" paragraph; `CONTEXT.md` — Interjection entry, Sub-agent entry; `docs/manual/commands.md` — the keys paragraph; `internal/tui/worker.go` — stepToBoundary doc

**Tests:** none beyond the guard greps (docs and comments only) — the extended grep above must return only sentences already carrying the pre-emption qualification.

**Acceptance:** `go build ./... && go vet ./internal/tui/ ./internal/agent/ && ! grep -n "Scheduled messages" IDEAS.md`

**Commit:** `docs(adr): queued messages pre-empt the sub-agents not yet started (ADR 0025 / ADR 0039 amendments)`

## 4. Engine ladder: Plan and Ask-Before run Apogee's own writers into the session scratch dir — ✅ DONE (2026-09-14)

NOTES (2026-09-14): `TestResolve_LadderTable`'s scratch rows ride a `ladderRow{ladderCase; inScratch; scratchDir}` wrapper appended to the same loop, because the existing literal table is positional and adding fields to `ladderCase` would have meant rewriting every row.
NOTES (2026-09-14): the `delete_file` pin initialises a git repo in the workspace (skipping without git on PATH) so the absent staging note is load-bearing — in a non-repo workspace the note is absent for every target.
NOTES (2026-09-14): `internal/agent/floorguards_test.go` needed no edit — `TestFloorGuard_RepairLeavesAWithdrawnToolToTheMode` sets no scratch dir, so the unchanged `planRefusalReason` const is still the wording it reads.

**What:** Second loosen of the ladder (ADR 0012 core invariant kept: bounded by path-safety to the box's writable set, the dir is the session's own). In `internal/agent`:
- `classifyWriteTarget` returns a `writeTargetClass{inFence, inScratch bool; escape string}` (changed shape; sole consumer `resolutionInput`, the `resolvedPath` twin untouched); `inScratch` = `pathWithin(abs, a.ScratchDir())` when a scratch dir is set. `resolutionInput` gains `writeTargetInScratch`.
- `resolveLadder` Plan arm: `planAdmits(in.tool) || (class == classWorkspaceWrite && in.writeTargetInScratch)` ⇒ Run; otherwise Refuse with `planRefusalReason(scratch)`: when a scratch dir is set the reason is exactly `plan mode: writes are permitted only inside the session scratch dir <path>` (the dir's spelling as `ScratchDir()` returns it); without one the old `plan mode: write tools are not permitted` stands. Ask-Before (`default` arm): `classWorkspaceWrite && in.writeTargetInScratch` ⇒ Run; everything else as today. Allow-Edits and Auto unchanged (scratch is already in-fence there).
- `loop.go` `toolMenu`: in Plan, offer a `classWorkspaceWrite` tool iff a scratch dir is set (menu and ladder key on one predicate again: `planOffers(tool, scratchSet)`); `planAdmits` keeps its read-only meaning.
- `gateReason` for a non-scratch write in Ask-Before keeps its wording.
- Terminal route untouched: `classSubprocess` still Refuse in Plan / Gate in Ask-Before regardless of the command text.
- `move_file` / `delete_file` on a scratch target in Plan spawn no git-staging child (`stageGitPaths` skips a non-repo path) — pin it.

**Regression guard.** `planRefusalReason` is a const read as a VALUE outside this item's files (`internal/agent/floorguards_test.go:799` `wireMessageContaining(…, planRefusalReason)`, `resolution_test.go:83-156`): keep the const as the no-scratch wording and add `planScratchRefusalReason(dir string) string` for the scratch-naming reason; `resolutionInput.box` (`domain.ConfinementBox`) carries only `WritablePaths`, so `resolutionInput` gains a `scratchDir string` field beside `writeTargetInScratch`. "Spawns no git-staging child" is false — `stageGitPaths` (`internal/tools/git_stage.go:72`) runs `git ls-files --error-unmatch` as a real child for EVERY move/delete, scratch target included, and only the `git add` is skipped — so pin the observable instead: the `delete_file` / `move_file` result carries neither " (deletion staged in git)" / " (rename staged in git)" nor " (git staging skipped: …)". The `resolution.go:399-401` comment ("a refusal here is now defensive only … never a tool Plan itself offered") is superseded: the Plan refusal becomes the model-facing answer for an offered writer — reword the comment with the item.

**Files:** `internal/agent/dispatch.go`, `internal/agent/resolution.go`, `internal/agent/loop.go`, `internal/agent/dispatch_test.go`, `internal/agent/resolution_test.go`, `internal/agent/writeescape_test.go`, `internal/agent/planmenu_test.go`, `internal/agent/floorguards_test.go`
**Read first:** `internal/agent/dispatch.go` — classifyWriteTarget, resolutionInput, confinementBox; `internal/agent/resolution.go` — resolveLadder, planAdmits, planRefusalReason; `internal/agent/loop.go` — toolMenu; `internal/tools/git_stage.go` — stageGitPaths

**Tests:**
- `TestDispatch_ScratchDirWriteIsInFence`: subtests "ask-before still gates it" → "ask-before runs it" (no Approver call, file lands), "plan still refuses it" → "plan runs it"; new "plan refuses a workspace target with the scratch-naming reason" asserting the exact string with the dir; "a sibling session's scratch dir stays out" and the Allow-Edits/Auto cases unchanged; `delete_file` on a scratch file in Plan lands with a result carrying none of the git-staging notes (neither "staged in git" nor "git staging skipped"); `TestFloorGuard_RepairLeavesAWithdrawnToolToTheMode` (`floorguards_test.go`) stays green on the unchanged `planRefusalReason` const.
- `TestResolve_LadderTable`: new rows for `writeTargetInScratch` in Plan and Ask-Before; `TestWriteEscapeVerdicts` "plan refuses the write and authorises nothing" becomes "plan refuses a workspace write" plus "plan mints for a scratch write".
- `TestPlanToolMenuAgreesWithTheLadder`: offered iff resolvable for a scratch target when a scratch dir is set; with none, the writers stay off the menu (`TestPlanToolMenuWithoutAScratchDirIsReadOnly`).
- Bite check: the flipped subtests fail on the pre-item tree.

**Acceptance:** `go build ./... && go test ./internal/agent/ -run 'Scratch|Ladder|WriteEscape|PlanToolMenu|Disposition' -count=1`

**Commit:** `feat(agent): Plan and Ask-Before run native writes into the session scratch dir unprompted`

## 5. Orientation bullet returns in Plan; mode prose in code, template and TUI glosses — ✅ DONE (2026-09-14)

NOTES (2026-09-14): consequential edit — internal/domain/tools.go: made necessary by the prose-guard rule (its ReadOnlyTool doc said Plan runs only read-only tools; qualified with the scratch-dir writers).
NOTES (2026-09-14): consequential edit — cmd/apogee/defaults/schedules.yaml: made necessary by the prose-guard rule (the seeded template's plan row said "it changes nothing on disk"; qualified with the firing's scratch dir).
NOTES (2026-09-14): consequential edit — internal/agent/resolvedpath_test.go: made necessary by the prose-guard rule (a test comment said Ask-Before gates every write; qualified).
NOTES (2026-09-14): consequential edit — internal/probe/confinement.go: made necessary by the prose-guard rule (a comment likened unfenceable Auto to "a plan run that fails at every write"; reworded to "a plan-shaped run that fails at every terminal command", which is what that path actually denies).
NOTES (2026-09-14): `internal/tui/schedule.go` `autoBlockedNote` ("runs in plan — read-only, but it runs") was qualified too under the extended read-only rule; no gloss test existed for either picker, so `TestModeGlossNamesTheScratchDirOnTheLowerRungs` and `TestScheduleModeGlossNamesTheScratchDir` are new. The `config.yaml` ask-before line is one line still (112 chars; the template holds longer ones). `internal/agent/loop.go:1387` and `resolution.go:433/462` were already qualified by item 4 and are unchanged.

**What:** Depends on item 4.
- `internal/agent/orientation.go`: drop the `a.Mode() != domain.ModePlan` gate on the scratch bullet — the line is announced in every mode, text verbatim; rewrite the function's comment: the mode leaves the block's inputs again (prefix-KV-cache constant within a session, as before 2026-09-14).
- `internal/domain/config.go`: `ModePlan` doc → "read-only, except Apogee's own writers into the session scratch dir"; `ModeAskBefore` doc → "every write outside the session scratch dir, every other command and every external reach needs an Approval".
- `internal/config/defaults/config.yaml` mode comments: same two qualifications, one clause each (the template gate compares comments too — keep the line count).
- `internal/tui/picker.go` `modeGloss`: Plan "reads and reports; writes only its scratch dir", Ask-Before "asks first for every edit outside its scratch dir and every command"; `internal/tui/schedule.go` `scheduleModeGloss`: Plan "read-only — it reads and reports; its only writes go to its own scratch dir".
- Prose guard rule: every code comment or gloss claiming Plan writes nothing / changes nothing / touches nothing, or that Ask-Before gates every write — `grep -rn -i "writes nothing\|changes nothing\|touch nothing\|every write\|no writes" internal/ cmd/` — is qualified.

**Regression guard.** The prose-guard grep misses the "read-only" phrasing: add `read-only` to it and state the rule as "every sentence naming Plan as read-only or write-free, help text included". The sites it must reach: `cmd/apogee/headless.go:451` (`--help` Long text "plan (the default, read-only)" — an announced surface, and item 6 makes exactly that run write), `internal/run/run.go:22,115` ("runs read-only (Plan)", "read-only floor"), `internal/agent/loop.go:1387` ("ADR: Plan is read-only"), `internal/agent/resolution.go:399` ("Plan runs the read-only floor and nothing else") — each qualified with "except its own scratch dir" (or a dated note in a comment).

**Files:** `internal/agent/orientation.go`, `internal/agent/orientation_test.go`, `internal/domain/config.go`, `internal/config/defaults/config.yaml`, `internal/config/defaults_test.go`, `internal/tui/picker.go`, `internal/tui/picker_test.go`, `internal/tui/schedule.go`, `internal/tui/schedule_test.go`, `cmd/apogee/headless.go`, `internal/run/run.go`, `internal/agent/loop.go`, `internal/agent/resolution.go`
**Read first:** `internal/agent/orientation.go` — orientationBlock, orientationScratchLine; `internal/agent/orientation_test.go` — TestOrientation_PlanModeOmitsTheScratchDir, TestOrientation_ScratchDirReturnsWhenPlanIsLeft; `internal/tui/picker.go` — modeGloss; `internal/tui/schedule.go` — scheduleModeGloss; `cmd/apogee/e2e_popups_test.go` — schedulePlanRow (prefix "read-only — it reads and reports" is Find-matched: keep it as the new gloss's prefix); `cmd/apogee/headless.go` — newHeadlessCommand Long text

**Tests:**
- `TestOrientation_PlanModeOmitsTheScratchDir` → `TestOrientation_EveryModeStatesTheScratchDir` (Plan included, line byte-equal across modes); `TestOrientation_ScratchDirReturnsWhenPlanIsLeft` deleted; `TestOrientation_WritingModesStateTheScratchDir` and `TestOrientation_FollowsAScratchDirMove` kept.
- Gloss tests pin the new Plan / Ask-Before strings; the config template gate stays green; `go build ./cmd/apogee/` after the help-text edit, and the extended grep (`read-only` included) returns only qualified sentences.

**Acceptance:** `go build ./... && go test ./internal/agent/ -run 'Orientation' -count=1 && go test ./internal/config/ ./internal/domain/ -count=1 && go test ./internal/tui/ -run 'Gloss|Picker|Schedule' -count=1`

**Commit:** `feat(agent): Plan announces its scratch dir again; mode prose says where the lower modes may write`

## 6. Announced-surface e2e: Plan and Ask-Before write the announced scratch dir; a Plan Firing does too

**What:** Depends on item 5. Drive the journey with the exact strings the program emits:
- `cmd/apogee/testdata/stubllm/announced-scratch-write.yaml` gains Plan and Ask-Before twins (or one script parameterised by `--mode`): the model reads the orientation's `Scratch dir: <path> — writable` line (the existing capture pattern) and calls `write_file` on `<path>/probe.txt`.
- A Plan refusal script: the model calls `write_file` on a workspace path in Plan; the tool result carries item 4's exact reason with the announced path.
- Headless: a `--mode plan` run whose scripted model writes into its own scratch dir lands the file with `denied: 0` (the `internal/run` denier is never consulted for a Run).

**Files:** `cmd/apogee/e2e_announced_test.go`, `cmd/apogee/testdata/stubllm/announced-scratch-write-plan.yaml`, `cmd/apogee/testdata/stubllm/announced-scratch-write-ask.yaml`, `cmd/apogee/testdata/stubllm/announced-scratch-refusal-plan.yaml`, `cmd/apogee/headless_test.go`
**Read first:** `cmd/apogee/e2e_announced_test.go` — TestE2EAnnouncedScratchDirIsWritableByTheNativeWriters, announcedScratchWritePath, toolResults; `cmd/apogee/testdata/stubllm/announced-scratch-write.yaml` — captures `Scratch dir: (\S+) — writable`; `cmd/apogee/headless_test.go` — TestHeadlessRunGetsItsOwnScratchDirAndSweepsStaleOnes, assertFiringScratchDir; `internal/run/run.go` — Result.Denied (counts denier refusals only, never a Plan Refuse); `internal/agent/dispatch.go` — appendToolResult (bare reason as Content; an advise trailer may follow — assert with Contains as floorguards_test's wireMessageContaining does)

**Tests:**
- `TestE2EAnnouncedScratchDirIsWritableInPlan`, `…InAskBefore`: no `ApprovalEvent`, file exists after the Turn, the request log shows the tool result is a success.
- `TestE2EPlanRefusalNamesTheAnnouncedScratchDir`: the refusal reason in the next request's tool result equals `plan mode: writes are permitted only inside the session scratch dir <captured path>`.
- `TestHeadlessPlanRunWritesItsOwnScratchDir`: file under `<home>/scratch/<record-id>/`, summary line `denied: 0`.
- `TestE2EAnnouncedScratchDirIsWritableByTheNativeWriters` (Allow-Edits) and `…RunsUnpromptedInAuto` unchanged and green.

**Acceptance:** `go build ./... && go test ./cmd/apogee/ -run 'E2EAnnounced|E2EPlanRefusal|HeadlessPlanRun' -count=1`

**Commit:** `test(e2e): Plan and Ask-Before write the announced scratch dir; Plan names it when it refuses`

## 7. Docs: ADR 0012 second loosen, the execution contract, ADR 0056/0023 notes, CONTEXT.md, manual

**What:** Depends on item 6. Record the loosen and its security argument:
- `docs/adr/0012-…`: dated amendment "(c) 2026-09-14 — the session scratch dir is writable in Plan and Ask-Before": the core invariant holds (bounded by path-safety to the box's writable set; the dir is per-session, 0700, one path in the fence, symlinks resolved by `EvalRealPath` at classification and again at execute via the permit-pinned `os.Root`; nothing under `~/.apogee` reads, loads or executes scratch content; the shell route is untouched; a scratch-planted executable can run only through a later mode's own gates — the pre-existing `RefuseExecFromWritablePath` scope, stated, not changed); the 2026-07-25 (b) sentence "in the lower three modes every non-read-only tool already gates" gets a dated note.
- `docs/design/confinement-execution-contract.md` §4: new row `WS-write, target in the session scratch dir | run | run | run | run | run`; legend `refuse = Plan-mode write refusal` qualified; the 2026-08-02 "ONE predicate" block gets a dated note (menu and ladder key on `planOffers`); §7 unchanged.
- `docs/adr/0056-…` D3 2026-09-14 note and `docs/adr/0023-…` 2026-08-25 amendment note: each gets a dated reversal line (Plan announces the dir again; the mode leaves the block's inputs).
- `CONTEXT.md`: **Agent mode** Plan and Ask-Before lines, **Scratch dir**, **Orientation block**, **Confinement** ("path-safety-to-workspace" → "to the box's writable set") — one clause each.
- `docs/manual/configuration.md` (system-prompt sentence, blast-radius section row for the scratch dir), `headless.md` and `daemon.md` (`plan` = read-only except its own scratch dir), `commands.md` Shift+Tab line, `README.md` "read-only Plan" mentions qualified once.
- Prose guard rule: every doc sentence stating Plan writes nothing / is read-only without the scratch qualification, or that Ask-Before gates every write — `grep -rn -i "read-only\|writes nothing\|every write\|cannot write" CONTEXT.md README.md docs/manual/*.md docs/adr/0012*.md docs/adr/0023*.md docs/adr/0056*.md docs/design/confinement-execution-contract.md` — is qualified or left with a dated note.
- Remove the `[P] Scratch dir in Plan / Ask-Before mode …` line from `IDEAS.md`.

**Files:** `docs/adr/0012-confinement-attaches-to-blast-radius-and-confine-to-workspace-flag.md`, `docs/design/confinement-execution-contract.md`, `docs/adr/0056-terminal-fail-fast-and-session-scratch.md`, `docs/adr/0023-the-system-prompt-is-a-configured-template-rendered-per-request.md`, `CONTEXT.md`, `docs/manual/configuration.md`, `docs/manual/headless.md`, `docs/manual/daemon.md`, `docs/manual/commands.md`, `README.md`, `IDEAS.md`
**Read first:** `docs/adr/0012-confinement-attaches-to-blast-radius-and-confine-to-workspace-flag.md` — Amendment (2026-07-25) line 237 "every non-read-only tool already gates", Amendment (2026-09-06); `docs/design/confinement-execution-contract.md` — §4 table, legend line 616 "**refuse** = Plan-mode write refusal"; `docs/adr/0056-terminal-fail-fast-and-session-scratch.md` — Note (2026-09-14) line 82;
`docs/adr/0023-the-system-prompt-is-a-configured-template-rendered-per-request.md` — Note (2026-09-14) line 298; `CONTEXT.md` — **Confinement** (817, "path-safety-to-workspace" 825); `docs/manual/configuration.md` — line 1448 "in every mode but Plan, which cannot write there"

**Tests:** none beyond the guard grep (docs only).

**Acceptance:** `! grep -n "Scratch dir in Plan" IDEAS.md && grep -c "2026-09-14" docs/adr/0012-confinement-attaches-to-blast-radius-and-confine-to-workspace-flag.md`

**Commit:** `docs(adr): the session scratch dir is writable in Plan and Ask-Before — ADR 0012's second loosen`
