# Sub-agent wave: skipped rows, finish-the-wave, background sub_agent

**Goal:** A skipped sub-agent row reads its reason in place and in a neutral tone; a message staged during a sub_agent wave waits for the whole wave unless sent with `ctrl+g`; on bench-approved classes `sub_agent background:true` runs the delegation as a one-item background workflow so the user's messages reach the model while the child runs.
**Date:** 2026-10-05
**Status:** unexecuted
**sized for:** ~200k-context host
**base:** db6b5d27

**Regression check (2026-10-05, db6b5d27):**
- 1: guard folded (pre-burst expand e2e at a pool width, own fixture)
- 2: guard folded (decision: item 2 owns the `preemptErrorWord` flip; failure body kept; pins and prose guard widened)
- 3: guard folded (ADR 0039 `Amended 2026-09-14` superseded in part; CONTEXT skip paragraph)
- 4: guard folded (decision: sibling `InterjectChildNow` across engine, facade, `tui.Engine` and every implementer)
- 5: guard folded (decision: wires ctrl+g only; existing journey sends with ctrl+g; files)
- 6: guard folded (decision: ADR 0089 D3 amended; ADR 0039 2026-09-20 ceiling amended; CONTEXT/ADR sentence rule)
- 7: guard folded (`publishesSeatChoice` untouched; a separate background predicate for `withoutSeatChoice`)
- 8: guard folded (decision: synthesized Receipt, report persisted to Output; final without retry; root retention handle; PlanHash stable; whole-package acceptance)
- 9: guard folded (background asked of sub_agent itself; blocking where the workflow cannot be kept; yields to `partitionDispatch`'s pure-function doc; ADR 0039 ceiling per item 6)
- 10: recast (decision: origin parameter, report read from the item's Output, no tally, multi-line sub_agent note restorable)
- 10 (re-check): guard folded (exported transcript-path helper, path only when written; SeatFallbackNote once; blank-line note separator; `sub_agent ` lead discriminator; one-line prose rule incl. CONTEXT.md:880; decision: fence-free restore-test report, fenced report keeps today's refusal)
- 11: guard folded (decision: background row paints as a background-workflow call; tally counts via item 8's Receipt; e2e lifts `workflow`)
- 12: guard folded (rule plus grep for manual sentences that count or enumerate background starts)

**Sources:**
- `bd show apogee-subagent-preempt-wave`; `bd show apogee-background-sub-agent`
- `docs/adr/0025-interjections-commit-at-the-between-steps-boundary.md` (Amended 2026-09-14)
- `docs/adr/0086-…`, `docs/adr/0087-…`, `docs/adr/0089-…`, `docs/adr/0063-…`, `docs/adr/0090-…`, `docs/adr/0031-…`
- `CONTEXT.md` — Sub-agent, Retained delegation, Run view, Workflow, Background workflow, Interjection

**Ratified design calls (owner, 2026-10-05):**
- **Finish the wave:** default; a staged message no longer preempts queued members. An explicit "now" send (`ctrl+g`) keeps today's preempt.
- **Now key:** `ctrl+g`, while a turn runs, in the main prompt and in a run view.
- **Skipped verdicts:** preempt → `not started · your message`; turn-cancel of a queued member → `stopped by you`; both neutral tone, no ✓. Wire results keep `IsError: true`.
- **Fan-out ceiling refusal:** stays `error`.
- **Old sessions:** replay re-derives the verdict from the stored result, so old skipped rows turn neutral too.
- **Stage hint:** the queued readout adds `after the wave · ctrl+g sends now` while a group has queued members; the armed-esc hint is reworded to match.
- **Background sub_agent:** option A — a one-item background workflow on ADR 0089's machinery, behind fan_out's gate (`OffersBackground && tools lift workflow`); headless and daemon block.
- **Child mode:** the background child runs the blocking sub_agent path (prose report, `max_steps`, `read-only` roster, `output_path`, may delegate); only delivery changes.
- **Finish note:** carries the child's report inline under the blocking result's cap, plus the transcript path.
- **Continue:** a named background child is retained on finish like a blocking one; `continue` with `background:true` is refused.
- **ADR 0094** amends ADR 0086, ADR 0087 ("exactly two sources") and ADR 0089.

**Closes:** apogee-subagent-preempt-wave (item 5); apogee-background-sub-agent (item 12).

**Standing requirements:**
- skills: coding-standards
- Bubble Tea v2 names only (`tea.KeyPressMsg`, `msg.String()`).
- Deviations from item text land as a dated NOTES line under the item.

**Out of scope:**
- Option B (early placeholder results) — rejected by the owner.
- Background for headless, daemon, or delegates.
- A config knob for the preempt.
- Switching skills (code-audit) to background fan_out.

## 1. Skipped sub-agent row expands in place before the burst — ✅ DONE (2026-10-05)

NOTES (2026-10-05): the pool fixture gates BOTH running children (`held` on alpha, `staged` on beta) rather than leaving one ungated: an ungated child reports before the message can be queued, so its worker would dequeue and RUN the third delegation instead of skipping it. Releasing `staged` after the queued readout is what makes the third skip at dequeue while alpha keeps the group unjoined.
NOTES (2026-10-05): the e2e awaits the parent's wrap-up request on the wire (preemptParentRequest) rather than the wrap-up text on screen, because the click leaves the viewport standing on the opened row instead of following the tail.
NOTES (2026-10-05): `preemptHome` gained a `parallel-agents` argument (the serial journey passes 1, the pool journey 2), and the file header's "the pool path gets no second journey here" sentence was rewritten to describe the new pool journey.

**What:**
**Goal:** Expanding (⏎ or click) a skipped or never-started sub-agent row before its group's results burst shows the skip's result text in place; no empty run view is pushed.
**Approach (assumed at the header base):** fixes the bug from apogee-subagent-preempt-wave, cause 1. `openRunAt` (`internal/tui/runview.go`) refuses on `span == 0 && head.done`; make it `span == 0 && subAgentReported(head)` (`subagentblock.go`). `transcript.setExpanded` (`transcript.go`) has the same `!head.done` hole — it must admit a reported head with no span, so the gesture falls through to the inline toggle. `subAgentFramed` (`subagentblock.go`) must use `!subAgentReported(head)` so an expanded, pre-burst, reported head paints unframed (`unframedSubAgentView`). Fix all three in one change and keep `setExpanded`'s doc ("the redirect's predicate, word for word") true. Fix `openRunAt`'s doc comment to match the code.
**Regression guard.** The serial fixture never holds a skipped row before the burst: at width 1 the skip is decided only after the held child returns and each slot commits at once (`dispatchGroup`'s `width <= 1` loop, `prepareCall`). Drive the pre-burst expand at a pool width — `parallel-agents: 2`, three delegations, one child gated with `await:`, one ungated so the third is skipped at dequeue — on its own fixture `cmd/apogee/testdata/stubllm/subagent-preempt-pool.yaml`.
**Files:** internal/tui/runview.go; internal/tui/transcript.go; internal/tui/subagentblock.go; internal/tui/subagentblock_test.go; internal/tui/runview_test.go; cmd/apogee/e2e_subagent_preempt_test.go; cmd/apogee/testdata/stubllm/subagent-preempt-pool.yaml
**Read first:** internal/tui/runview.go — Model.openRunAt; internal/tui/transcript.go — transcript.setExpanded; internal/tui/subagentblock.go — subAgentFramed, subAgentReported, unframedSubAgentView;
internal/tui/subagentblock_test.go — TestSubAgentSkippedRowReadsItsResult; internal/tui/runview_test.go — TestRunViewOpensOnExpand; cmd/apogee/testdata/stubllm/subagent-preempt.yaml
**Tests:** a `burst=false` case in `TestSubAgentSkippedRowReadsItsResult` driven through the Model (`enterOnLastBlock`, `clickLine`), asserting `!m.inRunView()` and the skip text on screen; a pre-burst subtest beside "a delegation that ran and left nothing keeps the inline toggle" in `TestRunViewOpensOnExpand`; the e2e expands a skipped row while one child is held on a gate, at `parallel-agents: 2` on `subagent-preempt-pool.yaml`.
**Acceptance:**
- `go build ./...`
- `go test -race -count=1 -run 'TestSubAgentSkipped|TestRunView' ./internal/tui/`
- `go test -race -count=1 -run TestE2EQueuedMessagePreempts ./cmd/apogee/`
**Commit:** `fix(tui): expand a skipped sub-agent row in place before its group's results`

## 2. Skipped delegations get a neutral verdict — ✅ DONE (2026-10-05)

NOTES (2026-10-05): the turn-cancel text is folded into delegationStoppedByUser (so proseDelegationOutcome/delegationOutcomeVerdict already word it `stopped by you`); the pre-emption text gets no DelegationOutcome field, because it is always error-shaped and only ever reaches the slot through absorbFailure (new delegationNeverStartedVerdict) — no domain change was needed.
NOTES (2026-10-05): stoppedSummary also accepts the new `not started · your message` verdict, which is what withholds the ✓ (subAgentFinished) — no separate predicate.
NOTES (2026-10-05): added TestSubAgentSkippedContentIsWhatTheVerdictMatches pinning the presenter's restated skip text to the test file's engine restatement; the replay round trip is TestSubAgentSkippedRowReplaysNeutralFromAnOldSession.
NOTES (2026-10-05): consequential edit — docs/layout/tool-layout.md: made necessary by the new neutral verdicts on the sub-agent slot list
NOTES (2026-10-05): consequential edit — docs/manual/commands.md: made necessary by the skipped and turn-cancelled rows' new verdict words

**What:**
**Goal:** A delegation skipped by an interjection preempt reads `not started · your message`, and one skipped by a turn cancel reads `stopped by you`, both in the neutral marker tone with no ✓, live and on replay of old sessions; the fan-out-ceiling refusal still reads `error`.
**Approach (assumed at the header base):** fixes the bug from apogee-subagent-preempt-wave, cause 2. The engine results stay unchanged (`IsError: true`, model-facing text untouched). In `internal/tui/toolregistry.go`, beside `delegationStoppedByUser`, add whole-content matches restating `skippedDelegationContent` and `cancelledQueuedDelegationContent` (`internal/agent/dispatch.go`). `toolView.absorbFailure` (`toolview.go`) maps them to `namedSummary(...)` with the two words. The no-✓ rule (`stoppedSummary`, `toolleader.go`; `subAgentFinished`) and `proseDelegationOutcome`/`delegationOutcomeVerdict` treat the new word like `stopped by you`. Replay: `fromWireToolView` (`transcriptbridge.go`) re-derives the verdict from the stored result body when the stored summary is `error`. Prose guard: every doc line saying a skipped row paints `error` (`grep -rn "error. verdict\|error\` verdict" CONTEXT.md layout.md docs/manual`) is restated.
**Regression guard.** Item 2 owns flipping `preemptErrorWord` (and any other e2e pin of the skipped row's verdict word) in `cmd/apogee/e2e_subagent_preempt_test.go` to `not started · your message`; item 5 builds on the flipped pin. Both new `absorbFailure` branches keep `tv.Details.with(failureBody(content))` exactly as the queued-stop branch does — `entry.neverStarted` reads `Details[0]` for the `sub-agent not started:` head. The existing `check ⋯ error` pins in `TestSubAgentSkippedRowReadsItsResult` flip to the new verdict, and the prose guard widens to `grep -rn "verdict, \`error\`\|\`error\` verdict" CONTEXT.md layout.md docs internal/tui` (it catches `subagentblock.go`'s `subAgentScheduled` doc).
**Files:** internal/tui/toolregistry.go; internal/tui/toolview.go; internal/tui/toolleader.go; internal/tui/transcriptbridge.go; internal/tui/subagentblock.go; internal/tui/toolregistry_test.go; internal/tui/subagentblock_test.go; cmd/apogee/e2e_subagent_preempt_test.go; CONTEXT.md; layout.md
**Read first:** internal/tui/toolview.go — toolView.absorbFailure; internal/tui/toolregistry.go — delegationStoppedByUser, proseDelegationOutcome; internal/tui/toolleader.go — stoppedSummary;
internal/tui/subagentblock.go — subAgentVerdictWord; internal/tui/transcriptbridge.go — fromWireToolView; internal/tui/transcript.go — entry.neverStarted; internal/agent/dispatch.go — skippedDelegationContent, cancelledQueuedDelegationContent
**Tests:** extend `TestDelegationVerdictReadsTheHumansStop` with both new contents and a ceiling refusal that stays `error`; a no-✓ case beside `TestSubAgentStoppedByYouWearsNoCheck`; a replay round trip of a stored `error` skipped row through `fromWireToolView`; `TestSubAgentSkippedRowReadsItsResult`'s `check ⋯ error` pins flip to the new verdict; `TestEscStopHintIgnoresDelegationsThatNeverStarted` keeps passing.
**Acceptance:**
- `go test -race -count=1 -run 'TestDelegationVerdict|TestSubAgentStopped|TestSubAgentSkipped|Replay|TestEscStopHintIgnoresDelegationsThatNeverStarted' ./internal/tui/`
- `go test -race -count=1 -run TestE2EQueuedMessage ./cmd/apogee/`
**Commit:** `fix(tui): show skipped and cancelled delegations in a neutral verdict`

## 3. ADR 0025 amendment: finish the wave, ctrl+g sends now — ✅ DONE (2026-10-05)

NOTES (2026-10-05): ADR 0025 decision 3's predicate sentence also gained a "since 2026-10-05, yes only for a message sent now" clause (the plan's Read first named decision 3); docs/manual/commands.md's queue rule is left for item 5, which owns it and the behaviour change.
NOTES (2026-10-05): the amendment describes the reworded armed-esc hint only as offering `ctrl+g` instead of ⏎, without quoting it — item 5 settles its exact text.

**What:**
**Goal:** ADR 0025 carries an `Amended 2026-10-05` block stating that a staged message waits for the whole sub_agent wave, queued members included, unless it was sent with `ctrl+g`, and CONTEXT.md's Interjection entry says the same.
**Approach (assumed at the header base):** add a blockquote under the 2026-09-14 block in `docs/adr/0025-…md` that narrows it: the predicate stays a yes/no answered by the Driver (ADR 0031, the engine never reads the queue); the Driver answers yes only while a "now" row is staged. The same rule holds at every depth: a child's mailbox preempts its grandchildren only for a "now" message. The model's `workflow message` never preempts. Record the stage hint and the verdicts from the header calls. Update the `Status:` text and restate `CONTEXT.md`'s Interjection preempt paragraph.
**Regression guard.** ADR 0039's `Amended 2026-09-14 — the pool yields its queued slots to a pending message` states the reversed rule (any staged message skips queued slots) and is superseded in part: add an `Amended 2026-10-05` blockquote beside it naming the "now"-only rule and linking ADR 0025's. Restate CONTEXT.md's sub-agent skip paragraph as well; the `Agent.InterjectChild(runID, in)` line stays true (item 4 adds a sibling) — name `InterjectChildNow` beside it.
**Files:** docs/adr/0025-interjections-commit-at-the-between-steps-boundary.md; docs/adr/0039-delegations-fan-out-concurrently-bounded-by-the-servers-parallel-agents-cap.md; CONTEXT.md
**Read first:** docs/adr/0025-interjections-commit-at-the-between-steps-boundary.md — Amended 2026-09-14 block, decision 3; docs/adr/0039-delegations-fan-out-concurrently-bounded-by-the-servers-parallel-agents-cap.md — Amended 2026-09-14;
CONTEXT.md — sub-agent skip paragraph, InterjectChild mailbox paragraph; docs/manual/commands.md — queue rule paragraph
**Tests:** none (docs).
**Acceptance:**
- `grep -n "Amended 2026-10-05" docs/adr/0025-interjections-commit-at-the-between-steps-boundary.md`
- `grep -n "Amended 2026-10-05" docs/adr/0039-delegations-fan-out-concurrently-bounded-by-the-servers-parallel-agents-cap.md`
- `grep -n "ctrl+g" CONTEXT.md`
**Commit:** `docs(adr): amend ADR 0025 — a message waits for the wave unless sent now`

## 4. Engine: a child mailbox preempts only for a "now" message — ✅ DONE (2026-10-05)

NOTES (2026-10-05): `childMailbox.hasPending` renamed to `hasNowPending`, because it now answers only for a row sent now; `childMailbox.add` gained a `now bool` parameter, and the queue holds `mailboxRow{in, now}` rows. `drain`/`close` still return `[]domain.UserInput`.
NOTES (2026-10-05): consequential edit — internal/agent/childrun_test.go: made necessary by `childMailbox.add` gaining its `now` parameter (three call sites pass `false`; one more in children_test.go).
NOTES (2026-10-05): consequential edit — internal/agent/doc.go: made necessary by adding `InterjectChildNow` beside `InterjectChild` in the package's door map.
NOTES (2026-10-05): the new `TestInterjectChild_OrdinaryMessageLetsAGrandchildRun` sets `Delegation.MaxDepth = 2` so the child really holds `sub_agent`; the existing now-test keeps the default depth, as before (its skip runs before the tool lookup). `internal/agent/fanout_test.go` needed no change: its `InterjectChild` call steers a depth-1 child that has no grandchildren.

**What:**
**Goal:** A child's mailbox reports a pending interjection to its grandchild dispatch only while it holds a message marked "now"; an ordinary message to a running child still lands at the child's next boundary.
**Approach (assumed at the header base):** depends on item 3. Add a "now" flag to the child mailbox rows (`internal/agent/children.go`, `mailbox.hasPending`) and to `Agent.InterjectChild` (a new parameter or a sibling `InterjectChildNow`; keep one deep entry point). `interjectionPending` (`dispatch.go`) is unchanged at depth 0 (still `cfg.InterjectionPending`). The `workflow` tool's `message` action (`workflowcall.go`) sends non-now. Update every `InterjectChild` caller (grep `InterjectChild(`).
**Regression guard.** The "now" path is one sibling method `InterjectChildNow` (same signature as `InterjectChild`), and item 4 owns adding it across the engine Agent, the public `apogee.Agent` alias/facade, the `tui.Engine` interface and every implementer and test fake (grep `InterjectChild(`: `lateEngine` in `cmd/apogee/wire_engine.go`, the fake engine in `internal/tui/seam_test.go`); `InterjectChild(runID, in)` stays the ordinary send, both sharing one recursive helper; item 5 only wires the TUI's ctrl+g to it.
**Files:** internal/agent/children.go; internal/agent/dispatch.go; internal/agent/workflowcall.go; internal/agent/children_test.go; internal/agent/fanout_test.go; internal/tui/interject.go; apogee.go; internal/tui/tui.go; internal/tui/seam_test.go; cmd/apogee/wire_engine.go
**Read first:** internal/agent/children.go — childMailbox, childMailbox.hasPending, Agent.InterjectChild; internal/agent/dispatch.go — Agent.interjectionPending, preemptDelegation;
internal/agent/workflowcall.go — workflow message action; internal/agent/children_test.go — TestInterjectChild_PendingMailboxSkipsAGrandchild; cmd/apogee/wire_engine.go — lateEngine.InterjectChild; internal/tui/tui.go — Engine
**Tests:** `TestInterjectChild_PendingMailboxSkipsAGrandchild` sends now; a new case where an ordinary message lets the grandchild run; the existing pool/serial preempt tests keep passing.
**Acceptance:**
- `go build ./...`
- `go vet . ./internal/agent/ ./internal/tui/ ./cmd/apogee/` (compiles the test fakes)
- `go test -race -count=1 -run 'TestInterjectChild|TestFanOut_PendingInterjection|TestDispatchSerially_Pending' ./internal/agent/`
**Commit:** `feat(agent): a child's mailbox preempts grandchildren only for a now message`

## 5. TUI: finish the wave by default, ctrl+g sends now — ✅ DONE (2026-10-05)

NOTES (2026-10-05): `interjectBox.pending()` is kept, and the now-only sibling `pendingNow()` is added; `Bridge.InterjectionPending` reads `pendingNow()`. `pending()` now has a single caller, the existing `TestRecipeLineWhileRunningIsRefused` in workflowblock_test.go. ⏎ and ctrl+g share two new helpers, `sendAtIdle` and `stageWhileRunning(now)`, in interject.go.
NOTES (2026-10-05): `TestEnterWhileRunningRaisesThePendingSeam` is renamed to `TestInterjectNowRaisesThePendingSeam`. Its claim is reversed by this item (⏎ no longer raises the seam; ctrl+g does), so the old name would be false.
NOTES (2026-10-05): the wave hint (`queuedWaveHint`, `Model.waitsForWave`) shows only while a top-level message staged with ⏎ waits. A now row, a child's row or a queued command does not show it, and a now row hides it, because "after the wave" would then be untrue.
NOTES (2026-10-05): the serial journey `TestE2EQueuedMessageWaitsForTheWholeWave` (`parallel-agents: 1`) asserts the behaviour and `1 queued`, but not the hint text. A serial wave draws no row for a member it has not started, so the TUI cannot see the queued member. The hint is asserted on screen by a new pooled journey, `TestE2EQueuedMessageWaitsForAPooledWaveAndSaysSo`, which reuses subagent-preempt-pool.yaml. The pool journey that already existed now sends its message with ctrl+g.
NOTES (2026-10-05): the /help cell is spelled `ctrl+g send now`, matching the readout's and the esc hint's `ctrl+g` (pinned in TestHelpNoteListsEveryVerb). It is not spelled `⌃g`.
NOTES (2026-10-05): consequential edit — internal/tuitest/driver_test.go: made necessary by adding `CtrlG` to keys.go (TestKeysDecodeAsIntended pins every key).
NOTES (2026-10-05): consequential edit — cmd/apogee/testdata/stubllm/subagent-preempt-pool.yaml: made necessary by the pool journey now sending with ctrl+g and the new ⏎ pooled journey reusing the script (header comment only).
NOTES (2026-10-05): consequential edit — layout.md: made necessary by rewording `escStopHintSkipsFormat` and adding the queued readout's wave hint.

**What:**
**Goal:** While a turn runs, ⏎ stages a message that lets the running sub_agent wave finish, queued members included, before it lands; `ctrl+g` stages one that skips the queued members (today's behaviour), in the main prompt and in a run view; the queued readout shows `after the wave · ctrl+g sends now` while a group has queued members; idle, `ctrl+g` submits like ⏎.
**Approach (assumed at the header base):** depends on items 2 and 4. `queuedInterjection` (`internal/tui/interject.go`) gains a `now` flag; `stageInterjection`/`stageChildMessage` take it; the `ctrl+g` case sits beside `case "enter"` in `model.go`. `Bridge.InterjectionPending` (`bridge.go`) returns true only when a now row is staged (`mailbox.pending()` gains a now-only sibling). The readout (`queuedSegment`) and `escStopHintSkipsFormat` are reworded; `/help` legend and `docs/manual/commands.md` (queue rule, keys) name `ctrl+g`. Flip `preemptErrorWord` in the e2e to the new verdict; add a finish-the-wave e2e.
**Regression guard.** Item 5 calls `InterjectChildNow` (item 4) from a run view's ctrl+g and never changes the engine seam. `preemptErrorWord` is already flipped by item 2; this item builds on that pin. The existing journey `TestE2EQueuedMessagePreemptsTheScheduledSubAgents` stages its message with ctrl+g (⏎ now lets beta run) — add `CtrlG Key = "\x07"` to `internal/tuitest/keys.go` — and the new finish-the-wave journey gives beta a fixture turn. `escStopHintSkipsFormat` is pinned by `TestEscStopHintNamesWhatASecondEscDoes`, and the `/help` legend lives in `helpKeyLegend`.
**Files:** internal/tui/interject.go; internal/tui/bridge.go; internal/tui/model.go; internal/tui/model_test.go; internal/tui/help.go; internal/tui/help_test.go; internal/tui/interject_test.go; internal/tui/bridge_test.go; internal/tuitest/keys.go; cmd/apogee/e2e_subagent_preempt_test.go; cmd/apogee/testdata/stubllm/subagent-preempt.yaml; docs/manual/commands.md
**Read first:** internal/tui/bridge.go — Bridge.InterjectionPending; internal/tui/interject.go — queuedInterjection, stageInterjection, stageChildMessage, Model.queuedSegment;
internal/tui/model.go — handleKey `case "enter"`, escStopHintSkipsFormat; internal/tui/help.go — helpKeyLegend; cmd/apogee/e2e_subagent_preempt_test.go — TestE2EQueuedMessagePreemptsTheScheduledSubAgents
**Tests:** `TestBridgeInterjectionPendingFollowsTheLiveBox` split for ⏎ vs `ctrl+g`; e2e: ⏎ during a wave at `parallel-agents: 1` runs every member, then the message lands; `ctrl+g` skips the queued members with `not started · your message`; the hint text on screen; `TestEscStopHintNamesWhatASecondEscDoes` and the help legend test follow the reworded text.
**Acceptance:**
- `go test -race -count=1 -run 'TestBridge|TestInterject|TestEscStopHint|TestHelp' ./internal/tui/`
- `go test -race -count=1 -run 'TestE2EQueuedMessage' ./cmd/apogee/`
**Closes:** apogee-subagent-preempt-wave
**Commit:** `feat(tui): a message waits for the sub-agent wave unless sent with ctrl+g`

## 6. ADR 0094: sub_agent may run in the background — ✅ DONE (2026-10-05)

NOTES (2026-10-05): ADR 0087 is amended beyond its "exactly two sources" lead — D2 ("`sub_agent` is unchanged"), D9 ("workflow children do not delegate") and D10's entry points are each made false by the background child mode, so each gains a dated blockquote and the Amends line names them.
NOTES (2026-10-05): ADR 0089 is also amended at D5 (save-as-recipe "any workflow" — a sub_agent-origin workflow offers none), beside the lead, D1 and D3 the item names; ADR 0094 records the plan's header calls as D1–D10 (gate, child mode, finish note, retention/continue refusal, capacity, outside the tool round, headless/daemon blocking, call-id-salted plan hash, `/workflows` origin, ADR 0031/bench).
NOTES (2026-10-05): consequential edit — CONTEXT.md Receipt entry: made necessary by ADR 0094 D2 (a background sub_agent's item child carries no `finish`; the engine builds its Receipt), beyond the Workflow, Background workflow and Sub-agent entries the item names.
NOTES (2026-10-05): retry — ADR 0094 D3 and the ADR 0089 D3 amendment put the transcript path on the `sub_agent <name> <outcome> — transcript: <path>` lead line (named only when one was written) with the report under it, per the run's decision.

**What:**
**Goal:** `docs/adr/0094-*.md` records sub_agent's `background` switch as a one-item background workflow behind ADR 0089's gate, with every header call for background sub_agent as a numbered decision; ADR 0086, 0087 and 0089 carry dated amendment blockquotes linking it; CONTEXT.md speaks it.
**Approach (assumed at the header base):** front matter `Status: accepted` and `Amends: ADR 0086 (…), ADR 0087 ("exactly two sources" — a third: sub_agent background), ADR 0089 (D1 launchers; the lead "sub_agent stays blocking")`. Decisions: gate; child mode; finish note inline under the blocking cap plus transcript path; retention and `continue` refusal; capacity per ADR 0089 D2; not subject to the ADR 0025 preempt (it is not in the tool round); headless/daemon run it blocking; plan hash salted with the call id, so a re-issue never resumes a finished one; `/workflows` lists it with a sub_agent origin and no save-as-recipe; ADR 0031 holds (engine emits, Driver wakes); bench drivability. Reconcile CONTEXT.md's Background workflow `_Avoid_: "async delegation"` line, Workflow sources, Sub-agent.
**Regression guard.** ADR 0094 also amends ADR 0089 D3 ("the note it carries is one line"): a background sub_agent's finish note carries the child's multi-line report under the blocking cap; add that clause to the Amends line and a dated blockquote under 0089 D3. It also amends ADR 0039's `Amended 2026-09-20` ceiling (which binds "every later `sub_agent` call"): a background sub_agent is not counted — Amends line plus a blockquote there. Rule: every CONTEXT.md/ADR sentence that says every workflow item hands back a Receipt, names exactly two sources, or says sub_agent stays blocking is restated (`grep -n "hands back a \*\*Receipt\|two sources\|stays blocking" CONTEXT.md docs/adr/008[679]-*.md`, plus ADR 0087's Decision lead, whose "exactly two" / "sources" wraps across lines).
**Files:** docs/adr/0094-sub-agent-may-run-as-a-one-item-background-workflow.md; docs/adr/0086-a-delegation-is-stopped-singly-and-a-named-one-stays-continuable-for-the-session.md; docs/adr/0087-the-engine-runs-workflows-the-model-or-a-recipe-asks-for.md; docs/adr/0089-a-workflow-may-run-in-the-background-and-wakes-the-agent-when-it-ends.md; docs/adr/0039-delegations-fan-out-concurrently-bounded-by-the-servers-parallel-agents-cap.md; CONTEXT.md
**Read first:** docs/adr/0089-*.md — Amends header, D1, D3; docs/adr/0087-*.md — Decision lead "exactly two sources"; docs/adr/0086-*.md — "We keep the blocking model" + 2026-09-27 amendment;
docs/adr/0039-*.md — 2026-09-20 ceiling amendment; CONTEXT.md — Workflow, Background workflow (_Avoid_), Sub-agent
**Tests:** none (docs).
**Acceptance:**
- `grep -l "0094" docs/adr/0086-*.md docs/adr/0087-*.md docs/adr/0089-*.md docs/adr/0039-*.md`
- `grep -n "^Amends:" docs/adr/0094-*.md`
**Commit:** `docs(adr): ADR 0094 — sub_agent may run as a one-item background workflow`

## 7. sub_agent publishes background behind fan_out's gate — ✅ DONE (2026-10-05)

NOTES (2026-10-05): the gate tests landed in internal/tools/sub_agent_test.go beside the existing sub_agent schema pins (`TestSubAgent_RegistryGates`, `TestSubAgentSchema_EachGateAddsOnlyItsProperty`), so internal/tools/registry_test.go is untouched; the registry hoists the shared gate into one local `background` that both sub_agent and fan_out read.
NOTES (2026-10-05): the workflow tool's `id` property description now reads "as fan_out, sub_agent or status gave it", beside the tool description the item names — same model-facing fact, same tool.

**What:**
**Goal:** The `sub_agent` schema has a `background` property exactly when the host offers background and the tools lift `workflow`; elsewhere, and for every delegate, the schema is byte-identical to today's.
**Approach (assumed at the header base):** depends on item 6. `internal/tools/sub_agent.go`: a second schema hole and `SubAgentOptions.Background`, `OffersBackground()`, `SubAgentArgs.Background`. `builtinToolsWith` (`registry.go`) sets it with the fan_out expression `host.OffersBackground && host.rosterDeltas().lifts(WorkflowToolName)`. `withoutSeatChoice`/`publishesSeatChoice` (`internal/agent/subagent.go`) also strip background for children. The `workflow` tool description (`workflow.go`) says "workflows you started with fan_out or sub_agent".
**Regression guard.** Leave `publishesSeatChoice` as it is — it alone gates the Delegations seat line (`orientation.go`), the reading of `run_on` (`subagent.go`, `dispatch.go`) and the width (`delegationwidth.go`), so widening it would give a `sub-agents-choice: fixed` session with `workflow` lifted a seat bullet and a read `run_on`. Add `SubAgent.OffersBackground` plus a separate predicate used only by `withoutSeatChoice`, so the swap to plain triggers on `run_on` OR background, as `publishesFanOutChoice` does for fan_out.
**Files:** internal/tools/sub_agent.go; internal/tools/registry.go; internal/tools/workflow.go; internal/tools/sub_agent_test.go; internal/tools/registry_test.go; internal/agent/subagent.go; internal/agent/subagent_test.go
**Read first:** internal/tools/sub_agent.go — subAgentSchemaTemplate, SubAgentOptions, NewSubAgentWith; internal/tools/registry.go — builtinTools, HostTools.OffersBackground;
internal/agent/subagent.go — withoutSeatChoice, publishesSeatChoice, publishesFanOutChoice; internal/agent/orientation.go — delegationSeats
**Tests:** `TestSubAgentPlainSchemaIsTheOneShippedBeforeSeatChoice` unchanged and passing; a gate test per `TestFanOut_RegistryGates`/`TestFanOutSchema_EachGateAddsOnlyItsProperty`; a child's menu carries no background; with `sub-agents-choice: fixed` and `workflow` lifted, `publishesSeatChoice` stays false (no seat bullet, `run_on` unread).
**Acceptance:**
- `go test -race -count=1 ./internal/tools/`
- `go test -race -count=1 -run 'TestSubAgent|Seat' ./internal/agent/`
**Commit:** `feat(tools): sub_agent offers background behind the workflow gate`

## 8. Workflow items gain a sub_agent mode

**What:**
**Goal:** A workflow plan can carry one item that runs the blocking sub_agent path (prose report, `max_steps`, roster incl. `read-only`, `output_path`, delegation at the configured depth, ledger row, retention of a named child), and its run status records a sub_agent origin that survives `plan.json`/`status.json` round trips.
**Approach (assumed at the header base):** depends on item 7. One deep path: extract the child-running core of `runSubAgent` (`internal/agent/subagent.go`) so the blocking dispatch and the workflow spawner (`workflowSpawner.Spawn`, `workflowspawn.go`) call the same function; no copy. The item carries `SubAgentArgs` (or its fields) in `workflow.ItemSpec`/`Stage` (`internal/workflow/plan.go`) under a mode the spawner switches on; such an item gets no `finish` tool and no Receipt. Add an additive `Origin` to `RunStatus` (`internal/workflow/store.go`).
**Regression guard.** Replacing "no Receipt" above: a sub_agent-mode item persists the child's prose report (`Outcome.Report`, after the blocking cap) to the item's Output file in the workflow folder, and records a Receipt synthesized from the child's outcome (completed → ok, capped → partial, faulted → blocked, stopped → as a stopped workflow item is tallied today) so `workflow.TallyOf`, the TUI finish line and `/workflows` count it honestly; the child still gets no `finish` tool. Such an item is final on any non-stopped ending — `Runner.isFinal`/`exhaustedReceipt` never retry or continue it (no "call finish" continuation). The spawner carries a pointer handle to the root's `retainedDelegates` (as it carries `children`; never a copy of the mutex struct — `backgroundHost` builds fresh ones), and the item text decides whether a background child books a ledger row. New `Stage`/`ItemSpec` fields are `json:"…,omitempty"` and `yaml:"-"` so `PlanHash` of every existing plan is unchanged and no recipe can set them.
**Files:** internal/workflow/plan.go; internal/workflow/store.go; internal/workflow/store_test.go; internal/workflow/runner.go; internal/workflow/runner_test.go; internal/agent/subagent.go; internal/agent/workflowspawn.go; internal/agent/workflowspawn_test.go; internal/agent/background_test.go
**Read first:** internal/agent/subagent.go — runSubAgent, delegationResult; internal/agent/workflowspawn.go — workflowSpawner.Spawn, workflowPhaseBody; internal/workflow/runner.go — Outcome, isFinal, exhaustedReceipt;
internal/workflow/store.go — RunStatus, PlanHash; internal/agent/background.go — backgroundHost; internal/agent/children.go — retainedDelegates
**Tests:** store round trip of `Origin`; a spawner test that a sub_agent-mode item gets the blocking child's tools (no `finish`), honours `max_steps`, and returns the prose report; a completed sub_agent item is spawned exactly once and tallies ok, with its report in the item's Output file; a pre-item plan's `PlanHash` is unchanged; a named child run through the background host is retained on the root (a later `continue` finds it).
**Acceptance:**
- `go test -race -count=1 ./internal/workflow/`
- `go test -race -count=1 ./internal/agent/`
**Commit:** `feat(workflow): a workflow item may run the sub_agent path`

## 9. Dispatch launches a background sub_agent

**What:**
**Goal:** On a host that offers it, `sub_agent` with `background:true` returns at once with a handle (workflow id and name) and runs as a one-item background workflow; it is neither pooled, nor preempted, nor counted against the fan-out ceiling; `continue` with `background:true` is refused; where the switch is not offered (headless, daemon, delegates) the call runs blocking.
**Approach (assumed at the header base):** depends on item 8. `partitionDispatch`, `prepareCall` (`dispatch.go`) and `resolve` (`resolution.go`) route a background sub_agent call (an `isBackgroundSubAgentCall` check reusing `asksBackground` and `offersBackground`, `workflowcall.go`) to the workflow verdict, before any `SpawnRunID` is minted. A builder turns `SubAgentArgs` into the one-item plan, salted with the call id; launch through `startBackground` (`background.go`). The immediate result has its own sub_agent wording beside `fanOutBackgroundStarted`/`Queued`.
**Regression guard.** `isBackgroundSubAgentCall` reads the sub_agent tool's own `OffersBackground()` (item 7) plus `!isDelegate()`, never `offersBackground`'s fan_out lookup (fan_out is default-off, so `workflow` lifted without it must still launch). Where the workflow cannot be kept — Plan with no scratch dir (`fanOutNoScratch`), no scratch, no workspace (`launchRefusal`) — the call runs blocking, the Goal's rule for an unoffered switch, never a `fan_out was not run:` refusal. Yields to `partitionDispatch`'s doc (`dispatch.go`, "a pure function of the call list"): the background answer is passed in, not read from Agent state. The ADR 0039 2026-09-20 ceiling is amended by item 6 for this call.
**Files:** internal/agent/dispatch.go; internal/agent/resolution.go; internal/agent/workflowcall.go; internal/agent/background.go; internal/agent/workflowcall_test.go; internal/agent/background_test.go
**Read first:** internal/agent/dispatch.go — partitionDispatch, prepareCall, refusePastCeiling; internal/agent/resolution.go — resolve (step 3); internal/agent/workflowcall.go — asksBackground, offersBackground, fanOutNoScratch;
internal/agent/launch.go — buildLaunch; internal/agent/background.go — startBackground
**Tests:** copies of `TestWorkflowCall_BackgroundAnswersAtOnceAndRunsBesideTheConversation` and `..._BackgroundRunsBlockingWhereTheSwitchIsNotOffered` for sub_agent; continue+background refused; two identical background tasks both run; a pending interjection does not skip it; `TestWorkflowControl_*` status/message/stop reach it; `workflow` lifted with fan_out not lifted still launches in the background; Plan with no scratch dir runs the call blocking.
**Acceptance:**
- `go test -race -count=1 ./internal/agent/`
**Commit:** `feat(agent): sub_agent background runs as a one-item workflow`

## 10. The finish note carries the background child's report

**What:**
Recast at the regression check (2026-10-05).
**Goal:** When a background sub_agent ends, its finish note carries the child's report inline under the blocking result's cap plus the transcript path, delivered at the next between-Steps boundary or as a wake when idle; fan_out notes are unchanged.
**Approach (assumed at the header base):** depends on item 9. `finishNote` (`internal/agent/background.go`) switches on the run's `Origin`; reuse the cap the blocking delegation result is held to (find it in the `runSubAgent` result path), never a new number. `Agent.Wake` and `workflow-wake` are unchanged.
**Regression guard.** `finishNote` learns the run's origin through a new parameter carried from `run.plan`/`backgroundRun` (update its callers incl. `TestFinishNote_SaysHowTheWorkflowEnded`); the sub_agent note is `sub_agent <name> <outcome> — transcript: <path>` followed by the report read from the item's Output file, with no tally part; `checkRestoredWorkflowNotes` accepts a multi-line note only for a sub_agent-origin note (fan_out notes keep the one-line check), and a snapshot restore round-trip test of a held multi-line sub_agent note is added; a test pins the successful note's exact text. Supersedes ADR 0089 D3's "one line" for this note, amended by ADR 0094 (item 6).
The transcript path comes from a new exported helper in `internal/workflow/store.go` (beside `ReadItemTranscript`) and is named only when the file exists (a child that faulted before starting writes none — pin that note); a sub_agent note carries `SeatFallbackNote` exactly once (from the persisted report or appended, never both — pin a fellBack case). `renderWorkflowNotes` separates notes with `workflowNoteSeparator` once any held note spans lines (pin a sub_agent-then-fan_out render). The restore discriminator is the note's `sub_agent ` lead (no `workflow …` fan_out note starts with it), and every line stays fence-checked.
The snapshot restore round-trip test uses a fence-free multi-line report; a report quoting an engine fence keeps today's restore refusal (forgesRestoredStructure), the same exposure a committed blocking result has.
Rule: restate every comment or doc sentence that calls a finish note one line, found with `grep -rn "one line\|one-line" internal/agent/background.go internal/agent/workflowcall.go CONTEXT.md docs/manual/workflows.md | grep -i note` (incl. CONTEXT.md:880 "one-line finish note", outside item 6's grep).
**Files:** internal/agent/background.go; internal/agent/workflowcall.go; internal/agent/wake_test.go; internal/agent/state_test.go; internal/workflow/store.go; CONTEXT.md; docs/manual/workflows.md
**Read first:** internal/agent/background.go — finishNote, checkRestoredWorkflowNotes, renderWorkflowNotes; internal/agent/subagent.go — delegationResult, capDelegateResult;
internal/workflow/store.go — WriteTranscript, ReadItemTranscript; internal/agent/state_test.go — TestRestoreState_ChecksTheHeldWorkflowNotes
**Tests:** beside `TestFinishNote_SaysHowTheWorkflowEnded`: a sub_agent note holds the report and path, its successful text pinned exactly; an over-cap report is cut like a blocking one; boundary and idle-wake cases per `TestWake_*`; a snapshot restore round trip of a held fence-free multi-line sub_agent note (beside `TestRestoreState_ChecksTheHeldWorkflowNotes`), and a multi-line fan_out note still refused; a faulted-before-start sub_agent note names no transcript; a fellBack sub_agent note carries `SeatFallbackNote` once; a sub_agent-then-fan_out render is blank-line separated.
**Acceptance:**
- `go test -race -count=1 ./internal/agent/`
- `go test -race -count=1 ./internal/workflow/`
**Commit:** `feat(agent): a background sub_agent's finish note carries its report`

## 11. TUI shows a background sub_agent

**What:**
**Goal:** `/workflows` lists a background sub_agent run under its delegation name with a sub_agent origin and offers no save-as-recipe; its stage opens its item's run view; the finish line names it; a driven test covers the launch, a user message reaching the model while the child runs, and the finish note's wake.
**Approach (assumed at the header base):** depends on item 10. `internal/tui/workflows.go`: label by `Origin`, gate `^s` on it. `finishLine`/`backgroundFinishFormat` (`internal/tui/workflow.go`) word it. The status-line readout already folds `workflow-` calls.
**Regression guard.** A background sub_agent `ToolCallEvent` (empty `SpawnRunID`) paints as a background-workflow call row like fan_out background, never as a delegation block; the paint sites are `toolview.go`, `transcript.go`'s sub_agent head path and `fold.go`. The finish line and the `/workflows` row count the item through item 8's synthesized Receipt; pin both texts in the driven test. The e2e home lifts `tools: enabled: [workflow]` (modelled on `preemptHome`) and holds the child with a stub `await:` gate the test releases (`stub.Release`).
**Files:** internal/tui/workflows.go; internal/tui/workflow.go; internal/tui/toolview.go; internal/tui/transcript.go; internal/tui/fold.go; internal/tui/workflow_test.go; internal/tui/workflows_test.go; cmd/apogee/e2e_background_subagent_test.go; cmd/apogee/testdata/stubllm/background-subagent.yaml
**Read first:** internal/tui/workflows.go — openWorkflowSave, shownInfo; internal/tui/workflow.go — finishLine, backgroundFinishFormat, tallyLine; internal/workflow/tally.go — countOutcome;
cmd/apogee/e2e_subagent_preempt_test.go — preemptHome; internal/stubllm/script.go — Turn.Await
**Tests:** e2e with a stub upstream: model calls `sub_agent background:true`; while the child is held, the user's ⏎ message reaches the model at its next boundary; the child finishes; the idle agent wakes on a note carrying the report; `/workflows` shows it without `^s`; the finish line and the `/workflows` row text pinned; a background sub_agent call row is not a sub-agent block.
**Acceptance:**
- `go test -race -count=1 -run 'TestBackgroundWorkflow|TestBackgroundSubAgent|TestWorkflows' ./internal/tui/`
- `go test -race -count=1 -run TestE2EBackgroundSubAgent ./cmd/apogee/`
**Commit:** `feat(tui): show a background sub_agent in /workflows and its finish`

## 12. Manual speaks background sub_agent

**What:**
**Goal:** The manual documents sub_agent's `background` switch, its gate, its finish note, and that headless and daemon run it blocking.
**Approach (assumed at the header base):** depends on item 11. `docs/manual/workflows.md` (roster paragraph, Background workflows, `workflow` tool), `docs/manual/configuration.md` (lifting `workflow` also gives sub_agent background), `docs/manual/headless.md`, `docs/manual/commands.md` (`/workflows`), `docs/manual/README.md` index blurb.
**Regression guard.** The site list is not closed: every manual sentence that counts or enumerates what starts, lists or resumes a background workflow, or what lifting `workflow` gives, is restated — e.g. `workflows.md` "There are two ways to start one", "once it ends, the same call resumes it" (false for a call-id-salted sub_agent plan), "every `fan_out` and every recipe run", and the "gives `fan_out` its `background` switch" lines in `configuration.md`/`workflows.md`. Find them with `grep -n "background\|two ways\|every \`fan_out\`" docs/manual/*.md README.md`.
**Files:** docs/manual/workflows.md; docs/manual/configuration.md; docs/manual/headless.md; docs/manual/commands.md; docs/manual/README.md; README.md
**Read first:** docs/manual/workflows.md — Background workflows, The workflows view; docs/manual/configuration.md — tools enabled list; docs/manual/commands.md — /workflows row;
docs/manual/headless.md — recipe paragraph; internal/tools/manual_drift_test.go — TestReadmeStatesTheToolCounts; README.md — Workflows bullet
**Tests:** none (docs); the pinned counts stay unchanged.
**Acceptance:**
- `go test -race -count=1 -run 'TestReadmeStatesTheToolCounts|TestManualStatesTheMomentCount' ./internal/tools/ ./internal/domain/`
- `grep -n "background" docs/manual/workflows.md | grep -i sub_agent`
**Closes:** apogee-background-sub-agent
**Commit:** `docs(manual): document sub_agent's background switch`
