# Sub-agent rows marked done before they finish — plan

**Goal:** A sub-agent row shows ✓ done only once its own child has handed back a real report. Runs are told apart by an engine-minted run id instead of the model's or server's call ids. A child that ends without a report says so. The undelivered-interjection note gives the real reason the message did not land.
**Date:** 2026-09-24
**Status:** unexecuted
**sized for:** ~200k-context host
**base:** 3d6fd1a6
**Closes:** apogee-subagent-premature-done

**Sources:**
- `bd show apogee-subagent-premature-done` (the diagnosis: causes #1–#4)
- `docs/adr/0039-*.md` §5–6 (child streams identified by the spawning call id)
- `docs/adr/0063-*.md` D1/D3; `docs/adr/0059-*.md` §6
- `docs/adr/0075-the-headless-event-stream-is-a-versioned-driver-protocol.md` §3, §7, §8, §10 (envelope key set; EventBase not extended; call tree from call ids; additive within `v:1`)
- `docs/plans/archived/2026-09-18 - 00 - capped-delegate-report-loss-plan.md` (the capped-path non-report head)
- `CONTEXT.md` — **Event**, **Sub-agent**, **Step cap**, **Interjection**

**Ratified design calls** (owner, 2026-09-24):
- **Run identity:** the engine mints a unique run id for each delegation and puts it on `domain.EventBase`. It is persisted and emitted in the JSON event stream, and the TUI keys runs and pairs results by it. Ids sent to the server are never rewritten (the `dispatchableCalls` rule stands). Sessions recorded before this change fall back to (depth, spawn call id).
- **Non-report (#3):** row label only. The TUI reads the report's shape with the capped path's classifier, moved to a shared package. The row reads `· ended without a report` in the step-cap tone and shows no ✓. The parent model receives exactly the same text as today.
- **Undelivered note (#4):** `domain.ChildInterjectionEvent` gains a reason set by each emitter, and each reason has its own wording.

**Standing requirements:**
- skills: coding-standards
- The Bubble Tea v2 `Model` is copied by value: no no-copy type is held by value (ADR 0011).
- Nothing in these items changes a request sent to any model.

**Out of scope:**
- Addressing `InterjectChild` by run id. It is still addressed by spawn call id, last-in-wins (`childRegistry`). This is filed as a follow-up bead.
- De-duplicating or rewriting native tool-call ids on the wire.
- Re-prompting a child that ended on narration (a Floor-guard / Reaction question).
- `schedule.go` Firing pairing by call id.

**Regression check (2026-09-24, 3d6fd1a6):**
- 1: guard folded (owner decision: supersedes ADR 0075 §7/§8 for delegation identity)
- 2: guard folded (owner decision: `run_id` envelope member, additive within `v:1`)
- 3: guard folded (owner decision: every `runRef{…}` literal carries the run id; item 3 owns transcriptbridge)
- 4: guard folded
- 5: guard folded
- 6: guard folded (owner decision: the no-report verdict is an outcome envelope)
- 7: guard folded
- 8: guard folded (owner decision: also owns `docs/manual/headless.md` and the ADR 0075 amendment)

## 1. Engine mints a run id per delegation and stamps it on every event — ✅ DONE (2026-09-24)

NOTES (2026-09-24): `appendToolResult` and `runSubAgent` each gained a run-id parameter (`newChildAgentOn` gained one, as the plan says). Their existing test call sites were updated mechanically to pass `""`, or `parent.runIDs.mint()` in the live test: advise_argv_test.go, advise_test.go, toolresultfloor_test.go, toolresultmarker_test.go, live_delegate_cap_test.go, seat_test.go and subagent_test.go.
NOTES (2026-09-24): consequential edit — internal/agent/dispatch_test.go: made necessary by the random per-root run-id prefix. TestDispatchGroup_OnePipelineAtEveryWidth compares the per-call events of two separate root Agents (width 1 and width 2) with DeepEqual, so the test now injects one fixed prefix (`newRunIDMinter("0badc0de")`) into both. The assertion is unchanged: ids are minted in emitted-call order at either width, so those events still match byte for byte.
NOTES (2026-09-24): consequential edit — internal/domain/ask.go, internal/domain/present.go, internal/domain/seampayload.go, internal/domain/events_test.go: made necessary by the plan's comment grep. Their comments called a spawn call id the "run identity"; they now say "spawning call" and point to EventBase.RunID.
NOTES (2026-09-24): a call that a pre-tool-exec Reaction redirects INTO sub_agent gets its run id in runDelegation. Its head ToolCallEvent carries no SpawnRunID, but its phases, child events and ToolResultEvent do. A call redirected OUT of sub_agent keeps on its ToolResultEvent the SpawnRunID its head carried, so the result still closes the block that call opened. Both cases are documented on the domain fields.
NOTES (2026-09-24): the run id is stamped only on Events. The context carrier (WithSpawnCallID), PresentRequest.SpawnCallID and the Reaction seam payload's call_id still carry only the call id. Item 2 owns the JSON and session-record surfaces.

**What:** Fixes cause #1/#2 of apogee-subagent-premature-done at the source: call ids are not unique across a delegation tree.
**Regression guard.** This item explicitly supersedes ADR 0075 §7 ("EventBase is not extended") and §8 ("the call tree is rebuildable by correlating ToolCallEvent ids") for delegation identity. Call ids are the model's or server's and can collide, which is the defect this plan fixes, so the run id is engine-owned identity, not Driver-stamped. The owner ratified this on 2026-09-24 ("Engine run id"). Add ADR 0075 to the header Sources; item 8 writes the dated ADR 0075 amendment.
- Keep `newChildAgent`'s signature (it mints from the shared minter for its 57 test call sites); thread the run id through `newChildAgentOn` only. The Agent's `runID` and minter live in `internal/agent/agent.go`.
- Mint before the `ToolCallEvent` for a call named `sub_agent`, and in `runDelegation` mint for any delegating slot that still has none (a pre-tool-exec `ToolCallEdit.SetTool` can redirect a call into or out of `sub_agent`). A reader falls back to (depth, spawn call id) whenever a delegated `RunID` is "".
- `emitSubAgentNamed` and both its callers (`subagent.go`: the inherited name and the naming goroutine, which reads `sub`'s run id) stamp the child's run id.
- "Never share a run id" means distinct per delegation within one root Agent's tree; the prefix is drawn per root Agent.
- Every comment calling `EventBase.CallID` a run's identity or unique is updated: `grep -rn 'EventBase.CallID\|unique per spawning call\|run identity' internal/domain internal/agent`.
**Goal:** `domain.EventBase` has a `RunID string` field: empty at depth 0, and at depth > 0 the run id of the delegation the emitting agent runs. A delegation's head `ToolCallEvent` and its `ToolResultEvent` carry the child's run id in a new `SpawnRunID string` member, and so does every `SubAgentPhaseEvent`/`SubAgentNamedEvent` for that delegation, through its `EventBase.RunID`. Two delegations never share a run id within one engine or across engines, even when their call ids are equal.
**Approach (assumed at the header base):** Mint the id in `prepareCall` (`internal/agent/dispatch.go`) for `sub_agent` calls, from a minter shared by the whole agent tree: the root owns it and `delegation.seed` hands it down, as `consoles.MintOwner` is shared. Format: `<prefix>.<n>`, where `<prefix>` is 8 hex characters from `crypto/rand`, drawn once per root Agent (injectable for tests), and `<n>` is a 1-based counter. `(*Agent).base` stamps `a.runID`; `delegation.seed` sets it; `emitSubAgentPhase` stamps the child's run id. Keep `EventBase.CallID` exactly as today, and update its doc comment so it no longer claims the call id is unique.
**Files:** internal/domain/events.go, internal/agent/agent.go, internal/agent/dispatch.go, internal/agent/construct.go, internal/agent/loop.go, internal/agent/subagent.go, internal/agent/seat_test.go, internal/agent/subagent_test.go, internal/agent/fanout_test.go, internal/agent/delegationphase_test.go, and every file the comment grep above hits
**Read first:** internal/agent/dispatch.go — prepareCall, runDelegation, emitSubAgentNamed; internal/agent/construct.go — delegation.seed; internal/agent/loop.go — (*Agent).base;
internal/agent/subagent.go — newChildAgentOn, startDelegationNaming; internal/domain/events.go — EventBase
**Tests:** A fan-out of two `sub_agent` calls with the SAME call id gets two distinct `SpawnRunID`s. Each child's events carry its own `RunID`. A nested (depth 2) delegation's `RunID` differs from its parent's. A top-level agent's events have an empty `RunID`. A `SubAgentNamedEvent` carries the child's `RunID`. With an injected prefix, two delegations in one root tree get distinct ids.
**Acceptance:** `go build ./... && go test -race -count=1 ./internal/agent/ ./internal/domain/`
**Commit:** `fix(agent): mint a unique run id per delegation and stamp it on its events`

## 2. Run ids reach the JSON event stream and the session record — ✅ DONE (2026-09-24)

NOTES (2026-09-24): re-derived from the assumption that the event lines are at `v:1`: the tree is at `v:2` (lineVersion 2), so `run_id` and `spawn_run_id` are additive within `v:2` and the version is not bumped. The envelope is written in writer.go, as the item's own guard says, not encode.go. encode.go only gains the `spawn_run_id` data members.
NOTES (2026-09-24): `spawn_run_id` is always present under `data` on every tool_call/tool_result line: `""` when the call spawns no delegation. This follows the package's no-omitempty rule (ADR 0075 decision 3). It sits last in each data object (additive, decision 10).
NOTES (2026-09-24): on session entries, `runID` is the emitting agent's EventBase.RunID (every delegated entry). `spawnRunID` is set only on the toolCall/toolResult entries of a delegation (ToolCallEvent/ToolResultEvent.SpawnRunID).
NOTES (2026-09-24): added TestTranscriptFoldRecordsDelegationRunIDs to internal/run/transcript_test.go, which the plan's Files list does not name. It pins the goal's "internal/run's transcript builder fills them". The spawnOf comment in internal/run/transcript.go no longer calls the call id the run identity.
NOTES (2026-09-24): docs/manual/headless.md still shows the old envelope key set. Item 8 owns that file and the ADR 0075 amendment, so it is left untouched here.

**What:** Depends on item 1.
**Regression guard.** `run_id` is a new ENVELOPE member of every Event line: always present, null at depth 0 and on frames, per ADR 0075 §3's always-present rule. `spawn_run_id` goes under `data` on a delegation's tool_call/tool_result. Both are additive within `v:1` (ADR 0075 §10), and the version is not bumped. item 2 fills session.Entry run ids in internal/run only; the TUI's own session writer is owned by item 3.
- The envelope is written in `internal/eventjson/writer.go` (`envelope`, `writeLine`), not `encode.go`: `run_id` goes right after `call_id`, and is tested through the Writer in `writer_test.go` (`TestWriterEnvelopeOrderAndNulls`); `doc.go` documents it. It extends ADR 0075 §3's envelope key set, which item 8's ADR 0075 amendment records.
- Every line changes: regenerate the byte goldens in `writer_test.go` and `cmd/apogee/testdata/eventlines/{run,not-started,not-started-after-sink}.jsonl` (`TestE2EEventLinesGolden`).
**Goal:** `internal/eventjson` emits `run_id` on every delegated event and `spawn_run_id` on a delegation's call and result. `session.Entry` persists `RunID`/`SpawnRunID` (JSON `runID`/`spawnRunID`, omitempty) next to `SpawnCallID`. `internal/run`'s transcript builder fills them. A session file written before this change still loads, with both fields empty.
**Approach (assumed at the header base):** Mirror how `SpawnCallID` travels: `spawnOf` in `internal/run/transcript.go`, and the `Entry` fields in `internal/session/transcript.go`, which are not stripped because they are match keys. In `internal/eventjson/encode.go`, add the keys where `call_id` is written.
**Files:** internal/eventjson/writer.go, internal/eventjson/writer_test.go, internal/eventjson/doc.go, internal/eventjson/encode.go, internal/eventjson/encode_test.go, cmd/apogee/testdata/eventlines/run.jsonl, cmd/apogee/testdata/eventlines/not-started.jsonl, cmd/apogee/testdata/eventlines/not-started-after-sink.jsonl, internal/session/transcript.go, internal/run/transcript.go, internal/session/transcript_test.go
**Read first:** internal/eventjson/writer.go — envelope, writeLine; internal/eventjson/encode.go — toolCallData, toolResultData; internal/eventjson/writer_test.go — TestWriterEnvelopeOrderAndNulls;
cmd/apogee/e2e_eventlines_test.go — TestE2EEventLinesGolden; internal/session/transcript.go — Entry; internal/run/transcript.go — spawnOf
**Tests:** Through the Writer, every line carries `run_id` right after `call_id` (null at depth 0 and on frames), and a delegated `ToolCallEvent`/`ToolResultEvent` carries `spawn_run_id` under `data`. A session entry written with a run id survives save and load. A legacy entry without the fields loads with them empty.
**Acceptance:** `go test -race -count=1 ./internal/eventjson/ ./internal/session/ ./internal/run/` and `go test -race -count=1 -run 'TestE2EEventLines' ./cmd/apogee/`
**Commit:** `feat(session): persist and emit delegation run ids`

## 3. TUI keys runs by run id and pairs results by run — ✅ DONE (2026-09-24)

NOTES (2026-09-24): re-derived from the assumption that every run consumer picks up the run id through runOf alone — render.go (paintRoot/breadcrumb/preview runEnd), interject.go (runLabel on the refusal note and the queued-row label; queuedInterjection gains a `run` field beside `spawn`, which still addresses InterjectChild per the plan's out-of-scope note), thinkingpane.go (runLabel) and activity.go (the oldestChild tie-break, which now also breaks on run id so colliding siblings stay deterministic) called the spawn-keyed helpers directly and were moved to the run-aware signatures
NOTES (2026-09-24): helper signatures changed rather than duplicated — headsRunFor/runHead/runHeadAt/breadcrumbTrail/runEnd/runName/runLabel/openSubAgentHead/addUserAt take a runRef; addToolCall/addToolResult gain a spawnRunID argument. The legacy match (no run id on either side) also requires the head to stand at depth-1, per the Goal; the run id decides when both the event and the head carry one
NOTES (2026-09-24): addSubAgentPhase lets a head with no spawnRunID (a call a Reaction redirected INTO sub_agent, whose run id is first named by its started phase) adopt the phase's run id, so its entries and result then match by run id
NOTES (2026-09-24): test helpers added beside the existing ones rather than re-signing them (fanout_test.go: stampedDelegation, stampedBase, stampedPhase, stampedHeadIndex); the "second run finishes" test was widened to both orders, since only the first-run-finishes order fails on the base tree
NOTES (2026-09-24): consequential edit — internal/tui/transcriptbridge_test.go: made necessary by the bridge now mapping session.Entry RunID/SpawnRunID (the session.Entry member enumeration in TestTranscriptCodecPersistsANamedDelegationAsItsTarget; it had been failing since item 2 widened the struct); the new round-trip test lives beside the other codec tests there
NOTES (2026-09-24): consequential edit — internal/tui/inspector_test.go: made necessary by runLabel taking a runRef
NOTES (2026-09-24): consequential edit — internal/tui/sessionsave_test.go: made necessary by addUserAt taking a runRef
NOTES (2026-09-24): consequential edit — internal/tui/workspacepath_test.go: made necessary by addToolCall's new spawnRunID argument
NOTES (2026-09-24): consequential edit — internal/tui/messages.go: made necessary by the run id replacing presentedMsg.SpawnCallID as the run identity (comment now names it the legacy key)
NOTES (2026-09-24): consequential edit — internal/tui/sink.go: made necessary by the run id replacing the spawning call id as what keeps siblings' coalesced text apart (comment only)

**What:** Depends on items 1 and 2. This fixes cause #1: a child's leaf result closed a sibling head with an equal call id and showed ✓ early. It also fixes cause #2: with duplicate `sub_agent` ids in one reply, the first child's finish ticked the other row.
**Regression guard.** The premise that every runRef consumer goes through runOf is false. Every site in internal/tui that builds a `runRef{…}` literal directly (find them with `grep -n "runRef{" internal/tui/*.go`, e.g. the run-view open in runview.go, closeRun's commitResidue in transcript.go, and inspector.go's record comparisons) must carry the run id too. At least one tui test must stamp RunID end to end through the run view and the inspector, not only through hand-built legacy events. item 3 owns transcriptbridge writing and reading session.Entry RunID/SpawnRunID.
- Changing `headsRunFor`/`runHeadAt`/`runName`/`breadcrumbTrail`/`runHead` signatures breaks `fold_test.go`, `runview_test.go` and `subagentblock_test.go`: update them, or keep the spawn-only helpers and add run-aware siblings.
- `addUserAt` takes a `runRef` (`runOf(e.EventBase)`) so a child interjection entry carries its run id. `addPresented` (`domain.PresentRequest` carries no run id) is a named legacy fallback on (depth, spawn).
- Every internal/tui comment calling the (depth, spawn) pair the run identity is updated: `grep -n '{depth, spawn}\|(depth, spawn\|(depth, callID)' internal/tui/*.go`.
**Goal:**
- A transcript `entry` records `runID` (the run it belongs to) and, on a delegation head, `spawnRunID`.
- `runRef` includes the run id.
- Every head lookup (`headsRunFor`, `runHeadAt`, `openSubAgentHead`, `runName`, `addSubAgentName`, `addSubAgentPhase`, `place`/`runEnd`) matches a head by `spawnRunID` when the event carries a run id.
- `addToolResult` pairs only with an open call in the same run (and, for a delegation result, the same `spawnRunID`). A result with no match falls to the orphan branch.
- Events with no run id (a restored legacy session, a hand-built test) match on (depth, spawn call id) plus the head's depth being `depth-1`.
**Approach (assumed at the header base):** `runOf(base)` builds the key. `transcriptbridge.go` maps `session.Entry.RunID`/`SpawnRunID` in both directions. Consumers keyed by `runRef` (`activity.go` `runActivities`, `thinking.go`, `advicepane.go`, `inspector.go`, `runview.go`) pick up the new key through `runOf`. Update test helpers (`fanout_test.go` `headIndex`, `transcript_test.go` `subAgentCall`, `render_test.go` `delegationCall`) so they can stamp run ids.
**Files:** internal/tui/transcript.go, internal/tui/subagentblock.go, internal/tui/runview.go, internal/tui/inspector.go, internal/tui/transcriptbridge.go, internal/tui/thinking.go, internal/tui/advicepane.go, internal/tui/model.go, internal/tui/doc.go, internal/tui/fanout_test.go, internal/tui/transcript_test.go, internal/tui/render_test.go, internal/tui/fold_test.go, internal/tui/runview_test.go, internal/tui/subagentblock_test.go
**Read first:** internal/tui/transcript.go — runRef, runOf, addToolResult, closeRun; internal/tui/runview.go — openRunAt; internal/tui/inspector.go — scopedWire;
internal/tui/thinkingpane.go — inThinkingScope; internal/tui/transcriptbridge.go — fromWireEntry
**Tests:**
- Heads c0 and c1 in one fan-out, then a child-of-c0 `ToolCallEvent` + `ToolResultEvent` whose `Call.ID` is `c1`: head c1 is not done and `subAgentReported` is false (fails on the base tree).
- Two heads sharing id c0 with distinct run ids: `SubAgentFinished` for the second run ticks only the second row.
- A legacy (no run id) restored transcript still pairs and renders as today.
- A run view opened on RunID-stamped events scopes `/thinking`, `/inspect` and the status line to that run.
- A landed child interjection whose spawn id collides with a sibling's lands under the head its `RunID` names.
- A session entry's `RunID`/`SpawnRunID` survive the transcriptbridge round trip.
**Acceptance:** `go test -race -count=1 ./internal/tui/`
**Commit:** `fix(tui): pair sub-agent results and phases by run id, not call id`

## 4. Driven e2e: a fan-out with colliding child call ids never ticks early — ✅ DONE (2026-09-24)

NOTES (2026-09-24): the pane shows the done verdict as the bare word `done` in the outcome slot after the leader (`⋯ done ▶`), never the literal `· done` the Goal names; the test's `delegationRowDone` asserts the ✓ and the leader-adjacent `done` slot word instead.
NOTES (2026-09-24): `fanOutScript` now takes the parent and child fixture names, and a `twoDelegationsScript` helper keeps the three existing server-switch callers on their original fixtures; `launchParallelSession` takes the script as its second parameter.
NOTES (2026-09-24): verified the new test fails at a4e3eb07 (items 1–3 reverted) — beta's row reads `beta ✓ … done` while its report is still held — and passes at HEAD.

**What:** Depends on item 3. This is the journey test for cause #1, using the stub's own per-reply `call_<n>` numbering.
**Regression guard.** Name the test with the `TestE2EParallel` prefix (e.g. `TestE2EParallelCollidingChildCallIDsTickNoRowEarly`) so Acceptance runs it. `launchParallelSession` hard-wires `fanOutScript`, so give it a script parameter.
Hold the no-✓ window short (`serialWatch`-sized) and Release B's gate well inside stubllm's 10s `awaitLimit`, or B faults on a slow `-race` box.
**Goal:** A `cmd/apogee` e2e test drives a width-2 fan-out. Child A answers with two tool calls in one reply, so its second call's id equals head B's. The test holds child B open with an `await:` gate and asserts that B's row shows no ✓ and no `· done` until B's scripted report is released. After release, both rows show ✓.
**Approach (assumed at the header base):** Model it on `TestE2EParallelDelegationsFollowAServerSwitch` (`cmd/apogee/e2e_parallel_test.go`) and the `parallel-two-delegations.yaml`/`parallel-child.yaml` fixtures, with the `parallel-agents: 2` pin. Gate child B per `docs/design/test-drivers.md` (`await:` / `Release`).
**Files:** cmd/apogee/e2e_parallel_test.go, cmd/apogee/testdata/stubllm/parallel-colliding-ids.yaml, cmd/apogee/testdata/stubllm/parallel-colliding-child.yaml
**Read first:** cmd/apogee/e2e_parallel_test.go — TestE2EParallelDelegationsFollowAServerSwitch, launchParallelSession, fanOutScript, awaitBothDelegatesRunning;
cmd/apogee/testdata/stubllm/parallel-two-delegations.yaml; internal/stubllm/script.go — Turn.Await; internal/stubllm/server.go — Server.Release, awaitLimit
**Tests:** the new e2e test, `TestE2EParallelCollidingChildCallIDsTickNoRowEarly`. It must fail with items 1–3 reverted.
**Acceptance:** `go test -race -count=1 -run TestE2EParallel ./cmd/apogee/`
**Commit:** `test(cmd/apogee): an e2e fan-out with colliding child call ids ticks no row early`

## 5. Move the delegate report-shape classifier into a shared package

**What:** A refactor so the TUI can read a completed report's shape (cause #3). There is no behaviour change.
**Regression guard.** The target is a package `internal/agent` imports and that is already in `go list -deps ./internal/tui`: default `internal/floor`, beside `HasToolCallMarkup` (`domain` cannot host it — `floor` imports `domain`).
Move the whole block from `type closingShape` through `endsOnIntent` (`nonBlankLines`, `hasReadFileHeader`, `isGrepMajority`, `readFileHeaderLine`, `grepHitLine`, `nonReportHeadLines`, `isNonReport` included); export the type, its five constants and `IsNonReport`.
Keep `internal/agent/testdata/closingshape` (`TestSubAgent_StepCapNamesANarratingChildsNonReport` reads it) and copy it under the target's testdata; inline `childClosingReport`'s literal in the moved table.
**Goal:** The classifier behind `closingShapeOf`/`endsOnIntent` (report, tool-call markup, file dump, grep dump, narration) is exported from a package that both `internal/agent` and `internal/tui` already import. `internal/agent` calls it there, and the capped path's output is byte-identical to today.
**Approach (assumed at the header base):** Move `closingShapeOf`, `endsOnIntent`, `intentAtSentenceStart`, `receiptLine` and the shape enum out of `internal/agent/subagent.go`. Choose a target package that adds no new cross-layer import (check `go list -deps`). The existing agent tests (`TestClosingShapeOf_ReadsTheFourNonReportShapes`, `TestSubAgent_StepCapNamesANarratingChildsNonReport`) move with the code or keep calling it.
**Files:** internal/agent/subagent.go, internal/agent/subagent_test.go, internal/agent/testdata/closingshape/ (kept), and the target package's new file plus its test (default internal/floor) and a copy of the fixtures under its testdata/closingshape/
**Read first:** internal/agent/subagent.go — closingShapeOf, closingShape, isNonReport, capResultHead; internal/floor/salvage.go — HasToolCallMarkup;
internal/agent/subagent_test.go — TestClosingShapeOf_ReadsTheFourNonReportShapes, closingShapeFixture, TestCapResultHead_NonReportVariantsKeepTheBoundPrefix
**Tests:** the moved classifier tests, unchanged in their assertions. `TestCapResultHead_NonReportVariantsKeepTheBoundPrefix` follows the rename to the exported names.
**Acceptance:** `go build ./... && go test -race -count=1 ./internal/agent/` plus the target package's tests
**Commit:** `refactor: share the delegate report-shape classifier with the TUI`

## 6. A delegation that ended without a report reads so, with no ✓

**What:** Depends on item 5. This fixes cause #3: a completed child whose final text is narration, markup-free non-report, or `[delegate returned no report]` reads `done` ✓.
**Regression guard.** The no-report verdict is an outcome envelope, like the bound head. It takes the row's slot ahead of a ONE-LINE report's first line, which today rides tv.stat (toolview.go), and ahead of a multi-line report's `· done`. The ✓ is withheld in both shapes. Tests cover a one-line and a multi-line non-report.
- `subAgentFinished` reads no-report from the verdict WORD wherever it rides (`head.tool.Summary.Text` when typed, else `head.tool.stat`), through one whole-phrase predicate beside `succeededSummary` that also matches the `· steered by` suffix.
- The reading is recovered from text on both the live and the replay path (`fromWireToolView` rebuilds Summary and stat from text); no new wire field.
**Goal:** For a non-error delegation result that carries no bound head (`delegationBoundHead`), the verdict is `ended without a report` when either the text is the no-report marker or the shared classifier reads it as not a report. That verdict is painted in the step-cap tone. `subAgentFinished` is false for it: no ✓, not red. Every other result keeps its current verdict. The parent model's result text is unchanged.
**Approach (assumed at the header base):** In `delegationVerdict` (`internal/tui/toolregistry.go`), add a `delegationNoReportVerdict` constant next to `delegationDoneVerdict`, and carry a no-report flag on the head's summary that `subAgentFinished` (`subagentblock.go`) reads. `toolregistry_test.go`'s "the no-report marker reads done" case is updated to the new verdict.
**Files:** internal/tui/toolregistry.go, internal/tui/subagentblock.go, internal/tui/toolview.go, internal/tui/toolleader.go, internal/tui/toolregistry_test.go, internal/tui/subagentblock_test.go, internal/tui/transcriptbridge_test.go
**Read first:** internal/tui/subagentblock.go — subAgentFinished, subAgentReported; internal/tui/toolregistry.go — delegationVerdict, delegationBoundHead, outputDetail;
internal/tui/toolview.go — applyStat; internal/tui/toolleader.go — succeededSummary; internal/tui/transcriptbridge.go — fromWireToolView
**Tests:**
- Narration ("Let me now read X.", one line) → `ended without a report`, no ✓.
- The no-report marker → the same.
- A multi-line non-report → `ended without a report` in place of `· done`, no ✓.
- A transcriptbridge round trip of a no-report row keeps it without ✓.
- A real report → `done` ✓.
- A capped result → still `stopped at its step cap`.
**Acceptance:** `go test -race -count=1 ./internal/tui/`
**Commit:** `fix(tui): a delegation that ended without a report no longer reads done`

## 7. The undelivered-interjection note names why the message did not land

**What:** This fixes cause #4: `<name> finished before your message landed` is shown for capped, faulted and cancelled runs, and for a refusal while the child is still running.
**Regression guard.** `runSubAgent`'s reaping defer runs BEFORE the outer defer's `classifyDelegation`, so no ledger outcome exists there: call `classifyDelegation` in the reaping defer with a panicking flag (set before `sub.Run`, cleared after) mapped to faulted, or stash `mailbox.close()` and report in the outer defer after classification.
`encode_test.go`'s two child_interjection cases gain `reason` ("" when landed). The zero/unknown Reason renders today's `finished` wording (open enum, ADR 0075 §10); `childMessage` gains a reason argument.
`apogee.go` re-exports the reason type and its constants, following the `SubAgentPhase` precedent.
**Goal:** `domain.ChildInterjectionEvent` carries a `Reason` enum, used only when `Landed` is false: completed, capped, faulted, cancelled, refused. `eventjson` emits it as `reason`. The TUI note reads, per reason:
- `<name> finished before your message landed`
- `<name> stopped at its cap before your message landed`
- `<name> failed before your message landed`
- `<name> was cancelled before your message landed`
- `<name> could not take your message`
**Approach (assumed at the header base):** `runSubAgent`'s deferred `reportUndelivered` sets the reason from the run's ledger outcome. `drainMailbox`'s refusal path sets `refused`. `addChildInterjection` (`internal/tui/transcript.go`) words it.
**Files:** internal/domain/events.go, apogee.go, internal/agent/children.go, internal/agent/subagent.go, internal/agent/children_test.go, internal/eventjson/encode.go, internal/eventjson/encode_test.go, internal/tui/transcript.go, internal/tui/transcript_test.go
**Read first:** internal/agent/subagent.go — runSubAgent (both defers), delegationResult; internal/agent/children.go — drainMailbox, reportUndelivered; internal/tui/transcript.go — addChildInterjection;
internal/eventjson/encode.go — childInterjectionData; internal/tui/transcript_test.go — childMessage; apogee.go — SubAgentPhase aliases
**Tests:** `TestInterjectChild_QueuedAfterTheLastStepIsReportedUndelivered` asserts the reason. `TestChildInterjectionLandsInsideItsRun` asserts each wording, and the zero Reason's `finished` wording. An eventjson case carries `reason`. A child that panics with a message queued reports faulted.
**Acceptance:** `go test -race -count=1 ./internal/agent/ ./internal/eventjson/ ./internal/tui/`
**Commit:** `fix(tui): say why a steering message did not reach its sub-agent`

## 8. Docs: run identity, the no-report verdict and the undelivered reasons

**What:** Depends on items 1–7. This is the one owner of every doc amendment this plan makes.
**Regression guard.** Also owns docs/manual/headless.md (the `run_id` envelope member, `spawn_run_id` and ChildInterjection `reason` keys) and a dated amendment to docs/adr/0075-the-headless-event-stream-is-a-versioned-driver-protocol.md §7/§8, recording the run id supersession. Add both paths to Files and extend the grep to docs/manual/headless.md.
- The ADR 0075 amendment also records `run_id` joining §3's envelope key set (item 2). The grep gains `call_id\|call id` (`docs/manual/reactions.md`'s `call_id` row calls it the run identity).
- Addressing sites (`InterjectChild`/`childRegistry`, e.g. CONTEXT.md's "addressable … by the spawning call-ID", ADR 0063 D1) keep the spawn call id. ADR bodies other than 0039's and 0075's dated amendments stay untouched.
**Goal:** Every doc that says a run is identified by its spawning call id, or that the call id is unique, says instead that the run id identifies the run and that the call id may repeat. Every doc listing a delegation row's verdict words lists `ended without a report`. Every doc quoting the undelivered note lists the five reasons.
- ADR 0039 gets a dated amendment under §5 recording the change.
Find the sites with:
```
grep -rn "spawning call.ID\|spawning call id\|unique per spawning\|call_id\|call id\|finished before your message\|· done\|stopped at its step cap" CONTEXT.md layout.md docs/layout docs/manual docs/adr
```
**Approach (assumed at the header base):** Known sites:
- `CONTEXT.md`: **Event**, **Sub-agent**, **Session record**, **Interjection**.
- `layout.md`: "## Collapsed and expanded blocks", "## Run view".
- `docs/layout/tool-layout.md`: "## Grouped Sub-agents".
- `docs/manual/reactions.md`: `call_id`.

The grep is the scope, not this list.
**Files:** CONTEXT.md, layout.md, docs/layout/tool-layout.md, docs/manual/reactions.md, docs/manual/headless.md, docs/adr/0039-*.md, docs/adr/0075-the-headless-event-stream-is-a-versioned-driver-protocol.md
**Read first:** CONTEXT.md — Event, Sub-agent (addressable by spawning call-ID), Interjection; layout.md — "## Run view"; docs/manual/reactions.md — call_id field table; docs/manual/headless.md — envelope member table;
docs/adr/0039-delegations-fan-out-concurrently-bounded-by-the-servers-parallel-agents-cap.md — decision 5; docs/adr/0063-sub-agent-runs-are-user-addressable-views.md — D1 child registry
**Tests:** none (docs).
**Acceptance:** the grep above returns no line that still claims call-id uniqueness, and none with the single `finished` wording as the only reason.
**Commit:** `docs: run ids identify delegations; the no-report verdict; undelivered reasons`
