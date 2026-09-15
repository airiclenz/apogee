# Engine-shape deepening (2026-09-14 architecture review, plan A) — plan

**Goal:** land the engine-side candidates of `docs/reviews/architecture-review-2026-09-14.html`
that survived fact-checking, each reduced to what the code and the ADRs support: Events carry
the tool name and write target (2r); one Reaction payload document (4r); one per-call dispatch
pipeline with one recover boundary (3r); a delegate constructed from a `delegation` value behind
one dialer seam (6); a `reactions:` entry validated once (14); the Exchange lifecycle finished
inside `turnLifecycle` (20). Sibling plan B (`2026-09-15 - 01`) holds the host/config/tools half.

**Date:** 2026-09-15
**Status:** unexecuted
**sized for:** ~200k-context host

**Regression check (2026-09-15, b2e3d75d):** four independent read-only reviewers (`regression-1..4.md` under the run dir); every item's `**Regression guard.**` / `**Read first:**` line below is theirs, folded verbatim.
- 2: guard folded — `writeTargetClass` carries the resolved path; the Event-lines golden and the manual's `tool_result` line join Acceptance.
- 3: recast — `narrationSink` keeps its per-call map for the delegation name; file-changed is `WriteTarget != "" && !Result.IsError`; the target moves to the post-edit call (admitted).
- 4: guard folded — every `reactions.Payload` reader listed; `ScheduleRef` moves to domain; the six-entry `Env()`; the member-order rule.
- 5: recast — the one recover lives in `runSubAgent`'s frame; yields to `dispatch.go`'s placement comment and ADR 0039 D4 (both stay satisfied).
- 6: recast — width 1 is the per-call loop; per-kind audit tail kept; parity tests re-pointed; yields to ADR 0039 D1/D3 (preserved, not reversed).
- 7: guard folded — `hookExecutionCtx` is deleted whole with `fireCascade`'s branch, `syncPermitCtx` untouched; supersedes confinement-execution-contract §10.3/§10.4, ADR 0076's 2026-09-12 note and `domain/confinement.go`'s two comments by dated amendment.
- 8: guard folded — `newAgent(cfg, up)` keeps its signature; the `delegation` value is the constructor input, runtime fields stay; the no-read test needs a loader seam or the directory-swap assertion.
- 9: guard folded — `isDelegate()` is `a.depth > 0`; `stepCap` is a bound, not child-ness; test- and comment-blind grep; `delegatereport.go` named.
- 10: guard folded — `New` and `Resume` both take the option; comment-blind acceptance grep; the wire tests keep httptest.
- 11: recast — only the argv[0]/URL/headers-env rows move; the timeout rule stays lane-side; yields to CONTEXT.md's Timeout entry and `config/reactions.go`'s 0s note.
- 12: guard folded — `SetReactions` returns an error and every caller is listed; the advise fixture is re-routed; malformed-entry rows re-pinned; supersedes `runner.go`/`runner_test.go`'s "the Runner validates what it is handed" (rewritten).
- 13: guard folded — every field reader/writer and every test file by grep rule; `construct.go` named.
- 14: recast — `growthBounds` is a pure derivation read live, never cached at Turn open; code-only acceptance grep.
- 15: guard folded — `construct.go` and the callback grep rule; a nil observer stays inert.
- 16: guard folded — every `Interject(` implementer/caller by grep; the test is recast to the existing fate unless the ctx error is stated as a contract change; supersedes `interject.go`'s `context.Background` comment (rewritten).
- 1: SAFE.
Second round (2026-09-15, b2e3d75d; `regression-5.md`, one reviewer over the amended draft) — every other item SAFE:
- 3: guard folded — the Acceptance grep excludes `classifyWriteTarget`/`writeTargetClass`/`ResolvedWriteTarget`/`workspaceWriteTarget` (untouched dispatch.go symbols); the rule is the second clause only.
- 5: guard folded — `runSubAgent(ctx, call)` keeps its signature (five direct test callers), the recover reads `a.turns.index` and is registered before the reaping defer; `dispatch.go`'s placement paragraph is rewritten to point at `runSubAgent`'s frame.
- 6: guard folded — a leaf's audit tail stays inside `run` (per-outcome, not per-kind); the width-1 interjection check sits where `dispatchSerially` has it; `run` wraps item 5's `runSubAgent` boundary and the recover pair is the Acceptance grep (writer's decision).
- 11: guard folded — the argv[0] widening onto the sync lane is admitted in What; the moved sentence is key-neutral; an `advise: []` row joins the config test.
- 14: guard folded — `room`/`transcriptBudget` derive off the advertised window, `historyFloor` off `b.History` (working-window row added); retitled "derived at one site", `turn.go` dropped from Files.

## Authoritative sources

- `docs/reviews/architecture-review-2026-09-14.html` — the review. Fact-checked 2026-09-15 by six
  read-only explorers; **where an item disagrees with the review, the item wins** (verdicts below).
- ADR 0001 (Events grow additively), 0010 (module order — `internal/domain` imports no sibling),
  0039 D4/D5, 0075 D10, 0076 D1/D2/D4/D7/D8/A4, 0017 §2, 0018 — decision text binds every item.
- `internal/eventjson/writer.go` (`lineVersion = 2`) and `cmd/apogee/testdata/eventlines/run.jsonl`.
- `CONTEXT.md` §Reaction, §Moment, §Exchange, §Delegation.

## Review verdicts (fact-check, 2026-09-15; HEAD b2e3d75d)

| # | Candidate | Verdict |
|---|---|---|
| 2 | Events carry engine facts | **IN, reduced** — `ToolResultEvent` half only (3 maps, not 5; eventjson is v2). SubAgentPhase half → bead `apogee-clb` |
| 3 | One per-call pipeline | **IN, reduced** — recover unification + shared phases; `resolve()` untouched (a gate fold would spawn gate scripts before the ladder verdict) |
| 4 | One Reaction invoke | **IN, reduced** — payload merge only; ADR 0076 D4/A4 ratify the Runner lane, D8 mandates the two postures |
| 5 | Live posture one value | **DENIED** — contradicts ADR 0037 D2 consequence text; swap-whole invites lost updates |
| 6 | Delegate constructed | **IN** — 17 post-hoc writes, no dialer seam, 7 test files stand up httptest |
| 14 | Validate once | **IN** — 3× at boot, +1 per reload |
| 19 | Step loop with mailbox | **DENIED** — ADR 0025 re-affirmed the rejection 2026-09-14 |
| 20 | Exchange lifecycle | **IN** |

## Ratified design calls (owner, 2026-09-15)

- **Scope:** the table above; denied rows are recorded here only.
- **Gate:** every collision item waits for plan `2026-09-14 - 04` to archive; items cite symbols, never lines.
- **Dialer seam:** lives in `internal/agent` as a constructor option (`domain` cannot import `provider`, ADR 0010); default `provider.NewClient`.
- **ADR text:** dated in-place amendments, never superseding ADRs.

## Standing requirements

- `skills: coding-standards`.
- Deviations land as a dated `NOTES:` line under the item.
- No item changes `VERSION`, a CHANGELOG release heading or a tag.

## Out of scope

- Candidates 5, 19 (denied); the gate-answer fold into `resolve()`; `SubAgentPhaseEvent` fields; any change to `reactions.Runner`'s sink seat, matcher or queue; a Posture value.
- Plan B's items (facade forwarders, `Options.StartupEntry`, `raise`, guard table, tools).

---

## 1. Gate: plan 2026-09-14 - 04 is archived — ✅ DONE (2026-09-15)

NOTES (2026-09-15): gate PASSED — `docs/plans/archived/` holds all three `2026-09-14 - 02/03/04` plans (grep -c = 3) and `docs/plans/` holds none (grep -c = 0); `2026-09-14 - 04 - queued-commands-delegation-width-and-accounting-plan.md` present in archived/. No code change; the plan says `Commit: none`.

**What.** Verify `docs/plans/archived/2026-09-14 - 04 - queued-commands-delegation-width-and-accounting-plan.md` exists and `docs/plans/` holds no `2026-09-14 - 02/03/04` file. If not, STOP the run here (resume later). No code change.

**Files:** none.
**Read first:** docs/plans/archived/ — the 2026-09-14 - 00/01 archives (naming kept verbatim on archive); docs/plans/ — 2026-09-14 - 02/03/04 still live at BASE

**Tests.** none.

**Acceptance.** `ls "docs/plans/archived/" | grep -c "2026-09-14 - 0[234]"` prints `3`; `ls docs/plans/ | grep -c "2026-09-14 - 0[234]"` prints `0`.

Commit: none (gate item).

## 2. `ToolResultEvent` carries the tool name and the classified write target — ✅ DONE (2026-09-15)

NOTES (2026-09-15): the regression guard's `path string` on `writeTargetClass` already exists as its `real` member (the abs `WorkspaceWriteTarget`), so no member was added there; the path rides `resolutionInput.writeTarget` (a field `resolve()` does not read) and `resolveAndExecute` returns it as a middle value, which `appendToolResult` — now taking the post-edit call and the write target — stamps onto the event.
NOTES (2026-09-15): consequential edit — internal/agent/gate_test.go: made necessary by the `resolveAndExecute` third return (callers take `_`).
NOTES (2026-09-15): consequential edit — internal/agent/setreactions_test.go: made necessary by the `resolveAndExecute` third return (callers take `_`).
NOTES (2026-09-15): consequential edit — internal/agent/advise_test.go: made necessary by the `appendToolResult` signature (call + write target parameters).
NOTES (2026-09-15): consequential edit — internal/agent/advise_argv_test.go: made necessary by the `appendToolResult` signature (call + write target parameters).
NOTES (2026-09-15): consequential edit — internal/agent/toolresultfloor_test.go: made necessary by the `appendToolResult` signature (call + write target parameters).
NOTES (2026-09-15): consequential edit — internal/agent/toolresultmarker_test.go: made necessary by the `appendToolResult` signature (call + write target parameters).
NOTES (2026-09-15): `cmd/apogee/e2e_eventlines_test.go` (named in Files) needed no edit — the golden regenerated under the existing `-update` flag; a pooled delegation slot stamps `WriteTarget ""` without resolving, since `sub_agent` is not a workspace-scoped writer and the classification would be `""` regardless.

**What.** Add two additive members to `domain.ToolResultEvent` (`internal/domain/events.go`): `Tool string` (the resolved tool name) and `WriteTarget string` (the workspace path the call wrote, `""` when the call is not a write — the same answer `tools.WorkspaceWriteTarget` gives today). Stamp both at the single emit site in `internal/agent/dispatch.go` (the result emit after `executeTool`; the write target is the one `classifyWriteTarget` already resolved for the ladder — thread its resolved path, never re-resolve). Encode both in `internal/eventjson/encode.go` (`tool_result` line gains `tool` and `write_target`; version stays 2, additive per ADR 0075 D10); regenerate `TestEncodeJSONGolden` and `cmd/apogee/testdata/eventlines/run.jsonl`. Producers: the dispatch emit. Consumers touched here: none yet (item 3 moves them). Depends on item 1.

**Regression guard.** `writeTargetClass` keeps no path for an in-root write (`escape` is `""` inside the fence and the verdict carries only `writeEscapeTarget`), so "thread the path classifyWriteTarget already resolved" needs a `path string` on `writeTargetClass` (the abs `WorkspaceWriteTarget` returned), carried from `resolutionInput`/`resolveAndExecute` to `appendToolResult` (`internal/agent/resolution.go` joins Files if it rides the verdict); every non-resolving route (pre-tool-exec fault, unknown tool, colliding/repeated keys, `skipDelegation`, the `hookFailed` slot) stamps `Tool = call.Tool` (post-edit) and `WriteTarget ""`. `cmd/apogee/testdata/eventlines/run.jsonl` is owned by `TestE2EEventLinesGolden` (`cmd/apogee/e2e_eventlines_test.go`, regenerate with `-update`), so Acceptance runs it; the `tool_result` line in `docs/manual/headless.md` gains `tool`/`write_target` beside `resolved_path`.

**Files:** `internal/domain/events.go`, `internal/agent/dispatch.go`, `internal/agent/resolution.go` (if the path rides the verdict), `internal/agent/dispatch_test.go`, `internal/eventjson/encode.go`, `internal/eventjson/encode_test.go`, `cmd/apogee/testdata/eventlines/run.jsonl`, `cmd/apogee/e2e_eventlines_test.go`, `docs/manual/headless.md` (the Event-line field list).
**Read first:** internal/agent/dispatch.go — appendToolResult, resolveAndExecute, resolutionInput, classifyWriteTarget, writeTargetClass; internal/domain/events.go — ToolResultEvent; internal/eventjson/encode.go — toolResultData; cmd/apogee/e2e_eventlines_test.go — TestE2EEventLinesGolden

**Tests.** `TestEncodeJSONGolden` updated; `TestE2EEventLinesGolden` regenerated (`-update`); a new `internal/agent` test asserting a `write_file` call's `ToolResultEvent` carries `Tool == "write_file"` and the written path, and a `read_file` call carries `WriteTarget == ""`.

**Acceptance.** `go build ./... && go test ./internal/domain/... ./internal/eventjson/... ./internal/agent/... -run 'Encode|ToolResult' && go test ./cmd/apogee/ -run TestE2EEventLinesGolden`.

Commit: `feat(events): tool_result carries the tool name and the classified write target`

## 3. The Runner and every Driver read the write target off the Event — ✅ DONE (2026-09-15)

NOTES (2026-09-15): `TestNarrationSinkWordsTheResult` gained one row beyond the plan's "updated to the Event field" — a result whose call never went by but whose Event names a tool (`call_10`, `read_file`) is now worded `← read_file ok`, pinning that the tool is read off the Event and no longer remembered from the call; the `call_9` id-only row stays for an Event with `Tool == ""`.
NOTES (2026-09-15): consequential edit — cmd/apogee/wire_settings_test.go: made necessary by the closure's deletion — `TestRootWiringEmitsThroughTheHookRunner`'s comment "the registry the closure has not got" rewritten (the test itself is unchanged), beside the planned deletion of `TestRootHookWriteTargetIsRaceSafeAcrossARosterSwap`.
NOTES (2026-09-15): the `tools` import was dropped from `cmd/apogee/wire_boot.go` and `cmd/apogee/wire_firing.go` (both closures were its only use there); `fmt` dropped from `internal/reactions/match_test.go` with `TestMatchPendingWritesAreBounded`.
NOTES (2026-09-15): four dirty paths in the tree are not this item's and were left untouched — `cmd/apogee/e2e_subagent_view_test.go`, `cmd/apogee/testdata/frames/t17-run-view.txt`, `internal/doctext/pdf_test.go`, `scripts/test-shards.sh` (modified 19:01–19:03 by another agent, before this item's first edit).

**What.** Recast at the regression check (2026-09-15). Delete `reactions.WriteTarget`, `Options.WriteTarget` and the matcher's bounded pending map (`internal/reactions/match.go` — `newMatcher` takes no write-target func; `file-changed` matches on `ToolResultEvent.WriteTarget != "" && !Result.IsError`). Delete the TUI closure in `cmd/apogee/wire_boot.go` and `firingWriteTarget` in `cmd/apogee/wire_firing.go` (the per-Firing throw-away registry goes with it). `headless.go`'s `narrationSink` names the tool from `ToolResultEvent.Tool`; its per-call map stays for the delegation display name and loses only its tool member (see the guard). Behaviour change accepted: `file-changed` no longer silently drops past 256 pending writes. Admitted change: the file-changed target moves from the pre-edit `ToolCallEvent` call (today `match.go` remembers it before the `pre-tool-exec` Moment reshapes the call) to the post-edit resolved one the Event now carries — an improvement. Depends on item 2.

**Regression guard.** headless narrationSink KEEPS its per-call map for the delegation display name (narratedCall.name, remember, subAgentName, the SubAgentNamedEvent rename) and drops only its tool member; resultLine reads ToolResultEvent.Tool and falls back to "← <id>" only when Tool is "". file-changed rule is `WriteTarget != "" && !Result.IsError` (an erroring write fires nothing, as today); TestMatchFileChanged's four rows stay, driven over the Event fields; every newMatcher call in match_test.go changes. Add internal/reactions/runner_test.go to Files and recast TestRunnerWithNoHooksTouchesNothing to assert no firing until Replace arms file-changed, then one firing off a ToolResultEvent{Tool, WriteTarget}. TestHeadlessDerivesTheFileChangedHookFromItsOwnRoster: the stub emits ToolResultEvent{Tool:"write_file", WriteTarget: filepath.Join(ws,"a.txt")} (the "Driver derives" premise is gone). Delete liveTools.lookup with the closure. Prose rule + grep: every comment naming reactions.WriteTarget or the injected closure — `grep -rn "WriteTarget" internal/reactions cmd/apogee internal/agent --include=*.go | grep -v Workspace` (internal/agent/reactions.go's block included), and that grep is the Acceptance. Admitted change: the file-changed target moves from the pre-edit ToolCallEvent call to the post-edit resolved one (an improvement — state it in What). Item 4's Files add internal/reactions/match.go and match_test.go; items 3 then 4 execute in order. Second round: `grep -v Workspace` is case-sensitive and leaves dispatch.go's `classifyWriteTarget`/`writeTargetClass`/`tools.ResolvedWriteTarget`, reactions.go's `classifyWriteTarget` comment and dispatch_test.go's `workspaceWriteTarget` — none of which this item touches — so the Acceptance filter is `grep -v 'Workspace\|workspaceWriteTarget\|classifyWriteTarget\|writeTargetClass\|ResolvedWriteTarget'`, and the rule is the second clause only: no symbol or comment naming reactions.WriteTarget, Options.WriteTarget or the injected closure.

**Files:** `internal/reactions/match.go`, `internal/reactions/match_test.go`, `internal/reactions/runner.go`, `internal/reactions/runner_test.go`, `internal/reactions/doc.go` (the injected-WriteTarget comment), `internal/agent/reactions.go` (the comment block naming `WriteTarget`), `cmd/apogee/wire_boot.go`, `cmd/apogee/wire_firing.go`, `cmd/apogee/wire_firing_test.go`, `cmd/apogee/wire_settings_test.go`, `cmd/apogee/wire_tools.go` (`liveTools.lookup` and its comment), `cmd/apogee/headless.go`, `cmd/apogee/headless_test.go`.
**Read first:** internal/reactions/match.go — matcher, newMatcher, rememberWrite, matchToolResult; internal/reactions/runner.go — Options.WriteTarget, Replace; cmd/apogee/headless.go — narrationSink, resultLine; cmd/apogee/wire_firing.go — firingWriteTarget

**Tests.** Delete `TestMatchPendingWritesAreBounded`, `TestMatchNeverAsksWriteTargetWhenFileChangedIsUnsubscribed`, `TestMatchFileChangedIgnoresAnUnknownResult`, `TestFiringWriteTargetNamesAWriteAndNothingElse`, `TestRootHookWriteTargetIsRaceSafeAcrossARosterSwap`; replace with one `TestMatchFileChangedReadsTheEventsWriteTarget`; `TestMatchFileChanged`'s four rows stay, driven over the Event fields; `TestRunnerWithNoHooksTouchesNothing` recast per the guard; `TestHeadlessDerivesTheFileChangedHookFromItsOwnRoster`'s stub emits the stamped `ToolResultEvent`; `TestNarrationSinkWordsTheResult` updated to the Event field; `TestNarrationSinkNamesTheSubAgent` stays green.

**Acceptance.** `go build ./... && go test ./internal/reactions/... ./cmd/apogee/ -run 'Match|Narration|Firing|RootHook|Runner|Headless'`; `grep -rn "WriteTarget" internal/reactions cmd/apogee internal/agent --include=*.go | grep -v 'Workspace\|workspaceWriteTarget\|classifyWriteTarget\|writeTargetClass\|ResolvedWriteTarget'` prints no symbol or comment naming `reactions.WriteTarget`, `Options.WriteTarget` or the injected closure.

Commit: `refactor(reactions): file-changed reads the write target off the Event; drop Options.WriteTarget and both Driver closures`

## 4. One Reaction payload document — ✅ DONE (2026-09-15)

NOTES (2026-09-15): the four `reactions.EnvEvent/EnvName/EnvWorkspace/EnvPath` constants (and the two Schedule ones) were deleted rather than aliased — the plan offered either; `command_test.go` reads `domain.EnvReaction*`, and `TestSeamPayloadEnvNamesMatchTheObserveLane` was deleted with the keys-parity test as one set of names leaves nothing to compare.
NOTES (2026-09-15): `match.go`'s `applyBase` became a package function `applyBase(*domain.SeamPayload, domain.EventBase)` — a method cannot be declared on a type from another package.
NOTES (2026-09-15): the plan's "observe payload stdin golden" is the literal-bytes assertion added to `TestCommandExecutorFeedsThePayloadOnStdinAndTheHookFactsInTheEnvironment` (captured from the pre-item type) plus `TestPayloadJSONGolden`'s unchanged expected strings; `TestSeamPayloadEnv` gained the Schedule-pair case.
NOTES (2026-09-15): `example_test.go` and `internal/reactions/match_test.go` are listed in the plan's Files but needed no edit — both compile unchanged against the alias and the `firing.Payload` field.
NOTES (2026-09-15): consequential edit — internal/eventjson/doc.go: made necessary by deleting `reactions.Payload` (its package comment named the type)
NOTES (2026-09-15): consequential edit — internal/tools/exec_common.go: made necessary by deleting `reactions.Payload.Env` (a comment named it beside `domain.SeamPayload.Env`)
NOTES (2026-09-15): consequential edit — internal/domain/doc.go, internal/reactions/doc.go: made necessary by the payload document moving whole into `seampayload.go` (both file maps described the old split)
NOTES (2026-09-15): consequential edit — docs/manual/reactions.md: made necessary by the merge (§The seam document opened "reads a **different** document"; now "the **same** document cut to the call" — the field tables and examples are untouched)

**What.** `domain.SeamPayload` (`internal/domain/seampayload.go`) becomes the only payload document: fold the observe-only fields of `reactions.Payload` (`internal/reactions/payload.go`) into it as `omitempty` members and keep one `Env()`; `internal/reactions` builds a `domain.SeamPayload` for observe firings and deletes its own type. Stdin JSON keys and field order for every existing member stay byte-identical (the two documents already share their keys). Delete the tag-pinning test `TestSeamPayloadSharesTheObservePayloadsKeys`. Fix `CONTEXT.md` §Reaction: an observe firing is delivered by the Runner and never books a `ReactionFiredEvent`; only advise/gate firings do. Depends on item 1.

**Regression guard.** Deleting `reactions.Payload` reaches readers the draft did not list, all now in Files: the public alias `apogee.go` `ReactionPayload = reactions.Payload` (→ `domain.SeamPayload`; `example_test.go`), the `Executor` implementers `internal/reactions/exec.go`, `webhook.go`, `runner_test.go` and the two `cmd/apogee` doubles in `wire_settings_test.go`, the decoders in `cmd/apogee/headless_test.go` and `e2e_reactions_test.go`, and `match.go`'s `Payload` literals (`applyBase` included). `Schedule *ScheduleRef` cannot fold into `domain.SeamPayload` while `ScheduleRef` lives in `internal/reactions` (ADR 0010): move `ScheduleRef` to `internal/domain` and keep `type ScheduleRef = domain.ScheduleRef` in `reactions` (`Options.Schedule` and its `cmd/apogee` callers untouched); the one `Env()` is the SIX-entry observe one (Schedule id/name kept — `TestPayloadEnv`, `docs/manual/reactions.md`). `TestSeamPayloadEnvNamesMatchTheObserveLane` reads `reactions.EnvEvent/EnvName/EnvWorkspace/EnvPath`: delete it with the keys-parity test (or alias the four `reactions` constants to `domain`'s). Member order rule: schedule/status/faulted/step_capped before tool/path, arguments/result last — the only order that keeps BOTH `TestPayloadJSONGolden` and `TestSeamPayloadJSONOmitsWhatTheMomentDoesNotCarry` byte-identical. Items 3 then 4 execute in order (both rewrite `match.go` and `match_test.go`).

**Files:** `internal/domain/seampayload.go`, `internal/domain/seampayload_test.go`, `internal/reactions/payload.go`, `internal/reactions/payload_test.go`, `internal/reactions/runner.go`, `internal/reactions/runner_test.go`, `internal/reactions/command.go`, `internal/reactions/command_test.go`, `internal/reactions/exec.go`, `internal/reactions/webhook.go`, `internal/reactions/webhook_test.go`, `internal/reactions/match.go`, `internal/reactions/match_test.go`, `apogee.go`, `example_test.go`, `cmd/apogee/wire_settings_test.go`, `cmd/apogee/headless_test.go`, `cmd/apogee/e2e_reactions_test.go`, `CONTEXT.md`.
**Read first:** internal/reactions/payload.go — Payload, ScheduleRef, Env; internal/domain/seampayload.go — SeamPayload, Env; internal/reactions/match.go — applyBase; internal/reactions/exec.go — encodePayload; apogee.go — ReactionPayload

**Tests.** `TestPayloadEnv*` / `TestSeamPayload*` re-pointed at the one type; `TestSeamPayloadEnvNamesMatchTheObserveLane` deleted with the keys-parity test (or re-pointed at aliased constants); `TestPayloadJSONGolden` and `TestSeamPayloadJSONOmitsWhatTheMomentDoesNotCarry` stay byte-identical; a golden asserting an observe payload's stdin JSON is unchanged from the pre-item bytes.

**Acceptance.** `go build ./... && go test ./internal/domain/... ./internal/reactions/... -run 'Payload|Seam'`; `grep -n "type Payload" internal/reactions/*.go` prints nothing.

Commit: `refactor(reactions): one payload document in domain; observe firings build it too`

## 5. One recover boundary for every delegation — ✅ DONE (2026-09-15)

NOTES (2026-09-15): consequential edit — internal/agent/fanout_test.go: TestFanOut_ChildPanicRecoversWithoutKillingTheSibling's doc comment said the boundary's "new home" was the worker; rewritten to name runSubAgent's frame and the serial mirror test — made necessary by moving the recover out of runDelegation.
NOTES (2026-09-15): runDelegation keeps its `(ctx, turn, call)` signature with `turn` now unread there (the recover reads `a.turns.index`), per the guard's "stays as the phase-bracketing wrapper item 6's run phase absorbs".

**What.** Recast at the regression check (2026-09-15). Move the `recover()` that today lives only in the pool's `runDelegation` (`internal/agent/dispatch.go`) into `runSubAgent`'s own frame (`internal/agent/subagent.go` — the entry both paths share; see the guard), so a serial delegation panic is contained at the child's boundary exactly as a fan-out one is (ADR 0039 D4: each child keeps panic recovery at its own boundary). Delete the "serial path deliberately keeps its existing shape" comment. Depends on item 1.

**Regression guard.** the ONE recover lives in runSubAgent's own frame (the entry both the serial executeDelegate path and the pool's runDelegation goroutine share), emitting the ErrorEvent + "tool %q panicked" result exactly as runDelegation's defer does today; only after it is in place are runDelegation's defer and the "serial path deliberately keeps its existing shape" comment deleted — a panic never crosses the pool goroutine's top frame (dispatch.go's placement comment and ADR 0039 D4 both stay satisfied); TestFanOut_ChildPanicRecoversWithoutKillingTheSibling stays green and the new serial-path test mirrors it. The documented placement (`dispatch.go`'s "a panic crossing a goroutine's top frame takes the process down" comment; ADR 0039 §Decision 4) is honoured, not superseded. Second round: runSubAgent KEEPS its signature `runSubAgent(ctx, call)` (five direct test callers in seat_test.go, subagent_test.go, live_delegate_cap_test.go stay untouched) and the recover reads the turn as `a.turns.index` (what dispatchTools' `turn` carries); the recover defer is registered FIRST in runSubAgent so it runs after the reaping defer (unregister / mailbox close / sub.Close) and still catches a panic raised in it. dispatch.go's "The recover sits HERE, inside the goroutine, because that is the only place it can be" paragraph is rewritten to point at runSubAgent's frame (still inside every pool goroutine's call chain); runDelegation stays as the phase-bracketing wrapper item 6's `run` phase absorbs.

**Files:** `internal/agent/dispatch.go`, `internal/agent/subagent.go`, `internal/agent/dispatch_test.go`, `internal/agent/fanout_test.go`.
**Read first:** internal/agent/dispatch.go — runDelegation, runDelegationPool, executeDelegate, executeTool (the leaf recover to mirror); internal/agent/subagent.go — runSubAgent, delegationResult; internal/agent/fanout_test.go — TestFanOut_ChildPanicRecoversWithoutKillingTheSibling; internal/agent/loop.go — base

**Tests.** A serial-path test that panics inside a delegate and asserts the parent Step continues with a tool error, mirroring `TestFanOut_ChildPanicRecoversWithoutKillingTheSibling`, which stays green; the ErrorEvent carries the current turn; the package's direct `runSubAgent(ctx, call)` callers compile unchanged.

**Acceptance.** `go build ./... && go test ./internal/agent/ -run 'Delegat|Recover|Panic'`.

Commit: `fix(agent): a serial delegation panic is recovered at the child's boundary like a fan-out one`

## 6. One per-call pipeline; the serial path is the pool at width one — ✅ DONE (2026-09-15)

NOTES (2026-09-15): consequential edit — internal/agent/subagent.go: the `isSubAgentCall` and `runSubAgent` doc comments named `resolveAndExecute` and "the serial executeDelegate and the pool's runDelegation worker"; made necessary by collapsing both onto `runDelegation`.
NOTES (2026-09-15): consequential edit — internal/agent/resolution.go, internal/tools/tools.go, internal/tui/toolargs.go, internal/tui/doc.go: comments pointing at `agent.resolveAndExecute` for the colliding/repeated-key refusal now point at `agent.prepareCall`; made necessary by deleting `resolveAndExecute`.
NOTES (2026-09-15): consequential edit — docs/adr/0013…md: the Decision sentence naming `resolveAndExecute` gained a dated in-place pointer to `prepareCall` (plan's ADR-text rule); made necessary by deleting `resolveAndExecute`.
NOTES (2026-09-15): consequential edit — internal/agent/gate_test.go, internal/agent/setreactions_test.go, internal/agent/advise_test.go, internal/agent/advise_argv_test.go, internal/agent/subagent_test.go: callers of `resolveAndExecute`/`prepareDelegation`/`dispatchSerially`/`commitDelegation`/`fanOutSlot` re-pointed at `prepareCall`+`runCall` (one `prepareAndRun` helper in gate_test.go), `dispatchGroup(…, 1, …)`, `commitCall` and `dispatchSlot`, and their comments; made necessary by the renames — no assertion changed. Those tests now also cross the ToolCallEvent and pre-tool-exec Moment `prepareCall` fires, which the old seam skipped; all stay green.
NOTES (2026-09-15): the pending-interjection row asserts "no gate activity after the skipped call's ToolCallEvent" at width 1 only — above 1 the check sits at the pool's dequeue by the guard's own rule, after the group was prepared (and gated) whole, so the row pins the gate being asked about the skipped delegation there; Approver, audit and phase assertions hold at both widths.
NOTES (2026-09-15): internal/agent/delegationphase_test.go (in Files) needed no change — nothing in it named a replaced symbol.

**What.** Recast at the regression check (2026-09-15). Refactor `dispatchSerially` and the fan-out path in `internal/agent/dispatch.go` onto one `prepare → run → commit` pipeline: `prepare` (ToolCallEvent, `pre-tool-exec` Moment, lookup, colliding/repeated checks, guard tier, `resolve()`, gates) and `commit` (audit, `post-tool-result`, append) are one function each; `run` is the leaf execute or the child run behind item 5's boundary; width is a parameter (serial = 1 — the per-call loop, see the guard). `resolve()`, `applyGates`, `gateAsk`, `gateRefusal` are NOT changed. Delete every "seam parity" comment (rule: every comment naming the other path — `grep -rn "serial path\|fan-out path" internal/`, `internal/agent/doc.go` and `internal/domain/events.go` included); the parity tests that pin two pipelines are re-pointed, not deleted (see the guard). Depends on item 5.

**Regression guard.** width 1 is the per-call loop prepare(i)→run(i)→commit(i) in emitted order — a call's result is in history before the next call's ToolCallEvent/pre-tool-exec/resolve, exactly as dispatchSerially does today (ADR 0039 D1 "cap 1 reproduces today's behavior exactly", D3 "a child's own delegations run serially inline" are preserved, not reversed); prepare-all/commit-all is the width>1 shape only. commit keeps the per-kind audit tail: recordExecutedTrip for a leaf Run/Confine, recordExecuted for a delegation (trip-silent), executeRefuse's recordBlocked stays in prepare. The three "parity" tests TestFanOut_CollidingArgumentKeysAreRefusedLikeASerialCall, TestFanOut_RepeatedArgumentKeysAreRefusedLikeASerialCall, TestFanOut_ToolCallEventCarriesTheResolvedPath are RE-POINTED as width-N rows of the new table test, not deleted; the new table test drives a TWO-call reply (one leaf, one delegation) at width 1 and width N so the interleaving is pinned. Prose rule widened: `grep -rn "serial path\|fan-out path" internal/` (internal/agent/doc.go and internal/domain/events.go included); Acceptance greps exclude _test.go and comments. TestContextFillNoticeReArmsAfterACancelledTurnRollsBack stays green and is named in Tests. ADR 0039 D1/D3 and `dispatch.go`'s "only the child RUN is concurrent" note are honoured, not superseded. Second round: a leaf's audit tail STAYS inside run — it is per-OUTCOME, not per-kind: executeRun/executeConfine/executeGate/executeConfineFallback are unchanged (an approved Gate records executed via executeGate→executeRun→recordExecutedTrip and feeds the breaker; a denied gate and a Confine fallback denial recordBlocked inside run), executeRefuse's recordBlocked stays in prepare; commit does only recordExecuted for a delegation that ran (commitDelegation's rule), then post-tool-result and append — never keyed on verdict.kind (dispatch_test.go's "audit records = %d, want 1 (the blocked call)" stays green). At width 1 the interjection check sits where dispatchSerially has it — after pre-tool-exec, before lookup/resolve/gates (the skip result replaces resolveAndExecute) — and stays at dequeue for width>1; the new table test's delegation row asserts no gate-script/Approver/audit activity after the ToolCallEvent for the skipped call, and TestDispatchSerially_PendingInterjectionSkipsTheNextDelegation stays green. The pipeline's `run` phase WRAPS item 5's `runSubAgent` boundary for a delegation — no recover is re-added in the pool worker or anywhere else; the only `recover()` sites after item 6 are `runSubAgent` (delegations) and `executeTool` (leaves), and that pair is the item's Acceptance grep (`grep -n "recover()" internal/agent/dispatch.go` shows exactly those two).

**Files:** `internal/agent/dispatch.go`, `internal/agent/gate.go` (comments only), `internal/agent/doc.go` (comment), `internal/domain/events.go` (comment), `internal/agent/dispatch_test.go`, `internal/agent/fanout_test.go`, `internal/agent/delegationphase_test.go`.
**Read first:** internal/agent/dispatch.go — dispatchSerially, prepareDelegation, runDelegationPool, commitDelegation, resolveAndExecute, executeGate, recordExecutedTrip; internal/agent/fanout_test.go — TestDispatchSerially_PendingInterjectionSkipsTheNextDelegation

**Tests.** The three parity tests re-pointed as width-N rows of the new table test; the existing ladder, gate and fan-out behaviour tests stay green unchanged; `TestContextFillNoticeReArmsAfterACancelledTurnRollsBack`, `TestDispatchSerially_PendingInterjectionSkipsTheNextDelegation` and `TestAuditEvent_EmittedForExecutedCall` stay green; one new table test drives a two-call reply (one leaf, one delegation; plus a colliding-keys, a repeated-key and a path-carrying row) through width 1 and width N and asserts the Events — identical per call, interleaved per the guard at width 1 — with a pending-interjection delegation row asserting no gate/Approver/audit activity for the skipped call.

**Acceptance.** `go build ./... && go test ./internal/agent/...`; `grep -rn "serial path\|fan-out path" internal/ --include=*.go | grep -v _test.go` prints nothing; `grep -n "recover()" internal/agent/dispatch.go internal/agent/subagent.go` shows exactly two sites — `runSubAgent` and `executeTool`.

Commit: `refactor(agent): one per-call pipeline with width as a parameter`

## 7. `hookExecutionCtx` stops installing an Auto subprocess permit for post-response reactions — ✅ DONE (2026-09-15)

NOTES (2026-09-15): consequential edit — internal/agent/syncexec_test.go: made necessary by deleting `hookExecutionCtx` (its `syncAgent` comment named the symbol and the Acceptance grep over `*.go` would have matched it); one comment sentence rewritten.
NOTES (2026-09-15): `TestFirePostResponseInstallsTheSubprocessPermit` renamed `TestFirePostResponseInstallsNoSubprocessPermit` on inversion — the old name asserted the opposite of the new assertion; still matched by the Acceptance `FirePostResponse` pattern. `TestHookSubprocessPermitLadder` keeps its name (it still walks the rows) and gains an "auto under a plan-mode parent" row via `runTurnWithPermitProbe`'s `tighten`, so the deleted `TestHookSubprocessPermitReadsEffectiveMode` case survives as a no-permit row and the helper's parameter stays in use; `assertPermitBox` deleted as dead (no granted permit remains to assert on).
NOTES (2026-09-15): `internal/agent/dispatch_test.go` is named in Files but holds no post-response permit reference (its permit tests are the ADR 0049 write-escape ones) — untouched.

**What.** In `internal/agent/dispatch.go`, `hookExecutionCtx` installs a `SubprocessPermit` for every post-response reaction in Auto; no shipped Reaction spawns at that Moment (retired lab-row leftover). Remove the post-response permit row; keep the `pre-tool-exec` one the gate stage uses (ADR 0076 D8). Depends on item 6.

**Regression guard.** hookpermit_test.go is named in Files; items 7 → 8 → 9 execute in that order and 8/9 name hookpermit_test.go too. `TestHookSubprocessPermitLadder` (`internal/agent/hookpermit_test.go`; its "auto with confine off/on" rows want `granted=true`) and `TestFirePostResponseInstallsTheSubprocessPermit` (`internal/agent/reactions_test.go`) go red: invert both into the "no permit at post-response in any mode" assertion (`runTurnWithPermitProbe`/`permitProbe` are the helpers the new test reuses); `TestHookSubprocessPermitReadsEffectiveMode` would stay green vacuously — delete it. "Keep the `pre-tool-exec` one" names a row `hookExecutionCtx` does not have: its table is the post-response row alone, and the gate stage's permit is minted by `syncPermitCtx` (called from `runSyncArgv`, `internal/agent/syncexec.go`), so removing the row = deleting `hookExecutionCtx` whole and its only caller, the `if m == domain.MomentPostResponse` branch in `fireCascade` (`internal/agent/reactions.go`); `syncPermitCtx` and `runSyncArgv` — the sync lane's pre-tool-exec/post-tool-result permit — are untouched; "the existing gate-permit test" = `TestSyncArgv*` in `internal/agent/syncexec_test.go`. Supersedes, by dated in-place amendment in the same commit: `docs/design/confinement-execution-contract.md` §10.3 ("The ladder row the engine installs") and §10.4's first scope row, which name `hookExecutionCtx` as a live minting site; ADR 0076's 2026-09-12 note ("§10.3's post-response row keeps its mode term"); and the `SubprocessPermit`/`WithSubprocessPermit` comments in `internal/domain/confinement.go` naming the post-response cascade.

**Files:** `internal/agent/dispatch.go`, `internal/agent/reactions.go`, `internal/agent/dispatch_test.go`, `internal/agent/hookpermit_test.go`, `internal/agent/reactions_test.go`, `internal/domain/confinement.go` (comments), `docs/design/confinement-execution-contract.md` (§10.3/§10.4 amendment), `docs/adr/0076-one-reaction-core-with-an-origin-by-class-policy-matrix.md` (dated note).
**Read first:** internal/agent/dispatch.go — hookExecutionCtx, syncPermitCtx, effectiveMode; internal/agent/reactions.go — fireCascade; internal/agent/hookpermit_test.go — permitProbe, runTurnWithPermitProbe, TestHookSubprocessPermitLadder; internal/agent/syncexec.go — runSyncArgv; docs/design/confinement-execution-contract.md — §10.3

**Tests.** `TestHookSubprocessPermitLadder` and `TestFirePostResponseInstallsTheSubprocessPermit` inverted to assert no permit is present in the ctx handed to a post-response handler in any mode (Auto included); `TestHookSubprocessPermitReadsEffectiveMode` deleted; `TestSyncArgv*` (the gate-permit tests) stay green.

**Acceptance.** `go build ./... && go test ./internal/agent/ -run 'Permit|HookExecution|Gate|SyncArgv|FirePostResponse'`; `grep -rn "hookExecutionCtx" internal/ --include=*.go` prints nothing.

Commit: `refactor(agent): drop the post-response subprocess permit row`

## 8. A delegate is built from a `delegation` value — ✅ DONE (2026-09-15)

NOTES (2026-09-15): the `delegation` value carries more fields than the plan's list names (`tokenCap`, `timeCap`, `now`, `effortDialect`, `upstreamOwned`, `tap`; `guards`, `contextFiles`, `journal`, `consoles`, `latch` spell out its `sharedHandles`; `seat` itself is not stored — it is consumed while composing the latch) — every one of the 20 writes `newChildAgentOn` made after `newAgent` moved into the value; the one deleted outright is `child.tasks = tasklist.New()` (the constructor's fresh list is the guarantee, stated on the type and on the literal).
NOTES (2026-09-15): constructor shape — `newAgent(cfg, up)` keeps its signature and `newDelegateAgent(cfg, up, d)` is added beside it, both over one `buildAgent(cfg, up, d)`; the fields the two kinds hold differently are seeded (`seedTopLevel` / `delegation.seed`) rather than built-then-replaced; `reloadContextFiles` runs only for `d == nil`.
NOTES (2026-09-15): no-read test taken through a loader seam (`contextFileLoader` package var in contextfiles.go, house pattern of `restreamHoldoff`), serial test with Cleanup restore — a directory-swap assertion alone cannot tell a read-then-overwrite from no read; verified the test fails on an unconditional reload (both assertions bite).
NOTES (2026-09-15): `hookpermit_test.go` (named in Files) needed no edit — it fakes a child through `a.liveMode`, a runtime field the item leaves in place, and compiles and passes unchanged.
NOTES (2026-09-15): consequential edit — internal/agent/agent.go: made necessary by the constructor split (two field comments: the latch is seeded by construction, a child is built through `newDelegateAgent`).
NOTES (2026-09-15): consequential edit — internal/agent/doc.go: made necessary by the constructor split (package map rows for construct.go and subagent.go).
NOTES (2026-09-15): consequential edit — docs/adr/0026-workspace-context-files-are-session-scoped-prompt-data.md: made necessary by closing the discarded read (dated amendment under §6, "copied rather than re-read" is now literal).

**What.** Add `delegation{depth, spawnCallID, task, name, stepCap, parentLiveMode, seat, seatFallback, consoleOwner, sharedHandles}` in `internal/agent/subagent.go`; `newAgent(cfg, upstream, d *delegation)` (`internal/agent/construct.go`) builds a child directly from it — no post-construction writes, `effortDialect` set once, context files taken from the parent (the child never re-reads them from disk — the discarded read is the in-passing defect), journal/consoles/tasks/guards handed in rather than replaced. `newChildAgentOn` shrinks to composing the value. Every field assignment `newChildAgentOn` makes today is enumerated at write time and either moves into the value or is deleted. Depends on item 1.

**Regression guard.** `newAgent(cfg, up)` keeps its signature — it is called at ~370 sites across 76 `internal/agent` test files plus `New` (`agent.go`) and `resumeAgent` (`construct.go`) — and the delegate constructor is added beside it (`newDelegateAgent(cfg, up, d)` or a variadic `newAgent(cfg, up, opts...)`), so no existing call site moves. The `delegation` value is the constructor INPUT; the Agent's runtime fields (`depth`, `stepCap`, `liveMode`, `midExchangeCompaction`, `task`, `callID`) stay where they are — construction copies them once; "no post-construction writes" is a fact about `newChildAgentOn`, not about the field set — so the tests that fake a child by poking a field (`hookpermit_test.go`, `autocompact_guard_test.go`, `subagent_test.go`, `emptyreply_test.go`, `hooksynthesis_test.go`, `fillnotice_test.go`, `undo_group_test.go`) keep compiling. The no-read test cannot be written as drafted: `construct_test.go` has no seam, `readContextFile` is `security.SafeOpen(workspaceDir, name)` with no injection point and `reloadContextFiles` is unconditional in `newAgent` — either add a swappable loader (an unexported package var wrapping `readContextFile`, or a field on the `delegation` value that says "cache handed in, skip `reloadContextFiles`") and count through it, or state the test as: the delegate constructor never calls `reloadContextFiles` — assert via a workspace whose context file is replaced by a directory AFTER the parent is built and a child whose cache carries no error entry. Item 7 executes before this item; `hookpermit_test.go` is named in Files.

**Files:** `internal/agent/construct.go`, `internal/agent/subagent.go`, `internal/agent/contextfiles.go` (if the loader seam is chosen), `internal/agent/subagent_test.go`, `internal/agent/construct_test.go`, `internal/agent/contextfiles_test.go`, `internal/agent/hookpermit_test.go`.
**Read first:** internal/agent/subagent.go — newChildAgentOn, newChildAgent, runSubAgent; internal/agent/construct.go — newAgent, resumeAgent; internal/agent/agent.go — Agent; internal/agent/contextfiles.go — reloadContextFiles, readContextFile

**Tests.** `TestNewChildAgent*` re-pointed; a test asserting a child spawn performs no file read of the workspace context files — through the loader seam, or as the guard's directory-swap assertion.

**Acceptance.** `go build ./... && go test ./internal/agent/ -run 'Child|Construct|Delegat'`.

Commit: `refactor(agent): a delegate is constructed from a delegation value, not overwritten`

## 9. One `isDelegate()` predicate — ✅ DONE (2026-09-15)

NOTES (2026-09-15): `internal/agent/hookpermit_test.go` (named in Files) needed no edit — after item 7 it carries no depth or task predicate; left untouched. `internal/agent/compact.go` likewise holds no child-ness read (only the `midExchangeCompaction` contract flag the guard exempts) — unchanged.
NOTES (2026-09-15): the new test `TestIsDelegate_TopLevelFalseChildTrue` lives in `agent_test.go` (not in Files) and reuses `delegateOn` from `delegatereport_test.go` to build a real parent/child pair rather than poking `depth`.

**What.** Replace every "is this a child?" test (`depth > 0`, `spawnCallID != ""`, `task != ""`, `stepCap`, `liveMode`, `midExchangeCompaction` reads used as child-ness) across `internal/agent/{agent,children,compact,dispatch,loop}.go` with `a.isDelegate()` reading item 8's value. Rule: every site that tests one of those fields for child-ness — `grep -n "depth > 0\|depth == 0\|callID != \"\"\|task != \"\"" internal/agent/*.go`. Depends on item 8.

**Regression guard.** `isDelegate()` is `a.depth > 0` (depth stays a runtime field, read numerically by `base()` and `WithSubAgentDepth` anyway); the rule covers depth predicates only — `liveMode` (a composed accessor), `midExchangeCompaction` (a contract flag `compact.go` documents) and `stepCap` keep their own reads, so the tests that fake a child by setting one field (`emptyreply_test.go`, `hooksynthesis_test.go`, `undo_group_test.go`, `autocompact_guard_test.go`, `hookpermit_test.go`) stay green. `stepCap` is struck from What's list: `a.stepCap > 0` in `Run` (`agent.go`) is the BOUND, not child-ness — replaced by `isDelegate()` a delegate with `delegation.max-steps` unset (stepCap 0 = uncapped today) would finish at its first Turn boundary. The site rule is `grep -n "a\.depth *[!=<>]=\? *0" internal/agent/*.go | grep -v _test` plus `task != ""` / `callID != ""`: it catches the `depth != 0` / `depth <= 0` spellings in `agent.go`, `dispatch.go` and `delegatereport.go` (added to Files); comments are reworded, tests left alone; `fillnotice.go`'s `view.Depth() > 0` is a reaction-facing `LoopView` read, not the Agent's own predicate — out of scope. Item 7 executes before this item; `hookpermit_test.go` is named in Files.

**Files:** `internal/agent/agent.go`, `internal/agent/children.go`, `internal/agent/compact.go`, `internal/agent/dispatch.go`, `internal/agent/loop.go`, `internal/agent/delegatereport.go`, `internal/agent/hookpermit_test.go`.
**Read first:** internal/agent/agent.go — closeUndoGroup, Run (stepCap check); internal/agent/loop.go — beginTurnJournal, the delegate FinishLength rule; internal/agent/dispatch.go — fanOutWidthFor, delegationWidth, interjectionPending, effectiveMode; internal/agent/delegatereport.go — delegateReportBlock

**Tests.** Existing child-behaviour tests stay green (the field-poking setups named in the guard untouched); one test that a top-level agent answers `false` and a child `true`.

**Acceptance.** `go build ./... && go test ./internal/agent/...`; `grep -n "a\.depth *[!=<>]=\? *0\|task != \"\"\|callID != \"\"" internal/agent/*.go | grep -v _test | grep -v "^\S*:[0-9]*:\s*//"` shows only `isDelegate`'s own body.

Commit: `refactor(agent): one isDelegate predicate`

## 10. Every dial crosses one `Dialer` seam — ✅ DONE (2026-09-15)

NOTES (2026-09-15): consequential edit — internal/agent/doc.go: made necessary by the Dialer seam and WithDialer option landing in agent.go (the package map's agent.go role line names them).
NOTES (2026-09-15): the httptest list shrank to exactly the four files the guard names — apikey_test.go, construct_test.go, harness_test.go, routedspawn_test.go (its wire test) — no further HTTP-testing file needed naming; fanout_test.go's `gruntUpstream` became a `gruntResponder` behind the fake Dialer (`routeToGrunt`).
NOTES (2026-09-15): the dial-only tests that build the parent through white-box `newAgent` install the fake by writing the Agent's `dial` field (same-package tests already poke sibling fields); the option path itself is covered by `New`/`Resume` in dialer_test.go and the reworked `TestSwitchUpstreamSwapsTheProviderClient`.

**What.** Add `type Dialer func(endpoint, model, apiKey string, opts ...provider.Option) provider.Responder` in `internal/agent` with a `WithDialer` constructor option on `New`/`Resume` (default `provider.NewClient`); the Agent holds it and `SwitchUpstream` (`rebind.go`), the routed spawn (`subagent.go`) and the child inherit it. `apogee.go` gains nothing (facade stays thin; tests reach the option through `internal/agent`). Tests in `routedspawn_test.go`, `switchupstream_test.go` and the other five agent test files that stand up `httptest.NewServer` for a dial switch to a fake Dialer returning the fake Responder ADR 0001 relies on. Depends on item 8.

**Regression guard.** `agent.go` holds TWO dial sites today — `New` and `Resume` — so both take the option (What's "one" reads as two); `rebind.go`'s "nothing can fail (`provider.NewClient` never does…" comment survives the refactor, so the acceptance grep is comment-blind (`provider\.NewClient(` with `grep -v "^\S*:[0-9]*:\s*//"`) or that comment is reworded. `routedspawn_test.go` cannot drop httptest wholesale: `TestRoutedChildSummarizerSpeaksTheTargetsDialectOnTheWire` asserts the effort field ON THE WIRE and `construct_test.go`'s `TestInspectorSurvivesASwitchUpstream` observes `WireRecords` the real client's observer option produces — only the DIAL-ONLY tests switch to the fake Dialer; `grep -ln "httptest.NewServer" internal/agent/*_test.go` shrinks to `apikey_test.go`, `construct_test.go`, `harness_test.go`, `routedspawn_test.go` (its wire tests) and whichever other file tests HTTP itself — a list named at implement time, not an open-ended shrink.

**Files:** `internal/agent/agent.go`, `internal/agent/construct.go`, `internal/agent/rebind.go`, `internal/agent/subagent.go`, `internal/agent/routedspawn_test.go`, `internal/agent/switchupstream_test.go`, `internal/agent/dialer_test.go`.
**Read first:** internal/agent/agent.go — New, Resume, closeOwnedUpstream; internal/agent/rebind.go — SwitchUpstream, Rebind; internal/agent/subagent.go — newChildAgentOn (routed dial, ownsUpstream, armWireCapture); internal/agent/construct.go — armWireCapture, wireTap; internal/provider — NewClient, Option

**Tests.** `TestDialerIsUsedForSwitchUpstreamAndRoutedSpawn`; the DIAL-ONLY tests switch to the fake Dialer while the wire tests named in the guard keep httptest; `grep -ln "httptest.NewServer" internal/agent/*_test.go` shrinks to `apikey_test.go`, `construct_test.go`, `harness_test.go`, `routedspawn_test.go` (wire) and the files named at implement time.

**Acceptance.** `go build ./... && go test ./internal/agent/ -run 'Dial|Routed|SwitchUpstream'`; `grep -n "provider\.NewClient(" internal/agent/*.go | grep -v _test | grep -v "^\S*:[0-9]*:\s*//"` shows exactly one site (the default).

Commit: `refactor(agent): one Dialer seam for every provider dial`

## 11. `domain.Reaction.Validate` owns every per-reaction rule — ✅ DONE (2026-09-15)

NOTES (2026-09-15): the config layer already called `Reaction.Validate` once per mapped entry and held no runnability rule, so `internal/config/reactions.go` changed only in the comment over that loop; the "exactly one site" sentence the guard asks to delete exists in no source file (nothing to delete).
NOTES (2026-09-15): `TestHookValidateRefusesAReactionTheCoreRejects` gained an empty-argv row so hooks_test.go still proves the forwarder surfaces the moved rule; `unparseableURL` moved with the URL rows to `internal/domain/reaction_test.go`.
NOTES (2026-09-15): `internal/agent/syncexec.go`'s fire-time `run: is empty` branch is now unreachable from config but left in place — it still guards a Reaction built in code that skipped `Validate`, and the file is outside this item's Files.

**What.** Recast at the regression check (2026-09-15). Move the handler-runnability rules of `reactions.Validate` (`internal/reactions/hooks.go` — argv[0] present, URL shape, headers-env) into `domain.Reaction.Validate` (`internal/domain/reaction.go`) so one table holds every rule a Reaction value can break; the non-positive-timeout rule stays lane-side (see the guard). `internal/config/reactions.go` keeps only the on-disk shape and the Floor-key refusal and calls `Reaction.Validate` once per entry. `reactions.Validate` / `ValidateAll` become thin forwarders (or are deleted when no caller remains). Admitted widening: the sync lane's `advise:`/`gate:` argv entries gain the argv[0] rule at load (an empty `advise: []` or `gate: ["  "]` loads today and fails at fire time with `run: is empty`; after, it is a startup refusal, and `syncexec.go`'s fire-time branch becomes unreachable from config). Depends on item 1.

**Regression guard.** move ONLY the argv[0], URL and headers-env rows into domain.Reaction.Validate; the non-positive-timeout rule STAYS lane-side (observe-lane only, in config's observe mapping / ValidateAll) because Timeout 0 means "class default" on the sync lane and for every Go handler (CONTEXT.md's Timeout entry and config/reactions.go's 0s note bind). Drop "notice length" (no such rule). The moved rows take the core's `apogee: invalid reaction %q:` wrap; config keeps reactionEntryError (`reaction %q: %s`) for on-disk shape refusals — delete the "exactly one site" sentence. TestHookValidateRefusesEachRule: only the argv/URL/headers-env rows move to internal/domain/reaction_test.go; the id, moments and unknown-event rows stay in hooks_test.go against the forwarder (trim + ParseEvent stay in internal/reactions — domain cannot import it, ADR 0010). The item yields to CONTEXT.md's Timeout entry ("class default on either lane") and `config/reactions.go`'s 0s note. Second round: the moved argv[0] row reaches the SYNC lane too (config/reactions.go maps `advise:`/`gate:` to ArgvHandler with no argv rule today) — the widening is admitted in What, and the moved sentence is worded key-neutrally ("the command's first element is the program to run and must not be blank"), never `run: …`, since it now fires for an entry written under `advise:`/`gate:`; one `advise: []` row joins TestLoadFileConfigRefusesMalformedReactions.

**Files:** `internal/domain/reaction.go`, `internal/domain/reaction_test.go`, `internal/reactions/hooks.go`, `internal/reactions/hooks_test.go`, `internal/config/reactions.go`, `internal/config/reactions_test.go`.
**Read first:** internal/reactions/hooks.go — Validate, validateHandler, validateWebhook; internal/domain/reaction.go — Reaction.Validate, ErrInvalidReaction; internal/config/reactions.go — entryReactions, argvList; internal/agent/syncexec.go — runSyncArgv

**Tests.** `TestHookValidateRefusesEachRule`: the argv/URL/headers-env rows move to `internal/domain/reaction_test.go`; its id, moments and unknown-event rows stay in `hooks_test.go` against the forwarder; `TestLoadFileConfigRefusesMalformedReactions` stays green with the same sentences and gains one `advise: []` row (refused at load with the key-neutral argv[0] sentence).

**Acceptance.** `go build ./... && go test ./internal/domain/... ./internal/reactions/... ./internal/config/... -run 'Validate|Reaction'`.

Commit: `refactor(domain): Reaction.Validate owns every per-reaction rule`

## 12. Consumers validate once; `SetReactions` arms like `Config.Reactions`

**What.** `Generation.Validate` (`internal/domain/reaction.go`) checks lane class and duplicates only, no longer calling `Reaction.Validate` per entry; `Runner.New`/`Replace` (`internal/reactions/runner.go`) stop re-running `ValidateAll` — the caller validates. `Agent.SetReactions` (`internal/agent/agent.go`) runs `Generation.Validate` and the builtin-id reservation `armReactions` applies to `Config.Reactions` (`internal/agent/reactions.go`), refusing instead of installing (in-passing defect: the live route installed unvalidated Sync). `CONTEXT.md` §Reaction's duplicate-id sentence names the arming seam as the one place. Depends on item 11.

**Regression guard.** same lane premise as 11 — Generation.Validate and the arming seam never apply the timeout rule to sync entries or Go handlers. `Agent.SetReactions` returns nothing today; refusing needs an `error` return, and errcheck (golangci's standard set) then fails `make check` at every bare call — `internal/run/run.go`, `cmd/apogee/wire_engine.go` (the bind replay and `lateEngine.SetReactions`) and the test callers (rule: `grep -rln "SetReactions(" --include=*.go .` — `setreactions_test.go`, `setlive_test.go`, `subagent_test.go`, `floorguards_test.go`, `advise_test.go`, `reactions_test.go`, `wire_engine_test.go`, `wire_settings_test.go`, `wire_helpers_test.go` today) — all in Files; `lateEngine.SetReactions` returns the agent's refusal BEFORE `runner.Replace` and does not park a refused gen in `pendingGeneration`, else the bind replay re-installs it and the Runner half swaps alone (a half-swapped Generation, ADR 0076 A8). `TestAdviseGoHandlerAtFileChangedIsNarrowed` (`internal/agent/advise_test.go`) arms an `OriginEngine` Go handler through `SetReactions` and `Generation.Validate` refuses a non-user Sync entry: re-route that fixture through a package-private arming helper (or give it `OriginUser` + an explicit note). `TestNewRefusesAMalformedList` (`internal/reactions/runner_test.go`) and the "an entry that does not validate" / "a sync entry that does not validate" rows of `TestGenerationValidateAcceptsAnObserveListAndRefusesTheRest` / `TestGenerationValidateGuardsTheSyncLane` (`internal/domain/reaction_test.go`) pin the validation the item removes: move the malformed-entry rows to the new caller-side tests; `apogee.NewReactionRunner` (`apogee.go`) is the embedder door that loses the check — say so in its doc. Supersedes `runner.go`'s and `runner_test.go`'s recorded intent ("the Runner validates what it is handed, so a root that skipped the config layer's own check cannot start a Reaction") — both are rewritten.

**Files:** `internal/domain/reaction.go`, `internal/domain/reaction_test.go`, `internal/reactions/runner.go`, `internal/reactions/runner_test.go`, `internal/agent/agent.go`, `internal/agent/reactions.go`, `internal/agent/reactions_test.go`, `internal/agent/advise_test.go`, `internal/agent/setreactions_test.go`, `internal/agent/setlive_test.go`, `internal/agent/subagent_test.go`, `internal/agent/floorguards_test.go`, `internal/run/run.go`, `cmd/apogee/wire_engine.go`, `cmd/apogee/wire_engine_test.go`, `cmd/apogee/wire_settings_test.go`, `cmd/apogee/wire_helpers_test.go`, `apogee.go`, `CONTEXT.md`.
**Read first:** internal/agent/agent.go — SetReactions; internal/agent/reactions.go — armReactions, guardIDs; internal/domain/reaction.go — Generation.Validate, SplitLanes; internal/reactions/runner.go — New, Replace; cmd/apogee/wire_engine.go — lateEngine.SetReactions, pendingGeneration; internal/agent/advise_test.go — TestAdviseGoHandlerAtFileChangedIsNarrowed

**Tests.** `TestReplaceRefusesAMalformedListAndKeepsRunning` moves its refusal to the caller (`TestSetReactionsRefusesAMalformedGeneration`); `TestNewRefusesAMalformedList` and the two malformed-entry rows in `internal/domain/reaction_test.go` move to the caller-side tests; `TestAdviseGoHandlerAtFileChangedIsNarrowed` re-routed per the guard; `TestSetReactionsRefusesAReservedBuiltinID` new; `TestNewRejectsReactionsItCannotArm` stays green; a `lateEngine` test that a refused Generation neither reaches `runner.Replace` nor lands in `pendingGeneration`.

**Acceptance.** `go build ./... && go test ./internal/domain/... ./internal/reactions/... ./internal/agent/ ./internal/run/... ./cmd/apogee/ -run 'Validate|SetReactions|Arm|Replace'`; `make lint` clean (errcheck on every `SetReactions` caller).

Commit: `fix(agent): SetReactions validates and reserves builtin ids; consumers stop re-validating`

## 13. `turnLifecycle` owns the Exchange state whole

**What.** Move `pendingInput`, `wrapUp`, `compactSat`, `compactFailed`, `fillRung`, `lastFault` from `Agent` (`internal/agent/agent.go`) into `turnLifecycle` (`internal/agent/turn.go`) behind verbs — `open`, `close`, `rollback`, `capped`, `noteFault`, `restore(snapshot)`; producers/consumers: `loop.go`, `compact.go`, `rebind.go` (Rebind and SwitchUpstream clear via one `reset` verb), `state.go` (`restoreState` calls `restore`, which clears `compactFailed`/`compactSat` — in-passing defect: `RestoreSession` left them latched). Tests stop writing `a.turns.*` fields directly and drive the verbs. Depends on item 1.

**Regression guard.** `internal/agent/construct.go` is added to Files (the `turnLifecycle` literal lives there). The six fields have readers/writers in four production files the draft did not list — `builtins.go` (`a.wrapUp`), `fillnotice.go` (`a.fillRung`, `rearmFillNotice`), `subagent.go` (`a.lastFault`), `construct.go` (`&a.compactFailed`) — rule: every site `grep -n "a\.\(pendingInput\|wrapUp\|compactSat\|compactFailed\|fillRung\|lastFault\)\b" internal/agent/*.go` prints moves with the field. The closed test-file list is replaced by the rule + grep `grep -ln "a\.turns\.\|\.\(compactSat\|compactFailed\|fillRung\|wrapUp\|lastFault\|pendingInput\)\b" internal/agent/*_test.go` (16 files today — `emergencyfold_test.go`, `autocompact_guard_test.go`, `switchupstream_test.go`, `fillnotice_test.go`, `delegatereport_test.go`, `subagent_test.go`, `contextfiles_test.go`, `minilang_test.go`, `overflowrecovery_test.go`, `predictiveguard_test.go` among them); every hit is re-pointed at a verb. Item 13 executes before item 15.

**Files:** `internal/agent/turn.go`, `internal/agent/agent.go`, `internal/agent/loop.go`, `internal/agent/compact.go`, `internal/agent/rebind.go`, `internal/agent/state.go`, `internal/agent/builtins.go`, `internal/agent/fillnotice.go`, `internal/agent/subagent.go`, `internal/agent/construct.go`, `internal/agent/turn_test.go`, `internal/agent/compact_test.go`, `internal/agent/rebind_test.go`, `internal/agent/state_test.go`, and every test file the guard's grep names.
**Read first:** internal/agent/turn.go — turnLifecycle, openExchange, closeExchange, end; internal/agent/agent.go — Agent fields, Submit, AbortExchange; internal/agent/state.go — restoreState; internal/agent/fillnotice.go — rearmFillNotice; internal/agent/construct.go — lifecycle wiring

**Tests.** `TestRestoreSessionClearsCompactionLatches` (fails before the item); existing compaction/rebind tests re-pointed at verbs.

**Acceptance.** `go build ./... && go test ./internal/agent/ -run 'Turn|Exchange|Compact|Rebind|Restore'`; `grep -n "turns\.\(exchangeStart\|inExchange\|compactFailed\|compactSat\) =" internal/agent/*_test.go` prints nothing.

Commit: `refactor(agent): turnLifecycle owns the Exchange state; RestoreSession clears the compaction latches`

## 14. `growthBounds` is derived at one site

**What.** Recast at the regression check (2026-09-15). The unknown-window fallback (`compactUnknownWindowTranscriptTokens`) is applied by four readers — `loop.go`, `compact.go` (two sites), `dispatch.go`. Derive one `growthBounds{room, historyFloor, transcriptBudget}` as a pure function of `a.budget()`/cfg that each of the four readers calls at read time (see the guard); the substitution happens in one place. Depends on item 13.

**Regression guard.** growthBounds is a PURE derivation off a.budget()/cfg that every reader calls at read time (still the one substitution site), never a value cached at Turn open — Agent.Compact (/compact, outside an Exchange) and a SwitchUpstream between Turns see the live window; requestExceedsWindow's `b.Used == 0` margin stays a live read. Acceptance: `grep -n "compactUnknownWindowTranscriptTokens" internal/agent/*.go | grep -v '_test\|//\|= 3072'` shows one site. Second round: the four readers draw on TWO windows — `room` and `transcriptBudget` derive off the ADVERTISED window (`b.Window`/`a.cfg.Context.MaxContextTokens`, as requestExceedsWindow and compactTranscriptChars read today), `historyFloor` off the WORKING-room allocation `b.History` (historyExceedsAllocation, structuralFloor) — per `domain/config.go`'s WorkingWindow split ("the ADVERTISED window still drives overflow detection"); growthBounds is never derived from one number. The table test gains a working-window row (WorkingWindow < MaxContextTokens) asserting compactTranscriptChars and structuralFloor keep today's numbers. growthBounds is a free function or a method off `a.budget()`, never hung on turnLifecycle — `turn.go` is out of Files.

**Files:** `internal/agent/loop.go`, `internal/agent/compact.go`, `internal/agent/dispatch.go`, `internal/agent/turn_test.go`.
**Read first:** internal/agent/compact.go — compactTranscriptChars, historyExceedsAllocation, compactUnknownWindowTranscriptTokens; internal/agent/loop.go — requestExceedsWindow, budget; internal/agent/dispatch.go — structuralFloor; internal/agent/rebind.go — SwitchUpstream window write; internal/domain/config.go — ContextConfig.WorkingWindow

**Tests.** One table test over `growthBounds` (known window, unknown window, zero reserve, working-window smaller than the advertised window — compactTranscriptChars and structuralFloor keep today's numbers); a row that a `SwitchUpstream` between Turns changes what `/compact` (`Agent.Compact`, outside an Exchange) reads; the four readers' existing tests stay green.

**Acceptance.** `go build ./... && go test ./internal/agent/ -run 'Growth|Window|Compact'`; `grep -n "compactUnknownWindowTranscriptTokens" internal/agent/*.go | grep -v '_test\|//\|= 3072'` shows one site.

Commit: `refactor(agent): the unknown-window fallback is applied at one site`

## 15. One `exchangeObserver` replaces the `*bool` and two callbacks

**What.** `turnLifecycle` reaches back into `Agent` through a `*compactFailed` pointer and `onClose`/`onRollback` callbacks (`internal/agent/turn.go`). Replace the three with one small `exchangeObserver` interface the Agent implements; after item 13 the pointer target lives in the lifecycle itself, so only the two notifications remain. Depends on item 13.

**Regression guard.** `internal/agent/construct.go` is added to Files (the `turnLifecycle` literal assigns `compactFailed:`, `onClose:` and `onRollback:` there). Rule: `grep -n "onClose\|onRollback\|compactFailed:" internal/agent/*.go` names every site — the comments in `agent.go`, `fillnotice.go` and `loop.go` that name the callbacks by field included. A nil observer stays inert: `turn_test.go` builds bare `turnLifecycle{conv: …}` literals with no Agent. Item 13 executes before this item.

**Files:** `internal/agent/turn.go`, `internal/agent/agent.go`, `internal/agent/construct.go`, `internal/agent/fillnotice.go` (comment), `internal/agent/loop.go` (comment), `internal/agent/turn_test.go`.
**Read first:** internal/agent/turn.go — turnLifecycle.onClose, onRollback, compactFailed, closeExchange, end; internal/agent/construct.go — lifecycle literal wiring; internal/agent/agent.go — closeUndoGroup; internal/agent/fillnotice.go — rearmFillNotice

**Tests.** `TestTurnLifecycleNotifiesItsObserver` (close and rollback each once); `TestTurnEnd_Table` / `TestOpenExchange` stay green over bare literals (nil observer).

**Acceptance.** `go build ./... && go test ./internal/agent/ -run 'Turn|Observer'`.

Commit: `refactor(agent): turnLifecycle notifies one exchangeObserver`

## 16. `Interject` resolves references under the caller's context

**What.** `Agent.Interject` (`internal/agent/interject.go`) resolves `@file`/PDF references under `context.Background()`, so a cancelled Step cannot stop it. Take a `ctx` parameter and resolve under it; the TUI worker (`internal/tui/worker.go` — `deliverInterjections`) and `children.go`'s mailbox drain pass the Step's context. Depends on item 1.

**Regression guard.** Files cover every implementer of the tui.Engine interface method and every test double — internal/tui/tui.go (Engine), the fake engines in internal/tui/*_test.go, cmd/apogee's engine wrapper (lateEngine in wire_engine.go) — enumerated by `grep -rn "Interject(" --include=*.go .` at implement time (minus `InterjectChild`; `internal/agent/filerefs_test.go` and `undo_group_test.go` call `a.Interject(domain.UserInput{…})` and take the ctx too). The drafted test cannot be written against the change as stated: `resolveFileRefs` returns a string, never an error — a cancelled ctx only turns a PDF ref into `ExtractPDF`'s "cancelled" failMessage → a `refIgnored` ErrorEvent, and Interject still commits the message exactly as Step's own path does; returning `ctx.Err()` would add a third refusal the Engine contract (`tui.go`) does not list, and a refusal STOPS the drain (`worker.go`; `children.go` → `reportUndelivered`). Either recast the test to the existing fate — pre-cancelled ctx + `copyPDFFixture` "minimal.pdf" via `interjectAgentAtBoundary`/`interjectConfig(&recordingSink{})`: Interject returns nil, the tail message carries the text and no "Referenced file" block, the sink holds one loop ErrorEvent containing "cancelled" — or, if the ctx error IS wanted, state it as a contract change (Engine doc comment; `drainMailbox` reporting the refused tail undelivered) so Interject and Step deliberately diverge; a plain-text ref (`readFileRef`, no ctx) cannot observe the cancel, only a PDF can. Supersedes `internal/agent/interject.go`'s comment recording `context.Background` as the intended choice ("the Turn's own context is not in scope at this seam") — rewrite it, never leave it contradicting the code.

**Files:** `internal/agent/interject.go`, `internal/agent/interject_test.go`, `internal/agent/filerefs_test.go`, `internal/agent/undo_group_test.go`, `internal/agent/children.go`, `internal/tui/tui.go`, `internal/tui/worker.go`, `internal/tui/seam_test.go`, `internal/tui/interject_test.go`, `cmd/apogee/wire_engine.go`, and every other file `grep -rn "Interject(" --include=*.go .` names at implement time.
**Read first:** internal/agent/interject.go — Agent.Interject; internal/agent/loop.go — resolveFileRefs, refIgnored; internal/tui/tui.go — Engine.Interject; internal/tui/worker.go — deliverInterjections; cmd/apogee/wire_engine.go — lateEngine.Interject; internal/agent/children.go — drainMailbox; internal/agent/filerefs_test.go — copyPDFFixture

**Tests.** Per the guard: either the recast test (pre-cancelled ctx, a PDF ref, `Interject` returns nil, no "Referenced file" block, one "cancelled" ErrorEvent) or — as a stated contract change — a test that `Interject` returns the ctx error and the drain reports the tail undelivered; the fake engines and `TestInterject*` compile against the new signature.

**Acceptance.** `go build ./... && go test ./internal/agent/ ./internal/tui/ ./cmd/apogee/ -run 'Interject'`.

Commit: `fix(agent): Interject resolves references under the caller's context`
